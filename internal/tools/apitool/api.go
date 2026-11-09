package apitool

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/util"
)

const (
	apiDefaultLimit = 100
)

var apiDescription = `Inspect the exported API surface of Go packages.

Lists exported functions, types, variables, and constants, and flags those
that appear unused outside their defining file. Helps detect dead public API.`

// ApiTool returns the API surface validator tool definition + handler.
func ApiTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "api",
			Description: apiDescription,
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
						"type": "integer",
						"description": fmt.Sprintf(
							"Maximum symbols to return. Example: 50 (default: %d)",
							apiDefaultLimit,
						),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in apiInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("api surface %s", p)
		},
		Run: runApi,
	}
}

type apiInput struct {
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type apiSymbol struct {
	file string
	name string
	kind string
	used bool
	refs int
}

func runApi(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in apiInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse api arguments: %w", err)
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
		limit = apiDefaultLimit
	}

	symbols, err := inspectApiSurface(searchPath, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(symbols) == 0 {
		return tooldef.Result{
			Content: "No exported symbols found",
			Detail:  "0 symbols",
			Output:  "No exported symbols found",
		}, nil
	}

	content := renderApiResults(ctx, symbols)
	detail := fmt.Sprintf("%d symbols", len(symbols))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func inspectApiSurface(root, glob string, limit int) ([]apiSymbol, error) {
	var goFiles []string
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
			goFiles = append(goFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var symbols []apiSymbol
	for _, file := range goFiles {
		fileSymbols, err := parseExports(file)
		if err != nil {
			continue
		}
		symbols = append(symbols, fileSymbols...)
	}

	// Cross-reference: check usage outside the defining file.
	cwd, _ := os.Getwd()
	for i := range symbols {
		refs, err := countReferences(root, symbols[i].file, symbols[i].name, cwd)
		if err != nil {
			refs = 0
		}
		symbols[i].refs = refs
		symbols[i].used = refs > 0
	}

	sortSymbols(symbols)

	if len(symbols) > limit {
		symbols = symbols[:limit]
	}

	return symbols, nil
}

var exportRe = regexp.MustCompile(`(?m)^(?:func|type|var|const)\s+([A-Z][a-zA-Z0-9_]*)\b`)

// parseExports collects exported top-level declarations of one Go file. The
// go/parser pass uses the real grammar, so declarations named inside raw
// string literals stay strings and grouped `var ( ... )` / `const ( ... )`
// members are reported individually (with the GenDecl's keyword as their
// kind) — the two blind spots the historic regexp could not see. Files too
// malformed to parse fall back to that regexp instead of being dropped.
func parseExports(path string) ([]apiSymbol, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if symbols, ok := parseExportsAST(path, b); ok {
		return symbols, nil
	}

	text := strings.ReplaceAll(string(b), "\r\n", "\n")

	matches := exportRe.FindAllStringSubmatch(text, -1)
	var symbols []apiSymbol
	for _, m := range matches {
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
		symbols = append(symbols, apiSymbol{
			file: path,
			name: m[1],
			kind: kind,
		})
	}

	return symbols, nil
}

func parseExportsAST(path string, src []byte) ([]apiSymbol, bool) {
	// SkipObjectResolution keeps the parse cheap; we only walk Decls.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, false
	}
	var symbols []apiSymbol
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && token.IsExported(d.Name.Name) {
				symbols = append(symbols, apiSymbol{
					file: path, name: d.Name.Name, kind: "func",
				})
			}
		case *ast.GenDecl:
			kind := genDeclKind(d.Tok)
			if kind == "" {
				continue
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if token.IsExported(s.Name.Name) {
						symbols = append(symbols, apiSymbol{
							file: path, name: s.Name.Name, kind: kind,
						})
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if token.IsExported(n.Name) {
							symbols = append(symbols, apiSymbol{
								file: path, name: n.Name, kind: kind,
							})
						}
					}
				}
			}
		}
	}
	return symbols, true
}

func genDeclKind(tok token.Token) string {
	switch tok {
	case token.TYPE:
		return "type"
	case token.VAR:
		return "var"
	case token.CONST:
		return "const"
	}
	return ""
}

// countReferences is a Go-native reference counter that replaces the previous
// shell-out to `grep -r`. Behavior change: matches are now anchored to Go
// identifier boundaries, so `Foo` no longer matches `FooBar` — that's the
// correct behavior for "is this exported name used" and removes the
// inflation that came with substring grep. The defining file and _test.go
// files are still excluded.
func countReferences(root, defFile, symbolName, cwd string) (int, error) {
	_ = cwd // was only used as cmd.Dir for the grep shell-out; unused now.
	return util.CountGoReferences(root, defFile, symbolName)
}

func sortSymbols(symbols []apiSymbol) {
	for i := 0; i < len(symbols); i++ {
		for j := i + 1; j < len(symbols); j++ {
			if symbols[j].refs > symbols[i].refs {
				symbols[i], symbols[j] = symbols[j], symbols[i]
			}
		}
	}
}

func renderApiResults(ctx context.Context, symbols []apiSymbol) string {
	var sb strings.Builder
	for _, s := range symbols {
		rel := tooldef.RelToCwd(ctx, s.file)
		status := "used"
		if !s.used {
			status = "unused"
		}
		sb.WriteString(fmt.Sprintf("%s\t%s\t%s\t%d refs\t%s\n", status, s.kind, s.name, s.refs, rel))
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
