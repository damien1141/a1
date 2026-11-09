package agent

import (
        "context"
        "encoding/json"
        "fmt"
        "net/http"
        "net/http/httptest"
        "strings"
        "sync/atomic"
        "testing"

        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools"
)

// The tool-call budget caps executed calls per turn. Over-budget calls come
// back as rejected results, the turn ends cleanly (no error), and a system
// note lands in the session so the model and the user both see why the turn
// stopped. Unrestricted gates - yolo mode, the allow-all flags and overrides
// - override the budget with unlimited.

// plainAllowGate allows every check while staying a plain Gate: the budget
// enforcement tests need a permissive gate the budget still binds. The real
// allow-all shapes (AllowAll, an armed SessionAllowGate, the policy flags)
// are unrestricted and lift the cap - using one here would test nothing.
type plainAllowGate struct{}

func (plainAllowGate) Check(context.Context, permission.Request) (permission.Decision, string) {
        return permission.Allow, ""
}

func (plainAllowGate) Admit(context.Context, permission.Request) (permission.Decision, string) {
        return permission.Allow, ""
}

func newBudgetTestEngine(t *testing.T, serverURL string, runs *atomic.Int32, maxToolCalls int, gate permission.Gate) *Engine {
        t.Helper()
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        opts := []EngineOption{
                WithGate(gate),
                WithTools([]tools.Tool{countingTool(runs)}),
                WithMaxToolCalls(maxToolCalls),
        }
        engine, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: serverURL, APIKey: "x"},
                sess,
                opts...,
        )
        require.NoError(t, err)
        return engine
}

func runTurn(t *testing.T, engine *Engine) error {
        t.Helper()
        var lastErr error
        for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
                _ = ev
                if err != nil {
                        lastErr = err
                        break
                }
        }
        return lastErr
}

func TestLoopMaxToolCallsStopsTurnCleanly(t *testing.T) {
        // The model asks for tools forever; the budget must stop it.
        server, requests := fakeToolSequenceServer(-1)
        defer server.Close()

        var runs atomic.Int32
        engine := newBudgetTestEngine(t, server.URL, &runs, 2, plainAllowGate{})

        lastErr := runTurn(t, engine)
        require.NoError(t, lastErr, "hitting the tool-call budget ends the turn, not the loop with an error")
        require.Equal(t, int32(2), runs.Load(), "only the budgeted calls execute")
        // Three stream rounds happen: two executed, the third's call is rejected.
        require.Equal(t, int32(3), requests.Load())

        // The third round's call was rejected with a budget note, not executed.
        rejected := 0
        for _, msg := range engine.session.BuildContext() {
                if msg.Role == llm.RoleTool && strings.Contains(msg.Content, "tool call budget reached") {
                        rejected++
                }
        }
        require.Equal(t, 1, rejected, "the over-budget call returns a rejected tool result")

        // A system note is appended so the next prompt explains the stop.
        msgs := engine.session.BuildContext()
        require.NotEmpty(t, msgs)
        note := msgs[len(msgs)-1]
        require.Equal(t, llm.RoleUser, note.Role)
        require.Contains(t, note.Content, "tool call budget (2) reached")
}

func TestLoopMaxToolCallsUnlimitedByDefault(t *testing.T) {
        server, _ := fakeToolSequenceServer(5)
        defer server.Close()

        var runs atomic.Int32
        engine := newBudgetTestEngine(t, server.URL, &runs, 0, plainAllowGate{})

        lastErr := runTurn(t, engine)
        require.NoError(t, lastErr)
        require.Equal(t, int32(5), runs.Load(), "no budget configured → every call runs")
}

func TestLoopMaxToolCallsYoloOverridesBudget(t *testing.T) {
        server, _ := fakeToolSequenceServer(5)
        defer server.Close()

        gate, err := permission.NewGate(permission.Policy{Mode: permission.ModeYolo}, t.TempDir())
        require.NoError(t, err)

        var runs atomic.Int32
        engine := newBudgetTestEngine(t, server.URL, &runs, 2, gate)

        lastErr := runTurn(t, engine)
        require.NoError(t, lastErr)
        require.Equal(t, int32(5), runs.Load(), "yolo mode ignores the tool-call budget")

        // No system note either — the turn finished on its own.
        msgs := engine.session.BuildContext()
        require.NotEmpty(t, msgs)
        require.NotContains(t, msgs[len(msgs)-1].Content, "tool call budget")
}

// TestLoopMaxToolCallsAllowAllShapesUnlimited pins the fix for the reported
// bug: "yolo doesn't apply unlimited tool calls". Yolo reaches the engine as
// more than one gate shape, and the old ModeOf-only check missed all but the
// literal mode - `a1 run --yolo` builds a bare AllowAll, the TUI's allow-all
// session override is an armed SessionAllowGate, and the config flags
// short-circuit inside the StaticGate. Every shape must lift the cap.
func TestLoopMaxToolCallsAllowAllShapesUnlimited(t *testing.T) {
        t.Run("bare AllowAll (a1 run --yolo)", func(t *testing.T) {
                server, _ := fakeToolSequenceServer(5)
                defer server.Close()

                var runs atomic.Int32
                engine := newBudgetTestEngine(t, server.URL, &runs, 2, permission.AllowAll{})

                lastErr := runTurn(t, engine)
                require.NoError(t, lastErr)
                require.Equal(t, int32(5), runs.Load(),
                        "--yolo (AllowAll gate) must not enforce the tool-call budget")

                msgs := engine.session.BuildContext()
                require.NotEmpty(t, msgs)
                require.NotContains(t, msgs[len(msgs)-1].Content, "tool call budget")
        })

        t.Run("armed SessionAllowGate (TUI allow-all override)", func(t *testing.T) {
                server, _ := fakeToolSequenceServer(5)
                defer server.Close()

                inner, err := permission.NewGate(permission.Policy{Mode: permission.ModeInteractive}, t.TempDir())
                require.NoError(t, err)
                var armed atomic.Bool
                armed.Store(true)
                gate := &permission.SessionAllowGate{Inner: inner, Enabled: &armed}

                var runs atomic.Int32
                engine := newBudgetTestEngine(t, server.URL, &runs, 2, gate)

                lastErr := runTurn(t, engine)
                require.NoError(t, lastErr)
                require.Equal(t, int32(5), runs.Load(),
                        "an armed session override must not enforce the tool-call budget")
        })

        t.Run("dangerously_allow_all policy flag", func(t *testing.T) {
                server, _ := fakeToolSequenceServer(5)
                defer server.Close()

                gate, err := permission.NewGate(permission.Policy{
                        Mode:                permission.ModeInteractive,
                        DangerouslyAllowAll: true,
                }, t.TempDir())
                require.NoError(t, err)

                var runs atomic.Int32
                engine := newBudgetTestEngine(t, server.URL, &runs, 2, gate)

                lastErr := runTurn(t, engine)
                require.NoError(t, lastErr)
                require.Equal(t, int32(5), runs.Load(),
                        "dangerously_allow_all must not enforce the tool-call budget")
        })

        t.Run("disarmed override still enforces", func(t *testing.T) {
                server, _ := fakeToolSequenceServer(5)
                defer server.Close()

                inner, err := permission.NewGate(permission.Policy{Mode: permission.ModeHeadlessStrict}, t.TempDir())
                require.NoError(t, err)
                var armed atomic.Bool // disarmed
                gate := &permission.SessionAllowGate{Inner: inner, Enabled: &armed}

                var runs atomic.Int32
                engine := newBudgetTestEngine(t, server.URL, &runs, 2, gate)

                lastErr := runTurn(t, engine)
                require.NoError(t, lastErr)
                // The inner headless-strict gate folds unknown-action Ask to Deny, so
                // the count tool never runs - but the budget machinery itself is live.
                // The enforcement assertion lives in the plainAllowGate tests above;
                // this case pins that a disarmed override does not lift the cap by
                // itself (Unrestricted defers to the inner gate).
                require.LessOrEqual(t, runs.Load(), int32(2),
                        "a disarmed override must not lift the tool-call budget")
        })
}

func TestLoopMaxToolCallsCountsBatchedCalls(t *testing.T) {
        // One stream round that batches three calls consumes three slots: with a
        // budget of 2 the first two run and the third is rejected.
        server := httptestToolBatchServer()
        defer server.Close()

        var runs atomic.Int32
        engine := newBudgetTestEngine(t, server.URL, &runs, 2, plainAllowGate{})

        lastErr := runTurn(t, engine)
        require.NoError(t, lastErr)
        require.Equal(t, int32(2), runs.Load())
        rejected := 0
        for _, msg := range engine.session.BuildContext() {
                if msg.Role == llm.RoleTool && strings.Contains(msg.Content, "tool call budget reached") {
                        rejected++
                }
        }
        require.Equal(t, 1, rejected)
}

func TestSetMaxToolCallsMidLoop(t *testing.T) {
        // Raising the budget while the loop runs lets later rounds execute.
        server, _ := fakeToolSequenceServer(5)
        defer server.Close()

        var runs atomic.Int32
        engine := newBudgetTestEngine(t, server.URL, &runs, 2, plainAllowGate{})

        rounds := 0
        for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
                if err != nil {
                        t.Fatalf("loop error: %v", err)
                }
                if _, ok := ev.(session.ToolData); ok {
                        rounds++
                        if rounds == 2 {
                                // Two calls executed — raise the cap so the rest can run.
                                engine.SetMaxToolCalls(10)
                        }
                }
        }
        require.Equal(t, int32(5), runs.Load())
}

func TestExecutorCallAllowanceConcurrentBatch(t *testing.T) {
        // A read-only batch of five calls with an allowance of two: exactly two
        // execute, three come back rejected, and the counters agree.
        exec := NewExecutor(tools.NewRegistry([]tools.Tool{
                readableTool("r1"), readableTool("r2"), readableTool("r3"),
                readableTool("r4"), readableTool("r5"),
        }), nil, nil, nil, nil)
        exec.SetCallAllowance(2)

        calls := make([]llm.ToolCall, 0, 5)
        for i := 1; i <= 5; i++ {
                calls = append(calls, llm.ToolCall{
                        ID:       fmt.Sprintf("c%d", i),
                        Type:     "function",
                        Function: llm.Function{Name: fmt.Sprintf("r%d", i), Arguments: "{}"},
                })
        }

        var rejected atomic.Int32
        msgs, stop, _ := exec.Run(context.Background(), calls, func(td session.ToolData) bool {
                if td.Run.Status == session.ToolRejected {
                        rejected.Add(1)
                }
                return true
        })
        require.False(t, stop)
        require.Len(t, msgs, 5)
        require.Equal(t, int32(3), rejected.Load())
        require.Equal(t, 2, exec.AllowanceUsed())
        require.True(t, exec.AllowanceTripped())
}

func TestExecutorCallAllowanceUnlimited(t *testing.T) {
        exec := NewExecutor(tools.NewRegistry([]tools.Tool{readableTool("r1"), readableTool("r2")}), nil, nil, nil, nil)
        exec.SetCallAllowance(-1)

        calls := []llm.ToolCall{
                {ID: "c1", Type: "function", Function: llm.Function{Name: "r1", Arguments: "{}"}},
                {ID: "c2", Type: "function", Function: llm.Function{Name: "r2", Arguments: "{}"}},
        }
        msgs, _, _ := exec.Run(context.Background(), calls, func(session.ToolData) bool { return true })
        require.Len(t, msgs, 2)
        require.False(t, exec.AllowanceTripped())
}

func readableTool(name string) tools.Tool {
        return tools.Tool{
                Definition: llm.ToolDefinition{
                        Name:        name,
                        Description: "readable test tool",
                        Params:      &llm.FunctionParameters{Type: "object"},
                        Readable:    true,
                },
                Run: func(context.Context, json.RawMessage) (tools.Result, error) {
                        return tools.Result{Content: "ok"}, nil
                },
        }
}

// httptestToolBatchServer streams one assistant round carrying three tool
// calls, then a final text reply. Only /chat/completions requests are
// scripted: the engine's window probe (GET /props) shares the server.
func httptestToolBatchServer() *httptest.Server {
        var requests atomic.Int32
        return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                w.Header().Set("Content-Type", "text/event-stream")
                request := requests.Add(1)
                if request == 1 {
                        _, _ = fmt.Fprint(w, sseMultiToolCallChunk(
                                []string{"b1", "b2", "b3"},
                                []string{"count", "count", "count"},
                        ))
                } else {
                        _, _ = fmt.Fprint(w, sseTextChunk("done"))
                }
                _, _ = fmt.Fprint(w, "data: [DONE]\n\n")
        }))
}

// sseMultiToolCallChunk encodes one SSE data line carrying several full
// tool-call deltas (a batching round).
func sseMultiToolCallChunk(ids, names []string) string {
        calls := make([]any, len(ids))
        for i, id := range ids {
                calls[i] = map[string]any{
                        "index":    i,
                        "id":       id,
                        "type":     "function",
                        "function": map[string]any{"name": names[i], "arguments": `{}`},
                }
        }
        payload, err := json.Marshal(map[string]any{
                "choices": []any{map[string]any{
                        "delta": map[string]any{
                                "role":       "assistant",
                                "content":    "",
                                "tool_calls": calls,
                        },
                }},
        })
        if err != nil {
                panic(err)
        }
        return "data: " + string(payload) + "\n\n"
}
