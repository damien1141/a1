package agent

import (
        "context"
        "encoding/json"
        "os/exec"
        "strings"
        "sync"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools"
        "github.com/damien1141/a1/internal/tools/scripttool"
)

// This file pins the tool-RPC steal's load-bearing half: every in-script
// tool call (the script tool's RPC server dialing back) traverses the
// normal per-call pipeline — permission gate with ask, evidence and
// mutation recording — instead of a private shortcut around it.

func dispatchRegistry(ran *strings.Builder) tools.Registry {
        return tools.Registry{
                "read": {
                        Definition: llm.ToolDefinition{Name: "read", Readable: true},
                        Run: func(context.Context, json.RawMessage) (tools.Result, error) {
                                ran.WriteString("read;")
                                return tools.Result{Content: "1|alpha\n2|beta"}, nil
                        },
                },
                "write": {
                        Definition: llm.ToolDefinition{Name: "write"},
                        Run: func(_ context.Context, args json.RawMessage) (tools.Result, error) {
                                ran.WriteString("write;")
                                var in struct {
                                        Path string `json:"path"`
                                }
                                _ = json.Unmarshal(args, &in)
                                if in.Path == "" {
                                        return tools.Result{}, assert.AnError
                                }
                                return tools.Result{Content: "wrote " + in.Path}, nil
                        },
                },
        }
}

func TestDispatchScriptRunsRegisteredTool(t *testing.T) {
        var ran strings.Builder
        ex := NewExecutor(dispatchRegistry(&ran), permission.AllowAll{}, nil, nil, nil)

        content, ok := ex.DispatchScript(t.Context(), "read", json.RawMessage(`{"path":"notes.txt"}`))
        assert.True(t, ok)
        assert.Equal(t, "1|alpha\n2|beta", content)
        assert.Contains(t, ran.String(), "read")
}

func TestDispatchScriptToolErrorIsNotOK(t *testing.T) {
        var ran strings.Builder
        ex := NewExecutor(dispatchRegistry(&ran), permission.AllowAll{}, nil, nil, nil)

        // write with no path fails inside the tool — ok=false, error text rides.
        content, ok := ex.DispatchScript(t.Context(), "write", json.RawMessage(`{"content":"x"}`))
        assert.False(t, ok)
        assert.NotEmpty(t, content)
}

func TestDispatchScriptUnknownToolNamesAlternatives(t *testing.T) {
        var ran strings.Builder
        ex := NewExecutor(dispatchRegistry(&ran), permission.AllowAll{}, nil, nil, nil)

        content, ok := ex.DispatchScript(t.Context(), "question", json.RawMessage(`{"q":"hi"}`))
        assert.False(t, ok)
        assert.Contains(t, content, "not available in this session")
        assert.Contains(t, content, "read")
}

func TestDispatchScriptPermissionDenyDoesNotRun(t *testing.T) {
        var ran strings.Builder
        ex := NewExecutor(dispatchRegistry(&ran), fixedGate{dec: permission.Deny, reason: "denied by policy"}, nil, nil, nil)

        content, ok := ex.DispatchScript(t.Context(), "write", json.RawMessage(`{"path":"src/main.go","content":"x"}`))
        assert.False(t, ok)
        assert.Equal(t, "denied by policy", content)
        assert.NotContains(t, ran.String(), "write", "denied call must not run the handler")
}

func TestDispatchScriptAskApprovedAndDenied(t *testing.T) {
        var ran strings.Builder
        var mu sync.Mutex
        asks := 0
        ask := func(context.Context, permission.Request, string) (permission.AskResult, error) {
                mu.Lock()
                defer mu.Unlock()
                asks++
                return permission.AskResult{Approved: true}, nil
        }
        ex := NewExecutor(dispatchRegistry(&ran), fixedGate{dec: permission.Ask, reason: "needs approval"}, ask, nil, nil)

        _, ok := ex.DispatchScript(t.Context(), "write", json.RawMessage(`{"path":"src/main.go","content":"x"}`))
        assert.True(t, ok, "approved ask must run the call")
        assert.Contains(t, ran.String(), "write")

        denier := func(context.Context, permission.Request, string) (permission.AskResult, error) {
                return permission.AskResult{Approved: false, Feedback: "not that file"}, nil
        }
        ex2 := NewExecutor(dispatchRegistry(&ran), fixedGate{dec: permission.Ask, reason: "needs approval"}, denier, nil, nil)
        content, ok := ex2.DispatchScript(t.Context(), "write", json.RawMessage(`{"path":"src/main.go","content":"x"}`))
        assert.False(t, ok, "denied ask must refuse the call")
        assert.Contains(t, content, "rejected by the user with feedback: not that file")
}

func TestDispatchScriptAskWithoutHandlerFailsClosed(t *testing.T) {
        var ran strings.Builder
        ex := NewExecutor(dispatchRegistry(&ran), fixedGate{dec: permission.Ask, reason: "needs approval"}, nil, nil, nil)

        content, ok := ex.DispatchScript(t.Context(), "write", json.RawMessage(`{"path":"a","content":"b"}`))
        assert.False(t, ok, "an Ask decision with nobody to ask must refuse, never run")
        assert.NotEmpty(t, content)
        assert.NotContains(t, ran.String(), "write", "refused call must not run the handler")
}

func TestDispatchScriptRecordsMutationsForNonReadableTools(t *testing.T) {
        var ran strings.Builder
        ex := NewExecutor(dispatchRegistry(&ran), permission.AllowAll{}, nil, nil, nil)

        _, ok := ex.DispatchScript(t.Context(), "write", json.RawMessage(`{"path":"src/main.go","content":"x"}`))
        require.True(t, ok)
        facts := ex.DrainMutations()
        require.Len(t, facts, 1, "an in-script write must trip the verify-on-stop gate")
        assert.Equal(t, "write", facts[0].Tool)
        assert.Equal(t, "src/main.go", facts[0].Path)

        // Readable tools mutate nothing and must not record.
        _, ok = ex.DispatchScript(t.Context(), "read", json.RawMessage(`{"path":"src/main.go"}`))
        require.True(t, ok)
        assert.Empty(t, ex.DrainMutations(), "readable calls never record mutations")
}

func TestDispatchScriptRecordsEvidenceForBash(t *testing.T) {
        var ran strings.Builder
        reg := dispatchRegistry(&ran)
        reg["bash"] = tools.Tool{
                Definition: llm.ToolDefinition{Name: "bash"},
                Run: func(context.Context, json.RawMessage) (tools.Result, error) {
                        return tools.Result{Content: evidenceGoTestPass}, nil
                },
        }
        ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)
        ledger := evidence.NewLedger("")
        ex.SetEvidenceLedger(ledger)

        _, ok := ex.DispatchScript(t.Context(), "bash", json.RawMessage(`{"command":"go test ./internal/auth/"}`))
        require.True(t, ok)
        assert.False(t, ledger.LastPassAt().IsZero(),
                "an in-script passing test run is exactly the evidence the verify gate prices")
}

// TestExecutorRunAttachesScriptDispatcher pins the composition point: a
// script tool call executed through Executor.Run carries the dispatcher on
// its context, so the python child's RPC calls land back in this
// executor's pipeline. Requires python3 (skips otherwise).
func TestExecutorRunAttachesScriptDispatcher(t *testing.T) {
        if _, err := exec.LookPath("python3"); err != nil {
                if _, err2 := exec.LookPath("python"); err2 != nil {
                        t.Skip("no python3 available")
                }
        }
        var ran strings.Builder
        reg := dispatchRegistry(&ran)
        reg["script"] = scripttool.ScriptTool()
        ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)

        msgs, _, _ := ex.Run(t.Context(), []llm.ToolCall{{
                ID:   "s1",
                Type: "function",
                Function: llm.Function{
                        Name: "script",
                        Arguments: mustJSON(t, map[string]any{
                                "code":    "from a1tools import read\nprint(read(\"notes.txt\").splitlines()[0])",
                                "timeout": 30,
                        }),
                },
        }}, func(session.ToolData) bool { return true })
        require.Len(t, msgs, 1)
        // The script printed the first line of the dispatched read's content.
        assert.Contains(t, msgs[0].Content, "1|alpha")
        assert.Contains(t, ran.String(), "read", "the in-script call must dispatch to the registry tool")
}

func mustJSON(t *testing.T, v any) string {
        t.Helper()
        data, err := json.Marshal(v)
        require.NoError(t, err)
        return string(data)
}
