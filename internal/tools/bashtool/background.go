package bashtool

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/damien1141/a1/internal/sandbox"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// Background shells: long-running commands (dev servers, watchers, tunnels)
// started with mode "start" run detached from the tool call that launched
// them. The model polls output with mode "attach" (optionally blocking until
// exit or a wait budget), lists shells with mode "ps", and stops them with
// mode "kill". Output is a bounded rolling tail — a server logging for hours
// never grows memory.
//
// Background output is NOT evidence: starts and attaches carry no exit code
// at tool-result time, and the executor's evidence hook skips non-run modes
// (a still-running suite must never record a pass). Verification commands
// belong in the foreground, where the harness sees them complete.

// bgShell is one detached command.
type bgShell struct {
	id      string
	command string
	start   time.Time
	cancel  context.CancelFunc
	buf     *cappedBuffer
	exited  chan struct{}
	err     error // exit error; nil = clean exit
	done    atomic.Bool
}

var (
	bgMu     sync.Mutex
	bgShells = map[string]*bgShell{}
	bgSeq    int
)

// bgMaxRetained bounds how many exited shells stay listed (for their final
// output and exit code) before the oldest are pruned.
const bgMaxRetained = 20

// startBackground launches command detached from the calling tool context
// (which is canceled as soon as the tool result returns). The session cwd
// and shell config carry over; the shell runs until it exits or mode "kill".
func startBackground(ctx context.Context, command string) (string, error) {
	bgCtx, cancel := context.WithCancel(context.Background())
	// Carry the calling context's cwd into the detached context so the
	// shell runs where the model expects.
	if dir, err := tooldef.Cwd(ctx); err == nil && dir != "" {
		bgCtx = tooldef.WithCwd(bgCtx, dir)
	}
	c, err := buildShellCommand(bgCtx, command)
	if err != nil {
		cancel()
		return "", err
	}
	sh := &bgShell{
		command: command,
		start:   time.Now(),
		cancel:  cancel,
		buf:     newCappedBuffer(BashMaxCollectBytes),
		exited:  make(chan struct{}),
	}
	c.Stdout = sh.buf
	c.Stderr = sh.buf

	bgMu.Lock()
	bgSeq++
	sh.id = fmt.Sprintf("bg%d", bgSeq)
	bgShells[sh.id] = sh
	bgMu.Unlock()

	// Containment rides background shells too: a long-lived command is
	// exactly the one worth walling. The goroutine below still owns Wait.
	// startLocked already prefixes enforcement failures with "sandbox:" —
	// no re-wrap here (it used to print "sandbox: sandbox: …").
	if serr := sandbox.Start(c, bashSandboxProfile(bgCtx)); serr != nil {
		bgMu.Lock()
		delete(bgShells, sh.id)
		bgMu.Unlock()
		cancel()
		return "", serr
	}
	go func() {
		defer close(sh.exited)
		sh.err = c.Wait()
		sh.done.Store(true)
		bgPrune()
	}()
	return sh.id, nil
}

// bgPrune drops the oldest exited shells beyond the retention cap. Caller:
// the exiting shell's goroutine (no lock held).
func bgPrune() {
	bgMu.Lock()
	defer bgMu.Unlock()
	var exited []*bgShell
	for _, sh := range bgShells {
		if sh.done.Load() {
			exited = append(exited, sh)
		}
	}
	if len(exited) <= bgMaxRetained {
		return
	}
	sort.Slice(exited, func(i, j int) bool { return exited[i].start.Before(exited[j].start) })
	for _, sh := range exited[:len(exited)-bgMaxRetained] {
		delete(bgShells, sh.id)
	}
}

// bgStatus renders one shell's running/exit state.
func (sh *bgShell) bgStatus() string {
	if !sh.done.Load() {
		return "running"
	}
	if sh.err == nil {
		return "exited 0"
	}
	return "exited: " + sh.err.Error()
}

// listBackground renders the ps table.
func listBackground() string {
	bgMu.Lock()
	shells := make([]*bgShell, 0, len(bgShells))
	for _, sh := range bgShells {
		shells = append(shells, sh)
	}
	bgMu.Unlock()
	if len(shells) == 0 {
		return "no background shells"
	}
	sort.Slice(shells, func(i, j int) bool { return shells[i].id < shells[j].id })
	var b strings.Builder
	b.WriteString("background shells:\n")
	for _, sh := range shells {
		fmt.Fprintf(&b, "- %s\t%s\t%s\t%s\n", sh.id, sh.bgStatus(),
			time.Since(sh.start).Round(time.Second), sh.command)
	}
	return strings.TrimRight(b.String(), "\n")
}

// attachBackground returns a shell's current output tail and status. When
// the shell is still running and waitSeconds > 0, it blocks until the shell
// exits or the wait budget is spent — the "wait for the server to be ready"
// move.
func attachBackground(shellID string, waitSeconds int) (string, error) {
	bgMu.Lock()
	sh, ok := bgShells[shellID]
	bgMu.Unlock()
	if !ok {
		return "", fmt.Errorf("unknown background shell %q (see bash mode ps)", shellID)
	}
	if waitSeconds > 0 && !sh.done.Load() {
		timer := time.NewTimer(time.Duration(waitSeconds) * time.Second)
		defer timer.Stop()
		select {
		case <-sh.exited:
		case <-timer.C:
		}
	}
	out := sh.buf.String()
	if strings.TrimSpace(out) == "" {
		out = "(no output yet)"
	}
	if sh.buf.Truncated() {
		out = "[older output discarded]\n" + out
	}
	header := fmt.Sprintf("[%s] %s — %s", sh.id, sh.bgStatus(), sh.command)
	if !sh.done.Load() {
		header += "\n(attach again for more output)"
	}
	return header + "\n" + out, nil
}

// killBackground stops a shell's whole process tree and waits briefly for
// the exit to settle.
func killBackground(shellID string) (string, error) {
	bgMu.Lock()
	sh, ok := bgShells[shellID]
	bgMu.Unlock()
	if !ok {
		return "", fmt.Errorf("unknown background shell %q (see bash mode ps)", shellID)
	}
	if sh.done.Load() {
		return fmt.Sprintf("[%s] already %s", sh.id, sh.bgStatus()), nil
	}
	sh.cancel()
	select {
	case <-sh.exited:
	case <-time.After(shellWaitDelay + time.Second):
		// The WaitDelay hard-kill guarantees exit shortly after cancel;
		// this branch is a belt-and-suspenders timeout.
	}
	return fmt.Sprintf("[%s] killed — %s", sh.id, sh.command), nil
}

// CountBackground reports how many background shells are live (tests).
func CountBackground() int {
	bgMu.Lock()
	defer bgMu.Unlock()
	return len(bgShells)
}

// ResetBackground kills and forgets every shell and resets the id counter
// (tests).
func ResetBackground() {
	bgMu.Lock()
	shells := make([]*bgShell, 0, len(bgShells))
	for _, sh := range bgShells {
		shells = append(shells, sh)
	}
	bgShells = map[string]*bgShell{}
	bgSeq = 0
	bgMu.Unlock()
	for _, sh := range shells {
		sh.cancel()
		<-sh.exited
	}
}
