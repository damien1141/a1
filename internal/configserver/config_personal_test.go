package configserver

import (
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "os"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// ---------- positive ----------

// TestConfigPersonalRoundTrip pins the editor surface for the personal:
// section: a save writes the block to config.yaml and a GET hands it back
// in the document shape the form paints from.
func TestConfigPersonalRoundTrip(t *testing.T) {
        path := writeTestConfig(t, `models:
  - name: gpt-4o
    api_key: sk-test
    default: true
`)
        h := &Handler{ConfigPath: path}

        rec := postConfig(t, h, map[string]any{
                "models": []map[string]any{
                        {"name": "gpt-4o", "apiKey": "sk-test", "default": true},
                },
                "personal": map[string]any{
                        "enabled":     true,
                        "calendarDir": "~/cal",
                        "mailDir":     "/var/mail/me",
                },
        })
        require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

        data, err := os.ReadFile(path)
        require.NoError(t, err)
        text := string(data)
        assert.Contains(t, text, "enabled: true")
        assert.Contains(t, text, "calendar_dir: ~/cal")
        assert.Contains(t, text, "mail_dir: /var/mail/me")

        get := httptest.NewRequest(http.MethodGet, "/api/config", nil)
        get.Host = "127.0.0.1:8080"
        getRec := httptest.NewRecorder()
        h.ServeHTTP(getRec, get)
        require.Equal(t, http.StatusOK, getRec.Code)
        var doc struct {
                Personal *struct {
                        Enabled     *bool  `json:"enabled"`
                        CalendarDir string `json:"calendarDir"`
                        MailDir     string `json:"mailDir"`
                } `json:"personal"`
        }
        require.NoError(t, json.NewDecoder(getRec.Body).Decode(&doc))
        require.NotNil(t, doc.Personal)
        require.NotNil(t, doc.Personal.Enabled)
        assert.True(t, *doc.Personal.Enabled)
        assert.Equal(t, "~/cal", doc.Personal.CalendarDir)
        assert.Equal(t, "/var/mail/me", doc.Personal.MailDir)
}

// TestConfigModelPersonalFlagsRoundTrip: the per-model read/write
// checkboxes survive a save → load cycle — the guardrail the user asked
// for lives and breathes through these two keys.
func TestConfigModelPersonalFlagsRoundTrip(t *testing.T) {
        path := writeTestConfig(t, `models:
  - name: gpt-4o
    api_key: sk-test
    default: true
`)
        h := &Handler{ConfigPath: path}

        rec := postConfig(t, h, map[string]any{
                "models": []map[string]any{
                        {"name": "gpt-4o", "apiKey": "sk-test", "default": true, "personalRead": true, "personalWrite": true},
                },
        })
        require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

        data, err := os.ReadFile(path)
        require.NoError(t, err)
        assert.Contains(t, string(data), "personal_read: true")
        assert.Contains(t, string(data), "personal_write: true")

        get := httptest.NewRequest(http.MethodGet, "/api/config", nil)
        get.Host = "127.0.0.1:8080"
        getRec := httptest.NewRecorder()
        h.ServeHTTP(getRec, get)
        require.Equal(t, http.StatusOK, getRec.Code)
        var doc struct {
                Models []struct {
                        Name          string `json:"name"`
                        PersonalRead  *bool  `json:"personalRead"`
                        PersonalWrite *bool  `json:"personalWrite"`
                } `json:"models"`
        }
        require.NoError(t, json.NewDecoder(getRec.Body).Decode(&doc))
        require.Len(t, doc.Models, 1)
        require.NotNil(t, doc.Models[0].PersonalRead)
        require.NotNil(t, doc.Models[0].PersonalWrite)
        assert.True(t, *doc.Models[0].PersonalRead)
        assert.True(t, *doc.Models[0].PersonalWrite)
}

// ---------- negative ----------

// TestConfigPersonalEnabledTypeGarbage_Negative: a string where a bool
// belongs is a decode error, not a silent default.
func TestConfigPersonalEnabledTypeGarbage_Negative(t *testing.T) {
        path := writeTestConfig(t, `models:
  - name: gpt-4o
    api_key: sk-test
    default: true
`)
        h := &Handler{ConfigPath: path}

        rec := postConfig(t, h, map[string]any{
                "models": []map[string]any{
                        {"name": "gpt-4o", "apiKey": "sk-test", "default": true},
                },
                "personal": map[string]any{"enabled": "banana"},
        })
        require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// ---------- differential ----------

// TestConfigPersonalUntouchedSaveMatchesFileGolden_Differential: a form
// that round-trips the document it loaded (personal comes from orig when
// the panel was never touched) preserves the section's keys against a
// hand-written golden block — the same protection jev got.
func TestConfigPersonalUntouchedSaveMatchesFileGolden_Differential(t *testing.T) {
        path := writeTestConfig(t, `models:
  - name: gpt-4o
    api_key: sk-test
    default: true
personal:
  enabled: true
  calendar_dir: ~/cal
  mail_dir: /var/mail/me
`)
        h := &Handler{ConfigPath: path}

        rec := postConfig(t, h, map[string]any{
                "models": []map[string]any{
                        {"name": "gpt-4o", "apiKey": "sk-test", "default": true},
                },
                "personal": map[string]any{
                        "enabled":     true,
                        "calendarDir": "~/cal",
                        "mailDir":     "/var/mail/me",
                },
        })
        require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

        data, err := os.ReadFile(path)
        require.NoError(t, err)
        golden := "personal:\n    enabled: true\n    calendar_dir: ~/cal\n    mail_dir: /var/mail/me\n"
        assert.Contains(t, string(data), golden, "the untouched section survives verbatim (yaml.Marshal indents list items' siblings with 4 spaces)")
}
