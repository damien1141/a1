package permission

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnrestricted covers every gate shape that promises "never ask, never
// hold back": yolo mode, the two allow-all policy flags, an armed
// SessionAllowGate override, and the bare AllowAll gate headless --yolo
// builds. The predicate exists because the tool-call budget used to key on
// ModeOf alone — ModeOf returns "" for the short-circuit gates, so `a1 run
// --yolo` and a TUI allow-all session still enforced a cap the user had
// every reason to believe was lifted.
func TestUnrestricted(t *testing.T) {
	newGate := func(mutate func(*Policy)) *StaticGate {
		p := DefaultPolicy()
		if mutate != nil {
			mutate(&p)
		}
		g, err := NewGate(p, t.TempDir())
		require.NoError(t, err)
		return g
	}

	cases := []struct {
		name string
		gate Gate
		want bool
	}{
		{"nil gate", nil, false},
		{"yolo mode", newGate(func(p *Policy) { p.Mode = ModeYolo }), true},
		{"interactive mode", newGate(func(p *Policy) { p.Mode = ModeInteractive }), false},
		{"readonly mode", newGate(func(p *Policy) { p.Mode = ModeReadonly }), false},
		{"autopilot mode", newGate(func(p *Policy) { p.Mode = ModeAutopilot }), false},
		{"dangerously_allow_all", newGate(func(p *Policy) { p.DangerouslyAllowAll = true }), true},
		{"allow_all_session", newGate(func(p *Policy) { p.AllowAllSession = true }), true},
		{"bare AllowAll (a1 run --yolo)", AllowAll{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Unrestricted(tc.gate))
		})
	}
}

// TestUnrestrictedSessionAllowGate pins the override semantics: an armed
// override is unrestricted regardless of what it wraps, and a disarmed one
// defers to the inner gate (both directions).
func TestUnrestrictedSessionAllowGate(t *testing.T) {
	inner, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)
	yolo, err := NewGate(policyInMode(ModeYolo), t.TempDir())
	require.NoError(t, err)

	var armed, disarmed atomic.Bool

	bypass := &SessionAllowGate{Inner: inner, Enabled: &armed}
	assert.False(t, Unrestricted(bypass), "disarmed override defers to the inner gate")
	armed.Store(true)
	assert.True(t, Unrestricted(bypass), "armed override is unrestricted even around a plain gate")
	armed.Store(false)
	assert.False(t, Unrestricted(bypass))

	yoloWrap := &SessionAllowGate{Inner: yolo, Enabled: &disarmed}
	assert.True(t, Unrestricted(yoloWrap), "disarmed override still defers to a yolo inner gate")

	// Wrappers stack: override around override.
	nested := &SessionAllowGate{Inner: bypass, Enabled: &disarmed}
	armed.Store(true)
	assert.True(t, Unrestricted(nested), "an armed inner override makes the whole chain unrestricted")
	armed.Store(false)
	assert.False(t, Unrestricted(nested))
}

// TestUnrestrictedUnknownGate documents that unknown gate types are NOT
// unrestricted: a custom gate that reports no known shape fails closed, the
// same posture ModeOf takes.
func TestUnrestrictedUnknownGate(t *testing.T) {
	assert.False(t, Unrestricted(customGateFunc{}))
}

type customGateFunc struct{}

func (customGateFunc) Check(_ context.Context, _ Request) (Decision, string) { return Allow, "" }
func (customGateFunc) Admit(_ context.Context, _ Request) (Decision, string) { return Allow, "" }
