package permission

import (
        "context"
        "os"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// withSandboxSupport overrides the host probe so containment logic is
// testable everywhere, including containers without Landlock.
func withSandboxSupport(t *testing.T, enabled bool) {
        t.Helper()
        orig := sandboxSupported
        sandboxSupported = func() bool { return enabled }
        t.Cleanup(func() { sandboxSupported = orig })
}

func TestSandboxedBashAutoAllowed(t *testing.T) {
        withSandboxSupport(t, true)
        g, err := NewGate(DefaultPolicy(), t.TempDir())
        require.NoError(t, err)
        require.True(t, g.Sandboxed())

        dec, reason := g.Check(context.Background(), Request{
                Action:  ActionBash,
                Command: "make all && mkdir -p out",
        })
        assert.Equal(t, Allow, dec, "containment substitutes for approval")
        assert.Contains(t, reason, "sandboxed")
}

func TestSandboxedBashDenyListStillFires(t *testing.T) {
        withSandboxSupport(t, true)
        g, err := NewGate(DefaultPolicy(), t.TempDir())
        require.NoError(t, err)

        dec, _ := g.Check(context.Background(), Request{
                Action:  ActionBash,
                Command: "sudo rm -rf /",
        })
        assert.Equal(t, Deny, dec, "the deny list fires before containment")
}

func TestSandboxOffKeepsAsking(t *testing.T) {
        withSandboxSupport(t, true)
        policy := DefaultPolicy()
        policy.Sandbox = "off"
        g, err := NewGate(policy, t.TempDir())
        require.NoError(t, err)
        assert.False(t, g.Sandboxed())

        dec, _ := g.Check(context.Background(), Request{
                Action:  ActionBash,
                Command: "make all",
        })
        assert.Equal(t, Ask, dec, "without containment the gate keeps asking")
}

func TestSandboxUnsupportedDegradesToAsking(t *testing.T) {
        withSandboxSupport(t, false)
        g, err := NewGate(DefaultPolicy(), t.TempDir())
        require.NoError(t, err)
        assert.False(t, g.Sandboxed(), "no backend, no containment")

        dec, _ := g.Check(context.Background(), Request{
                Action:  ActionBash,
                Command: "make all",
        })
        assert.Equal(t, Ask, dec)
}

func TestSandboxReadonlyStillAsks(t *testing.T) {
        withSandboxSupport(t, true)
        policy := DefaultPolicy()
        policy.Mode = ModeReadonly
        g, err := NewGate(policy, t.TempDir())
        require.NoError(t, err)
        require.True(t, g.Sandboxed())

        dec, _ := g.Check(context.Background(), Request{
                Action:  ActionBash,
                Command: "make all",
        })
        assert.Equal(t, Deny, dec, "readonly denies bash (writes-nothing contract); the sandbox must not loosen it")
}

func TestSandboxAutopilotBashRuns(t *testing.T) {
        withSandboxSupport(t, true)
        policy := DefaultPolicy()
        policy.Mode = ModeAutopilot
        g, err := NewGate(policy, t.TempDir())
        require.NoError(t, err)

        // Autopilot folds Ask to Deny; the sandboxed branch returns Allow
        // before the fold, so contained bash runs instead of dying.
        dec, _ := g.Check(context.Background(), Request{
                Action:  ActionBash,
                Command: "go build ./...",
        })
        assert.Equal(t, Allow, dec)
}

// Yolo suppresses containment at the source: the mode withdrew approval,
// and walls standing in for prompts would only misdescribe the run. The
// gate must not report "sandboxed" (bashtool compiles no profile on the
// exec side either — pinned in its own package), and the deny list still
// fires, because yolo is deliberately a mode rather than an allow-all flag.
func TestSandboxYoloDisablesContainment(t *testing.T) {
        withSandboxSupport(t, true)
        policy := DefaultPolicy()
        policy.Mode = ModeYolo
        g, err := NewGate(policy, t.TempDir())
        require.NoError(t, err)
        assert.False(t, g.Sandboxed(), "yolo suppresses containment even on a host that supports it")

        // The binary-inspection case that motivated the change: an unlisted,
        // un-denied command that the containment branch would have auto-allowed
        // with a "sandboxed" note now flows to the mode fold and allows bare.
        dec, reason := g.Check(context.Background(), Request{
                Action:  ActionBash,
                Command: "strings /usr/bin/ls | grep gcc",
        })
        assert.Equal(t, Allow, dec, "yolo still folds the would-be Ask to Allow")
        assert.NotContains(t, reason, "sandboxed", "the gate must not claim walls that are not there")

        dec, _ = g.Check(context.Background(), Request{
                Action:  ActionBash,
                Command: "sudo rm -rf /",
        })
        assert.Equal(t, Deny, dec, "the deny list still fires in yolo")
}

// Switching out of yolo re-arms containment: the mode cycle must work in
// both directions, because the gate is rebuilt on every mode switch.
func TestSandboxYoloToAutopilotReArmsContainment(t *testing.T) {
        withSandboxSupport(t, true)
        policy := DefaultPolicy()
        policy.Mode = ModeYolo
        g, err := NewGate(policy, t.TempDir())
        require.NoError(t, err)
        require.False(t, g.Sandboxed())

        policy.Mode = ModeAutopilot
        g, err = NewGate(policy, t.TempDir())
        require.NoError(t, err)
        require.True(t, g.Sandboxed(), "a non-yolo mode enforces again once the gate is rebuilt")

        dec, reason := g.Check(context.Background(), Request{
                Action:  ActionBash,
                Command: "strings /usr/bin/ls | grep gcc",
        })
        assert.Equal(t, Allow, dec)
        assert.Contains(t, reason, "sandboxed", "back in a contained mode the note returns")
}

func TestSandboxNetworkNote(t *testing.T) {
        withSandboxSupport(t, true)
        policy := DefaultPolicy()
        policy.SandboxNetwork = "allow"
        g, err := NewGate(policy, t.TempDir())
        require.NoError(t, err)

        _, reason := g.Check(context.Background(), Request{
                Action:  ActionBash,
                Command: "make all",
        })
        assert.Contains(t, reason, "writes confined")
        assert.NotContains(t, reason, "network denied")
}

// TestMain pins the host-probe seam to "unsupported" for the whole package
// so gate decisions are deterministic on every machine: on a Landlock host
// the real probe would otherwise flip every unpinned Ask expectation into
// a sandbox-folded Allow. Tests that exercise containment opt in
// explicitly via withSandboxSupport, which restores this default on
// cleanup.
func TestMain(m *testing.M) {
        sandboxSupported = func() bool { return false }
        os.Exit(m.Run())
}
