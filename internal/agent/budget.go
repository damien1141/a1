package agent

import (
        "context"
        "encoding/json"
        "strconv"
        "strings"
        "sync"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools"
        "github.com/damien1141/a1/internal/tools/tooldef"
)

// Budget contracting (evidence loop Phase 5). Every budget a1 had before —
// maxRounds, maxToolCalls, the verdict gate's stall limit, the zero-rank
// verification budget — is a harness-side static cap. Gate 1's STOPPING RULE
// doctrine ("explicit exit condition before any loop starts") was prompt-only
// and unverifiable. The VGR authority split says the model PROPOSES the
// budget (it knows how big the task is) and the harness HOLDS it: the
// declaration is a visible tool call, the ceiling is engine state the model
// cannot edit, and firing is harness-judged.
//
// Firing semantics — the ceiling binds without trapping the model:
//   - under the ceiling: nothing happens;
//   - at the ceiling with tools still wanted: the round runs on the turn's
//     ONE re-open (a grace round), and a note lands after the batch saying
//     so. The model can then extend with a fresh declaration (each
//     extension is a visible tool call) or wrap up with a final answer (the
//     zero-rank gate judges the contract as usual);
//   - a second work round without a new declaration: the pending calls are
//     rejected (never executed past the declared budget), the session stays
//     consistent (no unanswered tool calls), and the turn stops with a spend
//     report — including the contract residual, so an out-of-budget stop is
//     never mistaken for a certified one.
//
// Declaration rounds — batches containing ONLY `budget`/`contract` calls —
// never fire the gate. Without the exemption the grace note's "extend now"
// instruction would be a trap: the model reads the note after the grace
// round, and its declaring response would itself be rejected as an overrun.
// The exemption cannot bypass the gate: a mixed batch (declaration + work)
// is not declaration-only, so the gate judges it before any call runs.
//
// A turn with no declaration (plain chat) is untouched: the gate is inert
// unless the model bound itself. The harness hard caps (maxRounds,
// maxToolCalls) still backstop everything — a declared budget only ever
// tightens, never loosens, the harness's own limits.

// turnBudgetState is the turn's declared tool-round budget: an absolute
// round ceiling the model proposed via the `budget` tool and the harness
// holds. Mutex-guarded because the tool handler (inside executor.Run) and
// the loop both touch it. Reset at every Loop entry; a new user prompt
// starts a fresh budget.
type turnBudgetState struct {
        mu           sync.Mutex
        ceiling      int  // absolute round count the budget covers; 0 = none declared
        declarations int  // declarations this turn (extensions = declarations-1)
        reopens      int  // grace rounds consumed (rounds run past the ceiling on the re-open)
        reopenArmed  bool // one re-open per ceiling generation
}

// declare holds n more rounds from the current position: the ceiling becomes
// from+n (from = rounds already used, so the model gets exactly n more
// executed rounds before the gate can fire again). Every declaration re-arms
// the re-open — extending is always possible and always visible, because the
// declaration is a tool call in the transcript.
func (t *turnBudgetState) declare(from, n int) (ceiling, declarations int) {
        if t == nil {
                return 0, 0
        }
        t.mu.Lock()
        defer t.mu.Unlock()
        t.ceiling = from + n
        t.declarations++
        t.reopenArmed = true
        return t.ceiling, t.declarations
}

// tripped reports whether the held ceiling is reached at roundsUsed, and
// consumes the generation's re-open when it is: the first firing of a
// declaration runs as a grace round (the model may extend mid-round or wrap
// up after it), the next firing without a new declaration is terminal.
func (t *turnBudgetState) tripped(roundsUsed int) (fired, grace bool) {
        if t == nil {
                return false, false
        }
        t.mu.Lock()
        defer t.mu.Unlock()
        if t.ceiling <= 0 || roundsUsed < t.ceiling {
                return false, false
        }
        if t.reopenArmed {
                t.reopenArmed = false
                t.reopens++
                return true, true
        }
        return true, false
}

// stillSpent reports whether the ceiling is still reached at roundsUsed.
// Used after a grace round: a declaration made during it moved the ceiling
// past roundsUsed, and the declaration reply already said everything — no
// extra note is needed then.
func (t *turnBudgetState) stillSpent(roundsUsed int) bool {
        if t == nil {
                return false
        }
        t.mu.Lock()
        defer t.mu.Unlock()
        return t.ceiling > 0 && roundsUsed >= t.ceiling
}

// snapshot returns the held state for reports.
func (t *turnBudgetState) snapshot() (ceiling, declarations, reopens int) {
        if t == nil {
                return 0, 0, 0
        }
        t.mu.Lock()
        defer t.mu.Unlock()
        return t.ceiling, t.declarations, t.reopens
}

// reset clears the budget for a new turn.
func (t *turnBudgetState) reset() {
        if t == nil {
                return
        }
        t.mu.Lock()
        defer t.mu.Unlock()
        t.ceiling = 0
        t.declarations = 0
        t.reopens = 0
        t.reopenArmed = false
}

// budgetTool builds the `budget` tool: the model's declaration surface for
// tool-round budgets. Read-classed: it never touches the workspace (its only
// write is mutex-guarded engine state), so it batches with reads and never
// triggers a spurious pre-candidate baseline from the verdict gate.
func (engine *Engine) budgetTool() tools.Tool {
        return tools.Tool{
                Definition: llm.ToolDefinition{
                        Name: "budget",
                        Description: "Declare how many more tool rounds the remaining work needs. " +
                                "The harness holds the budget: when the rounds are spent and you still want tools, " +
                                "the next round runs as the turn's one re-open — extend by declaring again (each extension " +
                                "is a visible tool call) or finish with a final answer — and a further overrun stops the turn " +
                                "with a spend report. Declare before starting any loop; extend honestly rather than padding. " +
                                "Pairs with the contract tool: budgets bound the spend, contracts bound the finish line.",
                        Params: &llm.FunctionParameters{
                                Type: "object",
                                Properties: llm.Object{
                                        "rounds": llm.Object{
                                                "type":        "integer",
                                                "minimum":     1,
                                                "description": "Additional tool rounds from now (e.g. 5)",
                                        },
                                },
                                Required: []string{"rounds"},
                        },
                        Readable: true,
                },
                DetailFromArgs: func(input json.RawMessage) string {
                        var in struct {
                                Rounds int `json:"rounds"`
                        }
                        _ = json.Unmarshal(input, &in)
                        if in.Rounds < 1 {
                                return "budget"
                        }
                        return "budget: +" + strconv.Itoa(in.Rounds) + " rounds"
                },
                Run: func(_ context.Context, input json.RawMessage) (tooldef.Result, error) {
                        var in struct {
                                Rounds int `json:"rounds"`
                        }
                        if err := json.Unmarshal(input, &in); err != nil || in.Rounds < 1 {
                                return tooldef.Result{}, errBadBudgetArgs
                        }
                        used := int(engine.toolRoundsLive.Load())
                        ceiling, declarations := engine.turnBudget.declare(used, in.Rounds)

                        var b strings.Builder
                        b.WriteString("Budget held by the harness: ")
                        b.WriteString(strconv.Itoa(in.Rounds))
                        b.WriteString(" more tool round")
                        if in.Rounds != 1 {
                                b.WriteString("s")
                        }
                        b.WriteString(" (rounds used: ")
                        b.WriteString(strconv.Itoa(used))
                        b.WriteString(" → ceiling: ")
                        b.WriteString(strconv.Itoa(ceiling))
                        b.WriteString(")")
                        if declarations > 1 {
                                b.WriteString(", extension #")
                                b.WriteString(strconv.Itoa(declarations - 1))
                        }
                        b.WriteString(".")
                        b.WriteString("\nWhen the ceiling is reached the next round runs on the turn's one re-open: extend by declaring again (each extension is a visible tool call) or finish with a final answer. The next round that does any work without a fresh declaration stops the turn.")
                        if checks := engine.turnContract.snapshot(); len(checks) > 0 {
                                residual := engine.evidence.Residual(checks)
                                if len(residual) > 0 {
                                        b.WriteString("\nContract: ")
                                        b.WriteString(strconv.Itoa(len(checks)))
                                        b.WriteString(" check")
                                        if len(checks) != 1 {
                                                b.WriteString("s")
                                        }
                                        b.WriteString(" held, ")
                                        b.WriteString(strconv.Itoa(len(residual)))
                                        b.WriteString(" not green — stopping without them leaves the work uncertified.")
                                } else {
                                        b.WriteString("\nContract: all ")
                                        b.WriteString(strconv.Itoa(len(checks)))
                                        b.WriteString(" declared checks green.")
                                }
                        } else {
                                b.WriteString("\nNo contract declared — a budget pairs best with declared done-criteria (contract tool).")
                        }
                        if !engine.budgetContracts.Load() {
                                b.WriteString("\nBudget enforcement is disabled on this engine: recorded for the record only, the budget will not stop the turn.")
                        }
                        return tooldef.Result{Content: b.String(), Summary: "budget: +" + strconv.Itoa(in.Rounds) + " rounds"}, nil
                },
        }
}

var errBadBudgetArgs = &budgetArgsError{}

type budgetArgsError struct{}

func (*budgetArgsError) Error() string {
        return "budget: arguments must be {\"rounds\": <integer >= 1>}"
}

// budgetAction is the declared-budget gate's verdict for a round that wants
// tool calls. Grace: the round runs on the turn's re-open and a note lands
// after the batch. Terminal: the pending calls are rejected and the turn
// stops with a spend report.
type budgetAction struct {
        Grace    bool
        Terminal bool
}

// budgetDeclarationBatch reports whether every call in the batch is a
// declaration — `budget` or `contract`, the model's voice over its own
// bindings — with no workspace work. Such rounds always run: the extend
// path must stay usable after a firing, or the grace note's "extend now"
// instruction would be a trap (the declaring response itself would read as
// an overrun). The exemption cannot be abused to work past the budget: a
// batch that mixes a declaration with any work tool is not declaration-only,
// and the gate judges batches before any call in them runs.
func budgetDeclarationBatch(calls []llm.ToolCall) bool {
        if len(calls) == 0 {
                return false
        }
        for _, call := range calls {
                if call.Function.Name != "budget" && call.Function.Name != "contract" {
                        return false
                }
        }
        return true
}

// budgetGate evaluates the declared-budget gate for a round at roundsUsed
// that wants tool calls. Inert by construction: enforcement disabled, no
// ceiling held, under the ceiling, no tool calls, or a declaration-only
// batch — zero action, the loop is byte-identical to a turn that never
// declared.
func (engine *Engine) budgetGate(roundsUsed int, calls []llm.ToolCall) budgetAction {
        if engine == nil || !engine.budgetContracts.Load() || len(calls) == 0 {
                return budgetAction{}
        }
        if budgetDeclarationBatch(calls) {
                return budgetAction{}
        }
        fired, grace := engine.turnBudget.tripped(roundsUsed)
        if !fired {
                return budgetAction{}
        }
        if grace {
                return budgetAction{Grace: true}
        }
        return budgetAction{Terminal: true}
}

// budgetTerminalStop ends the turn on a second overrun. The model's pending
// tool calls come back as rejected results (never executed past the declared
// budget; the session never holds an unanswered tool call), the terminal
// message is persisted so the transcript shows what was wanted, and the
// spend report lands as a system note — with the contract residual when a
// contract is declared, so an out-of-budget stop is never mistaken for a
// certified one.
func (engine *Engine) budgetTerminalStop(
        msg llm.Message,
        completeEvent session.Event,
        roundsUsed int,
        yield func(session.Event, error) bool,
) {
        if !yield(completeEvent, nil) {
                return
        }
        reason := "[budget] rejected: declared tool-round budget exhausted (extend with the budget tool or finish with a final answer)"
        toolMsgs := engine.executor.RejectBatch(msg.ToolCalls, reason, func(td session.ToolData) bool {
                return yield(td, nil)
        })
        if err := engine.session.Append(msg); err != nil {
                yield(nil, err)
                return
        }
        if len(toolMsgs) > 0 {
                if err := engine.session.Append(toolMsgs...); err != nil {
                        yield(nil, err)
                        return
                }
        }
        engine.extensions.EmitTurnEnd(roundsUsed)
        note := engine.budgetTerminalNote(roundsUsed, len(msg.ToolCalls))
        if err := engine.session.Append(llm.Message{Role: llm.RoleUser, Content: note}); err != nil {
                yield(nil, err)
                return
        }
        yield(session.UserAppend{Text: note}, nil)
}

// budgetGraceNote is injected after a grace round whose ceiling was not
// extended mid-round. The model already burned its re-open; the note makes
// the state impossible to miss before the next round decides the turn.
func (engine *Engine) budgetGraceNote(roundsUsed int) string {
        ceiling, declarations, reopens := engine.turnBudget.snapshot()
        var b strings.Builder
        b.WriteString("System note: [budget] declared tool-round budget reached — the last round ran on this turn's re-open (rounds used: ")
        b.WriteString(strconv.Itoa(roundsUsed))
        b.WriteString(", ceiling: ")
        b.WriteString(strconv.Itoa(ceiling))
        b.WriteString(", extensions: ")
        b.WriteString(strconv.Itoa(declarations - 1))
        b.WriteString(", re-opens used: ")
        b.WriteString(strconv.Itoa(reopens))
        b.WriteString(").")
        b.WriteString("\nExtend now with the budget tool (each extension is a visible tool call in the transcript), or finish with a final answer. The next round that does any work without a fresh declaration stops the turn.")
        if checks := engine.turnContract.snapshot(); len(checks) > 0 {
                if residual := engine.evidence.Residual(checks); len(residual) > 0 {
                        b.WriteString("\nDeclared checks not green:")
                        b.WriteString(formatResidual(residual))
                        b.WriteString("\nFinishing now leaves the work uncertified.")
                }
        }
        return b.String()
}

// budgetTerminalNote is the spend report that ends a budget-stopped turn.
func (engine *Engine) budgetTerminalNote(roundsUsed, pending int) string {
        ceiling, declarations, reopens := engine.turnBudget.snapshot()
        var b strings.Builder
        b.WriteString("System note: [budget] declared tool-round budget exhausted — stopping this turn (rounds used: ")
        b.WriteString(strconv.Itoa(roundsUsed))
        b.WriteString(", ceiling: ")
        b.WriteString(strconv.Itoa(ceiling))
        b.WriteString(", extensions: ")
        b.WriteString(strconv.Itoa(declarations - 1))
        b.WriteString(", re-opens used: ")
        b.WriteString(strconv.Itoa(reopens))
        b.WriteString("). ")
        b.WriteString(strconv.Itoa(pending))
        b.WriteString(" pending tool call")
        if pending != 1 {
                b.WriteString("s")
        }
        b.WriteString(" were rejected, not executed.")
        if checks := engine.turnContract.snapshot(); len(checks) > 0 {
                if residual := engine.evidence.Residual(checks); len(residual) > 0 {
                        b.WriteString("\nContract: ")
                        b.WriteString(strconv.Itoa(len(residual)))
                        b.WriteString(" of ")
                        b.WriteString(strconv.Itoa(len(checks)))
                        b.WriteString(" declared checks not green — the work is NOT certified complete:")
                        b.WriteString(formatResidual(residual))
                        b.WriteString("\nThe user should review the residual set above before re-prompting.")
                } else {
                        b.WriteString("\nContract: all ")
                        b.WriteString(strconv.Itoa(len(checks)))
                        b.WriteString(" declared checks green at the stop.")
                }
        } else {
                b.WriteString("\nNo contract declared — the harness cannot certify this stop; review the transcript before re-prompting.")
        }
        return b.String()
}
