package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/extension"
	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/project/model"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools"
)

// Fleet nesting defaults (Bet 04). Depth counts agent generations below the
// top-level engine: depth 1 = child, 2 = grandchild, 3 = great-grandchild.
const (
	// defaultFleetDepth caps the agent tree at three generations.
	defaultFleetDepth = 3
	// nestedMaxConcurrent is the concurrency ceiling for a nested manager:
	// deliberately tighter than the top level so a worker's own fan-out
	// cannot explode the process.
	nestedMaxConcurrent = 2
	// nestedDrainWindow bounds how long a finishing child waits for its
	// own spawned jobs before cancelling them — the fleet dies with its
	// captain, but in-flight grandchildren get a grace window to finish.
	nestedDrainWindow = 30 * time.Second
)

// EngineRunner runs a child [Engine.Loop] as a [job.Runner].
//
// Each Run creates a fresh Engine with a persisted session under
// <job.Dir>/session/, ParentID from the job, and no Ask handler.
// Role (explore|worker|review) selects tools, permission mode, and the
// per-depth step budget; explore/review children get no agent_* tools (the
// no-nesting default), while worker children above the depth ceiling get a
// nested job manager so they can spawn their own fleet. ModelFn (when set)
// selects the model per role; otherwise Model is used as a fixed snapshot.
//
// Extensions (or ExtensionsFn) are inherited from the parent so policy
// tool_call handlers apply. Extension RegisterTool tools are not merged
// into child engines. ExtensionsFn wins when set (live reload).
type EngineRunner struct {
	Model        llm.ModelConfig
	ModelFn      func(role job.Role) llm.ModelConfig // if set, preferred over Model
	Gate         permission.Gate                     // nil → SpecForRole(job.Role).Mode on WorkDir
	Tools        []tools.Tool                        // nil → SpecForRole(job.Role).Tools
	MaxRounds    int                                 // 0 → Engine default
	Extensions   *extension.Runner
	ExtensionsFn func() *extension.Runner
	// ParentGateFn returns the parent trajectory's gate, so the child branch
	// can fork from the parent's label (L0c := Lp) and share its effect log.
	// Nil → the child starts from the role's default policy.
	ParentGateFn func() permission.Gate
	// JobsRoot, when non-empty, enables worker-child nesting (Bet 04): a
	// worker whose own depth is below MaxDepth gets a nested job manager
	// over the same jobs root. Explore/review children never nest.
	JobsRoot string
	// MaxDepth is the fleet depth ceiling used for nested managers
	// (0 → defaultFleetDepth).
	MaxDepth int
	// DB mirrors job metas into the vault's jobs table (Bet 01); nil =
	// files only. Flows into nested managers so grandchildren are recorded
	// too.
	DB *sql.DB
}

func (r EngineRunner) fleetDepth() int {
	if r.MaxDepth > 0 {
		return r.MaxDepth
	}
	return defaultFleetDepth
}

// childGate builds the child branch's gate. When the parent gate is a
// StaticGate, the child forks from the parent's current label and shares its
// effect log; otherwise the role's default policy is used. Either way the
// child's own trajectory state is a copy: the parent's label is never mutated
// by the child, and pending parent acceptances do not cross the boundary
// (APPA §6, Theorem 8).
func (r EngineRunner) childGate(spec ChildSpec, cwd string) (permission.Gate, error) {
	parent := permission.StaticGateOf(r.parentGate())
	if parent == nil {
		gate, err := permission.NewGateAt(permission.ChildPolicy(spec.Mode), cwd)
		if err != nil {
			return nil, err
		}
		if spec.Confinement != nil {
			return permission.NewConfinementGate(gate, *spec.Confinement), nil
		}
		return gate, nil
	}

	child := permission.ForkChildGate(parent, spec.Mode)
	if spec.Confinement != nil {
		return permission.NewConfinementGate(child, *spec.Confinement), nil
	}
	return child, nil
}

func (r EngineRunner) parentGate() permission.Gate {
	if r.ParentGateFn == nil {
		return nil
	}
	return r.ParentGateFn()
}

// nestedManager builds the job manager a worker child uses to spawn its own
// fleet (Bet 04 depth 2-3). RecoverIgnore is load-bearing: restart recovery
// belongs to the one top-level manager, which scans the shared root anyway —
// a nested manager must not mark its parent's live jobs failed.
func (r EngineRunner) nestedManager(gate permission.Gate) (*job.Manager, error) {
	return job.New(job.Options{
		Root: r.JobsRoot,
		DB:   r.DB,
		Runner: EngineRunner{
			Model:        r.Model,
			ModelFn:      r.ModelFn,
			ExtensionsFn: r.ExtensionsFn,
			ParentGateFn: func() permission.Gate { return gate },
			JobsRoot:     r.JobsRoot,
			MaxDepth:     r.fleetDepth(),
			DB:           r.DB,
		},
		MaxConcurrent: nestedMaxConcurrent,
		MaxDepth:      r.fleetDepth(),
		Recovery:      job.RecoverIgnore,
	})
}

// warmSession opens this job's session, resuming a previous worker's session
// when meta.WarmFrom is set (the warm-worker pool, Bet 04): the source
// session file is copied into the new job's session dir and resumed, so the
// worker keeps its prior repo context and skips re-reading. A missing or
// unreadable source falls back to a fresh session — warmth is an
// optimization, never a hard requirement.
func warmSession(meta job.Meta, sessionDir string) (*Session, error) {
	root := filepath.Dir(meta.Dir)
	srcDir := filepath.Join(root, meta.WarmFrom, "session")
	metas, err := session.ListSessions(srcDir)
	if err != nil || len(metas) == 0 {
		return nil, nil // caller falls back to a fresh session
	}
	src := metas[0].File
	if _, statErr := os.Stat(src); statErr != nil {
		// Vault-backed source (Bet 08): no file on disk, but the rows are
		// the source of truth — resume the synthetic path directly and
		// keep the worker's one continuous session.
		return NewSession(WithResumePath(src))
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return nil, nil
	}
	dst := filepath.Join(sessionDir, filepath.Base(src))
	if err := copyFile(src, dst); err != nil {
		return nil, nil
	}
	return NewSession(WithResumePath(dst))
}

// resumeJobSession reopens the job's OWN prior session after a restart
// (Bet 01 durable resume): the newest session under <job.Dir>/session/ is
// resumed, so the rebuilt engine starts with the prior turns' context and
// the evidence ledger replays its receipts through the same scope (the
// ledger is keyed by the session file's directory). Vault-backed sources
// resolve through their synthetic paths — ListSessions reports them the
// same way. A missing or unreadable session returns nil and the caller
// falls back to fresh, mirroring warmth's never-a-hard-requirement rule.
func resumeJobSession(sessionDir string, log func(string)) *Session {
	metas, err := session.ListSessions(sessionDir)
	if err != nil || len(metas) == 0 {
		return nil
	}
	src := metas[0].File // ListSessions is newest-first
	sess, err := NewSession(WithResumePath(src))
	if err != nil {
		log("resume unavailable (" + src + "): " + err.Error())
		return nil
	}
	return sess
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // path comes from the local jobs tree
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// Run implements [job.Runner].
func (r EngineRunner) Run(ctx context.Context, env job.RunEnv) (string, error) {
	if env.Job.Dir == "" {
		return "", errors.New("agent: EngineRunner requires job Dir")
	}

	cwd := env.Job.WorkDir
	if cwd == "" {
		cwd = "."
	}

	// This child's own depth in the fleet: one below its spawning parent.
	depth := env.Job.ParentDepth + 1
	spec := SpecForRoleAt(env.Job.Role, depth)

	var gate permission.Gate
	if r.Gate != nil {
		gate = r.Gate
	} else {
		built, err := r.childGate(spec, cwd)
		if err != nil {
			return "", err
		}
		gate = built
	}

	toolList := r.Tools
	if toolList == nil {
		toolList = spec.Tools
	}

	cfg := r.Model
	if r.ModelFn != nil {
		cfg = r.ModelFn(env.Job.Role)
	}

	extRunner := r.Extensions
	if r.ExtensionsFn != nil {
		extRunner = r.ExtensionsFn()
	}

	sessionDir := filepath.Join(env.Job.Dir, "session")
	var (
		sess *Session
		err  error
	)
	switch {
	case env.Job.Resume:
		// Durable resume (Bet 01): continue this job's own prior session.
		// Any role may resume itself (unlike the worker-only warm pool).
		sess = resumeJobSession(sessionDir, env.Log)
		if sess == nil {
			env.Log("no prior session to resume; starting fresh")
		}
	case env.Job.WarmFrom != "" && spec.Role == job.RoleWorker:
		sess, err = warmSession(env.Job, sessionDir)
		if err == nil && sess == nil {
			env.Log("warm resume unavailable for job " + env.Job.WarmFrom + "; starting fresh")
		}
	case env.Job.WarmFrom != "":
		env.Log("warm_from ignored: only role=worker may resume a prior session")
	}
	if sess == nil {
		sess, err = NewSession(
			WithCwd(cwd),
			WithSessionDir(sessionDir),
			WithPersist(true),
			WithParentID(env.Job.ParentID),
		)
	}
	if err != nil {
		return "", err
	}

	engineOpts := []EngineOption{
		WithGate(gate),
		WithTools(toolList),
		WithMaxRounds(r.MaxRounds),
		WithExtensions(extRunner),
		WithOmitExtensionTools(true),
		WithHooks(model.HooksFor(cfg.Name)),
		WithAgentDepth(depth),
	}

	// Worker nesting (Bet 04 depth 2-3): a worker below the ceiling gets
	// its own manager over the shared jobs root.
	var nested *job.Manager
	if spec.Role == job.RoleWorker && r.JobsRoot != "" && depth < r.fleetDepth() {
		nested, err = r.nestedManager(gate)
		if err != nil {
			return "", err
		}
		defer func() {
			drainCtx, cancel := context.WithTimeout(context.Background(), nestedDrainWindow)
			defer cancel()
			if err := nested.Close(drainCtx); err != nil {
				env.Log("nested jobs drain: " + err.Error())
			}
		}()
		engineOpts = append(engineOpts, WithJobs(nested))
	}

	engine, err := NewEngine(cfg, sess, engineOpts...)
	if err != nil {
		return "", err
	}
	if len(env.Contract) > 0 {
		engine.SeedTaskContract(env.Contract, spec.Role == job.RoleWorker)
	}

	env.Log(fmt.Sprintf("sub-agent role=%s depth=%d session=%s parent=%s",
		spec.Role, depth, engine.SessionID(), env.Job.ParentID))

	prompt := env.Job.Prompt
	if env.Job.Description != "" {
		prompt = env.Job.Description + "\n\n" + prompt
	}
	prompt = prompt + "\n\n" + spec.Hint

	var (
		lastText string
		lastErr  error
	)
	for ev, loopErr := range engine.Loop(ctx, prompt, LoopOpts{}) {
		if loopErr != nil {
			lastErr = loopErr
			env.Log("error: " + loopErr.Error())
			break
		}
		switch e := ev.(type) {
		case session.AssistantMessageUpdate:
			if e.Message.State == session.StateComplete {
				if t := strings.TrimSpace(e.Message.FlatText()); t != "" {
					lastText = t
				}
			}
		case session.ToolData:
			detail := e.Run.Detail
			if detail == "" {
				detail = e.Run.Name
			}
			env.Log(fmt.Sprintf("tool %s %s: %s", e.Run.Name, e.Run.Status, detail))
			if env.OnProgress != nil {
				env.OnProgress(job.Progress{
					ToolUseID: e.Run.ToolUseID,
					Name:      e.Run.Name,
					Status:    e.Run.Status.String(),
					Detail:    detail,
				})
			}
		}
	}

	if path := engine.SessionFile(); path != "" {
		env.Log("session_file=" + path)
	}

	// Structured handoff (Bet 04): result.md stays the prose summary, and
	// handoff.json carries the machine-checkable parts — files touched and
	// where every observed check stands — for agent_wait to merge into the
	// parent's context.
	if _, herr := writeHandoff(env.Job.Dir, lastText, engine); herr != nil {
		env.Log("handoff write failed: " + herr.Error())
	}

	if lastErr != nil {
		if lastText != "" {
			_ = env.WriteResult(lastText)
		}
		return lastText, lastErr
	}
	if ctx.Err() != nil {
		return lastText, ctx.Err()
	}
	if lastText == "" {
		lastText = "sub-agent finished with no text reply"
	}
	return lastText, nil
}
