package tree_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components/tree"
)

func TestPrefixForSiblings(t *testing.T) {
	st := tree.DefaultStyle()
	require.Equal(t, "├── ", tree.PrefixForSiblings(3, 0, st), "first")
	require.Equal(t, "├── ", tree.PrefixForSiblings(3, 1, st), "mid")
	require.Equal(t, "╰── ", tree.PrefixForSiblings(3, 2, st), "last")
}

func TestFlattenNested(t *testing.T) {
	roots := []tree.Node[string]{
		{
			Item: "a",
			Children: []tree.Node[string]{
				{Item: "a1"},
				{Item: "a2"},
			},
		},
		{Item: "b"},
	}
	flat := tree.Flatten(roots)
	require.Len(t, flat, 4)
	// a2 under a: ancestor a is not last among roots? a is first of 2, so ancestor last=false
	// Wait: a2's parent walk: when walking a's children, ancestors = [isLast of a among roots] = [false]
	p := tree.Prefix(flat[2], tree.DefaultStyle()) // a2, last child of a
	require.Equal(t, "│   ╰── ", p, "nested last under non-last parent")
	pLast := tree.Prefix(flat[3], tree.DefaultStyle()) // b
	require.Equal(t, "╰── ", pLast, "root last")
}
