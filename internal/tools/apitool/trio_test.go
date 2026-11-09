package apitool

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// astSymbol is one exported symbol as the stdlib Go parser sees it.
type astSymbol struct {
	file string
	kind string
	name string
}

// astApiSymbols is the symbol half of the oracle: go/parser + go/ast over the
// fixture files, keeping exported top-level funcs (methods excluded), types,
// vars, and consts.
func astApiSymbols(t *testing.T, path string) []astSymbol {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err, "fixture %s must parse", path)

	var out []astSymbol
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Recv != nil || !decl.Name.IsExported() {
				continue
			}
			out = append(out, astSymbol{file: path, kind: "func", name: decl.Name.Name})
		case *ast.GenDecl:
			kind := ""
			switch decl.Tok {
			case token.TYPE:
				kind = "type"
			case token.VAR:
				kind = "var"
			case token.CONST:
				kind = "const"
			default:
				continue
			}
			// Grouped decls contribute every exported member, and a value spec
			// can name several — the grammar is the oracle, not the old regexp's
			// one-decl-one-name shape.
			for _, spec := range decl.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name.IsExported() {
						out = append(out, astSymbol{file: path, kind: kind, name: s.Name.Name})
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.IsExported() {
							out = append(out, astSymbol{file: path, kind: kind, name: n.Name})
						}
					}
				}
			}
		}
	}
	return out
}

// astReferences is the usage half of the oracle: it counts *ast.Ident uses of
// symbol across the fixture's non-test Go files, skipping the defining file —
// the same population CountGoReferences greps. Each fixture use sits on its
// own line, so ident count equals the tool's per-line grep count.
func astReferences(t *testing.T, files []string, defFile, symbol string) int {
	t.Helper()
	count := 0
	for _, f := range files {
		if f == defFile || strings.HasSuffix(f, "_test.go") {
			continue
		}
		tree, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		require.NoError(t, err, "fixture %s must parse", f)
		ast.Inspect(tree, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == symbol {
				count++
			}
			return true
		})
	}
	return count
}

// writeApiFixture lays down a cross-referencing Go tree: lib.go declares the
// exports, other.go and extra.go use some of them, other_test.go's usage must
// not count, sub/ proves the walk recurses, and other.go carries a
// comment-only mention of Orphan — talk, not usage, which must not mark it
// used.
func writeApiFixture(t *testing.T) (root string, goFiles []string) {
	t.Helper()
	root = t.TempDir()
	files := map[string]string{
		"lib.go": "package fixture\n" +
			"\n" +
			"// Used is called from other files.\n" +
			"func Used() {}\n" +
			"\n" +
			"type Widget struct{}\n" +
			"\n" +
			"const Limit = 10\n" +
			"\n" +
			"// Grouped declarations: every exported member is part of the API.\n" +
			"var (\n" +
			"\tGroupedA = 1\n" +
			"\tGroupedB = 2\n" +
			"\n" +
			"\tprivate = 3\n" +
			")\n" +
			"\n" +
			"const (\n" +
			"\tAlpha, Beta = 4, 5\n" +
			")\n" +
			"\n" +
			"// Doc carries a raw-string decoy: lines that look like declarations\n" +
			"// inside the literal are string content, not exports.\n" +
			"const Doc = `doc prefix\n" +
			"func FakeRaw() {}\n" +
			"type FakeType struct{}\n" +
			"`\n" +
			"\n" +
			"// Orphan is never called outside this file.\n" +
			"func Orphan() {}\n" +
			"\n" +
			"func hidden() {}\n" +
			"\n" +
			"func (w Widget) Polish() {}\n",
		"other.go": "package fixture\n" +
			"\n" +
			"func consume() {\n" +
			"\tUsed()\n" +
			"\tUsed()\n" +
			"\tw := Widget{}\n" +
			"\t_ = w\n" +
			"\tif 1 < Limit {\n" +
			"\t\t_ = 1\n" +
			"\t}\n" +
			"\t// Orphan is only mentioned in this comment, never called.\n" +
			"}\n",
		"extra.go": "package fixture\n" +
			"\n" +
			"func extraUse() {\n" +
			"\tUsed()\n" +
			"}\n",
		"other_test.go": "package fixture\n" +
			"\n" +
			"import \"testing\"\n" +
			"\n" +
			"func TestOrphan(t *testing.T) {\n" +
			"\tOrphan()\n" +
			"}\n",
		"sub/deep.go": "package sub\n" +
			"\n" +
			"// Deep is declared in a subdirectory.\n" +
			"func Deep() {}\n",
		"readme.txt": "func NotGo() {}\n",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
		if strings.HasSuffix(name, ".go") {
			goFiles = append(goFiles, path)
		}
	}
	return root, goFiles
}

// oracle: stdlib go/ast + go/parser on the same fixture files — exported
// symbol set (methods excluded) plus ident-level reference counting over the
// non-test, non-defining files, compared against the tool's regexp scan and
// ref counts, including the refs-descending output order.
func TestRunApi_Differential(t *testing.T) {
	root, _ := writeApiFixture(t)
	// filepath.Walk order for the fixture: extra.go, lib.go, other.go,
	// other_test.go, sub/deep.go — mirror it so tie-broken rows are stable.
	sorted := []string{
		filepath.Join(root, "extra.go"),
		filepath.Join(root, "lib.go"),
		filepath.Join(root, "other.go"),
		filepath.Join(root, "other_test.go"),
		filepath.Join(root, "sub", "deep.go"),
	}
	for _, f := range sorted {
		require.FileExists(t, f)
	}

	type wantRow struct {
		status string
		kind   string
		name   string
		refs   int
	}
	var want []wantRow
	for _, f := range sorted {
		for _, s := range astApiSymbols(t, f) {
			refs := astReferences(t, sorted, f, s.name)
			status := "unused"
			if refs > 0 {
				status = "used"
			}
			want = append(want, wantRow{status: status, kind: s.kind, name: s.name, refs: refs})
		}
	}
	require.NotEmpty(t, want, "fixture must declare exports")

	raw, err := json.Marshal(apiInput{Path: root, Glob: "*.go", Limit: 50})
	require.NoError(t, err)
	out, err := runApi(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%d symbols", len(want)), out.Detail)

	var got []wantRow
	lines := strings.Split(strings.TrimSuffix(out.Content, "\n"), "\n")
	for _, line := range lines {
		fields := strings.Split(line, "\t")
		require.Len(t, fields, 5, "rendered row %q must be five tab fields", line)
		refs, err := strconv.Atoi(strings.TrimSuffix(fields[3], " refs"))
		require.NoError(t, err, "refs field %q must be numeric", fields[3])
		got = append(got, wantRow{status: fields[0], kind: fields[1], name: fields[2], refs: refs})
	}
	require.Len(t, got, len(want))
	assert.ElementsMatch(t, want, got)

	// The comment-only mention in other.go must not resurrect Orphan: it
	// stays unused (0 refs), the same verdict the ident-level oracle gives.
	for _, row := range got {
		if row.name == "Orphan" {
			assert.Equal(t, "unused", row.status, "comment mentions are not references")
			assert.Equal(t, 0, row.refs, "comment mentions are not references")
		}
	}

	// Raw-string decoys stay string content, never exports; grouped members
	// are real exports with the group keyword as their kind.
	for _, row := range got {
		assert.NotContains(t, []string{"FakeRaw", "FakeType"}, row.name,
			"declarations named inside the Doc raw string must not surface")
	}
	seenKind := map[string]string{}
	for _, row := range got {
		seenKind[row.name] = row.kind
	}
	for _, name := range []string{"GroupedA", "GroupedB", "Alpha", "Beta"} {
		require.Contains(t, seenKind, name, "grouped member %s must be reported", name)
		assert.Equal(t, "var", seenKind["GroupedA"])
		assert.Equal(t, "var", seenKind["GroupedB"])
		assert.Equal(t, "const", seenKind["Alpha"])
		assert.Equal(t, "const", seenKind["Beta"])
	}
	assert.NotContains(t, seenKind, "private", "unexported grouped members stay hidden")

	// The sort contract: rows come back with refs non-increasing.
	for i := 1; i < len(got); i++ {
		assert.GreaterOrEqual(t, got[i-1].refs, got[i].refs, "row %d must not have more refs than row %d", i, i-1)
	}

	// Spot-check the headline: the most-referenced symbol leads the report.
	require.NotEmpty(t, got)
	var maxRefs int
	for _, w := range want {
		if w.refs > maxRefs {
			maxRefs = w.refs
		}
	}
	assert.Equal(t, maxRefs, got[0].refs, "first row must carry the highest ref count")
}
