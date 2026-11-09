// Package mailtool exposes the personal Maildir to the agent: one
// `personal_mail` facade with an action enum (mailboxes | messages |
// headers), the calendartool shape minus every write. The Maildir is
// READ-ONLY by ruling (dashboard chunk 4): there is no action here that
// sends, moves, marks, or deletes a message, and none will be added —
// personal_write grants nothing on this surface, which is why Deps has
// no write hook at all. The ruling is structural, not advisory.
//
// One privacy line goes further than read-only: the model reads the
// ENVELOPE, never the letter. maildir.Open (bodies) is a webui
// privilege; the facade's per-message surface is maildir.Summary —
// headers, flags, size. The model can triage mail it may not read.
//
// The flags are enforced TWICE, by design. The engine mounts the tool
// only when the master switch is on and the model opted into reads; and
// every call re-reads the LIVE state — a mis-mounted tool (a stale mount
// after a config flip, a hand-built engine, a future wiring mistake)
// still cannot read anything the operator did not consent to.
package mailtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/maildir"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

const mailGuidance = `Read the user's local Maildir (the same one the dashboard's mail widget shows). Read-only by ruling: there is no action that sends, moves, marks, or deletes mail, and bodies are not exposed to you — the envelope is your surface, the letter stays with the human.

Pick ONE action:
- mailboxes: the mailbox rail with totals and unread counts.
- messages: one mailbox's latest messages (box required, e.g. box=inbox; limit optional, default 25, clamped to 100).
- headers: one message's headers by id (box + id, both exactly as messages reports them).

Reads need the model's personal_read. If a call is refused, say so — do not retry with different arguments.`

// Deps wires the facade to the live configuration. Both funcs are read
// at CALL time (the mount-time check is a convenience, not the
// enforcement): MailDir returns the live root ("" = unconfigured), and
// ReadAllowed folds the master switch together with the active model's
// personal_read opt-in. Nil funcs are treated as locked — the safe
// default. There is deliberately no write hook.
type Deps struct {
	MailDir     func() string
	ReadAllowed func() bool
}

// MailTool returns the personal_mail facade (nil MailDir → nil).
func MailTool(deps Deps) tooldef.Tool {
	if deps.MailDir == nil {
		return tooldef.Tool{}
	}
	readAllowed := deps.ReadAllowed
	if readAllowed == nil {
		readAllowed = func() bool { return false }
	}
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "personal_mail",
			Description: mailGuidance,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"action": llm.Object{
						"type":        "string",
						"description": "mailboxes | messages | headers",
						"enum":        []string{"mailboxes", "messages", "headers"},
					},
					"box": llm.Object{
						"type":        "string",
						"description": "messages/headers: the mailbox name, as mailboxes reports it.",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": "messages only: how many latest messages (default 25, clamped to 100).",
					},
					"id": llm.Object{
						"type":        "string",
						"description": "headers only: the message id, exactly as messages reports it.",
					},
				},
				Required: []string{"action"},
			},
		},
		DetailFromArgs: mailDetail,
		Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
			var in mailInput
			if err := json.Unmarshal(input, &in); err != nil {
				return tooldef.Result{}, err
			}
			switch strings.ToLower(strings.TrimSpace(in.Action)) {
			case "mailboxes":
				return runMailboxes(deps.MailDir, readAllowed)
			case "messages":
				return runMessages(deps.MailDir, readAllowed, in)
			case "headers":
				return runHeaders(deps.MailDir, readAllowed, in)
			default:
				return tooldef.Result{}, fmt.Errorf("personal_mail: unknown action %q (want mailboxes, messages, or headers)", in.Action)
			}
		},
	}
}

// mailInput is the superset of every action's arguments; each action
// reads the fields it owns and ignores the rest (the schema documents
// ownership).
type mailInput struct {
	Action string `json:"action"`
	Box    string `json:"box"`
	Limit  int    `json:"limit"`
	ID     string `json:"id"`
}

// ---------- shared plumbing ----------

// requireRead is the call-time gate, then the directory.
func requireRead(dirFn func() string, allowed func() bool) (string, error) {
	if !allowed() {
		return "", errors.New("personal_mail: reads are not enabled for this model (personal.enabled plus the model's personal_read flag in config.yaml)")
	}
	dir := strings.TrimSpace(dirFn())
	if dir == "" {
		return "", errors.New("personal_mail: no Maildir root is configured (personal.mail_dir in config.yaml)")
	}
	return dir, nil
}

// messageJSON is one message as the model sees it: the envelope, never
// a filesystem path and never a body.
type messageJSON struct {
	ID       string `json:"id"`
	From     string `json:"from,omitempty"`
	To       string `json:"to,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Date     string `json:"date"`
	Size     int64  `json:"size"`
	Seen     bool   `json:"seen"`
	Answered bool   `json:"answered"`
	Flagged  bool   `json:"flagged"`
	Trashed  bool   `json:"trashed"`
}

func messageToJSON(m maildir.Message) messageJSON {
	return messageJSON{
		ID:       m.ID,
		From:     m.From,
		To:       m.To,
		Subject:  m.Subject,
		Date:     m.Date.Format(timeFormat),
		Size:     m.Size,
		Seen:     m.Seen,
		Answered: m.Answered,
		Flagged:  m.Flagged,
		Trashed:  m.Trashed,
	}
}

// timeFormat is the wire format for dates (calendartool's choice too).
const timeFormat = "2006-01-02T15:04:05Z07:00"

// bounds for the messages action.
const (
	messagesDefault = 25
	messagesMax     = 100
)

// ---------- reads ----------

func runMailboxes(dirFn func() string, allowed func() bool) (tooldef.Result, error) {
	dir, err := requireRead(dirFn, allowed)
	if err != nil {
		return tooldef.Result{}, err
	}
	boxes, err := maildir.Mailboxes(dir)
	if err != nil {
		return tooldef.Result{}, err
	}
	out := make([]map[string]any, 0, len(boxes))
	for _, b := range boxes {
		out = append(out, map[string]any{
			"name":   b.Name,
			"total":  b.Total,
			"unread": b.Unread,
		})
	}
	return jsonResult(map[string]any{
		"count":     len(out),
		"mailboxes": out,
	}, "mailboxes")
}

func runMessages(dirFn func() string, allowed func() bool, in mailInput) (tooldef.Result, error) {
	dir, err := requireRead(dirFn, allowed)
	if err != nil {
		return tooldef.Result{}, err
	}
	box := strings.TrimSpace(in.Box)
	if box == "" {
		return tooldef.Result{}, errors.New("personal_mail: messages requires a box (see the mailboxes action)")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = messagesDefault
	}
	if limit > messagesMax {
		limit = messagesMax
	}
	msgs, st, err := maildir.List(dir, box, limit)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("personal_mail: %w", err)
	}
	out := make([]messageJSON, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, messageToJSON(m))
	}
	return jsonResult(map[string]any{
		"box":      box,
		"count":    len(out),
		"messages": out,
		"stats": map[string]any{
			"files":    st.Files,
			"skipped":  st.Skipped,
			"degraded": st.Degraded(),
		},
	}, fmt.Sprintf("messages %s", truncate(box, 40)))
}

func runHeaders(dirFn func() string, allowed func() bool, in mailInput) (tooldef.Result, error) {
	dir, err := requireRead(dirFn, allowed)
	if err != nil {
		return tooldef.Result{}, err
	}
	box := strings.TrimSpace(in.Box)
	if box == "" {
		return tooldef.Result{}, errors.New("personal_mail: headers requires a box")
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return tooldef.Result{}, errors.New("personal_mail: headers requires an id (as the messages action reports)")
	}
	msg, err := maildir.Summary(dir, box, id)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("personal_mail: %w", err)
	}
	return jsonResult(map[string]any{
		"box":     box,
		"message": messageToJSON(msg),
	}, fmt.Sprintf("headers %s", truncate(id, 40)))
}

// ---------- UI detail ----------

func mailDetail(input json.RawMessage) string {
	var in mailInput
	_ = json.Unmarshal(input, &in)
	switch strings.ToLower(strings.TrimSpace(in.Action)) {
	case "mailboxes":
		return "mailboxes"
	case "messages":
		return "messages " + truncate(in.Box, 40)
	case "headers":
		return "headers " + truncate(in.ID, 40)
	default:
		return "personal_mail"
	}
}

func jsonResult(body map[string]any, detail string) (tooldef.Result, error) {
	b, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return tooldef.Result{}, err
	}
	return tooldef.Result{Content: string(b), Detail: detail, Output: string(b)}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
