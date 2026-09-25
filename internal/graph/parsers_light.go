//go:build !treesitter

package graph

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func parseImports(path, rel string) ([]string, bool) {
	switch ext := strings.ToLower(filepath.Ext(rel)); ext {
	case ".go":
		return parseGoImports(path)
	case ".py":
		return parsePythonImports(path)
	case ".rs":
		return parseRustImports(path)
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
		return parseJSImports(path)
	default:
		return nil, false
	}
}

func parseGoImports(path string) ([]string, bool) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, false
	}
	var imports []string
	for _, imp := range node.Imports {
		p := strings.TrimSpace(imp.Path.Value)
		p = strings.Trim(p, `"`)
		if p != "" {
			imports = append(imports, p)
		}
	}
	return imports, true
}

var pythonImportRE = regexp.MustCompile(`(?m)^\s*(?:from\s+([\w.]+)\s+import|import\s+([\w.]+))`)

func parsePythonImports(path string) ([]string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	text := string(b)
	var imports []string
	for _, m := range pythonImportRE.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 {
			mod := strings.TrimSpace(m[1])
			if mod == "" && len(m) >= 3 {
				mod = strings.TrimSpace(m[2])
			}
			if mod != "" {
				imports = append(imports, mod)
			}
		}
	}
	return imports, true
}

var rustImportRE = regexp.MustCompile(`(?m)^\s*(?:use\s+([\w:]+)(?:\s*::\s*\{.*?\})?;|extern\s+crate\s+([\w_]+);)`)

func parseRustImports(path string) ([]string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	text := string(b)
	var imports []string
	for _, m := range rustImportRE.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 {
			mod := strings.TrimSpace(m[1])
			if mod == "" && len(m) >= 3 {
				mod = strings.TrimSpace(m[2])
			}
			if mod != "" {
				imports = append(imports, mod)
			}
		}
	}
	return imports, true
}

var jsImportRE = regexp.MustCompile(`(?m)^\s*(?:import\s+.*?from\s+['"]([^'"]+)['"]|require\(\s*['"]([^'"]+)['"]\s*\))`)

func parseJSImports(path string) ([]string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	text := string(b)
	var imports []string
	for _, m := range jsImportRE.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 {
			mod := strings.TrimSpace(m[1])
			if mod == "" && len(m) >= 3 {
				mod = strings.TrimSpace(m[2])
			}
			if mod != "" {
				imports = append(imports, mod)
			}
		}
	}
	return imports, true
}
