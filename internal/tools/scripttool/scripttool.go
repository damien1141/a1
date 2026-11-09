// Package scripttool implements the script tool — a1's port of hermes's
// programmatic tool calling ("execute_code"), their flagship context
// economizer: the model writes one Python script that calls real a1 tools
// as functions over a loopback Unix socket, and only the script's final
// stdout returns to the LLM. Ten tool steps become one context touch
// instead of ten turns of tool-call round-trips.
//
// The security envelope is hermes's, adapted to a1's shapes: a
// per-execution token (constant-time compare, fail closed), a fixed tool
// allow-list intersected with the session registry at dispatch, a call
// budget (only dispatched calls consume it), and — the load-bearing part —
// every RPC call traverses the normal permission pipeline via the
// executor-installed dispatcher (extension pre/post, gate check with ask,
// evidence and mutation recording). The child runs under the same OS
// containment as bash: arbitrary python is arbitrary code execution, and
// the walls exist for exactly that.
package scripttool

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/sandbox"
	"github.com/damien1141/a1/internal/tools/bashtool"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

const (
	// scriptDefaultTimeout bounds one script execution (the child process
	// and, transitively, every in-flight RPC call). Matches bash's default.
	scriptDefaultTimeout = 300
	// scriptMaxTimeout caps the caller-requested timeout, as bash does.
	scriptMaxTimeout = 3600
	// maxToolCalls is the per-execution RPC budget. Only dispatched calls
	// consume it; refusals (bad token, off-list, over budget) are free.
	maxToolCalls = 50
	// maxStdoutBytes is the inline stdout budget (40% head / 60% tail,
	// hermes's split) — the omitted middle spills to disk, so the model
	// can page it back with read() instead of re-running the script.
	maxStdoutBytes = 50 * 1024
	// maxSpillBytes caps the spilled file AND in-memory collection: a
	// runaway print loop must not fill the disk or the RAM.
	maxSpillBytes = 5 * 1024 * 1024
	// maxStderrCollect caps collected stderr (keep-tail: tracebacks live
	// at the end, and they are what the failure hints match against).
	maxStderrCollect = 64 * 1024
)

const scriptDescription = `Run a Python script that calls a1's tools as functions, collapsing multi-step
tool chains into one call: only the script's final stdout returns, so 10 tool
steps cost one context touch instead of 10 rounds. Use for 3+ tool calls with
logic between them — filtering/reducing large outputs before they enter
context, branching, loops over N files/pages, retry-on-failure. Use normal
tool calls for single steps, results you must reason over in full, or anything
needing user interaction.

Import tools: from a1tools import read, bash

` + "\nAVAILABLE FUNCTIONS\n\n" + `
%s

Every function returns a ToolResult — usable as a plain string, with .ok
recording success (permission denials and tool errors return their message
text; check .ok before trusting content).

Helpers (import from a1tools): json_parse(text) — tolerant json.loads for
JSON *inside* bash()/fetch() output; shell_quote(s) — shlex.quote for dynamic
bash() args; retry(fn, max_attempts=3, delay=2) — exponential backoff.

Print your final result to stdout — that is the only thing returned to you.
Runs in the session working directory with the project's python when a
virtualenv is active, else python3 (stdlib only — no third-party imports).
Limits: timeout (default 300s, max 3600s), max %d tool calls, stdout over
%d bytes shows head/tail inline with the full text spilled to a file whose
path rides in the result — read it back instead of re-running.`

// ScriptTool returns the script tool definition + handler.
func ScriptTool() tooldef.Tool {
	desc := fmt.Sprintf(scriptDescription, stubToolDocLines(), maxToolCalls, maxStdoutBytes)
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "script",
			Description: desc,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"code": llm.Object{
						"type":        "string",
						"description": "Python source. Import tools with `from a1tools import read, bash` and print the final result to stdout.",
					},
					"timeout": llm.Object{
						"type":        "integer",
						"description": "Execution timeout in seconds (default 300, max 3600).",
					},
				},
				Required: []string{"code"},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in scriptInput
			_ = json.Unmarshal(input, &in)
			return codeIdentity(in.Code)
		},
		Run: runScript,
	}
}

type scriptInput struct {
	Code    string `json:"code"`
	Timeout int    `json:"timeout"`
}

// codeIdentity is the tool-row identity: the first non-blank line of the
// script, de-indented and clipped — enough to recognize "which script was
// this" in a transcript without scrolling the whole body.
func codeIdentity(code string) string {
	for _, line := range strings.Split(code, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return truncateRunes(t, 80)
		}
	}
	return "(empty script)"
}

func runScript(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in scriptInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse script arguments: %w", err)
	}
	if strings.TrimSpace(in.Code) == "" {
		return tooldef.Result{}, errors.New(
			"no code provided: script requires Python source in 'code'. To run a shell command, use bash(command=...) instead")
	}
	dispatch := tooldef.ScriptDispatchFrom(ctx)
	if dispatch == nil {
		// A bare executor (tests, embedding) never attached the dispatcher;
		// executing the script anyway would strand every tool call.
		return tooldef.Result{}, errors.New(
			"script tool is not wired to a tool dispatcher in this session; use normal tool calls instead")
	}
	python, err := resolvePython()
	if err != nil {
		return tooldef.Result{}, err
	}

	timeout := in.Timeout
	if timeout <= 0 {
		timeout = scriptDefaultTimeout
	}
	if timeout > scriptMaxTimeout {
		timeout = scriptMaxTimeout
	}

	// Per-execution workspace: 0700 temp dir holding the generated stub
	// module, the model's script, and the RPC socket. Owner-only directory
	// perms are the socket's real ACL (hermes's private-dirs discipline).
	tmpdir, err := os.MkdirTemp("", "a1-script-")
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("script workspace: %w", err)
	}
	defer os.RemoveAll(tmpdir)

	token, err := newToken()
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("script token: %w", err)
	}

	endpoint, ln, err := listenRPC(tmpdir)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("script RPC listener: %w", err)
	}
	defer ln.Close()

	if err := os.WriteFile(filepath.Join(tmpdir, "a1tools.py"), []byte(stubModule()), 0o600); err != nil {
		return tooldef.Result{}, fmt.Errorf("write stub module: %w", err)
	}
	scriptPath := filepath.Join(tmpdir, "script.py")
	if err := os.WriteFile(scriptPath, []byte(in.Code), 0o600); err != nil {
		return tooldef.Result{}, fmt.Errorf("write script: %w", err)
	}

	srv := &rpcServer{token: token, allowed: allowedToolSet(), dispatch: dispatch}
	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	go srv.serve(serveCtx, ln)

	execCtx, cancelExec := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancelExec()

	stdout := newCappedWriter(maxSpillBytes, keepHead)
	stderr := newCappedWriter(maxStderrCollect, keepTail)

	cmd := exec.CommandContext(execCtx, python, "-B", scriptPath) //nolint:gosec // G204: running model-written scripts is this tool's purpose
	cmd.Dir = workDir(ctx)
	cmd.Env = append(bashtool.ShellEnv(),
		"PYTHONDONTWRITEBYTECODE=1",
		"PYTHONPATH="+tmpdir,
		"A1_TOOLS_SOCKET="+endpoint,
		"A1_TOOLS_TOKEN="+token,
	)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = bashtool.ProcessGroupAttr()
	cmd.WaitDelay = bashtool.ShellWaitDelay()
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return bashtool.KillProcessTree(cmd.Process.Pid)
	}

	start := time.Now()
	// Same containment as a bash child: the profile compiles from the live
	// permission policy (workspace + caches RW, network toggle). The RPC
	// socket lives in the OS temp dir, which the manifest already allows.
	prof := bashtool.SandboxProfile(ctx)
	var waitErr error
	if prof != nil {
		if serr := sandbox.Start(cmd, prof); serr != nil {
			return tooldef.Result{}, serr
		}
		waitErr = cmd.Wait()
	} else {
		waitErr = cmd.Run()
	}
	duration := time.Since(start)

	res := assembleResult(execCtx, waitErr, stdout, stderr, srv, duration, timeout)
	content, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("encode script result: %w", err)
	}
	body := string(content)
	return tooldef.Result{
		Content: body,
		Detail:  codeIdentity(in.Code),
		Summary: res.summaryLine(),
		Output:  body,
	}, nil
}

// scriptResult is the model-facing JSON contract. Key order is deliberate
// (hermes treats it as part of the contract): status first, the payload,
// then the failure diagnostics.
type scriptResult struct {
	Status          string           `json:"status"`
	Output          string           `json:"output"`
	ExitCode        int              `json:"exit_code"`
	ToolCallsMade   int              `json:"tool_calls_made"`
	DurationSeconds float64          `json:"duration_seconds"`
	Error           string           `json:"error,omitempty"`
	Hint            string           `json:"hint,omitempty"`
	ToolErrors      []toolErrorEntry `json:"tool_errors,omitempty"`
	// Stdout truncation metadata: byte counts ride alongside the textual
	// marker because a client layer can miss or re-truncate the marker.
	StdoutTruncated    bool   `json:"stdout_truncated,omitempty"`
	StdoutBytesTotal   int    `json:"stdout_bytes_total,omitempty"`
	StdoutBytesKept    int    `json:"stdout_bytes_kept,omitempty"`
	StdoutBytesDropped int    `json:"stdout_bytes_dropped,omitempty"`
	StdoutSpillPath    string `json:"stdout_spill_path,omitempty"`
}

type toolErrorEntry struct {
	Tool  string `json:"tool"`
	Error string `json:"error"`
}

func (r scriptResult) summaryLine() string {
	sep := " · "
	var b strings.Builder
	switch r.Status {
	case "success":
		b.WriteString("exit 0")
	case "timeout":
		b.WriteString("timeout")
	default:
		b.WriteString(fmt.Sprintf("exit %d", r.ExitCode))
	}
	fmt.Fprintf(&b, "%s%d tool calls%s%.1fs", sep, r.ToolCallsMade, sep, r.DurationSeconds)
	return b.String()
}

func assembleResult(
	execCtx context.Context,
	waitErr error,
	stdout, stderr *cappedWriter,
	srv *rpcServer,
	duration time.Duration,
	timeout int,
) scriptResult {
	res := scriptResult{
		Status:          "success",
		DurationSeconds: float64(duration.Milliseconds()) / 1000.0,
		ToolCallsMade:   srv.callsMade(),
	}

	// Stdout pipeline: decode, cap inline (head/tail), spill the full text.
	// totalBytes is the true count (the collector counts everything), so
	// the metadata stays honest even when collection itself hit the spill
	// ceiling.
	stdoutText := stdout.String()
	totalBytes := stdout.totalBytes()
	output, spillPath := capStdout(stdoutText)
	res.Output = output
	if len(stdoutText) > maxStdoutBytes {
		res.StdoutTruncated = true
		res.StdoutBytesTotal = totalBytes
		res.StdoutBytesKept = maxStdoutBytes
		res.StdoutBytesDropped = totalBytes - maxStdoutBytes
		res.StdoutSpillPath = spillPath
	}
	if stdout.truncated() {
		// Collection itself hit the 5MB ceiling: the spill is partial too,
		// and the model must not believe "full output" means all of it.
		res.Hint = combineHints(res.Hint, fmt.Sprintf(
			"output exceeded the %d-byte collection cap; the spill file holds only the first %d bytes — re-run with narrower output if more is required.",
			maxSpillBytes, maxSpillBytes))
	}

	// stderr rides under one marker so the model sees the failure inline;
	// collection is keep-tail so tracebacks (at the end) survive noisy
	// warnings.
	if stderrText := stderr.String(); stderrText != "" {
		res.Output = joinMarker(res.Output, "--- stderr ---", stderrText)
		if hint := failureHint(stderrText); hint != "" {
			res.Hint = combineHints(res.Hint, hint)
		}
	}

	var exitErr *exec.ExitError
	switch {
	case execCtx.Err() != nil && errors.Is(execCtx.Err(), context.DeadlineExceeded):
		res.Status = "timeout"
		res.Error = fmt.Sprintf(
			"script timed out after %ds and was killed; partial output above. A long-running step belongs in bash(), or raise timeout (max %ds).",
			timeout, scriptMaxTimeout)
		// The timeout message goes in the output too — an empty result
		// makes models answer as if nothing happened.
		res.Output = joinMarker(res.Output, "⏰ "+res.Error, "")
	case execCtx.Err() != nil:
		res.Status = "error"
		res.Error = "script was canceled"
		res.Output = joinMarker(res.Output, "(script canceled)", "")
	case waitErr != nil && errors.As(waitErr, &exitErr):
		res.Status = "error"
		res.ExitCode = exitErr.ExitCode()
		res.Error = fmt.Sprintf("script exited with code %d", exitErr.ExitCode())
	case waitErr != nil:
		res.Status = "error"
		res.Error = waitErr.Error()
	}

	if errs := srv.toolErrors(); len(errs) > 0 {
		res.ToolErrors = make([]toolErrorEntry, 0, len(errs))
		for _, e := range errs {
			msg := e.Error
			if msg == "" {
				msg = "(no error text)"
			}
			res.ToolErrors = append(res.ToolErrors, toolErrorEntry{Tool: e.Tool, Error: msg})
		}
	}
	return res
}

// combineHints joins two hint strings, tolerating either being empty.
func combineHints(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
}

// joinMarker appends marker (and text, when non-empty) under a blank line,
// keeping empty payloads from stacking up bare markers.
func joinMarker(body, marker, text string) string {
	joined := body
	if joined != "" {
		joined += "\n\n"
	}
	joined += marker
	if text != "" {
		joined += "\n" + text
	}
	return joined
}

// ---- stdout cap + spill ----------------------------------------------------

// capStdout caps inline stdout at maxStdoutBytes with a 40% head / 60% tail
// split (hermes's ratio: the head says what the script started producing,
// the tail says where it ended). The omitted middle spills to a
// content-digest-keyed file — identical reruns coalesce, and the result
// carries the path so the model pages it back with read() instead of
// re-running (recover-don't-rerun).
func capStdout(text string) (inline, spillPath string) {
	total := len(text)
	if total <= maxStdoutBytes {
		return text, ""
	}
	head := maxStdoutBytes * 2 / 5
	tail := maxStdoutBytes - head
	spillPath, spillErr := spillStdout(text)
	marker := fmt.Sprintf(
		"[script stdout truncated: showing %d of %d bytes — full output: %s — read it back with read(path=%q, offset=...) instead of re-running]",
		maxStdoutBytes, total, spillPath, spillPath)
	if spillErr != nil {
		marker = fmt.Sprintf(
			"[script stdout truncated: showing %d of %d bytes — spill failed (%v); re-run with narrower output if the omitted data is required]",
			maxStdoutBytes, total, spillErr)
		spillPath = ""
	}
	return text[:head] + marker + text[total-tail:], spillPath
}

// spillStdout writes the full stdout to the spill dir, keyed by content
// digest so identical reruns coalesce. Best-effort: a failed spill still
// returns the truncated inline output.
func spillStdout(text string) (string, error) {
	if len(text) > maxSpillBytes {
		text = text[:maxSpillBytes] + fmt.Sprintf("\n\n[... spill capped at %d bytes ...]", maxSpillBytes)
	}
	dir := filepath.Join(os.TempDir(), "a1-script-spill")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	digest := sha256Prefix12(text)
	path := filepath.Join(dir, "stdout-"+digest+".txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func sha256Prefix12(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:6])
}

// ---- failure hints -----------------------------------------------------------

// failureHintRules maps well-known script failures to one actionable
// recovery hint (bounded scan, first match wins). Hermes mined these from
// production state.db as the top execute_code failure classes; the a1
// variants adjust for text tool results instead of hermes's JSON dicts.
var failureHintRules = []struct {
	pattern *regexp.Regexp
	format  func(m []string) string
}{
	{
		regexp.MustCompile(`cannot import name '(\w+)' from 'a1tools'`),
		func(m []string) string {
			return fmt.Sprintf(
				"'%s' is not an a1tools function. Importable here: %s. For anything else, use the normal tool call instead of script.",
				m[1], allowedToolNames())
		},
	},
	{
		regexp.MustCompile(`No module named 'a1tools'`),
		func([]string) string {
			return "the a1tools stub module was not importable; the script ran outside its workspace. Retry the script call unchanged."
		},
	},
	{
		regexp.MustCompile(`No module named '([\w.]+)'`),
		func(m []string) string {
			return fmt.Sprintf(
				"'%s' is not installed in the script interpreter (stdlib only). Run the code via bash() with the project's python instead, or use stdlib.",
				m[1])
		},
	},
	{
		regexp.MustCompile(`NameError: name '(json_parse|shell_quote|retry)' is not defined`),
		func(m []string) string {
			return fmt.Sprintf("Import helpers before calling them: from a1tools import %s", m[1])
		},
	},
	{
		regexp.MustCompile(`string indices must be integers|'str' object has no attribute`),
		func([]string) string {
			return "tool functions return ToolResult (a str subclass), not dicts — do not json.loads() them or index them. Check result.ok, and use json_parse() only for JSON inside bash()/fetch() output."
		},
	},
}

// failureHint scans the first 4KB of stderr and returns the first matching
// hint, or "".
func failureHint(stderrText string) string {
	window := stderrText
	if len(window) > 4096 {
		window = window[:4096]
	}
	for _, rule := range failureHintRules {
		if m := rule.pattern.FindStringSubmatch(window); m != nil {
			return rule.format(m)
		}
	}
	return ""
}

// ---- interpreter + plumbing --------------------------------------------------

// resolvePython picks the script interpreter, hermes project-mode parity:
// the activated venv/conda python when one is active in a1's environment
// (matching what bash children see), else python3/python from PATH.
func resolvePython() (string, error) {
	for _, envName := range []string{"VIRTUAL_ENV", "CONDA_PREFIX"} {
		prefix := strings.TrimSpace(os.Getenv(envName))
		if prefix == "" {
			continue
		}
		for _, cand := range []string{
			filepath.Join(prefix, "bin", "python3"),
			filepath.Join(prefix, "bin", "python"),
			filepath.Join(prefix, "python.exe"),
			filepath.Join(prefix, "Scripts", "python.exe"),
		} {
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				return cand, nil
			}
		}
	}
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New(
		"script requires python3 and none was found on PATH; install python3 or use bash(command=...) for shell work instead")
}

// workDir resolves the session cwd for the child (project-mode: scripts
// run where the session runs, so relative paths in read()/write() behave
// exactly as they do in normal tool calls).
func workDir(ctx context.Context) string {
	if dir, err := tooldef.Cwd(ctx); err == nil && dir != "" {
		return dir
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// listenRPC binds the loopback RPC endpoint: a Unix socket inside the
// per-execution workspace, falling back to loopback TCP where AF_UNIX is
// unavailable (Windows). The endpoint string is what the stub dials, with
// a tcp:// prefix on the fallback so the stub picks the right family.
func listenRPC(tmpdir string) (string, netListener, error) {
	path := filepath.Join(tmpdir, "rpc.sock")
	ln, err := listenUnix(path)
	if err == nil {
		return path, ln, nil
	}
	tcp, terr := listenLoopbackTCP()
	if terr != nil {
		return "", nil, err // the unix error is the informative one
	}
	return "tcp://" + tcp.Addr().String(), tcp, nil
}

// newToken mints the per-execution RPC token: 32 random bytes, hex.
func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
