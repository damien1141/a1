package agent

import (
        "context"
        "encoding/json"
        "strings"
        "sync/atomic"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/tools"
)

// Budget contracting (Phase 5): the model proposes a tool-round budget via
// the `budget` tool, the harness holds it. These tests pin the state
// machine, the declaration surface, and the loop-level firing semantics:
// grace round → extend or wrap up, second overrun → terminal spend report,
// and total inertness for turns that never declare (chat safety).

// --- turnBudgetState ----------------------------------------------------

func TestTurnBudgetDeclareTripReset(t *testing.T) {
        var b turnBudgetState

        // Nothing declared: never trips, stillSpent is false.
        fired, grace := b.tripped(10)
        assert.False(t, fired)
        assert.False(t, grace)
        assert.False(t, b.stillSpent(10))

        // Declare 3 rounds from round 0 → ceiling 3.
        ceiling, declarations := b.declare(0, 3)
        assert.Equal(t, 3, ceiling)
        assert.Equal(t, 1, declarations)

        // Under the ceiling: no firing.
        fired, _ = b.tripped(2)
        assert.False(t, fired)

        // At the ceiling: fires with grace (the re-open), consuming it.
        fired, grace = b.tripped(3)
        assert.True(t, fired)
        assert.True(t, grace)
        assert.True(t, b.stillSpent(4), "past the ceiling with no extension: still spent")

        // Second firing without a new declaration: terminal.
        fired, grace = b.tripped(4)
        assert.True(t, fired)
        assert.False(t, grace)

        // A fresh declaration re-arms the re-open and moves the ceiling.
        ceiling, declarations = b.declare(4, 2)
        assert.Equal(t, 6, ceiling)
        assert.Equal(t, 2, declarations)
        fired, grace = b.tripped(6)
        assert.True(t, fired)
        assert.True(t, grace)
        assert.False(t, b.stillSpent(5), "ceiling moved past usage: not spent anymore")

        // Snapshot reports the full state for the spend report.
        c, d, r := b.snapshot()
        assert.Equal(t, 6, c)
        assert.Equal(t, 2, d)
        assert.Equal(t, 2, r)

        // Reset empties everything.
        b.reset()
        c, d, r = b.snapshot()
        assert.Zero(t, c)
        assert.Zero(t, d)
        assert.Zero(t, r)
}

// --- the budget tool -----------------------------------------------------

func TestBudgetToolRegisteredAndDeclare(t *testing.T) {
        engine := newContractTestEngine(t)
        assert.True(t, engine.HasTool("budget"), "budget tool must be registered on every engine")

        msg := runToolCall(t, engine.executor, "budget", `{"rounds":3}`)
        assert.Contains(t, msg.Content, "Budget held by the harness: 3 more tool rounds")
        assert.Contains(t, msg.Content, "rounds used: 0")
        assert.Contains(t, msg.Content, "ceiling: 3")
        assert.Contains(t, msg.Content, "one re-open")
        assert.Contains(t, msg.Content, "No contract declared")
        assert.Equal(t, 3, engine.TurnBudgetCeiling())
}

func TestBudgetToolDeclareSingularPlural(t *testing.T) {
        engine := newContractTestEngine(t)
        msg := runToolCall(t, engine.executor, "budget", `{"rounds":1}`)
        assert.Contains(t, msg.Content, "1 more tool round (rounds used: 0 → ceiling: 1)")
}

func TestBudgetToolExtensionEcho(t *testing.T) {
        engine := newContractTestEngine(t)
        runToolCall(t, engine.executor, "budget", `{"rounds":2}`)
        // Simulate the loop's live counter advancing before the extension.
        engine.toolRoundsLive.Store(5)
        msg := runToolCall(t, engine.executor, "budget", `{"rounds":4}`)
        assert.Contains(t, msg.Content, "rounds used: 5 → ceiling: 9")
        assert.Contains(t, msg.Content, "extension #1")
        assert.Equal(t, 9, engine.TurnBudgetCeiling())
}

func TestBudgetToolBadArgs(t *testing.T) {
        engine := newContractTestEngine(t)
        for _, args := range []string{`{"rounds":0}`, `{"rounds":-3}`, `{}`, `{invalid`} {
                msg := runToolCall(t, engine.executor, "budget", args)
                assert.Contains(t, msg.Content, "budget: arguments must be", "args %s refused", args)
        }
        assert.Equal(t, 0, engine.TurnBudgetCeiling(), "refused declarations hold no budget")
}

func TestBudgetToolReplyShowsContractStanding(t *testing.T) {
        engine := newContractTestEngine(t)

        // Declared contract, failing check → the budget reply warns.
        runToolCall(t, engine.executor, "contract", `{"checks":["go-build"]}`)
        engine.EvidenceLedger().Observe("go build ./...", "\n(exit error: exit status 1)", true, time.Now())
        msg := runToolCall(t, engine.executor, "budget", `{"rounds":2}`)
        assert.Contains(t, msg.Content, "Contract: 1 check held, 1 not green")
        assert.Contains(t, msg.Content, "leaves the work uncertified")

        // Green contract → the budget reply says so.
        engine.EvidenceLedger().Observe("go build ./...", "", false, time.Now())
        msg = runToolCall(t, engine.executor, "budget", `{"rounds":2}`)
        assert.Contains(t, msg.Content, "Contract: all 1 declared checks green")
}

func TestBudgetToolEnforcementDisabledEcho(t *testing.T) {
        engine := newContractTestEngine(t, WithBudgetContracts(false))
        assert.False(t, engine.BudgetContracts())

        msg := runToolCall(t, engine.executor, "budget", `{"rounds":3}`)
        assert.Contains(t, msg.Content, "enforcement is disabled on this engine")
        assert.Contains(t, msg.Content, "will not stop the turn")
        // The declaration is still recorded — visibility without the binding.
        assert.Equal(t, 3, engine.TurnBudgetCeiling())
}

func TestBudgetToolIsReadClassed(t *testing.T) {
        engine := newContractTestEngine(t)
        calls := []llm.ToolCall{
                {ID: "b1", Type: "function", Function: llm.Function{Name: "budget", Arguments: `{"rounds":2}`}},
                {ID: "c1", Type: "function", Function: llm.Function{Name: "contract", Arguments: `{"checks":["go-build"]}`}},
        }
        assert.False(t, engine.executor.BatchMutates(calls),
                "a budget/contract batch is read-classed: no spurious verdict-gate baseline")
}

// --- loop level ----------------------------------------------------------

// TestBudgetGateDeclarationBatchExemption pins the exemption semantics
// precisely: pure declaration batches never fire the gate (the extend path
// must stay usable after a firing), while a declaration paired with work in
// one batch still fires — pairing budget(+1) with every work round cannot
// bypass the ceiling.
func TestBudgetGateDeclarationBatchExemption(t *testing.T) {
        // budgetDeclarationBatch: only budget/contract batches qualify.
        assert.False(t, budgetDeclarationBatch(nil))
        assert.True(t, budgetDeclarationBatch([]llm.ToolCall{
                {Function: llm.Function{Name: "budget", Arguments: `{"rounds":1}`}},
        }))
        assert.True(t, budgetDeclarationBatch([]llm.ToolCall{
                {Function: llm.Function{Name: "contract", Arguments: `{"checks":["go-build"]}`}},
        }))
        assert.True(t, budgetDeclarationBatch([]llm.ToolCall{
                {Function: llm.Function{Name: "budget", Arguments: `{"rounds":1}`}},
                {Function: llm.Function{Name: "contract", Arguments: `{"checks":["go-build"]}`}},
        }))
        assert.False(t, budgetDeclarationBatch([]llm.ToolCall{
                {Function: llm.Function{Name: "budget", Arguments: `{"rounds":1}`}},
                {Function: llm.Function{Name: "bash", Arguments: `{"command":"echo"}`}},
        }), "declaration + work is a work batch")
        assert.False(t, budgetDeclarationBatch([]llm.ToolCall{
                {Function: llm.Function{Name: "read", Arguments: `{}`}},
        }), "reads are not declarations — the budget gates rounds, not just mutations")

        // The gate: pure declarations are exempt even past the ceiling; mixed
        // batches fire.
        engine := newContractTestEngine(t)
        _, _ = engine.turnBudget.declare(0, 1) // ceiling 1

        decl := []llm.ToolCall{{Function: llm.Function{Name: "budget", Arguments: `{"rounds":1}`}}}
        assert.Equal(t, budgetAction{}, engine.budgetGate(5, decl),
                "a pure declaration round never fires, however far past the ceiling")

        mixed := []llm.ToolCall{
                {Function: llm.Function{Name: "budget", Arguments: `{"rounds":1}`}},
                {Function: llm.Function{Name: "bash", Arguments: `{"command":"echo"}`}},
        }
        act := engine.budgetGate(1, mixed)
        assert.True(t, act.Grace, "a mixed batch at the ceiling consumes the re-open, not the exemption")
        act = engine.budgetGate(2, mixed)
        assert.True(t, act.Terminal, "pairing a declaration with work cannot bypass the ceiling")
}

// countingBashTool is a bash-named stub that counts executions and returns a
// fixed output. Commands are plain echoes (or go builds) so the evidence
// ledger records exactly what the script wants.
func countingBashTool(runs *atomic.Int32, output string) tools.Tool {
        return tools.Tool{
                Definition: llm.ToolDefinition{Name: "bash"},
                Run: func(context.Context, json.RawMessage) (tools.Result, error) {
                        runs.Add(1)
                        return tools.Result{Content: output, Output: output}, nil
                },
        }
}

func newBudgetEngine(t *testing.T, serverURL string, bash tools.Tool, opts ...EngineOption) *Engine {
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

// TestBudgetInertWithoutDeclaration is the chat-safety pin: a turn that
// never declares a budget behaves exactly as before Phase 5 — every round
// runs, no [budget] notes ever land.
func TestBudgetInertWithoutDeclaration(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "bash", Args: `{"command":"echo 1"}`},
                {Tool: "bash", Args: `{"command":"echo 2"}`},
                {Tool: "bash", Args: `{"command":"echo 3"}`},
                {Tool: "bash", Args: `{"command":"echo 4"}`},
                {Text: "done"},
        })
        defer server.Close()

        var runs atomic.Int32
        engine := newBudgetEngine(t, server.URL, countingBashTool(&runs, "ok"))

        require.NoError(t, drainLoop(t, engine, "chat away"))
        assert.Equal(t, int32(4), runs.Load(), "no declaration: every round runs")
        assert.Equal(t, int32(5), requests.Load())
        assert.Empty(t, userNotes(engine, "[budget]"), "no budget notes without a declaration")
        assert.Equal(t, 0, engine.TurnBudgetCeiling())
}

// TestBudgetFireGraceThenExtend is the Phase 5 centerpiece: the ceiling
// fires, the round runs on the turn's re-open with a note, the model
// extends with a visible declaration, and work continues to a clean end.
func TestBudgetFireGraceThenExtend(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "budget", Args: `{"rounds":2}`},         // r1: ceiling 3 (rounds used 1)
                {Tool: "bash", Args: `{"command":"echo a"}`},   // r2: rounds 2
                {Tool: "bash", Args: `{"command":"echo b"}`},   // r3: rounds 3 == ceiling
                {Tool: "bash", Args: `{"command":"echo c"}`},   // r4: grace round → 4, note
                {Tool: "budget", Args: `{"rounds":2}`},         // r5: extension → ceiling 7
                {Tool: "bash", Args: `{"command":"echo d"}`},   // r6: rounds 6
                {Text: "done"}, // r7: clean end
        })
        defer server.Close()

        var runs atomic.Int32
        engine := newBudgetEngine(t, server.URL, countingBashTool(&runs, "ok"))

        require.NoError(t, drainLoop(t, engine, "do the thing"))

        // Rounds r2, r3, r4 (grace), r6 executed; the two budget declarations
        // executed as tool rounds too but are not bash calls.
        assert.Equal(t, int32(4), runs.Load())
        assert.Equal(t, int32(7), requests.Load())

        // Exactly one grace note, no terminal stop.
        grace := userNotes(engine, "[budget] declared tool-round budget reached")
        require.Len(t, grace, 1, "exactly one grace note")
        assert.Contains(t, grace[0], "ran on this turn's re-open")
        assert.Contains(t, grace[0], "rounds used: 4")
        assert.Contains(t, grace[0], "ceiling: 3")
        assert.Contains(t, grace[0], "extensions: 0")
        assert.Empty(t, userNotes(engine, "[budget] declared tool-round budget exhausted"),
                "extending during the re-open must not stop the turn")

        // The extension priced itself against the live round counter (round 5).
        assert.Equal(t, 7, engine.TurnBudgetCeiling())
}

// TestBudgetFireTwiceWithoutExtensionStopsTurn: the re-open is one shot per
// declaration. A model that keeps calling tools past its budget without
// extending gets its pending calls rejected and a terminal spend report.
func TestBudgetFireTwiceWithoutExtensionStopsTurn(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "budget", Args: `{"rounds":1}`},       // r1: ceiling 2
                {Tool: "bash", Args: `{"command":"echo a"}`}, // r2: rounds 2 == ceiling
                {Tool: "bash", Args: `{"command":"echo b"}`}, // r3: grace → 3, note
                {Tool: "bash", Args: `{"command":"echo c"}`}, // r4: terminal — rejected
        })
        defer server.Close()

        var runs atomic.Int32
        engine := newBudgetEngine(t, server.URL, countingBashTool(&runs, "ok"))

        require.NoError(t, drainLoop(t, engine, "do the thing"), "a budget stop is a clean stop, never an error")
        assert.Equal(t, int32(2), runs.Load(), "r2 and the grace round ran; the terminal round's calls did not")
        assert.Equal(t, int32(4), requests.Load())

        // The pending call came back rejected, not executed.
        rejected := 0
        for _, msg := range engine.session.BuildContext() {
                if msg.Role == llm.RoleTool && strings.Contains(msg.Content, "[budget] rejected") {
                        rejected++
                }
        }
        assert.Equal(t, 1, rejected, "the terminal round's tool call returns a rejected result")

        terminal := userNotes(engine, "[budget] declared tool-round budget exhausted")
        require.Len(t, terminal, 1)
        assert.Contains(t, terminal[0], "stopping this turn")
        assert.Contains(t, terminal[0], "rounds used: 3")
        assert.Contains(t, terminal[0], "ceiling: 2")
        assert.Contains(t, terminal[0], "extensions: 0")
        assert.Contains(t, terminal[0], "re-opens used: 1")
        assert.Contains(t, terminal[0], "1 pending tool call")
        assert.Contains(t, terminal[0], "No contract declared")

        // The session stays consistent: every tool call in the tree has a result.
        assertSessionToolCallsAnswered(t, engine)
}

// TestBudgetStopSurfacesContractResidual: a terminal budget stop with a
// declared contract carries the residual set — an out-of-budget stop is
// never mistaken for a certified one.
func TestBudgetStopSurfacesContractResidual(t *testing.T) {
        server, _ := scriptedLLMServer([]scriptedRound{
                {Tool: "contract", Args: `{"checks":["go-build"]}`},
                {Tool: "budget", Args: `{"rounds":1}`},               // ceiling 3
                {Tool: "bash", Args: `{"command":"go build ./..."}`}, // fails → rounds 3
                {Tool: "bash", Args: `{"command":"go build ./..."}`}, // grace → 4, note
                {Tool: "bash", Args: `{"command":"go build ./..."}`}, // terminal
        })
        defer server.Close()

        var runs atomic.Int32
        bash := countingBashTool(&runs, "\n(exit error: exit status 1)")
        engine := newBudgetEngine(t, server.URL, bash)

        require.NoError(t, drainLoop(t, engine, "make it build"))

        terminal := userNotes(engine, "[budget] declared tool-round budget exhausted")
        require.Len(t, terminal, 1)
        assert.Contains(t, terminal[0], "1 of 1 declared checks not green")
        assert.Contains(t, terminal[0], "NOT certified complete")
        assert.Contains(t, terminal[0], "FAILING: go-build")

        // The grace note also carried the residual warning.
        grace := userNotes(engine, "[budget] declared tool-round budget reached")
        require.Len(t, grace, 1)
        assert.Contains(t, grace[0], "Declared checks not green")
}

// TestBudgetWrapUpAfterGraceCertifies: after a firing, wrapping up with a
// final answer ends the turn through the zero-rank gate — a green contract
// still certifies despite the exhausted budget.
func TestBudgetWrapUpAfterGraceCertifies(t *testing.T) {
        server, _ := scriptedLLMServer([]scriptedRound{
                {Tool: "contract", Args: `{"checks":["go-build"]}`},
                {Tool: "budget", Args: `{"rounds":1}`},               // ceiling 3
                {Tool: "bash", Args: `{"command":"go build ./..."}`}, // passes → rounds 3
                {Tool: "bash", Args: `{"command":"go build ./..."}`}, // grace → 4, note
                {Text: "done"}, // wrap up → zero-rank certifies
        })
        defer server.Close()

        var runs atomic.Int32
        engine := newBudgetEngine(t, server.URL, countingBashTool(&runs, ""))

        require.NoError(t, drainLoop(t, engine, "make it build"))

        assert.Equal(t, int32(2), runs.Load())
        certs := userNotes(engine, "[contract] turn certified complete")
        require.Len(t, certs, 1)
        assert.Contains(t, certs[0], "all 1 declared checks passing: go-build")
        assert.Empty(t, userNotes(engine, "[budget] declared tool-round budget exhausted"),
                "wrapping up after the re-open is a clean end, not a budget stop")

        // The grace note landed (budget state is visible) but without residual
        // warnings — the contract was green at firing time.
        grace := userNotes(engine, "[budget] declared tool-round budget reached")
        require.Len(t, grace, 1)
        assert.NotContains(t, grace[0], "not green")
}

// TestBudgetEnforcementDisabledNeverStops: with the toggle off the budget is
// recorded and echoed but inert at the loop level.
func TestBudgetEnforcementDisabledNeverStops(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "budget", Args: `{"rounds":1}`},
                {Tool: "bash", Args: `{"command":"echo a"}`},
                {Tool: "bash", Args: `{"command":"echo b"}`},
                {Tool: "bash", Args: `{"command":"echo c"}`},
                {Text: "done"},
        })
        defer server.Close()

        var runs atomic.Int32
        engine := newBudgetEngine(t, server.URL, countingBashTool(&runs, "ok"), WithBudgetContracts(false))

        require.NoError(t, drainLoop(t, engine, "do the thing"))
        assert.Equal(t, int32(3), runs.Load(), "enforcement off: the ceiling never binds")
        assert.Equal(t, int32(5), requests.Load())
        assert.Empty(t, userNotes(engine, "[budget]"), "no budget notes at all")
        assert.Equal(t, 2, engine.TurnBudgetCeiling(), "declaration still recorded for the record")
}

// TestBudgetDeclarationPricesAgainstLiveRounds: a mid-loop declaration
// prices itself against the rounds already used, not from zero.
func TestBudgetDeclarationPricesAgainstLiveRounds(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "bash", Args: `{"command":"echo a"}`}, // r1: rounds 1
                {Tool: "budget", Args: `{"rounds":2}`},       // r2: ceiling 2+2=4
                {Tool: "bash", Args: `{"command":"echo b"}`}, // r3: rounds 3
                {Tool: "bash", Args: `{"command":"echo c"}`}, // r4: rounds 4 == ceiling
                {Tool: "bash", Args: `{"command":"echo d"}`}, // r5: grace → 5, note
                {Text: "done"}, // r6: clean end
        })
        defer server.Close()

        var runs atomic.Int32
        engine := newBudgetEngine(t, server.URL, countingBashTool(&runs, "ok"))

        require.NoError(t, drainLoop(t, engine, "do the thing"))
        assert.Equal(t, int32(4), runs.Load())
        assert.Equal(t, int32(6), requests.Load())
        assert.Equal(t, 4, engine.TurnBudgetCeiling(), "2 more rounds from round 2, not from zero")

        grace := userNotes(engine, "[budget] declared tool-round budget reached")
        require.Len(t, grace, 1)
        assert.Contains(t, grace[0], "rounds used: 5")
        assert.Contains(t, grace[0], "ceiling: 4")
}

// TestBudgetFreshTurnStartsFreshBudget: the budget resets between turns —
// a spent budget on one prompt never bleeds into the next. Both turns run
// against one scripted server: requests 1-4 are turn 1 (terminal stop),
// requests 5-8 are turn 2 on the same engine and session.
func TestBudgetFreshTurnStartsFreshBudget(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                // Turn 1: declare 1, burn it, stop terminally.
                {Tool: "budget", Args: `{"rounds":1}`},
                {Tool: "bash", Args: `{"command":"echo a"}`},
                {Tool: "bash", Args: `{"command":"echo b"}`}, // grace
                {Tool: "bash", Args: `{"command":"echo c"}`}, // terminal
                // Turn 2: no declaration, three free rounds, done.
                {Tool: "bash", Args: `{"command":"echo x"}`},
                {Tool: "bash", Args: `{"command":"echo y"}`},
                {Tool: "bash", Args: `{"command":"echo z"}`},
                {Text: "done"},
        })
        defer server.Close()

        var runs atomic.Int32
        engine := newBudgetEngine(t, server.URL, countingBashTool(&runs, "ok"))

        require.NoError(t, drainLoop(t, engine, "first task"))
        require.NotEmpty(t, userNotes(engine, "[budget] declared tool-round budget exhausted"))
        assert.Equal(t, int32(2), runs.Load())

        require.NoError(t, drainLoop(t, engine, "second task"))
        assert.Equal(t, int32(5), runs.Load(), "turn 2 runs free: three more bash rounds")
        assert.Equal(t, int32(8), requests.Load())
        assert.Equal(t, 0, engine.TurnBudgetCeiling(), "the spent budget did not survive the turn")
}

// --- helpers -------------------------------------------------------------

// assertSessionToolCallsAnswered verifies no assistant message in the session
// tree holds a tool call without a following tool result — the consistency
// invariant the rejected-batch synthesis must preserve.
func assertSessionToolCallsAnswered(t *testing.T, engine *Engine) {
        t.Helper()
        msgs := engine.session.BuildContext()
        answered := map[string]bool{}
        for _, msg := range msgs {
                for _, call := range msg.ToolCalls {
                        answered[call.ID] = false
                }
                if msg.Role == llm.RoleTool && msg.ToolCallID != "" {
                        answered[msg.ToolCallID] = true
                }
        }
        for id, ok := range answered {
                assert.True(t, ok, "tool call %s must have a result", id)
        }
}
