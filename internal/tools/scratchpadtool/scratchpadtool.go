package scratchpadtool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	scratchpadFile = "notes.json"
)

var scratchpadDescription = `Agent scratchpad for notes and observations.

Stores freeform notes under ~/.a1/scratchpad/. Supports create, list, read,
update, and delete. Fully local; no external API calls.`

// ScratchpadTool returns the scratchpad tool definition + handler.
func ScratchpadTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "scratchpad",
			Description: scratchpadDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"action": llm.Object{
						"type":        "string",
						"description": "Action: create, list, read, update, delete. Example: create",
					},
					"id": llm.Object{
						"type":        "string",
						"description": "Note ID for read/update/delete. Example: note-1",
					},
					"title": llm.Object{
						"type":        "string",
						"description": "Note title. Example: Design decision",
					},
					"content": llm.Object{
						"type":        "string",
						"description": "Note body text.",
					},
					"tags": llm.Object{
						"type":        "string",
						"description": "Comma-separated tags. Example: decision, auth",
					},
					"query": llm.Object{
						"type":        "string",
						"description": "Substring search across title and content.",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": "Max notes to return for list/search. Example: 20",
					},
				},
				Required: []string{"action"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in scratchpadInput
			_ = json.Unmarshal(input, &in)
			a := strings.TrimSpace(in.Action)
			if a == "" {
				return "scratchpad"
			}
			return fmt.Sprintf("scratchpad %s", a)
		},
		Run: runScratchpad,
	}
}

type scratchpadInput struct {
	Action  string `json:"action"`
	ID      string `json:"id,omitempty"`
	Title   string `json:"title,omitempty"`
	Content string `json:"content,omitempty"`
	Tags    string `json:"tags,omitempty"`
	Query   string `json:"query,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type scratchpadNote struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func runScratchpad(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in scratchpadInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse scratchpad arguments: %w", err)
	}

	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" {
		return tooldef.Result{}, fmt.Errorf("action is required: create, list, read, update, delete")
	}

	dir, err := scratchpadDir()
	if err != nil {
		return tooldef.Result{}, err
	}

	notes, err := loadNotes(dir)
	if err != nil {
		return tooldef.Result{}, err
	}

	switch action {
	case "create":
		return createNote(ctx, dir, notes, in)
	case "list", "search":
		return listNotes(ctx, notes, in)
	case "read":
		return readNote(ctx, notes, in)
	case "update":
		return updateNote(ctx, dir, notes, in)
	case "delete":
		return deleteNote(ctx, dir, notes, in)
	default:
		return tooldef.Result{}, fmt.Errorf("unknown action %q: use create, list, read, update, delete", action)
	}
}

func scratchpadDir() (string, error) {
	proj := project.GetDefaultProject()
	if proj == nil {
		return "", fmt.Errorf("scratchpad: project not available")
	}
	dir := proj.Global().ScratchpadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("scratchpad: create dir: %w", err)
	}
	return dir, nil
}

func notesPath(dir string) string { return filepath.Join(dir, scratchpadFile) }

func loadNotes(dir string) ([]scratchpadNote, error) {
	p := notesPath(dir)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var notes []scratchpadNote
	if err := json.Unmarshal(b, &notes); err != nil {
		return nil, err
	}
	return notes, nil
}

func saveNotes(dir string, notes []scratchpadNote) error {
	b, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(notesPath(dir), b, 0o644)
}

func createNote(ctx context.Context, dir string, notes []scratchpadNote, in scratchpadInput) (tooldef.Result, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return tooldef.Result{}, fmt.Errorf("scratchpad: title is required to create a note")
	}
	content := strings.TrimSpace(in.Content)
	if content == "" {
		return tooldef.Result{}, fmt.Errorf("scratchpad: content is required to create a note")
	}
	now := time.Now()
	note := scratchpadNote{
		ID:        fmt.Sprintf("note-%d", now.UnixNano()),
		Title:     title,
		Content:   content,
		Tags:      parseTags(in.Tags),
		CreatedAt: now,
		UpdatedAt: now,
	}
	notes = append(notes, note)
	if err := saveNotes(dir, notes); err != nil {
		return tooldef.Result{}, err
	}
	contentOut := renderNote(note)
	return tooldef.Result{Content: contentOut, Detail: note.ID, Output: contentOut}, nil
}

func listNotes(ctx context.Context, notes []scratchpadNote, in scratchpadInput) (tooldef.Result, error) {
	query := strings.ToLower(strings.TrimSpace(in.Query))
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	out := make([]scratchpadNote, 0, limit)
	for _, n := range notes {
		if query != "" {
			q := query
			if !containsString(strings.ToLower(n.Title), q) && !containsString(strings.ToLower(n.Content), q) {
				continue
			}
		}
		out = append(out, n)
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return tooldef.Result{Content: "No notes found", Detail: "0 notes", Output: "No notes found"}, nil
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Scratchpad notes (%d shown)\n", len(out)))
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	for i, n := range out {
		sb.WriteString(fmt.Sprintf("%d. @scratchpad %s — %s\n", i+1, n.ID, n.Title))
		if len(n.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("   tags: %s\n", strings.Join(n.Tags, ", ")))
		}
		sb.WriteString(fmt.Sprintf("   updated: %s\n", n.UpdatedAt.Format("2006-01-02 15:04:05")))
		sb.WriteString("\n")
	}
	return tooldef.Result{Content: sb.String(), Detail: fmt.Sprintf("%d notes", len(out)), Output: sb.String()}, nil
}

func readNote(ctx context.Context, notes []scratchpadNote, in scratchpadInput) (tooldef.Result, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return tooldef.Result{}, fmt.Errorf("scratchpad: id is required to read a note")
	}
	for _, n := range notes {
		if n.ID == id {
			return tooldef.Result{Content: renderNote(n), Detail: n.ID, Output: renderNote(n)}, nil
		}
	}
	return tooldef.Result{}, fmt.Errorf("scratchpad: note %q not found", id)
}

func updateNote(ctx context.Context, dir string, notes []scratchpadNote, in scratchpadInput) (tooldef.Result, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return tooldef.Result{}, fmt.Errorf("scratchpad: id is required to update a note")
	}
	for i, n := range notes {
		if n.ID == id {
			title := strings.TrimSpace(in.Title)
			if title != "" {
				n.Title = title
			}
			content := strings.TrimSpace(in.Content)
			if content != "" {
				n.Content = content
			}
			if in.Tags != "" {
				n.Tags = parseTags(in.Tags)
			}
			n.UpdatedAt = time.Now()
			notes[i] = n
			if err := saveNotes(dir, notes); err != nil {
				return tooldef.Result{}, err
			}
			return tooldef.Result{Content: renderNote(n), Detail: n.ID, Output: renderNote(n)}, nil
		}
	}
	return tooldef.Result{}, fmt.Errorf("scratchpad: note %q not found", id)
}

func deleteNote(ctx context.Context, dir string, notes []scratchpadNote, in scratchpadInput) (tooldef.Result, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return tooldef.Result{}, fmt.Errorf("scratchpad: id is required to delete a note")
	}
	for i, n := range notes {
		if n.ID == id {
			notes = append(notes[:i], notes[i+1:]...)
			if err := saveNotes(dir, notes); err != nil {
				return tooldef.Result{}, err
			}
			return tooldef.Result{Content: "Deleted note: " + id, Detail: id, Output: "Deleted note: " + id}, nil
		}
	}
	return tooldef.Result{}, fmt.Errorf("scratchpad: note %q not found", id)
}

func renderNote(n scratchpadNote) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Note: %s\n", n.Title))
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("ID: %s\n", n.ID))
	sb.WriteString(fmt.Sprintf("Created: %s\n", n.CreatedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("Updated: %s\n", n.UpdatedAt.Format("2006-01-02 15:04:05")))
	if len(n.Tags) > 0 {
		sb.WriteString(fmt.Sprintf("Tags: %s\n", strings.Join(n.Tags, ", ")))
	}
	sb.WriteString("\n")
	sb.WriteString(n.Content)
	sb.WriteString("\n")
	return sb.String()
}

func parseTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}
