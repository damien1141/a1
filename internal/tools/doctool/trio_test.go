package doctool

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var docOracleSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, ".tox": true, "dist": true, "build": true,
}

// docOracleWalk lists files under root in lexical order — a WalkDir
// recursion independent of the production filepath.Walk collectors.
func docOracleWalk(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if docOracleSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		files = append(files, path)
		return nil
	})
	require.NoError(t, err)
	return files
}

type docOracleSymbol struct {
	file string
	line int
	kind string
	name string
}

// docOracleExports collects top-level exported declarations with go/parser —
// the real Go grammar, independent of the production line regexp.
func docOracleExports(t *testing.T, path string) []docOracleSymbol {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err, "oracle fixture file %s must parse", path)

	var out []docOracleSymbol
	add := func(name string, line int, kind string) {
		if token.IsExported(name) {
			out = append(out, docOracleSymbol{file: path, line: line, kind: kind, name: name})
		}
	}
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				add(d.Name.Name, fset.Position(d.Pos()).Line, "func")
			}
		case *ast.GenDecl:
			kind := ""
			switch d.Tok {
			case token.TYPE:
				kind = "type"
			case token.VAR:
				kind = "var"
			case token.CONST:
				kind = "const"
			default:
				continue
			}
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					add(s.Name.Name, fset.Position(d.Pos()).Line, kind)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						add(n.Name, fset.Position(d.Pos()).Line, kind)
					}
				}
			}
		}
	}
	return out
}

// docOracleMentions finds backticked symbol candidates with a hand-rolled
// scanner: after each backtick, an uppercase letter followed by two or more
// identifier bytes and a closing backtick. No regexp involved.
func docOracleMentions(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(b)
	isIdent := func(c byte) bool {
		return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
	}
	isUpper := func(c byte) bool { return c >= 'A' && c <= 'Z' }

	mentions := map[string]bool{}
	for i := 0; i < len(text); i++ {
		if text[i] != '`' || i+1 >= len(text) || !isUpper(text[i+1]) {
			continue
		}
		j := i + 1
		for j < len(text) && isIdent(text[j]) {
			j++
		}
		if j < len(text) && text[j] == '`' && j-i-1 >= 3 {
			mentions[text[i+1:j]] = true
		}
	}
	return mentions
}

// oracle: naive reference implementation in this test — exported symbols
// come from go/parser (not the production line regexp) and documentation
// mentions from a hand-rolled backtick scanner (not the production regexp),
// cross-checked on a fixture with documented, undocumented, phantom, and
// boundary (too-short, lowercase) mentions across .md and .txt files, plus
// raw-string decoys and a grouped var block that the line regexp mishandled.
func TestRunDocSync_Differential(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "alpha.go", `package demo

func AlphaLive() {}

func AlphaDead() {}

type Widget struct{}

type DeadWidget struct{}

var Version = "1.0"

const OrphanConst = 9

`+
		// Raw-string decoys: to a line-anchored regexp these look like top-level
		// declarations; to the Go parser they are string data, so they must never
		// show up as undocumented exports.
		"const trapSource = `\nfunc FakeFunc(\ntype FakeType\nvar FakeVar\n`\n"+
		`var (
        ExportedVarA = 1
)
`)
	writeFile(t, root, "beta.go", `package demo

func BetaDead() {}
`)
	writeFile(t, root, "README.md", "# Demo\n"+
		"`AlphaLive` is the entry point; see `Widget` and `Version`.\n"+
		"`GhostDoc` was removed last week. `Ab` and `lower` are not symbols.\n")
	writeFile(t, root, "notes.txt", "also gone: `TxtGhost`\n")

	// Reference computation over the same tree.
	exports := map[string]docOracleSymbol{}
	for _, file := range docOracleWalk(t, root) {
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		for _, s := range docOracleExports(t, file) {
			exports[s.name] = s
		}
	}
	mentions := map[string]bool{}
	for _, file := range docOracleWalk(t, root) {
		ext := strings.ToLower(filepath.Ext(file))
		if ext == ".md" || ext == ".txt" {
			for m := range docOracleMentions(t, file) {
				mentions[m] = true
			}
		}
	}

	var want []string
	for name, s := range exports {
		if !mentions[name] {
			want = append(want, fmt.Sprintf("%s:%d\t%s\texported %s %s is not documented",
				s.file, s.line, s.name, s.kind, s.name))
		}
	}
	for m := range mentions {
		if _, ok := exports[m]; !ok {
			// A phantom hit carries no file: RelToCwd("") renders as "." and
			// the line is 0 — this pins the current render contract.
			want = append(want, fmt.Sprintf(".:0\t%s\tdocumentation mentions %s but symbol does not exist", m, m))
		}
	}
	// Non-vacuity: five undocumented exports across three declaration kinds
	// (including the grouped var member) plus two phantom mentions.
	require.Len(t, want, 7)

	raw, err := json.Marshal(docInput{Path: root, Glob: "*.go", Limit: 100})
	require.NoError(t, err)
	out, err := runDocSync(t.Context(), raw)
	require.NoError(t, err)

	got := strings.Split(strings.TrimSuffix(out.Content, "\n"), "\n")
	sort.Strings(got)
	sorted := append([]string(nil), want...)
	sort.Strings(sorted)
	assert.Equal(t, sorted, got)
	assert.Equal(t, "7 documentation issues", out.Detail)

	// The raw-string decoys are string data, not declarations, so they must
	// not be reported as undocumented exports.
	assert.NotContains(t, out.Content, "FakeFunc")
	assert.NotContains(t, out.Content, "FakeType")
	assert.NotContains(t, out.Content, "FakeVar")
	// The grouped var member is an export: reported with the group keyword's
	// line and the var kind.
	assert.Contains(t, out.Content,
		fmt.Sprintf("%s:20\tExportedVarA\texported var ExportedVarA is not documented",
			filepath.Join(root, "alpha.go")))
}
