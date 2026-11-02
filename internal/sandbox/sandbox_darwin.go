//go:build darwin

package sandbox

import (
	"fmt"
	"os/exec"
)

// supported reports whether the seatbelt executor exists. Apple deprecates
// sandbox-exec but still ships it; when it disappears, this returns false
// and the permission gate goes back to asking.
func supported() bool {
	_, err := exec.LookPath("sandbox-exec")
	return err == nil
}

// startLocked wraps the command in sandbox-exec with the compiled profile.
func startLocked(cmd *exec.Cmd, p *Profile) error {
	bin, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return fmt.Errorf("sandbox: sandbox-exec not found: %w", err)
	}
	wrapped := append([]string{bin, "-p", seatbeltProfile(p), "--"}, cmd.Args...)
	cmd.Path = bin
	cmd.Args = wrapped
	return cmd.Start()
}
