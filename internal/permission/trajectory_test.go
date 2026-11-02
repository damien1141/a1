package permission

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

var _ Gate = fixedGate{}

type fixedGate struct {
	dec Decision
}

func (g fixedGate) Check(context.Context, Request) (Decision, string) {
	return g.dec, ""
}

func (g fixedGate) Admit(context.Context, Request) (Decision, string) {
	return Allow, ""
}

var _ Gate = (*recordingGate)(nil)

type recordingGate struct {
	last Request
}

func (g *recordingGate) Check(context.Context, Request) (Decision, string) {
	return Allow, ""
}

func (g *recordingGate) Admit(context.Context, Request) (Decision, string) {
	return Allow, ""
}

func TestTrajectoryRecordsSteps(t *testing.T) {
	traj := NewTrajectory(5)
	assert.True(t, traj.Record("read"))
	assert.True(t, traj.Record("grep"))
	assert.Equal(t, 2, traj.Len())
	assert.Equal(t, 1, traj.Count("read"))
}

func TestTrajectoryEnforcesMaxSteps(t *testing.T) {
	traj := NewTrajectory(2)
	assert.True(t, traj.Record("read"))
	assert.True(t, traj.Record("grep"))
	assert.False(t, traj.Record("bash"), "third step must be rejected")
	assert.Equal(t, 2, traj.Len())
}

func TestTrajectoryUnlimitedWhenMaxStepsZero(t *testing.T) {
	traj := NewTrajectory(0)
	for i := 0; i < 100; i++ {
		assert.True(t, traj.Record("read"), "step %d must be allowed", i)
	}
	assert.Equal(t, 100, traj.Len())
}

func TestConfinementGateBlocksDisallowedTool(t *testing.T) {
	inner := fixedGate{dec: Allow}
	gate := NewConfinementGate(inner, ConfinementPolicy{AllowedTools: []string{"read", "grep"}})

	dec, _ := gate.Check(nil, Request{Action: ActionRead, Tool: "read"})
	assert.Equal(t, Allow, dec)

	dec, _ = gate.Check(nil, Request{Action: ActionBash, Tool: "bash"})
	assert.Equal(t, Deny, dec)
}

func TestConfinementGateEnforcesStepLimit(t *testing.T) {
	inner := fixedGate{dec: Allow}
	gate := NewConfinementGate(inner, ConfinementPolicy{MaxSteps: 1})

	dec, _ := gate.Check(nil, Request{Tool: "read"})
	assert.Equal(t, Allow, dec)

	dec, _ = gate.Check(nil, Request{Tool: "grep"})
	assert.Equal(t, Deny, dec)
}

func TestConfinementGateAdmits(t *testing.T) {
	inner := &recordingGate{}
	gate := NewConfinementGate(inner, ConfinementPolicy{})

	dec, _ := gate.Admit(nil, Request{})
	assert.Equal(t, Allow, dec)
}
