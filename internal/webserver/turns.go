package webserver

import (
        "context"
        "net/http"
        "strings"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/session"
)

// turns.go — the re-run surfaces: regenerate (re-roll the last assistant
// turn) and edit-user-message (fork a branch from an earlier prompt).
//
// Both reduce to the same primitive: rewind the session cursor to the
// parent of the target user entry, then run a normal turn with the prompt.
// Engine.Loop appends the user message itself, and — because the cursor now
// sits at that entry's parent — the append lands as a SIBLING: a fresh
// branch. The previous response chain is never deleted; the store is
// append-only, so it survives as an off-path branch exactly the way the
// session tree's parent/child links intend.

// regenerateRequest names the session whose last turn should re-roll.
type regenerateRequest struct {
        SessionID string `json:"sessionId"`
}

// editRequest rewrites one user message and re-runs the turn from there.
type editRequest struct {
        SessionID string `json:"sessionId"`
        // UserIndex is the 0-based position of the target among the session's
        // user entries on the current path. The browser counts user rows the
        // same way (live and replayed rows alike), which is why the index — not
        // an entry id — crosses the wire: live turns mint their entry ids
        // server-side inside the turn goroutine, unseen by the client.
        UserIndex int    `json:"userIndex"`
        Prompt    string `json:"prompt"`
}

// userEntry is one user message on the current path, with its tree links.
type userEntry struct {
        id     string
        parent string
        text   string
}

// pathUserEntries walks the session's FULL branch and returns its user
// entries in order — the same row space the JSON transcript renders
// (both walk AllEntries), so the browser's userIndex maps 1:1 even after
// a compaction cut the context path. System notes the engine appends as
// user messages count on both sides alike.
func pathUserEntries(le *liveEngine) []userEntry {
        var out []userEntry
        for _, e := range le.engine.Session().AllEntries() {
                me, ok := e.(session.SessionMessageEntry)
                // Runtime riders are persisted context, not conversation rows: the
                // browser never renders them, so they must not consume a userIndex
                // slot either — the index maps 1:1 to rendered user rows.
                if !ok || me.Message.Role != llm.RoleUser || me.Message.Runtime {
                        continue
                }
                parent := ""
                if p := me.GetParent(); p != nil {
                        parent = *p
                }
                out = append(out, userEntry{id: me.GetID(), parent: parent, text: me.Message.Content})
        }
        return out
}

// handleRegenerate re-rolls the last assistant turn: the cursor rewinds to
// the parent of the LAST user entry and the same prompt re-runs as a new
// branch. The browser has already dropped the old bubbles; the fresh turn
// streams over the same SSE subscription.
func (s *Server) handleRegenerate(w http.ResponseWriter, r *http.Request) {
        if !s.requireProject(w) {
                return
        }
        var req regenerateRequest
        if err := decodeBody(r, &req); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        req.SessionID = strings.TrimSpace(req.SessionID)
        if req.SessionID == "" {
                writeStatus(w, http.StatusBadRequest, "session id is required")
                return
        }

        le, err := s.liveFor(req.SessionID)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        // Take the turn lock BEFORE reading the path: a turn finishing between
        // the read and the rewind would make the re-roll target a stale entry.
        if !s.lockTurnForRerun(w, le) {
                return
        }
        users := pathUserEntries(le)
        if len(users) == 0 {
                le.turn.Unlock()
                writeStatus(w, http.StatusBadRequest, "nothing to regenerate — the session has no user message")
                return
        }
        target := users[len(users)-1]
        s.finishRerun(w, le, target.parent, target.text, "regenerating…")
}

// handleEdit forks a new branch from an edited user message: the cursor
// rewinds to that entry's parent and the edited text runs as the branch's
// first prompt, as if it had been said then.
func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
        if !s.requireProject(w) {
                return
        }
        var req editRequest
        if err := decodeBody(r, &req); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        req.SessionID = strings.TrimSpace(req.SessionID)
        if req.SessionID == "" {
                writeStatus(w, http.StatusBadRequest, "session id is required")
                return
        }
        if strings.TrimSpace(req.Prompt) == "" {
                writeStatus(w, http.StatusBadRequest, "prompt is empty")
                return
        }

        le, err := s.liveFor(req.SessionID)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        if !s.lockTurnForRerun(w, le) {
                return
        }
        users := pathUserEntries(le)
        if req.UserIndex < 0 || req.UserIndex >= len(users) {
                le.turn.Unlock()
                writeStatus(w, http.StatusBadRequest, "user message index out of range")
                return
        }
        target := users[req.UserIndex]
        s.finishRerun(w, le, target.parent, req.Prompt, "re-running from edit…")
}

// lockTurnForRerun takes the engine's turn mutex for a re-run, checking
// engine membership under s.mu while doing it so a concurrent DELETE
// /api/session cannot evict the engine out from under a rerun that is
// about to hold the lock (TryLock never blocks, so holding s.mu across it
// cannot deadlock with the turn goroutine's le.turn → s.mu ordering).
// Writes the error response itself and returns false when the rerun must
// not proceed. On success the CALLER holds le.turn and must either hand
// it to finishRerun's goroutine or unlock it on every error path.
func (s *Server) lockTurnForRerun(w http.ResponseWriter, le *liveEngine) bool {
        s.mu.Lock()
        _, stillLive := s.engines[le.engine.SessionID()]
        if stillLive {
                stillLive = le.turn.TryLock()
        }
        s.mu.Unlock()
        if !stillLive {
                writeStatus(w, http.StatusConflict, "session is busy or no longer available")
                return false
        }
        return true
}

// finishRerun rewinds the engine's cursor to rewindTo ("" = session root),
// then kicks off a normal turn with prompt — Loop's own user-message append
// becomes the new branch's first entry. The CALLER holds le.turn (via
// lockTurnForRerun); this function transfers it to the turn goroutine.
// Response is 202 like /api/prompt: results stream over SSE.
func (s *Server) finishRerun(w http.ResponseWriter, le *liveEngine, rewindTo, prompt, statusText string) {
        sessionID := le.engine.SessionID()
        if err := le.engine.Session().RewindTo(rewindTo); err != nil {
                le.turn.Unlock()
                writeStatus(w, http.StatusInternalServerError, "rewind: "+err.Error())
                return
        }
        //nolint:gosec // G118: the turn must outlive this request; /api/cancel stops it
        ctx, cancel := context.WithCancel(context.Background())
        le.cancel = cancel
        go func() {
                defer le.turn.Unlock()
                defer cancel()
                s.events().publish(sessionID, serverEvent{Kind: eventStatus, Status: &statusPayload{Text: statusText}})
                s.runTurn(withSession(ctx, sessionID), le, prompt)
        }()
        writeJSON(w, http.StatusAccepted, map[string]string{"sessionId": sessionID})
}
