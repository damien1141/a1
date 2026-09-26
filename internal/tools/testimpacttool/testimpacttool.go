package testimpacttool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/graph"
)

var testImpactDescription = `Find test files affected by a source file change.

Uses the dependency graph to find all files that import the changed source
file, then filters to test files only. Returns the list of test files that
should be run to verify the change.`

// TestImpactTool returns the test impact tool definition + handler.
func TestImpactTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "testimpact",
			Description: testImpactDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "Source file path that changed. Example: internal/auth/auth.go",
					},
					"root": llm.Object{
						"type":        "string",
						"description": "Project root for graph lookup. Example: .",
					},
					"rescan": llm.Object{
						"type":        "boolean",
						"description": "Rebuild the dependency graph before querying.",
					},
				},
				Required: []string{"path"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in testImpactInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				return "testimpact"
			}
			return fmt.Sprintf("testimpact %s", p)
		},
		Run: runTestImpact,
	}
}

type testImpactInput struct {
	Path   string `json:"path"`
	Root   string `json:"root,omitempty"`
	Rescan bool   `json:"rescan"`
}

func runTestImpact(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in testImpactInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse testimpact arguments: %w", err)
	}

	path := strings.TrimSpace(in.Path)
	if path == "" {
		return tooldef.Result{}, fmt.Errorf("path is required: provide the source file that changed")
	}

	root := strings.TrimSpace(in.Root)
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("resolve root: %w", err)
	}

	g, err := graph.OpenGraph(ctx, absRoot)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("build graph: %w", err)
	}

	// Resolve path relative to root.
	absPath := path
	if !filepath.IsAbs(path) {
		absPath, err = filepath.Abs(filepath.Join(root, path))
		if err != nil {
			return tooldef.Result{}, fmt.Errorf("resolve path: %w", err)
		}
	}
	relPath := strings.TrimPrefix(absPath, absRoot)
	relPath = strings.TrimPrefix(relPath, string(filepath.Separator))
	if relPath == "" {
		return tooldef.Result{}, fmt.Errorf("testimpact: path must be within project root")
	}

	// Find all files that import the changed source file.
	importedBy := g.ImportedBy(relPath)
	if len(importedBy) == 0 {
		// Try resolving the source file as an import path.
		resolver := graph.NewPathResolver(absRoot)
		if resolved := resolver.Resolve(relPath); resolved != "" {
			importedBy = g.ImportedBy(resolved)
		}
	}
	if len(importedBy) == 0 {
		// Try without extension as an import path.
		ext := filepath.Ext(relPath)
		if ext != "" {
			importedBy = g.ImportedBy(strings.TrimSuffix(relPath, ext))
		}
	}
	if len(importedBy) == 0 {
		return tooldef.Result{Content: "No files import " + relPath, Detail: "0 tests", Output: "No files import " + relPath}, nil
	}

	// Resolve import paths to actual file paths.
	resolver := graph.NewPathResolver(absRoot)
	resolvedFiles := make([]string, 0, len(importedBy))
	for _, imp := range importedBy {
		if r := resolver.Resolve(imp); r != "" {
			resolvedFiles = append(resolvedFiles, r)
		}
	}
	if len(resolvedFiles) == 0 {
		return tooldef.Result{Content: "No files import " + relPath, Detail: "0 tests", Output: "No files import " + relPath}, nil
	}

	// Filter to test files only.
	testFiles := filterTestFiles(resolvedFiles)
	if len(testFiles) == 0 {
		return tooldef.Result{Content: "No test files import " + relPath, Detail: "0 tests", Output: "No test files import " + relPath}, nil
	}

	content := renderTestImpactResults(relPath, testFiles)
	detail := fmt.Sprintf("%d test files affected by %s", len(testFiles), relPath)
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func filterTestFiles(files []string) []string {
	var testFiles []string
	for _, f := range files {
		name := filepath.Base(f)
		ext := strings.ToLower(filepath.Ext(f))
		switch ext {
		case ".go":
			if strings.HasSuffix(name, "_test.go") {
				testFiles = append(testFiles, f)
			}
		case ".py":
			if strings.HasPrefix(name, "test_") || strings.Contains(name, "_test.py") {
				testFiles = append(testFiles, f)
			}
		case ".rs":
			if strings.Contains(name, ".rs") && (strings.HasPrefix(name, "test_") || strings.Contains(name, "_test")) {
				testFiles = append(testFiles, f)
			}
		case ".js", ".ts", ".jsx", ".tsx":
			if strings.HasSuffix(name, ".test.js") ||
				strings.HasSuffix(name, ".test.ts") ||
				strings.HasSuffix(name, ".test.jsx") ||
				strings.HasSuffix(name, ".test.tsx") ||
				strings.HasSuffix(name, ".spec.js") ||
				strings.HasSuffix(name, ".spec.ts") ||
				strings.HasSuffix(name, ".spec.jsx") ||
				strings.HasSuffix(name, ".spec.tsx") {
				testFiles = append(testFiles, f)
			}
		}
	}
	return testFiles
}

func renderTestImpactResults(sourcePath string, testFiles []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Test files affected by changes to %s\n", sourcePath))
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	for i, f := range testFiles {
		sb.WriteString(fmt.Sprintf("%d. @file %s\n", i+1, f))
	}
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Run these tests to verify the change:\n"))
	sb.WriteString(fmt.Sprintf("  go test ./... -run %s\n", guessTestPackage(testFiles)))
	return sb.String()
}

func guessTestPackage(testFiles []string) string {
	if len(testFiles) == 0 {
		return ""
	}
	// Use the directory of the first test file as the package path.
	dir := filepath.Dir(testFiles[0])
	if dir == "." {
		return ""
	}
	return "./" + dir
}

