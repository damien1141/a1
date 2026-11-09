package webserver

import (
        "fmt"
        "net/http"
        "os"
        "strings"

        "github.com/damien1141/a1/internal/project"
)

// themeapi.go is the theme button's persistence (the chunk-8 follow-up the
// user named precisely: "a key that's toggled on and off in the config
// file, by pressing the webui button"). One endpoint:
//
//      PUT /api/theme    write web.dark_mode (light / dark / system)
//
// There is deliberately no GET: the page serve itself stamps the choice
// onto the served document's <html> tag (handlePage → themedPage), so the
// browser never has to ask — the very first paint carries the persisted
// theme, no flash, no round-trip, and a choice saved from one browser
// reaches every other one through the file.
//
// The gate ruling: a theme choice is a UI preference, NOT personal data —
// no personalGate here. The dashboard must honor a dark choice even while
// personal data stays locked, and the ungated /settings config screen (a
// separate document with its own serving path) reads the same key from
// the file directly. But this IS a write to config.yaml, so it composes
// guard() like every other mutating verb: a cross-origin page must not be
// able to flip the user's config file.
//
// "system" is the picker's third state and means REMOVE the key: the OS
// preference drives again, live (the boot snippet's watcher). An absent
// body field is tolerated as "system" — an empty theme is the system
// theme, not a bad request.

// themePutJSON is the PUT body: one of "light", "dark", or "system".
type themePutJSON struct {
        Theme string `json:"theme"`
}

// themeResponseJSON echoes the normalized choice so the client can confirm
// what the file now says.
type themeResponseJSON struct {
        Theme string `json:"theme"`
}

// parseThemeChoice maps the wire spelling onto the config key. dark is the
// value web.dark_mode takes (nil = key absent = follow the OS); normalized
// is the echo; ok is false only for a genuinely unknown spelling.
func parseThemeChoice(s string) (dark *bool, normalized string, ok bool) {
        switch strings.ToLower(strings.TrimSpace(s)) {
        case "dark":
                t := true
                return &t, "dark", true
        case "light":
                f := false
                return &f, "light", true
        case "system", "":
                return nil, "system", true
        }
        return nil, "", false
}

// handleSetTheme writes the choice to config.yaml. On success the project
// config is reloaded — NOT via onConfigSaved, whose engine rebind exists
// for keys tools actually read; no engine or tool consumes web.dark_mode,
// and the only reader that matters (the next page serve) goes through
// proj.Config(), which the reload refreshes.
func (s *Server) handleSetTheme(w http.ResponseWriter, r *http.Request) {
        if !s.requireProject(w) {
                return
        }
        var req themePutJSON
        if err := decodeBody(r, &req); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        dark, normalized, ok := parseThemeChoice(req.Theme)
        if !ok {
                writeStatus(w, http.StatusBadRequest, "theme must be light, dark, or system")
                return
        }
        if err := project.SetWebDarkMode(s.proj.Global(), dark); err != nil {
                writeStatus(w, http.StatusInternalServerError, "save theme: "+err.Error())
                return
        }
        if err := s.proj.LoadConfig(); err != nil {
                // The write already landed; a reload failure must not read
                // as a failed save. Log and answer with the choice.
                fmt.Fprintf(os.Stderr, "web: reload config after theme save: %v\n", err)
        }
        writeJSON(w, http.StatusOK, themeResponseJSON{Theme: normalized})
}
