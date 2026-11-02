// Package questiontool implements the AskUserQuestion tool: it asks the human a
// structured question mid-turn and returns the answer to the model.
//
// Claude Code ships this as a tool with no side effects, which is the point — an
// agent that can only guess or stall gains a third option. The transport is
// a1's existing permission modal: the asker is attached to the turn context by
// the controller, so this package never imports the TUI.
package questiontool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

const toolName = "ask_user"

var description = `Ask the user one multiple-choice question mid-turn; the answer comes
back as the tool result. For genuine forks only: an ambiguous
requirement, an approach choice, a preference only the user knows. Not
a confirmation dialog, not a question a tool can answer - if the choice
is obvious, make it yourself.

Put your preferred option first and append " (Recommended)" to its
label; give every option a one-line description. 2-4 options, one
question per call. The user may answer free-form, or dismiss the
question - then continue with your best judgment, not re-asking.`

// QuestionTool returns the ask_user tool definition.
func QuestionTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        toolName,
			Description: description,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"header": llm.Object{
						"type":        "string",
						"description": "Question label, max 12 chars.",
					},
					"question": llm.Object{
						"type":        "string",
						"description": "The full question text.",
					},
					"options": llm.Object{
						"type":        "array",
						"description": "The choices, best first. 2 to 4.",
						"items": llm.Object{
							"type": "object",
							"properties": llm.Object{
								"label": llm.Object{
									"type":        "string",
									"description": "Short choice label.",
								},
								"description": llm.Object{
									"type":        "string",
									"description": "What this choice means.",
								},
							},
							"required": []string{"label"},
						},
					},
					"multi_select": llm.Object{
						"type":        "boolean",
						"description": "Allow selecting more than one.",
					},
				},
				Required: []string{"question", "options"},
			},
		},
		// DetailFromArgs gives the transcript row a stable identity for the
		// whole run — the question text, which never changes once written.
		DetailFromArgs: func(input json.RawMessage) string {
			var in inputSchema
			if err := json.Unmarshal(input, &in); err != nil {
				return ""
			}
			return strings.TrimSpace(in.Question)
		},
		Run: run,
	}
}

type inputSchema struct {
	Header   string `json:"header"`
	Question string `json:"question"`
	Options  []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
	MultiSelect bool `json:"multi_select"`
}

const (
	minOptions = 2
	maxOptions = 4
	// maxHeaderRunes matches the reference's chip width.
	maxHeaderRunes = 12
)

func run(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in inputSchema
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse ask_user arguments: %w", err)
	}

	q := strings.TrimSpace(in.Question)
	if q == "" {
		return tooldef.Result{}, fmt.Errorf("question is required")
	}
	if len(in.Options) < minOptions || len(in.Options) > maxOptions {
		return tooldef.Result{}, fmt.Errorf(
			"ask_user needs %d to %d options, got %d — a question outside that range is a decision you should make yourself",
			minOptions,
			maxOptions,
			len(in.Options),
		)
	}
	header := strings.TrimSpace(in.Header)
	if runes := []rune(header); len(runes) > maxHeaderRunes {
		header = string(runes[:maxHeaderRunes-1]) + "…"
	}

	ask := tooldef.QuestionFuncFrom(ctx)
	if ask == nil {
		// Fail closed and say why: a silent hang here would look like a hung
		// model rather than a wiring mistake.
		return tooldef.Result{}, fmt.Errorf("ask_user is unavailable: no question handler attached to this session")
	}

	question := tooldef.Question{Header: header, Question: q, MultiSelect: in.MultiSelect}
	for _, o := range in.Options {
		label := strings.TrimSpace(o.Label)
		if label == "" {
			return tooldef.Result{}, fmt.Errorf("every option needs a label")
		}
		question.Options = append(question.Options, tooldef.QuestionOption{
			Label:       label,
			Description: strings.TrimSpace(o.Description),
		})
	}

	res, err := ask(ctx, question)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("asking the user failed: %w", err)
	}
	if res.Cancelled {
		return tooldef.Result{Content: "The user dismissed the question without answering. " +
			"Do not re-ask immediately; continue with your best judgment, or state plainly that you need the answer to proceed."}, nil
	}

	answer := res.Choice
	if answer == "" && len(res.Choices) > 0 {
		answer = res.Choices[0]
	}
	if answer == "" {
		return tooldef.Result{Content: "The user answered without choosing an option. " +
			"Treat it as an instruction to continue and note what you did."}, nil
	}

	body := answer
	if len(res.Choices) > 1 {
		body = strings.Join(res.Choices, ", ")
	}
	if in.MultiSelect && len(res.Choices) > 1 {
		body = fmt.Sprintf("%s (selected %d)", strings.Join(res.Choices, ", "), len(res.Choices))
	}
	return tooldef.Result{Content: body}, nil
}
