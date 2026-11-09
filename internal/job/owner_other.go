//go:build !linux

package job

import (
	"os"
	"syscall"
)

// currentOwner is the degraded fingerprint: pid only. Most restarts are
// still caught (the pid is gone, or soon belongs to nothing); pid reuse
// can fool it, but the failure mode is a missed sweep, not a wrong kill.
// Linux carries the full fingerprint; see owner_linux.go.
func currentOwner() ownerIdentity {
	return ownerIdentity{pid: os.Getpid()}
}

// alive reports whether a process with this pid exists (signal-0 probe).
func (o ownerIdentity) alive() bool {
	if o.pid <= 0 {
		return false
	}
	p, err := os.FindProcess(o.pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
