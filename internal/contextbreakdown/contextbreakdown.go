// Package contextbreakdown estimates how a turn's context budget is spent.
//
// Claude Code shows a per-category token accounting — system prompt, system
// tools, MCP tools, agents, then the messages — as a grid of squares sized by
// each category's share of the window. a1's version is deliberately an
// *estimate*: there is no tokenizer dependency, so the counts are derived from
// bytes with a documented ratio and labelled as approximate wherever they are
// shown. Guessing at exactness would be worse than admitting the ratio.
package contextbreakdown

import (
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/agent/prompt"
	"github.com/damien1141/a1/internal/session"
)

// BytesPerToken is the estimate's conversion factor. 4 bytes/token is the usual
// English-text heuristic and is what every number here is built on; it is stated
// in the output so the user can discount it.
const BytesPerToken = 4

// Category is one line of the breakdown.
type Category struct {
	Name string
	// Tokens is the estimated count.
	Tokens int
	// Note explains an entry that is not a plain estimate — the "deferred"
	// categories count schema bytes that do not enter the context window.
	Note string
}

// Breakdown is the whole accounting for one turn.
type Breakdown struct {
	Categories []Category
	// ContextWindow is the model's usable window; 0 when unknown.
	ContextWindow int
	// Reserve is the headroom kept for the reply, if the caller knows it.
	Reserve int
}

// Total sums the categories that actually consume context.
func (b Breakdown) Total() int {
	n := 0
	for _, c := range b.Categories {
		if c.Note == "" {
			n += c.Tokens
		}
	}
	return n
}

// UsedPercent is the share of the window the breakdown accounts for.
func (b Breakdown) UsedPercent() int {
	if b.ContextWindow <= 0 {
		return 0
	}
	pct := b.Total() * 100 / b.ContextWindow
	return min(pct, 999)
}

// Estimate renders the breakdown as text for a /context response.
//
// The header states the ratio and that these are estimates. Presenting them as
// exact would be the one way this feature could mislead.
func (b Breakdown) Estimate() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Context estimate (~%d bytes/token, provider counts will differ)\n\n", BytesPerToken)

	width := 0
	for _, c := range b.Categories {
		if l := len(c.Name); l > width {
			width = l
		}
	}
	for _, c := range b.Categories {
		tokens := formatThousands(c.Tokens)
		note := ""
		if c.Note != "" {
			note = "  " + c.Note
		}
		fmt.Fprintf(&sb, "%-*s  %10s%s\n", width, c.Name, tokens, note)
	}
	fmt.Fprintf(&sb, "\n")
	total := formatThousands(b.Total())
	if b.ContextWindow > 0 {
		fmt.Fprintf(&sb, "Total %s / %s (%d%% used)\n",
			total, formatThousands(b.ContextWindow), b.UsedPercent())
	} else {
		fmt.Fprintf(&sb, "Total %s (window unknown for this model)\n", total)
	}
	return sb.String()
}

// Build assembles the breakdown for a snapshot. toolBytes/toolCount come from
// the engine (Engine.ToolSchemaBytes), which owns the registry.
//
// The message body count is the sum of every message's text, thinking, and tool
// payload — which is what actually sits in the request. Usage counters from the
// provider are *not* used here: they report what one call cost, not what the
// current context holds, and conflating them is how you get a readout that
// disagrees with the provider's own billing.
func Build(snap session.Snapshot, contextWindow, toolBytes, toolCount int) Breakdown {
	var b Breakdown
	b.ContextWindow = contextWindow

	b.Categories = append(b.Categories, Category{
		Name:   "System prompt",
		Tokens: PromptTokens(prompt.Build("", false, 0, nil)),
	})

	if toolCount > 0 {
		b.Categories = append(b.Categories, Category{
			Name:   "Tools",
			Tokens: estimateTokens(toolBytes),
			Note:   fmt.Sprintf("%d registered", toolCount),
		})
	}

	// Tool outputs are counted once from the map, not once per message: a
	// per-message loop would multiply every output by the number of messages.
	var msgs, thinking, toolIO int
	for _, m := range snap.Messages {
		msgs += len(m.Text)
		for _, blk := range m.Content {
			switch blk.Type {
			case session.BlockText:
				msgs += len(blk.Text)
			case session.BlockThinking:
				thinking += len(blk.Text)
			case session.BlockToolUse:
				toolIO += len(blk.Input)
			}
		}
	}
	for _, r := range snap.Tools {
		toolIO += len(r.Output) + len(r.Error)
	}

	if msgs > 0 {
		b.Categories = append(b.Categories, Category{Name: "Messages", Tokens: estimateTokens(msgs)})
	}
	if thinking > 0 {
		b.Categories = append(b.Categories, Category{Name: "Thinking", Tokens: estimateTokens(thinking)})
	}
	if toolIO > 0 {
		b.Categories = append(b.Categories, Category{Name: "Tool output", Tokens: estimateTokens(toolIO)})
	}
	return b
}

// PromptTokens estimates prompt tokens from a rendered prompt string.
func PromptTokens(s string) int { return estimateTokens(len(s)) }

func estimateTokens(n int) int { return (n + BytesPerToken - 1) / BytesPerToken }

func formatThousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
