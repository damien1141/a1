package nplusonetool

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
	nplusoneDefaultLimit = 100
)

var nplusoneDescription = `Detect potential N+1 query patterns in Go code.

Scans for database queries inside loops, which can cause performance issues
at scale. Returns the loop and query locations so the agent can refactor
to batch queries.`

// NplusoneTool returns the N+1 query detector tool definition + handler.
func NplusoneTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "nplusone",
			Description: nplusoneDescription,
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
						"description": fmt.Sprintf("Maximum results to return. Example: 50 (default: %d)", nplusoneDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in nplusoneInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("nplusone %s", p)
		},
		Run: runNplusone,
	}
}

type nplusoneInput struct {
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type nplusoneHit struct {
	file    string
	loopLine int
	queryLine int
	queryType string
	context string
}

func runNplusone(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in nplusoneInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse nplusone arguments: %w", err)
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
		limit = nplusoneDefaultLimit
	}

	hits, err := detectNPlusOne(searchPath, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(hits) == 0 {
		return tooldef.Result{Content: "No N+1 query patterns detected", Detail: "0 hits", Output: "No N+1 query patterns detected"}, nil
	}

	content := renderNplusoneResults(ctx, hits)
	detail := fmt.Sprintf("%d potential N+1 patterns", len(hits))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func detectNPlusOne(root, glob string, limit int) ([]nplusoneHit, error) {
	goFiles, err := collectGoFiles(root, glob)
	if err != nil {
		return nil, err
	}

	var hits []nplusoneHit
	for _, file := range goFiles {
		fileHits, err := scanFileForNPlusOne(file)
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

var queryRe = regexp.MustCompile(`(?i)(db\.Query|db\.QueryRow|db\.Exec|sql\.Query|sql\.QueryRow|gorm\.Find|gorm\.First|gorm\.Where|\.Raw\(|\.Scan\(|\.Select\(|\.Create\(|\.Save\(|\.Update\(|\.Delete\()`)

func scanFileForNPlusOne(path string) ([]nplusoneHit, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(b)
	lines := strings.Split(text, "\n")

	var hits []nplusoneHit
	for i, line := range lines {
		if strings.Contains(line, "for ") || strings.Contains(line, "for{") || strings.Contains(line, "for ") {
			loopStart := i
			loopEnd := findBlockEnd(lines, i)
			for j := loopStart + 1; j <= loopEnd && j < len(lines); j++ {
				queryLine := lines[j]
				if queryRe.MatchString(queryLine) {
					hits = append(hits, nplusoneHit{
						file:      path,
						loopLine:  loopStart + 1,
						queryLine: j + 1,
						queryType: extractQueryType(queryLine),
						context:   strings.TrimSpace(queryLine),
					})
					break
				}
			}
		}
	}

	return hits, nil
}

func findBlockEnd(lines []string, start int) int {
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimLeft(lines[i], " \t")
		if trimmed != "" && !strings.HasPrefix(trimmed, "}") {
			continue
		}
		if strings.HasPrefix(trimmed, "}") {
			return i
		}
	}
	return len(lines) - 1
}

func extractQueryType(line string) string {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "query") {
		return "query"
	}
	if strings.Contains(lower, "exec") {
		return "exec"
	}
	if strings.Contains(lower, "find") || strings.Contains(lower, "first") || strings.Contains(lower, "where") {
		return "gorm"
	}
	if strings.Contains(lower, "raw") {
		return "raw"
	}
	return "unknown"
}

func renderNplusoneResults(ctx context.Context, hits []nplusoneHit) string {
	var sb strings.Builder
	for _, h := range hits {
		rel := tooldef.RelToCwd(ctx, h.file)
			sb.WriteString(fmt.Sprintf("%s:%d\t%d\t%s\t%s\n", rel, h.loopLine, h.queryLine, h.queryType, h.context))
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
