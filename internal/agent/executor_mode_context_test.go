package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// modeCapturingExecutor builds an executor whose single tool records the
// permission mode the executor attached to the call's context.
func modeCapturingExecutor(t *testing.T, gate permission.Gate, seen *atomic.Value) *Executor {
	t.Helper()
	reg := tools.Registry{
		"read": {
			Definition: llm.ToolDefinition{
				Name:        "read",
				Description: "records the live permission mode",
				Params:      &llm.FunctionParameters{Type: "object"},
			},
			Run: func(ctx context.Context, _ json.RawMessage) (tools.Result, error) {
				seen.Store(tooldef.PermissionModeFrom(ctx))
				return tools.Result{Content: "ok"}, nil
			},
		},
	}
	return NewExecutor(reg, gate, nil, nil, nil)
}

func runProbe(t *testing.T, ex *Executor) {
	t.Helper()
	_, _, _ = ex.Run(t.Context(), []llm.ToolCall{{
		ID:       "c1",
		Function: llm.Function{Name: "read", Arguments: `{}`},
	}}, func(session.ToolData) bool { return true })
}

// The exec side of the yolo promise. "Yolo disables the bash sandbox" was only
// half-true: the GATE dropped containment for a literal Mode=yolo, but the
// tool that actually runs the command learns the mode from the context the
// executor attaches — and it attached only what ModeOf reported. `a1 run
// --yolo` builds a bare AllowAll (ModeOf → ""), so bash kept compiling a
// containment profile for a run whose entire point is that there are no walls.
// These pins make the two sides answer the same question with one predicate.
func TestExecutorAttachesYoloForUnrestrictedGates_Positive(t *testing.T) {
	cases := map[string]permission.Gate{
		// `a1 run --yolo` / HeadlessGate(DangerouslyAllowAll) — the shape that
		// regressed: no mode to report, yet unrestricted.
		"bare AllowAll": permission.AllowAll{},
	}

	// An armed session allow-all override wrapping a plainly-gated inner gate:
	// the override is what gated the call, so it is what the tool must see.
	var armed atomic.Bool
	armed.Store(true)
	inner, err := permission.NewGate(permission.Policy{Mode: permission.ModeInteractive}, t.TempDir())
	require.NoError(t, err)
	cases["armed session override"] = &permission.SessionAllowGate{Inner: inner, Enabled: &armed}

	for name, gate := range cases {
		t.Run(name, func(t *testing.T) {
			var seen atomic.Value
			ex := modeCapturingExecutor(t, gate, &seen)
			runProbe(t, ex)
			require.NotNil(t, seen.Load())
			assert.Equal(t, string(permission.ModeYolo), seen.Load().(string),
				"an unrestricted gate must hand the tool yolo, or bash keeps its walls up")
		})
	}
}

// The other half: an ordinary gate still reports its own mode, so broad
// "yolo" attachment cannot leak into contained sessions.
func TestExecutorAttachesRealModeForRestrictedGates_Negative(t *testing.T) {
	gate, err := permission.NewGate(permission.Policy{Mode: permission.ModeReadonly}, t.TempDir())
	require.NoError(t, err)

	var seen atomic.Value
	ex := modeCapturingExecutor(t, gate, &seen)
	runProbe(t, ex)

	require.NotNil(t, seen.Load())
	assert.Equal(t, string(permission.ModeReadonly), seen.Load().(string),
		"a restricted gate must report its own mode, never yolo")
}

// A DISARMED override defers to its inner gate — Unrestricted says so, and the
// context must agree: the tool sees the inner mode, not yolo.
func TestExecutorDisarmedOverrideKeepsInnerMode_Differential(t *testing.T) {
	inner, err := permission.NewGate(permission.Policy{Mode: permission.ModeAutopilot}, t.TempDir())
	require.NoError(t, err)
	var disarmed atomic.Bool
	gate := &permission.SessionAllowGate{Inner: inner, Enabled: &disarmed}

	var seen atomic.Value
	ex := modeCapturingExecutor(t, gate, &seen)
	runProbe(t, ex)

	require.NotNil(t, seen.Load())
	got := seen.Load().(string)
	// oracle: whatever permission.Unrestricted says about this gate decides
	// which branch the executor took.
	if permission.Unrestricted(gate) {
		assert.Equal(t, string(permission.ModeYolo), got)
	} else {
		assert.Equal(t, string(permission.ModeAutopilot), got)
	}
	assert.Equal(t, string(permission.ModeAutopilot), got, "a disarmed override must not lift containment")
}
