package agent

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/http/httptest"
        "path/filepath"
        "sync"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/memstore"
        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/tools"
)

// --- the effective-memory matrix -----------------------------------------
//
// The memory toggle must follow the persona: a persona with an explicit
// memory_enabled owns the state (either direction), a persona without one
// inherits the surface's raw toggle, and "off" silences the remember/recall
// tools AND the auto inlet/outlet together — one knob, one truth.

func newToggleTestEngine(t *testing.T, opts ...EngineOption) (*Engine, *persona.Store) {
        t.Helper()
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))
        sess, err := NewSession(WithPersonaStore(pstore))
        require.NoError(t, err)
        all := append([]EngineOption{
                WithEmbedder(memstore.FakeEmbedder{DimCount: 64}),
        }, opts...)
        eng, err := NewEngine(llm.ModelConfig{Name: "m"}, sess, all...)
        require.NoError(t, err)
        return eng, pstore
}

// TestMemoryTogglePersonaOverrideWinsBothDirections pins the core of the
// user report: switching personas must switch the memory state with them.
// An explicit persona setting wins over the surface toggle in BOTH
// directions — the persona that opts in turns memory on even with the
// global knob off, and the persona that opts out turns it off even with
// the knob on.
func TestMemoryTogglePersonaOverrideWinsBothDirections(t *testing.T) {
        eng, pstore := newToggleTestEngine(t)

        offVal, onVal := false, true
        off, err := pstore.Create("amnesiac", "memory explicitly off", "")
        require.NoError(t, err)
        _, err = pstore.SetSettings(off.ID, nil, nil, &offVal)
        require.NoError(t, err)
        on, err := pstore.Create("elephant", "memory explicitly on", "")
        require.NoError(t, err)
        _, err = pstore.SetSettings(on.ID, nil, nil, &onVal)
        require.NoError(t, err)

        ctx := context.Background()

        // Raw toggle defaults ON (engine never told otherwise).
        require.True(t, eng.MemoryEnabled(), "unset surface toggle defaults to on")

        // Explicit OFF beats the on-toggle: no auto store, no inlet injection.
        require.NoError(t, pstore.SetActive(off.ID))
        require.NoError(t, eng.SetPersona(off.ID))
        assert.False(t, eng.MemoryEnabled(), "persona memory_enabled=false must win over the on toggle")
        assert.Nil(t, eng.memStoreForActivePersona(), "auto inlet/outlet must skip for an opted-out persona")
        assert.Nil(t, eng.injectSemanticMemories(ctx, "what is my name?"),
                "the inlet must not inject for an opted-out persona")

        // Explicit ON: switch in, auto paths armed again.
        require.NoError(t, pstore.SetActive(on.ID))
        require.NoError(t, eng.SetPersona(on.ID))
        assert.True(t, eng.MemoryEnabled(), "persona memory_enabled=true keeps memory on")

        // Flip the surface toggle off — the explicit persona still wins.
        eng.SetMemoryEnabled(false)
        assert.True(t, eng.MemoryEnabled(), "an explicit persona ON beats the surface toggle off")

        // ...and the opted-out persona stays off under an on-toggle.
        eng.SetMemoryEnabled(true)
        require.NoError(t, pstore.SetActive(off.ID))
        require.NoError(t, eng.SetPersona(off.ID))
        assert.False(t, eng.MemoryEnabled(), "an explicit persona OFF beats the surface toggle on")
}

// TestMemoryToggleNilPersonaInheritsSurfaceToggle pins the inherit rule: a
// persona without an explicit memory_enabled (and the default persona)
// inherits the surface's raw toggle, and flipping the toggle moves them.
func TestMemoryToggleNilPersonaInheritsSurfaceToggle(t *testing.T) {
        eng, pstore := newToggleTestEngine(t, WithMemoryEnabled(false))

        plain, err := pstore.Create("vanilla", "no memory opinion", "")
        require.NoError(t, err)

        require.False(t, eng.MemoryEnabled(), "raw toggle off + no persona override = off")
        require.NoError(t, pstore.SetActive(plain.ID))
        require.NoError(t, eng.SetPersona(plain.ID))
        assert.False(t, eng.MemoryEnabled(), "a persona without a setting inherits the toggle (off)")

        eng.SetMemoryEnabled(true)
        assert.True(t, eng.MemoryEnabled(), "flipping the toggle moves a persona that only inherits")

        // The default persona inherits exactly the same way.
        require.NoError(t, eng.SetPersona(""))
        assert.True(t, eng.MemoryEnabled())
        eng.SetMemoryEnabled(false)
        assert.False(t, eng.MemoryEnabled(), "default persona also follows the raw toggle")
}

// TestMemoryToolsRegistrationFollowsPersona proves the remember/recall tool
// set moves with the effective memory state: registered when a shared bank
// is attached and memory is on, unregistered when the persona opts out —
// on the very same switch, without rebuilding the engine.
func TestMemoryToolsRegistrationFollowsPersona(t *testing.T) {
        bank, err := memstore.Open(filepath.Join(t.TempDir(), "bank.sqlite"), memstore.FakeEmbedder{DimCount: 64}, memstore.DefaultParams())
        require.NoError(t, err)
        t.Cleanup(func() { _ = bank.Close() })

        eng, pstore := newToggleTestEngine(t, WithMemoryStore(bank), WithMemoryEnabled(true))

        offVal := false
        off, err := pstore.Create("amnesiac", "memory explicitly off", "")
        require.NoError(t, err)
        _, err = pstore.SetSettings(off.ID, nil, nil, &offVal)
        require.NoError(t, err)
        // Create auto-activates the new persona (store semantics, pinned by
        // TestStore_CreateSetsActive) — restore the default as active so the
        // first assert really exercises "default persona + toggle on", the
        // state a user's session is in before they ever switch.
        require.NoError(t, pstore.SetActive(persona.DefaultPersonaID))

        hasRemember := func() bool {
                for _, tl := range eng.buildCoreTools(nil) {
                        if tl.Definition.Name == "remember" {
                                return true
                        }
                }
                return false
        }

        assert.True(t, hasRemember(), "toggle on + default persona (inherit) = tools registered")

        require.NoError(t, pstore.SetActive(off.ID))
        require.NoError(t, eng.SetPersona(off.ID))
        assert.False(t, eng.MemoryEnabled())
        assert.False(t, hasRemember(), "switching to an opted-out persona must unregister remember/recall")

        require.NoError(t, eng.SetPersona(""))
        assert.True(t, eng.MemoryEnabled())
        assert.True(t, hasRemember(), "switching back re-registers the tools")
}

// --- loop-level end-to-end ------------------------------------------------
//
// These drive the REAL Loop (scripted SSE model + real persona store +
// real SQLite semantic store) to prove the memory system is wired, not just
// that its pieces work in isolation: a turn WITH tool calls must reach the
// consolidation path, and a NEW session's first request must carry the
// injected memory.

// memoryLLMServer scripts both request shapes the memory loop produces:
// streaming rounds for streamTurn (SSE chunks, one scriptedRound each) and
// the outlet's non-streaming Compact call (plain JSON carrying opsJSON as
// the assistant content). Every request body is captured.
type capturedChatRequest struct {
        Stream   bool
        Messages []struct {
                Role    string `json:"role"`
                Content string `json:"content"`
        }
}

func memoryLLMServer(rounds []scriptedRound, opsJSON string) (*httptest.Server, func() []capturedChatRequest) {
        var mu sync.Mutex
        var captured []capturedChatRequest
        streamN := 0
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                body, _ := io.ReadAll(r.Body)
                var req struct {
                        Stream bool `json:"stream"`
                }
                _ = json.Unmarshal(body, &req)
                cap := capturedChatRequest{Stream: req.Stream}
                _ = json.Unmarshal(body, &cap)
                mu.Lock()
                captured = append(captured, cap)
                streamN++
                n := streamN
                mu.Unlock()

                if req.Stream {
                        w.Header().Set("Content-Type", "text/event-stream")
                        var out string
                        if n <= len(rounds) {
                                round := rounds[n-1]
                                if round.Tool != "" {
                                        out = sseToolCallChunk(fmt.Sprintf("call_%d", n), round.Tool, round.Args)
                                } else {
                                        out = sseTextChunk(round.Text)
                                }
                        } else {
                                out = sseTextChunk("done")
                        }
                        _, _ = fmt.Fprint(w, out)
                        _, _ = fmt.Fprint(w, "data: [DONE]\n\n")
                        return
                }
                // The outlet's consolidation call (client.Compact): one JSON body,
                // no streaming. Reply with the scripted ops as assistant content.
                w.Header().Set("Content-Type", "application/json")
                _ = json.NewEncoder(w).Encode(map[string]any{
                        "choices": []map[string]any{{
                                "message":       map[string]string{"role": "assistant", "content": opsJSON},
                                "finish_reason": "stop",
                        }},
                })
        }))
        return srv, func() []capturedChatRequest {
                mu.Lock()
                defer mu.Unlock()
                return append([]capturedChatRequest(nil), captured...)
        }
}

// newMemoryLoopEngine builds an engine over a real persona store whose LLM
// client points at the scripted server, with a bash stub for tool rounds.
func newMemoryLoopEngine(t *testing.T, pstore *persona.Store, serverURL string, bash tools.Tool) *Engine {
        t.Helper()
        sess, err := NewSession(WithPersonaStore(pstore), WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: serverURL, APIKey: "x"},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools([]tools.Tool{bash}),
                WithEmbedder(memstore.FakeEmbedder{DimCount: 64}),
        )
        require.NoError(t, err)
        return eng
}

const solisMemory = "The user's name is Solis and they build Go agent harnesses."
const solisOps = `{"ops":[{"operation":"CREATE","content":"` + solisMemory + `","kind":"fact","scope":"user"}]}`

// TestLoopMemoryOutletFiresOnToolTurn is the end-to-end disproof of the
// "turns with tool calls never reach consolidation" suspicion: a turn that
// runs a bash tool and THEN wraps up must consolidate — the wrap-up round
// carries the outlet, the memory lands in the persona's SQLite store, and
// exactly one consolidation LLM call was made.
func TestLoopMemoryOutletFiresOnToolTurn(t *testing.T) {
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))
        p, err := pstore.Create("solo", "solo persona", "")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        srv, requests := memoryLLMServer([]scriptedRound{
                {Tool: "bash", Args: `{"command":"go build ./..."}`},
                {Text: "The build is green. Nice to meet you, Solis."},
        }, solisOps)
        defer srv.Close()

        eng := newMemoryLoopEngine(t, pstore, srv.URL, stubBashTool(func() string { return "ok" }))
        require.NoError(t, drainLoop(t, eng, "hey, my name is Solis and I build Go agent harnesses — can you check the build?"))

        // Wait for the background outlet to finish before asserting on it.
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        require.NoError(t, eng.WaitForOutstandingOutlets(ctx))

        reqs := requests()
        streamReqs, compactReqs := 0, 0
        for _, rq := range reqs {
                if rq.Stream {
                        streamReqs++
                } else {
                        compactReqs++
                }
        }
        require.Equal(t, 2, streamReqs, "one tool round + one wrap-up round")
        require.Equal(t, 1, compactReqs, "exactly one consolidation call for the turn")

        // The memory must now live in the persona's semantic store.
        semStore, err := pstore.SemanticStore(p, memstore.FakeEmbedder{DimCount: 64}, memstore.DefaultParams())
        require.NoError(t, err)
        traces, err := semStore.Retrieve(ctx, "Solis Go agent harnesses", memstore.ModeCompanion, memstore.Cues{}, time.Now())
        require.NoError(t, err)
        found := false
        for _, tr := range traces {
                if tr.Content == solisMemory {
                        found = true
                }
        }
        assert.True(t, found, "the tool-carrying turn's memory must be consolidated into the persona store, got %+v", traces)
}

// TestLoopMemoryOutletFiresOnEarlyStop pins the other half of the fix: a
// turn cut short by the tool-call budget is still a completed turn — the
// conversation content is real, so the outlet fires on the way out. Before
// the fix, every early-exit terminal (verdict stop, executor stop, budget
// trip) skipped consolidation entirely.
func TestLoopMemoryOutletFiresOnEarlyStop(t *testing.T) {
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))
        p, err := pstore.Create("solo", "solo persona", "")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        // The model wants tools forever; the cap (1) cuts the turn short on
        // round 2, whose call comes back rejected.
        srv, requests := memoryLLMServer([]scriptedRound{
                {Tool: "bash", Args: `{"command":"go build ./..."}`},
                {Tool: "bash", Args: `{"command":"go test ./..."}`},
        }, solisOps)
        defer srv.Close()

        eng := newMemoryLoopEngine(t, pstore, srv.URL, stubBashTool(func() string { return "ok" }))
        eng.SetMaxToolCalls(1)
        require.NoError(t, drainLoop(t, eng, "hey, my name is Solis and I build Go agent harnesses — keep checking things"))

        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        require.NoError(t, eng.WaitForOutstandingOutlets(ctx))

        compactReqs := 0
        for _, rq := range requests() {
                if !rq.Stream {
                        compactReqs++
                }
        }
        require.Equal(t, 1, compactReqs,
                "a budget-tripped turn is still a completed turn — the outlet must fire")
}

// TestLoopMemoryInletInjectsIntoModelRequest proves the other half of the
// loop wiring: a NEW session over the same persona store gets the stored
// memory injected into the model's actual request payload, as a system
// message immediately before the last user message.
func TestLoopMemoryInletInjectsIntoModelRequest(t *testing.T) {
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))
        p, err := pstore.Create("solo", "solo persona", "")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        // Seed the persona's store with one memory — same handle the engine's
        // inlet reads through (the persona store caches per persona ID).
        emb := memstore.FakeEmbedder{DimCount: 64}
        semStore, err := pstore.SemanticStore(p, emb, memstore.DefaultParams())
        require.NoError(t, err)
        _, _, err = semStore.Insert(context.Background(), memstore.NewTrace{
                Content:  solisMemory,
                Kind:     "fact",
                Scope:    "user",
                Salience: 0.6,
        })
        require.NoError(t, err)

        srv, requests := memoryLLMServer([]scriptedRound{
                {Text: "You're Solis, the Go agent harness builder."},
        }, `{"ops":[]}`)
        defer srv.Close()

        // A brand-new session in the same persona: the memory must ride along.
        eng := newMemoryLoopEngine(t, pstore, srv.URL, stubBashTool(func() string { return "ok" }))
        require.NoError(t, drainLoop(t, eng, "what is my name again?"))

        reqs := requests()
        require.NotEmpty(t, reqs)
        var stream *capturedChatRequest
        for i := range reqs {
                if reqs[i].Stream {
                        stream = &reqs[i]
                        break // the first stream request is the turn's only round
                }
        }
        require.NotNil(t, stream, "a streaming request must have been made")
        msgs := stream.Messages
        require.NotEmpty(t, msgs)

        last := msgs[len(msgs)-1]
        require.Equal(t, "user", last.Role, "the last message must be the user prompt")
        require.Equal(t, "what is my name again?", last.Content)

        prev := msgs[len(msgs)-2]
        require.Equal(t, "system", prev.Role,
                "the memory injection must sit immediately before the last user message")
        assert.Contains(t, prev.Content, "memories about the user",
                "third-person framing")
        assert.Contains(t, prev.Content, "Solis",
                "the stored fact must ride in the injected block")
}
