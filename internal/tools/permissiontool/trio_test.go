package permissiontool

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/engineaccess"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// stubEngine is the minimal engineaccess.Engine surface the tool consumes.
type stubEngine struct{ gate permission.Gate }

func (e stubEngine) Gate() permission.Gate { return e.gate }

// denyAllGate refuses everything, so SessionAllowGate's deferral to the inner
// gate is observable through the permission package's own Check/Admit.
type denyAllGate struct{}

func (denyAllGate) Check(context.Context, permission.Request) (permission.Decision, string) {
	return permission.Deny, "stub denies everything"
}

func (denyAllGate) Admit(context.Context, permission.Request) (permission.Decision, string) {
	return permission.Deny, "stub denies everything"
}

func useEngine(t *testing.T, eng engineaccess.Engine) {
	t.Helper()
	engineaccess.SetActiveEngine(eng)
	// Cleanup clears the registry for whichever test runs next (and for
	// -count>1 reruns): SetActiveEngine(nil) is the documented clear path,
	// and it clears for real now that the store boxes the engine.
	t.Cleanup(func() { engineaccess.SetActiveEngine(nil) })
}

func runPermissionTool(t *testing.T, eng engineaccess.Engine, raw string) (tooldef.Result, error) {
	t.Helper()
	useEngine(t, eng)
	return PermissionTool().Run(t.Context(), json.RawMessage(raw))
}

func actionJSON(action string) string {
	b, _ := json.Marshal(map[string]any{"action": action})
	return string(b)
}

func newStaticGate(t *testing.T) *permission.StaticGate {
	t.Helper()
	g, err := permission.NewGate(permission.DefaultPolicy(), t.TempDir())
	require.NoError(t, err)
	return g
}

// TestRunPermission_NoEngine_Negative must be the first test in this file
// to touch the tool: ActiveEngine() only returns nil while nothing is
// stored. Every other test clears its engine on cleanup via
// SetActiveEngine(nil), so the registry is empty here on the first run and
// on -count>1 reruns alike; the skip stays as a guard against future
// registry leaks, not as a workaround for an unclearable global.
func TestRunPermission_NoEngine_Negative(t *testing.T) {
	if eng := engineaccess.ActiveEngine(); eng != nil {
		t.Skipf("engine already stored (%T); the nil path is covered by the first run", eng)
	}
	_, err := PermissionTool().Run(t.Context(), json.RawMessage(actionJSON("status")))
	require.Error(t, err)
	assert.EqualError(t, err, "permission: no active engine")
}

// ---------- positive ----------

func TestPermissionTool_Positive(t *testing.T) {
	tool := PermissionTool()
	assert.Equal(t, "permission", tool.Definition.Name)
	assert.True(t, tool.Definition.Readable)
	assert.Equal(t, []string{"action"}, tool.Definition.Params.Required)
	require.NotNil(t, tool.Run)
	require.NotNil(t, tool.DetailFromArgs)

	static := newStaticGate(t)
	eng := stubEngine{gate: static}

	t.Run("status reports mode and allow-all", func(t *testing.T) {
		res, err := runPermissionTool(t, eng, actionJSON("status"))
		require.NoError(t, err)
		assert.Contains(t, res.Content, "mode: interactive\n")
		assert.Contains(t, res.Content, "allow_all_session: false\n")
		assert.Equal(t, "permission status", res.Detail)
		assert.Equal(t, res.Content, res.Output)
	})

	t.Run("blank action defaults to status", func(t *testing.T) {
		res, err := runPermissionTool(t, eng, `{"action": "  "}`)
		require.NoError(t, err)
		assert.Contains(t, res.Content, "mode: interactive\n")
	})

	t.Run("status reports an armed session override", func(t *testing.T) {
		sg := &permission.SessionAllowGate{Inner: static, Enabled: new(atomic.Bool)}
		sg.Enabled.Store(true)
		res, err := runPermissionTool(t, stubEngine{gate: sg}, actionJSON("status"))
		require.NoError(t, err)
		assert.Contains(t, res.Content, "allow_all_session: true\n")
	})

	t.Run("trajectory on a static gate", func(t *testing.T) {
		res, err := runPermissionTool(t, eng, actionJSON("trajectory"))
		require.NoError(t, err)
		// NewTrajectoryState seeds the User label over the top reader set.
		assert.Contains(t, res.Content, "label: {user [user agent tool]}\n")
		assert.Contains(t, res.Content, "unresolved: []\n")
		assert.Contains(t, res.Content, "effects: []\n")
		assert.Contains(t, res.Content, "accepted_narrowing: map[]\n")
		assert.Contains(t, res.Content, "rulings: 0\n")
		assert.Equal(t, "trajectory state", res.Detail)
	})

	t.Run("recovery on a static gate", func(t *testing.T) {
		res, err := runPermissionTool(t, eng, actionJSON("recovery"))
		require.NoError(t, err)
		assert.Equal(t, "authorities: 0\n", res.Content)
		assert.Equal(t, "recovery graph", res.Detail)
	})

	t.Run("authority config", func(t *testing.T) {
		p := permission.DefaultPolicy()
		p.RequiresAuthority = true
		p.Authority = "otp-hsm"
		g, err := permission.NewGate(p, t.TempDir())
		require.NoError(t, err)
		res, err := runPermissionTool(t, stubEngine{gate: g}, actionJSON("authority"))
		require.NoError(t, err)
		assert.Equal(t, "requires_authority: true\nauthority_type: string\n", res.Content)
		assert.Equal(t, "authority config", res.Detail)

		res, err = runPermissionTool(t, eng, actionJSON("authority"))
		require.NoError(t, err)
		assert.Equal(t, "requires_authority: false\n", res.Content)
	})
}

// ---------- negative ----------

func TestPermissionTool_Negative(t *testing.T) {
	static := newStaticGate(t)

	t.Run("malformed json", func(t *testing.T) {
		_, err := runPermissionTool(t, stubEngine{gate: static}, `{"action"`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse permission arguments")
	})

	t.Run("unknown action", func(t *testing.T) {
		_, err := runPermissionTool(t, stubEngine{gate: static}, actionJSON("self_destruct"))
		require.Error(t, err)
		assert.EqualError(t, err, `unknown permission action "self_destruct"`)
	})

	t.Run("toggle on a gate without a session toggle", func(t *testing.T) {
		_, err := runPermissionTool(t, stubEngine{gate: static}, actionJSON("toggle_allow_all"))
		require.Error(t, err)
		assert.EqualError(t, err,
			"permission: session allow-all toggle not supported for gate type *permission.StaticGate")
	})

	t.Run("toggle on a session gate with a nil toggle", func(t *testing.T) {
		sg := &permission.SessionAllowGate{Inner: static}
		_, err := runPermissionTool(t, stubEngine{gate: sg}, actionJSON("toggle_allow_all"))
		require.Error(t, err)
		assert.EqualError(t, err, "permission: gate has no session allow-all toggle")
	})

	// trajectory/recovery/authority are StaticGate-only inspections: a
	// session-wrapped gate must be rejected with the specific message.
	sg := &permission.SessionAllowGate{Inner: static, Enabled: new(atomic.Bool)}
	for _, action := range []string{"trajectory", "recovery", "authority"} {
		t.Run(action+" requires StaticGate", func(t *testing.T) {
			_, err := runPermissionTool(t, stubEngine{gate: sg}, actionJSON(action))
			require.Error(t, err)
			assert.EqualError(t, err, "permission: "+action+" inspection requires StaticGate")
		})
	}

	t.Run("bare static gate carries no trajectory state", func(t *testing.T) {
		_, err := runPermissionTool(t, stubEngine{gate: &permission.StaticGate{}}, actionJSON("trajectory"))
		require.Error(t, err)
		assert.EqualError(t, err, "permission: no trajectory state")
	})
}

// ---------- differential ----------

// oracle: permission.Gate itself — the toggle action's reported state must
// agree with what the permission package observes through SessionAllowGate
// Check/Admit and Unrestricted on the very gate the tool mutated, not just
// with the tool's own echo of the flag it stored.
func TestRunPermissionToggle_Differential(t *testing.T) {
	sg := &permission.SessionAllowGate{Inner: denyAllGate{}, Enabled: new(atomic.Bool)}
	eng := stubEngine{gate: sg}
	ctx := t.Context()
	req := permission.Request{Action: permission.ActionBash, Tool: "bash", Command: "ls -la"}

	// Before: the override is disarmed, the inner gate's Deny shows through.
	dec, _ := sg.Check(ctx, req)
	assert.Equal(t, permission.Deny, dec)
	assert.False(t, permission.Unrestricted(sg))

	res, err := runPermissionTool(t, eng, actionJSON("toggle_allow_all"))
	require.NoError(t, err)
	assert.Equal(t, "allow_all_session: true\n", res.Content)

	// After: the same request now allows, and the package calls the gate
	// unrestricted — the tool's report matches the gate's actual behavior.
	dec, _ = sg.Check(ctx, req)
	assert.Equal(t, permission.Allow, dec)
	dec, _ = sg.Admit(ctx, req)
	assert.Equal(t, permission.Allow, dec)
	assert.True(t, permission.Unrestricted(sg))

	// A fresh status call agrees with the toggled state.
	res, err = runPermissionTool(t, eng, actionJSON("status"))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "allow_all_session: true\n")

	// Toggling back re-arms the inner gate's decisions.
	res, err = runPermissionTool(t, eng, actionJSON("toggle_allow_all"))
	require.NoError(t, err)
	assert.Equal(t, "allow_all_session: false\n", res.Content)
	dec, _ = sg.Check(ctx, req)
	assert.Equal(t, permission.Deny, dec)
	assert.False(t, permission.Unrestricted(sg))
}
