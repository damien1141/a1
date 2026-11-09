package agent

import (
        "context"
        "encoding/json"
        "fmt"
        "net/http"
        "net/http/httptest"
        "strings"
        "sync"
        "sync/atomic"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools"
)

// --- turnContractState -------------------------------------------------

func TestTurnContractDeclareRatchets(t *testing.T) {
        var c turnContractState

        held, added, refused := c.declare([]string{"go-build", " go-vet ", "go-build", "not-a-check", "  "})
        assert.Equal(t, []string{"go-build", "go-vet"}, held)
        assert.Equal(t, []string{"go-build", "go-vet"}, added)
        assert.Equal(t, []string{"  ", "not-a-check"}, refused)

        // Re-declaring a subset is a no-op, never a removal.
        held, added, refused = c.declare([]string{"go-build"})
        assert.Equal(t, []string{"go-build", "go-vet"}, held, "declared checks cannot be removed")
        assert.Empty(t, added)
        assert.Empty(t, refused)

        // Adding works; malformed ids are refused without touching the rest.
        held, added, refused = c.declare([]string{"go-test:github.com/x/auth", "bogus"})
        assert.Equal(t, []string{"go-build", "go-test:github.com/x/auth", "go-vet"}, held)
        assert.Equal(t, []string{"go-test:github.com/x/auth"}, added)
        assert.Equal(t, []string{"bogus"}, refused)

        // Reset empties for the next turn.
        c.reset()
        assert.Empty(t, c.snapshot())
}

// --- the contract tool --------------------------------------------------

func newContractTestEngine(t *testing.T, extra ...EngineOption) *Engine {
        t.Helper()
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        opts := append([]EngineOption{
                WithGate(permission.AllowAll{}),
                WithTools(nil), // DefaultTools never runs here; contract tool is the target
        }, extra...)
        engine, err := NewEngine(llm.ModelConfig{Name: "fake", APIKey: "x"}, sess, opts...)
        require.NoError(t, err)
        return engine
}

func runToolCall(t *testing.T, e *Executor, name, args string) llm.Message {
        t.Helper()
        msgs, _, _ := e.Run(t.Context(), []llm.ToolCall{{
                ID: "c1", Type: "function", Function: llm.Function{Name: name, Arguments: args},
        }}, func(session.ToolData) bool { return true })
        require.Len(t, msgs, 1)
        return msgs[0]
}

func TestContractToolRegisteredAndDeclared(t *testing.T) {
        engine := newContractTestEngine(t)
        assert.True(t, engine.HasTool("contract"), "contract tool must be registered on every engine")

        msg := runToolCall(t, engine.executor, "contract", `{"checks":["go-build","go-test:github.com/damien1141/a1/internal/auth"]}`)
        assert.Contains(t, msg.Content, "Contract held by the harness: 2 checks")
        assert.Contains(t, msg.Content, "go-build: NO EVIDENCE")
        assert.Contains(t, msg.Content, "go-test:github.com/damien1141/a1/internal/auth: NO EVIDENCE")
        assert.Contains(t, msg.Content, "cannot be removed, only satisfied")
        assert.Equal(t, []string{"go-build", "go-test:github.com/damien1141/a1/internal/auth"}, engine.TurnContract())
}

func TestContractToolStatusReflectsLedger(t *testing.T) {
        engine := newContractTestEngine(t)

        // Seed evidence: go-build passes, a package fails.
        engine.EvidenceLedger().Observe("go build ./...", "", false, time.Now())
        engine.EvidenceLedger().Observe("go test ./internal/auth/", evidenceGoTestFail, true, time.Now())

        msg := runToolCall(t, engine.executor, "contract", `{"checks":["go-build","go-test:github.com/damien1141/a1/internal/auth","go-vet"]}`)
        assert.Contains(t, msg.Content, "go-build: PASSING")
        assert.Contains(t, msg.Content, "go-test:github.com/damien1141/a1/internal/auth: FAILING")
        assert.Contains(t, msg.Content, "go-vet: NO EVIDENCE")
}

func TestContractToolRefusesInvalidAndNeverRemoves(t *testing.T) {
        engine := newContractTestEngine(t)

        msg := runToolCall(t, engine.executor, "contract", `{"checks":["go-build","make-it-pass"]}`)
        assert.Contains(t, msg.Content, "Refused (not valid check ids): make-it-pass")
        assert.Equal(t, []string{"go-build"}, engine.TurnContract())

        // A later declaration cannot shed the earlier check.
        msg = runToolCall(t, engine.executor, "contract", `{"checks":["go-vet"]}`)
        assert.Contains(t, msg.Content, "2 checks")
        assert.Equal(t, []string{"go-build", "go-vet"}, engine.TurnContract())

        // Empty declaration on an empty contract explains the grammar.
        engine.turnContract.reset()
        msg = runToolCall(t, engine.executor, "contract", `{"checks":[]}`)
        assert.Contains(t, msg.Content, "No checks declared")
}

func TestContractToolBadArgs(t *testing.T) {
        engine := newContractTestEngine(t)
        // Valid JSON of the wrong shape reaches the tool and is refused in
        // its own voice.
        msg := runToolCall(t, engine.executor, "contract", `"not-an-object"`)
        assert.Contains(t, msg.Content, "contract: arguments must be")
        assert.Empty(t, engine.TurnContract())

        // Unparseable arguments are refused one layer up, by the executor's
        // malformed-arguments guard — same outcome (no contract held), with
        // the guard naming what arrived and the shape to re-issue.
        msg = runToolCall(t, engine.executor, "contract", `{invalid`)
        assert.Contains(t, msg.Content, "malformed arguments")
        assert.Contains(t, msg.Content, `"checks"`)
        assert.Empty(t, engine.TurnContract())
}

// --- zero-rank stop (loop level) ----------------------------------------

// scriptedRound is one scripted LLM response: a tool call when Tool != "",
// otherwise a text reply.
type scriptedRound struct {
        Tool string
        Args string
        Text string
}

// scriptedLLMServer replays rounds in order; beyond the script it answers
// with a plain text "done".
func scriptedLLMServer(rounds []scriptedRound) (*httptest.Server, *atomic.Int32) {
        var requests atomic.Int32
        return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                w.Header().Set("Content-Type", "text/event-stream")
                n := int(requests.Add(1))
                var body string
                if n <= len(rounds) {
                        round := rounds[n-1]
                        if round.Tool != "" {
                                body = sseToolCallChunk(fmt.Sprintf("call_%d", n), round.Tool, round.Args)
                        } else {
                                body = sseTextChunk(round.Text)
                        }
                } else {
                        body = sseTextChunk("done")
                }
                _, _ = fmt.Fprint(w, body)
                _, _ = fmt.Fprint(w, "data: [DONE]\n\n")
        })), &requests
}

// sequencedBashTool returns a bash stub that pops scripted outputs in
// order (goroutine-safe; extra calls repeat the last output).
func sequencedBashTool(outputs ...string) tools.Tool {
        var mu sync.Mutex
        var seq int
        return tools.Tool{
                Definition: llm.ToolDefinition{Name: "bash"},
                Run: func(context.Context, json.RawMessage) (tools.Result, error) {
                        mu.Lock()
                        defer mu.Unlock()
                        out := ""
                        if seq < len(outputs) {
                                out = outputs[seq]
                        } else if len(outputs) > 0 {
                                out = outputs[len(outputs)-1]
                        }
                        seq++
                        return tools.Result{Content: out, Output: out}, nil
                },
        }
}

func newZeroRankEngine(t *testing.T, serverURL string, bash tools.Tool, opts ...EngineOption) *Engine {
        t.Helper()
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        all := append([]EngineOption{
                WithGate(permission.AllowAll{}),
                WithTools([]tools.Tool{bash}),
        }, opts...)
        engine, err := NewEngine(llm.ModelConfig{Name: "fake", BaseURL: serverURL, APIKey: "x"}, sess, all...)
        require.NoError(t, err)
        return engine
}

func drainLoop(t *testing.T, engine *Engine, prompt string) error {
        t.Helper()
        var lastErr error
        for ev, err := range engine.Loop(t.Context(), prompt, LoopOpts{}) {
                _ = ev
                if err != nil {
                        lastErr = err
                        break
                }
        }
        return lastErr
}

func userNotes(engine *Engine, marker string) []string {
        var notes []string
        for _, msg := range engine.session.BuildContext() {
                if msg.Role == llm.RoleUser && strings.Contains(msg.Content, marker) {
                        notes = append(notes, msg.Content)
                }
        }
        return notes
}

// TestZeroRankBlocksPrematureDoneThenCertifies is the Phase 3 centerpiece:
// the model declares go-build, breaks it, tries to stop — the harness
// re-opens the turn with the residual set; the model fixes it, stops again,
// and the harness certifies completion.
func TestZeroRankBlocksPrematureDoneThenCertifies(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "contract", Args: `{"checks":["go-build"]}`},
                {Tool: "bash", Args: `{"command":"go build ./..."}`},     // fails
                {Text: "done"},                                            // blocked
                {Tool: "bash", Args: `{"command":"go build ./..."}`},     // passes
                {Text: "done"},                                            // certified
        })
        defer server.Close()

        bash := sequencedBashTool("\n(exit error: exit status 1)", "")
        engine := newZeroRankEngine(t, server.URL, bash)

        require.NoError(t, drainLoop(t, engine, "make it build"))

        residual := userNotes(engine, "[contract] the turn cannot end yet")
        require.Len(t, residual, 1, "exactly one residual injection")
        assert.Contains(t, residual[0], "verification round 1 of 2")
        assert.Contains(t, residual[0], "FAILING: go-build")

        certs := userNotes(engine, "[contract] turn certified complete")
        require.Len(t, certs, 1)
        assert.Contains(t, certs[0], "all 1 declared checks passing: go-build")

        assert.Equal(t, int32(5), requests.Load(), "five LLM rounds: declare, fail, blocked stop, fix, certified stop")
}

// TestZeroRankBudgetExhaustedStopsTurn: a model that never fixes the check
// burns its verification budget and the turn stops with the residual set
// surfaced — never certified.
func TestZeroRankBudgetExhaustedStopsTurn(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "contract", Args: `{"checks":["go-build"]}`},
                {Tool: "bash", Args: `{"command":"go build ./..."}`}, // fails
                {Text: "done"},                                        // round 1/2
                {Text: "done"},                                        // round 2/2
        })
        defer server.Close()

        bash := sequencedBashTool("\n(exit error: exit status 1)")
        engine := newZeroRankEngine(t, server.URL, bash)

        require.NoError(t, drainLoop(t, engine, "make it build"))

        residual := userNotes(engine, "[contract] the turn cannot end yet")
        require.Len(t, residual, 2, "both continuation rounds fire")
        assert.Contains(t, residual[0], "verification round 1 of 2")
        assert.Contains(t, residual[1], "verification round 2 of 2")

        exhausted := userNotes(engine, "[contract] stopping this turn")
        require.Len(t, exhausted, 1)
        assert.Contains(t, exhausted[0], "verification budget (2 extra rounds) exhausted")
        assert.Contains(t, exhausted[0], "NOT certified complete")

        assert.Empty(t, userNotes(engine, "turn certified complete"))
        assert.Equal(t, int32(5), requests.Load())
}

// TestZeroRankNoContractEndsFreely: without a declared contract the
// harness cannot guess done-criteria — a failing check alone must not
// block the turn.
func TestZeroRankNoContractEndsFreely(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "bash", Args: `{"command":"go build ./..."}`},
                {Text: "done"},
        })
        defer server.Close()

        engine := newZeroRankEngine(t, server.URL, sequencedBashTool("\n(exit error: exit status 1)"))

        require.NoError(t, drainLoop(t, engine, "poke it"))
        assert.Empty(t, userNotes(engine, "[contract]"), "no contract, no zero-rank notes")
        assert.Equal(t, int32(2), requests.Load())
}

// TestZeroRankDisabledOption: WithZeroRankBudget(0) turns the gate off —
// the model's done stands even with a declared, failing contract.
func TestZeroRankDisabledOption(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "contract", Args: `{"checks":["go-build"]}`},
                {Tool: "bash", Args: `{"command":"go build ./..."}`},
                {Text: "done"},
        })
        defer server.Close()

        engine := newZeroRankEngine(t, server.URL,
                sequencedBashTool("\n(exit error: exit status 1)"),
                WithZeroRankBudget(0))
        assert.Equal(t, 0, engine.ZeroRankBudget())

        require.NoError(t, drainLoop(t, engine, "make it build"))
        assert.Empty(t, userNotes(engine, "[contract]"), "disabled gate stays silent")
        assert.Equal(t, int32(3), requests.Load())
}

// TestContractResetsBetweenTurns: each user prompt starts a fresh contract
// — turn one declares and certifies; turn two declares nothing and ends
// freely.
func TestContractResetsBetweenTurns(t *testing.T) {
        server1, _ := scriptedLLMServer([]scriptedRound{
                {Tool: "contract", Args: `{"checks":["go-build"]}`},
                {Text: "done"}, // certified: go-build evidence seeded below
        })
        defer server1.Close()

        // Seed passing evidence before the turn so the contract certifies on
        // the first stop attempt.
        engine := newZeroRankEngine(t, server1.URL, sequencedBashTool(""))
        engine.EvidenceLedger().Observe("go build ./...", "", false, time.Now())

        require.NoError(t, drainLoop(t, engine, "first"))
        require.Len(t, userNotes(engine, "turn certified complete"), 1)
        require.Equal(t, []string{"go-build"}, engine.TurnContract())

        server2, requests := scriptedLLMServer([]scriptedRound{
                {Text: "done"},
        })
        defer server2.Close()

        // Second turn, fresh server: rebind the model, no contract call.
        engine.SetModel(llm.ModelConfig{Name: "fake", BaseURL: server2.URL, APIKey: "x"})
        require.NoError(t, drainLoop(t, engine, "second"))
        assert.Empty(t, engine.TurnContract(), "contract resets per turn")
        assert.Equal(t, int32(1), requests.Load())
        notesAfterSecond := userNotes(engine, "[contract]")
        assert.Len(t, notesAfterSecond, 1, "only the first turn's certificate remains")
}

// TestZeroRankNoEvidenceBlocksToo: a declared check that never ran is
// residual — "existing but unchecked = not done" made mechanical.
func TestZeroRankNoEvidenceBlocksToo(t *testing.T) {
        server, _ := scriptedLLMServer([]scriptedRound{
                {Tool: "contract", Args: `{"checks":["go-vet"]}`},
                {Text: "done"},
                {Tool: "bash", Args: `{"command":"go vet ./..."}`},
                {Text: "done"},
        })
        defer server.Close()

        engine := newZeroRankEngine(t, server.URL, sequencedBashTool(""))

        require.NoError(t, drainLoop(t, engine, "vet it"))
        blocked := userNotes(engine, "[contract] the turn cannot end yet")
        require.Len(t, blocked, 1)
        assert.Contains(t, blocked[0], "NO EVIDENCE: go-vet")
        require.Len(t, userNotes(engine, "turn certified complete"), 1)
}
