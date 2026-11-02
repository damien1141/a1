package webserver

import (
        "net/http"
        "os"
        "path/filepath"
        "strings"

        "github.com/damien1141/a1/internal/session"
)

// sessions.go — DELETE /api/session: remove a stored conversation from the
// sidebar. Three stores hold session state and all three must agree by the
// time the 200 lands:
//
//   - the transcript itself: the vault row (Bet 08, FK-cascades its
//     entries) and/or the jsonl file on disk — including the pre-migration
//     shadow file a vault session may still have beside it,
//   - the pin list (pins.json) — an unpinned delete would otherwise leave a
//     dangling pin that resurrects the row's ghost in the pinned section,
//   - the live engine cache — a loaded engine keeps writing if a turn
//     starts later, silently re-creating the file.
//
// A session with a turn in flight is refused (409): deleting the
// transcript under a streaming agent corrupts the very history it is
// writing.

// handleDeleteSession removes a stored session, its pin, and any live
// engine. The sidebar's two-step confirm already asked for intent; this
// path is the destructive one.
func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
        if !s.requireProject(w) {
                return
        }
        id := strings.TrimSpace(r.URL.Query().Get("id"))
        if id == "" {
                writeStatus(w, http.StatusBadRequest, "session id is required")
                return
        }

        // Evict the live engine first (under s.mu, matching liveFor's
        // registry access), but refuse when its turn mutex is held — that
        // means a stream is writing the history we are about to delete.
        s.mu.Lock()
        if le, ok := s.engines[id]; ok {
                if !le.turn.TryLock() {
                        s.mu.Unlock()
                        writeStatus(w, http.StatusConflict, "session has a turn in flight — cancel it first")
                        return
                }
                le.turn.Unlock()
                delete(s.engines, id)
        }
        s.mu.Unlock()

        // Existence check through the same listing the sidebar renders: a
        // typo'd or already-deleted id is a clean 404, never a destructive
        // "success" the caller could mistake for having removed something.
        sessions, err := s.listSessions()
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, "list sessions: "+err.Error())
                return
        }
        known := false
        for _, m := range sessions {
                if m.ID == id {
                        known = true
                        break
                }
        }
        if !known {
                writeStatus(w, http.StatusNotFound, "no stored session with that id")
                return
        }

        // Unpin: a stale pin would pin a deleted id forever (toggle can no
        // longer reach it once the row is gone).
        s.pinStore().remove(id)

        // Vault-backed sessions live as rows; HasSession distinguishes them
        // from the plain file layout without trusting the synthetic path.
        if v := session.DefaultVault(); v != nil {
                if has, err := v.HasSession(id); err == nil && has {
                        if err := v.DeleteSession(id); err != nil {
                                writeStatus(w, http.StatusInternalServerError, "delete vault session: "+err.Error())
                                return
                        }
                }
        }

        // Remove the jsonl file(s): the plain-layout session's transcript, and
        // the pre-migration shadow file a vault session may still carry (the
        // vault delete above does not touch disk). Matching mirrors
        // sessionIDFromFilename: <timestamp>_<id>.jsonl with the id after the
        // FIRST underscore — the timestamp never contains one, while session
        // ids may (LastIndexByte silently never matched those, leaving the
        // file on disk after a "successful" delete).
        if err := deleteSessionFiles(s.proj.SessionDir(), id); err != nil {
                writeStatus(w, http.StatusInternalServerError, "delete session file: "+err.Error())
                return
        }

        writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

// deleteSessionFiles removes every <ts>_<id>.jsonl under dir whose session
// id matches exactly. Missing dir / no matches are success — the vault row
// may have been the only copy.
func deleteSessionFiles(dir, id string) error {
        entries, err := os.ReadDir(dir)
        if err != nil {
                if os.IsNotExist(err) {
                        return nil
                }
                return err
        }
        for _, e := range entries {
                if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
                        continue
                }
                name := strings.TrimSuffix(e.Name(), ".jsonl")
                idx := strings.IndexByte(name, '_')
                if idx < 0 || name[idx+1:] != id {
                        continue
                }
                if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !os.IsNotExist(err) {
                        return err
                }
        }
        return nil
}
