package job

import (
	"fmt"
	"time"
)

// The warden is the Manager's runtime conscience, and it runs three
// passes per tick:
//
//  1. Orphan sweep — leftover non-terminal jobs whose stamped owner is
//     dead get parked per the recovery policy. Construction-time recovery
//     (recoverStale) only sees the moment the Manager booted; a surface
//     that dies at 2am while another keeps running is only visible to a
//     sweep that keeps looking. Jobs whose owner is still alive are left
//     strictly alone — the TUI and a1 serve may coexist over one root.
//
//  2. Stall check — live jobs silent past the stall window fold to
//     StatusStalled (see stall.go).
//
//  3. Stale-delivery pass — outbox claims older than the claim-stale
//     horizon are released (their claimant died before acking; the row
//     re-presents), and rows nobody ever collected past the drop age are
//     dropped (the outbox does not grow without bound).
//
// Only the top-level Manager runs a warden: nested managers boot with
// RecoverIgnore precisely so they never sweep their parent's fleet.

const (
	defaultWardenTick = 15 * time.Second
	defaultDropAge    = 7 * 24 * time.Hour
)

func (m *Manager) startWarden() {
	m.wardenExit = make(chan struct{})
	go func() {
		defer close(m.wardenExit)
		t := time.NewTicker(m.wardenTick)
		defer t.Stop()
		for {
			select {
			case <-m.wardenDone:
				return
			case now := <-t.C:
				m.sweepOrphans(now)
				m.checkStalls(now)
				m.store.sweepDeliveries(m.claimStale, m.dropAge, now)
			}
		}
	}()
}

// stopWarden halts the sweep loop and waits for it to exit. Close calls it
// before cancelling jobs, so no force-finalize races the drain — and so the
// goroutine's disk touches cannot outlive the manager into a TempDir
// cleanup (the "unlinkat: directory not empty" shape). Safe to call when
// the warden never started (nested / RecoverIgnore managers).
func (m *Manager) stopWarden() {
	m.wardenStopOnce.Do(func() { close(m.wardenDone) })
	if m.wardenExit != nil {
		<-m.wardenExit
	}
}

// sweepOrphans parks non-terminal jobs whose owner is dead. A job live in
// this manager's map is ours and running; a job stamped with another live
// process belongs to that process; everything else non-terminal (dead
// owner, or no stamp at all — a pre-outbox leftover) is an orphan, and is
// parked per the recovery policy. Already-parked (pending) jobs are left
// alone: parking is idempotent for status, but re-parking would bump the
// attempt counter on every warden tick.
func (m *Manager) sweepOrphans(now time.Time) {
	ids, err := m.store.listIDs()
	if err != nil {
		return
	}
	for _, id := range ids {
		m.mu.Lock()
		_, live := m.jobs[id]
		m.mu.Unlock()
		if live {
			continue
		}
		meta, err := m.store.readMeta(id)
		if err != nil {
			continue
		}
		if meta.Status.Terminal() || meta.Status == StatusPending {
			continue
		}
		if meta.Owner != "" && meta.Owner != m.ownerStr && parseOwner(meta.Owner).alive() {
			continue // another live surface owns it
		}
		m.parkOrphan(meta)
	}
}

// parkOrphan folds one dead-owner job into the recovery policy. The
// MarkFailed variant publishes the delivery row before the status flip —
// a parent learning its job failed deserves the same one-shot notice as
// a parent learning it finished.
func (m *Manager) parkOrphan(meta Meta) {
	meta.Owner = "" // parked jobs have no live owner by construction
	if m.recovery == RecoverRequeue {
		meta.Status = StatusPending
		meta.Attempt++
		meta.Error = ""
		meta.FinishedAt = time.Time{}
		if err := m.store.writeMeta(meta); err != nil {
			m.reportStore("writeMeta", meta.ID, err)
			return
		}
		m.persistEvent(meta, fmt.Sprintf("orphaned: owner died; parked pending resume (attempt %d)", meta.Attempt))
		return
	}
	meta.Status = StatusFailed
	meta.FinishedAt = time.Now().UTC()
	meta.Error = "interrupted: owning process died before job finished"
	if meta.ParentID != "" {
		if err := m.store.publishDelivery(meta); err != nil {
			m.reportStore("publishDelivery", meta.ID, err)
		}
	}
	if err := m.store.writeMeta(meta); err != nil {
		m.reportStore("writeMeta", meta.ID, err)
		return
	}
	m.persistEvent(meta, "orphaned: owner died; marked failed")
}
