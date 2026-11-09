package job

import (
	"fmt"
	"time"
)

// Status is the lifecycle state of a job.
type Status string

// Job lifecycle status values.
const (
	// StatusStarting means the job was accepted and a goroutine is about to run.
	// When MaxConcurrent slots are full the job joins a bounded FIFO wait queue
	// (StatusQueued) instead; only a full queue makes Spawn return [ErrBusy].
	StatusStarting Status = "starting"
	// StatusQueued means the job holds a place in the wait queue and starts
	// as soon as a concurrency slot drains (FIFO).
	StatusQueued Status = "queued"
	// StatusPending means the job is durably parked (Bet 01): a restart
	// requeued it instead of failing it. It resumes via [Manager.Resume] —
	// the runner rebuilds the engine from the job's persisted session and
	// replays its evidence receipts — or cancels outright. Only reachable
	// under RecoverRequeue.
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
	StatusTimedOut  Status = "timed_out"
	// StatusStalled is the stall monitor's terminal fold: the runner went
	// silent past the stall window with no progress tokens, so "running"
	// stopped being true. The wedged goroutine may still exist; the ledger
	// no longer pretends it is working.
	StatusStalled Status = "stalled"
)

// Terminal reports whether no further work will run.
func (s Status) Terminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusCancelled, StatusTimedOut, StatusStalled:
		return true
	default:
		return false
	}
}

// Job kinds (Meta.Kind): what spawned the job. The empty kind is the
// default (agent_* tools); the Clock fires KindSchedule jobs.
const (
	KindAgent    = ""
	KindSchedule = "schedule"
)

// Meta is persisted as meta.json.
type Meta struct {
	ID          string `json:"id"`
	ParentID    string `json:"parent_id,omitempty"`
	ParentDepth int    `json:"parent_depth"`
	Role        Role   `json:"role,omitempty"` // explore | worker | review; empty → explore
	Prompt      string `json:"prompt"`
	Description string `json:"description,omitempty"`
	WorkDir     string `json:"workdir,omitempty"`
	// WarmFrom names a previous job whose session this job resumes (the
	// warm-worker pool: the child keeps its prior repo context). Empty = fresh.
	WarmFrom string `json:"warm_from,omitempty"`
	// Kind says what spawned the job (agent tool vs the Clock's scheduler).
	Kind string `json:"kind,omitempty"`
	// ScheduleID links a KindSchedule job to the schedule that fired it.
	ScheduleID string `json:"schedule_id,omitempty"`
	// Owner stamps the owning process ("pid:starttime" on Linux, bare pid
	// elsewhere) while the job is live, so a second manager over the same
	// root can tell a dead owner's leftover from another live surface's
	// running job. Cleared when a job is parked; re-stamped on resume.
	Owner string `json:"owner,omitempty"`
	// Attempt counts runs of this job: 0 until first start, +1 on every
	// restart requeue, so a resumed job knows it is a retry.
	Attempt int `json:"attempt,omitempty"`
	// Resume marks a job whose runner should continue the job's OWN prior
	// session (rebuild from the session store + replay evidence receipts)
	// instead of starting fresh — set by [Manager.Resume], never by Spawn.
	Resume bool `json:"resume,omitempty"`
	// TimeoutSec is the run timeout in seconds (0 = none), persisted so a
	// resumed job keeps its original bound.
	TimeoutSec int `json:"timeout_sec,omitempty"`
	// Contract is the fleet task-contract seed, persisted so a resumed
	// job keeps its finish line.
	Contract   []string  `json:"contract,omitempty"`
	Status     Status    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Error      string    `json:"error,omitempty"`
	ResultPath string    `json:"result_path,omitempty"`
	EventsPath string    `json:"events_path,omitempty"`
	Dir        string    `json:"dir"`
}

// Info is a list/get snapshot.
type Info struct {
	Meta
}

// SpawnRequest configures a new job.
type SpawnRequest struct {
	Prompt          string
	Description     string
	ParentID        string // parent session or parent job id (opaque to this package)
	ParentToolUseID string // parent agent tool_use id for TUI nesting (not persisted)
	Depth           int    // 0 = top-level; tool layer should force Depth for children
	Role            Role   // explore | worker | review; empty → explore
	WorkDir         string
	Timeout         time.Duration // 0 = no run timeout; Cancel still works
	// WarmFrom resumes a previous job's session (warm worker pool). The
	// runner interprets it; the manager only persists it.
	WarmFrom string
	// Contract carries the parent fleet's declared check ids (task
	// contracts). The runner seeds the child engine with them; the manager
	// only forwards it via RunEnv.
	Contract []string
	// Kind / ScheduleID mark scheduler-fired jobs (Bet 01); the default
	// empty kind is a plain agent spawn.
	Kind       string
	ScheduleID string
}

func (r SpawnRequest) validate() error {
	if r.Prompt == "" {
		return fmt.Errorf("%w: prompt is required", ErrInvalid)
	}
	if _, err := ParseRole(string(r.Role)); err != nil {
		return err
	}
	return nil
}

// Event is one JSONL log line.
type Event struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}

// WaitResult is returned by Wait / Task.
type WaitResult struct {
	Info    Info
	Summary string // contents of result.md when present
}

// RecoveryMode controls how [New] treats leftover non-terminal jobs on disk.
type RecoveryMode int

const (
	// RecoverMarkFailed marks starting/running jobs as failed. Safe for
	// TUI / CLI restarts so Wait/Cancel do not see zombies.
	RecoverMarkFailed RecoveryMode = iota
	// RecoverIgnore leaves disk state unchanged (tests / manual inspection).
	RecoverIgnore
	// RecoverRequeue parks starting/queued/running jobs as pending instead
	// of failing them (Bet 01): a restart requeued the process, not the
	// work. Pending jobs resume via [Manager.Resume] — the durable host
	// (a1 serve) auto-resumes them at boot — or cancel outright.
	RecoverRequeue
)
