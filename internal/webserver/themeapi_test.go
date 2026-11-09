package webserver

import (
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// The theme button's trio (themeapi.go): PUT /api/theme writes
// web.dark_mode to config.yaml, the page serve stamps the choice onto the
// served document (no GET needed — the HTML IS the read), and the write
// composes the CSRF guard like every other mutating verb. A UI preference,
// not personal data: the gate ruling under test is guard-only — a LOCKED
// personal section must not stop the toggle.

// doTheme issues one request against the server's routes.
func doTheme(t *testing.T, srv *Server, method, path, body, origin string) *httptest.ResponseRecorder {
        t.Helper()
        req := httptest.NewRequest(method, path, strings.NewReader(body))
        req.Header.Set("Content-Type", "application/json")
        if origin != "" {
                req.Header.Set("Origin", origin)
        }
        rec := httptest.NewRecorder()
        srv.routes().ServeHTTP(rec, req)
        return rec
}

// themeConfigPath resolves the fixture's config file (the fixture set HOME
// before Discover, so the global layout points inside it).
func themeConfigPath(t *testing.T) string {
        t.Helper()
        home, err := os.UserHomeDir()
        require.NoError(t, err)
        return filepath.Join(home, ".a1", "config.yaml")
}

// getThemePage fetches "/" through the real routes.
func getThemePage(t *testing.T, srv *Server) string {
        t.Helper()
        rec := doTheme(t, srv, http.MethodGet, "/", "", "")
        require.Equal(t, http.StatusOK, rec.Code)
        return rec.Body.String()
}

// ---------- positive ----------

// TestThemePutPersistsAndStamps_Positive: the button's whole contract in
// one arc — PUT dark lands the key in config.yaml, the very next page
// serve carries it stamped on the document (the reload the config screen
// and every other browser see), PUT system removes it, and the page serve
// follows back to unstamped. The PUT works with personal data LOCKED: no
// personalGate on a UI preference.
func TestThemePutPersistsAndStamps_Positive(t *testing.T) {
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: false\n")

        rec := doTheme(t, srv, http.MethodPut, "/api/theme", `{"theme":"dark"}`, "")
        require.Equal(t, http.StatusOK, rec.Code)
        var resp themeResponseJSON
        require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
        assert.Equal(t, "dark", resp.Theme)

        data, err := os.ReadFile(themeConfigPath(t))
        require.NoError(t, err)
        assert.Contains(t, string(data), "dark_mode: true", "the key lands in config.yaml")

        page := getThemePage(t, srv)
        assert.Contains(t, page, `<html lang="en" data-theme="dark">`, "the next page serve stamps the choice")

        // The toggle off: system removes the key entirely.
        rec = doTheme(t, srv, http.MethodPut, "/api/theme", `{"theme":"system"}`, "")
        require.Equal(t, http.StatusOK, rec.Code)
        require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
        assert.Equal(t, "system", resp.Theme)

        data, err = os.ReadFile(themeConfigPath(t))
        require.NoError(t, err)
        assert.NotContains(t, string(data), "dark_mode", "system removes the key")
        assert.NotContains(t, string(data), "web:", "the emptied web block is pruned")

        page = getThemePage(t, srv)
        assert.Contains(t, page, `<html lang="en">`)
        assert.NotContains(t, page, `<html lang="en" data-theme`, "unstamped again — the boot chain applies")

        // And light is an explicit choice, not an absence.
        rec = doTheme(t, srv, http.MethodPut, "/api/theme", `{"theme":"light"}`, "")
        require.Equal(t, http.StatusOK, rec.Code)
        data, err = os.ReadFile(themeConfigPath(t))
        require.NoError(t, err)
        assert.Contains(t, string(data), "dark_mode: false")
        page = getThemePage(t, srv)
        assert.Contains(t, page, `<html lang="en" data-theme="light">`)
}

// TestThemePutEmptyThemeMeansSystem_Positive: an omitted field is the
// system default, not a bad request — an empty theme IS the system theme.
func TestThemePutEmptyThemeMeansSystem_Positive(t *testing.T) {
        srv := newPersonalFixtureProject(t, "")
        rec := doTheme(t, srv, http.MethodPut, "/api/theme", `{}`, "")
        require.Equal(t, http.StatusOK, rec.Code)
        var resp themeResponseJSON
        require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
        assert.Equal(t, "system", resp.Theme)
}

// TestThemePutPreservesConfig_Positive: the surgical write takes nothing
// it does not own — comments and sibling blocks survive the toggle.
func TestThemePutPreservesConfig_Positive(t *testing.T) {
        srv := newPersonalFixtureProject(t, "")
        path := themeConfigPath(t)
        require.NoError(t, os.WriteFile(path, []byte("# my a1 config\nmodels:\n  - name: test-model\n    api_key: test-key\n    default: true\n"), 0o644))

        rec := doTheme(t, srv, http.MethodPut, "/api/theme", `{"theme":"dark"}`, "")
        require.Equal(t, http.StatusOK, rec.Code)

        data, err := os.ReadFile(path)
        require.NoError(t, err)
        text := string(data)
        assert.Contains(t, text, "# my a1 config", "comments survive")
        assert.Contains(t, text, "test-key", "models survive")
}

// ---------- negative ----------

// TestThemePutRejectsUnknownTheme_Negative: a configurator that saved
// known-bad values would be a slower way to hand-edit YAML.
func TestThemePutRejectsUnknownTheme_Negative(t *testing.T) {
        srv := newPersonalFixtureProject(t, "")
        rec := doTheme(t, srv, http.MethodPut, "/api/theme", `{"theme":"gruvbox"}`, "")
        assert.Equal(t, http.StatusBadRequest, rec.Code)
        data, err := os.ReadFile(themeConfigPath(t))
        require.NoError(t, err)
        assert.NotContains(t, string(data), "dark_mode", "nothing was written")
}

// TestThemePutCrossOriginBlocked_Negative: the write composes guard() — a
// cross-origin page must not be able to flip the user's config file.
func TestThemePutCrossOriginBlocked_Negative(t *testing.T) {
        srv := newPersonalFixtureProject(t, "")
        rec := doTheme(t, srv, http.MethodPut, "/api/theme", `{"theme":"dark"}`, "http://evil.example")
        assert.Equal(t, http.StatusForbidden, rec.Code)
        data, err := os.ReadFile(themeConfigPath(t))
        require.NoError(t, err)
        assert.NotContains(t, string(data), "dark_mode")
}

// TestThemePutWithoutProject_Negative: a Server built without a project
// answers cleanly instead of panicking on the nil dereference.
func TestThemePutWithoutProject_Negative(t *testing.T) {
        srv := &Server{}
        rec := doTheme(t, srv, http.MethodPut, "/api/theme", `{"theme":"dark"}`, "")
        assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// TestThemePageUnstampedWhenNoChoice_Negative: no key in the file means
// the document serves untouched — the boot snippet's own chain (stored
// choice, then OS preference) applies, and a bare Server (tests) serves
// the same unstamped page.
func TestThemePageUnstampedWhenNoChoice_Negative(t *testing.T) {
        srv := newPersonalFixtureProject(t, "")
        page := getThemePage(t, srv)
        assert.Contains(t, page, `<html lang="en">`)
        assert.NotContains(t, page, `<html lang="en" data-theme`)
}
