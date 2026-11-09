package agent

import (
        "context"
        "encoding/json"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/debuglog"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/skillstats"
)

// The learning loop (stolen from hermes-agent's background_review.py family):
// two nudge counters that tick on OPPORTUNITY and reset on ACTUAL USE, and a
// post-turn review fork that fires when an interval elapses.
//
//   - turnsSinceMemory: +1 per user turn (Loop entry) while the memory
//     substrate exists (memStore + MemoryEnabled, i.e. the remember/recall
//     tools are live). Reset when the model actually dispatches a memory tool
//     (remember/recall/revise/forget) — the inlet's automatic injection does
//     NOT count, or the counter could never fire.
//   - itersSinceSkill: +1 per tool-calling iteration (model round) while the
//     skill substrate exists (SkillPath configured). Reset when the model
//     reads a SKILL.md or writes under the skills root.
//
// When a counter crosses its interval (default 10), the turn that crossed it
// ends with a background review fork (learning_fork.go): the SAME model,
// system prompt, tool schema and transcript snapshot — byte-identical prefix,
// so the provider's prompt cache treats the replay as a warm hit — with a
// dispatch whitelist that denies everything but the learning tools, each
// denial naming its substitute (hermes #61521: a bare denial storm starves
// the loop; a redirect costs one round).
//
// The counters are session-scoped engine state and deliberately NOT reset per
// turn (hermes' _PER_TURN_RESET_STATE explicitly excludes them): they carry
// across Loops within the engine's lifetime. They are in-memory only — a
// restarted session starts its windows fresh, which merely delays the next
// review, never duplicates one.

// defaultLearnNudgeInterval matches hermes' memory.nudge_interval and
// skills.creation_nudge_interval defaults: a review every 10 opportunities.
const defaultLearnNudgeInterval = 10

// learningReviewTimeout caps a review fork's lifetime. The fork replays a
// transcript the parent just proved fits, under a 16-round iteration cap and
// a bounded call budget, so this is a backstop against a wedged provider —
// not the primary bound. A live turn preempts the fork long before this in
// practice.
const learningReviewTimeout = 10 * time.Minute

// memorySubstrateLive reports whether the memory dimension of the loop is
// armed: the shared bank exists AND memory is effectively on (persona-aware,
// same gate that registers the remember/recall tools).
func (engine *Engine) memorySubstrateLive() bool {
        if !engine.MemoryEnabled() {
                return false
        }
        return engine.memStoreForActivePersona() != nil
}

// skillSubstrateLive reports whether the skill dimension is armed: a skills
// directory is configured (list_skills is registered against it).
func (engine *Engine) skillSubstrateLive() bool {
        return strings.TrimSpace(engine.modelCfg.SkillPath) != ""
}

// tickMemoryNudge advances the turns-since-memory counter at turn start.
// Returns true when the interval elapsed — the turn now in flight should end
// with a memory review — and resets the counter (hermes ticks and fires in
// one move; the review then examines the turn that crossed the line).
func (engine *Engine) tickMemoryNudge() bool {
        interval := engine.learnMemInterval.Load()
        if interval <= 0 || !engine.memorySubstrateLive() {
                return false
        }
        n := engine.turnsSinceMemory.Add(1)
        if n >= interval {
                engine.turnsSinceMemory.Store(0)
                return true
        }
        return false
}

// tickSkillIteration advances the iterations-since-skill counter once per
// model round (hermes ticks in prepare_iteration, before each API call —
// including the round that ends the turn without tool calls).
func (engine *Engine) tickSkillIteration() {
        interval := engine.learnSkillInterval.Load()
        if interval <= 0 || !engine.skillSubstrateLive() {
                return
        }
        engine.itersSinceSkill.Add(1)
}

// checkSkillNudge is the turn-end half of the skill counter: reports whether
// the interval elapsed across this turn's iterations and resets the counter.
// Like hermes, the check runs at every completed-turn terminal whether or not
// the fork can actually spawn — the window is spent either way, and the next
// one re-arms from zero.
func (engine *Engine) checkSkillNudge() bool {
        interval := engine.learnSkillInterval.Load()
        if interval <= 0 || !engine.skillSubstrateLive() {
                return false
        }
        if engine.itersSinceSkill.Load() >= interval {
                engine.itersSinceSkill.Store(0)
                return true
        }
        return false
}

// learningSink is the executor dispatch hook (SetLearningSink) that resets
// the counters on actual use — the hermes reset point, after every gate has
// passed and just before the tool runs. A refused call never reaches the
// sink: permission-denied memory calls and whitelist denials are not use.
func (engine *Engine) learningSink(tool string, args json.RawMessage) {
        switch tool {
        case "remember", "recall", "revise", "forget":
                engine.turnsSinceMemory.Store(0)
        case "read", "write":
                var in struct {
                        Path string `json:"path"`
                }
                if json.Unmarshal(args, &in) != nil || strings.TrimSpace(in.Path) == "" {
                        return
                }
                // A SKILL.md read is the consultation moment (skillstats'
                // PickFromReadPath is the same signal); a write under the skills
                // root is a change. Either resets the skill counter.
                if _, ok := skillstats.PickFromReadPath(in.Path); ok {
                        engine.itersSinceSkill.Store(0)
                        return
                }
                if root := engine.skillsRootAbsolute(); root != "" && pathUnderDir(in.Path, root) {
                        engine.itersSinceSkill.Store(0)
                }
        }
}

// maybeLearn fires the post-turn review fork when a nudge counter crossed.
// Called at every completed-turn terminal path (the same paths that fire the
// memory outlet, minus the repetition/empty terminals whose content is a
// placeholder): the normal wrap-up, the zero-rank stop, the declared-budget
// terminal, the verdict-gate stop, an executor stop, and the tool-call
// allowance trip. Turns aborted by error, cancellation, or a declined
// continue-ask never learn — the model never finished, so there is no
// exchange to review. Every caller runs AFTER the terminal's rows are
// appended, so the snapshot is always a valid request shape (an assistant
// tool-call message must never ride without its results).
func (engine *Engine) maybeLearn(fireMemory bool) {
        if engine == nil {
                return
        }
        fireSkill := engine.checkSkillNudge()
        if !fireMemory && !fireSkill {
                return
        }
        snapshot := engine.learningSnapshot()
        engine.fireLearningReview(snapshot, fireMemory, fireSkill)
}

// learningSnapshot clones the model-visible context (the compaction-cut
// path, exactly what the next turn would send). The clone is structural: the
// fork's request pipeline (wire-out sanitize, runaway watch) must never be
// able to mutate the live transcript's slices.
func (engine *Engine) learningSnapshot() []llm.Message {
        return cloneMessages(engine.session.BuildContext())
}

// cloneMessages deep-copies the slice headers that matter: ToolCalls and
// Images are slices a downstream sanitizer could otherwise write through.
func cloneMessages(msgs []llm.Message) []llm.Message {
        out := make([]llm.Message, len(msgs))
        for i, m := range msgs {
                if len(m.ToolCalls) > 0 {
                        m.ToolCalls = append([]llm.ToolCall(nil), m.ToolCalls...)
                }
                if len(m.Images) > 0 {
                        m.Images = append([]llm.Image(nil), m.Images...)
                }
                out[i] = m
        }
        return out
}

// cancelLearningReview preempts an in-flight review fork. Wired at Loop
// entry: self-improvement work must never overlap a live user turn (hermes
// bounds its preemption wait; a goroutine needs no wait at all — cancel and
// move on). A fork cut mid-write loses at most the current tool call; every
// write the fork makes is a whole-file write.
func (engine *Engine) cancelLearningReview() {
        engine.reviewCancelMu.Lock()
        cancel := engine.reviewCancel
        engine.reviewCancelMu.Unlock()
        if cancel != nil {
                cancel()
        }
}

// fireLearningReview spawns the background review fork (learning_fork.go)
// unless a gate says no. Gates, in order: nil engine / empty snapshot; one
// fork per engine (a second fire while one runs is skipped, not queued — the
// in-flight review covers the neighborhood and the next window re-arms);
// subagents never review (hermes' delegate-depth gate — the parent's review
// sees the fleet's work through its own transcript); substrate re-check
// (the tick gates could have gone stale mid-turn, e.g. memory toggled off).
//
// The fork runs detached from the Loop iterator: the turn has already
// returned to its consumer by the time this is called, so there is no yield
// channel left to speak into. Its actions surface in the debug log and in
// the stores themselves — the next session reads what it wrote.
func (engine *Engine) fireLearningReview(snapshot []llm.Message, reviewMemory, reviewSkills bool) {
        if engine == nil || len(snapshot) == 0 || (!reviewMemory && !reviewSkills) {
                return
        }
        if engine.agentDepth > 0 {
                return
        }
        if reviewMemory && !engine.memorySubstrateLive() {
                reviewMemory = false
        }
        if reviewSkills && !engine.skillSubstrateLive() {
                reviewSkills = false
        }
        if !reviewMemory && !reviewSkills {
                return
        }
        if !engine.reviewInFlight.CompareAndSwap(false, true) {
                debuglog.Logf("learning-review: skipping — a review is already in flight")
                return
        }

        ctx, cancel := context.WithTimeout(context.Background(), learningReviewTimeout)
        engine.reviewCancelMu.Lock()
        engine.reviewCancel = cancel
        engine.reviewCancelMu.Unlock()
        engine.reviewOutstanding.Add(1)

        memCount := engine.turnsSinceMemory.Load()
        skillCount := engine.itersSinceSkill.Load()
        debuglog.Logf("learning-review: firing fork (memory=%v skills=%v, snapshot=%d messages, residual counters mem=%d skill=%d)",
                reviewMemory, reviewSkills, len(snapshot), memCount, skillCount)

        go func() {
                defer func() {
                        engine.reviewCancelMu.Lock()
                        engine.reviewCancel = nil
                        engine.reviewCancelMu.Unlock()
                        cancel()
                        engine.reviewOutstanding.Add(-1)
                        engine.reviewInFlight.Store(false)
                }()
                engine.runLearningReview(ctx, snapshot, reviewMemory, reviewSkills)
        }()
}
