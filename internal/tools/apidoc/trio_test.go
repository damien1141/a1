package apidoc

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// astDocEntry is one exported declaration as the stdlib Go parser sees it.
type astDocEntry struct {
	file string
	kind string
	name string
	doc  string
}

// normalizeDoc collapses all whitespace runs so the tool's joined-with-space
// doc rendering and ast's Text() rendering compare equal regardless of the
// per-line separators each side uses.
func normalizeDoc(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// astApiDocEntries is the oracle: go/parser + go/ast on the same fixture
// files, keeping exported top-level funcs (methods excluded), types, vars,
// and consts — the documented apidoc scope.
func astApiDocEntries(t *testing.T, path string) []astDocEntry {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	require.NoError(t, err, "fixture %s must parse", path)

	var out []astDocEntry
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			if decl.Recv != nil || !decl.Name.IsExported() {
				continue
			}
			out = append(out, astDocEntry{file: path, kind: "func", name: decl.Name.Name, doc: normalizeDoc(decl.Doc.Text())})
		case *ast.GenDecl:
			if len(decl.Specs) != 1 {
				continue
			}
			var name *ast.Ident
			kind := ""
			switch spec := decl.Specs[0].(type) {
			case *ast.TypeSpec:
				name, kind = spec.Name, "type"
			case *ast.ValueSpec:
				if len(spec.Names) != 1 {
					continue
				}
				name = spec.Names[0]
				switch decl.Tok {
				case token.VAR:
					kind = "var"
				case token.CONST:
					kind = "const"
				}
			}
			if name == nil || kind == "" || !name.IsExported() {
				continue
			}
			out = append(out, astDocEntry{file: path, kind: kind, name: name.Name, doc: normalizeDoc(decl.Doc.Text())})
		}
	}
	return out
}

// toolDocEntry is one entry parsed back out of the tool's rendered content.
type toolDocEntry struct {
	fileBase string
	kind     string
	name     string
	doc      string
}

func parseApidocContent(t *testing.T, content string) []toolDocEntry {
	t.Helper()
	var out []toolDocEntry
	for _, block := range strings.Split(strings.TrimSuffix(content, "\n\n"), "\n\n") {
		lines := strings.Split(block, "\n")
		require.GreaterOrEqual(t, len(lines), 3, "block %q must carry @file, kind+name, signature", block)
		require.True(t, strings.HasPrefix(lines[0], "@file "), "block must start with @file, got %q", lines[0])
		kn := strings.Fields(lines[1])
		require.Len(t, kn, 2, "kind+name line must be two fields, got %q", lines[1])
		e := toolDocEntry{fileBase: filepath.Base(strings.TrimSpace(strings.TrimPrefix(lines[0], "@file "))), kind: kn[0], name: kn[1]}
		for _, l := range lines[2:] {
			if doc, ok := strings.CutPrefix(l, "doc: "); ok {
				e.doc = normalizeDoc(doc)
			}
		}
		out = append(out, e)
	}
	return out
}

// writeApidocFixture lays down the scan target: two root Go files, a _test.go
// (apidoc scans those too), a nested package, and a non-Go decoy.
func writeApidocFixture(t *testing.T) (root string, goFiles []string) {
	t.Helper()
	root = t.TempDir()
	files := map[string]string{
		"alpha.go": "package fixture\n" +
			"\n" +
			"// AlphaDoc is documented across two lines.\n" +
			"// This is the second line.\n" +
			"func AlphaDoc() {}\n" +
			"\n" +
			"// Widget is a documented type.\n" +
			"type Widget struct{ X int }\n" +
			"\n" +
			"func betaHelper() {}\n" +
			"\n" +
			"var Gamma = 3\n" +
			"\n" +
			"const Delta2 = \"d\"\n" +
			"\n" +
			"// Polish is a method, not a top-level export.\n" +
			"func (w Widget) Polish() {}\n",
		"alpha_test.go": "package fixture\n" +
			"\n" +
			"// TestOnlyExport lives in a _test.go file.\n" +
			"func TestOnlyExport() {}\n",
		"notes.txt": "func NotGo() {}\n",
		"sub/nest.go": "package sub\n" +
			"\n" +
			"// Nested sits in a subdirectory.\n" +
			"func Nested() {}\n",
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
// top-level declarations (methods excluded) and their doc comments, compared
// entry by entry against the tool's regexp-based scan and rendering.
func TestRunApidoc_Differential(t *testing.T) {
	root, goFiles := writeApidocFixture(t)

	var want []astDocEntry
	for _, f := range goFiles {
		want = append(want, astApiDocEntries(t, f)...)
	}
	require.NotEmpty(t, want, "fixture must declare exports")
	for _, f := range goFiles {
		if strings.HasSuffix(f, "_test.go") {
			require.NotEmpty(t, astApiDocEntries(t, f), "the _test.go fixture must contribute entries")
		}
	}

	raw, err := json.Marshal(apidocInput{Path: root, Glob: "*.go", Limit: 50})
	require.NoError(t, err)
	out, err := runApidoc(t.Context(), raw)
	require.NoError(t, err)

	got := parseApidocContent(t, out.Content)
	require.Len(t, got, len(want))
	assert.Equal(t, fmt.Sprintf("%d API docs generated", len(want)), out.Detail)

	byFileBase := map[string][]astDocEntry{}
	for _, e := range want {
		byFileBase[filepath.Base(e.file)] = append(byFileBase[filepath.Base(e.file)], e)
	}
	for _, e := range got {
		candidates, ok := byFileBase[e.fileBase]
		require.True(t, ok, "tool reported unknown file %q", e.fileBase)
		var match *astDocEntry
		for i := range candidates {
			if candidates[i].name == e.name {
				match = &candidates[i]
				break
			}
		}
		require.NotNil(t, match, "tool reported unknown symbol %q in %q", e.name, e.fileBase)
		assert.Equal(t, match.kind, e.kind, "kind mismatch for %q", e.name)
		assert.Equal(t, match.doc, e.doc, "doc mismatch for %q", e.name)
	}
}
