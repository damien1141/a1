// Package toolguard holds the pure per-turn tool-call loop guardrails,
// ported from hermes-agent's agent/tool_guardrails.py (itself carrying the
// issue numbers that paid for each rule: #89069, #100849, and the
// batch-cycle port of can1357/oh-my-pi#10521).
//
// The controller is side-effect free: it tracks per-turn tool-call
// observations and returns decisions. The executor decides whether a
// decision becomes warning guidance appended to a tool result, a synthetic
// refusal for an imminent call, or a controlled turn halt.
//
// Three failure tiers, all per turn (reset at every user prompt):
//
//   - exact-args failure: the SAME tool called with the SAME canonical
//     arguments keeps failing. Warn early, block when it is clearly a
//     replay loop.
//   - same-tool failure: one tool keeps failing with different arguments.
//     For failure-tolerant tools (bash, fetch) a run of distinct red
//     commands is diagnosis, not a loop — warn, never halt. For everything
//     else a long enough streak is a dead endpoint being ground against.
//   - no-progress: a read-only (idempotent) call returning the SAME result
//     over and over. The result is already in context; re-asking burns a
//     round and re-sends the payload.
//
// A successful MUTATING call resets the failure streaks: the retry after
// an edit is a new experiment (edit → re-run), not a replay. Pure loops
// never mutate between attempts, so the replay detector keeps its teeth.
//
// Two loop breakers run alongside the tiers:
//
//   - the identical-call streak: CONSECUTIVE identical (tool, args, result)
//     calls. Any different call or result resets it, so re-reads after
//     edits and varied polling are never flagged. Poller tools are exempt
//     from the NOTICE (an unchanged poll is legitimate waiting) but not
//     from result deduplication.
//   - batch cycles (A,B,A,B,… with identical args AND results), which
//     defeat the consecutive streak by resetting it on every alternation.
//
// From the 2nd byte-identical repeat, a large (≥512 chars) duplicate
// result is replaced by a reference stub: the tool still executed, only
// the context representation is deduplicated, so polling semantics
// survive.
package toolguard

import (
        "crypto/sha256"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "strings"
        "sync"
)

// Config arms the guardrail tiers. Warnings append guidance to tool
// results but never prevent execution. HardStop arms the blocking tier —
// opt-in on attended surfaces, on by default for unattended ones (job
// runners, schedules), matching hermes's platform split.
type Config struct {
        Warnings bool
        HardStop bool
}

// Thresholds, ported from hermes defaults.
const (
        exactFailureWarnAfter  = 2
        exactFailureBlockAfter = 5
        sameToolWarnAfter      = 3
        sameToolHaltAfter      = 8
        noProgressWarnAfter    = 2
        noProgressBlockAfter   = 5

        identicalCallThreshold = 3
        stubMinResultChars     = 512
        stubArgsPreviewChars   = 120

        maxCyclePeriod    = 4
        cycleHistoryDepth = 64

        defaultLoopCap = 50
)

// repeatableTools are legitimately re-invoked with identical args and
// identical results (pollers); the identical-call NOTICE never fires for
// them. Result stubs still apply — an unchanged poll is where a stub saves
// the most context.
var repeatableTools = map[string]bool{
        "agent_wait": true,
        "agent_list": true,
}

// failureTolerantTools: their "failure" is normal work output (a red test
// run, a fetch timeout). same-tool failure (DIFFERENT commands) never
// halts these; only an exact-args replay with no intervening change, or an
// identical-result streak, can.
var failureTolerantTools = map[string]bool{
        "bash":  true,
        "fetch": true,
}

// loopCapTools caps total per-turn calls (not repeats) of runaway-prone
// tools. The caps fire regardless of HardStop — a runaway fetch or spawn
// loop is a spend problem, not a judgment call.
var loopCapTools = map[string]bool{
        "fetch":       true,
        "agent_spawn": true,
}

// Decision is one guardrail verdict.
type Decision struct {
        // Action: "block" (refuse the imminent call), "warn" (append guidance),
        // or "halt" (stop the turn).
        Action string
        Code   string
        // Count is the streak/total the decision fired on.
        Count   int
        Message string
        Tool    string
}

// sig is the stable identity of one tool call: tool name plus a hash of
// its canonical (sorted-key, compact) arguments.
type sig struct {
        tool     string
        argsHash string
}

type noProgressRecord struct {
        resultHash string
        repeats    int
}

type cycleEntry struct {
        s          sig
        resultHash string
        repeatable bool
}

// Controller is the per-turn guardrail state. All methods are safe for
// concurrent use (read-only tool batches run calls in parallel).
type Controller struct {
        cfg Config
        mu  sync.Mutex

        exactFailures map[sig]int
        // progressSince marks signatures whose last failure was followed by a
        // successful mutation: their next failure restarts the streak.
        progressSince map[sig]bool
        toolFailures  map[string]int
        noProgress    map[sig]noProgressRecord

        streakSig   *sig
        streakHash  string
        streakCount int
        streakFirst string // call id of the streak's first call

        history []cycleEntry

        fetchCalls int
        spawnCalls int

        // halted records that this turn already produced a halt decision;
        // the first halt wins and later tiers must not pile on.
        halted bool
}

// New builds a controller with the given config.
func New(cfg Config) *Controller {
        return &Controller{cfg: cfg}
}

// Reset clears all per-turn state (call it at every user prompt).
func (c *Controller) Reset() {
        c.mu.Lock()
        defer c.mu.Unlock()
        c.exactFailures = nil
        c.progressSince = nil
        c.toolFailures = nil
        c.noProgress = nil
        c.streakSig = nil
        c.streakHash = ""
        c.streakCount = 0
        c.streakFirst = ""
        c.history = nil
        c.fetchCalls = 0
        c.spawnCalls = 0
        c.halted = false
}

// Before judges an imminent call. A non-nil decision is a "block": the
// call should be refused with the decision's message as the tool result.
// Loop caps fire regardless of HardStop; the failure tiers only when it
// is armed.
func (c *Controller) Before(tool, args string, readable bool) *Decision {
        c.mu.Lock()
        defer c.mu.Unlock()
        s := makeSig(tool, args)
        if d := c.checkLoopCap(tool, s); d != nil {
                return d
        }
        if !c.cfg.HardStop {
                return nil
        }
        if cnt := c.exactCount(s); cnt >= exactFailureBlockAfter {
                return &Decision{
                        Action: "block", Code: "repeated_exact_failure_block",
                        Tool: tool, Count: cnt,
                        Message: fmt.Sprintf(
                                "Blocked %s: the same call failed %d times with identical arguments. "+
                                        "Stop retrying it unchanged; change strategy or report the blocker.",
                                tool, cnt),
                }
        }
        if readable {
                if rec, ok := c.noProgress[s]; ok && rec.repeats >= noProgressBlockAfter {
                        return &Decision{
                                Action: "block", Code: "idempotent_no_progress_block",
                                Tool: tool, Count: rec.repeats,
                                Message: fmt.Sprintf(
                                        "Blocked %s: this read-only call returned the same result %d times. "+
                                                "Use the result already provided or change the query instead of repeating it.",
                                        tool, rec.repeats),
                        }
                }
        }
        return nil
}

// checkLoopCap refuses the (cap+1)-th call of a capped tool this turn.
func (c *Controller) checkLoopCap(tool string, s sig) *Decision {
        if !loopCapTools[tool] {
                return nil
        }
        switch tool {
        case "fetch":
                c.fetchCalls++
                if c.fetchCalls > defaultLoopCap {
                        return &Decision{
                                Action: "block", Code: "loop_fetch_cap",
                                Tool: tool, Count: c.fetchCalls - 1,
                                Message: fmt.Sprintf(
                                        "Blocked fetch: this turn has already made %d web fetches, the per-turn limit. "+
                                                "This looks like a runaway fetch loop. Work with the results you already have and give the user your answer.",
                                        c.fetchCalls-1),
                        }
                }
        case "agent_spawn":
                c.spawnCalls++
                if c.spawnCalls > defaultLoopCap {
                        return &Decision{
                                Action: "block", Code: "loop_subagent_cap",
                                Tool: tool, Count: c.spawnCalls - 1,
                                Message: fmt.Sprintf(
                                        "Blocked agent_spawn: this turn has already spawned %d subagents (limit %d). "+
                                                "This looks like a runaway delegation loop. Finish the work with the results you have and answer the user.",
                                        c.spawnCalls-1, defaultLoopCap),
                        }
                }
        }
        return nil
}

// Report folds one completed call into the guard. result is the content
// the model sees (raw output or refusal text); callID identifies the call
// for stub references; failed marks a refused or errored call; readable
// marks a side-effect-free tool. The return carries, in order of
// severity: a warn/halt decision (nil when none), an identical-call or
// cycle notice ("" when none), and a result-reference stub ("" when the
// result should pass through unchanged).
func (c *Controller) Report(tool, args, result, callID string, failed, readable bool) (*Decision, string, string) {
        c.mu.Lock()
        defer c.mu.Unlock()
        s := makeSig(tool, args)
        rHash := resultHash(result)

        var decision *Decision
        if failed {
                // An identical failing call is only a REPLAY if nothing landed in
                // between; a mutation since the last identical failure restarts
                // the exact-args streak.
                if c.progressSince[s] {
                        delete(c.exactFailures, s)
                        delete(c.progressSince, s)
                }
                // A throttled call retried after its cooldown is the correct
                // move, not a replay: keep it out of the exact-args streak and
                // never give it that streak's "stop retrying unchanged" warning.
                // It still counts per tool, so the same-tool halt still bounds a
                // dead endpoint.
                throttled := isRateLimited(result)
                exactCnt := 0
                if !throttled {
                        c.exactFailuresMap()[s]++
                        exactCnt = c.exactFailures[s]
                }
                c.toolFailuresMap()[tool]++
                sameCnt := c.toolFailures[tool]
                delete(c.noProgress, s)
                if d := c.halt("same_tool_failure_halt", tool, sameCnt,
                        fmt.Sprintf(
                                "Stopped %s: it failed %d times this turn. "+
                                        "Stop retrying the same failing tool path and choose a different approach.",
                                tool, sameCnt),
                        c.cfg.HardStop && !failureTolerantTools[tool] && sameCnt >= sameToolHaltAfter); d != nil {
                        decision = d
                } else if c.cfg.Warnings && !throttled && exactCnt >= exactFailureWarnAfter {
                        decision = &Decision{
                                Action: "warn", Code: "repeated_exact_failure_warning",
                                Tool: tool, Count: exactCnt,
                                Message: fmt.Sprintf(
                                        "%s has failed %d times with identical arguments. This looks like a loop; "+
                                                "inspect the error and change strategy instead of retrying it unchanged.",
                                        tool, exactCnt),
                        }
                } else if c.cfg.Warnings && sameCnt >= sameToolWarnAfter {
                        decision = &Decision{
                                Action: "warn", Code: "same_tool_failure_warning",
                                Tool: tool, Count: sameCnt,
                                Message: recoveryHint(tool, sameCnt),
                        }
                }
        } else {
                delete(c.exactFailures, s)
                delete(c.toolFailures, tool)
                // A successful mutation is progress for every failing signature
                // still counted this turn. Pure loops never mutate between
                // attempts, so the replay detector keeps its teeth.
                if !readable {
                        if c.progressSince == nil {
                                c.progressSince = make(map[sig]bool)
                        }
                        for k := range c.exactFailures {
                                c.progressSince[k] = true
                        }
                        c.toolFailures = nil
                } else {
                        rec, ok := c.noProgress[s]
                        repeats := 1
                        if ok && rec.resultHash == rHash {
                                repeats = rec.repeats + 1
                        }
                        c.noProgressInit()[s] = noProgressRecord{resultHash: rHash, repeats: repeats}
                        if c.cfg.Warnings && repeats >= noProgressWarnAfter {
                                decision = &Decision{
                                        Action: "warn", Code: "idempotent_no_progress_warning",
                                        Tool: tool, Count: repeats,
                                        Message: fmt.Sprintf(
                                                "%s returned the same result %d times. Use the result already provided "+
                                                        "or change the query instead of repeating it unchanged.",
                                                tool, repeats),
                                }
                        }
                }
        }

        notice := c.observeStreak(tool, s, rHash, callID)
        if notice == "" {
                if period, laps, ok := c.detectCycle(); ok {
                        notice = fmt.Sprintf(
                                "[a1 note: the last %d rounds repeated the same cycle of %d tool calls "+
                                        "(ending with %s) with identical arguments and identical results. "+
                                        "Do not repeat the batch — change arguments, use a different tool, "+
                                        "or proceed with what you have.]",
                                laps, period, tool)
                        // Hard-stop widening (hermes #89069 / #100849): the failure tiers
                        // only cover failing calls, so a model replaying a SUCCESSFUL
                        // call byte-identically ran to the iteration budget unflagged.
                        // The streak and cycle breakers are tool-agnostic; with hard
                        // stops armed they halt at the same no-progress threshold.
                        if d := c.halt("identical_cycle_halt", tool, laps,
                                fmt.Sprintf(
                                        "Stopped %s: the same repeating cycle of tool calls (period %d) with identical "+
                                                "arguments and identical results has run %d times. Repeating the batch unchanged is not "+
                                                "progress; use the results already provided or change strategy.",
                                        tool, period, laps),
                                c.cfg.HardStop && laps >= noProgressBlockAfter); d != nil {
                                // A halt outranks any warn the tiers produced for the same
                                // call: stopping the turn makes the softer guidance moot.
                                decision = d
                        }
                }
        }
        // The consecutive streak carries the same widening (and outranks a
        // mere warning when both would fire).
        if d := c.halt("identical_call_streak_halt", tool, c.streakCount,
                fmt.Sprintf(
                        "Stopped %s: the same call with identical arguments returned the same result "+
                                "%d times in a row. Use the result already provided or change strategy.",
                        tool, c.streakCount),
                c.cfg.HardStop && c.streakCount >= noProgressBlockAfter); d != nil {
                decision = d
        }

        stub := ""
        if c.streakCount >= 2 && !failed && len(result) >= stubMinResultChars {
                stub = c.buildStub(tool, args)
        }
        return decision, notice, stub
}

// observeStreak tracks the consecutive identical (sig, result) streak and
// appends the call to the cycle history. Returns the notice ("" when
// none).
func (c *Controller) observeStreak(tool string, s sig, rHash, callID string) string {
        if c.streakSig != nil && *c.streakSig == s && c.streakHash == rHash {
                c.streakCount++
        } else {
                c.streakSig = &s
                c.streakHash = rHash
                c.streakCount = 1
                c.streakFirst = callID
        }
        notice := ""
        if !repeatableTools[tool] && c.streakCount >= identicalCallThreshold {
                notice = fmt.Sprintf(
                        "[a1 note: this is the %s consecutive identical call to %s with identical "+
                                "arguments returning the same result. Do not repeat it — change arguments, "+
                                "use a different tool, or proceed with what you have.]",
                        ordinal(c.streakCount), tool)
        }
        // Batch-cycle history: every observed call, streak or not.
        c.history = append(c.history, cycleEntry{s: s, resultHash: rHash, repeatable: repeatableTools[tool]})
        if len(c.history) > cycleHistoryDepth {
                c.history = c.history[len(c.history)-cycleHistoryDepth:]
        }
        return notice
}

// detectCycle finds a repeating identical-call cycle ending at the latest
// observed call: (period, laps) for the smallest period 2..max whose
// trailing laps (identical signature AND result per position) reach the
// notice threshold. Period 1 is the consecutive streak's job. A cycle made
// ONLY of poller-exempt tools is exempt (an unchanged poll loop is
// legitimate waiting); one non-exempt call in the cycle keeps the guard
// armed.
func (c *Controller) detectCycle() (period, laps int, ok bool) {
        h := c.history
        for p := 2; p <= maxCyclePeriod; p++ {
                if len(h) < p*identicalCallThreshold {
                        continue
                }
                laps := 1
                for {
                        base := len(h) - p*(laps+1)
                        if base < 0 {
                                break
                        }
                        equal := true
                        for i := 0; i < p; i++ {
                                a, b := h[base+i], h[len(h)-p+i]
                                if a.s != b.s || a.resultHash != b.resultHash {
                                        equal = false
                                        break
                                }
                        }
                        if !equal {
                                break
                        }
                        laps++
                }
                if laps >= identicalCallThreshold {
                        allRepeatable := true
                        for i := 0; i < p; i++ {
                                if !h[len(h)-p+i].repeatable {
                                        allRepeatable = false
                                        break
                                }
                        }
                        if !allRepeatable {
                                // A constant sub-cycle would already have fired at a
                                // smaller period.
                                return p, laps, true
                        }
                }
        }
        return 0, 0, false
}

// buildStub renders the reference stub for a byte-identical duplicate
// result.
func (c *Controller) buildStub(tool, args string) string {
        preview := canonicalArgs(args)
        if runes := []rune(preview); len(runes) > stubArgsPreviewChars {
                preview = string(runes[:stubArgsPreviewChars]) + "…"
        }
        ref := ""
        if c.streakFirst != "" {
                ref = fmt.Sprintf(" (call %s)", c.streakFirst)
        }
        return fmt.Sprintf(
                "[a1 note: this result is byte-identical to the %s result earlier this turn%s. "+
                        "Refer to that result; it has not changed. Args: %s]",
                tool, ref, preview)
}

func (c *Controller) exactCount(s sig) int {
        if c.progressSince[s] {
                return 0
        }
        return c.exactFailures[s]
}

// halt builds and records the turn's halt decision when armed and the
// condition holds; nil otherwise. The first halt wins: once recorded,
// later tiers return nil so the executor stops exactly once.
func (c *Controller) halt(code, tool string, count int, message string, armed bool) *Decision {
        if !armed || c.halted {
                return nil
        }
        c.halted = true
        return &Decision{Action: "halt", Code: code, Tool: tool, Count: count, Message: message}
}

func (c *Controller) exactFailuresMap() map[sig]int {
        if c.exactFailures == nil {
                c.exactFailures = make(map[sig]int)
        }
        return c.exactFailures
}

func (c *Controller) toolFailuresMap() map[string]int {
        if c.toolFailures == nil {
                c.toolFailures = make(map[string]int)
        }
        return c.toolFailures
}

func (c *Controller) noProgressInit() map[sig]noProgressRecord {
        if c.noProgress == nil {
                c.noProgress = make(map[sig]noProgressRecord)
        }
        return c.noProgress
}

// makeSig builds the stable call identity: tool + SHA-256 of the canonical
// arguments.
func makeSig(tool, args string) sig {
        sum := sha256.Sum256([]byte(canonicalArgs(args)))
        return sig{tool: tool, argsHash: hex.EncodeToString(sum[:])}
}

// canonicalArgs renders parsed tool arguments as sorted-key compact JSON
// (Go maps marshal sorted), so key order never mints a new signature. A
// non-object or unparseable argument string is trimmed and used raw: the
// executor's malformed-arguments guard refuses those before the guardrail
// ever sees them, so this only covers tools that legitimately take
// non-object args. json.Number preserves integer literals verbatim, so
// large ids never collide through float rounding.
func canonicalArgs(args string) string {
        trimmed := strings.TrimSpace(args)
        if trimmed == "" {
                return "{}"
        }
        dec := json.NewDecoder(strings.NewReader(trimmed))
        dec.UseNumber()
        var m map[string]any
        if err := dec.Decode(&m); err != nil {
                return trimmed
        }
        out, err := json.Marshal(m)
        if err != nil {
                return trimmed
        }
        return string(out)
}

// resultHash hashes a tool result. JSON-object results are canonicalized
// first (sorted keys, compact separators, json.Number), so a provider- or
// tool-side key reorder never mints a new result; everything else hashes
// raw.
func resultHash(result string) string {
        canonical := strings.TrimSpace(result)
        if canonical != "" {
                dec := json.NewDecoder(strings.NewReader(canonical))
                dec.UseNumber()
                var v any
                if err := dec.Decode(&v); err == nil {
                        if b, err := json.Marshal(v); err == nil {
                                canonical = string(b)
                        }
                }
        }
        sum := sha256.Sum256([]byte(canonical))
        return hex.EncodeToString(sum[:])
}

// isRateLimited reports whether a FAILED result says the backend throttled
// the call (a 429 status or rate-limit wording in its error). Only
// meaningful for results already judged failed: a successful page can
// mention rate limits in its content.
func isRateLimited(result string) bool {
        head := result
        if r := []rune(result); len(r) > 500 {
                head = string(r[:500])
        }
        head = strings.ToLower(head)
        return strings.Contains(head, "429") ||
                strings.Contains(head, "rate limit") ||
                strings.Contains(head, "too many requests") ||
                strings.Contains(head, "throttl")
}

// recoveryHint is the action-oriented guidance for a tool failing with
// varied arguments: diagnose before retrying, and name the escape hatches.
func recoveryHint(tool string, count int) string {
        common := fmt.Sprintf(
                "%s has failed %d times this turn. This looks like a loop. "+
                        "Do not switch to text-only replies; keep using tools, but diagnose before retrying. "+
                        "First inspect the latest error and verify your assumptions. ",
                tool, count)
        if tool == "bash" {
                return common + "For bash failures, run a small diagnostic such as `pwd && ls -la` " +
                        "in the same tool, then try an absolute path, a simpler command, a different " +
                        "working directory, or a different tool such as read/write/edit."
        }
        return common + "Try different arguments, a narrower query or path, an absolute path when " +
                "relevant, or a different tool that can make progress. If the blocker is external, " +
                "report the blocker after one diagnostic attempt instead of repeating the same failing path."
}

// ordinal renders 1st/2nd/3rd/4th… for the identical-call notice.
func ordinal(n int) string {
        switch {
        case n%100 >= 11 && n%100 <= 13:
                return fmt.Sprintf("%dth", n)
        case n%10 == 1:
                return fmt.Sprintf("%dst", n)
        case n%10 == 2:
                return fmt.Sprintf("%dnd", n)
        case n%10 == 3:
                return fmt.Sprintf("%drd", n)
        default:
                return fmt.Sprintf("%dth", n)
        }
}
