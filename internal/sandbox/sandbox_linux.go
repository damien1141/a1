//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// supported probes Landlock availability: the kernel returns its ABI
// version from landlock_create_ruleset(NULL, 0, VERSION), or ENOSYS /
// EOPNOTSUPP when the syscall or feature is missing (containers without it
// stay honest rather than pretending).
func supported() bool {
	v, err := abiVersion()
	return err == nil && v >= 1
}

func abiVersion() (int, error) {
	v, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET, 0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0, errno
	}
	return int(v), nil
}

// FS access bits handled by the ruleset, masked to the running kernel's ABI
// so an older kernel never rejects a newer bit it does not know.
func handledFS(abi int) uint64 {
	bits := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE |
		unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR |
		unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG |
		unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM)
	if abi >= 2 {
		bits |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		bits |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	return bits
}

func readOnlyBits(abi int) uint64 {
	bits := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE |
		unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_DIR)
	if abi >= 2 {
		bits |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	return bits
}

func readWriteBits(abi int) uint64 {
	return handledFS(abi) // the RW set is "every handled access", nothing narrower
}

// startLocked applies the ruleset on a dedicated locked thread and starts
// the command there. Landlock restricts the calling thread; the fork
// inherits the ruleset, and the thread dies (still restricted) when the
// goroutine returns — the harness's other threads never notice.
func startLocked(cmd *exec.Cmd, p *Profile) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := applyLandlock(p); err != nil {
			// Restriction never landed, so the thread is still clean and
			// may return to the scheduler.
			runtime.UnlockOSThread()
			done <- fmt.Errorf("sandbox: %w", err)
			return
		}
		// No UnlockOSThread here, on purpose: after restrict-self the
		// ruleset is bound to this thread until it dies. Returning from
		// the goroutine destroys it; unlocking would leak a restricted
		// thread into the runtime's pool.
		done <- cmd.Start()
	}()
	return <-done
}

// applyLandlock builds and enforces the ruleset on the current (locked)
// thread. Read+execute everywhere, writes only under the profile's RW set,
// TCP connect/bind denied wholesale when Network is false (handled access
// with no allow-rule denies by construction).
func applyLandlock(p *Profile) error {
	abi, err := abiVersion()
	if err != nil {
		return err
	}
	attr := unix.LandlockRulesetAttr{Handled_access_fs: handledFS(abi)}
	if !p.Network && abi >= 4 {
		attr.Handled_access_net = unix.LANDLOCK_ACCESS_NET_BIND_TCP | unix.LANDLOCK_ACCESS_NET_CONNECT_TCP
	}
	fd, _, errno := unix.Syscall(
		unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)),
		unsafe.Sizeof(attr),
		0,
	)
	if errno != 0 {
		return errno
	}
	defer unix.Close(int(fd))

	addRule := func(path string, access uint64) error {
		f, err := os.OpenFile(path, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		defer f.Close()
		rule := unix.LandlockPathBeneathAttr{
			Allowed_access: access,
			Parent_fd:      int32(f.Fd()),
		}
		// landlock_add_rule(ruleset_fd, rule_type, rule_attr, flags) takes
		// four arguments; the previous 3-arg Syscall put the rule type in
		// the ruleset-fd slot, so the kernel read fd 1 (stdout) and every
		// rule add failed with EBADFD ("file descriptor in bad state").
		if _, _, errno := unix.Syscall6(
			unix.SYS_LANDLOCK_ADD_RULE,
			fd, // ruleset fd from landlock_create_ruleset
			uintptr(unix.LANDLOCK_RULE_PATH_BENEATH),
			uintptr(unsafe.Pointer(&rule)),
			0, 0, 0, // flags must be 0
		); errno != 0 {
			return errno
		}
		return nil
	}

	// Read + execute everywhere; the RW rules layer write access on top.
	if err := addRule("/", readOnlyBits(abi)); err != nil {
		return err
	}
	for _, dir := range p.ReadWrite {
		if err := addRule(dir, readWriteBits(abi)); err != nil {
			return err
		}
	}
	// /dev/null writes: exec.Cmd opens it for nil stdin/stdout, and enough
	// shell idioms redirect there that denying it breaks harmless commands.
	if err := addRule(os.DevNull, unix.LANDLOCK_ACCESS_FS_WRITE_FILE|unix.LANDLOCK_ACCESS_FS_READ_FILE); err != nil {
		return err
	}

	// restrict-self requires no_new_privs so an unprivileged process may
	// drop its own rights (and the child cannot gain any back via setuid).
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	if _, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return errno
	}
	return nil
}
