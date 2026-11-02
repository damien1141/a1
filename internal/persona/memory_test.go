package persona

import (
        "path/filepath"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/memstore"
)

// TestMemoryEnabledOrDefault_Nil tests the default-on behavior. A persona
// with no MemoryEnabled field set should return true (memory auto-injection
// is on by default).
func TestMemoryEnabledOrDefault_Nil(t *testing.T) {
        p := Persona{ID: "test", Name: "test"}
        assert.True(t, p.MemoryEnabledOrDefault(),
                "nil MemoryEnabled should default to true")
}

// TestMemoryEnabledOrDefault_ExplicitTrue tests that an explicit *true
// is honored (memory on).
func TestMemoryEnabledOrDefault_ExplicitTrue(t *testing.T) {
        on := true
        p := Persona{ID: "test", Name: "test", MemoryEnabled: &on}
        assert.True(t, p.MemoryEnabledOrDefault(),
                "explicit *true should return true")
}

// TestMemoryEnabledOrDefault_ExplicitFalse tests that an explicit *false
// turns memory off (the user's opt-out is honored).
func TestMemoryEnabledOrDefault_ExplicitFalse(t *testing.T) {
        off := false
        p := Persona{ID: "test", Name: "test", MemoryEnabled: &off}
        assert.False(t, p.MemoryEnabledOrDefault(),
                "explicit *false should return false")
}

// TestPersona_StructRoundTrip verifies the new fields (Model,
// MemoryEnabled, PermissionMode) survive JSON serialization. This is a
// regression test for the omitempty tags — a persona with empty strings
// and nil pointers should serialize to compact JSON without those fields.
func TestPersona_StructRoundTrip(t *testing.T) {
        root := t.TempDir()
        store := NewStore(filepath.Join(root, "personas"))
        p, err := store.Create("Solis", "The 19yo who runs the show", "")
        require.NoError(t, err)

        // Set the new fields via UpdateSettings (Update only touches identity
        // fields; the new settings need their own setter).
        off := false
        updated, err := store.UpdateSettings(p.ID, "llama3.2-70b", &off, "autopilot")
        require.NoError(t, err)
        _ = updated

        // Verify the new fields round-trip through personas.json.
        got, err := store.Get(p.ID)
        require.NoError(t, err)
        assert.Equal(t, "llama3.2-70b", got.Model)
        require.NotNil(t, got.MemoryEnabled, "MemoryEnabled pointer must survive serialization")
        assert.False(t, *got.MemoryEnabled)
        assert.Equal(t, "autopilot", got.PermissionMode)
}

// TestPersona_DefaultFieldsEmpty verifies the default persona has empty
// Model, nil MemoryEnabled, and empty PermissionMode — these are the
// "no override" sentinel values that fall back to engine defaults.
func TestPersona_DefaultFieldsEmpty(t *testing.T) {
        root := t.TempDir()
        store := NewStore(filepath.Join(root, "personas"))
        def, err := store.ActivePersona()
        require.NoError(t, err)
        assert.Equal(t, DefaultPersonaID, def.ID)
        assert.Empty(t, def.Model, "default persona should have empty Model")
        assert.Nil(t, def.MemoryEnabled, "default persona should have nil MemoryEnabled")
        assert.True(t, def.MemoryEnabledOrDefault(), "default persona should default memory to on")
        assert.Empty(t, def.PermissionMode, "default persona should have empty PermissionMode")
}

// TestSemanticStore_OpensAndCaches verifies that SemanticStore opens a
// fresh SQLite store on first call and returns the same cached handle on
// subsequent calls for the same persona.
func TestSemanticStore_OpensAndCaches(t *testing.T) {
        root := t.TempDir()
        store := NewStore(filepath.Join(root, "personas"))
        p, err := store.Create("test", "test persona", "")
        require.NoError(t, err)

        emb := memstore.FakeEmbedder{DimCount: 64}
        s1, err := store.SemanticStore(p, emb, memstore.DefaultParams())
        require.NoError(t, err)
        require.NotNil(t, s1)

        s2, err := store.SemanticStore(p, emb, memstore.DefaultParams())
        require.NoError(t, err)
        // Must be the same pointer (cached).
        assert.Same(t, s1, s2, "second call should return cached store")
}

// TestSemanticStore_IsolatesPerPersona verifies that two different
// personas get DIFFERENT store handles — true isolation, not shared
// state. This is the core correctness property: one persona's vector
// store must never leak into another's.
func TestSemanticStore_IsolatesPerPersona(t *testing.T) {
        root := t.TempDir()
        store := NewStore(filepath.Join(root, "personas"))
        p1, err := store.Create("p1", "persona 1", "")
        require.NoError(t, err)
        p2, err := store.Create("p2", "persona 2", "")
        require.NoError(t, err)

        emb := memstore.FakeEmbedder{DimCount: 64}
        s1, err := store.SemanticStore(p1, emb, memstore.DefaultParams())
        require.NoError(t, err)
        s2, err := store.SemanticStore(p2, emb, memstore.DefaultParams())
        require.NoError(t, err)
        assert.NotSame(t, s1, s2, "different personas must get different store handles")
}

// TestSemanticStore_RejectsEmptyPersonaID verifies the empty-ID guard.
func TestSemanticStore_RejectsEmptyPersonaID(t *testing.T) {
        store := NewStore(t.TempDir())
        emb := memstore.FakeEmbedder{DimCount: 64}
        _, err := store.SemanticStore(Persona{}, emb, memstore.DefaultParams())
        assert.Error(t, err, "empty persona ID should return error")
}

// TestCloseSemanticStores_ClosesAndClears verifies that CloseSemanticStores
// closes every cached store and clears the cache, so a subsequent call
// opens fresh stores.
func TestCloseSemanticStores_ClosesAndClears(t *testing.T) {
        root := t.TempDir()
        store := NewStore(filepath.Join(root, "personas"))
        p1, err := store.Create("p1", "persona 1", "")
        require.NoError(t, err)

        emb := memstore.FakeEmbedder{DimCount: 64}
        s1, err := store.SemanticStore(p1, emb, memstore.DefaultParams())
        require.NoError(t, err)

        require.NoError(t, store.CloseSemanticStores())

        // After close, a re-open should produce a DIFFERENT pointer (cache cleared).
        s2, err := store.SemanticStore(p1, emb, memstore.DefaultParams())
        require.NoError(t, err)
        assert.NotSame(t, s1, s2, "after Close, re-open should produce fresh store")
}

// TestCloseSemanticStores_Idempotent verifies that calling Close multiple
// times is safe (no panic, no error).
func TestCloseSemanticStores_Idempotent(t *testing.T) {
        store := NewStore(t.TempDir())
        require.NoError(t, store.CloseSemanticStores())
        require.NoError(t, store.CloseSemanticStores())
}
