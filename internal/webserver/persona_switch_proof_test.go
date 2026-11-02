package webserver

import (
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/project"
)

// The persona-switch proof tests. The engine-side halves of these fixes are
// pinned in internal/agent (engine_persona_switch_test.go,
// engine_persona_settings_test.go, memory_loop_test.go); what was missing is
// proof at the SERVER level: a live engine built through liveFor (the real
// engineOptions wiring: model resolver, permission-mode setter, base mode,
// raw memory toggle, embedder) is switched via the real
// POST /api/personas/active handler, and the effect is asserted on the
// surfaces that matter —
//
//   - the footer's contextWindow (stateView), not just the engine's internals
//   - the server's ACTUAL gate decisions (a write goes Allow → Deny → Allow),
//     not just the setter having been called
//   - the memory chip (stateView) AND the live engine's tool surface moving
//     with the persona's memory_enabled in both directions

// newPersonaProofServer builds a server the way Start does, rooted at a real
// temp project with a two-window model catalog, a persona store, and a
// resumable session file, then primes the registries. The HOME override keeps
// the shared memory bank (memStoreFor) inside the test sandbox.
func newPersonaProofServer(t *testing.T) (*Server, *persona.Store, *project.Project, string) {
        t.Helper()

        home := t.TempDir()
        t.Setenv("HOME", home)
        require.NoError(t, os.MkdirAll(filepath.Join(home, ".a1"), 0o755))

        cfgYAML := `models:
  - name: big-model
    api: openai
    api_key: test-key
    context_window: 128000
    default: true
  - name: small-model
    api: openai
    api_key: test-key
    context_window: 8000
`
        require.NoError(t, os.WriteFile(filepath.Join(home, ".a1", "config.yaml"), []byte(cfgYAML), 0o644))

        proj, err := project.Discover(t.TempDir())
        require.NoError(t, err)
        require.NoError(t, proj.LoadConfig())

        // A minimal but valid session file so liveFor(WithResumeID) accepts it.
        sessionDir := proj.SessionDir()
        require.NoError(t, os.MkdirAll(sessionDir, 0o755))
        const sessionID = "c1d2e3f405162738495a6b7c8d9e0f11"
        require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "probe_"+sessionID+".jsonl"), []byte(
                `{"type":"EntrySession","id":"`+sessionID+`","timestamp":"2026-10-29T10-00-00","cwd":"`+t.TempDir()+`"}`+"\n"+
                        `{"type":"EntryMessage","id":"m1","parentID":null,"timestamp":"2026-10-29T10:00:01-05:00",`+
                        `"message":{"role":"user","content":"hello"}}`+"\n"), 0o600))

        store := persona.NewStore(filepath.Join(t.TempDir(), "personas"))
        s := &Server{
                proj:         proj,
                modes:        newModeState(),
                pins:         newPinnedSessions(t.TempDir()),
                personaStore: store,
        }
        s.initMaps()
        return s, store, proj, sessionID
}

// proofLiveEngine builds the live engine through liveFor — the same path the
// first prompt / transcript load takes — so the assertions run against an
// engine carrying the real engineOptions wiring.
func proofLiveEngine(t *testing.T, s *Server, sessionID string) *liveEngine {
        t.Helper()
        le, err := s.liveFor(sessionID)
        require.NoError(t, err)
        require.NotNil(t, le.engine)
        return le
}

// switchPersona drives the real POST /api/personas/active handler and fails
// the test on anything but 200 — a switch that errors must never look like a
// pass.
func switchPersona(t *testing.T, s *Server, id string) {
        t.Helper()
        req := httptest.NewRequest(http.MethodPost, "/api/personas/active", strings.NewReader(`{"id":"`+id+`"}`))
        rec := httptest.NewRecorder()
        s.handleSetActivePersona(rec, req)
        require.Equal(t, http.StatusOK, rec.Code, "persona switch to %q failed: %s", id, rec.Body.String())
}

// TestPersonaSwitchRescalesStateContextWindow pins the context-window fix at
// the webserver level: switching to a persona whose model has a different
// context window must rescale the state payload's contextWindow (the footer's
// denominator) on the very switch — not only after some later event refreshes
// it — and switching back to the default persona restores the base model's
// window. This is the TODO spec test for the "context-window recalc on
// persona switch" item.
func TestPersonaSwitchRescalesStateContextWindow(t *testing.T) {
        s, store, _, sessionID := newPersonaProofServer(t)

        // A persona pinned to the small-window model.
        small, err := store.Create("tiny", "runs the small-window model", "")
        require.NoError(t, err)
        model := "small-model"
        _, err = store.SetSettings(small.ID, &model, nil, nil)
        require.NoError(t, err)
        // Create auto-activates: park the default persona back in the active slot
        // so the engine builds under the base model, like a session that started
        // before any switch.
        require.NoError(t, store.SetActive(persona.DefaultPersonaID))

        le := proofLiveEngine(t, s, sessionID)

        // Baseline: the engine (and the footer) run on the big model's window.
        require.Equal(t, "big-model", le.engine.ModelName())
        require.Equal(t, 128000, s.stateView().ContextWindow,
                "baseline: the footer's denominator is the default model's window")

        // Switch to the small-window persona: the denominator must follow on the
        // switch itself.
        switchPersona(t, s, small.ID)
        assert.Equal(t, "small-model", le.engine.ModelName(),
                "the live engine must run the persona's model after the switch")
        assert.Equal(t, 8000, le.engine.ContextWindow(),
                "the engine's runtime window must re-seed to the persona model's window")
        assert.Equal(t, 8000, s.stateView().ContextWindow,
                "the state payload's contextWindow must equal the persona model's configured window (not the previous model's)")

        // Switch back to the default persona: the base model AND its window come
        // back. Before the fix, the last persona's window leaked forward.
        switchPersona(t, s, persona.DefaultPersonaID)
        assert.Equal(t, "big-model", le.engine.ModelName())
        assert.Equal(t, 128000, s.stateView().ContextWindow,
                "switching back to the default must restore the base model's window")
}

// TestPersonaSwitchFlipsPermissionGate pins the permission-mode fix at the
// webserver level: a persona with permission_mode: readonly must flip the
// server's ACTUAL gate (a write inside the workspace goes Allow → Deny) and
// the live engine's own gate with it, and switching back to the default
// persona must restore the previous mode. Asserting the setter was called is
// not enough — this test asserts gate decisions, the TODO spec's requirement.
func TestPersonaSwitchFlipsPermissionGate(t *testing.T) {
        s, store, proj, sessionID := newPersonaProofServer(t)

        // A readonly persona.
        ro, err := store.Create("auditor", "read-only reviewer", "")
        require.NoError(t, err)
        mode := "readonly"
        _, err = store.SetSettings(ro.ID, nil, &mode, nil)
        require.NoError(t, err)
        require.NoError(t, store.SetActive(persona.DefaultPersonaID))

        le := proofLiveEngine(t, s, sessionID)

        // A write INSIDE the project root: the readonly gate must deny it on
        // mode grounds, not workspace-fence grounds.
        writeReq := permission.Request{Action: permission.ActionWrite, Paths: []string{proj.Root() + "/f.txt"}}

        // Baseline: interactive mode allows the write (both the server's resolved
        // gate and the live engine's own gate instance).
        dec, _ := s.gate().Check(t.Context(), writeReq)
        require.Equal(t, permission.Allow, dec, "baseline: interactive allows the write")
        dec, _ = le.engine.Gate().Check(t.Context(), writeReq)
        require.Equal(t, permission.Allow, dec, "baseline: the live engine's gate allows the write")

        // Switch to the readonly persona.
        switchPersona(t, s, ro.ID)

        assert.Equal(t, permission.ModeReadonly, s.modeStore().mode(),
                "the mode store must carry the persona's mode after the switch")
        assert.Equal(t, "readonly", s.stateView().Mode,
                "the state payload's mode (the UI pill) must follow the switch")
        dec, reason := s.gate().Check(t.Context(), writeReq)
        assert.Equal(t, permission.Deny, dec, "the server's gate must deny writes under the readonly persona")
        assert.Contains(t, reason, "readonly")
        dec, _ = le.engine.Gate().Check(t.Context(), writeReq)
        assert.Equal(t, permission.Deny, dec,
                "the LIVE ENGINE's gate must deny too — the switch has to reach the executor, not just the mode store")

        // Switch back to the default persona: the base mode (interactive) is
        // restored — WithBasePermissionMode exists for exactly this.
        switchPersona(t, s, persona.DefaultPersonaID)
        assert.Equal(t, permission.ModeInteractive, s.modeStore().mode(),
                "switching back to the default must restore the base mode")
        dec, _ = s.gate().Check(t.Context(), writeReq)
        assert.Equal(t, permission.Allow, dec, "the write is allowed again after the switch back")
        dec, _ = le.engine.Gate().Check(t.Context(), writeReq)
        assert.Equal(t, permission.Allow, dec, "the live engine's gate allows again")
}

// TestPersonaSwitchMovesMemoryToggle pins the memory-toggle fix at the
// webserver level: the persona's memory_enabled owns the memory surface in
// BOTH directions. Switching to a persona that opts in turns memory on (the
// state payload's chip, the engine's effective state, and the remember/recall
// tool registration) even while the global settings toggle is OFF; switching
// to a persona that opts out turns the whole surface off; and the default
// persona falls back to the global toggle. This is the web-level half of the
// "memory toggle doesn't follow the persona" report — the engine-side matrix
// is pinned in internal/agent/memory_loop_test.go.
func TestPersonaSwitchMovesMemoryToggle(t *testing.T) {
        s, store, _, sessionID := newPersonaProofServer(t)

        // Personas with explicit memory settings on both sides.
        onVal, offVal := true, false
        elephant, err := store.Create("elephant", "memory explicitly on", "")
        require.NoError(t, err)
        _, err = store.SetSettings(elephant.ID, nil, nil, &onVal)
        require.NoError(t, err)
        amnesiac, err := store.Create("amnesiac", "memory explicitly off", "")
        require.NoError(t, err)
        _, err = store.SetSettings(amnesiac.ID, nil, nil, &offVal)
        require.NoError(t, err)
        require.NoError(t, store.SetActive(persona.DefaultPersonaID))

        // The global settings toggle stays OFF the whole test — every flip below
        // must come from the persona alone.
        require.False(t, s.stateStore().snapshot().MemoryEnabled)

        le := proofLiveEngine(t, s, sessionID)

        // Baseline: default persona inherits the global toggle — off, no tools.
        assert.False(t, s.stateView().MemoryEnabled)
        assert.False(t, le.engine.MemoryEnabled())
        assert.False(t, le.engine.HasTool("remember"),
                "baseline: global toggle off + default persona = memory tools unregistered")

        // Switch to the opt-in persona: memory turns ON despite the global knob.
        switchPersona(t, s, elephant.ID)
        assert.True(t, s.stateView().MemoryEnabled,
                "the state payload's memory chip must report the persona's explicit ON over the global OFF")
        assert.True(t, le.engine.MemoryEnabled(),
                "the live engine's effective memory state must follow the persona")
        assert.True(t, le.engine.HasTool("remember"),
                "switching to an opted-in persona must attach the shared bank and register remember/recall")

        // Switch to the opt-out persona: the whole surface turns OFF.
        switchPersona(t, s, amnesiac.ID)
        assert.False(t, s.stateView().MemoryEnabled)
        assert.False(t, le.engine.MemoryEnabled())
        assert.False(t, le.engine.HasTool("remember"),
                "switching to an opted-out persona must detach the bank and unregister the memory tools")

        // Back to the default persona: inherits the global toggle (still off).
        switchPersona(t, s, persona.DefaultPersonaID)
        assert.False(t, s.stateView().MemoryEnabled,
                "the default persona falls back to the global toggle")
        assert.False(t, le.engine.HasTool("remember"))
}

// TestFreshEngineAdoptsActivePersona pins the new-session half of the
// persona-settings story, found while proving the switch path above: a
// brand-new engine built while a persona with overrides is ALREADY active
// (the "switched yesterday, opened a new chat today" flow) must adopt that
// persona's model, window, and permission mode at creation — not run the
// global defaults until the next manual switch. Before the fix, only memory
// was compensated at build time; the model and the mode leaked from the
// global config.
func TestFreshEngineAdoptsActivePersona(t *testing.T) {
        s, store, proj, sessionID := newPersonaProofServer(t)

        // A persona carrying every override at once.
        tiny, err := store.Create("tiny", "small model, readonly, memory on", "")
        require.NoError(t, err)
        model, mode := "small-model", "readonly"
        onVal := true
        _, err = store.SetSettings(tiny.ID, &model, &mode, &onVal)
        require.NoError(t, err)
        // Create auto-activated "tiny" — leave it active, the state a returning
        // user's server is in when they open a fresh chat.

        le := proofLiveEngine(t, s, sessionID)

        assert.Equal(t, "small-model", le.engine.ModelName(),
                "a fresh engine must adopt the active persona's model")
        assert.Equal(t, 8000, le.engine.ContextWindow(),
                "...and its context window")
        assert.Equal(t, permission.ModeReadonly, s.modeStore().mode(),
                "a fresh engine must adopt the active persona's permission mode")
        assert.True(t, le.engine.MemoryEnabled(),
                "a fresh engine must adopt the active persona's memory setting")
        assert.True(t, le.engine.HasTool("remember"))

        // The adoption is behavioral, not cosmetic: the server's gate denies
        // writes from the very first turn.
        writeReq := permission.Request{Action: permission.ActionWrite, Paths: []string{proj.Root() + "/f.txt"}}
        dec, _ := s.gate().Check(t.Context(), writeReq)
        assert.Equal(t, permission.Deny, dec,
                "the readonly persona's gate must be live before any switch happens")
        dec, _ = le.engine.Gate().Check(t.Context(), writeReq)
        assert.Equal(t, permission.Deny, dec)

        // And the footer's denominator matches the adopted model, not the default.
        assert.Equal(t, 8000, s.stateView().ContextWindow)
}
