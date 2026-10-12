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
func (s *Server) handleSetMode(w http.ResponseWriter, r *http.Request) {
	var req modeRequest
	if err := decodeBody(r, &req); err != nil {
		writeStatus(w, http.StatusBadRequest, err.Error())
		return
	}
	modes := s.modeStore()

	mode := strings.TrimSpace(req.Mode)
	if mode == "cycle" {
		if err := s.setPermissionMode(modes.cycle()); err != nil {
			writeStatus(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, s.modeView())
		return
	}

	if err := s.setPermissionMode(permission.Mode(mode)); err != nil {
		writeStatus(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.modeView())
}

// modeView renders the switcher's current state.
func (s *Server) modeView() modeResponse {
	current := s.modeStore().mode()
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
