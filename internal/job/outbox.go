package job

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The durable outbox: one row per finished job with a parent session to
// notify, presented to that parent at its next turn boundary. The row
// survives restarts — the parent learns about a finished job even if it
// (or the surface running it) was down when the job landed — and the
// claim files keep two live managers over the same root from presenting
// one result twice. Rows whose parent never comes back are dropped by the
// warden's aged pass, so the outbox does not grow without bound.

const (
	// outboxDirName sits under the jobs root alongside job dirs; the
	// store's listIDs skips it by name.
	outboxDirName = "outbox"
	deliveryExt   = ".delivery"
	claimExt      = ".claim"
)

// deliveryRow is one pending result notice, persisted as
// <root>/outbox/<jobID>.delivery. The summary itself stays in the job's
// result.md; the row carries the addressing and the headline.
type deliveryRow struct {
	JobID       string    `json:"job_id"`
	ParentID    string    `json:"parent_id"`
	Description string    `json:"description,omitempty"`
	Role        string    `json:"role,omitempty"`
	Status      Status    `json:"status"`
	Error       string    `json:"error,omitempty"`
	FinishedAt  time.Time `json:"finished_at"`
}

// Delivery is one collected notice for a parent session: the row plus the
// result summary, read once at collection.
type Delivery struct {
	JobID       string
	Description string
	Role        string
	Status      Status
	Error       string
	Summary     string
	FinishedAt  time.Time
}

func (s *store) outboxPath() string { return filepath.Join(s.root, outboxDirName) }

// publishDelivery writes (or rewrites) the row for a finished job. Jobs
// without a parent to notify publish nothing — schedule-fired work is
// pulled by its own poll loop, not pushed at a turn boundary. Callers
// land this BEFORE the terminal status flip that makes it visible, so a
// concurrent collector can never observe a terminal job with no row.
func (s *store) publishDelivery(meta Meta) error {
	if meta.ParentID == "" {
		return nil
	}
	row := deliveryRow{
		JobID:       meta.ID,
		ParentID:    meta.ParentID,
		Description: meta.Description,
		Role:        string(meta.Role),
		Status:      meta.Status,
		Error:       meta.Error,
		FinishedAt:  meta.FinishedAt,
	}
	data, err := json.MarshalIndent(row, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.outboxPath(), 0o755); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.outboxPath(), meta.ID+deliveryExt), data)
}

// listDeliveries reads every row addressed to parentID ("" = all rows).
func (s *store) listDeliveries(parentID string) ([]deliveryRow, error) {
	entries, err := os.ReadDir(s.outboxPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []deliveryRow
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, deliveryExt) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.outboxPath(), name))
		if err != nil {
			continue // racing an ack — the row is gone, not broken
		}
		var row deliveryRow
		if json.Unmarshal(data, &row) != nil || row.JobID == "" {
			continue
		}
		if parentID != "" && row.ParentID != parentID {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// claimDelivery takes the row's mutex: O_EXCL on a FIXED claim file name.
// The fixed name is the whole point — a unique name per attempt never
// blocks anyone, and two collectors would both "win". A claim older than
// stale is a dead claimant's leftover: steal it and retry once.
func (s *store) claimDelivery(row deliveryRow, stale time.Duration, now time.Time) bool {
	claim := filepath.Join(s.outboxPath(), row.JobID+claimExt)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(claim, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return true
		}
		if !errors.Is(err, os.ErrExist) {
			return false
		}
		info, statErr := os.Stat(claim)
		if statErr != nil {
			continue // claim vanished — retry the create
		}
		if now.Sub(info.ModTime()) <= stale {
			return false // a live claimant holds it
		}
		_ = os.Remove(claim) // stale — steal and retry
	}
	return false
}

// ackDelivery removes the row and its claim: the parent has the result.
// A missing file is not an error — the row may already be collected, or
// the job never had one.
func (s *store) ackDelivery(jobID string) {
	_ = os.Remove(filepath.Join(s.outboxPath(), jobID+claimExt))
	_ = os.Remove(filepath.Join(s.outboxPath(), jobID+deliveryExt))
}

// releaseDelivery drops the claim but keeps the row, so a later turn
// re-presents it.
func (s *store) releaseDelivery(jobID string) {
	_ = os.Remove(filepath.Join(s.outboxPath(), jobID+claimExt))
}

// sweepDeliveries is the warden's stale-delivery pass: claims older than
// stale belong to claimants that died before acking — release them for
// re-presentation — and rows older than dropAge will never be collected
// (their parent is gone) — drop them so the outbox stays bounded.
func (s *store) sweepDeliveries(stale, dropAge time.Duration, now time.Time) {
	entries, err := os.ReadDir(s.outboxPath())
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		age := now.Sub(info.ModTime())
		switch {
		case strings.HasSuffix(name, claimExt):
			if age > stale {
				_ = os.Remove(filepath.Join(s.outboxPath(), name))
			}
		case strings.HasSuffix(name, deliveryExt):
			if age > dropAge {
				s.ackDelivery(strings.TrimSuffix(name, deliveryExt))
			}
		}
	}
}
