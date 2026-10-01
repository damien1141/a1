package permission

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionAllowGateToggle exercises the core APPA contract:
// a session allow-all toggle must not rebuild the gate, and must restore
// the inner gate's decisions when disabled.
func TestSessionAllowGateToggle(t *testing.T) {
	inner, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)

	var allow atomic.Bool
	g := &SessionAllowGate{Inner: inner, Enabled: &allow}

	// disabled → inner gate decides: sudo is hard-deny
	dec, _ := g.Check(context.Background(), Request{Action: ActionBash, Command: "sudo true"})
	assert.Equal(t, Deny, dec, "disabled: inner gate should deny sudo")

	// enabled → everything allowed, including hard-deny commands
	allow.Store(true)
	dec, _ = g.Check(context.Background(), Request{Action: ActionBash, Command: "sudo rm -rf /"})
	assert.Equal(t, Allow, dec, "enabled: session override should bypass deny list")

	// disabled again → inner gate back in control
	allow.Store(false)
	dec, _ = g.Check(context.Background(), Request{Action: ActionBash, Command: "sudo true"})
	assert.Equal(t, Deny, dec, "disabled again: inner gate should deny sudo")
}

// TestSessionAllowGateAdmitsWhenEnabled verifies post-execution admission
// is also bypassed by the session allow-all toggle.
func TestSessionAllowGateAdmitsWhenEnabled(t *testing.T) {
	p := DefaultPolicy()
	p.NoUntrustedOutput = true
	innerDeny, _ := NewGate(p, t.TempDir())

	var allow atomic.Bool
	g := &SessionAllowGate{Inner: innerDeny, Enabled: &allow}

	// disabled → untrusted output denied by policy
	dec, _ := g.Admit(context.Background(), Request{OutputLabel: Label{Trust: Untrusted}})
	assert.Equal(t, Deny, dec, "disabled: admission should enforce policy")

	// enabled → admission bypassed
	allow.Store(true)
	dec, _ = g.Admit(context.Background(), Request{OutputLabel: Label{Trust: Untrusted}})
	assert.Equal(t, Allow, dec, "enabled: admission should be bypassed")
}

// TestDangerouslyAllowAllPolicyFlag confirms the global config flag produces
// an allow-all gate without needing a SessionAllowGate wrapper.
func TestDangerouslyAllowAllPolicyFlag(t *testing.T) {
	p := DefaultPolicy()
	p.DangerouslyAllowAll = true

	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	dec, _ := g.Check(context.Background(), Request{Action: ActionBash, Command: "sudo rm -rf /"})
	assert.Equal(t, Allow, dec, "dangerously_allow_all should bypass deny list")
}

// TestAllowAllSessionPolicyFlag confirms the session-only flag produces
// an allow-all gate for this session only.
func TestAllowAllSessionPolicyFlag(t *testing.T) {
	p := DefaultPolicy()
	p.AllowAllSession = true

	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	dec, _ := g.Check(context.Background(), Request{Action: ActionBash, Command: "sudo true"})
	assert.Equal(t, Allow, dec, "allow_all_session should bypass deny list")
}

// TestModeOfUnwrapsSessionAllowGate ensures ModeOf still follows the chain
// through SessionAllowGate to the underlying StaticGate.
func TestModeOfUnwrapsSessionAllowGate(t *testing.T) {
	g, err := NewGate(policyInMode(ModeReadonly), t.TempDir())
	require.NoError(t, err)

	var allow atomic.Bool
	wrapped := &SessionAllowGate{Inner: g, Enabled: &allow}

	assert.Equal(t, ModeReadonly, ModeOf(wrapped), "ModeOf should unwrap SessionAllowGate")
}

// TestSessionAllowGatePreservesModeSwitch verifies that toggling the
// session allow-all flag does not interfere with mode switching.
func TestSessionAllowGatePreservesModeSwitch(t *testing.T) {
	inner, err := NewGate(policyInMode(ModeInteractive), t.TempDir())
	require.NoError(t, err)

	var allow atomic.Bool
	g := &SessionAllowGate{Inner: inner, Enabled: &allow}

	// mode switch rebuilds inner gate; wrapper stays the same
	inner2, err := NewGate(policyInMode(ModeReadonly), t.TempDir())
	require.NoError(t, err)
	g.Inner = inner2

	allow.Store(false)
	dec, _ := g.Check(context.Background(), Request{Action: ActionWrite, Paths: []string{t.TempDir() + "/a.txt"}})
	assert.Equal(t, Deny, dec, "readonly mode should deny writes after mode switch")

	allow.Store(true)
	dec, _ = g.Check(context.Background(), Request{Action: ActionWrite, Paths: []string{t.TempDir() + "/b.txt"}})
	assert.Equal(t, Allow, dec, "session allow-all should override readonly mode")
}

// TestAPPAIntegrationEndToEnd is a high-level smoke test exercising the
// full APPA stack introduced in steps 1-6: labels, admission, authority,
// trajectory confinement, and session allow-all.
func TestAPPAIntegrationEndToEnd(t *testing.T) {
	// --- step 1/3: admission gate with labels ---
	p := DefaultPolicy()
	p.NoUntrustedOutput = true
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	dec, _ := g.Admit(context.Background(), Request{OutputLabel: Label{Trust: Untrusted}})
	assert.Equal(t, Deny, dec, "admission should block untrusted output")

	dec, _ = g.Admit(context.Background(), Request{OutputLabel: Label{Trust: User}})
	assert.Equal(t, Allow, dec, "admission should allow trusted output")

	// --- step 2: effect log ---
	log := NewEffectLog()
	log.Commit("bash", "ticket_created")
	require.NoError(t, log.CheckRequiredEffects([]string{"ticket_created"}))
	require.Error(t, log.CheckRequiredEffects([]string{"missing"}))

	// --- step 4: authority ---
	auth := AuthorityFunc(func(ctx context.Context, callHash string, req Request) (Decision, string) {
		if callHash == "" {
			return Deny, "missing call hash"
		}
		return Allow, "authorized:" + callHash
	})
	req := Request{Tool: "bash", Command: "echo hi"}
	dec, _ = auth.Authorize(nil, CallHash(req), req)
	assert.Equal(t, Allow, dec)

	// --- step 5: trajectory confinement ---
	conf := NewConfinementGate(fixedGate{dec: Allow}, ConfinementPolicy{MaxSteps: 1})
	dec, _ = conf.Check(context.Background(), Request{Tool: "read"})
	assert.Equal(t, Allow, dec, "first step allowed")
	dec, _ = conf.Check(context.Background(), Request{Tool: "read"})
	assert.Equal(t, Deny, dec, "second step denied by trajectory limit")

	conf2 := NewConfinementGate(fixedGate{dec: Allow}, ConfinementPolicy{AllowedTools: []string{"read"}})
	dec, _ = conf2.Check(context.Background(), Request{Tool: "read"})
	assert.Equal(t, Allow, dec)
	dec, _ = conf2.Check(context.Background(), Request{Tool: "bash"})
	assert.Equal(t, Deny, dec, "disallowed tool denied by confinement")

	// --- step 6: session allow-all ---
	inner, _ := NewGate(DefaultPolicy(), t.TempDir())
	var allow atomic.Bool
	sg := &SessionAllowGate{Inner: inner, Enabled: &allow}

	allow.Store(false)
	dec, _ = sg.Check(context.Background(), Request{Action: ActionBash, Command: "sudo true"})
	assert.Equal(t, Deny, dec, "disabled: inner gate denies sudo")

	allow.Store(true)
	dec, _ = sg.Check(context.Background(), Request{Action: ActionBash, Command: "sudo true"})
	assert.Equal(t, Allow, dec, "enabled: session override allows sudo")

	allow.Store(false)
	dec, _ = sg.Check(context.Background(), Request{Action: ActionBash, Command: "git status"})
	assert.Equal(t, Allow, dec, "disabled: allowlisted bash still allowed")
}
