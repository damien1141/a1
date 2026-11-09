package scripttool

import (
        "bufio"
        "context"
        "encoding/json"
        "fmt"
        "net"
        "os"
        "os/exec"
        "path/filepath"
        "strings"
        "sync"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/tools/tooldef"
)

// ---- pure units -------------------------------------------------------------

func TestStubModuleContainsAllToolsAndHelpers(t *testing.T) {
        src := stubModule()
        for _, name := range []string{"read", "write", "edit", "search", "bash", "fetch"} {
                assert.Contains(t, src, "def "+name+"(", "stub for "+name)
        }
        for _, helper := range []string{"def json_parse(", "def shell_quote(", "def retry(", "class ToolResult("} {
                assert.Contains(t, src, helper)
        }
        // The stub must speak the env contract the runner sets.
        assert.Contains(t, src, `os.environ["A1_TOOLS_SOCKET"]`)
        assert.Contains(t, src, `os.environ.get("A1_TOOLS_TOKEN", "")`)
}

func TestStubModuleCompilesUnderPython(t *testing.T) {
        python, err := resolvePython()
        if err != nil {
                t.Skip("no python3 available")
        }
        dir := t.TempDir()
        path := filepath.Join(dir, "a1tools.py")
        require.NoError(t, os.WriteFile(path, []byte(stubModule()), 0o600))
        code := fmt.Sprintf("import sys; sys.path.insert(0, %q); import a1tools as m; assert callable(m.read) and callable(m.bash) and callable(m.retry)", dir)
        out, err := exec.Command(python, "-B", "-c", code).CombinedOutput()
        require.NoError(t, err, "stub module must be valid python: %s", string(out))
}

func TestCodeIdentity(t *testing.T) {
        assert.Equal(t, "import os", codeIdentity("\n\n  import os\nprint(1)"))
        assert.Equal(t, "(empty script)", codeIdentity("  \n\t\n"))
        long := strings.Repeat("x", 200)
        assert.Len(t, []rune(codeIdentity(long)), 81, "identity clips to 80 runes + ellipsis")
}

func TestFailureHintRules(t *testing.T) {
        cases := []struct {
                stderr string
                want   string
        }{
                {
                        "Traceback (most recent call last):\n  File \"script.py\", line 2, in <module>\nNameError: name 'json_parse' is not defined",
                        "from a1tools import json_parse",
                },
                {
                        "Traceback (most recent call last):\n  File \"script.py\", line 2, in <module>\n    import numpy\nModuleNotFoundError: No module named 'numpy'",
                        "not installed in the script interpreter",
                },
                {
                        "Traceback (most recent call last):\n  File \"script.py\", line 3, in <module>\n    r[\"content\"]\nTypeError: string indices must be integers",
                        "ToolResult (a str subclass)",
                },
                {
                        "Traceback (most recent call last):\n  File \"script.py\", line 1, in <module>\n    from a1tools import web_search\nImportError: cannot import name 'web_search' from 'a1tools'",
                        "is not an a1tools function",
                },
                {
                        "some unrelated failure\nKeyError: 'zzz'",
                        "",
                },
        }
        for _, tc := range cases {
                hint := failureHint(tc.stderr)
                if tc.want == "" {
                        assert.Empty(t, hint, "unexpected hint for %q", tc.stderr)
                        continue
                }
                assert.Contains(t, hint, tc.want, "hint for %q", tc.stderr)
        }
}

func TestCapStdoutHeadTailAndSpill(t *testing.T) {
        var b strings.Builder
        for i := 0; i < 20_000; i++ {
                fmt.Fprintf(&b, "line-%06d\n", i)
        }
        text := b.String()
        require.Greater(t, len(text), maxStdoutBytes)

        inline, spill := capStdout(text)
        assert.Contains(t, inline, "line-000000", "head survives")
        assert.Contains(t, inline, "line-019999", "tail survives")
        assert.NotContains(t, inline, "line-010000", "middle omitted")
        assert.Contains(t, inline, "script stdout truncated")
        require.NotEmpty(t, spill, "spill path expected")

        spilled, err := os.ReadFile(spill)
        require.NoError(t, err)
        assert.Equal(t, text, string(spilled), "spill holds the full output")

        // Identical reruns coalesce on the content digest.
        _, spill2 := capStdout(text)
        assert.Equal(t, spill, spill2)
}

func TestCapStdoutSmallPassesThrough(t *testing.T) {
        inline, spill := capStdout("short")
        assert.Equal(t, "short", inline)
        assert.Empty(t, spill)
}

func TestCappedWriterModes(t *testing.T) {
        head := newCappedWriter(10, keepHead)
        _, _ = head.Write([]byte("abcdefghijklmnop"))
        assert.Equal(t, "abcdefghij", head.String())
        assert.True(t, head.truncated())
        assert.Equal(t, 16, head.totalBytes())

        tail := newCappedWriter(10, keepTail)
        _, _ = tail.Write([]byte("abcdefghijklmnop"))
        assert.Equal(t, "ghijklmnop", tail.String())
        assert.Equal(t, 16, tail.totalBytes())
}

func TestStripBashBackgroundMode(t *testing.T) {
        kept := stripBashBackgroundMode(json.RawMessage(`{"command":"ls","mode":"run"}`))
        assert.JSONEq(t, `{"command":"ls","mode":"run"}`, string(kept))

        stripped := stripBashBackgroundMode(json.RawMessage(`{"command":"npm run dev","mode":"start"}`))
        assert.JSONEq(t, `{"command":"npm run dev"}`, string(stripped))

        untouched := stripBashBackgroundMode(json.RawMessage(`{"command":"ls"}`))
        assert.JSONEq(t, `{"command":"ls"}`, string(untouched))
}

// ---- RPC server over a real socket -------------------------------------------

type rpcClient struct {
        conn net.Conn
        r    *bufio.Reader
}

func dialRPC(t *testing.T, endpoint string) *rpcClient {
        t.Helper()
        var conn net.Conn
        var err error
        if strings.HasPrefix(endpoint, "tcp://") {
                conn, err = net.Dial("tcp", strings.TrimPrefix(endpoint, "tcp://"))
        } else {
                conn, err = net.Dial("unix", endpoint)
        }
        require.NoError(t, err)
        t.Cleanup(func() { conn.Close() })
        return &rpcClient{conn: conn, r: bufio.NewReader(conn)}
}

func (c *rpcClient) call(t *testing.T, tool, token string, args map[string]any) rpcReply {
        t.Helper()
        payload := map[string]any{"tool": tool, "args": args, "token": token}
        data, err := json.Marshal(payload)
        require.NoError(t, err)
        _, err = c.conn.Write(append(data, '\n'))
        require.NoError(t, err)
        _ = c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
        line, err := c.r.ReadString('\n')
        require.NoError(t, err)
        var reply rpcReply
        require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(line)), &reply))
        return reply
}

func startTestServer(t *testing.T, dispatch tooldef.ScriptDispatchFunc) (*rpcServer, string) {
        t.Helper()
        if dispatch == nil {
                dispatch = func(context.Context, string, json.RawMessage) (string, bool) { return "ok", true }
        }
        dir := t.TempDir()
        endpoint, ln, err := listenRPC(dir)
        require.NoError(t, err)
        t.Cleanup(func() { ln.Close() })
        srv := &rpcServer{token: "test-token", allowed: allowedToolSet(), dispatch: dispatch}
        go srv.serve(context.Background(), ln)
        return srv, endpoint
}

func TestRPCServerTokenGate(t *testing.T) {
        srv, endpoint := startTestServer(t, nil)
        c := dialRPC(t, endpoint)

        reply := c.call(t, "read", "wrong-token", map[string]any{"path": "x"})
        assert.False(t, reply.OK)
        assert.Contains(t, reply.Result, "unauthorized")
        assert.Equal(t, 0, srv.callsMade(), "refusals are free")

        reply = c.call(t, "read", "", map[string]any{"path": "x"})
        assert.False(t, reply.OK)
        assert.Contains(t, reply.Result, "unauthorized")

        reply = c.call(t, "read", "test-token", map[string]any{"path": "x"})
        assert.True(t, reply.OK)
        assert.Equal(t, "ok", reply.Result)
        assert.Equal(t, 1, srv.callsMade(), "only dispatched calls consume budget")
}

func TestRPCServerAllowList(t *testing.T) {
        srv, endpoint := startTestServer(t, nil)
        c := dialRPC(t, endpoint)

        reply := c.call(t, "question", "test-token", map[string]any{"q": "hi"})
        assert.False(t, reply.OK)
        assert.Contains(t, reply.Result, "not available inside scripts")
        assert.Contains(t, reply.Result, "read")
        assert.Equal(t, 0, srv.callsMade(), "off-list refusals are free")
}

func TestRPCServerBudget(t *testing.T) {
        // Drive maxToolCalls dispatched calls, then assert the next is refused.
        _, endpoint := startTestServer(t, nil)
        c := dialRPC(t, endpoint)
        for i := 0; i < maxToolCalls; i++ {
                reply := c.call(t, "read", "test-token", map[string]any{"path": "x", "n": i})
                require.True(t, reply.OK, "call %d should pass", i)
        }
        reply := c.call(t, "read", "test-token", map[string]any{"path": "x"})
        assert.False(t, reply.OK)
        assert.Contains(t, reply.Result, "tool call limit reached")
}

func TestRPCServerToolErrorsLog(t *testing.T) {
        dispatch := func(context.Context, string, json.RawMessage) (string, bool) {
                return "permission denied: workspace-only", false
        }
        srv, endpoint := startTestServer(t, dispatch)
        c := dialRPC(t, endpoint)

        reply := c.call(t, "write", "test-token", map[string]any{"path": "/etc/x", "content": "y"})
        assert.False(t, reply.OK)
        assert.Equal(t, "permission denied: workspace-only", reply.Result)

        errs := srv.toolErrors()
        require.Len(t, errs, 1)
        assert.Equal(t, "write", errs[0].Tool)
        assert.Equal(t, "permission denied: workspace-only", errs[0].Error)
}

func TestRPCServerStripsBashModeOverWire(t *testing.T) {
        seen := make(map[string]json.RawMessage)
        var mu sync.Mutex
        dispatch := func(_ context.Context, tool string, args json.RawMessage) (string, bool) {
                mu.Lock()
                defer mu.Unlock()
                seen[tool] = args
                return "done", true
        }
        _, endpoint := startTestServer(t, dispatch)
        c := dialRPC(t, endpoint)

        reply := c.call(t, "bash", "test-token", map[string]any{"command": "npm run dev", "mode": "start"})
        assert.True(t, reply.OK)
        mu.Lock()
        bashArgs := seen["bash"]
        mu.Unlock()
        require.NotNil(t, bashArgs)
        assert.NotContains(t, string(bashArgs), "start", "background mode must be stripped before dispatch")
        assert.Contains(t, string(bashArgs), "npm run dev")

        // Mode stripping is bash-only: search's legitimate mode field must
        // arrive at dispatch untouched.
        reply = c.call(t, "search", "test-token", map[string]any{"query": "x", "mode": "auto"})
        assert.True(t, reply.OK)
        mu.Lock()
        searchArgs := seen["search"]
        mu.Unlock()
        require.NotNil(t, searchArgs)
        assert.Contains(t, string(searchArgs), "auto", "search mode must survive dispatch")
}

// ---- end-to-end through python ----------------------------------------------

func pythonAvailable(t *testing.T) string {
        t.Helper()
        python, err := resolvePython()
        if err != nil {
                t.Skip("no python3 available")
        }
        return python
}

func runScriptTool(t *testing.T, ctx context.Context, code string) (scriptResult, error) {
        t.Helper()
        input, err := json.Marshal(scriptInput{Code: code, Timeout: 30})
        require.NoError(t, err)
        res, err := runScript(ctx, input)
        if err != nil {
                return scriptResult{}, err
        }
        var out scriptResult
        require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
        return out, nil
}

func TestScriptEndToEndCallsToolsAndPrints(t *testing.T) {
        pythonAvailable(t)
        var mu sync.Mutex
        var calls []string
        dispatch := func(_ context.Context, tool string, _ json.RawMessage) (string, bool) {
                mu.Lock()
                defer mu.Unlock()
                calls = append(calls, tool)
                switch tool {
                case "read":
                        return "1|hello\n2|world", true
                case "bash":
                        return "build ok", true
                default:
                        return "unexpected", false
                }
        }
        ctx := tooldef.WithScriptDispatch(tooldef.WithCwd(t.Context(), t.TempDir()), dispatch)

        out, err := runScriptTool(t, ctx, `
from a1tools import read, bash
text = read("notes.txt")
status = bash("make build")
print(text.strip().splitlines()[-1], "|", status, "| ok:", status.ok)
`)
        require.NoError(t, err)
        assert.Equal(t, "success", out.Status)
        assert.Equal(t, 2, out.ToolCallsMade)
        assert.Contains(t, out.Output, "world | build ok | ok: True")
        assert.Empty(t, out.ToolErrors)

        mu.Lock()
        defer mu.Unlock()
        assert.Equal(t, []string{"read", "bash"}, calls)
}

func TestScriptEndToEndFailedCallRidesToolErrors(t *testing.T) {
        pythonAvailable(t)
        dispatch := func(context.Context, string, json.RawMessage) (string, bool) {
                return "write refused: /etc is outside the workspace", false
        }
        ctx := tooldef.WithScriptDispatch(tooldef.WithCwd(t.Context(), t.TempDir()), dispatch)

        out, err := runScriptTool(t, ctx, `
from a1tools import write
r = write("/etc/passwd", "x")
print("result was:", r)
`)
        require.NoError(t, err)
        // The script ignored the failure — the tool_errors summary is the net.
        assert.Equal(t, "success", out.Status)
        require.Len(t, out.ToolErrors, 1)
        assert.Equal(t, "write", out.ToolErrors[0].Tool)
        assert.Contains(t, out.ToolErrors[0].Error, "outside the workspace")
        assert.Contains(t, out.Output, "result was: write refused")
}

func TestScriptEndToEndTimeout(t *testing.T) {
        pythonAvailable(t)
        dispatch := func(context.Context, string, json.RawMessage) (string, bool) { return "", true }
        ctx := tooldef.WithScriptDispatch(tooldef.WithCwd(t.Context(), t.TempDir()), dispatch)

        input, err := json.Marshal(scriptInput{Code: "import time\ntime.sleep(30)\nprint('done')", Timeout: 1})
        require.NoError(t, err)
        start := time.Now()
        res, err := runScript(ctx, input)
        require.NoError(t, err)
        require.Less(t, time.Since(start), 15*time.Second, "timeout must actually kill the child")

        var out scriptResult
        require.NoError(t, json.Unmarshal([]byte(res.Content), &out))
        assert.Equal(t, "timeout", out.Status)
        assert.Contains(t, out.Error, "timed out after 1s")
        assert.Contains(t, out.Output, "⏰")
}

func TestScriptEndToEndStderrAndHint(t *testing.T) {
        pythonAvailable(t)
        dispatch := func(context.Context, string, json.RawMessage) (string, bool) { return "", true }
        ctx := tooldef.WithScriptDispatch(tooldef.WithCwd(t.Context(), t.TempDir()), dispatch)

        out, err := runScriptTool(t, ctx, `
from a1tools import read
text = read("x")
parsed = json_parse(text)
print(parsed)
`)
        require.NoError(t, err)
        assert.Equal(t, "error", out.Status)
        assert.NotZero(t, out.ExitCode)
        assert.Contains(t, out.Output, "--- stderr ---")
        assert.Contains(t, out.Output, "NameError")
        assert.Contains(t, out.Hint, "from a1tools import json_parse")
}

func TestScriptEmptyCodeRefused(t *testing.T) {
        _, err := runScript(t.Context(), json.RawMessage(`{"code":"   "}`))
        require.Error(t, err)
        assert.Contains(t, err.Error(), "no code provided")
}

func TestScriptWithoutDispatcherFailsClosed(t *testing.T) {
        pythonAvailable(t)
        _, err := runScript(t.Context(), json.RawMessage(`{"code":"print(1)"}`))
        require.Error(t, err)
        assert.Contains(t, err.Error(), "not wired to a tool dispatcher")
}

func TestScriptResultSummaryLine(t *testing.T) {
        r := scriptResult{Status: "success", ToolCallsMade: 3, DurationSeconds: 1.42}
        assert.Equal(t, "exit 0 · 3 tool calls · 1.4s", r.summaryLine())
        r = scriptResult{Status: "timeout", ToolCallsMade: 0, DurationSeconds: 2}
        assert.Equal(t, "timeout · 0 tool calls · 2.0s", r.summaryLine())
}
