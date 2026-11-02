package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	cli "github.com/pulseaiclub/pli"

	"github.com/damien1141/a1/internal/eval"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/telemetry"

	"github.com/damien1141/a1/internal/vault"
)

// evalCommand is the Mirror (Bet 07): score stored sessions offline, replay
// them with mutated configs, and run the golden set. The default mode is a
// pure read path — no LLM, no workspace writes.
var evalCommand = cli.Command{
	Name: "eval",
	Desc: "score stored sessions (offline) and replay them with mutations",
	Long: `a1 eval                 score every stored session for this directory
a1 eval --json          machine-readable report
a1 eval --session ID    score one session
a1 eval replay          re-run a stored session's prompt with a tool subset
a1 eval golden          run the pinned golden set against the configured model

Scoring is offline (no LLM): session trees joined with evidence receipts.
rounds-to-green counts verification rounds until the recovered contract
went green; rollback rate is harness reversion receipts over verification
rounds; stall rate is evidence stop notes over governed turns.`,
	Flags: []cli.Flag{
		cli.Bool("json", "", "emit JSON instead of a table"),
		cli.String("session", "", "score only this session (id or unique prefix)", ""),
	},
	Run: runEval,
}

var evalReplayCommand = cli.Command{
	Name: "replay",
	Desc: "re-run a stored session's original prompt under a mutated config",
	Long: `Re-runs the session's first user prompt in a fresh session (the current\nworkspace), then scores before/after with the same offline scorer.\nRequires a configured model with an API key — this is the only eval\nmode that spends tokens.`,
	Flags: []cli.Flag{
		cli.String("session", "", "session id or unique prefix (required)", ""),
		cli.String("tools", "", "enable only these built-in tools (comma-separated)", ""),
		cli.Var("max-rounds", "", "N", "cap tool rounds", 0, parsePositiveInt),
		cli.String("model", "", "model name override (default: the configured default)", ""),
	},
}

var evalGoldenCommand = cli.Command{
	Name: "golden",
	Desc: "run the pinned golden set against the configured model",
	Long: `Runs each golden task in a fresh workspace, declares its contract\nchecks, and scores rounds-to-green / tokens / exit code. Results land in\nthe vault's eval_runs table. Without a configured model this prints a\nnotice and exits 0, so CI without secrets stays green.`,
	Flags: []cli.Flag{
		cli.String("only", "", "comma-separated task ids (default: all)", ""),
		cli.String("model", "", "model name override (default: the configured default)", ""),
		cli.Var("max-rounds", "", "N", "cap tool rounds per task", 0, parsePositiveInt),
		cli.String("variant", "", "label for this run's configuration (eval_runs.variant)", "default"),
		cli.String("dir", "", "base dir for workspaces and sessions (default: temp dir)", ""),
	},
}

func init() {
	evalReplayCommand.Run = runEvalReplay
	evalGoldenCommand.Run = runEvalGolden
	evalCommand.Add(&evalReplayCommand, &evalGoldenCommand)
}

// runEval is the offline scorer: sessions + evidence receipts in, metrics
// out. The vault boots read-side so vault-backed sessions resolve, and the
// telemetry sink attaches so scored turns materialize into the store
// (Bet 07: eval is the turns table's writer, never the hot path).
func runEval(_ []string, f cli.Flags) error {
	proj := project.GetDefaultProject()
	// Config errors are tolerated: scoring is a pure read path and must work
	// on a machine with no model configured (CI included).
	_ = proj.LoadConfig()

	if v, err := vault.Boot(proj.Global().Root()); err == nil {
		defer v.Close()
		session.SetDefaultVault(v)
		if err := telemetry.AttachDB(v.DB()); err != nil {
			fmt.Fprintf(os.Stderr, "warning: telemetry sink: %v\n", err)
		}
	}

	var results []eval.EvalResult
	var err error
	if id := strings.TrimSpace(f.String("session")); id != "" {
		path, perr := session.FindSessionFile(proj.SessionDir(), id)
		if perr != nil {
			return fmt.Errorf("a1 eval: %w", perr)
		}
		var res eval.EvalResult
		res, err = eval.ScoreFile(path, "")
		if err == nil {
			results = append(results, res)
		}
	} else {
		results, err = eval.ScoreDir(proj.SessionDir(), "")
	}
	if err != nil {
		return fmt.Errorf("a1 eval: %w", err)
	}

	// The write step: materialize turns + skill outcomes into the vault
	// when it is live. Scored rows are idempotent (keyed session, at).
	if v := session.DefaultVault(); v != nil {
		for _, res := range results {
			if perr := eval.Persist(res, v.DB()); perr != nil {
				fmt.Fprintf(os.Stderr, "warning: persist %s: %v\n", res.SessionID, perr)
			}
		}
	}

	if f.Bool("json") {
		return printEvalJSON(results)
	}
	printEvalTable(results)
	return nil
}

func runEvalReplay(_ []string, f cli.Flags) error {
	opts := eval.ReplayOptions{
		Session:   strings.TrimSpace(f.String("session")),
		Tools:     f.String("tools"),
		MaxRounds: f.Int("max-rounds"),
		Model:     strings.TrimSpace(f.String("model")),
	}
	if opts.Session == "" {
		return evalReplayCommand.Usagef("--session is required (id or unique prefix)")
	}
	report, err := eval.Replay(context.Background(), opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "a1 eval replay:", err)
		return exitCode(ExitUsage)
	}
	fmt.Fprintf(os.Stderr, "before: %s\n", formatEvalRow(report.Before))
	fmt.Fprintf(os.Stderr, "after:  %s\n", formatEvalRow(report.After))
	if report.Err != "" {
		fmt.Fprintf(os.Stderr, "replay error: %s (exit %d)\n", report.Err, report.Exit)
	}
	return exitCode(report.Exit)
}

func runEvalGolden(_ []string, f cli.Flags) error {
	var only []string
	if raw := strings.TrimSpace(f.String("only")); raw != "" {
		for part := range strings.SplitSeq(raw, ",") {
			if id := strings.TrimSpace(part); id != "" {
				only = append(only, id)
			}
		}
	}
	report, err := eval.RunGolden(context.Background(), eval.GoldenOptions{
		Only:      only,
		Model:     strings.TrimSpace(f.String("model")),
		MaxRounds: f.Int("max-rounds"),
		Variant:   f.String("variant"),
		BaseDir:   f.String("dir"),
	})
	if errors.Is(err, eval.ErrNoModel) {
		// Deliberate exit 0: CI without secrets must stay green; the notice
		// says what would happen with a model.
		fmt.Println("golden set needs a configured model")
		return nil
	}
	if err != nil {
		return fmt.Errorf("a1 eval golden: %w", err)
	}

	w := 0
	for _, r := range report.Results {
		if len(r.Task.ID) > w {
			w = len(r.Task.ID)
		}
	}
	var green, failed int
	var tokens int64
	for _, r := range report.Results {
		status := "green"
		if !r.Green {
			status = "not-green"
			if r.Err != "" {
				status = "error"
				failed++
			}
		} else {
			green++
		}
		tokens += r.Tokens
		fmt.Printf("%-*s  %-9s  r2g %2d  rollb %2d  stalls %2d  tokens %7d  exit %d",
			w, r.Task.ID, status, r.RoundsToGreen, r.Rollbacks, r.Stalls, r.Tokens, r.ExitCode)
		if r.Err != "" {
			fmt.Printf("  (%s)", firstLine(r.Err))
		}
		fmt.Println()
	}
	fmt.Println(strings.Repeat("-", w+70))
	fmt.Printf("%-*s  %d/%d green, %d failed, %d tokens, variant %q, model %s\n",
		w, "TOTAL", green, len(report.Results), failed, tokens, report.Variant, report.Model)
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

// printEvalJSON emits the machine-readable report.
func printEvalJSON(results []eval.EvalResult) error {
	out := struct {
		Sessions []eval.EvalResult `json:"sessions"`
		Totals   eval.Totals       `json:"totals"`
	}{results, eval.Summarize(results)}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// printEvalTable renders the human report in the toolaudit table style:
// fixed columns, a dashed separator, a TOTAL footer.
func printEvalTable(results []eval.EvalResult) {
	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "no scored sessions in this directory\n")
		return
	}
	fmt.Printf("%-16s  %-9s  %3s  %5s  %8s  %5s  %9s  %4s  %s\n",
		"SESSION", "GREEN", "R2G", "VERIF", "ROLLBACK", "STALL", "TOKENS", "COMP", "PROMPT")
	for _, r := range results {
		fmt.Println(formatEvalRow(r))
	}
	idw := 16
	fmt.Println(strings.Repeat("-", idw+62))
	t := eval.Summarize(results)
	fmt.Printf("%-16s  %9s  %3d  %5d  %8s  %5s  %9d  %4d\n",
		"TOTAL", fmt.Sprintf("%d/%d green", t.Green, t.Sessions),
		t.RoundsToGreen, t.VerificationRounds, "-", "-", t.TotalTokens, t.Compactions)
}

// formatEvalRow renders one compact row (shared by the table and replay).
func formatEvalRow(r eval.EvalResult) string {
	green := "not-green"
	if r.Green {
		green = "green"
	}
	prompt := strings.ReplaceAll(strings.TrimSpace(r.Prompt), "\n", " ")
	if len(prompt) > 44 {
		prompt = prompt[:44] + "…"
	}
	contract := strings.Join(r.Contract, ",")
	if len(contract) > 0 {
		if len(contract) > 30 {
			contract = contract[:30] + "…"
		}
		prompt = prompt + "  [" + contract + "]"
	}
	return fmt.Sprintf("%-16s  %-9s  %3d  %5d  %8.2f  %5.2f  %9d  %4d  %s",
		truncateID(r.SessionID), green, r.RoundsToGreen, r.VerificationRounds,
		r.RollbackRate, r.StallRate, r.TotalTokens, r.Compactions, prompt)
}

func truncateID(id string) string {
	if len(id) > 16 {
		return id[:16] + "…"
	}
	return id
}
