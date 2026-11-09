package agent

import (
        "context"
        "encoding/json"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/project"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/vault"
)

// The agent-side personal-calendar mount (dashboard chunk 2). The engine
// mounts personal_calendar only for an enabled section + a model that
// opted into reads; and because the ruling is "enforced at the tool call,
// not just at mount", a mounted tool refuses the moment the live state
// says no — the mount is a convenience, the call-time check is the wall.

func personalModel(read, write bool) llm.ModelConfig {
        return llm.ModelConfig{
                Name: "fake", BaseURL: "http://127.0.0.1:9", APIKey: "x",
                PersonalRead: read, PersonalWrite: write,
        }
}

func newPersonalEngine(t *testing.T, model llm.ModelConfig, live *project.PersonalConfig) *Engine {
        t.Helper()
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(model, sess, WithPersonalConfig(func() project.PersonalConfig {
                return *live
        }))
        require.NoError(t, err)
        return eng
}

func TestPersonalCalendarMountGating(t *testing.T) {
        on := true

        for _, tc := range []struct {
                name  string
                live  project.PersonalConfig
                model llm.ModelConfig
                want  bool
        }{
                {
                        name:  "both consents",
                        live:  project.PersonalConfig{Enabled: &on, CalendarDir: "/tmp/cal"},
                        model: personalModel(true, false),
                        want:  true,
                },
                {
                        name:  "master switch off",
                        live:  project.PersonalConfig{CalendarDir: "/tmp/cal"},
                        model: personalModel(true, true),
                        want:  false,
                },
                {
                        name:  "model without personal_read",
                        live:  project.PersonalConfig{Enabled: &on, CalendarDir: "/tmp/cal"},
                        model: personalModel(false, true),
                        want:  false,
                },
                {
                        name:  "no provider wired",
                        live:  project.PersonalConfig{Enabled: &on, CalendarDir: "/tmp/cal"},
                        model: personalModel(true, true),
                        want:  false,
                },
        } {
                t.Run(tc.name, func(t *testing.T) {
                        if tc.name == "no provider wired" {
                                sess, err := NewSession(WithCwd(t.TempDir()))
                                require.NoError(t, err)
                                eng, err := NewEngine(tc.model, sess)
                                require.NoError(t, err)
                                assert.Equal(t, tc.want, eng.HasTool("personal_calendar"))
                                return
                        }
                        eng := newPersonalEngine(t, tc.model, &tc.live)
                        assert.Equal(t, tc.want, eng.HasTool("personal_calendar"))
                })
        }
}

func TestPersonalCalendarCallTimeRefusal(t *testing.T) {
        // The mis-mount scenario: consent held at build time, then the live
        // config flipped OFF before the model called the tool.
        on := true
        live := &project.PersonalConfig{Enabled: &on, CalendarDir: t.TempDir()}
        eng := newPersonalEngine(t, personalModel(true, true), live)
        require.True(t, eng.HasTool("personal_calendar"))

        *live.Enabled = false // the config save that locks personal data

        // The tool itself must refuse — mount staleness cannot leak reads or
        // writes. Reaching through the executor's registry, the exact table
        // the loop dispatches tool calls through.
        _, err := runPersonalCalendar(t, eng, `{"action":"agenda"}`)
        require.Error(t, err, "a locked master switch refuses the call")
        assert.Contains(t, err.Error(), "personal_read")

        _, err = runPersonalCalendar(t, eng, `{"action":"create","summary":"X","start":"2030-01-01"}`)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "personal_write")

        // Re-enable: the same engine (no rebuild) works again — the check is
        // live state, not a build-time snapshot.
        *live.Enabled = true
        _, err = runPersonalCalendar(t, eng, `{"action":"agenda"}`)
        require.NoError(t, err)
}

func TestRefreshPersonalToolsRebinds(t *testing.T) {
        // Built while locked: no mount. The config flips on; a refresh (what
        // the web surface fires after a config save) mounts the tool on the
        // LIVE engine without restarting its session.
        live := &project.PersonalConfig{}
        eng := newPersonalEngine(t, personalModel(true, false), live)
        require.False(t, eng.HasTool("personal_calendar"))

        on := true
        live.Enabled = &on
        live.CalendarDir = "/tmp/cal"
        assert.False(t, eng.HasTool("personal_calendar"), "stale until the refresh")
        eng.RefreshPersonalTools()
        assert.True(t, eng.HasTool("personal_calendar"))

        // And the disable direction: refresh unmounts.
        *live.Enabled = false
        eng.RefreshPersonalTools()
        assert.False(t, eng.HasTool("personal_calendar"))
        // The kanban and mail facades ride the same rebind — the refresh
        // rebuilds the whole table, so chunk 3's and chunk 4's tools mount
        // and unmount with the same flip, not just the calendar that
        // motivated the hook.
        assert.False(t, eng.HasTool("personal_kanban"))
        assert.False(t, eng.HasTool("personal_mail"))
}

func TestPersonalKanbanMountGating(t *testing.T) {
        on := true

        for _, tc := range []struct {
                name  string
                live  project.PersonalConfig
                model llm.ModelConfig
                want  bool
        }{
                {
                        name:  "both consents",
                        live:  project.PersonalConfig{Enabled: &on},
                        model: personalModel(true, false),
                        want:  true,
                },
                {
                        name:  "master switch off",
                        live:  project.PersonalConfig{},
                        model: personalModel(true, true),
                        want:  false,
                },
                {
                        name:  "model without personal_read",
                        live:  project.PersonalConfig{Enabled: &on},
                        model: personalModel(false, true),
                        want:  false,
                },
        } {
                t.Run(tc.name, func(t *testing.T) {
                        eng := newPersonalEngine(t, tc.model, &tc.live)
                        assert.Equal(t, tc.want, eng.HasTool("personal_kanban"))
                })
        }

        // No provider wired: locked, like every personal surface.
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(personalModel(true, true), sess)
        require.NoError(t, err)
        assert.False(t, eng.HasTool("personal_kanban"))
}

func TestPersonalKanbanCallTimeRefusal(t *testing.T) {
        // The mis-mount scenario, the calendar's twin: consent held at
        // build time, then the live config flipped OFF before the model
        // called the tool. The board lives in the vault — no directory to
        // point at — so the vault handle is ALSO read at call time: a vault
        // installed after the engine was built is still seen, and a missing
        // vault refuses honestly instead of reporting an empty board.
        on := true
        live := &project.PersonalConfig{Enabled: &on}
        eng := newPersonalEngine(t, personalModel(true, true), live)
        require.True(t, eng.HasTool("personal_kanban"))

        // No vault yet: mounted, but the read cannot invent a board.
        _, err := runPersonalKanban(t, eng, `{"action":"list"}`)
        require.Error(t, err, "no vault means no board, not an empty one")
        assert.Contains(t, err.Error(), "no vault")

        v, err := vault.Open(t.TempDir())
        require.NoError(t, err)
        session.SetDefaultVault(v)
        t.Cleanup(func() {
                session.SetDefaultVault(nil)
                _ = v.Close()
        })

        *live.Enabled = false // the config save that locks personal data

        _, err = runPersonalKanban(t, eng, `{"action":"list"}`)
        require.Error(t, err, "a locked master switch refuses the read")
        assert.Contains(t, err.Error(), "personal_read")

        _, err = runPersonalKanban(t, eng, `{"action":"add","title":"X"}`)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "personal_write")

        // Re-enable: the same engine (no rebuild) reads the live vault —
        // the check is live state, not a build-time snapshot.
        *live.Enabled = true
        _, err = runPersonalKanban(t, eng, `{"action":"list"}`)
        require.NoError(t, err)
}

// runPersonalKanban invokes the engine's mounted personal_kanban tool with
// raw JSON args, failing the test when it is not mounted.
func runPersonalKanban(t *testing.T, eng *Engine, args string) (json.RawMessage, error) {
        t.Helper()
        require.NotNil(t, eng.executor, "engine has an executor")
        tool, ok := eng.executor.registry["personal_kanban"]
        require.True(t, ok, "personal_kanban is mounted")
        res, err := tool.Run(context.Background(), json.RawMessage(args))
        if err != nil {
                return nil, err
        }
        return json.RawMessage(res.Content), nil
}

// runPersonalCalendar invokes the engine's mounted personal_calendar tool
// with raw JSON args, failing the test when it is not mounted.
func runPersonalCalendar(t *testing.T, eng *Engine, args string) (json.RawMessage, error) {
        t.Helper()
        require.NotNil(t, eng.executor, "engine has an executor")
        tool, ok := eng.executor.registry["personal_calendar"]
        require.True(t, ok, "personal_calendar is mounted")
        res, err := tool.Run(context.Background(), json.RawMessage(args))
        if err != nil {
                return nil, err
        }
        return json.RawMessage(res.Content), nil
}

// TestPersonalMailMountGating is the calendar's table, the mail twin:
// the facade rides the same master-switch + personal_read gate as its
// siblings. personal_write is deliberately irrelevant — the surface has
// no write to grant.
func TestPersonalMailMountGating(t *testing.T) {
        on := true
        mailRoot := t.TempDir()

        for _, tc := range []struct {
                name  string
                live  project.PersonalConfig
                model llm.ModelConfig
                want  bool
        }{
                {
                        name:  "both consents",
                        live:  project.PersonalConfig{Enabled: &on, MailDir: mailRoot},
                        model: personalModel(true, false),
                        want:  true,
                },
                {
                        name:  "master switch off",
                        live:  project.PersonalConfig{MailDir: mailRoot},
                        model: personalModel(true, true),
                        want:  false,
                },
                {
                        name:  "model without personal_read",
                        live:  project.PersonalConfig{Enabled: &on, MailDir: mailRoot},
                        model: personalModel(false, true),
                        want:  false,
                },
                {
                        name:  "write opt-in alone grants nothing",
                        live:  project.PersonalConfig{Enabled: &on, MailDir: mailRoot},
                        model: personalModel(false, true),
                        want:  false,
                },
        } {
                t.Run(tc.name, func(t *testing.T) {
                        eng := newPersonalEngine(t, tc.model, &tc.live)
                        assert.Equal(t, tc.want, eng.HasTool("personal_mail"))
                })
        }

        // No provider wired: locked, like every personal surface.
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(personalModel(true, true), sess)
        require.NoError(t, err)
        assert.False(t, eng.HasTool("personal_mail"))
}

// TestPersonalMailCallTimeRefusal is the mis-mount scenario, the mail
// twin: consent held at build time, then the live config flipped OFF
// before the model called the tool. And the configured-directory flips
// the same way — a root that arrives (or leaves) after the engine was
// built is read live at call time.
func TestPersonalMailCallTimeRefusal(t *testing.T) {
        on := true
        live := &project.PersonalConfig{Enabled: &on, MailDir: t.TempDir()}
        eng := newPersonalEngine(t, personalModel(true, true), live)
        require.True(t, eng.HasTool("personal_mail"))

        // The root vanishes at runtime (config save pointing elsewhere,
        // directory moved): the call refuses honestly, it does not invent
        // an empty Maildir.
        savedDir := live.MailDir
        live.MailDir = ""
        _, err := runPersonalMail(t, eng, `{"action":"mailboxes"}`)
        require.Error(t, err, "no configured root means no mail, not an empty one")
        assert.Contains(t, err.Error(), "mail_dir")

        live.MailDir = savedDir
        *live.Enabled = false // the config save that locks personal data

        _, err = runPersonalMail(t, eng, `{"action":"mailboxes"}`)
        require.Error(t, err, "a locked master switch refuses the read")
        assert.Contains(t, err.Error(), "personal_read")
        _, err = runPersonalMail(t, eng, `{"action":"messages","box":"inbox"}`)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "personal_read")

        // Re-enable: the same engine (no rebuild) works again — the check
        // is live state, not a build-time snapshot.
        *live.Enabled = true
        _, err = runPersonalMail(t, eng, `{"action":"mailboxes"}`)
        require.NoError(t, err)
}

// runPersonalMail invokes the engine's mounted personal_mail tool with
// raw JSON args, failing the test when it is not mounted.
func runPersonalMail(t *testing.T, eng *Engine, args string) (json.RawMessage, error) {
        t.Helper()
        require.NotNil(t, eng.executor, "engine has an executor")
        tool, ok := eng.executor.registry["personal_mail"]
        require.True(t, ok, "personal_mail is mounted")
        res, err := tool.Run(context.Background(), json.RawMessage(args))
        if err != nil {
                return nil, err
        }
        return json.RawMessage(res.Content), nil
}

