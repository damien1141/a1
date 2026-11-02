package eval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/agent"
	"github.com/damien1141/a1/internal/eval/golden"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/session"
)

// evalRunsSchema is the golden-run history table (lazy DDL, same pattern as
// the telemetry and skillstats stores). Rows are append-only facts; CI
// compares against them, nothing mutates them.
const evalRunsSchema = `
CREATE TABLE IF NOT EXISTS eval_runs (
  id              INTEGER PRIMARY KEY,
  task            TEXT NOT NULL,
  variant         TEXT NOT NULL DEFAULT '',
  model           TEXT NOT NULL DEFAULT '',
  rounds_to_green INT    NOT NULL DEFAULT 0,
  rollbacks       INT    NOT NULL DEFAULT 0,
  stalls          INT    NOT NULL DEFAULT 0,
  tokens          INT    NOT NULL DEFAULT 0,
  exit_code       INT    NOT NULL DEFAULT 0,
  green           INT    NOT NULL DEFAULT 0,
  session_id      TEXT    NOT NULL DEFAULT '',
  at              TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_eval_runs_task ON eval_runs(task, at);
`

// ErrNoModel is returned by RunGolden when no model is configured. CI
// without secrets maps it to a graceful skip, not a failure.
var ErrNoModel = errors.New("golden set needs a configured model")

// GoldenOptions configures a golden-set run.
type GoldenOptions struct {
	// Only restricts the run to specific task ids ("" = all).
	Only []string
	// Model overrides the configured default model name ("" = default).
	Model string
	// MaxRounds caps tool rounds per task (0 = engine default).
	MaxRounds int
	// Variant labels this run's configuration ("default", a tool subset,
	// a model name — the eval_runs.variant column).
	Variant string
	// BaseDir is the run root: workspaces under <BaseDir>/<task>/, sessions
	// under <BaseDir>/sessions/<task>/. Empty uses a temp dir (the run is
	// then disposable; history still lands in the vault).
	BaseDir string
}

// GoldenTaskResult is one task's outcome.
type GoldenTaskResult struct {
	Task          golden.Task
	RoundsToGreen int    `json:"rounds_to_green"`
	Green         bool   `json:"green"`
	Rollbacks     int    `json:"rollbacks"`
	Stalls        int    `json:"stalls"`
	Tokens        int64  `json:"tokens"`
	ExitCode      int    `json:"exit_code"`
	SessionID     string `json:"session_id"`
	Err           string `json:"error,omitempty"`
}

// GoldenReport is a completed golden-set run.
type GoldenReport struct {
	Variant string             `json:"variant"`
	Model   string             `json:"model"`
	Results []GoldenTaskResult `json:"results"`
}

// RunGolden runs the golden set against the configured model, scoring each
// task's session with the same offline scorer and appending eval_runs rows
// to the vault when it is live. No LLM is touched until the model check
// passes, and a missing model is a skip (ErrNoModel), not a failure.
func RunGolden(ctx context.Context, opts GoldenOptions) (GoldenReport, error) {
	_, cfg, err := bootConfigured()
	if err != nil {
		// bootConfigured's error names the fix; wrap it as the skip sentinel
		// so CI can distinguish "no secrets" from "broken".
		return GoldenReport{}, fmt.Errorf("%w: %v", ErrNoModel, err)
	}
	model := cfg.Model()
	if opts.Model != "" {
		m, ok := cfg.FindModel(opts.Model)
		if !ok {
			return GoldenReport{}, fmt.Errorf("golden: model %q not configured", opts.Model)
		}
		model = m
	}
	variant := opts.Variant
	if variant == "" {
		variant = "default"
	}

	base := opts.BaseDir
	if base == "" {
		tmp, err := os.MkdirTemp("", "a1-golden-")
		if err != nil {
			return GoldenReport{}, fmt.Errorf("golden: %w", err)
		}
		defer os.RemoveAll(tmp)
		base = tmp
	}

	var db *sql.DB
	if v := session.DefaultVault(); v != nil {
		db = v.DB()
		if err := ensureEvalRuns(db); err != nil {
			return GoldenReport{}, err
		}
	}

	report := GoldenReport{Variant: variant, Model: model.Name}
	for _, task := range golden.Tasks {
		if len(opts.Only) > 0 && !containsString(opts.Only, task.ID) {
			continue
		}
		res := runGoldenTask(ctx, cfg, model, task, base, opts, db)
		report.Results = append(report.Results, res)
	}
	return report, nil
}

// runGoldenTask executes one task: fresh workspace from the fixture, one
// headless engine loop with the task prompt (plus the contract instruction
// — the model must declare the finish line so the scorer can see it), then
// the offline score.
func runGoldenTask(
	ctx context.Context,
	cfg *project.Config,
	model llm.ModelConfig,
	task golden.Task,
	base string,
	opts GoldenOptions,
	db *sql.DB,
) GoldenTaskResult {
	res := GoldenTaskResult{Task: task}

	ws := filepath.Join(base, task.ID)
	// Per-task session dir = per-task evidence scope: receipts from one
	// task must never satisfy another's contract.
	sessDir := filepath.Join(base, "sessions", task.ID)
	if err := writeFixture(ws, task.Fixture); err != nil {
		res.Err = err.Error()
		return res
	}

	gate, err := headlessGate(cfg, ws)
	if err != nil {
		res.Err = err.Error()
		return res
	}

	engineOpts := []agent.EngineOption{
		agent.WithGate(gate),
		agent.WithEvidenceChecks(cfg.EvidenceChecks()),
	}
	if opts.MaxRounds > 0 {
		engineOpts = append(engineOpts, agent.WithMaxRounds(opts.MaxRounds))
	}

	sess, err := agent.NewSession(
		agent.WithCwd(ws),
		agent.WithSessionDir(sessDir),
		agent.WithPersist(true),
	)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	engine, err := agent.NewEngine(model, sess, engineOpts...)
	if err != nil {
		res.Err = err.Error()
		return res
	}

	exit, loopErr := drainLoop(ctx, engine, goldenPrompt(task))
	res.ExitCode = exit
	res.SessionID = engine.SessionID()
	if loopErr != nil {
		res.Err = loopErr.Error()
	}

	scored, err := ScoreFile(engine.SessionFile(), model.Name)
	if err != nil {
		if res.Err != "" {
			res.Err += "; " + err.Error()
		} else {
			res.Err = err.Error()
		}
		return res
	}
	res.RoundsToGreen = scored.RoundsToGreen
	res.Green = scored.Green
	res.Rollbacks = scored.Rollbacks
	res.Stalls = scored.StallStops
	res.Tokens = scored.TotalTokens

	// The scorer's write step: turns + skill outcomes, plus the eval_runs
	// history row. All best-effort — a vault hiccup must not fail the run.
	if db != nil {
		_ = Persist(scored, db)
		_ = insertEvalRun(db, task.ID, opts.Variant, model.Name, res)
	}
	return res
}

// goldenPrompt appends the contract instruction to the task prompt. The
// model proposes the checks (VGR authority split); naming them explicitly
// keeps every run's finish line identical, which is what makes
// rounds-to-green comparable across nights.
func goldenPrompt(task golden.Task) string {
	quoted := make([]string, 0, len(task.Checks))
	for _, id := range task.Checks {
		quoted = append(quoted, fmt.Sprintf("%q", id))
	}
	var b strings.Builder
	b.WriteString(task.Prompt)
	b.WriteString("\n\nWork in the current directory. Start by declaring your done-criteria with the `contract` tool, passing exactly these check ids: [")
	b.WriteString(strings.Join(quoted, ", "))
	b.WriteString("]. The turn cannot end while any declared check is failing or has no evidence — run the commands so the harness records the results.")
	return b.String()
}

// writeFixture materializes a task's inline file set into dir.
func writeFixture(dir string, fixture map[string]string) error {
	for path, content := range fixture {
		// Fixture paths are forward-slash relative; rooting the clean at "/"
		// and joining keeps a bad path from ever escaping the workspace.
		dst := filepath.Join(dir, filepath.Clean("/"+path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("golden: %s: %w", path, err)
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return fmt.Errorf("golden: %s: %w", path, err)
		}
	}
	return nil
}

func ensureEvalRuns(db *sql.DB) error {
	if db == nil {
		return errors.New("eval: db is required")
	}
	if _, err := db.Exec(evalRunsSchema); err != nil {
		return fmt.Errorf("eval: eval_runs schema: %w", err)
	}
	return nil
}

func insertEvalRun(db *sql.DB, task, variant, model string, res GoldenTaskResult) error {
	green := 0
	if res.Green {
		green = 1
	}
	_, err := db.Exec(`INSERT INTO eval_runs
                (task, variant, model, rounds_to_green, rollbacks, stalls, tokens, exit_code, green, session_id, at)
                VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		task, variant, model, res.RoundsToGreen, res.Rollbacks, res.Stalls,
		res.Tokens, res.ExitCode, green, res.SessionID,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}
