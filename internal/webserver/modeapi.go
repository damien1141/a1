package webserver

import (
        "net/http"
        "strings"

        "github.com/damien1141/a1/internal/permission"
)

// modeRequest carries the requested permission mode. "cycle" advances to the
// next mode, which is how the switcher pill behaves on click.
type modeRequest struct {
        Mode string `json:"mode"`
}

// modeResponse is the switcher's state: the active mode plus every mode with
// its label and summary, so the client renders one static menu rather than
// hard-coding names that could drift from the Go constants.
type modeResponse struct {
        Mode    string      `json:"mode"`
        Label   string      `json:"label"`
        Summary string      `json:"summary"`
        Modes   []modeEntry `json:"modes"`
}

type modeEntry struct {
        Mode    string `json:"mode"`
        Label   string `json:"label"`
        Summary string `json:"summary"`
        Active  bool   `json:"active"`
}

// handleGetMode reports the live permission mode.
func (s *Server) handleGetMode(w http.ResponseWriter, _ *http.Request) {
        writeJSON(w, http.StatusOK, s.modeView())
}

// handleSetMode switches the permission mode and rebuilds the gate for every
// live engine. An unknown mode is rejected, so a typo can never silently land
// on interactive (which would green-light what the user just restricted).
// WHERE the write lands depends on the active persona, mirroring the memory
// toggle's ownership pattern: a persona carrying an explicit PermissionMode
// owns the setting (the switcher edits the persona, persisted to the store),
// while a persona without one writes the surface's global mode.
func (s *Server) handleSetMode(w http.ResponseWriter, r *http.Request) {
        var req modeRequest
        if err := decodeBody(r, &req); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }

        mode := strings.TrimSpace(req.Mode)
        if mode == "cycle" {
                next := s.effectiveCycleNext()
                if err := s.setPermissionMode(next); err != nil {
                        writeStatus(w, http.StatusInternalServerError, err.Error())
                        return
                }
                if s.personaPermissionMode() != "" {
                        // Persona-owned mode: a cycle from a persona override
                        // writes the persona so the pick survives restarts and
                        // future sessions — same ownership rule as an explicit
                        // pick below.
                        if !s.writePersonaPermissionMode(w, next) {
                                return
                        }
                } else {
                        // Surface-driven change: the engines' restore base follows,
                        // so a later persona-settings pass (memory toggle, persona
                        // switch) cannot revert the gate to its boot-time mode.
                        s.updateEngineBaseModes(next)
                }
                writeJSON(w, http.StatusOK, s.modeView())
                return
        }

        if err := s.setPermissionMode(permission.Mode(mode)); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        if s.personaPermissionMode() != "" {
                if !s.writePersonaPermissionMode(w, permission.Mode(mode)) {
                        return
                }
        } else {
                s.updateEngineBaseModes(permission.Mode(mode))
        }
        writeJSON(w, http.StatusOK, s.modeView())
}

// writePersonaPermissionMode persists mode as the ACTIVE persona's
// PermissionMode override and re-applies persona settings to live engines.
// Returns false (and has written the HTTP error) when the store rejects the
// write.
func (s *Server) writePersonaPermissionMode(w http.ResponseWriter, mode permission.Mode) bool {
        p, err := s.personaStore.ActivePersona()
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return false
        }
        m := string(mode)
        if _, err := s.personaStore.SetSettings(p.ID, nil, &m, nil); err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return false
        }
        // The modeStore + gates were already switched by setPermissionMode
        // above; re-applying the persona keeps engines' settings in sync with
        // the persisted override.
        s.applyPersonaToEngines()
        s.broadcastState()
        return true
}

// effectiveCycleNext advances the EFFECTIVE mode (persona override folded
// in) to the next entry, so cycling from a persona-owned mode steps from
// what the user actually sees, not from the raw base underneath it.
func (s *Server) effectiveCycleNext() permission.Mode {
        cur := permission.Mode(s.effectivePermissionMode())
        for i, m := range webPermissionModes {
                if m == cur {
                        return webPermissionModes[(i+1)%len(webPermissionModes)]
                }
        }
        return s.modeStore().cycle()
}

// modeView renders the switcher's current state. The current mode is the
// EFFECTIVE one — the persona's explicit PermissionMode when set, otherwise
// the surface's raw mode — so the pill highlights what new sessions
// actually run, matching what the engines fold in applyPersonaSettings.
func (s *Server) modeView() modeResponse {
        current := permission.Mode(s.effectivePermissionMode())
        out := modeResponse{
                Mode:    string(current),
                Label:   modeLabel[current],
                Summary: modeSummary[current],
                Modes:   make([]modeEntry, 0, len(webPermissionModes)),
        }
        for _, m := range webPermissionModes {
                out.Modes = append(out.Modes, modeEntry{
                        Mode:    string(m),
                        Label:   modeLabel[m],
                        Summary: modeSummary[m],
                        Active:  m == current,
                })
        }
        return out
}

// pinRequest toggles a session's pin. A session with no turns has nothing to
// pin yet, so the id alone is enough.
type pinRequest struct {
        SessionID string `json:"sessionId"`
}

// pinResponse is the pin state after a toggle.
type pinResponse struct {
        SessionID string   `json:"sessionId"`
        Pinned    bool     `json:"pinned"`
        Pins      []string `json:"pins"`
}

// handleGetPins lists pinned session ids.
func (s *Server) handleGetPins(w http.ResponseWriter, _ *http.Request) {
        pins := s.pinStore().list()
        if pins == nil {
                pins = []string{}
        }
        writeJSON(w, http.StatusOK, pinResponse{Pins: pins})
}

// handleTogglePin pins or unpins a session and reports the new state.
func (s *Server) handleTogglePin(w http.ResponseWriter, r *http.Request) {
        var req pinRequest
        if err := decodeBody(r, &req); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        id := strings.TrimSpace(req.SessionID)
        if id == "" {
                writeStatus(w, http.StatusBadRequest, "session id is required")
                return
        }
        pinned, err := s.pinStore().toggle(id)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        writeJSON(w, http.StatusOK, pinResponse{SessionID: id, Pinned: pinned, Pins: s.pinStore().list()})
}
