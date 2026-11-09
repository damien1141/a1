package graph

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trioFixture maps workspace-relative file -> raw import strings. The
// workspace is materialized on disk by trioWorkspace; module paths follow
// the fixture go.mod's module example.com/m.
var trioFixture = map[string][]string{
	"a/a.go": {"example.com/m/b"},
	"b/b.go": {"example.com/m/c", "example.com/m/d"},
	"c/c.go": {"example.com/m/d"},
	"d/d.go": nil,
	"e/e.go": nil,
}

// trioModuleFile is the hand-written module-path -> file mapping the
// resolver is expected to implement for this fixture: a package directory
// resolves to its base-named entry file.
var trioModuleFile = map[string]string{
	"example.com/m/a": "a/a.go",
	"example.com/m/b": "b/b.go",
	"example.com/m/c": "c/c.go",
	"example.com/m/d": "d/d.go",
	"example.com/m/e": "e/e.go",
}

// trioWorkspaceWith writes one .go file per fixture entry under a temp
// root, plus the go.mod the module-prefixed imports assume.
func trioWorkspaceWith(t *testing.T, fixture map[string][]string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	}
	write("go.mod", "module example.com/m\n\ngo 1.26\n")
	for rel, imports := range fixture {
		var sb strings.Builder
		fmt.Fprintf(&sb, "package %s\n", strings.TrimSuffix(filepath.Base(rel), ".go"))
		for _, imp := range imports {
			fmt.Fprintf(&sb, "import %q\n", imp)
		}
		write(rel, sb.String())
	}
	return dir
}

func trioWorkspace(t *testing.T) string {
	t.Helper()
	return trioWorkspaceWith(t, trioFixture)
}

// ---------- differential ----------

// oracle: naive adjacency maps — the fixture table read forward as the
// expected import lists, and iterated backward with a plain double loop to
// build the expected reverse adjacency (raw import string -> importers).
func TestEdgeQueries_Differential(t *testing.T) {
	g, err := OpenGraph(t.Context(), trioWorkspace(t))
	require.NoError(t, err)

	files := make([]string, 0, len(trioFixture))
	rev := make(map[string][]string)
	for src, imports := range trioFixture {
		files = append(files, src)
		for _, dst := range imports {
			rev[dst] = append(rev[dst], src)
		}
	}

	assert.Equal(t, len(trioFixture), g.Len())
	assert.ElementsMatch(t, files, g.AllFiles())

	for _, f := range files {
		assert.ElementsMatch(t, trioFixture[f], g.Imports(f), "forward edges of %s", f)
	}
	// Source order is preserved within one file's import list.
	assert.Equal(t, []string{"example.com/m/c", "example.com/m/d"}, g.Imports("b/b.go"))

	for mod, want := range rev {
		assert.ElementsMatch(t, want, g.ImportedBy(mod), "reverse edges of raw import %s", mod)
	}
	// The reverse map is keyed by the raw import string, not the resolved
	// file path — pin both sides of that behavior.
	for _, f := range files {
		assert.Empty(t, g.ImportedBy(f), "no file-path key in the reverse map: %s", f)
	}
	// vendor/ is never indexed, so its edge exists nowhere.
	assert.Empty(t, g.ImportedBy("example.com/m/a"))
	assert.Empty(t, g.Imports("vendor/v.go"))
}

// oracle: a hand-derived golden order (Kahn's run traced on paper for this
// fixture) plus a naive BFS over the hand-written module->file map —
// anything import-reachable from a file must precede it in the sort.
func TestTopologicalSort_Differential(t *testing.T) {
	ws := trioWorkspace(t)
	g, err := OpenGraph(t.Context(), ws)
	require.NoError(t, err)

	in := []string{"a/a.go", "b/b.go", "c/c.go", "d/d.go", "e/e.go"}
	got := g.TopologicalSortWithResolver(in, NewPathResolver(ws))

	assert.ElementsMatch(t, in, got, "a sort must be a permutation of its input")
	// Hand-derived: the out-degree-0 files (d, e) enter the LIFO ready queue
	// in input order, so they leave it reversed (e, d); then c, b, a peel off
	// as their importers' out-degrees hit zero.
	assert.Equal(t, []string{"e/e.go", "d/d.go", "c/c.go", "b/b.go", "a/a.go"}, got)

	idx := make(map[string]int, len(got))
	for i, f := range got {
		idx[f] = i
	}
	for _, from := range in {
		for _, dep := range trioReach(from) {
			assert.Less(t, idx[dep], idx[from],
				"%s imports (transitively) %s, so %s must come first", from, dep, dep)
		}
	}
}

// trioReach is the naive BFS reference: walk the fixture's import lists,
// mapping each raw module path through the hand-written module->file table.
func trioReach(from string) []string {
	seen := map[string]bool{}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, mod := range trioFixture[cur] {
			f := trioModuleFile[mod]
			if f == "" || seen[f] {
				continue
			}
			seen[f] = true
			queue = append(queue, f)
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	return out
}

// ---------- negative ----------

func TestGraph_Negative(t *testing.T) {
	t.Run("unknown nodes on a populated graph stay empty", func(t *testing.T) {
		g, err := OpenGraph(t.Context(), trioWorkspace(t))
		require.NoError(t, err)
		require.Equal(t, len(trioFixture), g.Len())
		assert.Empty(t, g.Imports("ghost/ghost.go"))
		assert.Empty(t, g.ImportedBy("example.com/m/ghost"))
		assert.Empty(t, g.ImportedBy(""))
	})

	t.Run("empty workspace indexes nothing", func(t *testing.T) {
		dir := t.TempDir()
		g, err := OpenGraph(t.Context(), dir)
		require.NoError(t, err)
		assert.Equal(t, 0, g.Len())
		assert.Empty(t, g.AllFiles())
		assert.Equal(t, dir, g.Root())
		// Constraint-free files still pass through the LIFO ready queue, so
		// they come back in reverse input order — actual behavior, pinned.
		assert.Equal(t, []string{"r.go", "q.go", "p.go"}, g.TopologicalSort([]string{"p.go", "q.go", "r.go"}))
	})

	t.Run("missing root degrades to an empty graph", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "definitely-absent")
		g, err := OpenGraph(t.Context(), missing)
		require.NoError(t, err, "a missing root is a first scan, not a failure")
		require.NotNil(t, g)
		assert.Equal(t, 0, g.Len())
		assert.Empty(t, g.AllFiles())
		assert.Equal(t, missing, g.Root())
	})

	t.Run("cycle falls back to input order without hanging", func(t *testing.T) {
		ws := trioWorkspaceWith(t, map[string][]string{
			"x/x.go": {"example.com/m/y"},
			"y/y.go": {"example.com/m/x"},
			"z/z.go": {"example.com/m/x"},
		})
		g, err := OpenGraph(t.Context(), ws)
		require.NoError(t, err)
		in := []string{"x/x.go", "y/y.go", "z/z.go"}
		assert.Equal(t, in, g.TopologicalSortWithResolver(in, NewPathResolver(ws)),
			"nothing in a cycle reaches out-degree 0, so the leftovers keep their input order")
	})

	t.Run("self import is a one-node cycle", func(t *testing.T) {
		ws := trioWorkspaceWith(t, map[string][]string{"s/s.go": {"example.com/m/s"}})
		g, err := OpenGraph(t.Context(), ws)
		require.NoError(t, err)
		assert.Equal(t, []string{"s/s.go"}, g.TopologicalSortWithResolver([]string{"s/s.go"}, NewPathResolver(ws)))
	})

	t.Run("duplicate imports duplicate the reverse edge", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "dup"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "dup", "dup.go"),
			[]byte("package dup\nimport \"x\"\nimport \"x\"\n"), 0o644))
		g, err := OpenGraph(t.Context(), dir)
		require.NoError(t, err)
		assert.Equal(t, 1, g.Len())
		assert.Equal(t, []string{"x", "x"}, g.Imports("dup/dup.go"))
		// Actual behavior, pinned: the reverse list keeps one entry per edge,
		// so a twice-imported target lists the importer twice.
		assert.Equal(t, []string{"dup/dup.go", "dup/dup.go"}, g.ImportedBy("x"))
	})
}
