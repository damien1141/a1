package webserver

import (
        "net/http"

        "github.com/damien1141/a1/internal/project"
)

// personalStatusResponse is the dashboard's ground truth for the personal
// widgets: whether the master switch is on and where the personal data
// lives. It is deliberately UNGATED — the dashboard needs it to render
// honest locked states — and it exposes no personal content itself, only
// the switch, the configured paths, and the mail account COUNT (a number,
// never a name: the account labels ride the gated mail endpoints).
type personalStatusResponse struct {
        Enabled       bool   `json:"enabled"`
        CalendarDir   string `json:"calendarDir"`
        MailDir       string `json:"mailDir"`
        MailAccounts  int    `json:"mailAccounts"`
}

// personalConfig resolves the current personal section off the live project
// config (nil-safe for tests building a Server without one).
func (s *Server) personalConfig() project.PersonalConfig {
        if s.proj == nil || s.proj.Config() == nil {
                return project.PersonalConfig{}
        }
        return s.proj.Config().Personal
}

// handlePersonalStatus reports the master switch + configured sources.
func (s *Server) handlePersonalStatus(w http.ResponseWriter, _ *http.Request) {
        p := s.personalConfig()
        writeJSON(w, http.StatusOK, personalStatusResponse{
                Enabled:      p.EnabledResolved(),
                CalendarDir:  p.CalendarDir,
                MailDir:      p.MailDir,
                MailAccounts: len(p.ResolvedMailAccounts()),
        })
}

// personalGate is the server-side enforcement for every /api/personal/*
// endpoint EXCEPT status: when the master switch is off the handler never
// runs. The config editor's checkbox only hides widgets client-side — this
// is the gate that actually matters. The calendar / kanban / mail handlers
// (dashboard chunks 2-4) compose it: s.personalGate(s.handleXxx).
func (s *Server) personalGate(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                if !s.personalConfig().EnabledResolved() {
                        writeStatus(w, http.StatusForbidden, "personal data is disabled (set personal.enabled in config.yaml)")
                        return
                }
                next(w, r)
        }
}
