package tree_test

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/components/tree"
)

func TestPrefixForSiblings_HostileIndices_Negative(t *testing.T) {
	st := tree.DefaultStyle()
	for _, tc := range []struct {
		count, index int
	}{
		{0, 0}, {-3, 0}, {3, -1}, {3, 3}, {3, 100}, {0, -1},
	} {
		assert.Equal(t, "", tree.PrefixForSiblings(tc.count, tc.index, st),
			"count=%d index=%d must be rejected as the empty prefix", tc.count, tc.index)
	}
}

func TestFlatten_EmptyRoots_Negative(t *testing.T) {
	assert.Empty(t, tree.Flatten[int](nil), "nil roots flatten to nothing")
	assert.Empty(t, tree.Flatten[int]([]tree.Node[int]{}), "empty roots flatten to nothing")
}

func TestPrefix_DegenerateStyleNormalizes_Negative(t *testing.T) {
	// A zero-value Style normalizes to the documented default connectors
	// (│ ├ ╰ ─, indent 4), so it renders identically to DefaultStyle().
	last := tree.Flat[string]{Item: "x", Depth: 0, IsLast: true}
	assert.Equal(t, "╰── ", tree.Prefix(last, tree.Style{}))
	assert.Equal(t, tree.Prefix(last, tree.DefaultStyle()), tree.Prefix(last, tree.Style{}))

	// Indents below the documented minimum of 2 fall back to the default 4.
	for _, indent := range []int{0, 1, -9} {
		assert.Equal(t, "├── ", tree.Prefix(tree.Flat[string]{IsLast: false}, tree.Style{Indent: indent}),
			"indent %d", indent)
	}
	// A legal custom indent reshapes both the branch connector and the
	// ancestor padding: branch + (indent-2) horizontals + space, ancestor
	// either indent spaces or vertical + (indent-1) spaces.
	st3 := tree.Style{Indent: 3}
	assert.Equal(t, "├─ ", tree.Prefix(tree.Flat[string]{IsLast: false}, st3), "indent 3 branch")
	assert.Equal(t, "│  ├─ ", tree.Prefix(tree.Flat[string]{AncestorsAreLast: []bool{false}, IsLast: false}, st3),
		"indent 3 non-last ancestor")
	assert.Equal(t, "   ╰─ ", tree.Prefix(tree.Flat[string]{AncestorsAreLast: []bool{true}, IsLast: true}, st3),
		"indent 3 last ancestor")
}

// naiveFlattenOrder is an independent DFS: an explicit LIFO stack with
// children pushed in reverse (so pops yield document order), carrying each
// frame's depth, is-last flag, and an immutable copy of the ancestor path.
func naiveFlattenOrder[T any](roots []tree.Node[T]) []tree.Flat[T] {
	type frame struct {
		node      tree.Node[T]
		depth     int
		isLast    bool
		ancestors []bool
	}
	var out []tree.Flat[T]
	var stack []frame
	for i := len(roots) - 1; i >= 0; i-- {
		stack = append(stack, frame{node: roots[i], isLast: i == len(roots)-1})
	}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out = append(out, tree.Flat[T]{
			Item:             f.node.Item,
			Depth:            f.depth,
			IsLast:           f.isLast,
			AncestorsAreLast: append([]bool(nil), f.ancestors...),
		})
		kids := f.node.Children
		path := append(append([]bool(nil), f.ancestors...), f.isLast)
		for i := len(kids) - 1; i >= 0; i-- {
			stack = append(stack, frame{
				node:      kids[i],
				depth:     f.depth + 1,
				isLast:    i == len(kids)-1,
				ancestors: append([]bool(nil), path...),
			})
		}
	}
	return out
}

// oracle: iterative stack-based DFS reference (above) on hand-built and
// randomized trees; the recursive Flatten must agree on items, depths,
// is-last flags, and ancestor paths.
func TestFlatten_MatchesNaiveStackWalk_Differential(t *testing.T) {
	built := []tree.Node[string]{
		{
			Item: "a",
			Children: []tree.Node[string]{
				{Item: "a1", Children: []tree.Node[string]{{Item: "a1a"}, {Item: "a1b"}}},
				{Item: "a2"},
			},
		},
		{Item: "b"},
		{Item: "c", Children: []tree.Node[string]{{Item: "c1"}}},
	}

	rng := rand.New(rand.NewSource(13))
	var random func(depth int) []tree.Node[string]
	random = func(depth int) []tree.Node[string] {
		n := rng.Intn(4)
		if depth > 3 {
			n = 0
		}
		nodes := make([]tree.Node[string], n)
		for i := range n {
			nodes[i] = tree.Node[string]{Item: strconv.Itoa(depth) + "-" + strconv.Itoa(i)}
			nodes[i].Children = random(depth + 1)
		}
		return nodes
	}
	trees := [][]tree.Node[string]{built, nil, {}}
	for range 40 {
		trees = append(trees, random(0))
	}

	for i, roots := range trees {
		assert.Equal(t, naiveFlattenOrder(roots), tree.Flatten(roots), "tree %d", i)
	}
}

// naivePrefix assembles the connector string from the documented format with
// strings.Repeat: each ancestor contributes indent columns (all spaces when
// that ancestor was a last child, vertical + (indent-1) spaces otherwise),
// then the branch glyph, (indent-2) horizontals, and one trailing space.
func naivePrefix(ancestorsAreLast []bool, isLast bool, st tree.Style) string {
	vertical, tee, elbow, horizontal := st.Vertical, st.Tee, st.Elbow, st.Horizontal
	indent := st.Indent
	if vertical == "" {
		vertical = "│"
	}
	if tee == "" {
		tee = "├"
	}
	if elbow == "" {
		elbow = "╰"
	}
	if horizontal == "" {
		horizontal = "─"
	}
	if indent < 2 {
		indent = 4
	}
	var b strings.Builder
	for _, last := range ancestorsAreLast {
		if last {
			b.WriteString(strings.Repeat(" ", indent))
		} else {
			b.WriteString(vertical + strings.Repeat(" ", indent-1))
		}
	}
	branch := tee
	if isLast {
		branch = elbow
	}
	b.WriteString(branch + strings.Repeat(horizontal, indent-2) + " ")
	return b.String()
}

// oracle: naive connector assembly (above) on hand-derived edge cases and
// randomized flat rows × styles; Prefix must reproduce the hand-computed
// box-drawing strings byte for byte.
func TestPrefix_MatchesNaiveConnectors_Differential(t *testing.T) {
	styles := []tree.Style{
		tree.DefaultStyle(),
		{}, // zero value normalizes to defaults
		{Vertical: " ", Tee: "+", Elbow: "\\", Horizontal: "-", Indent: 2},
		{Vertical: "|", Tee: "]", Elbow: "L", Horizontal: "=", Indent: 6},
		{Indent: 3}, // sub-minimum indent → default 4
		{Vertical: "│", Tee: "├", Elbow: "╰", Horizontal: "─", Indent: 5},
	}

	rng := rand.New(rand.NewSource(17))
	for i := range 300 {
		st := styles[rng.Intn(len(styles))]
		depth := rng.Intn(5)
		ancestors := make([]bool, depth)
		for j := range ancestors {
			ancestors[j] = rng.Intn(2) == 0
		}
		f := tree.Flat[string]{
			Item:             strconv.Itoa(i),
			Depth:            depth,
			IsLast:           rng.Intn(2) == 0,
			AncestorsAreLast: ancestors,
		}
		assert.Equal(t, naivePrefix(ancestors, f.IsLast, st), tree.Prefix(f, st),
			"case %d: ancestors %v isLast %v style %#v", i, ancestors, f.IsLast, st)
	}

	// Hand-derived edges the random walk may under-sample.
	deep := tree.Flat[string]{AncestorsAreLast: []bool{true, true}, IsLast: true}
	assert.Equal(t, "        ╰── ", tree.Prefix(deep, tree.DefaultStyle()), "all-last chain")
	mixed := tree.Flat[string]{AncestorsAreLast: []bool{false, true}, IsLast: false}
	assert.Equal(t, "│       ├── ", tree.Prefix(mixed, tree.DefaultStyle()), "mixed chain")
}

// oracle: hand-written golden literal — the classic rendered tree for one
// fixed node shape, written out directly (prefix + item per line), never
// generated by Flatten/Prefix.
func TestTreeRender_MatchesGoldenLiteral_Differential(t *testing.T) {
	roots := []tree.Node[string]{
		{
			Item: "a",
			Children: []tree.Node[string]{
				{Item: "a1", Children: []tree.Node[string]{{Item: "a1a"}, {Item: "a1b"}}},
				{Item: "a2"},
			},
		},
		{Item: "b"},
	}
	var b strings.Builder
	for _, f := range tree.Flatten(roots) {
		b.WriteString(tree.Prefix(f, tree.DefaultStyle()))
		b.WriteString(f.Item)
		b.WriteString("\n")
	}
	want := "" +
		"├── a\n" +
		"│   ├── a1\n" +
		"│   │   ├── a1a\n" +
		"│   │   ╰── a1b\n" +
		"│   ╰── a2\n" +
		"╰── b\n"
	assert.Equal(t, want, b.String())
}
