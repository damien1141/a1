//go:build linux

package job

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
)

// currentOwner fingerprints this process: pid + start time from
// /proc/<pid>/stat (field 22). The Manager reads it once and stamps it
// onto every job it takes live, so a later manager over the same root can
// tell "the owning process still runs" from "the pid was recycled to
// someone else entirely".
func currentOwner() ownerIdentity {
	pid := os.Getpid()
	st, err := procStartTime(pid)
	if err != nil {
		return ownerIdentity{pid: pid}
	}
	return ownerIdentity{pid: pid, startTime: st}
}

// alive reports whether the stamped process is still that process. A
// missing /proc entry means the owner is dead; a start-time mismatch
// means the pid was recycled and a different process now wears it — dead
// either way from the job's perspective.
func (o ownerIdentity) alive() bool {
	if o.pid <= 0 {
		return false
	}
	st, err := procStartTime(o.pid)
	if err != nil {
		return false // no such process
	}
	if o.startTime > 0 && st != o.startTime {
		return false // pid recycled
	}
	return true
}

// procStartTime reads field 22 (starttime, clock ticks since boot) from
// /proc/<pid>/stat. The comm field can contain spaces and parentheses, so
// the parse anchors on the LAST ')' — everything after it is fields 3+.
func procStartTime(pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)) //nolint:gosec // pid is an int, not caller input
	if err != nil {
		return 0, err
	}
	rest := data[bytes.LastIndexByte(data, ')')+1:]
	fields := bytes.Fields(rest)
	// fields[0] is field 3 (state); field 22 is index 19.
	if len(fields) < 20 {
		return 0, fmt.Errorf("job: short /proc/%d/stat", pid)
	}
	return strconv.ParseUint(string(fields[19]), 10, 64)
}
