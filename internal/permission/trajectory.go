package permission

import (
	"context"
	"fmt"
	"strings"
)

// Trajectory records the tool-call history for a single sub-agent execution.
type Trajectory struct {
	Steps    []string // tool names in call order
	StepSet  map[string]int
	MaxSteps int
}

// NewTrajectory creates an empty trajectory bounded by maxSteps.
func NewTrajectory(maxSteps int) *Trajectory {
	if maxSteps < 0 {
		maxSteps = 0
	}
	return &Trajectory{
		Steps:    make([]string, 0, maxSteps),
		StepSet:  make(map[string]int),
		MaxSteps: maxSteps,
	}
}

// Record appends a tool name. Returns false if the trajectory is full.
func (t *Trajectory) Record(tool string) bool {
	if t == nil {
		return true
	}
	if t.MaxSteps > 0 && len(t.Steps) >= t.MaxSteps {
		return false
	}
	t.Steps = append(t.Steps, tool)
	t.StepSet[tool]++
	return true
}

// Count returns how many times tool appears in the trajectory.
func (t *Trajectory) Count(tool string) int {
	if t == nil {
		return 0
	}
	return t.StepSet[tool]
}

// Len returns the number of recorded steps.
func (t *Trajectory) Len() int {
	if t == nil {
		return 0
	}
	return len(t.Steps)
}

// ConfinementPolicy describes trajectory limits for a sub-agent.
type ConfinementPolicy struct {
	// MaxSteps is the maximum number of tool calls allowed. 0 = unlimited.
	MaxSteps int
	// AllowedTools is a whitelist of tool names. Empty = all tools allowed.
	AllowedTools []string
}

// ConfinementGate wraps an inner Gate and enforces trajectory limits.
type ConfinementGate struct {
	Inner      Gate
	Policy     ConfinementPolicy
	trajectory *Trajectory
}

// NewConfinementGate creates a gate that enforces trajectory limits.
func NewConfinementGate(inner Gate, policy ConfinementPolicy) *ConfinementGate {
	return &ConfinementGate{
		Inner:      inner,
		Policy:     policy,
		trajectory: NewTrajectory(policy.MaxSteps),
	}
}

// Check evaluates req and enforces trajectory limits before delegating to Inner.
func (g *ConfinementGate) Check(ctx context.Context, req Request) (Decision, string) {
	if !g.trajectory.Record(req.Tool) {
		return Deny, fmt.Sprintf("trajectory limit reached: max %d steps", g.Policy.MaxSteps)
	}
	if len(g.Policy.AllowedTools) > 0 && !g.allowed(req.Tool) {
		return Deny, fmt.Sprintf("tool %q not allowed by trajectory confinement", req.Tool)
	}
	return g.Inner.Check(ctx, req)
}

// Admit delegates to the inner gate. Trajectory is not re-checked on admission.
func (g *ConfinementGate) Admit(ctx context.Context, req Request) (Decision, string) {
	if inner, ok := g.Inner.(AdmissionGate); ok {
		return inner.Admit(ctx, req)
	}
	return Allow, ""
}

// Trajectory returns the recorded trajectory for inspection.
func (g *ConfinementGate) Trajectory() *Trajectory {
	return g.trajectory
}

func (g *ConfinementGate) allowed(tool string) bool {
	for _, a := range g.Policy.AllowedTools {
		if strings.EqualFold(a, tool) {
			return true
		}
	}
	return false
}
