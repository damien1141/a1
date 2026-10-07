package writetool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/util"
)

var editDescription = `Replace exact text in a file.
- Provide the EXACT text to find in 'old_str' and the replacement in 'new_str'.
- 'old_str' MUST be unique in the file. If it appears multiple times, include more surrounding lines to make it unique.
- 'old_str' MUST match the file content exactly, including whitespace and indentation.
- If 'new_str' is empty, the text will be deleted.
- Use 'write' tool to create new files or overwrite the entire file.`

// EditTool returns the str_replace tool definition + handler.
func EditTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "edit",
			Description: editDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "File path to modify.",
					},
					"old_str": llm.Object{
						"type":        "string",
						"description": "Exact text to find. Must be unique in the file.",
					},
					"new_str": llm.Object{
						"type":        "string",
						"description": "Text to replace 'old_str' with.",
					},
				},
				Required: []string{"path", "old_str", "new_str"},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in editInput
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.Path)
		},
		Run: runEdit,
	}
}

type editInput struct {
	Path   string `json:"path"`
	OldStr string `json:"old_str"`
	NewStr string `json:"new_str"`
}

func runEdit(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in editInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse edit arguments: %w", err)
	}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		var m map[string]any
		if err := json.Unmarshal(input, &m); err == nil {
			if p, ok := m["file_path"].(string); ok {
				path = strings.TrimSpace(p)
			}
		}
	}
	if path == "" {
		return tooldef.Result{}, errors.New("edit requires a non-empty path")
	}
	path, err := tooldef.ResolveToCwd(ctx, path)
	if err != nil {
		return tooldef.Result{}, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to read file %s: %w", path, err)
	}
	fileStr := util.NormalizeLF(string(content))
	oldStr := util.NormalizeLF(in.OldStr)
	newStr := util.NormalizeLF(in.NewStr)

	if oldStr == "" {
		return tooldef.Result{}, errors.New("old_str is required and cannot be empty")
	}

	count := strings.Count(fileStr, oldStr)
	if count == 0 {
		return tooldef.Result{}, fmt.Errorf("old_str not found in %s. Ensure it matches the file exactly, including whitespace and indentation", path)
	}
	if count > 1 {
		return tooldef.Result{}, fmt.Errorf("old_str appears %d times in %s. Include more surrounding context to make it unique", count, path)
	}

	newContent := strings.Replace(fileStr, oldStr, newStr, 1)

	if err := os.WriteFile(path, []byte(newContent), 0o644); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to write file %s: %w", path, err)
	}

	display := tooldef.RelToCwd(ctx, path)
	newTag := util.ComputeFileHash(newContent)
	diff := util.GenerateFileDiff(path, fileStr, newContent, 3)

	body := util.FormatFileHeader(display, newTag) + "\n\n" + diff

	return tooldef.Result{
		Content: body,
		Detail:  fmt.Sprintf("edited %s", display),
		Output:  body,
	}, nil
}
