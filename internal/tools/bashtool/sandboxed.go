package bashtool

import (
	"context"
	"strings"

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
