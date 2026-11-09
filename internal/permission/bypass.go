package permission

import (
	"context"
	"sync/atomic"
)

// SessionAllowGate wraps an inner Gate and allows everything when Enabled is true.
// This replaces the legacy BypassGate, which violated APPA principles by
// bypassing both pre- and post-execution checks unconditionally.
type SessionAllowGate struct {
	Inner   Gate
	Enabled *atomic.Bool
}

// Check returns Allow whenever the session override is enabled; otherwise it defers to the inner gate.
func (g *SessionAllowGate) Check(ctx context.Context, req Request) (Decision, string) {
	if g != nil && g.Enabled != nil && g.Enabled.Load() {
		return Allow, ""
	}
	if g == nil || g.Inner == nil {
		return Allow, ""
	}
	return g.Inner.Check(ctx, req)
}

// Admit returns Allow whenever the session override is enabled; otherwise it defers to the inner gate.
func (g *SessionAllowGate) Admit(ctx context.Context, req Request) (Decision, string) {
	if g != nil && g.Enabled != nil && g.Enabled.Load() {
		return Allow, ""
	}
	if g == nil || g.Inner == nil {
		return Allow, ""
	}
	return g.Inner.Admit(ctx, req)
}

// Unrestricted reports whether g never holds a call back for approval: every
// shape the harness offers for "stop asking me" — yolo mode, the global
// dangerously_allow_all / allow_all_session policy flags, a live
// SessionAllowGate override, or the AllowAll gate headless --yolo builds.
//
// This is the predicate budgets and pacing guards must consult, NOT ModeOf:
// ModeOf answers "which mode's fold rules apply" and deliberately returns ""
// for the short-circuit gates (their folds never run, so there is no mode to
// report), while Unrestricted answers "would this gate ask the model to slow
// down for a human" — and every allow-all shape answers no. Before this
// predicate existed, the per-turn tool-call budget keyed on ModeOf alone, so
// `a1 run --yolo` (an AllowAll gate) and a TUI allow-all session (a
// SessionAllowGate with the override armed) still enforced the cap a user in
// yolo had every reason to believe was lifted.
//
// Like ModeOf it unwraps SessionAllowGate — but only when the override is
// disarmed; an armed override is itself an unrestricted gate regardless of
// what it wraps.
func Unrestricted(g Gate) bool {
	for g != nil {
		switch x := g.(type) {
		case *StaticGate:
			return x.Policy.Mode == ModeYolo ||
				x.Policy.DangerouslyAllowAll ||
				x.Policy.AllowAllSession
		case *SessionAllowGate:
			if x.Enabled != nil && x.Enabled.Load() {
				return true
			}
			g = x.Inner
		case AllowAll:
			return true
		default:
			return false
		}
	}
	return false
}
