package configserver

import (
        "io"
        "net/http"
        "net/http/httptest"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// TestPageServedNoStore pins the stale-cache fix: the editor page must be
// served with Cache-Control: no-store. The TUI launches this page on a fresh
// random port per run, and a browser that cached a previous build's HTML
// kept showing the old design — "the config page activated from the TUI
// doesn't get the updated design language".
func TestPageServedNoStore(t *testing.T) {
        h := &Handler{ConfigPath: t.TempDir() + "/config.yaml"}
        srv := httptest.NewServer(h)
        defer srv.Close()

        resp, err := http.Get(srv.URL + "/")
        require.NoError(t, err)
        defer resp.Body.Close()

        require.Equal(t, http.StatusOK, resp.StatusCode)
        assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
        assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
}

// TestPageDesignLanguage pins the design refactor at the structural level:
// the kami tokens, the serif display type, the inlined Dodec brand mark, and
// the frosted save footer must all be present, and none of the old
// dark-theme remnants (dark hover slabs, old blues/greens) may survive.
func TestPageDesignLanguage(t *testing.T) {
        h := &Handler{ConfigPath: t.TempDir() + "/config.yaml"}
        srv := httptest.NewServer(h)
        defer srv.Close()

        resp, err := http.Get(srv.URL + "/")
        require.NoError(t, err)
        defer resp.Body.Close()

        pageBytes, err := io.ReadAll(resp.Body)
        require.NoError(t, err)
        page := string(pageBytes)

        for _, marker := range []string{
                "--accent: #537D96",  // seal token
                "--red: #8B2C1F",     // cinnabar token
                "color-scheme: light", // the :root declaration stays light-first
                `[data-theme="dark"]`, // the dark palette rides its own scoped block
                "EB Garamond",        // serif display type (chat hero voice)
                `class="brand-mark"`, // inlined Dodec wireframe
                "backdrop-filter",    // frosted save footer
                `id="save"`,          // save machinery intact
                `id="models"`,        // model list intact
                "data-i18n",          // i18n wiring intact
                "llama.cpp / ollama", // context-window auto-detect help
        } {
                assert.Contains(t, page, marker)
        }

        // The old page hardcoded dark values at the TOP level — a broken
        // half-dark design. The dark theme is now legitimate but strictly
        // scoped to [data-theme="dark"]; the remnants below are the ones
        // that must never come back at all.
        for _, remnant := range []string{
                "#26262b",            // dark hover slab
                "#1e1e28",            // dark toggle slab
                "rgba(138, 180, 248", // old dark-theme blue
                "#3a5a4a",            // old dark-theme green border
                "#1a1215",            // old dark danger hover
                "#e6f2ea",            // light-on-dark save text
                "innerHTML = el(",    // el() returns an element; innerHTML stringifies it
        } {
                assert.NotContains(t, page, remnant, "dark-theme remnant must be gone")
        }
}
