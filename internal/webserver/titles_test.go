package webserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The naming-attempt marker must be in-memory: persisting it (the original
// implementation) let one failed call — nil ops client, a single 429 —
// permanently suppress smart naming for a session, surviving restarts,
// for a purely cosmetic label.
func TestSessionTitlesAttemptedInMemory(t *testing.T) {
	dir := t.TempDir()
	store := newSessionTitles(dir)

	store.markAttempted("s1")
	assert.True(t, store.attemptedHas("s1"))

	// A fresh store over the same dir (a server restart) has a clean slate.
	reopened := newSessionTitles(dir)
	assert.False(t, reopened.attemptedHas("s1"))

	// Setting a title clears the in-memory marker and writes only titles
	// to disk — no attempt bookkeeping leaks into titles.json.
	store.set("s1", "A Named Session")
	assert.False(t, store.attemptedHas("s1"))
	assert.Equal(t, "A Named Session", store.get("s1"))

	raw, err := os.ReadFile(filepath.Join(dir, "titles.json"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "A Named Session")
	assert.NotContains(t, string(raw), "attempted")
}

// An old titles.json carrying the persisted "attempted" key from the
// previous format must load cleanly (unknown field ignored) and not
// resurrect a marker into the new in-memory scheme.
func TestSessionTitlesLegacyFileLoads(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"titles":{"legacy":"Legacy Name"},"attempted":{"legacy":true,"other":true}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "titles.json"), []byte(legacy), 0o644))

	store := newSessionTitles(dir)
	assert.Equal(t, "Legacy Name", store.get("legacy"))
	assert.False(t, store.attemptedHas("legacy"))
}
