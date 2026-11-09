package job

import (
	"fmt"
	"time"
)

// The stall monitor: a progress-token watchdog. A job whose runner has
// emitted nothing — no tool progress, no result — for StallWindow is
// wedged, and "running" on disk is a lie. The monitor folds it to
// StatusStalled so Wait / Cancel / List tell the truth and the outbox
// notifies the parent. The wedged goroutine itself may never return; it
// keeps its concurrency slot until process exit — the fold cannot free
// what only the runner can release. A runner that keeps emitting progress
// is alive by definition, however long it takes.

// defaultStallWindow is the no-progress silence that means "wedged".
const defaultStallWindow = 10 * time.Minute

// checkStalls folds live jobs whose last progress token is older than the
// stall window. Queued jobs are skipped — waiting for a slot is not
// wedging; the clock only runs once a job starts.
func (m *Manager) checkStalls(now time.Time) {
	if m.stallWin <= 0 {
		return // disabled
	}
	m.mu.Lock()
	var wedged []*liveJob
	for _, lj := range m.jobs {
		if lj.queued {
			continue
		}
		idle := time.Duration(now.UnixNano() - lj.progressAt.Load())
		if idle > m.stallWin {
			wedged = append(wedged, lj)
		}
	}
	m.mu.Unlock()
	for _, lj := range wedged {
		idle := time.Duration(now.UnixNano() - lj.progressAt.Load())
		m.forceFinalizeStalled(lj, idle)
	}
}

// forceFinalizeStalled owns the terminal fold for a wedged job. The
// terminalClaimed CAS is the single arbiter: if run() is already folding
// its own terminal state (the runner just returned on its own), the
// monitor stands down — one writer per fold, always. Winning the CAS
// means the monitor writes the result placeholder and the delivery row
// BEFORE the status flip (commit-point ordering: a Wait or collector
// that sees StatusStalled must also see the result and the row), then
// cancels the run context so a listening runner can unwind. run()
// re-checks the flag after the runner returns and defers to this fold.
func (m *Manager) forceFinalizeStalled(lj *liveJob, idle time.Duration) {
	if !lj.terminalClaimed.CompareAndSwap(false, true) {
		return // run() owns the fold
	}
	lj.cancel() // best effort: unwedge a runner that still listens

	m.mu.Lock()
	meta := lj.meta
	// Persist first, unregister last: a Wait polling between the two sees
	// the job in-map with terminal meta (or gone-and-terminal), never
	// gone-and-non-terminal.
	meta.Status = StatusStalled
	meta.FinishedAt = time.Now().UTC()
	meta.Error = fmt.Sprintf("stalled: no progress for %s", idle.Round(time.Second))
	lj.meta = meta
	m.mu.Unlock()

	if err := m.store.writeResult(meta, "# Stalled\n\n"+meta.Error+"\n"); err != nil {
		m.reportStore("writeResult", meta.ID, err)
	}
	if meta.ParentID != "" {
		if err := m.store.publishDelivery(meta); err != nil {
			m.reportStore("publishDelivery", meta.ID, err)
		}
	}
	m.persistMeta(meta)
	m.persistEvent(meta, "stalled: "+meta.Error)

	// Unblock Waiters parked on done (closeOnce makes this idempotent
	// with run()'s own defer), then unregister. The goroutine itself may
	// still be wedged — that is the runner's problem, not the ledger's.
	lj.closeDone()
	m.mu.Lock()
	delete(m.jobs, meta.ID)
	m.mu.Unlock()
}
