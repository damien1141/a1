package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
)

// readCacheEntry remembers the last full `read` of a file so an identical
// re-read of an unchanged file can collapse to a short note instead of
// re-sending the content. callID names the tool message that carries that
// content — the liveness of THAT message in the live context is what makes
// elision safe (see readResultLive).
type readCacheEntry struct {
	callID string
	mtime  time.Time
	args   string // canonical file-mode args (offset/limit); identical reads only
}

// readElideNote replaces the content of an elided read. It must teach the
// escape hatch, or a model that needs the content again is stuck.
const readElideNote = "Unchanged since your earlier read of this file (same modification time) — the earlier content is still in your context above. Pass {\"refresh\": true} to receive the content again."

// elideUnchangedRead is the executor's read elider. It returns the elision
// note when this exact read (same resolved file, same mtime, same
// offset/limit) has already been served AND the tool message carrying that
// earlier content still rides in the live context window.
//
// Compaction-aware by construction: when the fold summarizes the earlier read
// away, its message is gone from PathEntries, the liveness check fails, and
// the full content is served again. That was the failure mode of the naive
// (path, mtime) cache — a model post-compaction would get a note pointing at
// content it no longer has, with no way to recover it.
//
// Bookkeeping: every non-elided file-mode read records its entry (its own
// message is the content carrier). An ELIDED read deliberately keeps the
// previous entry — the elided call's message carries only the note, so it
// must never become the liveness anchor.
func (engine *Engine) elideUnchangedRead(call llm.ToolCall, args json.RawMessage) (string, bool) {
	if engine == nil || engine.session == nil {
		return "", false
	}
	var in struct {
		Mode    string `json:"mode"`
		Path    string `json:"path"`
		Offset  int    `json:"offset"`
		Limit   int    `json:"limit"`
		Refresh bool   `json:"refresh"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", false // malformed args: the tool's own error is the answer
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode != "" && mode != "file" {
		return "", false // ls/watch carry their own freshness story
	}
	p := strings.TrimSpace(in.Path)
	if p == "" {
		return "", false
	}
	if filepath.IsAbs(p) {
		p = filepath.Clean(p)
	} else {
		p = filepath.Join(engine.SessionCwd(), p)
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", false // missing/unreadable: let the tool report it, cache nothing
	}
	// Offset/limit are part of the identity: a paginated read of the same
	// unchanged file is a different question, not a repeat.
	canonical := fmt.Sprintf("off=%d;lim=%d", in.Offset, in.Limit)

	engine.readCacheMu.Lock()
	defer engine.readCacheMu.Unlock()
	if engine.readCache == nil {
		engine.readCache = make(map[string]readCacheEntry)
	}
	prev, ok := engine.readCache[p]
	if ok && !in.Refresh && prev.mtime.Equal(info.ModTime()) && prev.args == canonical && engine.readResultLive(prev.callID) {
		return readElideNote, true
	}
	engine.readCache[p] = readCacheEntry{callID: call.ID, mtime: info.ModTime(), args: canonical}
	return "", false
}

// readResultLive reports whether the tool message for callID still rides in
// the live context. PathEntries reflects the post-compaction chain — a folded
// message is absent — so this is exactly the "earlier content still in the
// window" check that keeps elision honest.
func (engine *Engine) readResultLive(callID string) bool {
	if callID == "" {
		return false
	}
	for _, e := range engine.session.PathEntries() {
		m, ok := e.(session.SessionMessageEntry)
		if !ok {
			continue
		}
		if m.Message.Role == llm.RoleTool && m.Message.ToolCallID == callID {
			return true
		}
	}
	return false
}
