package agent

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/damien1141/a1/internal/debuglog"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/llm/client"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools"
	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/toolguard"
)

// The review fork (hermes' build_cache_parity_fork): a detached conversation
// that reuses the parent's model, system prompt, tool schema and transcript
// snapshot so the request PREFIX is byte-identical — whatever prefix caching
// the provider does treats the replay as the warm hit it is (hermes measures
// ~26% off their fork cost; the same trick, whatever the provider) — while
// dispatch is restricted to the learning tools. Advertising is not
// permission.
//
// The fork is NOT an Engine.Loop: no turn machinery (contracts, budgets,
// verify gate, compaction), no session persistence (its rows live only in
// this call's memory), and no recursion by construction — nothing in the
// fork ticks a nudge counter or fires another review. It is a small
// stream→tools→stream driver over the same Executor the engine uses, so
// every dispatch guard (malformed args, loop guardrails, the whitelist) and
// every tool result shape the model already knows how to read apply
// unchanged.

// learningReviewMaxRounds bounds the fork's model rounds (hermes'
// _REVIEW_MAX_ITERATIONS = 16).
const learningReviewMaxRounds = 16

// learningReviewCallBudget bounds the fork's total EXECUTED tool calls.
// Refusals (whitelist denials included) never claim a slot, so a confused
// fork that hammers denied tools spends rounds, not calls.
const learningReviewCallBudget = 64

// forkReadMarks tracks which files the fork has actually read, enforcing
// read-before-write on existing files: a patch written against remembered
// content instead of the file's current bytes is exactly the blind overwrite
// hermes' guard exists to stop (#61521's sibling lesson — content quoted in
// the transcript is stale by definition once anyone else wrote to the file).
type forkReadMarks struct {
	mu    sync.Mutex
	paths map[string]bool
}

func (m *forkReadMarks) mark(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.paths == nil {
		m.paths = map[string]bool{}
	}
	m.paths[path] = true
}

func (m *forkReadMarks) wasRead(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paths[path]
}

// learningReviewPromptData renders prompt/learning-review.tmpl.
type learningReviewPromptData struct {
	ReviewMemory bool
	ReviewSkills bool
	SkillRoot    string
}

//go:embed prompt/learning-review.tmpl
var learningReviewTmpl string

var learningReviewTemplate = template.Must(template.New("learning-review").Parse(learningReviewTmpl))

// buildLearningReviewPrompt renders the review instruction. Its capability
// notice is kept in sync with the deny message below (hermes keeps the two
// wordings aligned so the model never burns a round discovering the
// boundary the hard way).
func buildLearningReviewPrompt(data learningReviewPromptData) (string, error) {
	var sb strings.Builder
	if err := learningReviewTemplate.Execute(&sb, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// skillsRootAbsolute resolves the configured skills directory to an
// absolute path for containment checks (the write wrapper compares
// resolved target paths against it).
func (engine *Engine) skillsRootAbsolute() string {
	root := strings.TrimSpace(engine.modelCfg.SkillPath)
	if root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(root)
}

// pathUnderDir reports whether path (absolute or cwd-relative) resolves
// inside dir. Symlinks are not resolved on either side — the skills root is
// operator configuration, not an attack surface, and the check exists to
// keep an unsupervised fork honest about scope, not to defend against it.
func pathUnderDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(dir, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// wrapForkRead wraps the read tool for the fork's registry: on success the
// resolved path is marked read, feeding the read-before-write guard. The
// advertised Definition is untouched — parity is the whole point.
func wrapForkRead(base tooldef.Tool, marks *forkReadMarks) tooldef.Tool {
	out := base
	inner := base.Run
	out.Run = func(ctx context.Context, args json.RawMessage) (tooldef.Result, error) {
		res, err := inner(ctx, args)
		if err == nil {
			var in struct {
				Path string `json:"path"`
			}
			if json.Unmarshal(args, &in) == nil && strings.TrimSpace(in.Path) != "" {
				if resolved, rerr := tooldef.ResolveToCwd(ctx, in.Path); rerr == nil {
					marks.mark(resolved)
				}
			}
		}
		return res, err
	}
	return out
}

// wrapForkWrite wraps the write tool (which folds edit) for the fork's
// registry with two guards:
//
//  1. Scope: writes must land under the skills root. The fork edits SKILL.md
//     files; it has no business touching the workspace, dotfiles, or config.
//  2. Read-before-write: an EXISTING target must have been read during this
//     review (the mark set by wrapForkRead, or by a prior successful write
//     refreshing the fork's own knowledge). Creating a new file is free.
//
// Both refusals ride the tool's own error path so the model sees them as
// ordinary tool results, in the executor's established refusal voice.
func wrapForkWrite(base tooldef.Tool, marks *forkReadMarks, skillRoot string) tooldef.Tool {
	out := base
	inner := base.Run
	out.Run = func(ctx context.Context, args json.RawMessage) (tooldef.Result, error) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Path) == "" {
			// Not a shape the wrapper can judge — let the tool report its
			// own parse error in its own voice.
			return inner(ctx, args)
		}
		path, err := tooldef.ResolveToCwd(ctx, in.Path)
		if err != nil {
			return inner(ctx, args)
		}
		if !pathUnderDir(path, skillRoot) {
			return tooldef.Result{}, fmt.Errorf(
				"write to %s was not executed — the background review may only write inside the skills directory (%s). "+
					"Put the learning in a skill there, or state it in your reply instead",
				tooldef.RelToCwd(ctx, path), skillRoot,
			)
		}
		if _, statErr := os.Stat(path); statErr == nil && !marks.wasRead(path) {
			return tooldef.Result{}, fmt.Errorf(
				"write to %s was not executed — read-before-write: the file exists but was not read during this review, "+
					"and content quoted in the conversation may be stale. Read %s once, then re-issue the write based on what it returned",
				tooldef.RelToCwd(ctx, path), tooldef.RelToCwd(ctx, path),
			)
		}
		res, err := inner(ctx, args)
		if err == nil {
			// The fork now holds the file's true post-write state.
			marks.mark(path)
		}
		return res, err
	}
	return out
}

// forkMemoryRecorder captures successful memory ops for the action summary.
type forkMemoryRecorder struct {
	mu    sync.Mutex
	added []string
	fixed []string
}

func (r *forkMemoryRecorder) recordAdded(content string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.added = append(r.added, firstLine(content, 72))
}

func (r *forkMemoryRecorder) recordFixed(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fixed = append(r.fixed, id)
}

// wrapForkRemember / wrapForkRevise pass through to the memory tools,
// recording successes. Recall needs no wrapper (read-only, not an action);
// forget is not whitelisted at all — the fork adds and fixes, it never
// destroys.
func wrapForkRemember(base tooldef.Tool, rec *forkMemoryRecorder) tooldef.Tool {
	out := base
	inner := base.Run
	out.Run = func(ctx context.Context, args json.RawMessage) (tooldef.Result, error) {
		res, err := inner(ctx, args)
		if err == nil {
			var in struct {
				Content string `json:"content"`
			}
			_ = json.Unmarshal(args, &in)
			rec.recordAdded(in.Content)
		}
		return res, err
	}
	return out
}

func wrapForkRevise(base tooldef.Tool, rec *forkMemoryRecorder) tooldef.Tool {
	out := base
	inner := base.Run
	out.Run = func(ctx context.Context, args json.RawMessage) (tooldef.Result, error) {
		res, err := inner(ctx, args)
		if err == nil {
			var in struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(args, &in)
			rec.recordFixed(in.ID)
		}
		return res, err
	}
	return out
}

func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

// forkDenyMessage renders the whitelist denial, naming the substitutes so
// one denial redirects instead of starting a storm (hermes #61521: ~142
// denials over two days on one deployment before the message learned to
// point at the alternative).
func forkDenyMessage(tool string, skillRoot string, memoryLive bool) string {
	var subs []string
	subs = append(subs, "read/search to inspect")
	if skillRoot != "" {
		subs = append(subs, "list_skills to browse, write (mode=write|edit, under "+skillRoot+" only) to change skills")
	}
	if memoryLive {
		subs = append(subs, "remember/revise to store or fix a memory")
	}
	return fmt.Sprintf(
		"tool '%s' was not executed — the background review can only use learning tools: %s. Do not retry '%s'.",
		tool, strings.Join(subs, "; "), tool,
	)
}

// runLearningReview drives the fork: build the cache-parity client and the
// guarded executor, replay the snapshot with the review instruction as the
// new user message, and let the model run the learning tools. Everything is
// best-effort and silent to the user: a failed review logs and dies, and
// the next window re-arms.
func (engine *Engine) runLearningReview(ctx context.Context, snapshot []llm.Message, reviewMemory, reviewSkills bool) {
	skillRoot := ""
	if reviewSkills {
		skillRoot = engine.skillsRootAbsolute()
	}

	// Cache parity: the same tool list the parent's client advertises
	// (buildToolList is deterministic given the engine state) and the same
	// system prompt builder — the fork's request differs from the parent's
	// last request ONLY by the trailing review message, which is exactly the
	// shape of a next turn.
	toolList := engine.buildToolList(engine.baseTools)
	forkClient := client.NewClient(
		engine.modelCfg,
		engine.hooks,
		tools.Definitions(toolList),
		engine.systemPrompt(),
	)

	// The guarded registry: same tools, read/write wrapped for the fork's
	// containment rules, memory tools wrapped for the action summary.
	marks := &forkReadMarks{}
	memRec := &forkMemoryRecorder{}
	registryTools := make([]tooldef.Tool, len(toolList))
	for i, t := range toolList {
		switch t.Definition.Name {
		case "read":
			t = wrapForkRead(t, marks)
		case "write":
			if skillRoot != "" {
				t = wrapForkWrite(t, marks, skillRoot)
			}
		case "remember":
			if reviewMemory {
				t = wrapForkRemember(t, memRec)
			}
		case "revise":
			if reviewMemory {
				t = wrapForkRevise(t, memRec)
			}
		}
		registryTools[i] = t
	}

	// Whitelist: everything the review may dispatch. Read/search are
	// unrestricted on purpose (hermes learned this the hard way — denying
	// them caused the storm); the write wrapper is the confinement. Forget
	// is absent: an unattended pass never destroys.
	whitelist := map[string]bool{
		"read":   true,
		"search": true,
	}
	if skillRoot != "" {
		whitelist["list_skills"] = true
		whitelist["write"] = true
	}
	if reviewMemory {
		whitelist["remember"] = true
		whitelist["recall"] = true
		whitelist["revise"] = true
	}

	// A bare executor: AllowAll gate (the whitelist IS the policy — a
	// permission Ask would deadlock an unattended fork), no ask handler, no
	// extensions, no evidence ledger, no memory bank. Its own loop-guard
	// controller catches a fork hammering one tool.
	forkExec := NewExecutor(tools.NewRegistry(registryTools), nil, nil, nil, nil)
	forkExec.SetMeta("", engine.SessionCwd())
	forkExec.SetLoopGuard(toolguard.New(toolguard.Config{Warnings: true, HardStop: true}))
	forkExec.SetImageEnabled(engine.modelCfg.ImageEnabled)
	forkExec.SetDispatchWhitelist(whitelist, func(tool string) string {
		return forkDenyMessage(tool, skillRoot, reviewMemory)
	})

	prompt, err := buildLearningReviewPrompt(learningReviewPromptData{
		ReviewMemory: reviewMemory,
		ReviewSkills: reviewSkills,
		SkillRoot:    skillRoot,
	})
	if err != nil {
		debuglog.Logf("learning-review: prompt render failed: %v", err)
		return
	}

	msgs := append(snapshot, llm.Message{Role: llm.RoleUser, Content: prompt})
	callsUsed := 0
	rounds := 0
	var skillWrites []string
	emit := func(session.ToolData) bool { return true }

	for rounds < learningReviewMaxRounds {
		if ctx.Err() != nil {
			debuglog.Logf("learning-review: fork cancelled after %d rounds", rounds)
			return
		}
		msg, err := forkStreamRound(ctx, forkClient, msgs)
		if err != nil {
			if ctx.Err() != nil {
				debuglog.Logf("learning-review: fork cancelled after %d rounds", rounds)
			} else {
				debuglog.Logf("learning-review: stream failed on round %d: %v", rounds+1, err)
			}
			return
		}
		rounds++
		if len(msg.ToolCalls) == 0 {
			engine.logReviewSummary(rounds, msg.Content, memRec, skillWrites)
			return
		}
		remaining := learningReviewCallBudget - callsUsed
		if remaining <= 0 {
			debuglog.Logf("learning-review: call budget (%d) exhausted after %d rounds", learningReviewCallBudget, rounds)
			return
		}
		forkExec.SetCallAllowance(remaining)
		toolMsgs, stop, _ := forkExec.Run(ctx, msg.ToolCalls, emit)
		callsUsed += forkExec.AllowanceUsed()
		msgs = append(msgs, msg)
		msgs = append(msgs, toolMsgs...)
		skillWrites = append(skillWrites, forkSkillWrites(marks, skillRoot)...)
		if stop {
			debuglog.Logf("learning-review: executor stopped the fork after %d rounds", rounds)
			return
		}
	}
	debuglog.Logf("learning-review: round budget (%d) exhausted", learningReviewMaxRounds)
}

// forkSkillWrites lists SKILL.md paths under the skills root that the fork
// has marked (read or written) this round-window — a cheap proxy for "which
// skills the fork touched", derived from the same marks the guards use
// rather than a second tracking structure.
func forkSkillWrites(marks *forkReadMarks, skillRoot string) []string {
	if skillRoot == "" {
		return nil
	}
	marks.mu.Lock()
	defer marks.mu.Unlock()
	var out []string
	for p := range marks.paths {
		if pathUnderDir(p, skillRoot) && strings.HasSuffix(p, "SKILL.md") {
			out = append(out, p)
		}
	}
	return out
}

// logReviewSummary reports what the fork did. The fork's writes ARE the
// learning output — they land in the stores the next session reads; this
// log line is the audit trail.
func (engine *Engine) logReviewSummary(rounds int, finalText string, memRec *forkMemoryRecorder, skillWrites []string) {
	memRec.mu.Lock()
	added, fixed := len(memRec.added), len(memRec.fixed)
	memRec.mu.Unlock()
	seen := map[string]bool{}
	var skills []string
	for _, p := range skillWrites {
		if !seen[p] {
			seen[p] = true
			skills = append(skills, p)
		}
	}
	if added == 0 && fixed == 0 && len(skills) == 0 {
		debuglog.Logf("learning-review: fork done in %d round(s) — nothing saved (%s)",
			rounds, firstLine(finalText, 120))
		return
	}
	debuglog.Logf("learning-review: fork done in %d round(s) — %d memory add(s), %d memory fix(es), %d skill file(s): %v",
		rounds, added, fixed, len(skills), skills)
	memRec.mu.Lock()
	for _, a := range memRec.added {
		debuglog.Logf("learning-review: memory+ %s", a)
	}
	memRec.mu.Unlock()
}

// forkStreamRound consumes one assistant stream silently — no UI events,
// no session rows; the fork's transcript lives in this call's slice only.
// The runaway watch rides along: a degenerate review reply is cut before it
// wastes the budget, and the looped bytes are never kept.
func forkStreamRound(ctx context.Context, forkClient *client.Client, msgs []llm.Message) (llm.Message, error) {
	var final llm.Message
	gotDone := false
	textWatch := llm.NewRunawayWatch()
	thinkWatch := llm.NewRunawayWatch()
	for event, err := range forkClient.Stream(ctx, msgs) {
		if err != nil {
			return llm.Message{}, err
		}
		switch event.Type {
		case llm.StreamEventTypeError:
			errText := event.Err
			if errText == "" {
				errText = "stream error"
			}
			return llm.Message{}, fmt.Errorf("%s", errText)
		case llm.StreamEventTypeDelta:
			if event.Delta.ReasoningContent != "" && thinkWatch.Feed(event.Delta.ReasoningContent) {
				return llm.Message{}, ErrRepetitionLoop
			}
			if event.Delta.Content != "" && textWatch.Feed(event.Delta.Content) {
				return llm.Message{}, ErrRepetitionLoop
			}
		case llm.StreamEventTypeDone:
			if event.Final == nil {
				return llm.Message{}, fmt.Errorf("stream finished with no assistant message")
			}
			final = *event.Final
			gotDone = true
		}
	}
	if !gotDone {
		return llm.Message{}, fmt.Errorf("stream closed without assistant output")
	}
	return final, nil
}
