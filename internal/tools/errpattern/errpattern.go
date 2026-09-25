package errpattern

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
	errPatternDefaultLimit = 100
)

var errPatternDescription = `Detect common Go error handling anti-patterns.

Scans for ignored errors, panic usage, log.Fatal in libraries,
and other error handling issues that reduce code quality.`

// ErrPatternTool returns the error pattern matcher tool definition + handler.
func ErrPatternTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "errpattern",
			Description: errPatternDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "Directory or package path. Example: ./internal/tools",
					},
					"glob": llm.Object{
						"type":        "string",
						"description": "File pattern filter. Example: *.go",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum results to return. Example: 50 (default: %d)", errPatternDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in errPatternInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("errpattern %s", p)
		},
		Run: runErrPattern,
	}
}

type errPatternInput struct {
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type errPatternHit struct {
	file    string
	line    int
	pattern string
	message string
}

func runErrPattern(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in errPatternInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse errpattern arguments: %w", err)
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
		return tooldef.Result{}, fmt.Errorf("path not found: %s", searchPath)
	}

	glob := strings.TrimSpace(in.Glob)
	if glob == "" {
		glob = "*.go"
	}

	limit := in.Limit
	if limit <= 0 {
		limit = errPatternDefaultLimit
	}

	hits, err := scanErrorPatterns(searchPath, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(hits) == 0 {
		return tooldef.Result{Content: "No error handling issues detected", Detail: "0 hits", Output: "No error handling issues detected"}, nil
	}

	content := renderErrPatternResults(ctx, hits)
	detail := fmt.Sprintf("%d error pattern issues", len(hits))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func scanErrorPatterns(root, glob string, limit int) ([]errPatternHit, error) {
	goFiles, err := collectGoFiles(root, glob)
	if err != nil {
		return nil, err
	}

	var hits []errPatternHit
	for _, file := range goFiles {
		fileHits, err := scanFileForErrorPatterns(file)
		if err != nil {
			continue
		}
		hits = append(hits, fileHits...)
		if len(hits) >= limit {
			break
		}
	}

	if len(hits) > limit {
		hits = hits[:limit]
	}

	return hits, nil
}

func collectGoFiles(root, glob string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info == nil || info.IsDir() {
			if info != nil && info.IsDir() {
				if shouldSkipDir(filepath.Base(path)) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		matched, err := filepath.Match(glob, filepath.Base(path))
		if err != nil || !matched {
			return nil
		}

		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

var errPatterns = []struct {
	pattern *regexp.Regexp
	message string
}{
	{regexp.MustCompile(`(?i)_\s*=\s*err`), "error is ignored; handle or explicitly discard with blank identifier"},
	{regexp.MustCompile(`(?i)err\s*:=\s*\w+\([^)]*\)\s*;`), "error is ignored; handle or explicitly discard with blank identifier"},
	{regexp.MustCompile(`(?i)if\s+err\s*:=\s*\w+\([^)]*\);\s*err\s*!=\s*nil\s*\{\s*return\s*err\s*\}`), "error is returned without context; consider wrapping with additional context"},
	{regexp.MustCompile(`(?i)panic\(`), "panic usage; consider returning error instead"},
	{regexp.MustCompile(`(?i)log\.Fatal`), "log.Fatal in library code; consider returning error instead"},
}

func scanFileForErrorPatterns(path string) ([]errPatternHit, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(b)
	lines := strings.Split(text, "\n")

	var hits []errPatternHit
	for _, pattern := range errPatterns {
		for i, line := range lines {
			if pattern.pattern.MatchString(line) {
				hits = append(hits, errPatternHit{
					file:    path,
					line:    i + 1,
					pattern: pattern.pattern.String(),
					message: pattern.message,
				})
			}
		}
	}

	return hits, nil
}

func renderErrPatternResults(ctx context.Context, hits []errPatternHit) string {
	var sb strings.Builder
	for _, h := range hits {
		rel := tooldef.RelToCwd(ctx, h.file)
		sb.WriteString(fmt.Sprintf("%s:%d\t%s\n", rel, h.line, h.message))
	}
	return sb.String()
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".tox", "dist", "build":
		return true
	}
	return false
}
