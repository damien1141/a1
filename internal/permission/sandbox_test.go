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
