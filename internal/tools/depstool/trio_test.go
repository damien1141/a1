package depstool

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var depOracleSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, ".tox": true, "dist": true, "build": true,
}

// depOracleWalk lists files under root in lexical order, skipping the
// ignored directories — a WalkDir recursion independent of collectGoFiles.
func depOracleWalk(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if depOracleSkipDirs[d.Name()] {
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

// depOracleImports extracts the import paths of one file with go/parser
// ImportsOnly — the real Go grammar rather than the production line regexp.
func depOracleImports(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	require.NoError(t, err, "oracle fixture file %s must parse", path)
	var out []string
	for _, spec := range file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		require.NoError(t, err)
		out = append(out, p)
	}
	return out
}

// depOracleScan aggregates external import counts over the tree, mirroring
// only the documented rule: import paths starting with "." are not external.
func depOracleScan(t *testing.T, root string) []depEntry {
	t.Helper()
	counts := map[string]int{}
	for _, file := range depOracleWalk(t, root) {
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		for _, imp := range depOracleImports(t, file) {
			if strings.HasPrefix(imp, ".") {
				continue
			}
			counts[imp]++
		}
	}
	var out []depEntry
	for path, n := range counts {
		out = append(out, depEntry{path: path, count: n})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].count > out[j].count })
	return out
}

// oracle: go/parser on the same fixture tree — import paths come from the
// real Go grammar (parser.ImportsOnly + strconv.Unquote) instead of the
// production single-line regexp, aggregated with plain map counting and a
// stable descending sort. The fixture pins quote-form imports, relative
// (non-external) paths, non-Go files, skipped directories, and — via f.go —
// grouped `import ( ... )` blocks with an aliased entry, which the production
// regexp never matched.
func TestRunDeps_Differential(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", `package demo

import "fmt"
import "os"
import "./local"
`)
	writeFile(t, root, "b.go", `package demo

import "fmt"
import "os"
import "strings"
`)
	writeFile(t, root, "c.go", `package demo

import "fmt"
import "os"
`)
	// f.go exercises the grouped form: a parenthesized import block with an
	// aliased entry. The oracle (go/parser) sees fmt, os, and strings from it;
	// the production scan must too, or the differential catches the miss.
	writeFile(t, root, "f.go", `package demo

import (
        "fmt"
        "os"

        str "strings"
)
`)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	writeFile(t, filepath.Join(root, "sub"), "d.go", "package demo\n\nimport \"fmt\"\nimport `errors`\nimport \"strings\"\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "build"), 0o755))
	writeFile(t, filepath.Join(root, "build"), "e.go", `package demo

import "bytes"
`)
	writeFile(t, root, "notes.md", "import \"text/template\"\n")

	want := depOracleScan(t, root)
	// Non-vacuity: four external deps with distinct, tie-free counts so the
	// rendered order is deterministic (fmt 5, os 4, strings 3, errors 1).
	require.Len(t, want, 4)
	require.Equal(t, []int{5, 4, 3, 1}, []int{want[0].count, want[1].count, want[2].count, want[3].count})

	var wantContent strings.Builder
	for _, e := range want {
		wantContent.WriteString(strconv.Itoa(e.count))
		wantContent.WriteString("\t")
		wantContent.WriteString(e.path)
		wantContent.WriteString("\n")
	}

	raw, err := json.Marshal(depsInput{Path: root, Glob: "*.go", Limit: 100})
	require.NoError(t, err)
	out, err := runDeps(t.Context(), raw)
	require.NoError(t, err)

	assert.Equal(t, wantContent.String(), out.Content)
	assert.Equal(t, "4 dependencies", out.Detail)
	assert.Equal(t, wantContent.String(), out.Output)
}
