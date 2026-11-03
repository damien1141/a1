package bashtool

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/sandbox"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// useProjectWithPermissions installs a temp-HOME project whose config.yaml
// carries the given permissions block, so bashSandboxProfile's config reads
// are deterministic and no test depends on the developer's real ~/.a1.
// Cleanup resets the singleton so later tests in the package re-discover.
func useProjectWithPermissions(t *testing.T, permissionsYAML string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := filepath.Join(home, ".a1", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
	require.NoError(t, os.WriteFile(cfg, []byte("models:\n  - name: test-model\n    api_key: sk-test\npermissions:\n"+permissionsYAML), 0o644))
	proj, err := project.Discover("")
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())
	project.SetDefaultProject(proj)
	t.Cleanup(func() { project.SetDefaultProject(nil) })
}

// The mode resolution is a pure function of the live context mode and the
// config fallback: the executor-attached mode wins when present, the config
// file's mode answers otherwise, and only yolo suppresses.
func TestSandboxSuppressedByMode(t *testing.T) {
	cases := []struct {
		name       string
		ctxMode    string
		configMode permission.Mode
		suppressed bool
	}{
		{"live yolo wins over config interactive", "yolo", permission.ModeInteractive, true},
		{"live yolo wins over config autopilot", "yolo", permission.ModeAutopilot, true},
		{"config yolo when no live mode", "", permission.ModeYolo, true},
		{"live interactive overrides config yolo", "interactive", permission.ModeYolo, false},
		{"interactive stays contained", "", permission.ModeInteractive, false},
		{"autopilot stays contained", "", permission.ModeAutopilot, false},
		{"readonly stays contained (gate excludes it anyway)", "", permission.ModeReadonly, false},
		{"empty everything is contained", "", "", false},
		{"unknown live mode is not yolo", "yolo-mode", permission.ModeInteractive, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.ctxMode != "" {
				ctx = tooldef.WithPermissionMode(ctx, tc.ctxMode)
			}
			assert.Equal(t, tc.suppressed, sandboxSuppressedByMode(ctx, tc.configMode))
		})
	}
}

// The shift-tab case: the config file says one thing, the live mode (what
// the executor attached from the gate it consulted) says yolo. The profile
// must be nil — the exec side agrees with sandboxEnforcing on the gate
// side, or the gate would allow believing in walls the exec never raised.
func TestBashSandboxProfile_YoloModeOnContextSuppresses(t *testing.T) {
	useProjectWithPermissions(t, "  mode: interactive\n  sandbox: on\n")
	prof := bashSandboxProfile(tooldef.WithPermissionMode(t.Context(), string(permission.ModeYolo)))
	assert.Nil(t, prof, "a live yolo mode compiles no containment profile")
}

// The config-file case: a headless run (or any bash invocation outside an
// engine's executor) has no live mode on the context, so the config's
// yolo must suppress on its own.
func TestBashSandboxProfile_YoloModeInConfigSuppresses(t *testing.T) {
	useProjectWithPermissions(t, "  mode: yolo\n  sandbox: on\n")
	prof := bashSandboxProfile(t.Context())
	assert.Nil(t, prof, "config mode yolo compiles no containment profile")
}

// The regression guard: a mode check that suppresses everywhere, or a
// parse that maps every mode onto yolo, would turn this feature into
// "sandbox never enforces". Interactive with sandbox: on must still
// compile a profile on a host that supports a backend.
func TestBashSandboxProfile_InteractiveStillCompiles(t *testing.T) {
	useProjectWithPermissions(t, "  mode: interactive\n  sandbox: on\n")
	prof := bashSandboxProfile(t.Context())
	if !sandbox.Supported() {
		assert.Nil(t, prof, "no backend on this host: containment degrades to off")
		return
	}
	require.NotNil(t, prof, "interactive mode with sandbox: on must compile a profile")
}

// The explicit off switch stays independent of the mode logic: yolo or
// not, permissions.sandbox: off is off.
func TestBashSandboxProfile_OffStaysOff(t *testing.T) {
	useProjectWithPermissions(t, "  mode: interactive\n  sandbox: off\n")
	assert.Nil(t, bashSandboxProfile(t.Context()))
}
