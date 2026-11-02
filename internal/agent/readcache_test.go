package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	"github.com/damien1141/a1/internal/tools/readtool"
)

// --- read elision: identical re-reads of unchanged files collapse to a note,
// but only while the earlier content provably still rides in the live context.

// readElideCapture records what the model was actually sent, including tool
// call ids (the stock capturedChatRequest drops them, and the whole point
// here is knowing WHICH read a tool message belongs to).
type readElideCapture struct {
	Stream   bool
	Messages []struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		ToolCallID string `json:"tool_call_id"`
	}
}

// readElideServer scripts five read rounds then a final text reply. Before
// serving round 5 it rewrites the file (content + a future-dated mtime, so
// the change is visible regardless of filesystem timestamp granularity):
//
//	r1  read          → full content ("alpha")
//	r2  read          → elided note
//	r3  read refresh  → full content (forced)
//	r4  read          → elided note (refresh re-armed the cache)
//	r5  read (changed)→ full content ("gamma")
func readElideServer(t *testing.T, file string) (*httptest.Server, func() []readElideCapture) {
	t.Helper()
	var mu sync.Mutex
	var captured []readElideCapture
	var streamN atomic.Int32
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
		cap := readElideCapture{Stream: req.Stream}
		_ = json.Unmarshal(body, &cap)
		mu.Lock()
		captured = append(captured, cap)
		mu.Unlock()

		if !req.Stream {
			// Memory-outlet consolidation shape; not exercised here, but the
			// branch keeps the server total for any engine that calls it.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message":       map[string]string{"role": "assistant", "content": `{"ops":[]}`},
					"finish_reason": "stop",
				}},
			})
			return
		}

		n := int(streamN.Add(1))
		w.Header().Set("Content-Type", "text/event-stream")
		var out string
		switch n {
		case 1:
			out = sseToolCallChunk(fmt.Sprintf("call_%d", n), "read", `{"path":"notes.txt"}`)
		case 2:
			out = sseToolCallChunk(fmt.Sprintf("call_%d", n), "read", `{"path":"notes.txt"}`)
		case 3:
			out = sseToolCallChunk(fmt.Sprintf("call_%d", n), "read", `{"path":"notes.txt","refresh":true}`)
		case 4:
			out = sseToolCallChunk(fmt.Sprintf("call_%d", n), "read", `{"path":"notes.txt"}`)
		case 5:
			// Rewrite the file before the model's round-5 read runs: new
			// content plus a mtime far in the future, so the cache sees a
			// change even on coarse-grained filesystems.
			require.NoError(t, os.WriteFile(file, []byte("gamma\n"), 0o644))
			future := time.Now().Add(time.Hour)
			require.NoError(t, os.Chtimes(file, future, future))
			out = sseToolCallChunk(fmt.Sprintf("call_%d", n), "read", `{"path":"notes.txt"}`)
		default:
			out = sseTextChunk("done")
		}
		_, _ = fmt.Fprint(w, out)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	return srv, func() []readElideCapture {
		mu.Lock()
		defer mu.Unlock()
		return append([]readElideCapture(nil), captured...)
	}
}

// TestReadElisionLoop drives the full engine loop: the model reads the same
// file five times and the tool messages the model actually receives must be
// full/note/full/note/full in that order. The last request's message list is
// the ground truth — it is exactly what the model sees on the final round.
func TestReadElisionLoop(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notes.txt")
	require.NoError(t, os.WriteFile(file, []byte("alpha\n"), 0o644))

	srv, requests := readElideServer(t, file)
	defer srv.Close()

	sess, err := NewSession(WithCwd(dir))
	require.NoError(t, err)
	eng, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{readtool.ReadTool()}),
	)
	require.NoError(t, err)
	require.NoError(t, drainLoop(t, eng, "read notes.txt five times"))

	reqs := requests()
	require.NotEmpty(t, reqs)
	var last readElideCapture
	for i := range reqs {
		if reqs[i].Stream {
			last = reqs[i]
		}
	}
	byCall := make(map[string]string)
	for _, m := range last.Messages {
		if m.ToolCallID != "" {
			byCall[m.ToolCallID] = m.Content
		}
	}
	require.Contains(t, byCall, "call_1")
	assert.Contains(t, byCall["call_1"], "alpha", "first read serves full content")

	require.Contains(t, byCall, "call_2")
	assert.Contains(t, byCall["call_2"], "Unchanged since your earlier read",
		"identical re-read of an unchanged file collapses to the note")
	assert.Contains(t, byCall["call_2"], `{"refresh": true}`, "the note teaches the escape hatch")

	require.Contains(t, byCall, "call_3")
	assert.Contains(t, byCall["call_3"], "alpha", "refresh forces the full content")

	require.Contains(t, byCall, "call_4")
	assert.Contains(t, byCall["call_4"], "Unchanged since your earlier read",
		"a refreshed read re-arms elision for the next identical read")

	require.Contains(t, byCall, "call_5")
	assert.Contains(t, byCall["call_5"], "gamma", "a changed file (new mtime) serves full content")
	assert.NotContains(t, byCall["call_5"], "Unchanged since your earlier read")
}

// TestReadElisionCompactionAware pins the safety property the naive
// (path, mtime) cache got wrong: once compaction folds the earlier read's
// message away, the next read must serve full content again — a note that
// points at content the model no longer has is unrecoverable.
func TestReadElisionCompactionAware(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(file, []byte("content\n"), 0o644))

	sess, err := NewSession(WithCwd(dir))
	require.NoError(t, err)
	eng, err := NewEngine(llm.ModelConfig{Name: "m"}, sess)
	require.NoError(t, err)

	readCall := func(id, args string) llm.ToolCall {
		return llm.ToolCall{ID: id, Type: "function", Function: llm.Function{Name: "read", Arguments: args}}
	}
	const args = `{"path":"a.txt"}`

	// First read records the entry; its tool message is not in the session
	// yet (the executor has not returned it), so nothing elides.
	_, elided := eng.elideUnchangedRead(readCall("r1", args), json.RawMessage(args))
	require.False(t, elided, "first read must serve full content")

	// The loop appends the tool result; an identical read now elides.
	require.NoError(t, sess.Append(llm.Message{Role: llm.RoleTool, ToolCallID: "r1", Content: "content"}))
	note, elided := eng.elideUnchangedRead(readCall("r2", args), json.RawMessage(args))
	require.True(t, elided, "identical re-read with live content must elide")
	assert.Contains(t, note, "Unchanged since your earlier read")

	// Elided reads must keep the ORIGINAL entry as the liveness anchor —
	// the elided call's own message carries only the note. A second elision
	// against r2's message would survive compaction of r1 and lie.
	note2, elided2 := eng.elideUnchangedRead(readCall("r2b", args), json.RawMessage(args))
	require.True(t, elided2)
	assert.Equal(t, note, note2)

	// Compaction folds everything before a later entry away; the r1 message
	// leaves the live context, so elision must not fire.
	require.NoError(t, sess.Append(llm.Message{Role: llm.RoleUser, Content: "later"}))
	kept := sess.LastID()
	require.NoError(t, sess.AppendCompaction(session.Compaction{Summary: "summarized", FirstKeptEntryID: kept}))
	_, elided = eng.elideUnchangedRead(readCall("r3", args), json.RawMessage(args))
	require.False(t, elided, "a compacted-away read must serve full content again")
}

// TestReadElisionConservativeEdges covers the no-elide corners: pagination
// changes the question, ls/watch never elide, missing files cache nothing,
// and refresh always serves content.
func TestReadElisionConservativeEdges(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(file, []byte("l1\nl2\n"), 0o644))

	sess, err := NewSession(WithCwd(dir))
	require.NoError(t, err)
	eng, err := NewEngine(llm.ModelConfig{Name: "m"}, sess)
	require.NoError(t, err)

	readCall := func(id, args string) llm.ToolCall {
		return llm.ToolCall{ID: id, Type: "function", Function: llm.Function{Name: "read", Arguments: args}}
	}

	// Seed: one full read whose message rides in the session.
	_, elided := eng.elideUnchangedRead(readCall("s1", `{"path":"a.txt"}`), json.RawMessage(`{"path":"a.txt"}`))
	require.False(t, elided)
	require.NoError(t, sess.Append(llm.Message{Role: llm.RoleTool, ToolCallID: "s1", Content: "l1"}))

	// Different pagination: same file, different question — full content.
	_, elided = eng.elideUnchangedRead(readCall("s2", `{"path":"a.txt","offset":2}`), json.RawMessage(`{"path":"a.txt","offset":2}`))
	assert.False(t, elided, "a paginated read is not an identical re-read")

	// ls/watch modes never elide (they carry their own freshness story).
	_, elided = eng.elideUnchangedRead(readCall("s3", `{"mode":"ls","path":"."}`), json.RawMessage(`{"mode":"ls","path":"."}`))
	assert.False(t, elided, "mode=ls must never elide")
	_, elided = eng.elideUnchangedRead(readCall("s4", `{"mode":"watch","path":"."}`), json.RawMessage(`{"mode":"watch","path":"."}`))
	assert.False(t, elided, "mode=watch must never elide")

	// refresh always serves content.
	_, elided = eng.elideUnchangedRead(readCall("s5", `{"path":"a.txt","refresh":true}`), json.RawMessage(`{"path":"a.txt","refresh":true}`))
	assert.False(t, elided, "refresh must serve full content")

	// A missing file elides nothing and records nothing.
	_, elided = eng.elideUnchangedRead(readCall("s6", `{"path":"gone.txt"}`), json.RawMessage(`{"path":"gone.txt"}`))
	assert.False(t, elided, "missing file must not elide")
	// ...proven by the same path still not eliding after its "read" would
	// have been appended (the entry was never recorded).
	require.NoError(t, sess.Append(llm.Message{Role: llm.RoleTool, ToolCallID: "s6", Content: "no such file"}))
	_, elided = eng.elideUnchangedRead(readCall("s7", `{"path":"gone.txt"}`), json.RawMessage(`{"path":"gone.txt"}`))
	assert.False(t, elided, "a failed read must not arm elision")
}

// TestReadElisionBareExecutorOff proves the nil-hook default: an executor
// without an installed elider serves full content, which is also the shape
// every non-engine caller (tests, extensions) sees.
func TestReadElisionBareExecutorOff(t *testing.T) {
	exec := NewExecutor(tools.NewRegistry([]tools.Tool{readtool.ReadTool()}), permission.AllowAll{}, nil, nil, nil)
	exec.SetMeta("s", t.TempDir())
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(file, []byte("hello\n"), 0o644))

	args := json.RawMessage(`{"path":"` + filepath.ToSlash(file) + `"}`)
	calls := []llm.ToolCall{
		{ID: "r1", Type: "function", Function: llm.Function{Name: "read", Arguments: string(args)}},
		{ID: "r2", Type: "function", Function: llm.Function{Name: "read", Arguments: string(args)}},
	}
	msgs, _, _ := exec.Run(context.Background(), calls, func(session.ToolData) bool { return true })
	require.Len(t, msgs, 2)
	assert.Contains(t, msgs[0].Content, "hello")
	assert.Contains(t, msgs[1].Content, "hello", "no elider installed: identical re-read serves full content")
}
