package job

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Options configures a [Manager].
type Options struct {
	Root   string // required: jobs directory
	Runner Runner // required
	// DB mirrors every persisted meta into the vault's jobs table (Bet 01):
	// status transitions upsert rows, spawn-rejects delete them. Nil = files
	// only (the degraded mode when the vault failed to boot).
	DB            *sql.DB
	MaxConcurrent int // default 4; full slots queue FIFO instead of failing
	MaxDepth      int // default 3 (worker children may nest; see agent.EngineRunner)
	// MaxQueue bounds the FIFO wait queue (default 64). When the queue is
	// also full, Spawn returns [ErrBusy] — the hard backpressure edge.
	MaxQueue int
	// Recovery selects the restart policy for leftover non-terminal jobs.
	// The agent glue (TUI, a1 run, a1 serve) passes RecoverRequeue.
	Recovery RecoveryMode
	// OnStoreError is called when a disk write fails after the job is live.
	// Create/Spawn still return the create error directly.
	OnStoreError func(op, jobID string, err error)
}

// defaultMaxQueue is the FIFO depth before Spawn fails with ErrBusy.
const defaultMaxQueue = 64

// Manager owns in-process job lifecycles and a disk store.
type Manager struct {
	store         *store
	runner        Runner
	recovery      RecoveryMode
	maxConcurrent int
	maxDepth      int
	maxQueue      int
	onStoreError  func(op, jobID string, err error)

	mu     sync.Mutex
	closed bool
	slots  chan struct{}
	// waiting is the bounded FIFO of queued jobs (Bet 04 backpressure):
	// spawn requests that arrive while every concurrency slot is busy join
	// this queue and start as slots drain, instead of failing with ErrBusy.
	waiting chan *liveJob
	jobs    map[string]*liveJob
	subs    []*subscriber
}

type liveJob struct {
	meta            Meta
	parentToolUseID string
	cancel          context.CancelFunc
	done            chan struct{}
	// timeout is the SpawnRequest timeout, applied when the run actually
	// starts — a queued job does not burn its wall-clock budget in the queue.
	timeout time.Duration
	// contract is the fleet task-contract seed forwarded to RunEnv.
	contract []string
	// queued (guarded by m.mu) marks a job sitting in waiting; cancelled
	// (atomic) lets retire skip queue entries finalized by Cancel/Close.
	queued    bool
	cancelled atomic.Bool
}

// subscriber is one progress channel. closed is guarded by m.mu: emitProgress
// sends while holding m.mu, and cancel/Close mark closed and close the channel
// under the same lock, so a send can never race a channel close.
type subscriber struct {
	ch     chan Progress
	closed bool
}

// New creates a Manager. Root and Runner are required.
//
// By default ([RecoverMarkFailed]), leftover starting/running jobs on disk are
// marked failed so a process restart does not leave Wait/Cancel zombies.
func New(opts Options) (*Manager, error) {
	if opts.Runner == nil {
		return nil, fmt.Errorf("%w: Runner is required", ErrInvalid)
	}
	st, err := newStore(opts.Root, opts.DB)
	if err != nil {
		return nil, err
	}
	maxC := opts.MaxConcurrent
	if maxC <= 0 {
		maxC = 4
	}
	maxD := opts.MaxDepth
	if maxD <= 0 {
		maxD = 3
	}
	maxQ := opts.MaxQueue
	if maxQ <= 0 {
		maxQ = defaultMaxQueue
	}
	m := &Manager{
		store:         st,
		runner:        opts.Runner,
		recovery:      opts.Recovery,
		maxConcurrent: maxC,
		maxDepth:      maxD,
		maxQueue:      maxQ,
		onStoreError:  opts.OnStoreError,
		slots:         make(chan struct{}, maxC),
		waiting:       make(chan *liveJob, maxQ),
		jobs:          make(map[string]*liveJob),
	}
	if opts.Recovery != RecoverIgnore {
		if err := m.recoverStale(); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// recoverStale folds leftover non-terminal jobs into the restart policy:
// MarkFailed force-fails them (the pre-Clock behavior, kept for embedders
// that prefer no zombies), Requeue parks them as pending — attempt bumped,
// resume flag ready — for [Manager.Resume] / [ResumePending] to pick up.
func (m *Manager) recoverStale() error {
	ids, err := m.store.listIDs()
	if err != nil {
		return err
	}
	requeue := m.recovery == RecoverRequeue
	now := time.Now().UTC()
	for _, id := range ids {
		meta, err := m.store.readMeta(id)
		if err != nil {
			m.reportStore("readMeta", id, err)
			continue
		}
		if meta.Status.Terminal() {
			continue
		}
		if requeue {
			meta.Status = StatusPending
			meta.Attempt++
			meta.Error = ""
			meta.FinishedAt = time.Time{}
			if err := m.store.writeMeta(meta); err != nil {
				return fmt.Errorf("job: recover %s: %w", id, err)
			}
			if err := m.store.appendEvent(meta, fmt.Sprintf("requeued: pending resume after restart (attempt %d)", meta.Attempt)); err != nil {
				m.reportStore("appendEvent", id, err)
			}
			continue
		}
		meta.Status = StatusFailed
		meta.FinishedAt = now
		meta.Error = "interrupted: process restarted before job finished"
		if err := m.store.writeMeta(meta); err != nil {
			return fmt.Errorf("job: recover %s: %w", id, err)
		}
		if err := m.store.appendEvent(meta, "recovered: marked failed after restart"); err != nil {
			m.reportStore("appendEvent", id, err)
		}
	}
	return nil
}

func (m *Manager) reportStore(op, jobID string, err error) {
	if err == nil || m.onStoreError == nil {
		return
	}
	m.onStoreError(op, jobID, err)
}

func (m *Manager) persistMeta(meta Meta) {
	if err := m.store.writeMeta(meta); err != nil {
		m.reportStore("writeMeta", meta.ID, err)
	}
}

func (m *Manager) persistEvent(meta Meta, msg string) {
	if err := m.store.appendEvent(meta, msg); err != nil {
		m.reportStore("appendEvent", meta.ID, err)
	}
}

// Spawn starts a job asynchronously. The returned Info reflects starting
// (or queued) state.
//
// Concurrency: when MaxConcurrent slots are full the job joins a bounded
// FIFO wait queue and starts as slots drain; only a full queue returns
// [ErrBusy]. Depth: if req.Depth >= MaxDepth, returns [ErrDepth].
func (m *Manager) Spawn(ctx context.Context, req SpawnRequest) (Info, error) {
	if err := req.validate(); err != nil {
		return Info{}, err
	}
	if req.Depth >= m.maxDepth {
		return Info{}, fmt.Errorf("%w: depth %d >= max %d", ErrDepth, req.Depth, m.maxDepth)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Info{}, ErrClosed
	}
	id, err := newJobID()
	if err != nil {
		m.mu.Unlock()
		return Info{}, err
	}
	now := time.Now().UTC()
	timeoutSec := 0
	if req.Timeout > 0 {
		timeoutSec = int(req.Timeout.Seconds())
	}
	meta := Meta{
		ID:          id,
		ParentID:    req.ParentID,
		ParentDepth: req.Depth,
		Role:        NormalizeRole(string(req.Role)),
		Prompt:      req.Prompt,
		Description: req.Description,
		WorkDir:     req.WorkDir,
		WarmFrom:    req.WarmFrom,
		Kind:        req.Kind,
		ScheduleID:  req.ScheduleID,
		TimeoutSec:  timeoutSec,
		Contract:    req.Contract,
		Status:      StatusStarting,
		CreatedAt:   now,
	}
	meta, err = m.store.create(meta)
	if err != nil {
		m.mu.Unlock()
		_ = m.store.remove(id)
		return Info{}, err
	}

	// Fast path: a concurrency slot is free — start now.
	select {
	case m.slots <- struct{}{}:
		// Parent ctx cancel does not kill the job (jobs outlive a single tool call);
		// callers use Cancel. Still respect if Spawn itself is aborted before start.
		if err := ctx.Err(); err != nil {
			m.mu.Unlock()
			<-m.slots
			meta.Status = StatusCancelled
			meta.FinishedAt = time.Now().UTC()
			meta.Error = err.Error()
			m.persistMeta(meta)
			return Info{}, err
		}
		lj := &liveJob{
			meta:            meta,
			parentToolUseID: req.ParentToolUseID,
			done:            make(chan struct{}),
			timeout:         req.Timeout,
			contract:        req.Contract,
		}
		runCtx, cancel := runContextFor(lj.timeout)
		lj.cancel = cancel
		m.jobs[id] = lj
		m.mu.Unlock()
		go m.run(runCtx, lj)
		return Info{Meta: meta}, nil
	default:
	}

	// Slow path: every slot is busy — join the bounded FIFO wait queue
	// (Bet 04 backpressure) and start when a slot drains.
	lj := &liveJob{
		meta:            meta,
		parentToolUseID: req.ParentToolUseID,
		done:            make(chan struct{}),
		timeout:         req.Timeout,
		contract:        req.Contract,
	}
	select {
	case m.waiting <- lj:
		lj.queued = true
		meta.Status = StatusQueued
		lj.meta = meta
		m.jobs[id] = lj
		m.mu.Unlock()
		m.persistMeta(meta)
		m.persistEvent(meta, "queued: waiting for a concurrency slot")
		return Info{Meta: meta}, nil
	default:
		m.mu.Unlock()
		// Rejected before any lifecycle existed — remove the freshly
		// created dir so no orphaned non-terminal meta survives on disk.
		_ = m.store.remove(id)
		return Info{}, ErrBusy
	}
}

func runContextFor(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(context.Background(), timeout)
	}
	return context.WithCancel(context.Background())
}

func (m *Manager) run(ctx context.Context, lj *liveJob) {
	defer close(lj.done)
	// Hand the slot to the oldest queued job, or release it. Ownership
	// transfer (never release-then-reacquire) keeps FIFO order race-free:
	// the slot does not return to the pool while waiters exist, so a
	// concurrent Spawn cannot jump the queue.
	defer m.retire()

	meta := lj.meta
	meta.Status = StatusRunning
	meta.StartedAt = time.Now().UTC()
	m.persistMeta(meta)
	m.setLiveMeta(meta)
	m.persistEvent(meta, "running")

	env := RunEnv{
		Job: meta,
		Log: func(message string) {
			m.persistEvent(meta, message)
		},
		WriteResult: func(summary string) error {
			err := m.store.writeResult(meta, summary)
			if err != nil {
				m.reportStore("writeResult", meta.ID, err)
			}
			return err
		},
		OnProgress: func(p Progress) {
			p.JobID = meta.ID
			if p.ParentToolUseID == "" {
				p.ParentToolUseID = lj.parentToolUseID
			}
			if p.Time.IsZero() {
				p.Time = time.Now().UTC()
			}
			m.emitProgress(p)
		},
		Contract: lj.contract,
	}

	summary, err := m.runner.Run(ctx, env)

	meta.FinishedAt = time.Now().UTC()

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		meta.Status = StatusTimedOut
		if err != nil {
			meta.Error = err.Error()
		} else {
			meta.Error = ctx.Err().Error()
		}
		m.persistEvent(meta, "timed_out: "+meta.Error)
	case errors.Is(ctx.Err(), context.Canceled):
		meta.Status = StatusCancelled
		if err != nil {
			meta.Error = err.Error()
		} else {
			meta.Error = ctx.Err().Error()
		}
		m.persistEvent(meta, "cancelled: "+meta.Error)
	case err != nil:
		meta.Status = StatusFailed
		meta.Error = err.Error()
		m.persistEvent(meta, "failed: "+meta.Error)
	default:
		meta.Status = StatusCompleted
		if summary != "" {
			if werr := m.store.writeResult(meta, summary); werr != nil {
				m.reportStore("writeResult", meta.ID, werr)
				meta.Status = StatusFailed
				meta.Error = "failed to write result.md: " + werr.Error()
				m.persistEvent(meta, "failed: "+meta.Error)
			} else {
				m.persistEvent(meta, "completed")
			}
		} else {
			m.persistEvent(meta, "completed")
		}
	}

	m.persistMeta(meta)
	m.setLiveMeta(meta)

	m.mu.Lock()
	delete(m.jobs, meta.ID)
	m.mu.Unlock()
}

func (m *Manager) setLiveMeta(meta Meta) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lj, ok := m.jobs[meta.ID]; ok {
		lj.meta = meta
	}
}

// List returns all jobs known on disk, newest CreatedAt first.
func (m *Manager) List(ctx context.Context) ([]Info, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ids, err := m.store.listIDs()
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(ids))
	for _, id := range ids {
		info, err := m.loadInfo(id)
		if err != nil {
			continue
		}
		out = append(out, info)
	}
	slices.SortFunc(out, func(a, b Info) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	return out, nil
}

// Get returns one job by id.
func (m *Manager) Get(ctx context.Context, id string) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	return m.loadInfo(id)
}

func (m *Manager) loadInfo(id string) (Info, error) {
	m.mu.Lock()
	if lj, ok := m.jobs[id]; ok {
		meta := lj.meta
		m.mu.Unlock()
		return Info{Meta: meta}, nil
	}
	m.mu.Unlock()
	meta, err := m.store.readMeta(id)
	if err != nil {
		return Info{}, err
	}
	return Info{Meta: meta}, nil
}

// Wait blocks until the job reaches a terminal status or ctx is done.
//
// A Wait timeout (ctx deadline) does not cancel the job; use [Manager.Cancel].
func (m *Manager) Wait(ctx context.Context, id string) (WaitResult, error) {
	for {
		info, err := m.loadInfo(id)
		if err != nil {
			return WaitResult{}, err
		}
		if info.Status.Terminal() {
			summary, _ := m.store.readResult(info.Meta)
			return WaitResult{Info: info, Summary: summary}, nil
		}

		m.mu.Lock()
		lj, ok := m.jobs[id]
		var done <-chan struct{}
		if ok {
			done = lj.done
		}
		m.mu.Unlock()

		if !ok {
			info, err = m.loadInfo(id)
			if err != nil {
				return WaitResult{}, err
			}
			if info.Status.Terminal() {
				summary, _ := m.store.readResult(info.Meta)
				return WaitResult{Info: info, Summary: summary}, nil
			}
			if info.Status == StatusPending {
				// Parked by a restart — waiting cannot help; the
				// durable host resumes it or the caller cancels.
				return WaitResult{}, fmt.Errorf("%w: %s is pending resume (resume or cancel it)", ErrNotRunning, id)
			}
			return WaitResult{}, fmt.Errorf("%w: %s disappeared while non-terminal", ErrNotFound, id)
		}

		select {
		case <-ctx.Done():
			return WaitResult{}, fmt.Errorf("%w: %w", ErrWaitTimeout, ctx.Err())
		case <-done:
		}
	}
}

// Log returns the last limit events (0 = all).
func (m *Manager) Log(ctx context.Context, id string, limit int) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := m.loadInfo(id)
	if err != nil {
		return nil, err
	}
	return m.store.readEvents(info.Meta, limit)
}

// Resume restarts a pending job (Bet 01): the runner sees Resume=true and
// continues the job's own prior session — rebuilt from the session store,
// evidence receipts replaying through the same ledger scope — instead of
// starting fresh. Concurrency and queueing discipline match Spawn. Only a
// parked (pending) job can resume; live or terminal jobs error.
func (m *Manager) Resume(ctx context.Context, id string) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	meta, err := m.store.readMeta(id)
	if err != nil {
		return Info{}, err
	}
	if meta.Status != StatusPending {
		return Info{}, fmt.Errorf("%w: %s is %s, not pending", ErrInvalid, id, meta.Status)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Info{}, ErrClosed
	}
	if _, live := m.jobs[id]; live {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("%w: %s is already live", ErrInvalid, id)
	}
	meta.Resume = true
	lj := &liveJob{
		meta:     meta,
		done:     make(chan struct{}),
		timeout:  time.Duration(meta.TimeoutSec) * time.Second,
		contract: meta.Contract,
	}
	// Same fast/slow split as Spawn: a free slot starts now, a full set
	// parks the resume in the FIFO wait queue.
	select {
	case m.slots <- struct{}{}:
		if err := ctx.Err(); err != nil {
			m.mu.Unlock()
			<-m.slots
			return Info{}, err
		}
		runCtx, cancel := runContextFor(lj.timeout)
		lj.cancel = cancel
		m.jobs[id] = lj
		m.mu.Unlock()
		go m.run(runCtx, lj)
		return Info{Meta: meta}, nil
	default:
	}
	select {
	case m.waiting <- lj:
		lj.queued = true
		meta.Status = StatusQueued
		lj.meta = meta
		m.jobs[id] = lj
		m.mu.Unlock()
		m.persistMeta(meta)
		m.persistEvent(meta, "queued: resume waiting for a concurrency slot")
		return Info{Meta: meta}, nil
	default:
		m.mu.Unlock()
		return Info{}, ErrBusy
	}
}

// ResumePending resumes every parked job (Bet 01): the durable host
// (a1 serve) calls it at boot so overnight work continues where the dead
// process left off. Jobs already terminal, live, or unreadable are
// skipped; a full wait queue surfaces as ErrBusy for the caller to log.
func (m *Manager) ResumePending(ctx context.Context) error {
	ids, err := m.store.listIDs()
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		meta, err := m.store.readMeta(id)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				m.reportStore("readMeta", id, err)
			}
			continue
		}
		if meta.Status != StatusPending {
			continue
		}
		if _, err := m.Resume(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("resume %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

// Cancel requests cancellation of a running/starting/queued job. A parked
// (pending) job cancels in place: it never had a goroutine, so the meta is
// finalized directly.
func (m *Manager) Cancel(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	lj, ok := m.jobs[id]
	queued := ok && lj.queued
	m.mu.Unlock()
	if !ok {
		info, err := m.loadInfo(id)
		if err != nil {
			return err
		}
		if info.Status.Terminal() {
			return nil
		}
		if info.Status == StatusPending {
			// Parked by a restart, no goroutine to join — finalize in place.
			info.Status = StatusCancelled
			info.FinishedAt = time.Now().UTC()
			info.Error = "cancelled while pending resume"
			m.persistMeta(info.Meta)
			m.persistEvent(info.Meta, "cancelled: while pending resume")
			return nil
		}
		return fmt.Errorf("%w: %s", ErrNotRunning, id)
	}
	if queued {
		m.cancelQueued(lj)
		return nil
	}
	lj.cancel()
	select {
	case <-lj.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// cancelQueued finalizes a job still sitting in the wait queue: it never
// started, so there is no goroutine to join — mark cancelled, close done,
// unregister. The stale entry stays in the channel; retire skips it.
func (m *Manager) cancelQueued(lj *liveJob) {
	m.mu.Lock()
	if !lj.queued {
		// Already popped and started by retire (or double-cancelled).
		m.mu.Unlock()
		return
	}
	lj.queued = false
	lj.cancelled.Store(true)
	meta := lj.meta
	meta.Status = StatusCancelled
	meta.FinishedAt = time.Now().UTC()
	meta.Error = "cancelled while queued"
	delete(m.jobs, meta.ID)
	m.mu.Unlock()
	close(lj.done)
	m.persistMeta(meta)
	m.persistEvent(meta, "cancelled: while waiting for a concurrency slot")
}

// retire hands a finishing job's concurrency slot directly to the oldest
// queued job, or releases it when the wait queue is empty. Ownership
// transfer (never release-then-reacquire) keeps FIFO order race-free: the
// slot does not return to the pool while waiters exist, so a concurrent
// Spawn cannot jump the queue.
func (m *Manager) retire() {
	for {
		select {
		case lj := <-m.waiting:
			if m.startQueued(lj) {
				return // the waiter now owns this slot
			}
			// Cancelled while queued — try the next waiter.
		default:
			<-m.slots
			return
		}
	}
}

// startQueued begins a queued job on the caller's transferred slot. It
// reports whether the job started; a job cancelled while queued never
// starts (cancelQueued already finalized it).
func (m *Manager) startQueued(lj *liveJob) bool {
	m.mu.Lock()
	if !lj.queued {
		m.mu.Unlock()
		return false
	}
	lj.queued = false
	if lj.cancelled.Load() {
		m.mu.Unlock()
		return false
	}
	runCtx, cancel := runContextFor(lj.timeout)
	lj.cancel = cancel
	m.mu.Unlock()
	go m.run(runCtx, lj)
	return true
}

// Close cancels all live jobs (running and queued) and waits for them to exit.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	lives := make([]*liveJob, 0, len(m.jobs))
	var queued []*liveJob
	for id, lj := range m.jobs {
		if lj.queued {
			// Queued jobs never started — finalize in place
			// instead of joining a goroutine that does not exist.
			lj.queued = false
			lj.cancelled.Store(true)
			meta := lj.meta
			meta.Status = StatusCancelled
			meta.FinishedAt = time.Now().UTC()
			meta.Error = "cancelled while queued (manager closed)"
			lj.meta = meta
			queued = append(queued, lj)
			delete(m.jobs, id)
			continue
		}
		lives = append(lives, lj)
		lj.cancel()
	}
	// Close subscriber channels under the lock so they cannot race a send in
	// emitProgress (which also holds m.mu). The closed flag makes Close safe
	// even if a subscriber's cancel func runs later.
	for _, s := range m.subs {
		if !s.closed {
			s.closed = true
			close(s.ch)
		}
	}
	m.subs = nil
	m.mu.Unlock()

	for _, lj := range queued {
		m.persistMeta(lj.meta)
		m.persistEvent(lj.meta, "cancelled: manager closed while queued")
		close(lj.done)
	}

	for _, lj := range lives {
		select {
		case <-lj.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Subscribe receives live [Progress] events. The channel is buffered; slow
// consumers may miss events. Cancel closes the channel and unregisters.
func (m *Manager) Subscribe() (<-chan Progress, func()) {
	ch := make(chan Progress, 64)
	sub := &subscriber{ch: ch}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	m.subs = append(m.subs, sub)
	m.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			for i, s := range m.subs {
				if s == sub {
					m.subs = slices.Delete(m.subs, i, i+1)
					break
				}
			}
			// Idempotent vs Manager.Close: never close an already closed channel.
			if !sub.closed {
				sub.closed = true
				close(ch)
			}
		})
	}
	return ch, cancel
}

func (m *Manager) emitProgress(p Progress) {
	// Sends are non-blocking, so holding the lock is safe; it makes send and
	// channel close mutually exclusive (cancel/Close close under m.mu).
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.subs {
		select {
		case s.ch <- p:
		default:
			// drop if subscriber is slow
		}
	}
}

// ParentToolUseID returns the UI correlation id for a live job, if any.
func (m *Manager) ParentToolUseID(jobID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lj, ok := m.jobs[jobID]; ok {
		return lj.parentToolUseID
	}
	return ""
}

// MaxDepth exposes the configured nesting ceiling (for tool adapters).
func (m *Manager) MaxDepth() int { return m.maxDepth }

// MaxConcurrent exposes the concurrency ceiling.
func (m *Manager) MaxConcurrent() int { return m.maxConcurrent }

// MaxQueue exposes the wait-queue ceiling (spawns beyond it fail with
// [ErrBusy] — the hard backpressure edge).
func (m *Manager) MaxQueue() int { return m.maxQueue }

// LiveCount returns how many jobs are currently queued/starting/running
// in-process.
func (m *Manager) LiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.jobs)
}
