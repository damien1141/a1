//go:build !linux && !darwin

package sandbox

import "os/exec"

// supported reports that this platform has no sandbox backend; Start
// degrades to a plain exec and the permission gate keeps asking.
func supported() bool { return false }

func startLocked(cmd *exec.Cmd, _ *Profile) error { return cmd.Start() }
