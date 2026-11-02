package agent

import (
        "path/filepath"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/permission"
)

// setupTestEngine builds a minimal Engine wired with a persona store,
// an embedder, and (optionally) a model resolver + permission mode
// setter. Returns the engine, the persona store, and pointers to the
// test's resolver/setter state so tests can assert they were called.
func setupTestEngine(t *testing.T) (*Engine, *persona.Store, *bool, *permission.Mode) {
        t.Helper()
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))

        sess, err := NewSession(WithPersonaStore(pstore))
        require.NoError(t, err)

        resolverCalled := false
        var capturedMode permission.Mode
        eng, err := NewEngine(llm.ModelConfig{Name: "default-model"}, sess,
                WithPersonaModelResolver(func(name string) (llm.ModelConfig, bool) {
                        resolverCalled = true
                        if name == "known-model" {
                                return llm.ModelConfig{Name: name, API: llm.OpenAI}, true
                        }
                        return llm.ModelConfig{}, false
                }),
                WithPermissionModeSetter(func(m permission.Mode) {
                        capturedMode = m
                }),
        )
        require.NoError(t, err)
        return eng, pstore, &resolverCalled, &capturedMode
}

// TestApplyPersonaSettings_AppliesModel verifies that switching to a
// persona with a non-empty Model field + a wired resolver updates
// engine.modelCfg to the resolved config.
func TestApplyPersonaSettings_AppliesModel(t *testing.T) {
        eng, pstore, resolverCalled, _ := setupTestEngine(t)
        p, err := pstore.Create("work", "work persona", "")
        require.NoError(t, err)
        _, err = pstore.UpdateSettings(p.ID, "known-model", nil, "")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        require.NoError(t, eng.SetPersona(p.ID))

        assert.True(t, *resolverCalled, "model resolver should have been called")
        assert.Equal(t, "known-model", eng.modelCfg.Name,
                "engine.modelCfg should be updated to the resolved model")
}

// TestApplyPersonaSettings_UnknownModelFallsBack verifies that when the
// resolver returns false (unknown model name), the engine keeps its
// previous modelCfg rather than breaking.
func TestApplyPersonaSettings_UnknownModelFallsBack(t *testing.T) {
        eng, pstore, _, _ := setupTestEngine(t)
        p, err := pstore.Create("broken", "persona with bad model", "")
        require.NoError(t, err)
        _, err = pstore.UpdateSettings(p.ID, "nonexistent-model", nil, "")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        originalModel := eng.modelCfg.Name
        require.NoError(t, eng.SetPersona(p.ID))
        assert.Equal(t, originalModel, eng.modelCfg.Name,
                "engine should keep previous model when resolver returns false")
}

// TestApplyPersonaSettings_AppliesPermissionMode verifies that switching
// to a persona with a non-empty PermissionMode field + a wired setter
// invokes the setter with the configured mode.
func TestApplyPersonaSettings_AppliesPermissionMode(t *testing.T) {
        eng, pstore, _, capturedMode := setupTestEngine(t)
        p, err := pstore.Create("autopilot", "autopilot persona", "")
        require.NoError(t, err)
        _, err = pstore.UpdateSettings(p.ID, "", nil, "autopilot")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        require.NoError(t, eng.SetPersona(p.ID))
        assert.Equal(t, permission.ModeAutopilot, *capturedMode,
                "permission mode setter should be called with autopilot")
}

// TestApplyPersonaSettings_ClearsMemoryInlet verifies that the stale
// memoryInletMsg from the previous persona is cleared on switch (the new
// persona has its own semantic memory namespace; the old injection is
// invalid).
func TestApplyPersonaSettings_ClearsMemoryInlet(t *testing.T) {
        eng, pstore, _, _ := setupTestEngine(t)
        p, err := pstore.Create("switch", "switch persona", "")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        // Simulate a previous turn having set memoryInletMsg.
        eng.memoryInletMsg = &llm.Message{
                Role:    llm.RoleSystem,
                Content: "stale memory from old persona",
        }
        require.NotNil(t, eng.memoryInletMsg, "inlet must be set before switch")

        require.NoError(t, eng.SetPersona(p.ID))
        assert.Nil(t, eng.memoryInletMsg,
                "memoryInletMsg must be cleared on persona switch")
}

// TestApplyPersonaSettings_DefaultPersonaSkips verifies that switching
// back to the default persona does NOT call the resolver or setter (the
// default has no Model or PermissionMode overrides — engine keeps its
// original config).
func TestApplyPersonaSettings_DefaultPersonaSkips(t *testing.T) {
        eng, pstore, resolverCalled, capturedMode := setupTestEngine(t)

        // Set up a non-default persona first, switch to it (so resolver + setter fire).
        p, err := pstore.Create("temp", "temp persona", "")
        require.NoError(t, err)
        _, err = pstore.UpdateSettings(p.ID, "known-model", nil, "readonly")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))
        require.NoError(t, eng.SetPersona(p.ID))

        // Reset the call trackers.
        *resolverCalled = false
        *capturedMode = ""

        // Switch back to default — resolver/setter should NOT fire.
        require.NoError(t, pstore.SetActive(persona.DefaultPersonaID))
        require.NoError(t, eng.SetPersona(persona.DefaultPersonaID))

        assert.False(t, *resolverCalled, "resolver should NOT fire on default persona")
        assert.Empty(t, string(*capturedMode), "setter should NOT fire on default persona")
}

// TestApplyPersonaSettings_NoResolverSkips verifies that when no resolver
// is wired (WithPersonaModelResolver not called), a persona with a Model
// field has no effect — graceful degradation, not a crash.
func TestApplyPersonaSettings_NoResolverSkips(t *testing.T) {
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))
        p, err := pstore.Create("no-resolver", "persona with no resolver", "")
        require.NoError(t, err)
        _, err = pstore.UpdateSettings(p.ID, "should-be-ignored", nil, "")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        sess, err := NewSession(WithPersonaStore(pstore))
        require.NoError(t, err)
        eng, err := NewEngine(llm.ModelConfig{Name: "default"}, sess) // no resolver
        require.NoError(t, err)

        originalModel := eng.modelCfg.Name
        require.NoError(t, eng.SetPersona(p.ID))
        assert.Equal(t, originalModel, eng.modelCfg.Name,
                "without resolver wired, persona.Model should be silently ignored")
}

// TestApplyPersonaSettings_NoSetterSkips verifies that when no permission
// mode setter is wired, a persona with a PermissionMode field has no
// effect — graceful degradation, not a crash.
func TestApplyPersonaSettings_NoSetterSkips(t *testing.T) {
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))
        p, err := pstore.Create("no-setter", "persona with no setter", "")
        require.NoError(t, err)
        _, err = pstore.UpdateSettings(p.ID, "", nil, "autopilot")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        sess, err := NewSession(WithPersonaStore(pstore))
        require.NoError(t, err)
        eng, err := NewEngine(llm.ModelConfig{Name: "default"}, sess) // no setter
        require.NoError(t, err)

        // Should not panic, should not error, should not call any setter
        // (there is none).
        require.NoError(t, eng.SetPersona(p.ID))
}

// TestApplyPersonaSettings_NoPersonaStoreNoOps verifies that applyPersonaSettings
// is a safe no-op when the engine has no persona store attached.
func TestApplyPersonaSettings_NoPersonaStoreNoOps(t *testing.T) {
        sess, err := NewSession() // no persona store
        require.NoError(t, err)
        eng, err := NewEngine(llm.ModelConfig{Name: "default"}, sess)
        require.NoError(t, err)

        // Should not panic.
        eng.applyPersonaSettings()
        // memoryInletMsg should still be cleared (step 1 happens before the
        // personaStore check).
        assert.Nil(t, eng.memoryInletMsg,
                "memoryInletMsg should be cleared even without persona store")
}
