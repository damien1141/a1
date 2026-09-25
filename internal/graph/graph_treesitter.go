//go:build treesitter

package graph

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// OpenGraph scans root and indexes imports using tree-sitter.
func OpenGraph(ctx context.Context, root string) (*Graph, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	root = filepath.Clean(root)
	g := &Graph{
		edges:   make(map[string][]string),
		reverse: make(map[string][]string),
		root:    root,
	}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == ".git" || name == "node_modules" || name == "target" || name == "dist" || name == "build" {
				return fs.SkipDir
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rel := strings.TrimPrefix(path, root)
		rel = strings.TrimPrefix(rel, string(filepath.Separator))
		if rel == "" {
			return nil
		}
		imports, ok := parseImports(path, rel)
		if !ok {
			return nil
		}
		g.mu.Lock()
		g.edges[rel] = imports
		for _, dst := range imports {
			g.reverse[dst] = append(g.reverse[dst], rel)
		}
		g.mu.Unlock()
		return nil
	}); err != nil {
		return nil, err
	}
	return g, nil
}

func parseImports(path, rel string) ([]string, bool) {
	entry := grammars.DetectLanguage(rel)
	if entry == nil || entry.Language() == nil {
		return nil, false
	}
	lang := entry.Language()
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse(src)
	if err != nil || tree == nil || tree.RootNode() == nil {
		return nil, false
	}
	root := tree.RootNode()
	var imports []string
	var walk func(*gotreesitter.Node)
	walk = func(n *gotreesitter.Node) {
		if n == nil {
			return
		}
		if isImportNode(n, lang) {
			if text := extractImportPath(n, src); text != "" {
				imports = append(imports, text)
			}
		}
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return imports, true
}

func isImportNode(n *gotreesitter.Node, lang *gotreesitter.Language) bool {
	if n == nil || !n.IsNamed() {
		return false
	}
	sym := n.Symbol()
	importDecl, _ := lang.SymbolByName("import_declaration")
	importSpec, _ := lang.SymbolByName("import_spec")
	useDecl, _ := lang.SymbolByName("use_declaration")
	scopedUseDecl, _ := lang.SymbolByName("scoped_use_declaration")
	callExpr, _ := lang.SymbolByName("call_expression")

	switch sym {
	case importDecl, importSpec, useDecl, scopedUseDecl:
		return true
	case callExpr:
		// JS/TS import()
		return true
	}
	return false
}

func extractImportPath(n *gotreesitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	start := int(n.StartByte())
	end := int(n.EndByte())
	if start < 0 || end > len(src) || start > end {
		return ""
	}
	text := string(src[start:end])
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.TrimPrefix(text, "import")
	text = strings.TrimPrefix(text, "use")
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "(")
	text = strings.TrimSuffix(text, ")")
	text = strings.TrimSpace(text)
	text = strings.Trim(text, `"`)
	text = strings.Trim(text, "`")
	text = strings.Trim(text, "'")
	return strings.TrimSpace(text)
}
