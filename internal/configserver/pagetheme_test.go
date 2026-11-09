package configserver

import (
        "bytes"
        "encoding/json"
        "io"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// The config screen's theme trio: the page serve stamps the config file's
// web.dark_mode (the key the chat UI's theme button writes) onto the
// document, the editor document carries its own dark palette + boot chain,
// and a full-document save CANNOT wipe the key the editor has no form for
// (the ToolsDoc carry discipline, extended to the web block).

// getPage fetches the editor document from a Handler over the config at
// path and returns its body.
func getPage(t *testing.T, configPath string) string {
        t.Helper()
        srv := httptest.NewServer(&Handler{ConfigPath: configPath})
        defer srv.Close()
        resp, err := http.Get(srv.URL + "/")
        require.NoError(t, err)
        defer resp.Body.Close()
        require.Equal(t, http.StatusOK, resp.StatusCode)
        body, err := io.ReadAll(resp.Body)
        require.NoError(t, err)
        return string(body)
}

// writeThemeConfig seeds a config file with the given web block.
func writeThemeConfig(t *testing.T, webYAML string) string {
        t.Helper()
        path := filepath.Join(t.TempDir(), "config.yaml")
        err := os.WriteFile(path, []byte("models:\n  - name: m\n    api_key: k\n"+webYAML), 0o644)
        require.NoError(t, err)
        return path
}

func TestPageThemeStampedDark(t *testing.T) {
        page := getPage(t, writeThemeConfig(t, "web:\n  dark_mode: true\n"))
        assert.Contains(t, page, `<html lang="en" data-theme="dark">`, "the config file's choice is stamped on the document")
}

func TestPageThemeStampedLight(t *testing.T) {
        // false is an explicit choice, not an absence — the stamp says light.
        page := getPage(t, writeThemeConfig(t, "web:\n  dark_mode: false\n"))
        assert.Contains(t, page, `<html lang="en" data-theme="light">`)
}

func TestPageThemeUnstampedWhenNoChoice(t *testing.T) {
        page := getPage(t, writeThemeConfig(t, ""))
        assert.NotContains(t, page, `<html lang="en" data-theme`, "no key means the boot script's own chain applies (stored choice, then OS)")
        assert.Contains(t, page, `<html lang="en">`)
}

func TestPageThemeMalformedConfigDegrades(t *testing.T) {
        // A broken config must not break the editor the user would fix it
        // with: the page still serves, unstamped.
        path := filepath.Join(t.TempDir(), "config.yaml")
        require.NoError(t, os.WriteFile(path, []byte("models: [ :"), 0o644))
        page := getPage(t, path)
        assert.Contains(t, page, `<html lang="en">`)
        assert.NotContains(t, page, `<html lang="en" data-theme`)
}

func TestPageCarriesOwnBootAndPalette(t *testing.T) {
        // The stamped attribute only works because the document's boot
        // script yields to it and its palette carries the dark tokens —
        // pin both halves of the mechanism.
        page := getPage(t, writeThemeConfig(t, "web:\n  dark_mode: true\n"))
        assert.Contains(t, page, `localStorage.getItem('a1-theme')`, "same-origin stored-choice fallback")
        assert.Contains(t, page, "prefers-color-scheme", "OS-preference fallback")
        assert.Contains(t, page, "el.dataset.theme === 'light' || el.dataset.theme === 'dark'", "the server stamp wins pre-paint")
        assert.Contains(t, page, "--bg: #1B1814", "the warm charcoal ground")
        assert.Contains(t, page, `[data-theme="dark"] .brand-mark`, "the dodecahedron re-tints")
}

func TestEditorSaveCarriesWebDarkMode(t *testing.T) {
        // The regression this pins: the editor's web form knows only
        // autoNameSessions. A save of that toggle arrives WITHOUT darkMode,
        // and without the carry it would wipe the theme choice the chat
        // UI's button wrote to the file.
        path := writeThemeConfig(t, "web:\n  dark_mode: true\n  auto_name_sessions: true\n")
        h := &Handler{ConfigPath: path}
        srv := httptest.NewServer(h)
        defer srv.Close()

        body := `{"models":[{"name":"m","apiKey":"k","default":true}],"web":{"autoNameSessions":false}}`
        req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/config", bytes.NewReader([]byte(body)))
        require.NoError(t, err)
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Origin", srv.URL)
        resp, err := http.DefaultClient.Do(req)
        require.NoError(t, err)
        defer resp.Body.Close()
        require.Equal(t, http.StatusOK, resp.StatusCode)

        data, err := os.ReadFile(path)
        require.NoError(t, err)
        text := string(data)
        assert.Contains(t, text, "dark_mode: true", "the theme choice survives an editor save")
        assert.Contains(t, text, "auto_name_sessions: false", "the toggled value lands")

        // And the GET exposes it, so the editor round-trips the field.
        get, err := http.Get(srv.URL + "/api/config")
        require.NoError(t, err)
        defer get.Body.Close()
        var doc ConfigDoc
        require.NoError(t, json.NewDecoder(get.Body).Decode(&doc))
        require.NotNil(t, doc.Web)
        require.NotNil(t, doc.Web.DarkMode)
        assert.True(t, *doc.Web.DarkMode)
        require.NotNil(t, doc.Web.AutoNameSessions)
        assert.False(t, *doc.Web.AutoNameSessions)
}

func TestEditorSaveWholeWebBlockCarried(t *testing.T) {
        // A payload with NO web block at all (the form untouched) carries
        // the file's whole web section, theme included.
        path := writeThemeConfig(t, "web:\n  dark_mode: true\n")
        h := &Handler{ConfigPath: path}
        srv := httptest.NewServer(h)
        defer srv.Close()

        body := `{"models":[{"name":"m","apiKey":"k","default":true}]}`
        req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/config", strings.NewReader(body))
        require.NoError(t, err)
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Origin", srv.URL)
        resp, err := http.DefaultClient.Do(req)
        require.NoError(t, err)
        defer resp.Body.Close()
        require.Equal(t, http.StatusOK, resp.StatusCode)

        data, err := os.ReadFile(path)
        require.NoError(t, err)
        assert.Contains(t, string(data), "dark_mode: true")
}
