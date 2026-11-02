package commands

import (
        "context"
        "errors"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/bw"
        "github.com/damien1141/a1/internal/components/toast"
        "github.com/damien1141/a1/internal/tui/controller"
)

// browser_test.go pins the /browser command's feedback contract: the
// command's outcome (success with the window mode, or the exact failure)
// must reach the user as a toast. DispatchSlash discards both the result
// and the error, so before this fix a missing BetterWright install or a
// successful headless spawn both read as "nothing happened" — the
// "doesn't launch a live web browser" report.

// fakeBWSwap swaps the daemon for a fixed envelope (or a transport error)
// and resets the binary-resolution cache, restoring everything on cleanup.
// The interactive latch (bwOpened) is deliberately left alone: it is
// package-private to browsertool, and a warm latch only skips the (fake)
// warm-up — the open action itself is what these tests assert on.
func fakeBWSwap(t *testing.T, out string, runErr error) {
        t.Helper()
        origRun, origResolve := bw.RunCommand, bw.Resolve
        bw.RunCommand = func(_ context.Context, _ string, _ []string, _ []string) ([]byte, error) {
                if runErr != nil {
                        return nil, runErr
                }
                return []byte(out), nil
        }
        bw.Resolve = func() (string, error) { return "/usr/bin/fake-betterwright", nil }
        bw.ResetForTest()
        t.Cleanup(func() {
                bw.RunCommand, bw.Resolve = origRun, origResolve
                bw.ResetForTest()
        })
}

// drainToasts reads every ToastMsg currently pending on the bus.
func drainToasts(t *testing.T, bus *controller.Bus) []controller.ToastMsg {
        t.Helper()
        var out []controller.ToastMsg
        for _, m := range bus.Drain() {
                if tm, ok := m.(controller.ToastMsg); ok {
                        out = append(out, tm)
                }
        }
        return out
}

func TestBrowserCommand_ToastsOutcome(t *testing.T) {
        fakeBWSwap(t, `{"ok":true,"result":{"url":"https://example.com","title":"Example"}}`, nil)

        r := NewCommandRegistry()
        (&BrowserCommands{}).Register(r)

        bus := controller.NewBus(nil)
        ctx := NewContext(bus, nil)

        // Test env has no DISPLAY by default: the open lands headless, and the
        // toast must SAY so (the legible-downgrade contract).
        require.True(t, r.DispatchSlash("/browser open https://example.com", ctx))
        toasts := drainToasts(t, bus)
        require.Len(t, toasts, 1, "exactly one toast per command run")
        assert.Equal(t, toast.ToastSuccess, toasts[0].Kind)
        assert.Contains(t, toasts[0].Message, "Browser:",
                "the toast must be identifiable as the browser command's outcome")
        assert.Contains(t, toasts[0].Message, "headless",
                "a headless landing must be named, not silent")
}

func TestBrowserCommand_ToastsFailure(t *testing.T) {
        fakeBWSwap(t, "", errors.New("betterwright is not installed"))

        r := NewCommandRegistry()
        (&BrowserCommands{}).Register(r)

        bus := controller.NewBus(nil)
        ctx := NewContext(bus, nil)

        require.True(t, r.DispatchSlash("/browser open https://example.com", ctx))
        toasts := drainToasts(t, bus)
        require.Len(t, toasts, 1)
        assert.Equal(t, toast.ToastError, toasts[0].Kind,
                "a failed open must surface as an error toast")
        assert.Contains(t, toasts[0].Message, "not installed",
                "the toast must carry the actionable failure, not swallow it")
}
