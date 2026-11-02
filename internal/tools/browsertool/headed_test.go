package browsertool

import (
        "context"
        "encoding/json"
        "os"
        "path/filepath"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/bw"
        "github.com/damien1141/a1/internal/project"
)

// headed_test.go pins the "launch a LIVE web browser" fixes:
//
//   - browserHeadless resolves env → config → display probe, so a user can
//     force a visible window where the probe fails (tmux/SSH) or force
//     headless on a desktop (CI).
//   - ensureBrowser recovers a headless daemon: when this process froze
//     the shared profile's daemon headless (a background lane), the
//     interactive open stops every daemon first so the headed spawn is not
//     silently absorbed by the warm headless one.
//   - /browser open says which window mode landed instead of silently
//     downgrading.

// installConfigProject writes a config.yaml with the given browser section
// under a temp HOME, re-discovers the project singleton under that HOME,
// and loads the config onto the LIVE instance. SetDefaultProject alone is
// not enough here: it resets the init once, so the next GetDefaultProject
// re-discovers a config-less project and silently discards the installed
// one — every Config()-reading consumer would see nil.
func installConfigProject(t *testing.T, browserYAML string) {
        t.Helper()
        home := t.TempDir()
        t.Setenv("HOME", home)
        t.Setenv("USERPROFILE", home)
        require.NoError(t, os.MkdirAll(filepath.Join(home, ".a1"), 0o755))
        cfg := filepath.Join(home, ".a1", "config.yaml")
        require.NoError(t, os.WriteFile(cfg, []byte("models:\n  - name: m\n    api: openai\n    api_key: k\n"+browserYAML), 0o644))

        project.SetDefaultProject(nil) // force the singleton to re-discover
        live := project.GetDefaultProject()
        require.NotNil(t, live)
        require.NoError(t, live.LoadConfig())
        t.Cleanup(func() { project.SetDefaultProject(nil) })
}

// TestBrowserHeadless_ResolutionOrder pins env → config → display-probe.
func TestBrowserHeadless_ResolutionOrder(t *testing.T) {
        // Display probe: no env, no config.
        t.Setenv("DISPLAY", "")
        t.Setenv("WAYLAND_DISPLAY", "")
        t.Setenv("A1_BROWSER_HEADLESS", "")
        installConfigProject(t, "")
        assert.True(t, browserHeadless(), "no display → headless")

        // Display present → headed.
        t.Setenv("DISPLAY", ":99")
        assert.False(t, browserHeadless(), "a reachable display → headed")

        // Config forces headless even with a display.
        installConfigProject(t, "browser:\n  headless: true\n")
        assert.True(t, browserHeadless(), "browser.headless: true wins over the display probe")

        // Config forces a visible window where the probe fails (tmux/SSH).
        t.Setenv("DISPLAY", "")
        installConfigProject(t, "browser:\n  headless: false\n")
        assert.False(t, browserHeadless(), "browser.headless: false wins over a failed probe")

        // The env var beats the config for per-invocation control.
        installConfigProject(t, "browser:\n  headless: false\n")
        t.Setenv("A1_BROWSER_HEADLESS", "1")
        assert.True(t, browserHeadless(), "A1_BROWSER_HEADLESS=1 beats browser.headless: false")
}

// headlessRec is the fake-daemon recorder for the ensureBrowser tests: it
// captures every CLI invocation (close and run) in order.
type headlessRec struct {
        calls [][]string
}

func (r *headlessRec) install(t *testing.T) {
        t.Helper()
        origRun, origResolve := bw.RunCommand, bw.Resolve
        bw.RunCommand = func(_ context.Context, _ string, args []string, _ []string) ([]byte, error) {
                r.calls = append(r.calls, args)
                return []byte(`{"ok":true,"result":true}`), nil
        }
        bw.Resolve = func() (string, error) { return "/usr/bin/fake-betterwright", nil }
        t.Cleanup(func() {
                bw.RunCommand, bw.Resolve = origRun, origResolve
                browserMu.Lock()
                bwOpened = false
                browserMu.Unlock()
                bw.ResetSpawnTracking()
        })
}

// openLatch resets the interactive latch so ensureBrowser runs its warm-up.
func openLatch(t *testing.T) {
        t.Helper()
        browserMu.Lock()
        bwOpened = false
        browserMu.Unlock()
}

// TestEnsureBrowser_RecoversHeadlessDaemon pins the recovery: a background
// headless spawn on the shared profile (e.g. a PDF render) must not poison
// the interactive open — ensureBrowser stops every daemon first, then
// spawns headed.
func TestEnsureBrowser_RecoversHeadlessDaemon(t *testing.T) {
        rec := &headlessRec{}
        rec.install(t)
        bw.ResetSpawnTracking()
        openLatch(t)

        // A display exists, so the interactive path wants a HEADED browser.
        t.Setenv("DISPLAY", ":99")
        t.Setenv("A1_BROWSER_HEADLESS", "")
        installConfigProject(t, "")

        // The background lane cold-spawns the shared profile headless (the
        // poisoning scenario).
        _, err := bw.RunLane(t.Context(), bw.BackgroundSession, "", false, "return true")
        require.NoError(t, err)

        require.NoError(t, ensureBrowser(t.Context()))

        // The recovery close must appear, and the interactive warm-up must
        // come AFTER it (the poisoning run on the background lane is also
        // recorded — it is the first call — so ordering is asserted relative
        // to the interactive lane, not to the absolute first call).
        closeIdx := -1
        warmIdx := -1
        for i, args := range rec.calls {
                if len(args) == 0 {
                        continue
                }
                if args[0] == "close" {
                        require.Equal(t, []string{"close", "--all"}, args,
                                "the recovery close stops every daemon")
                        if closeIdx < 0 {
                                closeIdx = i
                        }
                }
                if args[0] == "run" {
                        if j := indexOfArg(args, "--session"); j >= 0 && args[j+1] == bw.Session {
                                require.Contains(t, args, "--headed",
                                        "the interactive warm-up must be headed — that is the whole point")
                                if warmIdx < 0 {
                                        warmIdx = i
                                }
                        }
                }
        }
        require.GreaterOrEqual(t, closeIdx, 0,
                "a headless daemon on the shared profile must be stopped before the headed spawn")
        require.GreaterOrEqual(t, warmIdx, 0, "the warm-up must run on the interactive lane")
        assert.Greater(t, warmIdx, closeIdx,
                "the headed warm-up must come after the recovery close")
}

// TestEnsureBrowser_NoRecoveryWhenDaemonHeaded pins the other side: when
// this process last spawned the profile HEADED (or never spawned it), the
// warm-up goes straight to the run — no gratuitous close that would throw
// away a live browser's tabs.
func TestEnsureBrowser_NoRecoveryWhenDaemonHeaded(t *testing.T) {
        rec := &headlessRec{}
        rec.install(t)
        bw.ResetSpawnTracking()
        openLatch(t)

        t.Setenv("DISPLAY", ":99")
        t.Setenv("A1_BROWSER_HEADLESS", "")
        installConfigProject(t, "")

        require.NoError(t, ensureBrowser(t.Context()))

        for _, args := range rec.calls {
                require.NotEqual(t, "close", args[0],
                        "no headless spawn happened: nothing to recover, and a close would needlessly drop tabs")
        }
        require.NotEmpty(t, rec.calls, "the warm-up must still run")
}

// TestBrowserOpen_ReportsWindowMode pins the legibility half: the open
// result says which window mode landed, so a headless downgrade (or a
// visible launch) is never a silent "nothing happened".
func TestBrowserOpen_ReportsWindowMode(t *testing.T) {
        rec := &headlessRec{}
        rec.install(t)
        bw.ResetSpawnTracking()
        // The open action runs under the opened latch (runBrowser's gate).
        browserMu.Lock()
        bwOpened = true
        browserMu.Unlock()

        t.Setenv("A1_BROWSER_HEADLESS", "")
        installConfigProject(t, "")

        // Headed: a display exists.
        t.Setenv("DISPLAY", ":99")
        in, _ := json.Marshal(browserInput{Action: "open", URL: "https://example.com"})
        res, err := runBrowser(t.Context(), in)
        require.NoError(t, err)
        assert.Contains(t, res.Content, "visible window",
                "a headed open must say so")
        assert.NotContains(t, res.Content, "headless")

        // Headless: no display, no config override.
        t.Setenv("DISPLAY", "")
        res, err = runBrowser(t.Context(), in)
        require.NoError(t, err)
        assert.Contains(t, res.Content, "headless",
                "a headless downgrade must be visible in the result, not silent")
        assert.Contains(t, res.Content, "browser.headless: false",
                "the result must tell the user how to force a visible window")
}

func indexOfArg(xs []string, want string) int {
        for i, x := range xs {
                if x == want {
                        return i
                }
        }
        return -1
}
