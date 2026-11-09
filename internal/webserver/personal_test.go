package webserver

import (
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/project"
)

// newPersonalFixtureProject boots a fixture project whose config carries a
// personal: section, so the status endpoint and the gate have a real
// LoadConfig behind them.
func newPersonalFixtureProject(t *testing.T, personalYAML string) *Server {
        t.Helper()
        home := t.TempDir()
        t.Setenv("HOME", home)
        t.Setenv("USERPROFILE", home)
        cfgYAML := "models:\n  - name: test-model\n    api_key: test-key\n    default: true\n" + personalYAML
        require.NoError(t, os.MkdirAll(filepath.Join(home, ".a1"), 0o755))
        require.NoError(t, os.WriteFile(filepath.Join(home, ".a1", "config.yaml"), []byte(cfgYAML), 0o644))
        proj, err := project.Discover(t.TempDir())
        require.NoError(t, err)
        require.NoError(t, proj.LoadConfig())
        srv, err := newServer(proj)
        require.NoError(t, err)
        return srv
}

// ---------- positive ----------

func TestPersonalStatusReportsConfig_Positive(t *testing.T) {
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: true\n  calendar_dir: ~/cal\n  mail_dir: /var/mail/me\n")
        rec := httptest.NewRecorder()
        srv.handlePersonalStatus(rec, httptest.NewRequest(http.MethodGet, "/api/personal/status", nil))
        require.Equal(t, http.StatusOK, rec.Code)
        var body struct {
                Enabled     bool   `json:"enabled"`
                CalendarDir string `json:"calendarDir"`
                MailDir     string `json:"mailDir"`
        }
        require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
        assert.True(t, body.Enabled)
        home, _ := os.UserHomeDir()
        assert.Equal(t, filepath.Join(home, "cal"), body.CalendarDir, "~ expanded by the runtime parse")
        assert.Equal(t, "/var/mail/me", body.MailDir)
}

func TestPersonalGatePassesWhenEnabled_Positive(t *testing.T) {
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: true\n")
        ran := false
        inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
                ran = true
                w.WriteHeader(http.StatusOK)
        })
        rec := httptest.NewRecorder()
        srv.personalGate(inner)(rec, httptest.NewRequest(http.MethodGet, "/api/personal/calendar", nil))
        require.Equal(t, http.StatusOK, rec.Code)
        assert.True(t, ran, "the wrapped handler runs")
}

// ---------- negative ----------

func TestPersonalStatusLockedWhenAbsent_Negative(t *testing.T) {
        srv := newPersonalFixtureProject(t, "")
        rec := httptest.NewRecorder()
        srv.handlePersonalStatus(rec, httptest.NewRequest(http.MethodGet, "/api/personal/status", nil))
        require.Equal(t, http.StatusOK, rec.Code, "status itself answers — it is the one ungated endpoint")
        var body struct {
                Enabled bool `json:"enabled"`
        }
        require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
        assert.False(t, body.Enabled)
}

func TestPersonalGateBlocksWhenDisabled_Negative(t *testing.T) {
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: false\n")
        inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
                w.WriteHeader(http.StatusOK)
                _, _ = w.Write([]byte("leaked"))
        })
        rec := httptest.NewRecorder()
        srv.personalGate(inner)(rec, httptest.NewRequest(http.MethodGet, "/api/personal/calendar", nil))
        require.Equal(t, http.StatusForbidden, rec.Code)
        assert.NotContains(t, rec.Body.String(), "leaked", "the wrapped handler never runs")
}

// ---------- differential ----------

// TestPersonalGateMatchesResolver_Differential: over every spelling of the
// section, the gate's pass/fail must equal EnabledResolved exactly — the
// gate is a thin wrapper over the resolver and this pins that it never
// grows a second opinion.
func TestPersonalGateMatchesResolver_Differential(t *testing.T) {
        for _, tc := range []struct {
                name string
                yaml string
        }{
                {"absent section", ""},
                {"empty section", "personal:\n"},
                {"enabled false", "personal:\n  enabled: false\n"},
                {"enabled true", "personal:\n  enabled: true\n"},
                {"dirs without enabled", "personal:\n  calendar_dir: ~/cal\n"},
        } {
                t.Run(tc.name, func(t *testing.T) {
                        srv := newPersonalFixtureProject(t, tc.yaml)
                        want := srv.personalConfig().EnabledResolved()
                        inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
                        rec := httptest.NewRecorder()
                        srv.personalGate(inner)(rec, httptest.NewRequest(http.MethodGet, "/api/personal/x", nil))
                        if want {
                                assert.Equal(t, http.StatusOK, rec.Code)
                        } else {
                                assert.Equal(t, http.StatusForbidden, rec.Code)
                        }
                })
        }
}
