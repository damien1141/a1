package propertytool

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// oracleFunc is one expected suggestion triple derived from the AST.
type oracleFunc struct {
	file      string
	name      string
	signature string
	pkg       string
}

func oracleSkipDirs(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".tox", "dist", "build":
		return true
	}
	return false
}

// oracleFuncs re-derives the suggestion list from the go/ast of the fixture:
// exported top-level functions without receivers or variadic params, in
// source order, with signatures sliced straight out of the file bytes. Files
// that do not parse (no package clause) fall back to hand-written entries.
func oracleFuncs(t *testing.T, root string) []oracleFunc {
	t.Helper()
	var out []oracleFunc
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if oracleSkipDirs(filepath.Base(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		require.NoError(t, rerr)
		rel = filepath.ToSlash(rel)

		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if perr != nil {
			// The regex-based scanner still sees functions in files without a
			// package clause; they land in the "propertytest" package.
			if rel == "nolabel.go" {
				out = append(out, oracleFunc{
					file: rel, name: "Loose", signature: "func Loose(a int) int", pkg: "propertytest",
				})
			}
			return nil
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !ast.IsExported(fn.Name.Name) {
				continue
			}
			variadic := false
			if fn.Type.Params != nil {
				for _, p := range fn.Type.Params.List {
					if _, isEll := p.Type.(*ast.Ellipsis); isEll {
						variadic = true
					}
				}
			}
			if variadic {
				continue
			}
			// token.Pos carries the FileSet base; Position().Offset maps it back
			// to a byte offset into src.
			off := func(p token.Pos) int { return fset.Position(p).Offset }
			params := ""
			if fn.Type.Params != nil {
				params = string(src[off(fn.Type.Params.Opening)+1 : off(fn.Type.Params.Closing)])
			}
			results := ""
			if fn.Type.Results != nil && fn.Body != nil {
				results = strings.TrimSpace(string(src[off(fn.Type.Params.Closing)+1 : off(fn.Body.Lbrace)]))
			}
			out = append(out, oracleFunc{
				file:      rel,
				name:      fn.Name.Name,
				signature: "func " + fn.Name.Name + "(" + params + ") " + results,
				pkg:       file.Name.Name,
			})
		}
		return nil
	})
	require.NoError(t, err)
	return out
}

// writePropertyFixture builds a tree with exported functions, an unexported
// one, a method, a variadic, sub-packages, a vendor dir, a prose file, and a
// Go file without a package clause.
func writePropertyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "vendor"), 0o755))

	writeFile(t, root, "calc.go", `package calc

func Add(a, b int) int {
        return a + b
}

func helper(x int) int {
        return x
}

func (c Calc) Twice(x int) int {
        return 2 * x
}

func Sum(xs ...int) int {
        return 0
}

func Name() string {
        return "calc"
}

func Log(msg string) {
        _ = msg
}
`)
	writeFile(t, root, "nolabel.go", `func Loose(a int) int {
        return a
}
`)
	writeFile(t, root, "notes.txt", "func NotGo() int\n")
	writeFile(t, root, filepath.Join("sub", "extra.go"), `package extra

func Join(parts []string) string {
        return ""
}
`)
	writeFile(t, root, filepath.Join("vendor", "skip.go"), `package vendored

func Skip() {}
`)
	return root
}

func runPropertyOn(t *testing.T, glob string, limit int) (string, string) {
	t.Helper()
	raw, err := json.Marshal(propertyInput{Path: ".", Glob: glob, Limit: limit})
	require.NoError(t, err)
	out, err := runProperty(t.Context(), raw)
	require.NoError(t, err)
	return out.Content, out.Detail
}

// trioBlock is one parsed suggestion from the rendered output.
type trioBlock struct {
	file      string
	name      string
	signature string
	skeleton  string
}

// parsePropertyOutput splits the rendered output back into suggestion blocks
// and pulls the Go skeleton out of each one.
func parsePropertyOutput(t *testing.T, content string) []trioBlock {
	t.Helper()
	var blocks []trioBlock
	for _, chunk := range strings.Split(content, "@file ") {
		if chunk == "" {
			continue
		}
		lines := strings.SplitN(chunk, "\n", 3)
		require.Len(t, lines, 3)
		var b trioBlock
		b.file = lines[0]
		require.True(t, strings.HasPrefix(lines[1], "func: "), "func line in %q", chunk)
		b.name = strings.TrimPrefix(lines[1], "func: ")
		rest := lines[2]
		nl := strings.Index(rest, "\n")
		require.True(t, strings.HasPrefix(rest, "signature: "), "signature line in %q", chunk)
		b.signature = strings.TrimPrefix(rest[:nl], "signature: ")
		body := rest[nl+1:]
		require.True(t, strings.HasPrefix(body, "\n"), "blank line after signature in %q", chunk)
		skel := strings.TrimPrefix(body, "\n")
		// The skeleton runs to the end of this block; strip the renderer's
		// trailing blank separator when present.
		b.skeleton = strings.TrimSuffix(skel, "\n")
		blocks = append(blocks, b)
	}
	return blocks
}

// oracle: stdlib go/ast — the suggestion triples must match an independent
// AST derivation (exported, receiver-less, non-variadic functions with
// signatures sliced from the source bytes), and every generated skeleton must
// itself parse as Go in the right package with exactly one Test<Name>_Property
// function.
func TestRunProperty_MatchesGoParserOracle_Differential(t *testing.T) {
	root := writePropertyFixture(t)
	t.Chdir(root)

	got, detail := runPropertyOn(t, "*.go", 50)
	want := oracleFuncs(t, root)
	require.Len(t, want, 5, "Add, Name, Log, Loose, Join survive filtering")

	blocks := parsePropertyOutput(t, got)
	require.Len(t, blocks, len(want))
	for i, b := range blocks {
		assert.Equal(t, want[i].file, b.file, "suggestion %d file", i)
		assert.Equal(t, want[i].name, b.name, "suggestion %d name", i)
		assert.Equal(t, want[i].signature, b.signature, "suggestion %d signature", i)

		fset := token.NewFileSet()
		skel, err := parser.ParseFile(fset, b.file+"_property_test.go", b.skeleton, parser.SkipObjectResolution)
		require.NoError(t, err, "skeleton for %s must parse", b.name)
		assert.Equal(t, want[i].pkg, skel.Name.Name, "skeleton for %s must use the source package", b.name)
		testFuncs := 0
		for _, decl := range skel.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				testFuncs++
				assert.Equal(t, "Test"+want[i].name+"_Property", fn.Name.Name)
			}
		}
		assert.Equal(t, 1, testFuncs, "skeleton for %s declares exactly one test function", b.name)
	}
	assert.Equal(t, "5 property test skeletons", detail)

	// Excluded by the AST rule and by the walk rule alike.
	for _, banned := range []string{"helper", "Twice", "Sum", "Skip", "NotGo", "vendor"} {
		assert.NotContains(t, got, "func: "+banned)
	}

	t.Run("limit keeps the first suggestions in walk order", func(t *testing.T) {
		got, detail := runPropertyOn(t, "*.go", 3)
		blocks := parsePropertyOutput(t, got)
		require.Len(t, blocks, 3)
		for i, b := range blocks {
			assert.Equal(t, want[i].name, b.name)
		}
		assert.Equal(t, "3 property test skeletons", detail)
	})
}

// oracle: hand-written golden literal — the complete rendered output for one
// exported function must equal this string exactly: header lines, signature,
// package clause, imports, and the testing/quick skeleton.
func TestRunProperty_OutputGoldenLiteral_Differential(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "calc.go", `package calc

func Add(a, b int) int {
        return a + b
}
`)
	t.Chdir(root)

	got, detail := runPropertyOn(t, "*.go", 10)
	want := strings.Join([]string{
		"@file calc.go",
		"func: Add",
		"signature: func Add(a, b int) int",
		"",
		"package calc",
		"",
		"import (",
		"\t\"testing\"",
		"\t\"testing/quick\"",
		")",
		"",
		"func TestAdd_Property(t *testing.T) {",
		"\tf := func(a, b int) bool {",
		"\t\t// TODO: implement a property check for Add.",
		"\t\t// A good starter: round-trip (f(x) inverse-f applied) or idempotency (f(f(x)) == f(x)).",
		"\t\treturn true",
		"\t}",
		"\tif err := quick.Check(f, nil); err != nil {",
		"\t\tt.Errorf(\"property Add failed: %v\", err)",
		"\t}",
		"}",
		"",
		"",
	}, "\n")
	assert.Equal(t, want, got)
	assert.Equal(t, "1 property test skeletons", detail)
}
