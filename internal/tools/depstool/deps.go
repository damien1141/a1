package depstool

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
	depsDefaultLimit = 100
)

var depsDescription = `Scan Go files for external import paths and return a dependency map.

Lists every external module imported across the target files, grouped by
import path. Useful for understanding cross-repo coupling before a change.`

// DepsTool returns the cross-repo dependency tracker tool definition + handler.
func DepsTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "deps",
			Description: depsDescription,
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
						"description": fmt.Sprintf("Maximum results to return. Example: 50 (default: %d)", depsDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in depsInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("deps %s", p)
		},
		Run: runDeps,
	}
}

type depsInput struct {
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type depEntry struct {
	path    string
	files   []string
	count   int
}

func runDeps(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in depsInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse deps arguments: %w", err)
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
		limit = depsDefaultLimit
	}

	entries, err := scanDeps(searchPath, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(entries) == 0 {
		return tooldef.Result{Content: "No external dependencies found", Detail: "0 deps", Output: "No external dependencies found"}, nil
	}

	content := renderDepsResults(ctx, entries)
	detail := fmt.Sprintf("%d dependencies", len(entries))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func scanDeps(root, glob string, limit int) ([]depEntry, error) {
	goFiles, err := collectGoFiles(root, glob)
	if err != nil {
		return nil, err
	}

	depMap := make(map[string]*depEntry)
	for _, file := range goFiles {
		imports, err := parseImports(file)
		if err != nil {
			continue
		}
		for _, imp := range imports {
			if !isExternalImport(imp) {
				continue
			}
			entry, ok := depMap[imp]
			if !ok {
				entry = &depEntry{path: imp}
				depMap[imp] = entry
			}
			entry.count++
			entry.files = append(entry.files, file)
		}
	}

	var entries []depEntry
	for _, entry := range depMap {
		entries = append(entries, *entry)
	}

	sortDeps(entries)

	if len(entries) > limit {
		entries = entries[:limit]
	}

	return entries, nil
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

var importRe = regexp.MustCompile(`(?m)^import\s+(?:"([^"]+)"|` + "`" + `([^` + "`" + `]+)` + "`" + `)`)

func parseImports(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(b)
	var imports []string
	for _, m := range importRe.FindAllStringSubmatch(text, -1) {
		imp := ""
		if m[1] != "" {
			imp = m[1]
		} else {
			imp = m[2]
		}
		if imp != "" {
			imports = append(imports, imp)
		}
	}
	return imports, nil
}

func isExternalImport(imp string) bool {
	return !strings.HasPrefix(imp, ".")
	}

func sortDeps(entries []depEntry) {
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].count > entries[i].count {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
}

func renderDepsResults(ctx context.Context, entries []depEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("%d\t%s\n", e.count, e.path))
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
