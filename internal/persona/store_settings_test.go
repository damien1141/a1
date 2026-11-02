package persona

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SetSettings is the editor-facing settings writer (pointer semantics:
// non-nil sets EXACTLY the given value — including "" to clear an
// override — nil leaves the field untouched). UpdateSettings, by
// contrast, treats "" as "keep stored" and cannot express clearing.

func TestSetSettings_SetsAllThree(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "personas"))
	p, err := store.Create("worker", "work persona", "")
	require.NoError(t, err)

	model := "gpt-oss-27b"
	mode := "readonly"
	on := true
	_, err = store.SetSettings(p.ID, &model, &mode, &on)
	require.NoError(t, err)

	got, err := store.Get(p.ID)
	require.NoError(t, err)
	assert.Equal(t, "gpt-oss-27b", got.Model)
	assert.Equal(t, "readonly", got.PermissionMode)
	require.NotNil(t, got.MemoryEnabled)
	assert.True(t, *got.MemoryEnabled)
}

func TestSetSettings_ClearsOverridesWithEmptyString(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "personas"))
	p, err := store.Create("worker", "work persona", "")
	require.NoError(t, err)

	model := "gpt-oss-27b"
	mode := "readonly"
	_, err = store.SetSettings(p.ID, &model, &mode, nil)
	require.NoError(t, err)

	// Clear both string overrides; the memory pointer stays untouched.
	empty := ""
	_, err = store.SetSettings(p.ID, &empty, &empty, nil)
	require.NoError(t, err)

	got, err := store.Get(p.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Model, "explicit empty string must clear the model override")
	assert.Empty(t, got.PermissionMode, "explicit empty string must clear the mode override")
}

func TestSetSettings_NilLeavesFieldsUntouched(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "personas"))
	p, err := store.Create("worker", "work persona", "")
	require.NoError(t, err)

	model := "gpt-oss-27b"
	off := false
	_, err = store.SetSettings(p.ID, &model, nil, &off)
	require.NoError(t, err)

	// A nil model pointer must not clobber the stored model even when
	// another field changes in the same call.
	newMode := "autopilot"
	_, err = store.SetSettings(p.ID, nil, &newMode, nil)
	require.NoError(t, err)

	got, err := store.Get(p.ID)
	require.NoError(t, err)
	assert.Equal(t, "gpt-oss-27b", got.Model, "nil pointer must keep the stored model")
	assert.Equal(t, "autopilot", got.PermissionMode)
	require.NotNil(t, got.MemoryEnabled)
	assert.False(t, *got.MemoryEnabled)
}

func TestSetSettings_NotFound(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "personas"))
	model := "x"
	_, err := store.SetSettings("no-such-id", &model, nil, nil)
	require.Error(t, err)
}

// TestClearMemoryEnabled pins the tri-state escape hatch: SetSettings can
// set memory_enabled exactly (on or off) but cannot express "remove the
// override" — JSON null and absent both unmarshal to a nil *bool — so
// ClearMemoryEnabled reverts the persona to inheriting the global toggle.
func TestClearMemoryEnabled(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	p, err := s.Create("worker", "work persona", "")
	require.NoError(t, err)

	// No override to start with.
	got, err := s.Get(p.ID)
	require.NoError(t, err)
	require.Nil(t, got.MemoryEnabled)

	// Set it off, then on — the pointer contract.
	off := false
	_, err = s.SetSettings(p.ID, nil, nil, &off)
	require.NoError(t, err)
	got, _ = s.Get(p.ID)
	require.NotNil(t, got.MemoryEnabled)
	assert.False(t, *got.MemoryEnabled)

	// Clear: back to inherit, and the clear survives a reload from disk.
	cleared, err := s.ClearMemoryEnabled(p.ID)
	require.NoError(t, err)
	assert.Nil(t, cleared.MemoryEnabled)
	got, _ = s.Get(p.ID)
	assert.Nil(t, got.MemoryEnabled)

	fresh := NewStore(dir)
	got, err = fresh.Get(p.ID)
	require.NoError(t, err)
	assert.Nil(t, got.MemoryEnabled,
		"the cleared override must persist as inherit across store reloads")

	// Unknown persona is an error, not a silent no-op.
	_, err = s.ClearMemoryEnabled("no-such-persona")
	require.Error(t, err)
}
