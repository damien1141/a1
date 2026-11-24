package webserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/memstore"
)

// seedMemories writes n distinct memories into a fresh store and returns it.
func seedMemories(t *testing.T, contents ...string) *memstore.Store {
	t.Helper()
	store, err := memstore.Open(t.TempDir()+"/mem.db", memstore.FakeEmbedder{DimCount: 64}, memstore.DefaultParams())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	for _, c := range contents {
		_, _, err := store.Insert(context.Background(), memstore.NewTrace{
			Content: c, Kind: "fact", Scope: "user", Salience: 0.5,
		})
		require.NoError(t, err)
	}
	return store
}

// TestMemstoreList_Positive: List returns every active memory, newest first,
// with the ids Forget/Revise take. This is the read the persona panel was
// missing — before it, the only way to enumerate was Retrieve (activation
// ranked, query-shaped), so a UI had nothing to list.
func TestMemstoreList_Positive(t *testing.T) {
	store := seedMemories(t, "alpha fact", "beta fact", "gamma fact")

	got, err := store.List(0)
	require.NoError(t, err)
	require.Len(t, got, 3)

	// Newest-first: gamma was inserted last.
	assert.Equal(t, "gamma fact", got[0].Content)
	assert.Equal(t, "alpha fact", got[2].Content)
	for _, tr := range got {
		assert.NotZero(t, tr.ID, "every row must carry the id the edit/delete verbs take")
	}
}

// TestMemstoreList_LimitAndDelete_Negative: the limit is honoured, and Forget
// (which the DELETE verb calls) removes the row so List stops returning it.
func TestMemstoreList_LimitAndDelete_Negative(t *testing.T) {
	store := seedMemories(t, "one", "two", "three")

	limited, err := store.List(2)
	require.NoError(t, err)
	assert.Len(t, limited, 2, "limit must cap the result set")

	victim := limited[0].ID
	require.NoError(t, store.Forget(victim))

	after, err := store.List(0)
	require.NoError(t, err)
	assert.Len(t, after, 2)
	for _, tr := range after {
		assert.NotEqual(t, victim, tr.ID, "a forgotten memory must not come back")
	}
}

// TestMemstoreList_ReviseDifferential: the edit path (Revise) must leave List
// showing the new text — the oracle is the caller's own input, so an edit that
// silently fails to persist would flip this.
func TestMemstoreList_ReviseDifferential(t *testing.T) {
	const before = "prefers dark mode"
	const after = "prefers light mode, always"

	store := seedMemories(t, before)
	rows, err := store.List(0)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.NoError(t, store.Revise(context.Background(), rows[0].ID, after, "test"))

	updated, err := store.List(0)
	require.NoError(t, err)
	require.Len(t, updated, 1)
	assert.Equal(t, after, updated[0].Content, "List must reflect the revision")
	assert.NotEqual(t, before, updated[0].Content)
}

// TestHandlePersonaMemories_Edit_RejectsEmpty_Negative pins the PUT guard:
// an empty content would be a delete wearing an edit's clothes.
func TestHandlePersonaMemories_Edit_RejectsEmpty_Negative(t *testing.T) {
	s, store := avatarTestServer(t)
	p, err := store.Create("mem", "", "")
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]any{"id": p.ID, "mid": 1, "content": "   "})
	rec := httptest.NewRecorder()
	s.handlePersonaMemories(rec, httptest.NewRequest(http.MethodPut, "/api/personas/memories", bytes.NewReader(body)))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "content is required")
}

// TestPersonaListBadge_MatchesPanel_Differential: the count in the persona
// LIST response must equal the number of rows the memory panel returns for the
// same persona. They used to read two different tables (the JSONL trace bank
// vs. the semantic store), so a persona could show "0 memories" above a list of
// three. The oracle is the panel's own GET body, so the badge is checked at the
// wire the UI actually reads — not against a helper that could drift from it.
func TestPersonaListBadge_MatchesPanel_Differential(t *testing.T) {
	s, store := avatarTestServer(t)
	p, err := store.Create("counter", "", "")
	require.NoError(t, err)

	// Seed through the store the panel reads, so there is something to count.
	sem := s.personaSemanticStore(p)
	require.NotNil(t, sem, "the test server must expose a semantic store")
	seedMemoriesInto(t, sem, "likes tea", "works late", "prefers tabs")

	// The list endpoint's badge...
	listRec := httptest.NewRecorder()
	s.handleListPersonas(listRec, httptest.NewRequest(http.MethodGet, "/api/personas", nil))
	require.Equal(t, http.StatusOK, listRec.Code, listRec.Body.String())
	var list personaListResponse
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &list))
	var badge int
	for _, got := range list.Personas {
		if got.ID == p.ID {
			badge = got.MemoryCount
		}
	}

	// ...must equal the panel's row count...
	panelRec := httptest.NewRecorder()
	s.handlePersonaMemories(panelRec, httptest.NewRequest(http.MethodGet, "/api/personas/memories?id="+p.ID, nil))
	require.Equal(t, http.StatusOK, panelRec.Code, panelRec.Body.String())
	var panel struct {
		Memories []memorySummaryDTO `json:"memories"`
	}
	require.NoError(t, json.Unmarshal(panelRec.Body.Bytes(), &panel))

	assert.Equal(t, len(panel.Memories), badge,
		"the list badge must equal the panel's row count — one source, one number")
	assert.Equal(t, 3, badge, "both must see the three seeded memories")
}

// TestPersonaMemoryCount_EmptyStore_Negative: nothing remembered means zero, not
// an error and not the trace-mirror's inflated figure.
func TestPersonaMemoryCount_EmptyStore_Negative(t *testing.T) {
	s, store := avatarTestServer(t)
	p, err := store.Create("blank", "", "")
	require.NoError(t, err)
	assert.Equal(t, 0, s.personaMemoryCount(p))
}

// seedMemoriesInto writes facts into an already-open store.
func seedMemoriesInto(t *testing.T, store *memstore.Store, contents ...string) {
	t.Helper()
	for _, c := range contents {
		_, _, err := store.Insert(context.Background(), memstore.NewTrace{
			Content: c, Kind: "fact", Scope: "user", Salience: 0.5,
		})
		require.NoError(t, err)
	}
}
