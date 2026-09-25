package todotool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	todoDefaultLimit = 100
)

var todoDescription = `Find TODO, FIXME, HACK, XXX, OPTIMIZE, and BUG markers in code.

Returns file headers plus line anchors. Use glob to limit file types.
Results are capped; increase limit or refine the pattern if truncated.`

// TodoTool returns the todo/fixme collector tool definition + handler.
func TodoTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "todo",
			Description: todoDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "Directory or file to search. Example: ./src",
					},
					"glob": llm.Object{
						"type":        "string",
						"description": "File pattern filter. Example: *.go",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum matches to return. Example: 50 (default: %d)", todoDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in todoInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("todo in %s", p)
		},
		Run: runTodo,
	}
}

type todoInput struct {
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

var todoPattern = regexp.MustCompile(`(?i)\b(?:TODO|FIXME|HACK|XXX|OPTIMIZE|BUG)\b`)

func runTodo(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in todoInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse todo arguments: %w", err)
	}

	searchRel := strings.TrimSpace(in.Path)
	if searchRel == "" {
		searchRel = "."
	}
	searchPath, err := tooldef.ResolveToCwd(ctx, searchRel)
	if err != nil {
		return tooldef.Result{}, err
	}

	if _, err := os.Stat(searchPath); err != nil {
		return tooldef.Result{}, fmt.Errorf("path not found: %s. Check the path and try again", searchPath)
	}

	gitDir := filepathJoin(searchPath, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return tooldef.Result{}, fmt.Errorf("not a git repository (or any parent up to mount point): %s", searchPath)
	}

	limit := in.Limit
	if limit <= 0 {
		limit = todoDefaultLimit
	}

	glob := strings.TrimSpace(in.Glob)

	matches, err := searchTodos(searchPath, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(matches) == 0 {
		return tooldef.Result{Content: "No TODO/FIXME/HACK/XXX/OPTIMIZE/BUG markers found", Detail: "0 markers", Output: "No TODO/FIXME/HACK/XXX/OPTIMIZE/BUG markers found"}, nil
	}

	content := strings.Join(matches, "\n")
	detail := fmt.Sprintf("%d markers", len(matches))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func searchTodos(root, glob string, limit int) ([]string, error) {
	var matches []string
	markerCount := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info == nil || info.IsDir() {
			// Skip .git and common non-source directories.
			if info != nil && info.IsDir() {
				base := filepath.Base(path)
				if base == ".git" || base == "node_modules" || base == "vendor" || base == ".venv" || base == "venv" {
					return filepath.SkipDir
				}
			}
			return nil
		}

		if glob != "" {
			matched, err := filepath.Match(glob, filepath.Base(path))
			if err != nil || !matched {
				return nil
			}
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := strings.ReplaceAll(string(b), "\r\n", "\n")
		lines := strings.Split(text, "\n")

		fileTag := computeFileHash(text)
		rel := path
		if cwd, err := os.Getwd(); err == nil {
			if r, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
		}
		if fileTag != "" {
			matches = append(matches, formatFileHeader(rel, fileTag))
		}

		for i, line := range lines {
			if todoPattern.MatchString(line) {
				markerCount++
				if markerCount > limit {
					matches = append(matches, fmt.Sprintf("... (%d markers limit reached; use limit=%d for more)", limit, limit*2))
					return filepath.SkipDir
				}
				lineText := strings.TrimRight(line, "\r")
				h := computeLineHash(lineText)
				ref := fmt.Sprintf("%d#%s", i+1, h)
				matches = append(matches, fmt.Sprintf("%s:>>%s|%s", rel, ref, lineText))
			}
		}

		return nil
	})

	return matches, err
}

func filepathJoin(elem ...string) string {
	parts := make([]string, len(elem))
	for i, e := range elem {
		parts[i] = e
	}
	return filepath.Join(parts...)
}

func computeFileHash(text string) string {
	// Minimal placeholder; replace with util.ComputeFileHash if needed.
	return ""
}

func computeLineHash(line string) string {
	// Minimal placeholder; replace with util.ComputeLineHash if needed.
	return ""
}

func formatFileHeader(path, tag string) string {
	return fmt.Sprintf("@file %s#%s", path, tag)
}
