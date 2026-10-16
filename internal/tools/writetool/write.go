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

var writeDescription = `Write content to a file. Creates the file if it does not exist; overwrites the entire file if it does. Creates parent directories.`

// WriteTool returns the write tool definition + handler.
func WriteTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "write",
			Description: writeDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "File path to write (created or overwritten). Example: src/new.go",
					},
					"content": llm.Object{
						"type":        "string",
						"description": "Content to write to the file.",
					},
				},
				Required: []string{"path", "content"},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in writeInput
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.Path)
		},
		Run: runWrite,
	}
}

type writeInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
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
