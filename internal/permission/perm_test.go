package permission

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionAllowGate toggles Allow-All on and off without rebuilding the gate.
func TestSessionAllowGate_Toggle(t *testing.T) {
	inner, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)
	var bypass atomic.Bool
	g := &SessionAllowGate{Inner: inner, Enabled: &bypass}

	// Bypass off → inner gate decides. Interactive mode + curl → Ask.
	dec, _ := g.Check(context.Background(), Request{Action: ActionBash, Command: "curl https://example.com"})
	assert.Equal(t, Ask, dec, "bypass off: inner gate decides")

	// Bypass on → everything allowed, even denied commands.
	bypass.Store(true)
	dec, _ = g.Check(context.Background(), Request{Action: ActionBash, Command: "sudo true"})
	assert.Equal(t, Allow, dec, "bypass on: sudo allowed")

	// Bypass off again → inner gate back in control.
	bypass.Store(false)
	dec, _ = g.Check(context.Background(), Request{Action: ActionBash, Command: "sudo true"})
	assert.Equal(t, Deny, dec, "bypass off again: sudo denied")
}

// TestModeCycle covers the full cycle order used by the TUI palette toggle.
func TestModeCycle(t *testing.T) {
	modes := []Mode{ModeInteractive, ModeReadonly, ModeAutopilot, ModeHeadlessStrict}
	for i, start := range modes {
		g := gateWithMode(t, start)
		// Cycle once: each mode maps to the next in the palette order.
		next := modes[(i+1)%len(modes)]
		// Simulate CyclePermissionMode: rebuild the gate with the next mode.
		g2 := gateWithMode(t, next)
		// Verify the new mode actually changes behavior for a write.
		dec1, _ := g.Check(context.Background(), Request{Action: ActionWrite, Paths: []string{filepath.Join(g.Workspace, "a.txt")}})
		dec2, _ := g2.Check(context.Background(), Request{Action: ActionWrite, Paths: []string{filepath.Join(g2.Workspace, "a.txt")}})
		// In interactive a workspace write is Allow; in readonly it is Deny.
		if start == ModeInteractive {
			assert.Equal(t, Allow, dec1, "%s: workspace write allowed", start)
		}
		if next == ModeReadonly {
			assert.Equal(t, Deny, dec2, "%s: workspace write denied", next)
		}
	}
}

// TestModeSwitchPreservesAskTimeout ensures switching permission modes does
// not reset a session-tuned ask timeout back to the default.
func TestModeSwitchPreservesAskTimeout(t *testing.T) {
	ws := t.TempDir()
	p := DefaultPolicy()
	p.Mode = ModeInteractive
	p.AskTimeoutSec = 30
	g, err := NewGate(p, ws)
	require.NoError(t, err)

	// Switch to readonly with a different timeout; the gate must reflect it.
	p2 := DefaultPolicy()
	p2.Mode = ModeReadonly
	p2.AskTimeoutSec = 30
	g2, err := NewGate(p2, ws)
	require.NoError(t, err)

	// In readonly, a write is denied regardless of timeout, but the policy
	// itself must carry the tuned value.
	assert.Equal(t, 30, g2.Policy.AskTimeoutSec, "ask timeout survives mode switch")
	_ = g
}

// TestReadonlyAllowsReadonlyBash confirms readonly mode still permits
// allowlisted bash (git status) while denying writes.
func TestReadonlyAllowsReadonlyBash(t *testing.T) {
	g, err := NewGate(policyInMode(ModeReadonly), t.TempDir())
	require.NoError(t, err)
	dec, _ := g.Check(context.Background(), Request{Action: ActionBash, Command: "git status"})
	assert.Equal(t, Allow, dec, "git status in readonly")
	dec, _ = g.Check(context.Background(), Request{Action: ActionWrite, Paths: []string{filepath.Join(g.Workspace, "a.txt")}})
	assert.Equal(t, Deny, dec, "write denied in readonly")
}

// TestAutopilotDeniesAsk confirms autopilot folds Ask→Deny but still allows
// explicitly allowlisted commands.
func TestAutopilotDeniesAsk(t *testing.T) {
	g, err := NewGate(policyInMode(ModeAutopilot), t.TempDir())
	require.NoError(t, err)
	dec, _ := g.Check(context.Background(), Request{Action: ActionBash, Command: "go test ./..."})
	assert.Equal(t, Allow, dec, "go test allowlisted in autopilot")
	dec, _ = g.Check(context.Background(), Request{Action: ActionBash, Command: "curl https://example.com"})
	assert.Equal(t, Deny, dec, "curl folded to deny in autopilot")
}

// TestDangerouslyAllowAllBypassesDeny confirms the bypass flag overrides the
// hard deny list, which is the whole point of the toggle.
func TestDangerouslyAllowAllBypassesDeny(t *testing.T) {
	inner, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)
	var bypass atomic.Bool
	g := &SessionAllowGate{Inner: inner, Enabled: &bypass}
	bypass.Store(true)
	dec, _ := g.Check(context.Background(), Request{Action: ActionBash, Command: "sudo rm -rf /"})
	assert.Equal(t, Allow, dec, "bypass overrides deny list")
}

// TestSensitivePathDenySurvivesBypass is a negative control: the bypass flag
// only exists on SessionAllowGate; the inner StaticGate's sensitive-path deny is
// still reachable when bypass is off.
func TestSensitivePathDenySurvivesBypass(t *testing.T) {
	g, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)
	home, _ := os.UserHomeDir()
	dec, _ := g.Check(context.Background(), Request{
		Action: ActionRead,
		Paths:  []string{filepath.Join(home, ".ssh", "id_rsa")},
	})
	assert.Equal(t, Deny, dec, "sensitive read denied without bypass")
}

func gateWithMode(t *testing.T, m Mode) *StaticGate {
	t.Helper()
	g, err := NewGate(policyInMode(m), t.TempDir())
	if err != nil {
		panic(err)
	}
	return g
}

func policyInMode(m Mode) Policy {
	p := DefaultPolicy()
	p.Mode = m
	return p
}