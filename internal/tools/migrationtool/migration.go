package migrationtool

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
	migrationDefaultLimit = 100
)

var migrationDescription = `Detect common Go migration targets and suggest upgrade steps.

Scans for deprecated APIs, old patterns, and version-specific code, then
returns a prioritized migration plan so the agent can modernize safely.`

// MigrationTool returns the migration assistant tool definition + handler.
func MigrationTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "migrate",
			Description: migrationDescription,
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
						"description": fmt.Sprintf("Maximum results to return. Example: 50 (default: %d)", migrationDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in migrationInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("migrate %s", p)
		},
		Run: runMigration,
	}
}

type migrationInput struct {
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type migrationHit struct {
	file    string
	line    int
	pattern string
	message string
}

func runMigration(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in migrationInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse migration arguments: %w", err)
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
		limit = migrationDefaultLimit
	}

	hits, err := scanMigrationTargets(searchPath, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(hits) == 0 {
		return tooldef.Result{Content: "No migration targets detected", Detail: "0 hits", Output: "No migration targets detected"}, nil
	}

	content := renderMigrationResults(ctx, hits)
	detail := fmt.Sprintf("%d migration targets", len(hits))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func scanMigrationTargets(root, glob string, limit int) ([]migrationHit, error) {
	goFiles, err := collectGoFiles(root, glob)
	if err != nil {
		return nil, err
	}

	var hits []migrationHit
	for _, file := range goFiles {
		fileHits, err := scanFileForMigrations(file)
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

var migrationPatterns = []struct {
	pattern *regexp.Regexp
	message string
}{
	{regexp.MustCompile(`(?i)ioutil\.ReadFile`), "ioutil.ReadFile is deprecated; use os.ReadFile (Go 1.16+)"},
	{regexp.MustCompile(`(?i)ioutil\.WriteFile`), "ioutil.WriteFile is deprecated; use os.WriteFile (Go 1.16+)"},
	{regexp.MustCompile(`(?i)ioutil\.ReadAll`), "ioutil.ReadAll is deprecated; use io.ReadAll (Go 1.16+)"},
	{regexp.MustCompile(`(?i)ioutil\.NopCloser`), "ioutil.NopCloser is deprecated; use io.NopCloser (Go 1.16+)"},
	{regexp.MustCompile(`(?i)golang\.org/x/net/context`), "golang.org/x/net/context is deprecated; use context package (Go 1.7+)"},
	{regexp.MustCompile(`(?i)context\.TODO\(\)`), "context.TODO() suggests incomplete error handling; consider context.Background() or context.WithCancel"},
	{regexp.MustCompile(`(?i)http\.Get\(|http\.Post\(|http\.Do\(`), "http.Client.Get/Post/Do without timeout; wrap in context.WithTimeout"},
	{regexp.MustCompile(`(?i)sql\.Rows\.Scan\([^)]*\*[^)]*\)`), "sql.Rows.Scan without error check; always check returned error"},
	{regexp.MustCompile(`(?i)defer\s+\w+\.Close\(\)\s*$`), "defer Close() without error check; consider checking Close() error"},
}

func scanFileForMigrations(path string) ([]migrationHit, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(b)
	lines := strings.Split(text, "\n")

	var hits []migrationHit
	for _, pattern := range migrationPatterns {
		for i, line := range lines {
			if pattern.pattern.MatchString(line) {
				hits = append(hits, migrationHit{
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

func renderMigrationResults(ctx context.Context, hits []migrationHit) string {
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
