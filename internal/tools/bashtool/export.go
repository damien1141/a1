package bashtool

import (
	"context"
	"syscall"
	"time"

	"github.com/damien1141/a1/internal/sandbox"
)

// Process-group and kill-tree plumbing shared with the script tool
// (internal/tools/scripttool): its python child gets the same lifecycle
// guarantees as a bash child — its own process group so a timeout kills
// the whole tree (python may have spawned compilers, test binaries, ...),
// never just the interpreter.
//
// The platform halves live in process_unix.go / process_windows.go; these
// wrappers are the version-agnostic surface other tool packages may use.

// ProcessGroupAttr returns the SysProcAttr that puts the child in its own
// process group (POSIX) or hides its console window (Windows).
func ProcessGroupAttr() *syscall.SysProcAttr { return processGroupAttr() }

// KillProcessTree kills pid and all descendants, falling back to the leader
// alone if the group is already gone.
func KillProcessTree(pid int) error { return killProcessTree(pid) }

// ShellWaitDelay is how long Cmd.Wait waits after Cancel for the tree to
// exit before the runtime's own hard kill lands.
func ShellWaitDelay() time.Duration { return shellWaitDelay }

// ShellEnv returns the environment for shell children: the parent env with
// a1's bin dir prepended to PATH. The script tool's python child gets the
// same environment a bash child would.
func ShellEnv() []string { return shellEnv() }

// SandboxProfile compiles the containment manifest for a non-bash child
// from the live permission policy — the script tool's python runs under the
// same walls as bash, because arbitrary python is arbitrary code execution.
func SandboxProfile(ctx context.Context) *sandbox.Profile { return bashSandboxProfile(ctx) }
