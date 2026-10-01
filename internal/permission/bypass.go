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
