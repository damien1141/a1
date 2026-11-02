package eval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/damien1141/a1/internal/agent"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools"

	"github.com/damien1141/a1/internal/vault"
)

// ReplayOptions configures `a1 eval replay`: a stored session re-run fresh
// (its original first prompt, the current workspace) under a mutated tool
// set, then scored with the same offline scorer for a before/after read.
type ReplayOptions struct {
	// Session is the stored session id (exact or unique prefix).
	Session string
	// Tools is the comma-separated built-in tool subset ("" = all defaults,
	// matching `a1 run --tools`).
	Tools string
	// MaxRounds caps tool rounds (0 = engine default).
	MaxRounds int
	// Model overrides the configured default model name ("" = default).
	Model string
}

// ReplayReport is the before/after pair the command prints.
type ReplayReport struct {
	Before EvalResult `json:"before"`
	After  EvalResult `json:"after"`
	// Exit is the replay loop's exit code (0 ok, 1 error, 2 max rounds —
	// the `a1 run` contract).
	Exit int `json:"exit"`
	// Err is the terminal loop error, if any (already reflected in Exit).
	Err string `json:"error,omitempty"`
}

// Replay re-runs a stored session's original prompt against a mutated
// configuration and scores both sessions. It needs a configured model with
// an API key: this is the only part of the eval surface that spends tokens.
func Replay(ctx context.Context, opts ReplayOptions) (ReplayReport, error) {
	proj, cfg, err := bootConfigured()
	if err != nil {
		return ReplayReport{}, err
	}

	before, err := scoreStoredSession(proj, opts.Session)
	if err != nil {
		return ReplayReport{}, err
	}

	model := cfg.Model()
	if opts.Model != "" {
		m, ok := cfg.FindModel(opts.Model)
		if !ok {
			return ReplayReport{}, fmt.Errorf("eval replay: model %q not configured", opts.Model)
		}
		model = m
	}

	subset, err := SelectBuiltinTools(opts.Tools)
	if err != nil {
		return ReplayReport{}, err
	}

	// Isolated session dir: the replay must not pollute the project's
	// session listing, and its evidence receipts must not mix into the
	// original scope — the after-score would otherwise count the original
	// session's verified checks.
	replayDir := filepath.Join(proj.Global().Root(), "eval", "sessions")
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		return ReplayReport{}, fmt.Errorf("eval replay: %w", err)
	}

	cwd, _ := os.Getwd()
	gate, err := headlessGate(cfg, cwd)
	if err != nil {
		return ReplayReport{}, err
	}

	engineOpts := []agent.EngineOption{
		agent.WithGate(gate),
		agent.WithTools(subset),
		agent.WithEvidenceChecks(cfg.EvidenceChecks()),
	}
	if opts.MaxRounds > 0 {
		engineOpts = append(engineOpts, agent.WithMaxRounds(opts.MaxRounds))
	}
	// Note: no extension runner and no MCP pool. A replay is a clean-room
	// rerun of the built-in stack — user-installed extensions would make
	// before/after comparisons about the extension set, not the config
	// mutation under test.

	sess, err := agent.NewSession(
		agent.WithCwd(cwd),
		agent.WithSessionDir(replayDir),
		agent.WithPersist(true),
	)
	if err != nil {
		return ReplayReport{}, fmt.Errorf("eval replay: %w", err)
	}
	engine, err := agent.NewEngine(model, sess, engineOpts...)
	if err != nil {
		return ReplayReport{}, fmt.Errorf("eval replay: %w", err)
	}

	exit, loopErr := drainLoop(ctx, engine, before.Prompt)

	// SessionFile is the vault-or-file identity path; OpenSession resolves
	// it back to the same tree the engine persisted.
	after, err := ScoreFile(engine.SessionFile(), model.Name)
	if err != nil {
		return ReplayReport{Before: before, Exit: exit}, err
	}
	report := ReplayReport{Before: before, After: after, Exit: exit}
	if loopErr != nil {
		report.Err = loopErr.Error()
	}
	return report, nil
}

// bootConfigured is the replay/golden bootstrap: default project + config
// + vault. internal/eval cannot import cmd (where loadRunBootstrap lives),
// so this replicates the minimal startup the headless runner performs:
// config must yield a model with an API key, and the vault boots (failure
// degrades to the file layout, same as every surface).
func bootConfigured() (*project.Project, *project.Config, error) {
	proj := project.GetDefaultProject()
	if err := proj.LoadConfig(); err != nil {
		return nil, nil, fmt.Errorf("eval: a configured model is required: %w", err)
	}
	cfg := proj.Config()
	m := cfg.Model()
	if m.Name == "" || m.APIKey == "" {
		return nil, nil, errors.New("eval: a configured model with an api_key is required (a1 config)")
	}
	if v, err := vault.Boot(proj.Global().Root()); err == nil {
		session.SetDefaultVault(v)
	}
	return proj, cfg, nil
}

// headlessGate replicates cmd.HeadlessGate: an empty policy mode defaults
// to headless-strict so Ask decisions fold to Deny; dangerously_allow_all
// is honored exactly like `a1 run`.
func headlessGate(cfg *project.Config, cwd string) (permission.Gate, error) {
	policy := cfg.Permissions
	if policy.Mode == "" {
		policy.Mode = permission.ModeHeadlessStrict
	}
	if policy.DangerouslyAllowAll {
		return permission.AllowAll{}, nil
	}
	return permission.NewGateAt(policy, cwd)
}

// scoreStoredSession resolves a session id in the project's session dir and
// scores it.
func scoreStoredSession(proj *project.Project, id string) (EvalResult, error) {
	path, err := session.FindSessionFile(proj.SessionDir(), id)
	if err != nil {
		return EvalResult{}, fmt.Errorf("eval replay: %w", err)
	}
	return ScoreFile(path, "")
}

// drainLoop consumes the engine loop like cmd/runloop's runLoop, mapping
// the terminal error to the `a1 run` exit-code contract.
func drainLoop(ctx context.Context, engine *agent.Engine, prompt string) (int, error) {
	exit := 0
	for ev, err := range engine.Loop(ctx, prompt, agent.LoopOpts{}) {
		if err != nil {
			if errors.Is(err, agent.ErrMaxRounds) {
				exit = 2
			} else {
				exit = 1
			}
			return exit, err
		}
		_ = ev
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return 1, ctxErr
	}
	return exit, nil
}

// SelectBuiltinTools resolves a comma-separated built-in tool subset, the
// same contract as `a1 run --tools` (a local copy of cmd's helper —
// internal/eval cannot import cmd, and exporting the cmd one would widen
// the wrong package). Empty input selects the full default set.
func SelectBuiltinTools(raw string) ([]tools.Tool, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	requested := make(map[string]struct{})
	for part := range strings.SplitSeq(raw, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			return nil, errors.New("tool list contains an empty name")
		}
		requested[name] = struct{}{}
	}
	defaults := tools.DefaultTools()
	available := make([]string, 0, len(defaults))
	selected := make([]tools.Tool, 0, len(requested))
	// Default schema order regardless of flag order; duplicates collapse.
	for _, tool := range defaults {
		name := tool.Definition.Name
		available = append(available, name)
		if _, ok := requested[name]; ok {
			selected = append(selected, tool)
			delete(requested, name)
		}
	}
	if len(requested) > 0 {
		return nil, fmt.Errorf("unknown built-in tool(s): %s (available: %s)",
			strings.Join(keys(requested), ", "), strings.Join(available, ", "))
	}
	return selected, nil
}

func keys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
