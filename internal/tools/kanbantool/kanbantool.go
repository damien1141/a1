// Package kanbantool exposes the personal kanban board to the agent: one
// `personal_kanban` facade with an action enum (list | add | move | edit |
// delete), the calendartool shape. The board is vault-backed (there is no
// kanban directory to configure), governed by chunk 0's flags: the master
// switch (personal.enabled) plus the active model's personal_read /
// personal_write opt-ins.
//
// The flags are enforced TWICE, by design. The engine mounts the tool only
// when the switch is on and the model opted into reads; and every call
// re-reads the LIVE state — a mis-mounted tool (a stale mount after a
// config flip, a hand-built engine, a future wiring mistake) still cannot
// read or write anything the operator did not consent to.
package kanbantool

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/kanban"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

const kanbanGuidance = `Read and manage the user's personal kanban board (the same three-column board the dashboard shows: todo, doing, done).

Pick ONE action:
- list: the whole board, every column, top-to-bottom.
- add: create a card (title required; col defaults to todo; notes and position optional — position 0 is the top, omit it to append at the bottom).
- move: relocate a card by id (col = destination column, omit to keep the current one; position = target index, omit to append).
- edit: patch a card's title and/or notes by id (absent fields are untouched; an empty notes string clears it).
- delete: remove a card by id.

Cards are identified by the id that list/add return. Column names are exactly "todo", "doing", "done".

Reads need the model's personal_read; add/move/edit/delete need personal_write. If a call is refused, say so — do not retry with different arguments.`

// Deps wires the facade to the live board. All three funcs are read at
// CALL time (the mount-time check is a convenience, not the enforcement):
// DB returns the vault handle ("" nil = no vault), and the two Allowed
// funcs fold the master switch together with the active model's opt-in
// flags. Nil funcs are treated as locked — the safe default.
type Deps struct {
	DB          func() *sql.DB
	ReadAllowed func() bool
	WriteAllowed func() bool
}

// KanbanTool returns the personal_kanban facade (nil DB provider → nil).
func KanbanTool(deps Deps) tooldef.Tool {
	if deps.DB == nil {
		return tooldef.Tool{}
	}
	readAllowed := deps.ReadAllowed
	if readAllowed == nil {
		readAllowed = func() bool { return false }
	}
	writeAllowed := deps.WriteAllowed
	if writeAllowed == nil {
		writeAllowed = func() bool { return false }
	}
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "personal_kanban",
			Description: kanbanGuidance,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"action": llm.Object{
						"type":        "string",
						"description": "list | add | move | edit | delete",
						"enum":        []string{"list", "add", "move", "edit", "delete"},
					},
					"id": llm.Object{
						"type":        "string",
						"description": "move / edit / delete: the card's id, as returned by list or add.",
					},
					"col": llm.Object{
						"type":        "string",
						"description": "add (defaults to todo) / move: the column — exactly one of todo, doing, done. Move omits it to keep the current column.",
					},
					"title": llm.Object{
						"type":        "string",
						"description": "add (required) / edit: the card's one-line title (empty string on edit is rejected — a card always needs a title).",
					},
					"notes": llm.Object{
						"type":        "string",
						"description": "add / edit: free-text notes (empty string on edit clears them).",
					},
					"position": llm.Object{
						"type":        "integer",
						"description": "add / move: the target index in the column, 0 = top; omit to append at the bottom.",
					},
				},
				Required: []string{"action"},
			},
		},
		DetailFromArgs: kanbanDetail,
		Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
			var in kanbanInput
			if err := json.Unmarshal(input, &in); err != nil {
				return tooldef.Result{}, err
			}
			switch strings.ToLower(strings.TrimSpace(in.Action)) {
			case "list":
				return runList(deps.DB, readAllowed)
			case "add":
				return runAdd(deps.DB, writeAllowed, in)
			case "move":
				return runMove(deps.DB, writeAllowed, in)
			case "edit":
				return runEdit(deps.DB, writeAllowed, in)
			case "delete":
				return runDelete(deps.DB, writeAllowed, in)
			default:
				return tooldef.Result{}, fmt.Errorf("personal_kanban: unknown action %q (want list, add, move, edit, or delete)", in.Action)
			}
		},
	}
}

// kanbanInput is the superset of every action's arguments; each action
// reads the fields it owns and ignores the rest (the schema documents
// ownership). Title/notes are pointers so edit can distinguish "absent
// (untouched)" from "present but empty (clear the notes)" — the calendar
// patch semantics.
type kanbanInput struct {
	Action   string  `json:"action"`
	ID       string  `json:"id"`
	Col      string  `json:"col"`
	Title    *string `json:"title"`
	Notes    *string `json:"notes"`
	Position *int    `json:"position"`
}

// ---------- shared plumbing ----------

// requireRead is the call-time read gate: consent first, then the vault.
func requireRead(dbFn func() *sql.DB, allowed func() bool) (*sql.DB, error) {
	if !allowed() {
		return nil, errors.New("personal_kanban: reads are not enabled for this model (personal.enabled plus the model's personal_read flag in config.yaml)")
	}
	return requireDB(dbFn)
}

// requireWrite is the call-time write gate.
func requireWrite(dbFn func() *sql.DB, allowed func() bool) (*sql.DB, error) {
	if !allowed() {
		return nil, errors.New("personal_kanban: writes are not enabled for this model (personal.enabled plus the model's personal_write flag in config.yaml)")
	}
	return requireDB(dbFn)
}

func requireDB(dbFn func() *sql.DB) (*sql.DB, error) {
	db := dbFn()
	if db == nil {
		return nil, errors.New("personal_kanban: no vault is available (the board lives in a1.db)")
	}
	return db, nil
}

// cardJSON is one card as the model sees it.
type cardJSON struct {
	ID        string `json:"id"`
	Col       string `json:"col"`
	Position  int    `json:"position"`
	Title     string `json:"title"`
	Notes     string `json:"notes,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func cardToJSON(c kanban.Card) cardJSON {
	return cardJSON{
		ID:        c.ID,
		Col:       c.Col,
		Position:  c.Position,
		Title:     c.Title,
		Notes:     c.Notes,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
}

func jsonResult(body map[string]any, detail string) (tooldef.Result, error) {
	b, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return tooldef.Result{}, err
	}
	return tooldef.Result{Content: string(b), Detail: detail, Output: string(b)}, nil
}

// ---------- reads ----------

func runList(dbFn func() *sql.DB, allowed func() bool) (tooldef.Result, error) {
	db, err := requireRead(dbFn, allowed)
	if err != nil {
		return tooldef.Result{}, err
	}
	cards, err := kanban.List(db)
	if err != nil {
		return tooldef.Result{}, err
	}
	out := make([]cardJSON, 0, len(cards))
	for _, c := range cards {
		out = append(out, cardToJSON(c))
	}
	return jsonResult(map[string]any{
		"count": len(out),
		"cards": out,
	}, fmt.Sprintf("list (%d cards)", len(out)))
}

// ---------- writes ----------

func runAdd(dbFn func() *sql.DB, allowed func() bool, in kanbanInput) (tooldef.Result, error) {
	db, err := requireWrite(dbFn, allowed)
	if err != nil {
		return tooldef.Result{}, err
	}
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		return tooldef.Result{}, errors.New("personal_kanban: add requires a title")
	}
	// An omitted column is the natural default: the backlog.
	col := strings.ToLower(strings.TrimSpace(in.Col))
	if col == "" {
		col = kanban.ColTodo
	}
	added, err := kanban.Add(db, kanban.AddInput{
		Col:      col,
		Title:    *in.Title,
		Notes:    strOrEmpty(in.Notes),
		Position: in.Position,
	})
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("personal_kanban: %w", err)
	}
	return jsonResult(map[string]any{
		"added": true,
		"card":  cardToJSON(added),
	}, "add "+truncate(added.Title, 60))
}

func runMove(dbFn func() *sql.DB, allowed func() bool, in kanbanInput) (tooldef.Result, error) {
	db, err := requireWrite(dbFn, allowed)
	if err != nil {
		return tooldef.Result{}, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return tooldef.Result{}, errors.New("personal_kanban: move requires an id")
	}
	if strings.TrimSpace(in.Col) == "" && in.Position == nil {
		return tooldef.Result{}, errors.New("personal_kanban: move requires a col and/or a position (otherwise it changes nothing)")
	}
	moved, err := kanban.Move(db, kanban.MoveInput{
		ID:       id,
		Col:      strings.ToLower(strings.TrimSpace(in.Col)),
		Position: in.Position,
	})
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("personal_kanban: %w", err)
	}
	return jsonResult(map[string]any{
		"moved": true,
		"card":  cardToJSON(moved),
	}, fmt.Sprintf("move %s → %s[%d]", truncate(moved.Title, 40), moved.Col, moved.Position))
}

func runEdit(dbFn func() *sql.DB, allowed func() bool, in kanbanInput) (tooldef.Result, error) {
	db, err := requireWrite(dbFn, allowed)
	if err != nil {
		return tooldef.Result{}, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return tooldef.Result{}, errors.New("personal_kanban: edit requires an id")
	}
	if in.Title == nil && in.Notes == nil {
		return tooldef.Result{}, errors.New("personal_kanban: edit changes nothing (pass title and/or notes)")
	}
	edited, err := kanban.Edit(db, id, kanban.EditInput{
		Title: in.Title,
		Notes: in.Notes,
	})
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("personal_kanban: %w", err)
	}
	return jsonResult(map[string]any{
		"edited": true,
		"card":   cardToJSON(edited),
	}, "edit "+truncate(edited.Title, 60))
}

func runDelete(dbFn func() *sql.DB, allowed func() bool, in kanbanInput) (tooldef.Result, error) {
	db, err := requireWrite(dbFn, allowed)
	if err != nil {
		return tooldef.Result{}, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return tooldef.Result{}, errors.New("personal_kanban: delete requires an id")
	}
	if err := kanban.Delete(db, id); err != nil {
		return tooldef.Result{}, fmt.Errorf("personal_kanban: %w", err)
	}
	return jsonResult(map[string]any{
		"deleted": true,
		"id":      id,
	}, "delete "+truncate(id, 40))
}

// ---------- UI detail ----------

func kanbanDetail(input json.RawMessage) string {
	var in kanbanInput
	_ = json.Unmarshal(input, &in)
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	switch strings.ToLower(strings.TrimSpace(in.Action)) {
	case "list":
		return "list board"
	case "add":
		return "add " + truncate(str(in.Title), 60)
	case "move":
		return fmt.Sprintf("move %s", truncate(in.ID, 40))
	case "edit":
		return fmt.Sprintf("edit %s", truncate(in.ID, 40))
	case "delete":
		return fmt.Sprintf("delete %s", truncate(in.ID, 40))
	default:
		return "personal_kanban"
	}
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
