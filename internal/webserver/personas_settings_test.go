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

        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/project"
)

// newSettingsTestServer builds a Server with a real project (config loaded
// from a temp HOME) and a persona store, so the persona-update handler's
// settings path and the model catalog endpoint can be exercised end to end
// without touching the developer's real ~/.a1.
func newSettingsTestServer(t *testing.T) (*Server, *persona.Store) {
        t.Helper()

        home := t.TempDir()
        t.Setenv("HOME", home)
        require.NoError(t, os.MkdirAll(filepath.Join(home, ".a1"), 0o755))

        cfgYAML := `models:
  - name: big-model
    api: openai
    api_key: test-key
    context_window: 128000
    default: true
  - name: small-model
    api: openai
    api_key: test-key
    context_window: 8000
`
        require.NoError(t, os.WriteFile(filepath.Join(home, ".a1", "config.yaml"), []byte(cfgYAML), 0o644))

        proj, err := project.Discover(t.TempDir())
        require.NoError(t, err)
        require.NoError(t, proj.LoadConfig())

        store := persona.NewStore(filepath.Join(t.TempDir(), "personas"))
        s := &Server{proj: proj, personaStore: store}
        s.initMaps()
        return s, store
}

// TestHandleUpdatePersona_AppliesSettings verifies the persona editor's
// settings ride the update endpoint: model, permission mode, and the
// memory toggle persist through SetSettings, and the response DTO carries
// them back so the UI re-renders from the response.
func TestHandleUpdatePersona_AppliesSettings(t *testing.T) {
        s, store := newSettingsTestServer(t)
        p, err := store.Create("worker", "work persona", "")
        require.NoError(t, err)

        body := `{"name":"worker","tagline":"work persona","model":"small-model","permissionMode":"readonly","memoryEnabled":true}`
        req := httptest.NewRequest(http.MethodPut, "/api/personas/"+p.ID, stringBody(body))
        req.SetPathValue("id", p.ID)
        rec := httptest.NewRecorder()

        s.handleUpdatePersona(rec, req)
        require.Equal(t, http.StatusOK, rec.Code, "update with settings must succeed: %s", rec.Body.String())

        var resp struct {
                Persona personaDTO `json:"persona"`
        }
        require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

        got, err := store.Get(p.ID)
        require.NoError(t, err)
        assert.Equal(t, "small-model", got.Model)
        assert.Equal(t, "readonly", got.PermissionMode)
        require.NotNil(t, got.MemoryEnabled)
        assert.True(t, *got.MemoryEnabled)

        assert.Equal(t, "small-model", resp.Persona.Model, "DTO must round-trip the model")
        assert.Equal(t, "readonly", resp.Persona.PermissionMode)
        require.NotNil(t, resp.Persona.MemoryEnabled)
        assert.True(t, *resp.Persona.MemoryEnabled)
}

// TestHandleUpdatePersona_ClearsModelWithEmptyString verifies the editor's
// "Default (global config)" dropdown entry: sending an explicit empty
// string clears the override (the store's SetSettings pointer contract),
// while a body WITHOUT the settings fields leaves them untouched.
func TestHandleUpdatePersona_ClearsModelWithEmptyString(t *testing.T) {
        s, store := newSettingsTestServer(t)
        p, err := store.Create("worker", "work persona", "")
        require.NoError(t, err)

        model := "small-model"
        _, err = store.SetSettings(p.ID, &model, nil, nil)
        require.NoError(t, err)

        // Profile-only update (no settings keys) must not clobber the model.
        profileOnly := `{"name":"worker","tagline":"new tagline"}`
        req := httptest.NewRequest(http.MethodPut, "/api/personas/"+p.ID, stringBody(profileOnly))
        req.SetPathValue("id", p.ID)
        rec := httptest.NewRecorder()
        s.handleUpdatePersona(rec, req)
        require.Equal(t, http.StatusOK, rec.Code)

        got, _ := store.Get(p.ID)
        assert.Equal(t, "small-model", got.Model, "a body without settings keys must keep stored settings")

        // Explicit empty string clears it.
        clearing := `{"name":"worker","tagline":"new tagline","model":"","permissionMode":""}`
        req = httptest.NewRequest(http.MethodPut, "/api/personas/"+p.ID, stringBody(clearing))
        req.SetPathValue("id", p.ID)
        rec = httptest.NewRecorder()
        s.handleUpdatePersona(rec, req)
        require.Equal(t, http.StatusOK, rec.Code)

        got, _ = store.Get(p.ID)
        assert.Empty(t, got.Model, "explicit empty string must clear the model override")
        assert.Empty(t, got.PermissionMode, "explicit empty string must clear the mode override")
}

// TestHandleUpdatePersona_RejectsUnknownMode verifies a typo'd permission
// mode is a 400 — not a stored value that silently no-ops on every switch.
func TestHandleUpdatePersona_RejectsUnknownMode(t *testing.T) {
        s, store := newSettingsTestServer(t)
        p, err := store.Create("worker", "work persona", "")
        require.NoError(t, err)

        body := `{"name":"worker","permissionMode":"sorta-readonly"}`
        req := httptest.NewRequest(http.MethodPut, "/api/personas/"+p.ID, stringBody(body))
        req.SetPathValue("id", p.ID)
        rec := httptest.NewRecorder()

        s.handleUpdatePersona(rec, req)
        assert.Equal(t, http.StatusBadRequest, rec.Code)

        got, _ := store.Get(p.ID)
        assert.Empty(t, got.PermissionMode, "an invalid mode must not be stored")
}

// TestHandleUpdatePersona_UnknownPersonaNotFound verifies the settings path
// surfaces the store's not-found as 404 rather than a 500.
func TestHandleUpdatePersona_UnknownPersonaNotFound(t *testing.T) {
        s, _ := newSettingsTestServer(t)

        body := `{"name":"ghost","model":"small-model"}`
        req := httptest.NewRequest(http.MethodPut, "/api/personas/ghost", stringBody(body))
        req.SetPathValue("id", "ghost")
        rec := httptest.NewRecorder()

        s.handleUpdatePersona(rec, req)
        assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestHandleListModels_ReturnsCatalog verifies GET /api/models serves the
// configured catalog with context windows — the persona editor's dropdown
// source and the footer's per-model denominators.
func TestHandleListModels_ReturnsCatalog(t *testing.T) {
        s, _ := newSettingsTestServer(t)

        req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
        rec := httptest.NewRecorder()
        s.handleListModels(rec, req)
        require.Equal(t, http.StatusOK, rec.Code)

        var resp struct {
                Models []modelInfo `json:"models"`
        }
        require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
        require.Len(t, resp.Models, 2)

        byName := map[string]modelInfo{}
        for _, m := range resp.Models {
                byName[m.Name] = m
        }
        assert.Equal(t, 128000, byName["big-model"].ContextWindow)
        assert.True(t, byName["big-model"].Default, "the default-flagged entry is reported")
        assert.Equal(t, 8000, byName["small-model"].ContextWindow)
        assert.False(t, byName["small-model"].Default)
}

// stringBody wraps a literal request body.
func stringBody(s string) *strings.Reader { return strings.NewReader(s) }
