package session

import (
	"fmt"
	"strings"
)

// ItemKind classifies a projected transcript row.
type ItemKind int

// ItemKind values for projected transcript rows.
const (
	ItemUser ItemKind = iota
	ItemThinking
	ItemAssistant
	ItemTool
	ItemCompaction
)

// Item is one list row projected from Snapshot.
type Item struct {
	ID   string
	Kind ItemKind

	Text  string
	State State

	Thinking    string
	Streaming   bool
	Interrupted bool

	ToolRun ToolRun

	// TokensBefore is the pre-cut context size on ItemCompaction rows.
	TokensBefore int
}

// Collapsible read-only tools: a run of these with nothing meaningful between
// them collapses into a single summary row, the way Claude Code folds
// consecutive reads and greps. Without it a 15-file exploration is 15 large
// cards — which is exactly what happened once read/grep grew their own card
// renderer.
var collapsibleReadTools = map[string]bool{
	"read": true, "grep": true, "ls": true, "glob": true, "find": true,
	// fetch folds too: a turn that pulls eight docs is eight rows otherwise, and
	// each is a single glanceable line that costs a lot of vertical space. The
	// newest one is kept as the group's hint.
	//
	// browser does NOT fold. Its calls are a sequential chain over one page —
	// open, navigate, click, type — where each step depends on the previous one.
	// Collapsing that into a count hides the order and what each step did, and
	// the tool is not Readable so it cannot be batched anyway.
	"fetch": true,
}

// CollapseGroups flattens a message's content blocks into grouped items.
// Consecutive read-only tool calls collapse into one item whose ToolRun carries
// the group, so the transcript shows "Read 12 files, searched for 3 patterns"
// instead of fifteen rows. A text block, a non-collapsible tool (a write, a
// bash, an agent spawn), or an error breaks the run — an error must stay
// visible on its own.
func CollapseGroups(blocks []ContentBlock, tools map[string]ToolRun) []Item {
	var items []Item
	var run []ContentBlock

	flush := func() {
		if len(run) == 0 {
			return
		}
		// A single call is not a group: collapsing it would hide one card behind
		// an expand arrow for no gain. Two or more is where rows start to crowd.
		if len(run) == 1 {
			b := run[0]
			items = append(items, Item{
				ID:   "tool-" + b.ID,
				Kind: ItemTool,
				ToolRun: mergeToolRun(ToolRun{
					ToolUseID: b.ID,
					Name:      b.Name,
					Status:    ToolInProgress,
					Detail:    b.Input,
				}, tools),
			})
			run = nil
			return
		}
		items = append(items, collapseRun(run, tools))
		run = nil
	}

	for _, b := range blocks {
		// Only tool_use blocks belong here; thinking and text are the caller's,
		// and emitting them here is what duplicated them into the item list.
		if b.Type != BlockToolUse {
			flush()
			continue
		}
		if collapsibleReadTools[strings.ToLower(b.Name)] && !toolRunErrored(tools[b.ID]) {
			run = append(run, b)
			continue
		}
		flush()
		items = append(items, Item{
			ID:   "tool-" + b.ID,
			Kind: ItemTool,
			ToolRun: mergeToolRun(ToolRun{
				ToolUseID: b.ID,
				Name:      b.Name,
				Status:    ToolInProgress,
				Detail:    b.Input,
			}, tools),
		})
	}
	flush()
	return items
}

// toolRunErrored reports whether a run failed, so a failed read stays a row of
// its own rather than being folded into a count the user cannot act on.
func toolRunErrored(run ToolRun) bool {
	switch run.Status {
	case ToolError, ToolCancelled, ToolRejected:
		return true
	default:
		return false
	}
}

// collapseRun turns a run of read-only calls into one grouped ToolRun. The
// counts live in the ToolRun so the widget has nothing to recompute, and the
// member ids are kept for expansion.
func collapseRun(blocks []ContentBlock, tools map[string]ToolRun) Item {
	var reads, searches, lists, fetched int
	var ids []string
	var latestHint string

	for _, b := range blocks {
		ids = append(ids, b.ID)
		switch strings.ToLower(strings.TrimSpace(b.Name)) {
		case "read":
			reads++
		case "grep", "glob", "find":
			searches++
		case "ls":
			lists++
		case "fetch", "browser":
			fetched++
		default:
			// A collapsible tool with no bucket of its own still counts as a
			// read for the summary, so the row never says "Read 0 files" for a
			// group that clearly read something.
			reads++
		}
		run := tools[b.ID]
		if !toolRunErrored(run) && strings.TrimSpace(run.Detail) != "" {
			latestHint = strings.TrimSpace(run.Detail)
		}
	}

	// A member with no live run is one the executor has not reported on yet, so
	// it is still outstanding: an unreported member must not let the group claim
	// completion. The content-block fallback is ToolInProgress for exactly this
	// reason, and mergeToolRun is what turns a missing member into it.
	status := ToolDone
	for _, b := range blocks {
		run := mergeToolRun(ToolRun{
			ToolUseID: b.ID,
			Name:      b.Name,
			Status:    ToolInProgress,
			Detail:    b.Input,
		}, tools)
		if run.Status == ToolInProgress {
			status = ToolInProgress
			break
		}
		if toolRunErrored(run) {
			status = run.Status
		}
	}

	return Item{
		ID:   "tool-group-" + blocks[0].ID,
		Kind: ItemTool,
		ToolRun: ToolRun{
			ToolUseID: blocks[0].ID,
			Name:      "read_group",
			Status:    status,
			Detail:    groupedDetail(reads, searches, lists, fetched),
			Group: &ToolGroup{
				Members:  ids,
				Reads:    reads,
				Searches: searches,
				Lists:    lists,
				Fetched:  fetched,
				Hint:     latestHint,
			},
		},
	}
}

// groupedDetail is the row's one-line summary: "Read 12 files, searched for 3
// patterns, fetched 2 pages". Only non-zero parts appear, and the pluralization
// matches.
func groupedDetail(reads, searches, lists, fetched int) string {
	var parts []string
	if reads > 0 {
		parts = append(parts, plural("Read %d", "file", "files", reads))
	}
	if searches > 0 {
		parts = append(parts, plural("searched for %d", "pattern", "patterns", searches))
	}
	if lists > 0 {
		parts = append(parts, plural("listed %d", "directory", "directories", lists))
	}
	if fetched > 0 {
		parts = append(parts, plural("fetched %d", "page", "pages", fetched))
	}
	return strings.Join(parts, ", ")
}

// plural renders one summary part, e.g. ("Read %d file", "files", 3) →
// "Read 3 files" and ("Read %d file", "files", 1) → "Read 1 file".
// plural renders one summary part: plural("Read %d", "file", "files", 3) →
// "Read 3 files", and with n == 1 → "Read 1 file".
func plural(format, one, many string, n int) string {
	if n == 1 {
		return fmt.Sprintf(format, 1) + " " + one
	}
	return fmt.Sprintf(format, n) + " " + many
}

// Project flattens Snapshot into list items in content order:
// user → thinking / assistant text / tool rows.
func Project(s Snapshot) []Item {
	var items []Item
	for _, m := range s.Messages {
		switch m.Role {
		case RoleUser:
			if strings.TrimSpace(m.Text) != "" || len(m.Images) > 0 {
				items = append(items, Item{
					ID:   m.ID,
					Kind: ItemUser,
					Text: m.Text,
				})
			}
		case RoleAssistant:
			items = append(items, projectAssistant(m, s.Tools)...)
		case RoleCompaction:
			items = append(items, Item{
				ID:           m.ID,
				Kind:         ItemCompaction,
				TokensBefore: m.TokensBefore,
			})
		case RoleLocalBash:
			items = append(items, Item{
				ID:   "bash-" + m.ID,
				Kind: ItemTool,
				ToolRun: mergeToolRun(ToolRun{
					ToolUseID: m.ID,
					Name:      "bash",
					Status:    ToolInProgress,
					Detail:    m.Text,
					Local:     true,
				}, s.Tools),
			})
		}
	}
	return items
}

func projectAssistant(m Message, tools map[string]ToolRun) []Item {
	var items []Item
	textSeg := 0

	emitText := func(text string, streamingTail bool) {
		if text == "" && !streamingTail {
			return
		}
		st := m.State
		if !streamingTail && m.State == StateStreaming {
			st = StateComplete
		}
		if m.State == StateCancelled {
			st = StateCancelled
		}
		items = append(items, Item{
			ID:    fmt.Sprintf("%s-text-%d", m.ID, textSeg),
			Kind:  ItemAssistant,
			Text:  text,
			State: st,
		})
		textSeg++
	}

	var textBuf strings.Builder
	flushText := func() {
		emitText(textBuf.String(), false)
		textBuf.Reset()
	}

	// A run of collapsible read-only calls is held here until a non-collapsible
	// block (text, thinking, any other tool) ends it, so the group renders as
	// one row in the position the calls actually occupy.
	var group []ContentBlock
	flushGroup := func() {
		if len(group) == 0 {
			return
		}
		// A single call is not a group: collapsing it would hide one card behind
		// an expand arrow for no gain. Two or more is where the rows start to
		// crowd the transcript.
		if len(group) == 1 {
			b := group[0]
			items = append(items, Item{
				ID:   "tool-" + b.ID,
				Kind: ItemTool,
				ToolRun: mergeToolRun(ToolRun{
					ToolUseID: b.ID,
					Name:      b.Name,
					Status:    ToolInProgress,
					Detail:    b.Input,
				}, tools),
			})
			group = nil
			return
		}
		items = append(items, collapseRun(group, tools))
		group = nil
	}

	for i, b := range m.Content {
		switch b.Type {
		case BlockThinking:
			if strings.TrimSpace(b.Text) == "" && m.State != StateStreaming {
				continue
			}
			flushGroup()
			flushText()
			thinkStreaming := m.State == StateStreaming && isTrailingThinking(m.Content, i)
			items = append(items, Item{
				ID:          fmt.Sprintf("%s-thinking-%d", m.ID, i),
				Kind:        ItemThinking,
				Thinking:    b.Text,
				Streaming:   thinkStreaming,
				Interrupted: m.State == StateCancelled,
			})
		case BlockText:
			textBuf.WriteString(b.Text)
			// Text ends a read run: reading then explaining is not one block of
			// reads, and folding across the prose would hide where it came from.
			flushGroup()
			flushText()
		case BlockToolUse:
			// Lowercased because the replay path restores the model's casing
			// ("Read"), which must still collapse.
			if collapsibleReadTools[strings.ToLower(strings.TrimSpace(b.Name))] &&
				!toolRunErrored(tools[b.ID]) {
				// Text before the run flushes first, preserving block order.
				flushText()
				group = append(group, b)
				continue
			}
			flushText()
			flushGroup()
			items = append(items, Item{
				ID:   "tool-" + b.ID,
				Kind: ItemTool,
				ToolRun: mergeToolRun(ToolRun{
					ToolUseID: b.ID,
					Name:      b.Name,
					Status:    ToolInProgress,
					Detail:    b.Input,
				}, tools),
			})
		}
	}
	flushGroup()

	tail := textBuf.String()
	emptyWait := m.State == StateStreaming && len(items) == 0 && tail == ""
	emitText(tail, emptyWait || (m.State == StateStreaming && tail != ""))

	if m.State == StateCancelled {
		for i := range items {
			if items[i].Kind == ItemThinking {
				items[i].Streaming = false
				items[i].Interrupted = true
			}
			if items[i].Kind == ItemAssistant {
				items[i].State = StateCancelled
			}
		}
	}
	return items
}

// mergeToolRun overlays live execution state onto the content-block fallback
// so the projected row always has Name / ToolUseID / Detail even when the
// live run omitted them.
func mergeToolRun(fallback ToolRun, live map[string]ToolRun) ToolRun {
	if live == nil {
		return fallback
	}
	tr, ok := live[fallback.ToolUseID]
	if !ok {
		return fallback
	}
	if tr.Name == "" {
		tr.Name = fallback.Name
	}
	if tr.Detail == "" {
		tr.Detail = fallback.Detail
	}
	if tr.ToolUseID == "" {
		tr.ToolUseID = fallback.ToolUseID
	}
	if fallback.Local {
		tr.Local = true
	}
	return tr
}

func isTrailingThinking(blocks []ContentBlock, i int) bool {
	for j := i + 1; j < len(blocks); j++ {
		switch blocks[j].Type {
		case BlockThinking:
			return false
		case BlockText:
			if blocks[j].Text != "" {
				return false
			}
		case BlockToolUse:
			return false
		}
	}
	return true
}
