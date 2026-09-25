package deadcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	deadcodeDefaultLimit = 100
)

var deadcodeDescription = `Detect potentially dead exported Go symbols.

Scans Go files for exported functions, types, variables, and constants,
then cross-references them against the codebase to find symbols that
appear unused outside their defining file.`

// DeadcodeTool returns the dead code detector tool definition + handler.
func DeadcodeTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "deadcode",
			Description: deadcodeDescription,
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
						"description": fmt.Sprintf("Maximum results to return. Example: 50 (default: %d)", deadcodeDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in deadcodeInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("deadcode %s", p)
		},
		Run: runDeadcode,
	}
}

type deadcodeInput struct {
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type deadcodeSymbol struct {
	file    string
	name    string
	kind    string
	line    int
	refs    int
}

func runDeadcode(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in deadcodeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse deadcode arguments: %w", err)
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
		limit = deadcodeDefaultLimit
	}

	symbols, err := findDeadCode(searchPath, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(symbols) == 0 {
		return tooldef.Result{Content: "No dead code detected", Detail: "0 dead symbols", Output: "No dead code detected"}, nil
	}

	content := renderDeadcodeResults(ctx, symbols)
	detail := fmt.Sprintf("%d potentially dead symbols", len(symbols))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func findDeadCode(root, glob string, limit int) ([]deadcodeSymbol, error) {
	goFiles, err := collectGoFiles(root, glob)
	if err != nil {
		return nil, err
	}

	var candidates []deadcodeSymbol
	for _, file := range goFiles {
		fileSymbols, err := parseExports(file)
		if err != nil {
			continue
		}
		candidates = append(candidates, fileSymbols...)
	}

	cwd, _ := os.Getwd()
	var dead []deadcodeSymbol
	for i := range candidates {
		refs, err := countReferences(root, candidates[i].file, candidates[i].name, cwd)
		if err != nil {
			refs = 0
		}
		candidates[i].refs = refs
		if refs == 0 {
			dead = append(dead, candidates[i])
		}
	}

	if len(dead) > limit {
		dead = dead[:limit]
	}

	return dead, nil
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

func parseExports(path string) ([]deadcodeSymbol, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	lines := strings.Split(text, "\n")

	matches := exportRe.FindAllStringSubmatch(text, -1)
	symbols := make([]deadcodeSymbol, 0, len(matches))
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
		symbols = append(symbols, deadcodeSymbol{
			file: path,
			name: m[1],
			kind: kind,
			line: lineNum,
		})
	}

	return symbols, nil
}

func countReferences(root, defFile, symbolName string, cwd string) (int, error) {
	args := []string{
		"-r",
		"-n",
		"--include=*.go",
		symbolName,
		root,
	}
	cmd := exec.Command("grep", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return 0, nil
		}
		return 0, fmt.Errorf("grep: %s", strings.TrimSpace(string(out)))
	}

	lines := strings.Split(string(out), "\n")
	count := 0
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 {
			continue
		}
		file := parts[0]
		if file == defFile {
			continue
		}
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		count++
	}

	return count, nil
}

func renderDeadcodeResults(ctx context.Context, symbols []deadcodeSymbol) string {
	var sb strings.Builder
	for _, s := range symbols {
		rel := tooldef.RelToCwd(ctx, s.file)
		sb.WriteString(fmt.Sprintf("%s:%d\t%s\t%s\n", rel, s.line, s.kind, s.name))
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
