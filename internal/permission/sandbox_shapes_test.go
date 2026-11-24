package permission_test

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/permission"
)

// Every UI that can put the harness into "never ask" mode, and the gate shape
// each one hands the engine. The bug this pins: the two sides of the bash
// sandbox asked DIFFERENT questions. The gate suppressed containment on
// `Mode == yolo` alone; the exec side read the live mode off the context. So a
// switch made through a shape that reports no mode (a bare AllowAll from
// `a1 run --yolo`, an armed session override) lifted approval while leaving the
// walls up — "yolo doesn't disable the sandbox", reported from the UI.
//
// The contract asserted here is the one the fix establishes: for every shape,
// gate.Sandboxed() is false AND permission.Unrestricted reports true. When both
// hold, the executor attaches yolo to the tool context, and bashtool compiles
// no profile (pinned in that package). One predicate, no drift.
func TestSandboxOffForEveryNeverAskShape_Differential(t *testing.T) {
	restore := permission.PinSandboxSupportedForTest(true)
	defer restore()

	// Each constructor below mirrors a real entry point:
	//   - web/TUI mode switch  -> a StaticGate whose Policy.Mode is yolo
	//   - config flag          -> DangerouslyAllowAll on an interactive policy
	//   - per-session lift     -> AllowAllSession
	//   - `a1 run --yolo`      -> HeadlessGate's bare AllowAll
	//   - allow-all button     -> an armed SessionAllowGate
	armed := &atomic.Bool{}
	armed.Store(true)
	disarmed := &atomic.Bool{}

	inner, err := permission.NewGate(permission.Policy{Mode: permission.ModeInteractive, Sandbox: "on"}, t.TempDir())
	require.NoError(t, err)

	yoloPolicy := permission.DefaultPolicy()
	yoloPolicy.Mode = permission.ModeYolo
	yoloPolicy.Sandbox = "on"

	flagPolicy := permission.DefaultPolicy()
	flagPolicy.Mode = permission.ModeInteractive
	flagPolicy.Sandbox = "on"
	flagPolicy.DangerouslyAllowAll = true

	sessionPolicy := permission.DefaultPolicy()
	sessionPolicy.Mode = permission.ModeInteractive
	sessionPolicy.Sandbox = "on"
	sessionPolicy.AllowAllSession = true

	shapes := map[string]permission.Gate{
		"web/TUI yolo mode":            mustGate(t, yoloPolicy),
		"config dangerously_allow_all": mustGate(t, flagPolicy),
		"session allow_all_session":    mustGate(t, sessionPolicy),
		"a1 run --yolo (AllowAll)":     permission.AllowAll{},
		"armed session override":       &permission.SessionAllowGate{Inner: inner, Enabled: armed},
	}

	for name, gate := range shapes {
		t.Run(name, func(t *testing.T) {
			assert.True(t, permission.Unrestricted(gate),
				"the gate must report itself unrestricted, or the executor never marks the context yolo")

			// The sandbox question, asked wherever the gate exposes an answer.
			if sg, ok := gate.(*permission.StaticGate); ok {
				assert.False(t, sg.Sandboxed(), "an unrestricted gate must not claim walls")
			}
		})
	}

	// The controls: shapes that still ask must keep their walls, so the mapping
	// above cannot be a blanket "sandbox never enforces".
	contained := map[string]permission.Gate{
		"interactive":               mustGate(t, mustPolicy(t, permission.ModeInteractive)),
		"readonly":                  mustGate(t, mustPolicy(t, permission.ModeReadonly)),
		"autopilot":                 mustGate(t, mustPolicy(t, permission.ModeAutopilot)),
		"headless-strict":           mustGate(t, mustPolicy(t, permission.ModeHeadlessStrict)),
		"disarmed session override": &permission.SessionAllowGate{Inner: inner, Enabled: disarmed},
	}
	for name, gate := range contained {
		t.Run(name, func(t *testing.T) {
			assert.False(t, permission.Unrestricted(gate))
			if sg, ok := gate.(*permission.StaticGate); ok {
				assert.True(t, sg.Sandboxed(), "a prompting gate keeps its containment")
			}
		})
	}
}

func mustGate(t *testing.T, p permission.Policy) *permission.StaticGate {
	t.Helper()
	g, err := permission.NewGate(p, t.TempDir())
	require.NoError(t, err)
	return g
}

func mustPolicy(t *testing.T, mode permission.Mode) permission.Policy {
	t.Helper()
	p := permission.DefaultPolicy()
	p.Mode = mode
	p.Sandbox = "on"
	return p
}

// TestPolicyUnrestricted_MatchesGateUnrestricted_Differential: the policy-level
// predicate (which bashtool uses on the exec side) and the gate-level predicate
// (which the executor uses to mark the context) must agree on every shape. They
// were separate expressions before, and that separation IS the bug class.
func TestPolicyUnrestricted_MatchesGateUnrestricted_Differential(t *testing.T) {
	restore := permission.PinSandboxSupportedForTest(true)
	defer restore()

	cases := []permission.Policy{
		{Mode: permission.ModeInteractive},
		{Mode: permission.ModeReadonly},
		{Mode: permission.ModeAutopilot},
		{Mode: permission.ModeHeadlessStrict},
		{Mode: permission.ModeYolo},
		{Mode: permission.ModeInteractive, DangerouslyAllowAll: true},
		{Mode: permission.ModeInteractive, AllowAllSession: true},
		{Mode: permission.ModeReadonly, DangerouslyAllowAll: true},
		{Mode: permission.ModeYolo, DangerouslyAllowAll: true, AllowAllSession: true},
	}
	for _, p := range cases {
		g, err := permission.NewGate(p, t.TempDir())
		require.NoError(t, err)
		assert.Equal(t, p.Unrestricted(), permission.Unrestricted(g),
			"policy and gate must answer the same question for %+v", p)
		assert.Equal(t, p.Unrestricted(), !g.Sandboxed(),
			"unrestricted and unsandboxed are the same truth for %+v", p)
	}
}
