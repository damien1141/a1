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
        if sandboxSuppressedByMode(ctx, perm.Mode) {
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

// sandboxSuppressedByMode reports whether the effective permission mode
// disables OS-level containment for this run. Yolo is the one mode that
// does, and it does it wholesale: containment exists to substitute for
// human approval, and yolo withdraws approval entirely — walls that stand
// in for a prompt nobody is sending anymore just get in the way (writes to
// system paths a binary-inspection workflow needs, no_new_privs on the
// child, network egress cut). The live mode from the executor's context
// wins over the config file's mode, so a shift-tab cycle or a web dropdown
// flip is honored mid-session without a config rewrite. What still fires
// is the regex deny list: it lives in the permission gate, which yolo
// deliberately does not bypass.
func sandboxSuppressedByMode(ctx context.Context, configMode permission.Mode) bool {
        mode := configMode
        if live := tooldef.PermissionModeFrom(ctx); live != "" {
                mode = permission.Mode(live)
        }
        return mode == permission.ModeYolo
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
