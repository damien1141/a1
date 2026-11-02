package session

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// benchSink prevents the compiler from eliminating benchmarked calls.
var benchSink []MessageEntry

func BenchmarkBuildSessionContext(b *testing.B) {
	type scenario struct {
		name    string
		entries []MessageEntry
		leafID  string
	}
	var scenarios []scenario
	add := func(name string, entries []MessageEntry, leafID string) {
		scenarios = append(scenarios, scenario{name: name, entries: entries, leafID: leafID})
	}
	{
		entries, leaf := linearMessages(2000)
		add("noCompaction", entries, leaf)
	}
	{
		entries, leaf := compactedHistory()
		add("compactedHistory", entries, leaf)
	}
	{
		entries, leaf := forkedShortBranch()
		add("forkedShortBranch", entries, leaf)
	}

	for _, sc := range scenarios {
		byID := idMap(sc.entries)
		// Sanity check that the scenario exercises both implementations
		// identically before timing anything.
		require.Equal(b, contextIDs(referenceBuildSessionContext(sc.entries, sc.leafID, byID)),
			contextIDs(buildSessionContext(sc.entries, sc.leafID, byID)))

		b.Run(sc.name+"/optimized", func(b *testing.B) {
			for range b.N {
				benchSink = buildSessionContext(sc.entries, sc.leafID, byID)
			}
		})
		b.Run(sc.name+"/reference", func(b *testing.B) {
			for range b.N {
				benchSink = referenceBuildSessionContext(sc.entries, sc.leafID, byID)
			}
		})
	}
}

// linearMessages returns a root-first chain of n messages and the leaf ID.
func linearMessages(n int) ([]MessageEntry, string) {
	entries := make([]MessageEntry, 0, n)
	var parent *string
	for i := range n {
		id := fmt.Sprintf("m%d", i)
		entry := testMessageEntry(id, parent)
		entries = append(entries, entry)
		parent = &id
	}
	return entries, entries[len(entries)-1].GetID()
}

// compactedHistory returns a branch with five fully summarized rounds of
// history below the short retained tail of the newest compaction round.
func compactedHistory() ([]MessageEntry, string) {
	var (
		entries []MessageEntry
		parent  *string
		count   int
	)
	add := func(entry MessageEntry) {
		entries = append(entries, entry)
		id := entry.GetID()
		parent = &id
	}
	for range 5 {
		for range 1000 {
			add(testMessageEntry(fmt.Sprintf("old%d", count), parent))
			count++
		}
		add(testCompactionEntry(fmt.Sprintf("round%d", count), parent, fmt.Sprintf("old%d", count-1000)))
	}
	for i := range 20 {
		add(testMessageEntry(fmt.Sprintf("kept%d", i), parent))
	}
	add(testCompactionEntry("cnew", parent, "kept0"))
	for i := range 30 {
		add(testMessageEntry(fmt.Sprintf("post%d", i), parent))
	}
	return entries, entries[len(entries)-1].GetID()
}

// forkedShortBranch returns many entries on other branches plus a short
// current branch, mirroring a session that forked after heavy use.
func forkedShortBranch() ([]MessageEntry, string) {
	entries, _ := linearMessages(10000)
	var parent *string
	for i := range 30 {
		id := fmt.Sprintf("b%d", i)
		entry := testMessageEntry(id, parent)
		entries = append(entries, entry)
		parent = &id
	}
	return entries, entries[len(entries)-1].GetID()
}
