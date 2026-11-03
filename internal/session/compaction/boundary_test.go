package compaction

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
)

// fakeClassifier scripts the decision model for the selection logic: it
// records the candidates it was offered and answers from a canned clean
// set or error.
type fakeClassifier struct {
	clean map[int]bool
	err   error
	got   []BoundaryCandidate
}

func (f *fakeClassifier) ClassifyBoundaries(
	_ context.Context,
	candidates []BoundaryCandidate,
) (map[int]bool, error) {
	f.got = candidates
	if f.err != nil {
		return nil, f.err
	}
	return f.clean, nil
}

// boundaryFixture builds a session whose positional cut lands mid-turn:
//
//	index: 0    1    2    3    4    5    6    7
//	       u1   a1   u2   a2   u3   a3   u4   a4
//	tokens: 10   50   10   10   10   40   10   10
//
// With keepRecentTokens=25 the backward budget walk reaches the budget
// inside a3, so the positional cut is index 5 (assistant, mid-turn of the
// turn that u3 opens). Candidates within the ±3 window after constraint
// filters (maxKept=100, floor=25/2): u2(2), a2(3), u3(4), u4(6) — a4(7)
// keeps only 10 tokens, under the floor.
func boundaryFixture() ([]session.MessageEntry, Settings) {
	return []session.MessageEntry{
		msgEntry("u1", llm.RoleUser, 10, 0),
		msgEntry("a1", llm.RoleAssistant, 50, 0),
		msgEntry("u2", llm.RoleUser, 10, 0),
		msgEntry("a2", llm.RoleAssistant, 10, 0),
		msgEntry("u3", llm.RoleUser, 10, 0),
		msgEntry("a3", llm.RoleAssistant, 40, 0),
		msgEntry("u4", llm.RoleUser, 10, 0),
		msgEntry("a4", llm.RoleAssistant, 10, 0),
	}, Settings{keepRecentTokens: 25, maxKeptTokens: 100}
}

// summarizeIDs returns the content of each summarized message. The fixture
// content is sized x-runs, so messages are identified by length.
func summarizeIDs(prep *CompactionPreparation) []int {
	var out []int
	for _, m := range prep.MessagesToSummarize {
		out = append(out, len(m.Content)/4)
	}
	return out
}

func TestRefineCutPoint_MovesToCleanBoundary(t *testing.T) {
	entries, settings := boundaryFixture()
	clf := &fakeClassifier{clean: map[int]bool{4: true}} // u3, a turn boundary

	prep, err := PrepareCompact(
		t.Context(),
		entries,
		settings.WithBoundaryClassifier(clf),
	)
	require.NoError(t, err)

	// The cut moved to u3: a user message, so no mid-turn split and no
	// turn-prefix summary — the whole [u3..] turn stays kept, and the
	// summarizer sees the complete turns before it.
	assert.Equal(t, "u3", prep.FirstKeptEntryId)
	assert.False(t, prep.IsMidTurnCut)
	assert.Empty(t, prep.TurnPrefixMessages)
	assert.Equal(t, []int{10, 50, 10, 10}, summarizeIDs(prep))

	// The constraint filters held: a4 (index 7) was never offered — its
	// kept zone is under half the recent budget.
	offered := make([]int, 0, len(clf.got))
	for _, cand := range clf.got {
		offered = append(offered, cand.Index)
	}
	assert.Equal(t, []int{2, 3, 4, 6}, offered)

	// The classifier saw the roles around the clean boundary.
	for _, cand := range clf.got {
		if cand.Index == 4 {
			assert.Equal(t, llm.RoleAssistant, cand.PrecedingRole)
			assert.Equal(t, llm.RoleUser, cand.FollowingRole)
			assert.NotEmpty(t, cand.Preceding)
			assert.NotEmpty(t, cand.Following)
		}
	}
}

func TestRefineCutPoint_ClassifierErrorFallsBackToPositional(t *testing.T) {
	entries, settings := boundaryFixture()
	clf := &fakeClassifier{err: errors.New("provider down")}

	prep, err := PrepareCompact(
		t.Context(),
		entries,
		settings.WithBoundaryClassifier(clf),
	)
	require.NoError(t, err)

	// Positional cut: a3, mid-turn, with the orphaned prefix to summarize.
	assert.Equal(t, "a3", prep.FirstKeptEntryId)
	assert.True(t, prep.IsMidTurnCut)
	require.Len(t, prep.TurnPrefixMessages, 1) // u3: the turn's user message
	assert.Len(t, prep.TurnPrefixMessages[0].Content, 40)
	assert.Equal(t, []int{10, 50, 10, 10}, summarizeIDs(prep))
}

func TestRefineCutPoint_NoCleanCandidateFallsBack(t *testing.T) {
	entries, settings := boundaryFixture()
	clf := &fakeClassifier{clean: map[int]bool{}}

	prep, err := PrepareCompact(
		t.Context(),
		entries,
		settings.WithBoundaryClassifier(clf),
	)
	require.NoError(t, err)
	assert.Equal(t, "a3", prep.FirstKeptEntryId)
	assert.True(t, prep.IsMidTurnCut)
}

func TestRefineCutPoint_UnknownCleanIndexIgnored(t *testing.T) {
	entries, settings := boundaryFixture()
	// A classifier that answers with an index never offered must not move
	// the cut anywhere.
	clf := &fakeClassifier{clean: map[int]bool{0: true}}

	prep, err := PrepareCompact(
		t.Context(),
		entries,
		settings.WithBoundaryClassifier(clf),
	)
	require.NoError(t, err)
	assert.Equal(t, "a3", prep.FirstKeptEntryId)
}

func TestRefineCutPoint_NearestCleanBoundaryWins(t *testing.T) {
	entries, settings := boundaryFixture()
	clf := &fakeClassifier{clean: map[int]bool{2: true, 4: true}}

	prep, err := PrepareCompact(
		t.Context(),
		entries,
		settings.WithBoundaryClassifier(clf),
	)
	require.NoError(t, err)
	// u2 is 3 boundaries away, u3 is 1: the nearest clean boundary wins.
	assert.Equal(t, "u3", prep.FirstKeptEntryId)
}

func TestRefineCutPoint_TiePrefersOlderBoundary(t *testing.T) {
	entries, settings := boundaryFixture()
	// u3 (index 4) and u4 (index 6) are both one boundary from the
	// positional cut (index 5); the older one keeps more context.
	clf := &fakeClassifier{clean: map[int]bool{4: true, 6: true}}

	prep, err := PrepareCompact(
		t.Context(),
		entries,
		settings.WithBoundaryClassifier(clf),
	)
	require.NoError(t, err)
	assert.Equal(t, "u3", prep.FirstKeptEntryId)
}

func TestRefineCutPoint_CeilingFiltersOlderCandidates(t *testing.T) {
	entries, settings := boundaryFixture()
	settings.maxKeptTokens = 75 // u2 (kept 90) and a2 (kept 80) now exceed it
	clf := &fakeClassifier{clean: map[int]bool{4: true}}

	prep, err := PrepareCompact(
		t.Context(),
		entries,
		settings.WithBoundaryClassifier(clf),
	)
	require.NoError(t, err)

	offered := make([]int, 0, len(clf.got))
	for _, cand := range clf.got {
		offered = append(offered, cand.Index)
	}
	assert.Equal(t, []int{4, 6}, offered)
	assert.Equal(t, "u3", prep.FirstKeptEntryId)
}

func TestRefineCutPoint_NoClassifierKeepsPositionalCut(t *testing.T) {
	entries, settings := boundaryFixture()

	prep, err := PrepareCompact(t.Context(), entries, settings)
	require.NoError(t, err)
	assert.Equal(t, "a3", prep.FirstKeptEntryId)
	assert.True(t, prep.IsMidTurnCut)
}
