package bashtool

import (
	"context"
	"strings"

	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/sandbox"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// bashSandboxProfile compiles the containment manifest for one bash run
// from the session's permission policy: the workspace (session cwd) plus
// the well-known caches, sensitive deny prefixes excluded, network per the
// policy toggle. Nil when sandboxing is off or the host has no backend —
// callers then run the command untouched and the permission gate keeps
// asking before risky commands.
func bashSandboxProfile(ctx context.Context) *sandbox.Profile {
	proj := project.GetDefaultProject()
	if proj == nil || proj.Config() == nil {
		return nil
	}
	perm := proj.Config().Permissions
	if sandboxSuppressed(ctx, perm) {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(perm.Sandbox)) {
	case "off", "false", "no":
		return nil
	}
	if !sandbox.Supported() {
		return nil
	}
	network := strings.EqualFold(strings.TrimSpace(perm.SandboxNetwork), "allow")
	workspace := ""
	if dir, err := tooldef.Cwd(ctx); err == nil {
		workspace = dir
	}
	p := sandbox.Compile(workspace, network, perm.SensitivePathDeny)
	return &p
}

// sandboxSuppressed reports whether OS-level containment should be skipped for
// this run. Two independent things can say so, and BOTH must be consulted or
// one UI becomes the hole:
//
//  1. The live gate. The executor attaches "yolo" to the context for every
//     permission.Unrestricted gate (a bare AllowAll from `a1 run --yolo`, an
//     armed session override, a policy carrying the allow-all flags), so a
//     switch made anywhere — shift-tab, the web footer, the settings row, a
//     persona's permission_mode override — reaches the exec side the moment
//     the gate changes. This is the path a session switch takes.
//  2. The loaded policy. Outside an executor there is no attached mode, so a
//     headless run, a background shell, or the script tool reading the config
//     directly must still see the flags. Policy.Unrestricted is the same
//     predicate the gate's own sandboxEnforcing uses, which is what keeps the
//     two sides from drifting.
//
// The live context wins when it carries a mode (it reflects the gate that
// actually gated this call); the config fills in the rest. What still fires
// under either is the regex deny list: it lives in the permission gate, which
// no allow-all short-circuit bypasses.
func sandboxSuppressed(ctx context.Context, perm permission.Policy) bool {
	if perm.Unrestricted() {
		return true
	}
	if live := tooldef.PermissionModeFrom(ctx); live != "" {
		return permission.Mode(live) == permission.ModeYolo
	}
	return false
}

// sandboxDeniedHint explains the failure mode a contained command hits:
// writes outside the allowed set die with EACCES, and a bare "Permission
// denied" reads like a mystery rather than a wall.
const sandboxDeniedHint = "\n(sandbox: the path is outside the allowed write set — work inside the workspace, or ask the user to widen permissions.sandbox in config.yaml)"

// appendSandboxHint adds the explanation when a sandboxed run shows
// permission-denied output.
func appendSandboxHint(out string, sandboxed bool) string {
	if !sandboxed || !strings.Contains(out, "Permission denied") {
		return out
	}
	return out + sandboxDeniedHint
}
