package agent

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "runtime/debug"
        "strings"
        "sync"
        "sync/atomic"
        "time"

        "github.com/damien1141/a1/internal/debuglog"
        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/extension"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/session/memory"
        "github.com/damien1141/a1/internal/skillstats"
        "github.com/damien1141/a1/internal/telemetry"
        "github.com/damien1141/a1/internal/toolguard"
        "github.com/damien1141/a1/internal/tools"
        "github.com/damien1141/a1/internal/tools/tooldef"
        "github.com/damien1141/a1/internal/util"
)

// ToolCanceledResult is returned to the model when a user cancels a tool call.
const ToolCanceledResult = "User cancelled the tool call."

const (
        extContextOpen  = "<ext_context>"
        extContextClose = "</ext_context>"
)

// Executor runs model tool_calls against a tool registry and emits ToolData for the UI.
type Executor struct {
        registry   tools.Registry
        gate       permission.Gate
        ask        permission.AskFunc
        question   tooldef.QuestionFunc
        ext        *extension.Runner // nil = disabled; methods are nil-safe no-ops
        sessionID  string
        cwd        string
        memoryBank sessionMemoryBank
        telemetry  *telemetry.Recorder
        // evidence is the protected evidence ledger (nil = disabled). Completed
        // bash calls are classified and recorded; protected regressions gain an
        // advisory note on the tool result the model reads next round.
        evidence *evidence.Ledger
        // evidenceMu guards evidenceVerdicts, collected by the evidence hook as
        // bash calls complete (the concurrent read-only batch can fold several
        // at once) and drained by the engine's verdict gate after each Run.
        evidenceMu       sync.Mutex
        evidenceVerdicts []evidence.Verdict

        // governed reports whether the session's evidence surfaces are live
        // (chat-mode ergonomics: an ungoverned session leaves no evidence
        // trace). The engine binds engine.IsGoverned; nil (a bare executor,
        // as tests build) records unconditionally — the latch is a session
        // concept only engines have.
        governed func() bool

        // jevAdv is the optional JEV decision-model evidence advisor (nil =
        // disabled). Consulted only when a completed bash command produced
        // no deterministic evidence, so the fallback can only ADD records,
        // never change what a deterministic classifier concluded. Installed
        // via SetJevAdvisor.
        jevAdv *jevEvidenceAdvisor

        // readElider collapses identical re-reads of unchanged files to a
        // short note when the earlier content provably still rides in the
        // live context (engine-owned, compaction-aware; nil = disabled, the
        // default on a bare executor). Installed via SetReadElider.
        readElider func(call llm.ToolCall, args json.RawMessage) (string, bool)

        // imageEnabled mirrors the active model's ImageEnabled flag so tool
        // results can attach image blocks only for models that can see them.
        // The engine rebinds it on model switches; a bare executor keeps false
        // and serves text-only results (which must stand alone anyway).
        imageEnabled bool

        // callAllowance is the per-round tool-call budget, owned by the engine.
        // Negative = unlimited for this round. The engine sets it before each
        // Run; allowedUsed / allowanceTripped track that round only. Atomics
        // because read-only batches claim slots from parallel goroutines.
        allowance        int64 // atomic
        allowedUsed      int64 // atomic
        allowanceTripped atomic.Bool

        // approveAdmit folds an approved Gate 2 admission into the trajectory.
        // It is set by the engine when the user approves an admission Ask, so
        // the label/effect mutations only ever happen on an explicit decision.
        approveAdmit func(permission.Request)

        // askMu serializes approval prompts: a concurrent read-only batch can
        // otherwise pop multiple dialogs / interleave stdin reads at once.
        askMu sync.Mutex

        // loopGuard is the per-turn tool-call loop guardrail controller (nil =
        // disabled — a bare executor, as tests build). Installed via
        // SetLoopGuard; the engine resets it at every user prompt.
        loopGuard *toolguard.Controller

        // dispatchWhitelist limits which tools this executor may dispatch (nil
        // = no restriction). A call outside it is refused before budget,
        // permission, or execution — for constrained sub-engines the whitelist
        // IS the policy. Installed via SetDispatchWhitelist.
        dispatchWhitelist map[string]bool
        dispatchDenier    func(tool string) string

        // learningSink is the engine's nudge-counter reset hook (nil = no
        // learning loop). Called just before an approved tool runs.
        learningSink func(tool string, args json.RawMessage)

        // mutationMu guards mutations, the verify-on-stop gate's fact list:
        // successful workspace-mutating calls with a path argument, folded
        // into the engine's turn state after each Run and drained by the
        // same contract as the evidence verdicts.
        mutationMu sync.Mutex
        mutations  []MutationFact
}

type sessionMemoryBank interface {
        Append(entryType memory.EntryType, content string) error
}

// NewExecutor builds an executor. extRunner may be nil, and a nil question
// asker leaves ask_user unable to run (it reports that rather than blocking).
func NewExecutor(
        registry tools.Registry,
        gate permission.Gate,
        ask permission.AskFunc,
        extRunner *extension.Runner,
        question tooldef.QuestionFunc,
) *Executor {
        if gate == nil {
                gate = permission.AllowAll{}
        }
        rec := telemetry.NewRecorder()
        telemetry.SetGlobalRecorder(rec)
        e := &Executor{
                registry:  registry,
                gate:      gate,
                ask:       ask,
                question:  question,
                ext:       extRunner,
                telemetry: rec,
        }
        // Zero-value allowance (0) reads as "reject everything", so every
        // executor starts unlimited; the engine sets a real budget per round.
        e.allowance = -1
        return e
}

// SetMeta attaches session identity used in extension Event payloads.
func (e *Executor) SetMeta(sessionID, cwd string) {
        if e == nil {
                return
        }
        e.sessionID = sessionID
        e.cwd = cwd
        e.ext.SetMeta(sessionID, cwd)
}

// SetMemoryBank attaches a memory bank for tool-call journaling.
func (e *Executor) SetMemoryBank(bank sessionMemoryBank) {
        if e == nil {
                return
        }
        e.memoryBank = bank
}

// SetEvidenceLedger attaches the protected evidence ledger (nil = disabled).
// Any verdicts collected against the previous ledger are dropped so the
// drain never mixes observations from two ledgers.
func (e *Executor) SetEvidenceLedger(l *evidence.Ledger) {
        if e == nil {
                return
        }
        e.evidence = l
        e.evidenceMu.Lock()
        e.evidenceVerdicts = nil
        e.evidenceMu.Unlock()
}

// SetGovernedFn attaches the session's governed latch. fn reports whether
// evidence recording is live for this executor's session; the engine binds
// engine.IsGoverned so the answer is read live at record time. Nil (the
// default on a bare executor) records unconditionally.
func (e *Executor) SetGovernedFn(fn func() bool) {
        if e == nil {
                return
        }
        e.governed = fn
}

// SetJevAdvisor attaches the JEV decision-model evidence advisor (nil =
// disabled). The advisor is consulted only for check-like commands that
// produced no deterministic evidence; see jev_evidence.go.
func (e *Executor) SetJevAdvisor(a *jevEvidenceAdvisor) {
        if e == nil {
                return
        }
        e.jevAdv = a
}

// SetReadElider attaches the read-elision hook (nil = disabled). The engine
// installs its compaction-aware elideUnchangedRead; a bare executor keeps
// the nil default and serves full content, which is also the correct fallback
// when elision cannot be proven safe.
func (e *Executor) SetReadElider(fn func(call llm.ToolCall, args json.RawMessage) (string, bool)) {
        if e == nil {
                return
        }
        e.readElider = fn
}

// SetImageEnabled attaches the active model's image support flag. Tools
// consult it via tooldef.ImageEnabledFrom(ctx) to skip image work for blind
// models; the executor also strips surviving blocks so a provider that
// ignores tool-message images never sees them.
func (e *Executor) SetImageEnabled(enabled bool) {
        if e == nil {
                return
        }
        e.imageEnabled = enabled
}

// DrainEvidenceVerdicts returns and clears the verdicts collected by the
// evidence hook during the last executor.Run. A non-empty result is the
// candidate marker for the engine's verdict gate: at least one classified
// check ran in the batch.
func (e *Executor) DrainEvidenceVerdicts() []evidence.Verdict {
        if e == nil {
                return nil
        }
        e.evidenceMu.Lock()
        defer e.evidenceMu.Unlock()
        v := e.evidenceVerdicts
        e.evidenceVerdicts = nil
        return v
}

// BatchMutates reports whether any call in the batch can modify state: any
// unregistered tool name (a typo could be a write — same conservative rule
// as readableBatch) or any registered tool that is not read-only. bash is
// never readable, so every bash batch is mutating. The verdict gate takes
// its pre-candidate baseline before such batches.
func (e *Executor) BatchMutates(calls []llm.ToolCall) bool {
        if e == nil || len(calls) == 0 {
                return false
        }
        for _, call := range calls {
                tool, ok := e.registry[call.Function.Name]
                if !ok || !tool.Definition.Readable {
                        return true
                }
        }
        return false
}

// RejectBatch synthesizes rejected results for a batch of tool calls without
// running anything: no allowance is claimed, no permission is asked, no tool
// executes. Used by the declared-budget gate (Phase 5) when a self-declared
// tool-round budget fires terminally while the model still wants tools — the
// pending calls must come back as results so the session never holds an
// unanswered tool call, but they must not execute past the budget the model
// declared for itself.
func (e *Executor) RejectBatch(
        calls []llm.ToolCall,
        reason string,
        emit func(session.ToolData) bool,
) []llm.Message {
        if e == nil || len(calls) == 0 {
                return nil
        }
        msgs := make([]llm.Message, 0, len(calls))
        for _, call := range calls {
                detail := call.Function.Arguments
                if tool, ok := e.registry[call.Function.Name]; ok && tool.DetailFromArgs != nil {
                        if d := tool.DetailFromArgs(json.RawMessage(call.Function.Arguments)); d != "" {
                                detail = d
                        }
                }
                msgs = append(msgs, e.rejectResult(call, detail, reason, emit))
        }
        return msgs
}

// SetCallAllowance sets the per-round tool-call budget (n < 0 = unlimited)
// and resets the per-round counters. The engine calls this before each Run.
func (e *Executor) SetCallAllowance(n int) {
        if e == nil {
                return
        }
        atomic.StoreInt64(&e.allowance, int64(n))
        atomic.StoreInt64(&e.allowedUsed, 0)
        e.allowanceTripped.Store(false)
}

// claimCall reserves one tool-call slot for the current round. It returns
// false when the round's allowance is exhausted; the caller rejects the call
// with a budget note instead of executing it.
func (e *Executor) claimCall() bool {
        limit := atomic.LoadInt64(&e.allowance)
        if limit < 0 {
                return true
        }
        if atomic.AddInt64(&e.allowedUsed, 1) > limit {
                // Give the slot back so AllowanceUsed stays the granted count.
                atomic.AddInt64(&e.allowedUsed, -1)
                e.allowanceTripped.Store(true)
                return false
        }
        return true
}

// AllowanceUsed reports how many calls were granted in the current round.
func (e *Executor) AllowanceUsed() int {
        if e == nil {
                return 0
        }
        return int(atomic.LoadInt64(&e.allowedUsed))
}

// AllowanceTripped reports whether any call in the current round was rejected
// because the tool-call budget ran out.
func (e *Executor) AllowanceTripped() bool {
        return e != nil && e.allowanceTripped.Load()
}

// Telemetry returns the telemetry recorder for this executor.
func (e *Executor) Telemetry() *telemetry.Recorder {
        if e == nil {
                return nil
        }
        return e.telemetry
}

// Run executes tool calls and yields ToolData updates via emit.
// Returns role=tool messages for the next LLM turn (including cancel stubs).
// stop=true means an extension PostTool asked to end the agent loop.
//
// A batch whose calls all target read-only tools (Definition.Readable) runs
// concurrently — read-only calls have no side effects, so parallel execution
// is order-independent. Any write-capable or unknown call falls back to
// sequential execution. Results always come back in call order.
func (e *Executor) Run(
        ctx context.Context,
        calls []llm.ToolCall,
        emit func(session.ToolData) bool,
) (msgs []llm.Message, stop bool, stopReason string) {
        if e.readableBatch(calls) {
                return e.runConcurrent(ctx, calls, emit)
        }
        return e.runSequential(ctx, calls, emit)
}

// readableBatch reports whether every call targets a registered read-only
// tool. Unknown tools are treated as unsafe (a typo'd name could be a write).
func (e *Executor) readableBatch(calls []llm.ToolCall) bool {
        if len(calls) < 2 {
                return false
        }
        for _, call := range calls {
                tool, ok := e.registry[call.Function.Name]
                if !ok || !tool.Definition.Readable {
                        return false
                }
        }
        return true
}

// runSequential executes calls one at a time, stopping at the first halt.
func (e *Executor) runSequential(
        ctx context.Context,
        calls []llm.ToolCall,
        emit func(session.ToolData) bool,
) (msgs []llm.Message, stop bool, stopReason string) {
        results := make([]llm.Message, 0, len(calls))
        for _, call := range calls {
                if ctx.Err() != nil {
                        results = append(results, e.cancelResult(call, emit))
                        continue
                }
                msg, halt, reason := e.runOne(ctx, call, emit)
                results = append(results, msg)
                if halt {
                        return results, true, reason
                }
        }
        return results, false, ""
}

// runConcurrent executes a read-only batch in parallel, one goroutine per
// call. UI emits are serialized so ToolData never interleaves from two
// goroutines; results are reassembled in call order. A halt ends the turn —
// later calls already ran, but since they are read-only their results are
// safe to drop.
func (e *Executor) runConcurrent(
        ctx context.Context,
        calls []llm.ToolCall,
        emit func(session.ToolData) bool,
) (msgs []llm.Message, stop bool, stopReason string) {
        type outcome struct {
                msg    llm.Message
                halt   bool
                reason string
        }
        outcomes := make([]outcome, len(calls))

        var emitMu sync.Mutex
        var stopped atomic.Bool
        lockedEmit := func(td session.ToolData) bool {
                emitMu.Lock()
                defer emitMu.Unlock()
                if stopped.Load() {
                        return false
                }
                if !emit(td) {
                        stopped.Store(true)
                        return false
                }
                return true
        }

        var wg sync.WaitGroup
        for i, call := range calls {
                wg.Go(func() {
                        msg, halt, reason := e.runOne(ctx, call, lockedEmit)
                        outcomes[i] = outcome{msg: msg, halt: halt, reason: reason}
                })
        }
        wg.Wait()

        msgs = make([]llm.Message, 0, len(calls))
        for _, o := range outcomes {
                msgs = append(msgs, o.msg)
                if o.halt {
                        return msgs, true, o.reason
                }
        }
        return msgs, false, ""
}

// runOne executes a single tool call behind a panic guard: a panicking tool,
// hook, or extension becomes a ToolError result the model can react to,
// instead of taking down the whole process. Every surface (TUI, webserver,
// headless runs) shares one process with arbitrary tool code and
// third-party extensions, so one buggy Run must not kill them all.
func (e *Executor) runOne(
        ctx context.Context,
        call llm.ToolCall,
        emit func(session.ToolData) bool,
) (msg llm.Message, halt bool, reason string) {
        defer func() {
                if r := recover(); r != nil {
                        errText := fmt.Sprintf("tool '%s' panicked: %v", call.Function.Name, r)
                        debuglog.Logf("executor: %s\n%s", errText, debug.Stack())
                        // The in-progress row was already emitted before Run; close it
                        // as an error so the UI never shows a tool stuck "running".
                        _ = emit(session.ToolData{
                                Run: e.toolRun(call, session.ToolError, call.Function.Arguments, errText, ""),
                        })
                        msg = e.toolMessage(call.ID, errText, nil)
                        halt = false
                        reason = ""
                }
        }()
        return e.runOneChecked(ctx, call, emit)
}

func (e *Executor) runOneChecked(
        ctx context.Context,
        call llm.ToolCall,
        emit func(session.ToolData) bool,
) (llm.Message, bool, string) {
        ctx = tools.WithCwd(ctx, e.cwd)
        // Script dispatch (scripttool's RPC): every tool Run carries this
        // executor's dispatch pipeline, so the script tool's child process
        // lands back in the executor that gated the script itself.
        ctx = tooldef.WithScriptDispatch(ctx, e.DispatchScript)
        ctx = tooldef.WithImageEnabled(ctx, e.imageEnabled)
        // The live permission mode rides the context for the same reason the
        // cwd does: the gate below is the authority for this call, and tools
        // that behave mode-sensitively (bash containment turns off under
        // yolo) must see the mode that actually gated them — not whatever the
        // config file said at load, which a shift-tab cycle or a web dropdown
        // has long since superseded. ModeOf unwraps SessionAllowGate; a gate
        // with no mode (AllowAll) attaches nothing and tools fall back to
        // the config's mode.
        if mode := permission.ModeOf(e.gate); mode != "" {
                ctx = tooldef.WithPermissionMode(ctx, string(mode))
        }
        // Attached here rather than per-engine so the asker rides the same context
        // the tool already receives. A nil asker is not stored by WithQuestionFunc,
        // and the tool then fails closed with an explicit message.
        if e.question != nil {
                ctx = tooldef.WithQuestionFunc(ctx, e.question)
        }
        tool, ok := e.registry[call.Function.Name]
        args := json.RawMessage(call.Function.Arguments)
        detail := call.Function.Arguments
        // detailIsArgs is true while detail is still the raw argument JSON, i.e. the
        // tool supplied no DetailFromArgs. It gates the completion-time Detail
        // override; see use in the result branch below.
        detailIsArgs := true
        if ok && tool.DetailFromArgs != nil {
                if d := tool.DetailFromArgs(args); d != "" {
                        detail = d
                        detailIsArgs = false
                }
        }

        // Dispatch whitelist: a constrained executor allows only its named
        // tools; anything else is refused before budget, permission, or
        // execution. The refusal feeds the loop guard like any other — a model
        // re-issuing an out-of-surface call is exactly the replay loop the
        // streaks exist to catch.
        if refused, denied := e.guardDispatch(call, detail, emit); denied {
                return refused, false, ""
        }

        // Tool-call budget: claim a slot before anything runs, so an exhausted
        // budget neither prompts the user nor flashes an in-progress row. The
        // claim happens before the extension/gate pipeline for the same reason.
        if !e.claimCall() {
                reason := fmt.Sprintf(
                        "tool call budget reached (%d tool calls per turn) — the turn was stopped; raise the cap or ask to continue",
                        atomic.LoadInt64(&e.allowance),
                )
                return e.rejectResult(call, detail, reason, emit), false, ""
        }

        if !emit(session.ToolData{Run: e.toolRun(call, session.ToolInProgress, detail, "", "")}) {
                return e.toolMessage(call.ID, ToolCanceledResult, nil), false, ""
        }

        if !ok {
                errText := fmt.Sprintf("tool '%s' not found", call.Function.Name)
                errText = e.guardRefusal(call, args, errText)
                _ = emit(session.ToolData{Run: e.toolRun(call, session.ToolError, detail, errText, "")})
                return e.toolMessage(call.ID, errText, nil), false, ""
        }

        e.ext.EmitToolExecutionStart(call.Function.Name, call.ID, args)

        // ExtensionPre → Gate → Run → ExtensionPost. Pre runs before permission Ask
        // so org policy can deny without prompting the user.
        var preContext string
        newArgs, blocked, reason, ctxText := e.ext.PreTool(ctx, call.Function.Name, call.ID, args)
        preContext = ctxText
        if blocked {
                if reason == "" {
                        reason = "tool execution denied by extension"
                }
                reason = appendExtContext(e.guardRefusal(call, args, reason), preContext)
                e.ext.EmitToolExecutionEnd(call.Function.Name, call.ID, true)
                return e.rejectResult(call, detail, reason, emit), false, ""
        }
        if len(newArgs) > 0 {
                args = newArgs
                if tool.DetailFromArgs != nil {
                        if d := tool.DetailFromArgs(args); d != "" {
                                detail = d
                                detailIsArgs = false
                        }
                } else {
                        detail = string(args)
                        detailIsArgs = true
                }
        }

        // Degenerate-call guard: a call whose arguments carry no fields at all —
        // blank, or {} — against a tool that declares required parameters cannot be
        // a real attempt. Long sessions surface exactly this from the wire (a model
        // at the edge of its context, or a gateway collapsing arguments it failed
        // to parse), and before this guard the executor ran it anyway: a round was
        // burned to rediscover "path is required", and the bare tool error told the
        // model nothing it could correct against — some models genuinely believe
        // they sent the arguments, so the retry is identical and the rows
        // accumulate. Refuse before permission and execution, name every required
        // parameter, and show the shape to re-issue; the corrective round is the
        // recovery. Non-empty arguments always pass: mode-dispatched tools accept
        // shapes their schema cannot express (read with mode=ls lists the cwd with
        // no path), and a non-object argument is the tool's own parse error to
        // report in its own voice.
        if params := tool.Definition.Params; params != nil && len(params.Required) > 0 && emptyToolArgs(args) {
                quoted := make([]string, 0, len(params.Required))
                for _, name := range params.Required {
                        quoted = append(quoted, fmt.Sprintf("%q", name))
                }
                raw := strings.TrimSpace(string(args))
                if raw == "" {
                        raw = "none"
                }
                errText := fmt.Sprintf(
                        "tool '%s' was called with empty arguments (%s) and was not executed — it requires %s. "+
                                "Re-issue the call with its arguments as one JSON object, e.g. {\"%s\": <value>}",
                        call.Function.Name,
                        raw,
                        strings.Join(quoted, ", "),
                        params.Required[0],
                )
                errText = e.guardRefusal(call, args, errText)
                debuglog.Logf("executor: refused empty-arguments call %s raw=%q", call.Function.Name, string(args))
                _ = emit(session.ToolData{Run: e.toolRun(call, session.ToolError, detail, errText, "")})
                e.ext.EmitToolExecutionEnd(call.Function.Name, call.ID, true)
                return e.toolMessage(call.ID, errText, nil), false, ""
        }

        // Malformed-arguments guard: non-empty arguments that are not valid
        // JSON cannot be any tool's real call shape — a stream cut
        // mid-arguments (or a gateway collapsing fragments it failed to
        // parse) leaves a truncated prefix, and duplicate-delivery echoes
        // leave two objects concatenated. emptyToolArgs passes both (they are
        // neither blank nor {}), and before this guard they died at the
        // permission extractor with a wrapped parse error — "permission
        // check failed: write args: unexpected end of JSON input" — that told
        // the model nothing it could correct against. Worse, the malformed
        // call itself stays in the session, and replayed verbatim it poisons
        // every later request against strict gateways: a bare 400 ("Provider
        // returned error") that no amount of compaction fixes, because the
        // poisoned message rides in the compaction kept zone. Refuse before
        // permission, show the model what actually arrived, and ask for the
        // complete object; the corrective round is the recovery. Valid
        // non-object JSON (a bare string, a number) still passes: the tool's
        // own parse error reports those in its own voice.
        if raw := strings.TrimSpace(string(args)); raw != "" && !json.Valid([]byte(raw)) {
                example := "argument"
                if params := tool.Definition.Params; params != nil && len(params.Required) > 0 {
                        example = params.Required[0]
                }
                errText := fmt.Sprintf(
                        "tool '%s' was called with malformed arguments (not valid JSON): %s and was not executed — the arguments arrived truncated or corrupted. "+
                                "Re-issue the call with its arguments as one complete JSON object, e.g. {\"%s\": <value>}",
                        call.Function.Name,
                        truncateForModel(raw),
                        example,
                )
                errText = e.guardRefusal(call, args, errText)
                debuglog.Logf("executor: refused malformed-arguments call %s raw=%q", call.Function.Name, string(args))
                _ = emit(session.ToolData{Run: e.toolRun(call, session.ToolError, detail, errText, "")})
                e.ext.EmitToolExecutionEnd(call.Function.Name, call.ID, true)
                return e.toolMessage(call.ID, errText, nil), false, ""
        }

        // Tool loop guardrail, pre-call: block an imminent call the controller
        // judges hopeless — an exact-args replay past its block threshold, a
        // no-progress read past its block threshold, or a capped tool over its
        // per-turn budget. Loop caps fire regardless of HardStop: a runaway
        // fetch or spawn loop is a spend problem, not a judgment call.
        if e.loopGuard != nil {
                if d := e.loopGuard.Before(call.Function.Name, string(args), tool.Definition.Readable); d != nil {
                        _ = emit(session.ToolData{Run: e.toolRun(call, session.ToolError, detail, d.Message, "")})
                        e.ext.EmitToolExecutionEnd(call.Function.Name, call.ID, true)
                        return e.toolMessage(call.ID, d.Message, nil), false, ""
                }
        }

        if msg, rejected := e.checkPermission(ctx, call, args, detail, emit); rejected {
                if e.loopGuard != nil {
                        d, notice, _ := e.loopGuard.Report(call.Function.Name, string(args), msg.Content, call.ID, true, tool.Definition.Readable)
                        msg.Content = appendGuardNotes(msg.Content, d, notice)
                }
                e.ext.EmitToolExecutionEnd(call.Function.Name, call.ID, true)
                return msg, false, ""
        }

        if e.telemetry != nil {
                e.telemetry.Record(
                        telemetry.Event{Kind: telemetry.EventToolCall, Tool: call.Function.Name, CallID: call.ID, When: time.Now()},
                )
        }

        // Latency and session attribution ride the terminal telemetry events
        // (Bet 07): the tool_call pre-event fires before the run, so only
        // the ok/error events carry a duration.
        // Learning sink (see learning.go): the engine's nudge counters
        // reset on actual use — here, after every gate has passed and just
        // before the tool runs, so a refused call (permission, whitelist,
        // malformed args) never counts as use.
        if e.learningSink != nil {
                e.learningSink(call.Function.Name, args)
        }

        start := time.Now()
        result, err := tool.Run(tools.WithToolCallID(ctx, call.ID), args)
        latency := time.Since(start).Milliseconds()

        // Verify-on-stop: a successful mutating call with a path argument is
        // a mutation fact — the turn's finish will be judged against it. A
        // failed or rejected call mutated nothing, and read-only tools never
        // count (a shell command without a path is deliberately invisible:
        // its file effects cannot be attributed without semantic analysis).
        if err == nil && !tool.Definition.Readable {
                e.recordMutation(call, args)
        }

        if e.telemetry != nil {
                if err != nil {
                        e.telemetry.Record(
                                telemetry.Event{
                                        Kind:       telemetry.EventToolErr,
                                        Tool:       call.Function.Name,
                                        CallID:     call.ID,
                                        When:       time.Now(),
                                        Err:        err.Error(),
                                        DurationMS: latency,
                                        SessionID:  e.sessionID,
                                },
                        )
                }
        }

        var (
                errText string
                content string
                output  string
        )
        var images []llm.Image
        if err != nil {
                if ctx.Err() != nil {
                        e.ext.EmitToolExecutionEnd(call.Function.Name, call.ID, true)
                        return e.cancelResult(call, emit), false, ""
                }
                errText = err.Error()
                content = errText
        } else {
                content = result.Content
                output = result.Output
                if output == "" {
                        output = result.Content
                }
                // Image blocks ride the tool message for vision-capable models. The
                // gate lives here (not in the tool) so a model switch takes effect
                // immediately and no provider ever receives blocks it would drop.
                if e.imageEnabled {
                        images = result.Images
                }
                // Detail stays the arg-derived identity for the whole run. A tool that
                // wants to say what happened sets Summary; overwriting Detail here made
                // "browser navigate https://x" become "Pricing" and "fetch https://x"
                // become "200 OK", so the row stopped saying what it was asked to do.
                //
                // detailIsArgs records whether the identity came from DetailFromArgs or
                // is still the raw arguments JSON. Only the latter yields to
                // result.Detail, because a raw-JSON row is not an identity at all.
                if detailIsArgs && result.Detail != "" {
                        detail = result.Detail
                }
        }

        // Human handoff: a tool that hit a state only a person can resolve (bot
        // challenge, login wall, payment step) routes through the permission ask
        // flow — the same gate that asks before a risky bash command asks before
        // a human takes the wheel.
        if err == nil && result.Handoff != nil {
                note := e.askHandoff(ctx, call, result.Handoff)
                content = appendHandoffNote(content, note)
                output = content
        }

        // Read elision: collapse an identical re-read of an unchanged file to
        // a note when the earlier content provably still rides in the live
        // context. The check is engine-side and compaction-aware — a folded-
        // away read never elides, so the model always has a path back to the
        // content (refresh, or any compaction in between).
        if err == nil && e.readElider != nil {
                if note, elide := e.readElider(call, args); elide {
                        content = note
                        output = note
                        images = nil
                }
        }

        // Evidence ledger: fold completed bash runs and named browser captures
        // into the protected check state and capture an advisory note for
        // protected regressions. The note rides on the model-visible content
        // after PostTool below.
        var evidenceNote string
        if err == nil && e.evidence != nil {
                if call.Function.Name == "bash" {
                        evidenceNote = e.recordEvidence(ctx, args, output)
                }
                if result.Screenshot != nil {
                        note := e.recordScreenshotEvidence(result.Screenshot)
                        if note != "" {
                                if evidenceNote != "" {
                                        evidenceNote += "\n\n" + note
                                } else {
                                        evidenceNote = note
                                }
                        }
                }
        }

        var (
                postContext string
                postStop    bool
                postReason  string
        )
        newContent, ctxText, stop, reason := e.ext.PostTool(
                ctx,
                call.Function.Name,
                call.ID,
                args,
                content,
                err != nil,
                errText,
        )
        postContext = ctxText
        postStop = stop
        postReason = reason
        // PostTool passes content through untouched when the runner is nil or no
        // handler rewrites it; only a changed result is applied so tool Error/Output
        // never duplicate (and nil behaves like an empty runner).
        if newContent != "" && newContent != content {
                content = newContent
                output = newContent
        }
        e.ext.EmitToolExecutionEnd(call.Function.Name, call.ID, err != nil)

        // Tool loop guardrail, post-call: fold the completed observation into
        // the controller. A warn decision rides the tool result as guidance; a
        // byte-identical large duplicate result becomes a reference stub (the
        // tool still executed — only the context representation is
        // deduplicated); an armed halt stops the turn with the guardrail's
        // reason so the model and the surface both see why.
        if e.loopGuard != nil {
                d, notice, stub := e.loopGuard.Report(call.Function.Name, string(args), content, call.ID, err != nil, tool.Definition.Readable)
                if stub != "" {
                        content = stub
                }
                if notice != "" {
                        content += "\n\n" + notice
                }
                if d != nil {
                        switch d.Action {
                        case "halt":
                                _ = emit(session.ToolData{Run: e.toolRun(call, session.ToolError, detail, d.Message, output)})
                                return e.toolMessage(call.ID, content+"\n\n"+d.Message, nil), true, "tool loop guardrail: " + d.Message
                        case "warn":
                                content += "\n\nTool loop warning: " + d.Message
                        }
                }
        }

        // Skill-routing bandit (Bet 07): a successful read of a skill's
        // SKILL.md is the observable moment the model committed to that
        // skill's operating loop. Best-effort — no vault or a failed write
        // never affects the tool result.
        if err == nil {
                e.recordSkillPick(call, args)
        }

        if e.memoryBank != nil {
                memType := memory.EntryTrace
                memContent := fmt.Sprintf(
                        "%s(%s) => %s",
                        call.Function.Name,
                        strings.TrimSpace(string(args)),
                        strings.TrimSpace(content),
                )
                if err != nil {
                        memType = memory.EntryError
                        memContent = fmt.Sprintf("%s(%s) failed: %s", call.Function.Name, strings.TrimSpace(string(args)), errText)
                }
                _ = e.memoryBank.Append(memType, memContent)
                // remember mirrors the fact itself into the bank as a real
                // memory entry. The semantic store (memstore) stays the
                // recall engine, but the persona manager renders THIS bank —
                // without the mirror, a persona's own remembered facts never
                // show up or count in its memory view.
                if err == nil && call.Function.Name == "remember" {
                        if fact := rememberFactFromArgs(args); fact != "" {
                                _ = e.memoryBank.Append(memory.EntryMemory, fact)
                        }
                }
        }

        modelContent := appendExtContext(content, joinExtContexts(preContext, postContext))
        if evidenceNote != "" {
                modelContent = modelContent + "\n\n" + evidenceNote
        }

        if err != nil {
                _ = emit(session.ToolData{Run: e.toolRun(call, session.ToolError, detail, errText, output)})
                return e.toolMessage(call.ID, modelContent, nil), postStop, postReason
        }

        // APPA Phase 1 post-execution admission: fold realized output into trajectory.
        if admitErr := e.admitExecution(ctx, call, content, output, emit); admitErr != nil {
                _ = emit(session.ToolData{Run: e.toolRun(call, session.ToolError, detail, admitErr.Error(), output)})
                return e.toolMessage(call.ID, admitErr.Error(), nil), postStop, postReason
        }

        run := e.toolRun(call, session.ToolDone, detail, "", output)
        run.Expanded = result.Expanded
        // The tool's completion summary rides alongside its identity. Detail stays
        // whatever DetailFromArgs produced, so the row keeps saying what it was
        // asked to do while Summary says what came back.
        run.Summary = result.Summary
        // Copy Sources (search-mode fetch produces these; nil for every
        // other tool). The session-layer ToolSource type mirrors
        // tooldef.Source so the JSON shape the web frontend reads is
        // identical to what the tool produced — no per-field translation
        // needed in the executor or the renderer.
        if len(result.Sources) > 0 {
                run.Sources = make([]session.ToolSource, len(result.Sources))
                for i, s := range result.Sources {
                        run.Sources[i] = session.ToolSource{
                                Rank:       s.Rank,
                                URL:        s.URL,
                                Title:      s.Title,
                                Host:       s.Host,
                                FaviconURL: s.FaviconURL,
                                Snippet:    s.Snippet,
                                Content:    s.Content,
                                FetchError: s.FetchError,
                        }
                }
        }
        if e.telemetry != nil {
                e.telemetry.Record(
                        telemetry.Event{
                                Kind:       telemetry.EventToolOK,
                                Tool:       call.Function.Name,
                                CallID:     call.ID,
                                When:       time.Now(),
                                DurationMS: latency,
                                SessionID:  e.sessionID,
                        },
                )
        }
        _ = emit(session.ToolData{Run: run})
        return e.toolMessage(call.ID, modelContent, images), postStop, postReason
}

// recordSkillPick records a skill-routing pick when a `read` call landed on
// a skill's SKILL.md. The skill's directory name is the routing key — the
// same name list_skills renders and skillstats aggregates.
//
// The signal is the read target's basename, not the configured skill root:
// the executor has no access to the engine's model config (where SkillPath
// lives), and SKILL.md is the one filename the skills catalog instructs the
// model to read, so basename matching captures exactly the
// skill-consultation moment without a config dependency.
func (e *Executor) recordSkillPick(call llm.ToolCall, args json.RawMessage) {
        if e == nil || call.Function.Name != "read" {
                return
        }
        var in struct {
                Path string `json:"path"`
        }
        if err := json.Unmarshal(args, &in); err != nil || in.Path == "" {
                return
        }
        skill, ok := skillstats.PickFromReadPath(in.Path)
        if !ok {
                return
        }
        v := session.DefaultVault()
        if v == nil {
                return // file layout: no durable store to record into
        }
        if err := skillstats.RecordPick(v.DB(), skill); err != nil {
                debuglog.Logf("skillstats: record pick %s: %v", skill, err)
        }
}

// admitExecution performs the APPA Gate 2 post-execution admission check.
// It folds the realized output into the trajectory state via gate.Admit,
// passing the reported outcome so effect tokens commit only on success.
// After Admit returns Allow, it calls ResolveUnresolved (if the gate
// implements UnresolvedSourceResolver) so registered casts fold unresolved
// sources incrementally instead of accumulating forever — the wire-up
// called for by doc/history-issues.md #9.
func (e *Executor) admitExecution(
        ctx context.Context,
        call llm.ToolCall,
        content, output string,
        emit func(session.ToolData) bool,
) error {
        req := permission.Request{
                Action:      toolAction(call.Function.Name),
                Tool:        call.Function.Name,
                OutputLabel: permission.DefaultTrustForToolOutput(call.Function.Name),
                Succeeded:   true,
        }
        if !e.outputLabeled(call.Function.Name) {
                // The tool declares no output label, so the realized output is an
                // unannotated source rather than an established one.
                req.OutputLabel = permission.Label{}
        }

        dec, reason := e.gate.Admit(ctx, req)
        switch dec {
        case permission.Allow:
                // Apply registered casts so unresolved sources fold into the
                // trajectory's Established label instead of piling up in
                // Unresolved. The executor stays decoupled from StaticGate by
                // going through the UnresolvedSourceResolver interface; gates
                // that don't implement it (e.g. AllowAll) just skip this step.
                if r, ok := e.gate.(permission.UnresolvedSourceResolver); ok {
                        _ = r.ResolveUnresolved()
                }
                return nil
        case permission.Deny:
                if reason == "" {
                        reason = "tool execution denied by admission check"
                }
                _ = emit(session.ToolData{Run: e.toolRun(call, session.ToolRejected, "", reason, output)})
                return errors.New(reason)
        case permission.Ask:
                if reason == "" {
                        reason = "tool execution requires admission approval"
                }
                if e.approveAdmit != nil {
                        req.Succeeded = true
                        e.approveAdmit(req)
                        return nil
                }
                _ = emit(session.ToolData{Run: e.toolRun(call, session.ToolRejected, "", reason, output)})
                return errors.New(reason)
        default:
                return errors.New("unknown admission decision")
        }
}

// outputLabeled reports whether the tool declares an output label, so an
// unannotated tool's result is treated as an unresolved source at admission.
func (*Executor) outputLabeled(tool string) bool {
        return permission.IsKnownTrust(permission.DefaultTrustForToolOutput(tool).Trust)
}

func toolAction(name string) permission.Action {
        switch name {
        case "read", "grep", "find", "ls", "search", "contract":
                // `search` folds the old grep/find/graph/rank tools via mode=grep|
                // find|graph|rank; all are read-only analysis. The mode-aware
                // extraction in permission/extract.go picks the right Action per
                // sub-mode; here at the executor level `search` is read-only.
                // `contract` declares done-criteria in engine state — read-class
                // (extract.go agrees) so it never prompts or trips readonly mode.
                return permission.ActionRead
        case "write", "edit":
                // `edit` was folded into `write` (mode=edit). The mode-aware
                // extraction picks ActionEdit for mode=edit and ActionWrite for
                // mode=write (or empty); the executor treats both as writes for
                // gate decisions.
                return permission.ActionWrite
        case "bash":
                return permission.ActionBash
        case "agent":
                return permission.ActionAgent
        default:
                // Mode-dispatched facade tools land here deliberately — `git` joins
                // `test` and `document` in NOT getting a case. This function only
                // feeds Gate 2 admission (admitExecution), and Admit does not
                // dispatch on Action; the mode-aware refinement that matters runs in
                // permission/extract.go before Gate 1's Check. Mapping "git" to
                // ActionRead here would be a lie for mode=commit, and there is no
                // single Action that fits all 12 modes — so it stays
                // Action("git"), inert at admission like Action("test").
                return permission.Action(name)
        }
}

// prospectiveGaps returns the prospective gaps of req so an authority ruling
// can be bound to the specific requirement it covers, rather than to an
// unnamed "consult" pseudo-gap. Nil when the gate exposes no trajectory.
func (e *Executor) prospectiveGaps(req permission.Request) []string {
        static := permission.StaticGateOf(e.gate)
        if static == nil {
                return nil
        }
        return static.EvaluateProspective(req).Gaps
}

// consultAuthority delegates to the gate's built-in authority handler so that
// call-scoped rulings are logged atomically in trajectory state. gaps are the
// prospective gaps the ruling is allowed to cover (Theorem 6: a dispatch that
// ignores a release-side requirement must consume a ruling over that exact gap).
func (e *Executor) consultAuthority(
        ctx context.Context,
        dec permission.Decision,
        reason string,
        req permission.Request,
        gaps []string,
) (permission.Decision, string) {
        if dec != permission.Ask {
                return dec, reason
        }
        static := permission.StaticGateOf(e.gate)
        if static == nil {
                return dec, reason
        }
        return static.ConsultAuthority(ctx, dec, reason, req, gaps)
}

func (e *Executor) checkPermission(
        ctx context.Context,
        call llm.ToolCall,
        args json.RawMessage,
        detail string,
        emit func(session.ToolData) bool,
) (llm.Message, bool) {
        req, err := permission.ExtractAt(call.Function.Name, args, e.cwd)
        if err != nil {
                // The raw parse error alone ("unexpected end of JSON input",
                // "cannot unmarshal array") is not something the model can
                // correct against; the arguments it describes ride along so the
                // re-issue has something to differ from.
                reason := fmt.Sprintf("permission check failed: %v (arguments: %s)", err, truncateForModel(string(args)))
                return e.rejectResult(call, detail, reason, emit), true
        }

        dec, reason := e.gate.Check(ctx, req)
        dec, reason = e.consultAuthority(ctx, dec, reason, req, e.prospectiveGaps(req))
        switch dec {
        case permission.Allow:
                return llm.Message{}, false
        case permission.Deny:
                if reason == "" {
                        reason = "tool execution denied by permissions"
                }
                return e.rejectResult(call, detail, reason, emit), true
        case permission.Ask:
                if e.ask == nil {
                        if reason == "" {
                                reason = "tool requires approval but no ask handler is configured"
                        }
                        return e.rejectResult(call, detail, reason, emit), true
                }
                res, askErr := func() (permission.AskResult, error) {
                        e.askMu.Lock()
                        defer e.askMu.Unlock()
                        return e.ask(ctx, req, reason)
                }()
                if askErr != nil {
                        msg := fmt.Sprintf("approval failed: %v", askErr)
                        return e.rejectResult(call, detail, msg, emit), true
                }
                if !res.Approved {
                        msg := "tool execution rejected by user"
                        if res.Feedback != "" {
                                msg = "This tool call was rejected by the user with feedback: " + res.Feedback
                        }
                        return e.rejectResult(call, detail, msg, emit), true
                }
                return llm.Message{}, false
        default:
                return e.rejectResult(call, detail, "unknown permission decision", emit), true
        }
}

func (e *Executor) rejectResult(
        call llm.ToolCall,
        detail, reason string,
        emit func(session.ToolData) bool,
) llm.Message {
        _ = emit(session.ToolData{Run: e.toolRun(call, session.ToolRejected, detail, reason, "")})
        return e.toolMessage(call.ID, reason, nil)
}

func (e *Executor) cancelResult(call llm.ToolCall, emit func(session.ToolData) bool) llm.Message {
        detail := call.Function.Arguments
        if tool, ok := e.registry[call.Function.Name]; ok && tool.DetailFromArgs != nil {
                if d := tool.DetailFromArgs(json.RawMessage(call.Function.Arguments)); d != "" {
                        detail = d
                }
        }
        _ = emit(session.ToolData{Run: e.toolRun(call, session.ToolCancelled, detail, "", ToolCanceledResult)})
        return e.toolMessage(call.ID, ToolCanceledResult, nil)
}

// toolRun builds a ToolData payload with Name always set so headless JSONL
// and stderr logs never omit toolName.
func (*Executor) toolRun(
        call llm.ToolCall,
        status session.ToolStatus,
        detail, errText, output string,
) session.ToolRun {
        return session.ToolRun{
                ToolUseID: call.ID,
                Name:      call.Function.Name,
                Status:    status,
                Detail:    detail,
                Error:     errText,
                Output:    output,
        }
}

func (*Executor) toolMessage(id, content string, images []llm.Image) llm.Message {
        return llm.Message{
                Role:       llm.RoleTool,
                ToolCallID: id,
                Content:    content,
                Images:     images,
        }
}

// emptyToolArgs reports whether a tool call's arguments carry no fields at
// all: blank, null, or a JSON object with zero keys. Anything else returns
// false — including an object that is missing required fields (the guard
// above explains why those must pass) and a non-object argument (the tool's
// own parse error to report, in its own voice).
func emptyToolArgs(args json.RawMessage) bool {
        trimmed := strings.TrimSpace(string(args))
        if trimmed == "" {
                return true
        }
        var fields map[string]json.RawMessage
        if err := json.Unmarshal([]byte(trimmed), &fields); err != nil {
                return false
        }
        return len(fields) == 0
}

// truncateForModel clips a raw string for inclusion in a model-facing error
// message. Rune-safe so a path never ends in a half character, and capped
// small: the message must teach what went wrong, not re-send the payload.
func truncateForModel(s string) string {
        const maxRunes = 160
        if r := []rune(s); len(r) > maxRunes {
                return string(r[:maxRunes]) + "…"
        }
        return s
}

// askHandoff routes a tool-reported handoff state through the permission
// ask flow and returns the model-facing note describing the outcome.
func (e *Executor) askHandoff(ctx context.Context, call llm.ToolCall, h *tooldef.Handoff) string {
        reason := "browser needs a human: " + h.Reason
        if h.URL != "" {
                reason += " — " + h.URL
        }
        if e.ask == nil {
                // Headless or bare executors have nobody to ask; the model still
                // learns the state and can surface it via the question tool.
                return reason + " (no ask handler configured; use the question tool to reach the user)"
        }
        req := permission.Request{
                Action: permission.ActionRead,
                Tool:   call.Function.Name,
        }
        res, err := func() (permission.AskResult, error) {
                e.askMu.Lock()
                defer e.askMu.Unlock()
                return e.ask(ctx, req, reason)
        }()
        if err != nil {
                return reason + " (ask failed: " + err.Error() + ")"
        }
        if res.Approved {
                return "user accepted the handoff (" + h.Reason + ") — they will act in the browser; continue from the current page state"
        }
        note := "user declined the handoff (" + h.Reason + ")"
        if res.Feedback != "" {
                note += " — " + res.Feedback
        }
        return note
}

// appendHandoffNote adds a handoff outcome to the tool result content.
func appendHandoffNote(content, note string) string {
        if note == "" {
                return content
        }
        if content == "" {
                return note
        }
        return content + "\n\n" + note
}

func joinExtContexts(parts ...string) string {
        var nonempty []string
        for _, p := range parts {
                if p != "" {
                        nonempty = append(nonempty, p)
                }
        }
        return strings.Join(nonempty, "\n\n")
}

// rememberFactFromArgs extracts the human-readable fact from a remember tool
// call's arguments for the memory-bank mirror. Only the content lands in the
// bank — kind/scope stay in the semantic store — so the persona manager's
// preview reads like notes, not JSON.
func rememberFactFromArgs(args json.RawMessage) string {
        var in struct {
                Content string `json:"content"`
        }
        if err := json.Unmarshal(args, &in); err != nil {
                return ""
        }
        fact := strings.TrimSpace(in.Content)
        // Long facts are truncated (rune-safe) to keep bank lines (and the
        // persona manager's one-line preview) scannable; the semantic store
        // keeps the full text.
        if runes := []rune(fact); len(runes) > 300 {
                fact = string(runes[:300]) + "…"
        }
        return fact
}

// appendExtContext adds model-facing extension notes. TUI Detail/Output stay clean.
func appendExtContext(content, ctx string) string {
        if ctx == "" {
                return content
        }
        escaped := util.ReplaceAll(ctx, extContextClose, "</ext_context\u200b>")
        block := extContextOpen + "\n" + escaped + "\n" + extContextClose
        if content == "" {
                return block
        }
        return content + "\n\n" + block
}
