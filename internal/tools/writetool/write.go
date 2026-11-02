package writetool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/util"
)

var writeDescription = `Write or edit a file.
- write (default): create or overwrite the whole file (path + content),
  parent directories created. Overwrites existing content; prefer edit
  for changing part of a file.
- edit: replace one exact string (path + old_str + new_str). old_str
  must match exactly, whitespace included, and be unique in the file;
  add surrounding lines to disambiguate. Empty new_str deletes.`

// WriteTool returns the write tool definition + handler.
// Dispatches to runWrite (mode=write, default) or runEdit (mode=edit) — the
// edit handler is in edit.go in this package. Both modes share the same
// tool name so the LLM only has one write/edit surface to learn; the mode
// field picks the behavior.
func WriteTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "write",
			Description: writeDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"mode": llm.Object{
						"type":        "string",
						"description": "write (default): whole file. edit: str_replace.",
						"enum":        []string{"write", "edit"},
					},
					"path": llm.Object{
						"type":        "string",
						"description": "File path. Example: src/new.go",
					},
					"content": llm.Object{
						"type":        "string",
						"description": "Whole file content (mode=write).",
					},
					"old_str": llm.Object{
						"type":        "string",
						"description": "Text to replace (mode=edit); must be unique in the file.",
					},
					"new_str": llm.Object{
						"type":        "string",
						"description": "Replacement (mode=edit); empty deletes.",
					},
				},
				Required: []string{"path"},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in writeInput
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.Path)
		},
		Run: runWriteDispatch,
	}
}

// writeInput is the union of write + edit params so json.Unmarshal captures
// whichever set the caller actually sent. Mode picks which handler runs.
type writeInput struct {
	Mode    string `json:"mode,omitempty"`
	Path    string `json:"path"`
	Content string `json:"content"`
	OldStr  string `json:"old_str"`
	NewStr  string `json:"new_str"`
}

// runWriteDispatch reads the mode field and routes to the matching handler.
// Default mode is "write" for backward compatibility with old callers that
// only sent path + content; an explicit "edit" routes to the edit handler.
// An empty mode is treated as "write" so the legacy schema (no mode field)
// keeps working without a change.
func runWriteDispatch(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in writeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse write arguments: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "write"
	}
	switch mode {
	case "write":
		return runWrite(ctx, input)
	case "edit":
		return runEdit(ctx, input)
	default:
		return tooldef.Result{}, fmt.Errorf("unknown write mode %q: use write or edit", in.Mode)
	}
}

func runWrite(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in writeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse write arguments: %w", err)
	}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return tooldef.Result{}, errors.New("path is required")
	}
	path, err := tooldef.ResolveToCwd(ctx, path)
	if err != nil {
		return tooldef.Result{}, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to create parent directories: %w", err)
	}

	oldContent, readErr := os.ReadFile(path)
	fileExisted := readErr == nil

	//nolint:gosec // G306: source files should stay world-readable
	if err := os.WriteFile(path, []byte(in.Content), 0o644); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to write file %s: %w", path, err)
	}

	display := tooldef.RelToCwd(ctx, path)
	var body string
	if fileExisted {
		oldStr := util.NormalizeLF(string(oldContent))
		newStr := util.NormalizeLF(in.Content)
		switch {
		case oldStr == newStr:
			body = fmt.Sprintf("wrote %d bytes to %s (no changes)", len(in.Content), display)
		case strings.TrimSpace(oldStr) == "":
			// Nothing to diff against: the whole file is the addition.
			body = fmt.Sprintf("Wrote %d lines to %s.\n\n%s",
				countLines(newStr), display, prefixedLines(newStr, "+"))
		default:
			body = fmt.Sprintf("Updated %s.\n\n%s", display,
				util.GenerateFileDiff(path, oldStr, newStr, 3))
		}
	} else {
		body = fmt.Sprintf("Created %s (%d lines).\n\n%s",
			display, countLines(util.NormalizeLF(in.Content)), prefixedLines(util.NormalizeLF(in.Content), "+"))
	}

	return tooldef.Result{Content: body, Detail: display, Output: body}, nil
}

// countLines counts the newline-terminated lines in text.
func countLines(text string) int {
	if text == "" {
		return 0
	}
	n := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		n++
	}
	return n
}

// prefixedLines returns every line of text with prefix prepended, so content
// that has no old version to diff against still reads as an addition.
func prefixedLines(text, prefix string) string {
	var sb strings.Builder
	for ln := range strings.SplitSeq(text, "\n") {
		if ln == "" {
			continue
		}
		sb.WriteString(prefix)
		sb.WriteString(ln)
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}
