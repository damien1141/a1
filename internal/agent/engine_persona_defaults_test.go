package agent

import (
        "path/filepath"
        "reflect"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/permission"
)

// engine_persona_defaults_test.go pins the "persona settings are the
// defaults" fixes at the engine level:
//
//   - The DEFAULT persona's settings are real settings. The persona editor
//     writes Model/PermissionMode/MemoryModel for every persona, and the old
//     isDefault skips made them silently dead — fresh sessions fell back to
//     the startup model, and the memory-ops model never routed.
//   - A persona model swap carries the override model's request hooks (a
//     persona model on another provider needs that provider's wire shape).

// TestApplyPersonaSettings_DefaultPersonaOverridesApply pins the headline
// fix: Model and PermissionMode set on the DEFAULT persona apply on
// SetPersona — the default persona is a persona like any other, not a
// special "carry no overrides" slot. The revert path (clearing the default
// persona's overrides) restores the engine's base.
func TestApplyPersonaSettings_DefaultPersonaOverridesApply(t *testing.T) {
        eng, pstore, resolverCalled, capturedMode := setupTestEngine(t)

        // Configure the DEFAULT persona with both overrides — exactly what the
        // persona editor writes when a user sets their defaults on the shipped
        // "a1" persona.
        model, mode := "known-model", "autopilot"
        _, err := pstore.SetSettings(persona.DefaultPersonaID, &model, &mode, nil)
        require.NoError(t, err)

        require.NoError(t, eng.SetPersona(persona.DefaultPersonaID))

        assert.True(t, *resolverCalled,
                "the default persona's Model must reach the resolver (it did not before the fix)")
        assert.Equal(t, "known-model", eng.modelCfg.Name,
                "the default persona's Model must apply to the engine")
        assert.Equal(t, permission.ModeAutopilot, *capturedMode,
                "the default persona's PermissionMode must reach the setter")

        // Clearing the overrides restores the engine's base model (the
        // basePermissionMode restore is exercised the same way as any persona
        // without a mode).
        empty := ""
        _, err = pstore.SetSettings(persona.DefaultPersonaID, &empty, &empty, nil)
        require.NoError(t, err)
        require.NoError(t, eng.SetPersona(persona.DefaultPersonaID))
        assert.Equal(t, "default-model", eng.modelCfg.Name,
                "clearing the default persona's Model must restore the base model")
}

// TestApplyPersonaSettings_HooksFollowPersonaModel pins the hooks half of
// the model swap: a persona model with a built-in preset (deepseek's
// thinking interceptor) must carry that preset's hooks, and reverting to a
// persona without a model restores the construction-time hooks. Wrong
// hooks on a cross-provider persona model meant vendor-specific request
// shaping from the MAIN model leaking onto the override model's wire.
func TestApplyPersonaSettings_HooksFollowPersonaModel(t *testing.T) {
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))

        sess, err := NewSession(WithPersonaStore(pstore))
        require.NoError(t, err)
        // Base hooks stay zero (none wired); the persona model resolves to
        // deepseek-chat, whose preset ships the deepseek thinking interceptor.
        eng, err := NewEngine(llm.ModelConfig{Name: "default-model"}, sess,
                WithPersonaModelResolver(func(name string) (llm.ModelConfig, bool) {
                        if name == "deepseek-flash" {
                                return llm.ModelConfig{Name: name, API: llm.OpenAI}, true
                        }
                        return llm.ModelConfig{}, false
                }),
        )
        require.NoError(t, err)
        require.True(t, reflect.ValueOf(eng.hooks).IsZero(),
                "test precond: base hooks start zero (none wired)")

        p, err := pstore.Create("deep", "persona pinned to a preset model", "")
        require.NoError(t, err)
        _, err = pstore.UpdateSettings(p.ID, "deepseek-flash", nil, "")
        require.NoError(t, err)
        require.NoError(t, eng.SetPersona(p.ID))

        require.Equal(t, "deepseek-flash", eng.modelCfg.Name)
        assert.False(t, reflect.ValueOf(eng.hooks).IsZero(),
                "a persona model with a preset must install that preset's hooks")

        // Switch back to the default persona (no Model): base model AND base
        // hooks return.
        require.NoError(t, eng.SetPersona(persona.DefaultPersonaID))
        assert.Equal(t, "default-model", eng.modelCfg.Name)
        assert.True(t, reflect.ValueOf(eng.hooks).IsZero(),
                "reverting to a persona without a model must restore the base hooks")
}

// opsTestEngine builds an engine whose resolver records every name it is
// asked to resolve, for asserting which model the memory-ops client picked.
func opsTestEngine(t *testing.T) (*Engine, *persona.Store, *[]string) {
        t.Helper()
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))

        var asked []string
        sess, err := NewSession(WithPersonaStore(pstore))
        require.NoError(t, err)
        eng, err := NewEngine(llm.ModelConfig{Name: "default-model"}, sess,
                WithPersonaModelResolver(func(name string) (llm.ModelConfig, bool) {
                        asked = append(asked, name)
                        if name == "known-model" {
                                return llm.ModelConfig{Name: name, API: llm.OpenAI, BaseURL: "http://ops.invalid/v1"}, true
                        }
                        return llm.ModelConfig{}, false
                }),
        )
        require.NoError(t, err)
        return eng, pstore, &asked
}

// TestOpsClient_DefaultPersonaMemoryModel pins the smart-titles fix: the
// memory-ops client (consolidation AND the web GUI's session naming)
// follows the ACTIVE persona's MemoryModel — including the DEFAULT
// persona's. Before the fix, memoryConsolidationClient gated on
// session.activePersona() (false for the default), so a memory-ops model
// configured on the default persona never routed and titles ran on the
// main model.
func TestOpsClient_DefaultPersonaMemoryModel(t *testing.T) {
        eng, pstore, asked := opsTestEngine(t)

        // No MemoryModel: the ops client IS the engine's main client.
        require.Same(t, eng.client, eng.OpsClient())

        // MemoryModel on the DEFAULT persona: the ops client must route there.
        _, err := pstore.SetMemoryModel(persona.DefaultPersonaID, "known-model")
        require.NoError(t, err)
        ops := eng.OpsClient()
        require.NotSame(t, eng.client, ops,
                "the default persona's MemoryModel must produce a dedicated ops client")
        assert.Contains(t, *asked, "known-model",
                "the memory model must resolve through the catalog (provider-correct config), not a name swap")

        // Clearing the MemoryModel returns the main client.
        _, err = pstore.SetMemoryModel(persona.DefaultPersonaID, "")
        require.NoError(t, err)
        require.Same(t, eng.client, eng.OpsClient())
}

// TestOpsClient_NonDefaultPersonaMemoryModel pins the same routing for a
// custom persona (the pre-existing behavior, now via the catalog resolver)
// plus the fallbacks: a memory model equal to the running model collapses
// to the main client, and a catalog miss degrades to the legacy name-swap
// instead of breaking.
func TestOpsClient_NonDefaultPersonaMemoryModel(t *testing.T) {
        eng, pstore, asked := opsTestEngine(t)

        p, err := pstore.Create("worker", "worker persona", "")
        require.NoError(t, err)

        // Memory model equal to the running model: same client, no round-trip.
        _, err = pstore.SetMemoryModel(p.ID, "default-model")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))
        require.Same(t, eng.client, eng.OpsClient(),
                "a memory model equal to the running model must reuse the main client")

        // Unknown catalog name: the resolver is consulted, misses, and the
        // legacy name-swap still routes somewhere.
        _, err = pstore.SetMemoryModel(p.ID, "off-catalog-8b")
        require.NoError(t, err)
        ops := eng.OpsClient()
        require.NotSame(t, eng.client, ops,
                "a catalog miss must fall back to the name-swap ops client, not break")
        assert.Contains(t, *asked, "off-catalog-8b",
                "the catalog resolver is consulted first even on the fallback path")

        // Known catalog name: resolved through the resolver (asserted by the
        // default-persona test as well; this pins the non-default path).
        _, err = pstore.SetMemoryModel(p.ID, "known-model")
        require.NoError(t, err)
        ops = eng.OpsClient()
        require.NotSame(t, eng.client, ops)
        assert.Contains(t, *asked, "known-model")
}
