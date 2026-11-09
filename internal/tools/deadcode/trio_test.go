package deadcode

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oracleSkipDirs mirrors the directories both the candidate walk and the
// reference counter refuse to enter (the fixture only exercises "build").
var oracleSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, ".tox": true, "dist": true, "build": true,
}

// oracleWalk lists every file under root in lexical order, skipping the
// directories above — an os.ReadDir recursion, not filepath.Walk.
func oracleWalk(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	var files []string
	for _, name := range names {
		full := filepath.Join(root, name)
		info, err := os.Stat(full)
		require.NoError(t, err)
		if info.IsDir() {
			if oracleSkipDirs[name] {
				continue
			}
			files = append(files, oracleWalk(t, full)...)
			continue
		}
		files = append(files, full)
	}
	return files
}

type oracleSymbol struct {
	file string
	line int
	kind string
	name string
}

// oracleExports collects top-level exported declarations with go/parser —
// the real Go grammar, independent of the production scan. The fixture now
// includes grouped var/const blocks and raw-string decoys, so this oracle is
// the source of truth for both (the production scan must agree with it).
func oracleExports(t *testing.T, path string) []oracleSymbol {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err, "oracle fixture file %s must parse", path)

	var out []oracleSymbol
	add := func(name string, line int, kind string) {
		if token.IsExported(name) {
			out = append(out, oracleSymbol{file: path, line: line, kind: kind, name: name})
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

// isIdentByte reports whether c can appear inside a Go identifier.
func isIdentByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// oracleLineMentions reports whether line contains symbol as a standalone
// identifier, checked by hand-rolled boundary bytes instead of regexp \b.
func oracleLineMentions(line, symbol string) bool {
	for i := 0; i+len(symbol) <= len(line); i++ {
		if line[i:i+len(symbol)] != symbol {
			continue
		}
		before := i == 0 || !isIdentByte(line[i-1])
		after := i+len(symbol) == len(line) || !isIdentByte(line[i+len(symbol)])
		if before && after {
			return true
		}
	}
	return false
}

// oracleCountRefs counts referencing lines the way the documented signal
// defines them: non-test .go files, outside the defining file, outside the
// skipped directories.
func oracleCountRefs(t *testing.T, root, defFile, symbol string) int {
	t.Helper()
	refs := 0
	for _, file := range oracleWalk(t, root) {
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") || file == defFile {
			continue
		}
		b, err := os.ReadFile(file)
		require.NoError(t, err)
		for _, line := range strings.Split(string(b), "\n") {
			if oracleLineMentions(line, symbol) {
				refs++
				break // one line counts once, grep -c style
			}
		}
	}
	return refs
}

// oracle: naive reference implementation in this test — declarations come
// from go/parser (not the production line regexp) and reference counts from
// a hand-rolled identifier-boundary scan (not util.CountGoReferences's
// regexp), compared on a fixture tree with alive, dead, test-only-referenced,
// build-dir-buried, name-boundary-trap, raw-string-decoy, and grouped
// var/const symbols.
func TestRunDeadcode_Differential(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "alpha.go", `package demo

func AlphaLive() {}

func AlphaDead() {}

type Widget struct{}

type DeadWidget struct{}

var Version = "1.0"

const Pi = 3.14

const OrphanConst = 9

`+
		// Raw-string decoys: to a line-anchored regexp these look like top-level
		// declarations; to the Go parser they are string data. They must not
		// appear in the report.
		"const trapSource = `\nfunc FakeRawString(\ntype FakeTypeToo\nvar FakeVarAlso\n`\n"+
		`var (
        GroupedDead   = 1
        GroupedLiving = 2
)

const (
        GroupedConstDead = 3
)
`)
	writeFile(t, root, "beta.go", `package demo

func BetaUse() {
        AlphaLive()
        var w Widget
        _ = w
        ratio := Pi * 2
        _ = ratio
        NotAlphaLive()
        AlphaLiveExtended()
        _ = GroupedLiving
}
`)
	writeFile(t, root, "ghost_test.go", `package demo

import "testing"

func TestHelper(t *testing.T) {
        AlphaDead()
        GammaDead()
}
`)
	writeFile(t, root, "notes.txt", "AlphaDead DeadWidget\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	writeFile(t, filepath.Join(root, "sub"), "gamma.go", `package demo

func GammaLive() {}

func GammaDead() {}
`)
	writeFile(t, filepath.Join(root, "sub"), "use.go", `package demo

func UseGamma() {
        GammaLive()
}
`)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "build"), 0o755))
	writeFile(t, filepath.Join(root, "build"), "ignored.go", `package demo

func Buried() {}

func BuriedRefs() {
        GammaDead()
        DeadWidget{}
}
`)

	// Expected dead symbols, in candidate (lexical walk) order.
	var want []oracleSymbol
	for _, file := range oracleWalk(t, root) {
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		for _, s := range oracleExports(t, file) {
			if oracleCountRefs(t, root, s.file, s.name) == 0 {
				want = append(want, s)
			}
		}
	}
	// Non-vacuity: the fixture must produce a specific dead set spanning
	// all four declaration kinds, including grouped members.
	kindSet := map[string]bool{}
	for _, s := range want {
		kindSet[s.kind] = true
	}
	var kinds []string
	for k := range kindSet {
		kinds = append(kinds, k)
	}
	assert.ElementsMatch(t, []string{"func", "type", "var", "const"}, kinds)
	require.Len(t, want, 10)

	var wantContent strings.Builder
	for _, s := range want {
		wantContent.WriteString(fmt.Sprintf("%s:%d\t%s\t%s\n", s.file, s.line, s.kind, s.name))
	}

	raw, err := json.Marshal(deadcodeInput{Path: root, Glob: "*.go", Limit: 100})
	require.NoError(t, err)
	out, err := runDeadcode(t.Context(), raw)
	require.NoError(t, err)

	assert.Equal(t, wantContent.String(), out.Content)
	assert.Equal(t, "10 potentially dead symbols", out.Detail)
	assert.Equal(t, wantContent.String(), out.Output)

	// The raw-string decoys are string data, not declarations.
	assert.NotContains(t, out.Content, "FakeRawString")
	assert.NotContains(t, out.Content, "FakeTypeToo")
	assert.NotContains(t, out.Content, "FakeVarAlso")
	// The grouped var/const members are reported with the group keyword's
	// line and the GenDecl's kind.
	alpha := filepath.Join(root, "alpha.go")
	assert.Contains(t, out.Content, fmt.Sprintf("%s:22\tvar\tGroupedDead\n", alpha))
	assert.Contains(t, out.Content, fmt.Sprintf("%s:27\tconst\tGroupedConstDead\n", alpha))
}
