package job

import (
	"strconv"
	"strings"
)

// ownerIdentity fingerprints the process that owns a live job: pid plus,
// where the OS offers it, the process start time. The start time is what
// makes the fingerprint honest — pids are recycled, and a recycled pid
// would make a dead owner look alive (or another process's job look owned).
// Linux carries the start time in /proc/<pid>/stat; other platforms
// degrade to pid existence, which catches most restarts but can be fooled
// by pid reuse — a documented degradation, not a hidden one.
type ownerIdentity struct {
	pid       int
	startTime uint64 // clock ticks since boot (Linux); 0 = unknown
}

// String renders the persisted form: "pid:starttime" when the start time
// is known, bare "pid" otherwise. parseOwner is its inverse.
func (o ownerIdentity) String() string {
	if o.pid <= 0 {
		return ""
	}
	if o.startTime > 0 {
		return strconv.Itoa(o.pid) + ":" + strconv.FormatUint(o.startTime, 10)
	}
	return strconv.Itoa(o.pid)
}

// parseOwner decodes a persisted owner stamp. An empty or malformed stamp
// is the zero identity — treated as "no owner on record", never as alive.
func parseOwner(s string) ownerIdentity {
	pid, start, ok := strings.Cut(s, ":")
	if !ok {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return ownerIdentity{}
		}
		return ownerIdentity{pid: n}
	}
	n, err := strconv.Atoi(pid)
	if err != nil || n <= 0 {
		return ownerIdentity{}
	}
	st, _ := strconv.ParseUint(start, 10, 64)
	return ownerIdentity{pid: n, startTime: st}
}

// zero reports whether the identity was never set.
func (o ownerIdentity) zero() bool { return o.pid <= 0 }
