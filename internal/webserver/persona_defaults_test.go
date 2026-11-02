package webserver

import (
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/persona"
)

// persona_defaults_test.go pins the "persona settings are the default
// selection" fixes at the server level:
//
//   - An untouched model knob resolves to the ACTIVE persona's Model (the
//     footer chip, the settings panel, and every new engine), not straight
//     to the startup model — including the DEFAULT persona's Model.
//   - A persona switch re-owns the defaults (the manual knob clears).
//   - The settings-panel model picker and the mode switcher write the
//     persona when it owns the setting (same ownership rule the memory
//     toggle already follows).
//   - The mode surfaces show the EFFECTIVE mode (persona override folded
//     in), not the raw pre-persona base.
//   - clearMemoryEnabled reverts a persona's memory toggle to "inherit".

// TestEffectiveModelName_PersonaIsDefault pins the display/构造 resolution:
// with the manual knob untouched, the active persona's Model is the
// effective model — for a custom persona AND for the default persona. The
// old resolution went straight to the project default, which is exactly
// the "falls back to the startup model" report.
func TestEffectiveModelName_PersonaIsDefault(t *testing.T) {
        s, store := newSettingsTestServer(t)

        // Custom persona pinned to the small model, active.
        p, err := store.Create("worker", "work persona", "")
        require.NoError(t, err)
        model := "small-model"
        _, err = store.SetSettings(p.ID, &model, nil, nil)
        require.NoError(t, err)

        assert.Equal(t, "small-model", s.effectiveModelName(),
                "an untouched knob must resolve to the active persona's model")
        assert.Equal(t, "small-model", s.stateView().Model,
                "the footer chip must name the persona's model, not an empty default")
        assert.Equal(t, 8000, s.stateView().ContextWindow,
                "the footer's denominator must be the persona model's window")

        // The settings panel's current model follows the same resolution.
        req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
        rec := httptest.NewRecorder()
        s.handleGetSettings(rec, req)
        require.Equal(t, http.StatusOK, rec.Code)
        var settings struct {
                State struct {
                        Model string `json:"model"`
                } `json:"state"`
        }
        require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &settings))
        assert.Equal(t, "small-model", settings.State.Model,
                "the settings panel must highlight the persona's model")

        // The DEFAULT persona's Model is equally real: park the default persona
        // in the active slot with an override and the resolution must follow.
        require.NoError(t, store.SetActive(persona.DefaultPersonaID))
        assert.Equal(t, "", s.effectiveModelName(),
                "precond: the shipped default persona carries no model yet")
        _, err = store.SetSettings(persona.DefaultPersonaID, &model, nil, nil)
        require.NoError(t, err)
        assert.Equal(t, "small-model", s.effectiveModelName(),
                "the DEFAULT persona's model must be the default selection too")
}

// TestEffectiveModelName_KnobWins pins the override direction: a manual
// pick from the settings panel beats the persona until the next switch.
func TestEffectiveModelName_KnobWins(t *testing.T) {
        s, store := newSettingsTestServer(t)
        p, err := store.Create("worker", "work persona", "")
        require.NoError(t, err)
        model := "small-model"
        _, err = store.SetSettings(p.ID, &model, nil, nil)
        require.NoError(t, err)

        s.stateStore().setModel("big-model")
        assert.Equal(t, "big-model", s.effectiveModelName(),
                "a manual knob pick wins over the persona's model")

        // A persona switch re-owns the defaults: the knob clears and the new
        // persona's model shows again.
        switchPersona(t, s, p.ID)
        assert.Equal(t, "", s.stateStore().snapshot().Model,
                "a persona switch must clear the stale manual knob")
        assert.Equal(t, "small-model", s.effectiveModelName())
}

// TestHandleSetModel_PersonaOwnsPick pins the write direction: when the
// active persona carries a Model, the settings-panel picker edits the
// PERSONA (persisted, survives restarts) instead of the per-run knob.
func TestHandleSetModel_PersonaOwnsPick(t *testing.T) {
        s, store := newSettingsTestServer(t)
        p, err := store.Create("worker", "work persona", "")
        require.NoError(t, err)
        model := "small-model"
        _, err = store.SetSettings(p.ID, &model, nil, nil)
        require.NoError(t, err)

        // The user picks the big model from the panel.
        req := httptest.NewRequest(http.MethodPost, "/api/settings/model", stringBody(`{"name":"big-model"}`))
        rec := httptest.NewRecorder()
        s.handleSetModel(rec, req)
        require.Equal(t, http.StatusOK, rec.Code, "set model must succeed: %s", rec.Body.String())

        got, err := store.Get(p.ID)
        require.NoError(t, err)
        assert.Equal(t, "big-model", got.Model,
                "the pick must be persisted on the persona that owns the model setting")
        assert.Equal(t, "", s.stateStore().snapshot().Model,
                "the per-run knob must stay untouched when the persona owns the setting")

        // Picking "project default" (empty name) CLEARS the persona's override —
        // the same semantics the memory toggle's owner path already has.
        req = httptest.NewRequest(http.MethodPost, "/api/settings/model", stringBody(`{"name":""}`))
        rec = httptest.NewRecorder()
        s.handleSetModel(rec, req)
        require.Equal(t, http.StatusOK, rec.Code)
        got, err = store.Get(p.ID)
        require.NoError(t, err)
        assert.Equal(t, "", got.Model, "an empty pick must clear the persona's model override")
}

// TestHandleSetModel_GlobalKnobWhenPersonaInherits pins the other side: a
// persona WITHOUT a Model keeps the old behavior — the picker writes the
// per-run global knob.
func TestHandleSetModel_GlobalKnobWhenPersonaInherits(t *testing.T) {
        s, store := newSettingsTestServer(t)
        p, err := store.Create("plain", "no overrides", "")
        require.NoError(t, err)
        require.NoError(t, store.SetActive(p.ID))

        req := httptest.NewRequest(http.MethodPost, "/api/settings/model", stringBody(`{"name":"small-model"}`))
        rec := httptest.NewRecorder()
        s.handleSetModel(rec, req)
        require.Equal(t, http.StatusOK, rec.Code)

        assert.Equal(t, "small-model", s.stateStore().snapshot().Model,
                "a persona without a Model keeps the per-run knob write path")
        got, _ := store.Get(p.ID)
        assert.Equal(t, "", got.Model, "the persona must not gain an override it did not have")
}

// TestModeSurfaces_ShowPersonaOverride pins the permission-mode half: the
// mode switcher's GET shows the persona's explicit PermissionMode (with the
// right entry marked active), even before any engine exists to push it into
// the raw mode store — the pill must reflect what a new session runs.
func TestModeSurfaces_ShowPersonaOverride(t *testing.T) {
        s, store := newSettingsTestServer(t)
        p, err := store.Create("auditor", "read-only reviewer", "")
        require.NoError(t, err)
        mode := "readonly"
        _, err = store.SetSettings(p.ID, nil, &mode, nil)
        require.NoError(t, err)

        // The raw mode store still holds the boot-time base…
        assert.Equal(t, permission.ModeInteractive, s.modeStore().mode(),
                "precond: the raw surface mode is untouched")

        // …but the mode view resolves the persona's override.
        view := s.modeView()
        assert.Equal(t, "readonly", view.Mode,
                "the mode view must show the persona's explicit mode over the raw base")
        activeCount := 0
        for _, m := range view.Modes {
                if m.Active {
                        activeCount++
                }
        }
        assert.Equal(t, 1, activeCount, "exactly one mode may be marked active")
        assert.Equal(t, "readonly", s.effectivePermissionMode())

        // Cycling steps from the EFFECTIVE mode, not the raw base.
        next := s.effectiveCycleNext()
        assert.Equal(t, permission.ModeAutopilot, next,
                "cycling from a persona-owned readonly must advance from readonly")

        // The state view's mode chip carries the same resolution.
        assert.Equal(t, "readonly", s.stateView().Mode)
}

// TestHandleSetMode_PersonaOwnsPick pins the write direction for modes:
// when the active persona carries a PermissionMode, the switcher writes
// the persona (persisted); otherwise it writes the surface's global mode.
func TestHandleSetMode_PersonaOwnsPick(t *testing.T) {
        s, store := newSettingsTestServer(t)
        p, err := store.Create("auditor", "read-only reviewer", "")
        require.NoError(t, err)
        mode := "readonly"
        _, err = store.SetSettings(p.ID, nil, &mode, nil)
        require.NoError(t, err)

        req := httptest.NewRequest(http.MethodPost, "/api/mode", stringBody(`{"mode":"autopilot"}`))
        rec := httptest.NewRecorder()
        s.handleSetMode(rec, req)
        require.Equal(t, http.StatusOK, rec.Code, "set mode must succeed: %s", rec.Body.String())

        got, err := store.Get(p.ID)
        require.NoError(t, err)
        assert.Equal(t, "autopilot", got.PermissionMode,
                "the mode pick must be persisted on the persona that owns the setting")

        // Without an override, the write goes to the surface store (old path).
        plain, err := store.Create("plain", "no overrides", "")
        require.NoError(t, err)
        require.NoError(t, store.SetActive(plain.ID))
        req = httptest.NewRequest(http.MethodPost, "/api/mode", stringBody(`{"mode":"yolo"}`))
        rec = httptest.NewRecorder()
        s.handleSetMode(rec, req)
        require.Equal(t, http.StatusOK, rec.Code)
        assert.Equal(t, permission.ModeYolo, s.modeStore().mode(),
                "a persona without a mode keeps the surface-store write path")
        gotPlain, _ := store.Get(plain.ID)
        assert.Equal(t, "", gotPlain.PermissionMode)
}

// TestHandleUpdatePersona_ClearMemoryEnabled pins the tri-state memory
// toggle's escape hatch: clearMemoryEnabled reverts the persona's
// memory_enabled to nil (inherit the global setting) — the pointer
// contract alone cannot express it, and the old binary form silently
// pinned "off" on every save.
func TestHandleUpdatePersona_ClearMemoryEnabled(t *testing.T) {
        s, store := newSettingsTestServer(t)
        p, err := store.Create("worker", "work persona", "")
        require.NoError(t, err)
        onVal := true
        _, err = store.SetSettings(p.ID, nil, nil, &onVal)
        require.NoError(t, err)

        body := `{"name":"worker","tagline":"work persona","clearMemoryEnabled":true}`
        req := httptest.NewRequest(http.MethodPut, "/api/personas/"+p.ID, stringBody(body))
        req.SetPathValue("id", p.ID)
        rec := httptest.NewRecorder()
        s.handleUpdatePersona(rec, req)
        require.Equal(t, http.StatusOK, rec.Code, "clear must succeed: %s", rec.Body.String())

        got, err := store.Get(p.ID)
        require.NoError(t, err)
        assert.Nil(t, got.MemoryEnabled,
                "clearMemoryEnabled must revert the override to inherit")
        assert.True(t, s.effectiveMemoryEnabled(true),
                "with the override gone, the global toggle rules again")
}

// TestFreshEngineAdoptsDefaultPersonaSettings pins the liveFor half for
// the DEFAULT persona: a fresh engine built while the default persona
// carries Model/PermissionMode overrides adopts them — previously the
// deferred SetPersona skipped DefaultPersonaID and fresh sessions fell
// back to the startup model.
func TestFreshEngineAdoptsDefaultPersonaSettings(t *testing.T) {
        s, store, _, sessionID := newPersonaProofServer(t)

        model, mode := "small-model", "readonly"
        _, err := store.SetSettings(persona.DefaultPersonaID, &model, &mode, nil)
        require.NoError(t, err)

        le := proofLiveEngine(t, s, sessionID)

        assert.Equal(t, "small-model", le.engine.ModelName(),
                "a fresh engine must run the DEFAULT persona's model (the startup-model fallback was the bug)")
        assert.Equal(t, 8000, le.engine.ContextWindow())
        assert.Equal(t, permission.ModeReadonly, s.modeStore().mode(),
                "a fresh engine must adopt the default persona's permission mode")
        assert.Equal(t, "small-model", s.stateView().Model,
                "the footer chip must name the persona's model, not the startup model")
        assert.Equal(t, "readonly", s.stateView().Mode)
}

// TestPersonaEditActiveModelResetsKnob pins the editor path: explicitly
// writing the ACTIVE persona's Model clears a stale manual knob so the new
// model is what the UI shows and new engines run.
func TestPersonaEditActiveModelResetsKnob(t *testing.T) {
        s, store := newSettingsTestServer(t)
        p, err := store.Create("worker", "work persona", "")
        require.NoError(t, err)
        s.stateStore().setModel("big-model") // stale manual pick

        body := `{"name":"worker","tagline":"work persona","model":"small-model"}`
        req := httptest.NewRequest(http.MethodPut, "/api/personas/"+p.ID, stringBody(body))
        req.SetPathValue("id", p.ID)
        rec := httptest.NewRecorder()
        s.handleUpdatePersona(rec, req)
        require.Equal(t, http.StatusOK, rec.Code, "update must succeed: %s", rec.Body.String())

        assert.Equal(t, "", s.stateStore().snapshot().Model,
                "an explicit Model write on the active persona must clear the stale manual knob")
        assert.Equal(t, "small-model", s.effectiveModelName())

        // A profile-only edit must NOT clear the knob (only an explicit Model
        // write re-owns the default).
        s.stateStore().setModel("big-model")
        profileOnly := `{"name":"worker","tagline":"renamed"}`
        req = httptest.NewRequest(http.MethodPut, "/api/personas/"+p.ID, stringBody(profileOnly))
        req.SetPathValue("id", p.ID)
        rec = httptest.NewRecorder()
        s.handleUpdatePersona(rec, req)
        require.Equal(t, http.StatusOK, rec.Code)
        assert.Equal(t, "big-model", s.stateStore().snapshot().Model,
                "a profile-only edit must leave the manual knob alone")
}
