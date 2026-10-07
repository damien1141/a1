package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/project"
)

func TestPermissionMode_CycleAndSet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PHI_MODEL", "test-model")
	t.Setenv("PHI_API_KEY", "test-key")
	t.Setenv("PHI_BASE_URL", "http://127.0.0.1:9")

	cwd := t.TempDir()
	proj, err := project.Discover(cwd)
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())

	bus := NewBus(nil)
	ctrl, err := NewController(bus, proj, cwd)
	require.NoError(t, err)
	require.NotNil(t, ctrl)

	// Default mode is interactive.
	assert.Equal(t, permission.ModeInteractive, ctrl.PermissionMode())

	// Set to readonly.
	ctrl.SetPermissionMode(permission.ModeReadonly)
	assert.Equal(t, permission.ModeReadonly, ctrl.PermissionMode())

	// Cycle should move to autopilot.
	ctrl.CyclePermissionMode()
	assert.Equal(t, permission.ModeAutopilot, ctrl.PermissionMode())

	// Cycle again -> headless-strict.
	ctrl.CyclePermissionMode()
	assert.Equal(t, permission.ModeHeadlessStrict, ctrl.PermissionMode())

	// Cycle again -> interactive.
	ctrl.CyclePermissionMode()
	assert.Equal(t, permission.ModeInteractive, ctrl.PermissionMode())

	// Verify APPA trajectory state survives mode switches.
	inner := ctrl.gate.(*permission.SessionAllowGate).Inner.(*permission.StaticGate)
	inner.Trajectory().Effects.Commit("setup", "setup_done")
	inner.Trajectory().Label = permission.NewPartialLabel(
		permission.Label{Trust: permission.Untrusted, ReaderSet: []string{}},
		[]string{"src1"},
	)

	ctrl.SetPermissionMode(permission.ModeReadonly)
	inner = ctrl.gate.(*permission.SessionAllowGate).Inner.(*permission.StaticGate)
	assert.True(t, inner.Trajectory().Effects.Has("setup_done"), "trajectory effects should survive mode switch")
	assert.Equal(t, 1, len(inner.Trajectory().Label.Unresolved), "unresolved sources should survive mode switch")
}
