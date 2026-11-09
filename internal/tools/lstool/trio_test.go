package lstool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// ---------- differential ----------

// oracleNode mirrors the listed tree: name, dir-ness, children.
type oracleNode struct {
	name     string
	isDir    bool
	children []*oracleNode
}

// oracleVisible is the documented listing rule, hand-written here instead of
// reading the production skip set: hidden files and well-known cache/VCS
// directories never appear.
func oracleVisible(name string) bool {
	if strings.HasPrefix(name, ".") {
		return false
	}
	switch name {
	case "node_modules", "vendor", "build", "dist", "target", "__pycache__":
		return false
	}
	return true
}

// oracleBuildTree re-derives buildTree over os.ReadDir listings: entries are
// ordered by lowercased name, files increment the counter, directories past
// maxDepth become leaves, and counting stops once the file budget is spent.
func oracleBuildTree(t *testing.T, dir string, fileCount *int, limit, depth, maxDepth int) *oracleNode {
	t.Helper()
	ents, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})

	node := &oracleNode{name: filepath.Base(dir), isDir: true}
	for _, name := range names {
		if !oracleVisible(name) {
			continue
		}
		if *fileCount >= limit {
			break
		}
		childPath := filepath.Join(dir, name)
		if info, err := os.Stat(childPath); err == nil && info.IsDir() {
			if depth+1 >= maxDepth {
				node.children = append(node.children, &oracleNode{name: name, isDir: true})
				continue
			}
			node.children = append(node.children, oracleBuildTree(t, childPath, fileCount, limit, depth+1, maxDepth))
			continue
		}
		*fileCount++
		node.children = append(node.children, &oracleNode{name: name})
	}
	return node
}

// oracleRender flattens the tree in preorder and rebuilds each line from its
// ancestor is-last chain — a different mechanism from the recursive
// prefix-accumulating renderer under test.
func oracleRender(rootName string, root *oracleNode) string {
	type flat struct {
		ancestry []bool // is-last flags of the ancestors, outermost first
		isLast   bool
		node     *oracleNode
	}
	var rows []flat
	var walk func(n *oracleNode, ancestry []bool)
	walk = func(n *oracleNode, ancestry []bool) {
		for i, c := range n.children {
			isLast := i == len(n.children)-1
			rows = append(rows, flat{ancestry: ancestry, isLast: isLast, node: c})
			if c.isDir && len(c.children) > 0 {
				walk(c, append(append([]bool{}, ancestry...), isLast))
			}
		}
	}
	walk(root, nil)

	var b strings.Builder
	b.WriteString(rootName + "/\n")
	for _, r := range rows {
		for _, last := range r.ancestry {
			if last {
				b.WriteString("    ")
			} else {
				b.WriteString("│   ")
			}
		}
		if r.isLast {
			b.WriteString("└── ")
		} else {
			b.WriteString("├── ")
		}
		b.WriteString(r.node.name)
		if r.node.isDir {
			b.WriteString("/")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeLsFixture materializes the fixture tree under root/tree:
//
//	tree/a.txt
//	tree/Alpha/{note.md,zebra.txt}
//	tree/beta.txt
//	tree/.hidden                (skipped)
//	tree/Gamma/Sub/deep.txt     (file at depth 3: listed)
//	tree/Gamma/Sub/SubSub/x.txt (dir at depth 3: listed as a leaf only)
//	tree/node_modules/dep.js    (skipped)
func writeLsFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	tree := filepath.Join(root, "tree")
	mkdirs := []string{
		filepath.Join(tree, "Alpha"),
		filepath.Join(tree, "Gamma", "Sub", "SubSub"),
		filepath.Join(tree, "node_modules"),
	}
	for _, d := range mkdirs {
		require.NoError(t, os.MkdirAll(d, 0o755))
	}
	files := map[string]string{
		filepath.Join(tree, "a.txt"):                           "a",
		filepath.Join(tree, "Alpha", "note.md"):                "n",
		filepath.Join(tree, "Alpha", "zebra.txt"):              "z",
		filepath.Join(tree, "beta.txt"):                        "b",
		filepath.Join(tree, ".hidden"):                         "h",
		filepath.Join(tree, "Gamma", "Sub", "deep.txt"):        "d",
		filepath.Join(tree, "Gamma", "Sub", "SubSub", "x.txt"): "x",
		filepath.Join(tree, "node_modules", "dep.js"):          "j",
	}
	for p, content := range files {
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	return root
}

func runLsOn(t *testing.T, path string, limit, maxDepth int) tooldef.Result {
	t.Helper()
	raw, err := json.Marshal(lsInput{Path: path, Limit: limit, MaxDepth: maxDepth})
	require.NoError(t, err)
	out, err := runLs(t.Context(), raw)
	require.NoError(t, err)
	return out
}

// oracle: stdlib os.ReadDir reference over the fixture tree — independent
// traversal, hand-written hidden/cache skip rule, case-insensitive ordering,
// and an ancestor-chain renderer must reproduce the tool's tree byte for
// byte, including depth-pruned directory leaves.
func TestLs_TreeMatchesReadDirOracle_Differential(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		root := writeLsFixture(t)
		t.Chdir(root)
		tree := filepath.Join(root, "tree")

		fileCount := 0
		want := oracleRender("tree", oracleBuildTree(t, tree, &fileCount, 500, 0, 3))
		require.Equal(t, 5, fileCount, "five files are visible within depth 3")

		got := runLsOn(t, "tree", 0, 0)
		assert.Equal(t, want, got.Content)
		assert.Equal(t, "tree", got.Detail)
		assert.Contains(t, want, "SubSub/", "depth-3 directory is a leaf")
		assert.NotContains(t, want, "x.txt", "depth-4 file is pruned")
		assert.NotContains(t, want, ".hidden", "hidden files are skipped")
		assert.NotContains(t, want, "dep.js", "cache directories are skipped")
	})

	t.Run("max depth 1 lists directories as leaves", func(t *testing.T) {
		root := writeLsFixture(t)
		t.Chdir(root)
		tree := filepath.Join(root, "tree")

		fileCount := 0
		want := oracleRender("tree", oracleBuildTree(t, tree, &fileCount, 500, 0, 1))

		got := runLsOn(t, "tree", 0, 1)
		assert.Equal(t, want, got.Content)
		assert.NotContains(t, want, "note.md", "no expansion below depth 1")
	})
}

// writeLsCountFixture builds a tree whose case-insensitive entry order is
// Aardvark, apple.txt, banana.txt, Cherry, date.txt — interleaving files and
// directories so the naive counter must recurse before counting siblings.
func writeLsCountFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	tree := filepath.Join(root, "tree")
	for _, d := range []string{"Aardvark", "Cherry"} {
		require.NoError(t, os.MkdirAll(filepath.Join(tree, d), 0o755))
	}
	files := map[string]string{
		filepath.Join(tree, "Aardvark", "f1.txt"): "1",
		filepath.Join(tree, "Aardvark", "f2.txt"): "2",
		filepath.Join(tree, "apple.txt"):          "a",
		filepath.Join(tree, "banana.txt"):         "b",
		filepath.Join(tree, "Cherry", "c1.txt"):   "c",
		filepath.Join(tree, "date.txt"):           "d",
	}
	for p, content := range files {
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	return root
}

// oracle: naive file-count arithmetic on the same fixture — only files count
// toward the budget, counting stops mid-sibling-list once it is spent, and
// the truncation banner fires exactly when the count reaches the limit. The
// expected tree comes from the os.ReadDir reference above.
func TestLs_TruncationMatchesNaiveCount_Differential(t *testing.T) {
	root := writeLsCountFixture(t)
	t.Chdir(root)
	tree := filepath.Join(root, "tree")

	fileCount := 0
	wantTree := oracleRender("tree", oracleBuildTree(t, tree, &fileCount, 3, 0, 3))
	require.Equal(t, 3, fileCount, "exactly three files fit in the budget")
	require.NotContains(t, wantTree, "banana.txt", "counting stops before later siblings")

	got := runLsOn(t, "tree", 3, 0)
	want := fmt.Sprintf(truncatedMessage, 3) + wantTree
	assert.Equal(t, want, got.Content)
	assert.Equal(t, "tree", got.Detail)

	// A budget larger than the visible file count: same full tree, no banner.
	got = runLsOn(t, "tree", 7, 0)
	fileCount = 0
	noBanner := oracleRender("tree", oracleBuildTree(t, tree, &fileCount, 7, 0, 3))
	require.Equal(t, 6, fileCount, "six files are visible in total")
	assert.Equal(t, noBanner, got.Content)
	assert.NotContains(t, got.Content, "Tree truncated")
}
