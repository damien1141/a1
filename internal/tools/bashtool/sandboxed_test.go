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
	// The seam itself must hold for anything below to mean anything:
	// GetDefaultProject hands bashSandboxProfile the project above with
	// its config loaded. It once re-discovered a config-less twin
	// instead — every profile came back nil, the nil-asserting yolo/off
	// cases stayed green while testing nothing, and only a host with a
	// sandbox backend (where the guard test demands a real profile)
	// could notice.
	require.NotNil(t, project.GetDefaultProject().Config(),
		"default-project seam must expose the installed project's config")
	t.Cleanup(func() { project.SetDefaultProject(nil) })
}

// The suppression resolution folds two inputs: the policy's own promises (its
// mode and its allow-all flags, through Policy.Unrestricted) and the live mode
// the executor attached from the gate it consulted. Either can say "no walls",
// and the flags are why this is not a mode check: `dangerously_allow_all`
// leaves Mode at interactive while the gate short-circuits every rule, so a
// mode-only predicate walled in the exact runs that never ask.
func TestSandboxSuppressed(t *testing.T) {
	cases := []struct {
		name       string
		ctxMode    string
		policy     permission.Policy
		suppressed bool
	}{
		{"live yolo wins over interactive policy", "yolo", permission.Policy{Mode: permission.ModeInteractive}, true},
		{"live yolo wins over autopilot policy", "yolo", permission.Policy{Mode: permission.ModeAutopilot}, true},
		{"policy yolo when no live mode", "", permission.Policy{Mode: permission.ModeYolo}, true},
		{"live interactive overrides policy yolo", "interactive", permission.Policy{Mode: permission.ModeYolo}, true},
		{"interactive stays contained", "", permission.Policy{Mode: permission.ModeInteractive}, false},
		{"autopilot stays contained", "", permission.Policy{Mode: permission.ModeAutopilot}, false},
		{"readonly stays contained (gate excludes it anyway)", "", permission.Policy{Mode: permission.ModeReadonly}, false},
		{"empty everything is contained", "", permission.Policy{}, false},
		{"unknown live mode is not yolo", "yolo-mode", permission.Policy{Mode: permission.ModeInteractive}, false},
		// The allow-all flags: no mode says yolo, yet approval is gone.
		{"dangerously_allow_all suppresses", "", permission.Policy{Mode: permission.ModeInteractive, DangerouslyAllowAll: true}, true},
		{"allow_all_session suppresses", "", permission.Policy{Mode: permission.ModeInteractive, AllowAllSession: true}, true},
		{"flags suppress even with a live contained mode", "interactive", permission.Policy{Mode: permission.ModeInteractive, DangerouslyAllowAll: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.ctxMode != "" {
				ctx = tooldef.WithPermissionMode(ctx, tc.ctxMode)
			}
			assert.Equal(t, tc.suppressed, sandboxSuppressed(ctx, tc.policy))
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

// The flags end-to-end: `dangerously_allow_all: true` in config.yaml must
// compile no bash profile. Before this, the gate short-circuited every rule
// (Allow to everything, no prompt) while the exec side still walled the
// command in — the user's "yolo doesn't disable the sandbox" report.
func TestBashSandboxProfile_AllowAllFlagsSuppress(t *testing.T) {
	// dangerously_allow_all is the flag config.yaml can actually carry;
	// allow_all_session is runtime-only (set programmatically for a single
	// session), so it is covered by TestSandboxSuppressed's policy cases
	// rather than through a YAML key that does not exist.
	useProjectWithPermissions(t, "  mode: interactive\n  sandbox: on\n  dangerously_allow_all: true\n")
	assert.Nil(t, bashSandboxProfile(t.Context()),
		"dangerously_allow_all compiles no containment profile")
}

// The same guarantee through a live context mode, which is how a session
// switch made in any UI reaches the exec side.
func TestBashSandboxProfile_LiveYoloSuppresses(t *testing.T) {
	useProjectWithPermissions(t, "  mode: interactive\n  sandbox: on\n")
	ctx := tooldef.WithPermissionMode(t.Context(), string(permission.ModeYolo))
	assert.Nil(t, bashSandboxProfile(ctx))
}
