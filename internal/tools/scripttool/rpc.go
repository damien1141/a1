package scripttool

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// rpcRequest is one call from the script: the tool name, its JSON args,
// and the per-execution token proving the caller is the child this server
// was started for (any process on the machine can dial the socket; only
// the token holder gets tools).
type rpcRequest struct {
	Tool  string          `json:"tool"`
	Args  json.RawMessage `json:"args"`
	Token string          `json:"token"`
}

// rpcReply is the single response shape: ok=false carries the refusal or
// error text in result, so a script that ignores failure still sees the
// words (and the tool_errors summary in the final result re-surfaces them
// to the model — hermes's tool_errors_since safety net).
type rpcReply struct {
	OK     bool   `json:"ok"`
	Result string `json:"result"`
}

// callLogEntry records one dispatched RPC call for the result contract.
type callLogEntry struct {
	Tool     string
	OK       bool
	Error    string // truncated; set when !OK
	Duration time.Duration
}

// rpcServer serves the script's tool calls over a loopback Unix socket.
// One connection at a time, newline-delimited JSON, serial dispatch — the
// stub serializes its round-trips with a lock for exactly this reason.
type rpcServer struct {
	token    string
	allowed  map[string]bool
	dispatch tooldef.ScriptDispatchFunc

	mu    sync.Mutex
	calls int
	log   []callLogEntry
}

// maxRPCLine bounds one request line. Write args can be large (a generated
// file rides in "content"), but not unboundedly large: 8 MB matches the
// bash collector's cap convention.
const maxRPCLine = 8 * 1024 * 1024

// rpcIdleTimeout is how long a connection may sit silent before the server
// drops it. Generous by design — a script may think between calls; the
// script's own execution timeout is the real bound.
const rpcIdleTimeout = 300 * time.Second

// serve accepts connections until the listener closes or ctx is done,
// answering every request with token check → allow-list → call budget →
// dispatch. Refusals are free; only a dispatched call consumes budget.
func (s *rpcServer) serve(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		if ctx.Err() != nil {
			conn.Close()
			return
		}
		s.serveConn(ctx, conn)
	}
}

func (s *rpcServer) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		if ctx.Err() != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(rpcIdleTimeout))
		line, err := readLine(r)
		if err != nil {
			return
		}
		if len(line) == 0 {
			continue
		}
		reply := s.handleLine(ctx, line)
		_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
		data, _ := json.Marshal(reply)
		if _, err := conn.Write(append(data, '\n')); err != nil {
			return
		}
	}
}

// readLine reads one newline-terminated line with a hard cap. An oversized
// line cannot be resynchronized (the remainder is unknown bytes), so the
// caller drops the connection — the stub sees EOF and raises. io.EOF and
// deadline errors end the connection; a final partial line is dropped.
func readLine(r *bufio.Reader) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > maxRPCLine {
			return nil, errors.New("rpc request line too long")
		}
		if err == nil {
			return trimEOL(buf), nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return nil, err
	}
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func (s *rpcServer) handleLine(ctx context.Context, line []byte) rpcReply {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return rpcReply{OK: false, Result: fmt.Sprintf("invalid script RPC request: %v", err)}
	}
	if !s.tokenOK(req.Token) {
		return rpcReply{OK: false, Result: "unauthorized script RPC request (bad or missing token)"}
	}
	if !s.allowed[req.Tool] {
		return rpcReply{OK: false, Result: fmt.Sprintf(
			"tool '%s' is not available inside scripts. Available: %s", req.Tool, allowedToolNames())}
	}
	s.mu.Lock()
	if s.calls >= maxToolCalls {
		s.mu.Unlock()
		return rpcReply{OK: false, Result: fmt.Sprintf(
			"script tool call limit reached (%d calls). No more tool calls are allowed in this execution.", maxToolCalls)}
	}
	s.calls++
	s.mu.Unlock()

	var args json.RawMessage = req.Args
	if req.Tool == "bash" {
		args = stripBashBackgroundMode(req.Args)
	}
	start := time.Now()
	content, ok := s.dispatch(ctx, req.Tool, args)
	entry := callLogEntry{Tool: req.Tool, OK: ok, Duration: time.Since(start)}
	if !ok {
		entry.Error = truncateRunes(content, 300)
	}
	s.mu.Lock()
	s.log = append(s.log, entry)
	s.mu.Unlock()
	return rpcReply{OK: ok, Result: content}
}

// tokenOK is a constant-time compare; an empty server token fails closed
// (it can only happen if generation was skipped, which the runner never
// does — but the check is free and the failure mode is worth refusing).
func (s *rpcServer) tokenOK(token string) bool {
	if s.token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}

// stripBashBackgroundMode removes the bash tool's background-management
// modes from an in-script bash call, hermes's _TERMINAL_BLOCKED_PARAMS: a
// background shell started inside a script outlives the RPC server (the
// listener closes when the script ends) and escapes the call budget's
// accounting entirely. The foreground default ("run") passes through.
// Bash-only by contract — the caller gates on tool == "bash", since every
// other allow-listed tool has a legitimate `mode` field of its own
// (search mode=auto, read mode=ls) that must never be touched.
func stripBashBackgroundMode(args json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return args
	}
	mode, ok := fields["mode"]
	if !ok {
		return args
	}
	var m string
	if err := json.Unmarshal(mode, &m); err != nil {
		return args
	}
	m = strings.ToLower(strings.TrimSpace(m))
	if m == "" || m == "run" {
		return args
	}
	delete(fields, "mode")
	out, err := json.Marshal(fields)
	if err != nil {
		return args
	}
	return out
}

// toolErrors returns the failed in-script calls (max 5) for the result
// contract: a script that ignores a call's ok=false would otherwise report
// plain success while the write it relied on never happened.
func (s *rpcServer) toolErrors() []callLogEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []callLogEntry
	for _, e := range s.log {
		if !e.OK {
			out = append(out, e)
			if len(out) == 5 {
				break
			}
		}
	}
	return out
}

func (s *rpcServer) callsMade() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// truncateRunes clips s to max runes, rune-safe so a path never ends in
// half a character.
func truncateRunes(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
