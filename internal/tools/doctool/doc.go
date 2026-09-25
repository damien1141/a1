package doctool

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
	docDefaultLimit = 100
)

var docDescription = `Compare Go exported symbols against documentation and flag drift.

Scans Go files for exported functions, types, variables, and constants, then
checks markdown and godoc comments for matching references. Returns symbols
that are undocumented, plus doc mentions of symbols that no longer exist.`

// DocTool returns the doc-to-code sync checker tool definition + handler.
func DocTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "docsync",
			Description: docDescription,
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
						"description": fmt.Sprintf("Maximum results to return. Example: 50 (default: %d)", docDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in docInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("docsync %s", p)
		},
		Run: runDocSync,
	}
}

type docInput struct {
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type docHit struct {
	file    string
	line    int
	symbol  string
	kind    string
	message string
}

func runDocSync(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in docInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse docsync arguments: %w", err)
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
		limit = docDefaultLimit
	}

	hits, err := checkDocSync(searchPath, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(hits) == 0 {
		return tooldef.Result{Content: "No documentation drift detected", Detail: "0 hits", Output: "No documentation drift detected"}, nil
	}

	content := renderDocSyncResults(ctx, hits)
	detail := fmt.Sprintf("%d documentation issues", len(hits))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func checkDocSync(root, glob string, limit int) ([]docHit, error) {
	goFiles, err := collectGoFiles(root, glob)
	if err != nil {
		return nil, err
	}

	exportSet := make(map[string]docHit)
	for _, file := range goFiles {
		symbols, err := parseExports(file)
		if err != nil {
			continue
		}
		for _, s := range symbols {
			exportSet[s.name] = docHit{
				file:   s.file,
				line:   s.line,
				symbol: s.name,
				kind:   s.kind,
			}
		}
	}

	docFiles, err := collectDocFiles(root)
	if err != nil {
		return nil, err
	}

	docMentions := make(map[string]bool)
	for _, docFile := range docFiles {
		mentions, err := parseDocMentions(docFile, exportSet)
		if err != nil {
			continue
		}
		for m := range mentions {
			docMentions[m] = true
		}
	}

	var hits []docHit
	for name, hit := range exportSet {
		if !docMentions[name] {
			hit.message = fmt.Sprintf("exported %s %s is not documented", hit.kind, hit.symbol)
			hits = append(hits, hit)
		}
	}

	for mention := range docMentions {
		if _, ok := exportSet[mention]; !ok {
			hits = append(hits, docHit{
				symbol:  mention,
				message: fmt.Sprintf("documentation mentions %s but symbol does not exist", mention),
			})
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

func collectDocFiles(root string) ([]string, error) {
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

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".md" || ext == ".txt" {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

var exportRe = regexp.MustCompile(`(?m)^(?:func|type|var|const)\s+([A-Z][a-zA-Z0-9_]*)\b`)

func parseExports(path string) ([]struct{ file, name, kind string; line int }, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(b)
	lines := strings.Split(text, "\n")

	matches := exportRe.FindAllStringSubmatch(text, -1)
	symbols := make([]struct{ file, name, kind string; line int }, 0, len(matches))
	for _, m := range matches {
		lineNum := 0
		for i, line := range lines {
			if strings.Contains(line, m[0]) {
				lineNum = i + 1
				break
			}
		}
		kind := ""
		switch {
		case strings.HasPrefix(m[0], "func"):
			kind = "func"
		case strings.HasPrefix(m[0], "type"):
			kind = "type"
		case strings.HasPrefix(m[0], "var"):
			kind = "var"
		case strings.HasPrefix(m[0], "const"):
			kind = "const"
		}
		symbols = append(symbols, struct{ file, name, kind string; line int }{
			file: path,
			name: m[1],
			kind: kind,
			line: lineNum,
		})
	}

	return symbols, nil
}

var docSymbolRe = regexp.MustCompile("`([A-Z][a-zA-Z0-9_]{2,})`")

func parseDocMentions(path string, exports map[string]docHit) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(b)

	mentions := make(map[string]bool)
	for _, m := range docSymbolRe.FindAllStringSubmatch(text, -1) {
		name := m[1]
		mentions[name] = true
	}

	return mentions, nil
}

func renderDocSyncResults(ctx context.Context, hits []docHit) string {
	var sb strings.Builder
	for _, h := range hits {
		rel := tooldef.RelToCwd(ctx, h.file)
		if rel == "" {
			rel = "(docs)"
		}
		sb.WriteString(fmt.Sprintf("%s:%d\t%s\t%s\n", rel, h.line, h.symbol, h.message))
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
