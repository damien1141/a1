package scripttool

import (
	"fmt"
	"sort"
	"strings"
)

// sandboxTool describes one tool exposed to scripts. The stub signature and
// docstring are the model's only view of the callable surface inside a
// script, so they mirror the real tool schemas faithfully — a stub that
// lies about `limit` semantics teaches the model to write broken loops.
type sandboxTool struct {
	name string
	// stub is the Python def line (without the leading "def " / trailing ":").
	stub string
	// doc is the single-line docstring for the stub.
	doc string
	// args is the Python dict-literal expression building the JSON payload.
	args string
}

// sandboxTools is the fixed allow-list hermes calls SANDBOX_ALLOWED_TOOLS:
// the curated set a script may call, intersected at dispatch time with the
// registry (a tool not registered for this session is refused with the
// available list, not a 404-shaped mystery). Deliberately excludes the
// interactive surfaces — question, permission, browser profile — an
// unattended script blocking on a human dialog is a stuck budget, and the
// agent-spawn tools, whose sub-agents would escape the call budget's
// accounting entirely.
var sandboxTools = []sandboxTool{
	{
		name: "read",
		stub: `path, mode="file", offset=None, limit=None, refresh=None`,
		doc:  `Read a file ("path" required; 1-based offset/limit paginate) or a directory (mode="ls"). Returns the file text (lines are "n|content") or the listing.`,
		args: `{"path": path, "mode": mode, "offset": offset, "limit": limit, "refresh": refresh}`,
	},
	{
		name: "write",
		stub: `path, content`,
		doc:  `Write a whole file (overwrites). Returns a status line. An existing file must have been read first or the write is refused.`,
		args: `{"path": path, "content": content, "mode": "write"}`,
	},
	{
		name: "edit",
		stub: `path, old_str, new_str`,
		doc:  `Replace old_str with new_str in path (must be unique in the file; empty new_str deletes). Same tool as write(mode="edit").`,
		args: `{"path": path, "old_str": old_str, "new_str": new_str, "mode": "edit"}`,
	},
	{
		name: "search",
		stub: `query=None, mode="auto", path=".", limit=None, glob=None, literal=None`,
		doc:  `Search code: mode auto|text|symbol|file|grep|find|graph|rank. Omit query (with path set) to list a directory. Returns matches as text.`,
		args: `{"query": query, "mode": mode, "path": path, "limit": limit, "glob": glob, "literal": literal}`,
	},
	{
		name: "bash",
		stub: `command, timeout=None, cwd=None`,
		doc:  `Run a shell command in the foreground and wait. Returns combined output and exit status as text. Background modes are not available from scripts.`,
		args: `{"command": command, "timeout": timeout, "cwd": cwd}`,
	},
	{
		name: "fetch",
		stub: `url=None, urls=None, query=None, max_results=None`,
		doc:  `Fetch URLs (url, or up to several in urls, in parallel) or web-search a query. Returns page/search content as text.`,
		args: `{"url": url, "urls": urls, "query": query, "max_results": max_results}`,
	},
}

// allowedToolSet is the dispatch-side allow-list — the RPC server refuses
// anything outside it before the executor ever sees the call.
func allowedToolSet() map[string]bool {
	m := make(map[string]bool, len(sandboxTools))
	for _, t := range sandboxTools {
		m[t.name] = true
	}
	return m
}

// stubModule renders the a1tools.py source the script imports. One UDS
// transport (a1 is a single local binary — hermes's file-RPC and remote
// kernel transports are product surface we did not steal), the shared
// helpers hermes mined out of production failures, and one function per
// allow-listed tool.
func stubModule() string {
	var b strings.Builder
	b.WriteString(stubHeader)
	for _, t := range sandboxTools {
		fmt.Fprintf(&b, "\n\ndef %s(%s):\n    \"\"\"%s\"\"\"\n    return _call(%q, %s)\n",
			t.name, t.stub, t.doc, t.name, t.args)
	}
	return b.String()
}

// stubHeader is the transport + helpers section of a1tools.py. The
// single-connection design is hermes's: the server handles one client at a
// time with no request-id in the protocol, so concurrent _call() from
// multiple threads would race on the shared socket and get each other's
// responses — the lock serializes the whole send+recv round-trip.
const stubHeader = `"""Auto-generated a1 tool RPC stubs (import as: from a1tools import read, bash)."""
import json, os, shlex, socket, threading, time

_sock = None
_call_lock = threading.Lock()


class ToolResult(str):
    """A tool call result: usable as a plain string, with .ok recording
    whether the call succeeded. Failed calls (permission denial, tool error)
    still return their message text so a script can branch, print it, or
    keep going — check .ok before trusting content."""
    ok = True


def _connect():
    global _sock
    if _sock is None:
        _sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        _sock.connect(os.environ["A1_TOOLS_SOCKET"])
        _sock.settimeout(300)
    return _sock


def _call(tool_name, args):
    """Send one tool call to the agent and return its result as ToolResult."""
    payload = {k: v for k, v in args.items() if v is not None}
    request = json.dumps({
        "tool": tool_name,
        "args": payload,
        "token": os.environ.get("A1_TOOLS_TOKEN", ""),
    }) + "\n"
    with _call_lock:
        conn = _connect()
        conn.sendall(request.encode())
        buf = b""
        while True:
            chunk = conn.recv(65536)
            if not chunk:
                raise RuntimeError("agent process disconnected (the script tool call was likely canceled)")
            buf += chunk
            if buf.endswith(b"\n"):
                break
    reply = json.loads(buf.decode())
    result = ToolResult(reply.get("result", ""))
    result.ok = bool(reply.get("ok"))
    return result


# ---------------------------------------------------------------------------
# Convenience helpers (avoid common scripting pitfalls)
# ---------------------------------------------------------------------------

def json_parse(text):
    """Parse JSON tolerant of control characters and UTF-8 BOM (strict=False).
    Use this when parsing JSON *inside* bash()/fetch() output — not on a tool
    result itself (results are plain text, not JSON envelopes)."""
    if isinstance(text, str) and text.startswith("\ufeff"):
        text = text[1:]
    return json.loads(text, strict=False)


def shell_quote(s):
    """Shell-escape a string for safe interpolation into bash() commands:
        bash("echo " + shell_quote(user_input))"""
    return shlex.quote(s)


def retry(fn, max_attempts=3, delay=2):
    """Retry a function up to max_attempts times with exponential backoff.
    Use for transient failures (network errors, rate limits):
        result = retry(lambda: bash("go build ./..."))"""
    last_err = None
    for attempt in range(max_attempts):
        try:
            return fn()
        except Exception as e:
            last_err = e
            if attempt < max_attempts - 1:
                time.sleep(delay * (2 ** attempt))
    raise last_err
`

// stubToolDocLines renders the per-tool documentation block for the script
// tool's model-facing description — the same signatures the stubs carry, so
// the description never drifts from the generated module.
func stubToolDocLines() string {
	var b strings.Builder
	for _, t := range sandboxTools {
		fmt.Fprintf(&b, "  %s(%s)\n    %s\n", t.name, t.stub, t.doc)
	}
	return strings.TrimRight(b.String(), "\n")
}

// allowedToolNames returns the sorted allow-list, for refusal messages.
func allowedToolNames() string {
	names := make([]string, 0, len(sandboxTools))
	for _, t := range sandboxTools {
		names = append(names, t.name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
