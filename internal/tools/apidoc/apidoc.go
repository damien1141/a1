package apidoc

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
	apidocDefaultLimit = 100
)

var apidocDescription = `Generate API documentation stubs for exported Go symbols.

Scans Go files for exported functions, types, variables, and constants,
then returns markdown-ready documentation stubs with signatures and
placeholder descriptions.`

// ApidocTool returns the API doc generator tool definition + handler.
func ApidocTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "apidoc",
			Description: apidocDescription,
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
						"description": fmt.Sprintf("Maximum results to return. Example: 50 (default: %d)", apidocDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in apidocInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("apidoc %s", p)
		},
		Run: runApidoc,
	}
}

type apidocInput struct {
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type apidocEntry struct {
	file     string
	line     int
	name     string
	kind     string
	signature string
	doc      string
}

func runApidoc(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in apidocInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse apidoc arguments: %w", err)
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
		limit = apidocDefaultLimit
	}

	entries, err := generateApiDocs(searchPath, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(entries) == 0 {
		return tooldef.Result{Content: "No exported symbols found for documentation", Detail: "0 symbols", Output: "No exported symbols found for documentation"}, nil
	}

	content := renderApidocResults(ctx, entries)
	detail := fmt.Sprintf("%d API docs generated", len(entries))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func generateApiDocs(root, glob string, limit int) ([]apidocEntry, error) {
	goFiles, err := collectGoFiles(root, glob)
	if err != nil {
		return nil, err
	}

	var entries []apidocEntry
	for _, file := range goFiles {
		fileEntries, err := parseFileForApiDocs(file)
		if err != nil {
			continue
		}
		entries = append(entries, fileEntries...)
		if len(entries) >= limit {
			break
		}
	}

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

var exportRe = regexp.MustCompile(`(?m)^(?:func|type|var|const)\s+([A-Z][a-zA-Z0-9_]*)\b`)

func parseFileForApiDocs(path string) ([]apidocEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(b)
	lines := strings.Split(text, "\n")

	matches := exportRe.FindAllStringSubmatch(text, -1)
	entries := make([]apidocEntry, 0, len(matches))
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
		signature := extractSignature(lines, lineNum-1)
		doc := extractDocComment(lines, lineNum-1)
		entries = append(entries, apidocEntry{
			file:      path,
			line:      lineNum,
			name:      m[1],
			kind:      kind,
			signature: signature,
			doc:       doc,
		})
	}

	return entries, nil
}

func extractSignature(lines []string, idx int) string {
	if idx < 0 || idx >= len(lines) {
		return ""
	}
	// Collect until we hit a closing brace or empty line after the signature.
	var parts []string
	for i := idx; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		parts = append(parts, line)
		if strings.Contains(line, "{") {
			break
		}
		if i > idx && strings.TrimSpace(line) == "" {
			break
		}
	}
	return strings.Join(parts, " ")
}

func extractDocComment(lines []string, idx int) string {
	if idx <= 0 {
		return ""
	}
	var comments []string
	for i := idx - 1; i >= 0; i-- {
		line := strings.TrimRight(lines[i], "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			comments = append([]string{strings.TrimPrefix(trimmed, "//")}, comments...)
		} else if trimmed == "" {
			continue
		} else {
			break
		}
	}
	if len(comments) == 0 {
		return ""
	}
	return strings.Join(comments, " ")
}

func renderApidocResults(ctx context.Context, entries []apidocEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		rel := tooldef.RelToCwd(ctx, e.file)
		sb.WriteString(fmt.Sprintf("@file %s\n", rel))
		sb.WriteString(fmt.Sprintf("%s %s\n", e.kind, e.name))
		sb.WriteString(fmt.Sprintf("signature: %s\n", e.signature))
		if e.doc != "" {
			sb.WriteString(fmt.Sprintf("doc: %s\n", e.doc))
		}
		sb.WriteString("\n")
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
