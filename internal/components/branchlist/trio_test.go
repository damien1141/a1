package branchlist

import (
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components/listpicker"
	"github.com/damien1141/a1/internal/util/gitx"
)

func TestItemsEmptyAndDuplicateInputs_Negative(t *testing.T) {
	assert.Empty(t, Items(nil, nil), "no branches at all")
	assert.Empty(t, Items([]gitx.Branch{}, []string{"main"}), "recent names without branches map to nothing")
	assert.Empty(t, Items(nil, []string{"ghost", "", "origin/x"}))

	// A remote sharing a name with a local row: the first listing wins and
	// the second occurrence is dropped, never double-listed.
	dup := Items([]gitx.Branch{
		{Name: "dup", Current: true, Subject: "local"},
		{Name: "dup", Remote: true, Subject: "remote"},
	}, nil)
	require.Len(t, dup, 1)
	assert.Equal(t, "●", dup[0].Leading, "the current local row wins the name")
	assert.Equal(t, "local", dup[0].Detail)

	// Two rows claiming HEAD (git never allows it): both anchor the list, in
	// input order, and the rest still trails them.
	multi := Items([]gitx.Branch{
		{Name: "first", Current: true},
		{Name: "second", Current: true},
		{Name: "plain"},
	}, nil)
	assert.Equal(t, []string{"first", "second", "plain"}, primaries(multi))

	// The same recent name twice must not list the branch twice.
	repeat := Items([]gitx.Branch{{Name: "x", Subject: "s"}}, []string{"x", "x"})
	assert.Equal(t, []string{"x"}, primaries(repeat))
}

// naiveItemsOrder recomputes the display order by scoring, not scanning:
// every branch name gets the (pass, index) of its first appearance across the
// four passes (current / recent / local / remote), deduplicates by keeping
// the minimum tuple, then stable-sorts by that tuple.
func naiveItemsOrder(branches []gitx.Branch, recent []string) []gitx.Branch {
	byName := make(map[string]int, len(branches))
	for i, b := range branches {
		byName[b.Name] = i
	}
	type cand struct {
		pass, idx int
		b         gitx.Branch
	}
	var cands []cand
	for i, b := range branches {
		if b.Current {
			cands = append(cands, cand{0, i, b})
		}
	}
	for j, name := range recent {
		if i, ok := byName[name]; ok {
			cands = append(cands, cand{1, j, branches[i]})
		}
	}
	for i, b := range branches {
		if !b.Remote {
			cands = append(cands, cand{2, i, b})
		}
	}
	for i, b := range branches {
		if b.Remote {
			cands = append(cands, cand{3, i, b})
		}
	}
	best := make(map[string]cand, len(branches))
	for _, c := range cands {
		if prev, ok := best[c.b.Name]; !ok || c.pass < prev.pass || (c.pass == prev.pass && c.idx < prev.idx) {
			best[c.b.Name] = c
		}
	}
	uniq := make([]cand, 0, len(best))
	for _, c := range best {
		uniq = append(uniq, c)
	}
	sort.SliceStable(uniq, func(i, j int) bool {
		if uniq[i].pass != uniq[j].pass {
			return uniq[i].pass < uniq[j].pass
		}
		return uniq[i].idx < uniq[j].idx
	})
	out := make([]gitx.Branch, 0, len(uniq))
	for _, c := range uniq {
		out = append(out, c.b)
	}
	return out
}

// oracle: the scored-sort reference above on randomized branch sets and
// recent lists (including names that do not exist), plus gitx.LocalName as
// the known-good ID mapper for each surviving row.
func TestItemsMatchesScoredSortOracle_Differential(t *testing.T) {
	names := []string{"main", "dev", "fix/one", "origin/main", "origin/dev", "feature/x-y", "release"}
	rng := rand.New(rand.NewSource(5))
	for i := range 200 {
		branches := make([]gitx.Branch, rng.Intn(9))
		for j := range branches {
			branches[j] = gitx.Branch{
				Name:    names[rng.Intn(len(names))],
				Current: rng.Intn(6) == 0,
				Remote:  rng.Intn(3) == 0,
				Subject: []string{"wip", "chore: bump", ""}[rng.Intn(3)],
			}
		}
		recent := make([]string, rng.Intn(6))
		for j := range recent {
			if rng.Intn(4) == 0 {
				recent[j] = "deleted-branch" // must be ignored
				continue
			}
			recent[j] = names[rng.Intn(len(names))]
		}

		want := naiveItemsOrder(branches, recent)
		got := Items(branches, recent)
		require.Len(t, got, len(want), "case %d row count", i)
		for r := range want {
			b := want[r]
			assert.Equal(t, listpicker.Item{
				ID:       b.LocalName(),
				Leading:  map[bool]string{true: "●", false: ""}[b.Current],
				Primary:  b.Name,
				Detail:   b.Subject,
				Keywords: b.Name + " " + b.Subject,
			}, got[r], "case %d row %d", i, r)
		}
	}
}
