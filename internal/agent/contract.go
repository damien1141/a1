package agent

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/damien1141/a1/internal/evidence"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/tools"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// defaultZeroRankBudget is how many extra verification rounds the harness
// buys when the model tries to end a turn with declared checks not green.
// The verdict gate (Phase 2) already bounds thrash DURING the turn; this is
// the last-mile backstop between "the model said done" and "done".
const defaultZeroRankBudget = 2

// turnContractState is the turn's declared done-criteria: a ratcheted set
// of check ids the harness judges against the evidence ledger at turn end.
//
// Authority split (VGR): the model PROPOSES the criteria (it knows what
// "done" means for this task); the harness HOLDS them and refuses to end
// the turn while any declared check is failing or unverified. The set only
// grows within a turn — a declaration can add checks but never remove
// them, so the model cannot weaken its own finish line mid-task (the same
// ratchet principle as the protected evidence set). A new user prompt
// starts a fresh contract.
type turnContractState struct {
	mu     sync.Mutex
	checks []string
}

// declare validates and merges ids into the contract. Returns the full
// held set, the newly added ids, and the refused ids (malformed, or
// already held). Removals are impossible by construction: anything not in
// ids that is already held simply stays held.
func (t *turnContractState) declare(ids []string) (held, added, refused []string) {
	if t == nil {
		return nil, nil, nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	heldSet := make(map[string]bool, len(t.checks))
	for _, c := range t.checks {
		heldSet[c] = true
	}
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if !evidence.ValidCheckID(id) {
			refused = append(refused, raw)
			continue
		}
		if heldSet[id] {
			continue
		}
		heldSet[id] = true
		t.checks = append(t.checks, id)
		added = append(added, id)
	}
	held = append([]string(nil), t.checks...)
	sort.Strings(held)
	sort.Strings(refused)
	return held, added, refused
}

// snapshot returns the held checks (sorted copy).
func (t *turnContractState) snapshot() []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := append([]string(nil), t.checks...)
	sort.Strings(out)
	return out
}

// reset clears the contract for a new turn.
func (t *turnContractState) reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.checks = nil
}

// contractTool builds the `contract` tool: the model's declaration surface
// for machine-checked done-criteria. Read-classed: it never touches the
// workspace (its only write is mutex-guarded engine state), so it batches
// with reads and never triggers a spurious pre-candidate baseline from the
// verdict gate.
func (engine *Engine) contractTool() tools.Tool {
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name: "contract",
			Description: "Declare the done-criteria for this task as machine-checked check ids. " +
				"The harness holds the contract and will not end the turn while any declared check is failing or has no evidence. " +
				"Ids: go-build, go-vet, cargo-build, js-test (npm/pnpm/yarn/bun test, vitest, jest), playwright-test, mypy, ruff, eslint, cargo-clippy, dotnet-test; " +
				"go-test:<package>, go-test:<prefix>/... (all packages under a prefix), go-test:... (whole module), go-test#<TestName>, pytest:<nodeid>, cargo-test:<name>; " +
				"exit:<code>:<command> — any single command's exit code, e.g. exit:0:make lint (declare it, then run the command so the harness records the result); " +
				"http:<status>:<url> — endpoint status via curl, e.g. http:200:localhost:3000/health (run curl -I against it); " +
				"port:<host>:<port> — open TCP port via nc -z, e.g. port:localhost:5432; " +
				"screenshot:<name> — visual check: declare it, then capture with browser_read mode=screenshot name:<same name> (first capture becomes the baseline, later ones diff against it); " +
				"check:<name> — checks defined in config.yaml. " +
				"Declared checks accumulate for the turn — they cannot be removed, only satisfied.",
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"checks": llm.Object{
						"type":        "array",
						"items":       llm.Object{"type": "string"},
						"description": "Check ids to declare, e.g. [\"go-build\", \"go-vet\", \"go-test:github.com/x/internal/auth\", \"exit:0:make lint\"]",
					},
				},
				Required: []string{"checks"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in struct {
				Checks []string `json:"checks"`
			}
			_ = json.Unmarshal(input, &in)
			if len(in.Checks) == 0 {
				return "contract"
			}
			joined := strings.Join(in.Checks, ", ")
			if len(joined) > 120 {
				joined = joined[:120] + "…"
			}
			return "declare: " + joined
		},
		Run: func(_ context.Context, input json.RawMessage) (tooldef.Result, error) {
			var in struct {
				Checks []string `json:"checks"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return tooldef.Result{}, errBadContractArgs
			}
			held, added, refused := engine.turnContract.declare(in.Checks)
			// Task contracts (Bet 04): every declaration also enters
			// the session-scoped task contract — never reset,
			// ratcheted — so spawns inherit the parent's declared
			// finish line.
			engine.taskContract.declare(in.Checks)

			// Declared watchable ids (exit:/http:/port:) arm the
			// ledger's observation gate: from this declaration on,
			// completed runs of the named command (or curl/nc
			// probes of the named target) record evidence for the
			// declared expectation. Nothing else ever grows the
			// ledger from an arbitrary bash call.
			for _, id := range added {
				engine.evidence.Watch(id)
			}

			statuses := engine.evidence.ContractStatus(held)
			var b strings.Builder
			if len(held) == 0 {
				b.WriteString(
					"No checks declared. Pass check ids like go-build, go-vet, go-test:<package>, go-test:<prefix>/...",
				)
				if len(refused) > 0 {
					b.WriteString("\nRefused (not valid check ids): " + strings.Join(refused, ", "))
				}
				return tooldef.Result{Content: b.String(), Summary: "contract: empty"}, nil
			}

			// A real declaration flips the session's coding-mode
			// latch: from this call on the session is governed —
			// the ledger records, the verdict gate may baseline
			// and roll back — and it stays on for the rest of the
			// session (sticky across turns). An empty or
			// all-refused declaration arms nothing, and budget
			// declarations never arm it either: spend is not a
			// finish line.
			engine.governed.Store(true)

			b.WriteString("Contract held by the harness: ")
			b.WriteString(strconv.Itoa(len(held)))
			b.WriteString(" check")
			if len(held) != 1 {
				b.WriteString("s")
			}
			if len(added) > 0 {
				b.WriteString(" (")
				b.WriteString(strconv.Itoa(len(added)))
				b.WriteString(" newly declared)")
			}
			b.WriteString(". Declared checks accumulate for this turn — they cannot be removed, only satisfied.")
			failing := 0
			for _, st := range statuses {
				b.WriteString("\n- ")
				b.WriteString(st.Check)
				b.WriteString(": ")
				switch st.State {
				case evidence.CheckPassing:
					b.WriteString("PASSING")
				case evidence.CheckFailing:
					failing++
					b.WriteString("FAILING — ")
					b.WriteString(st.Detail)
				default:
					failing++
					b.WriteString("NO EVIDENCE — run it so the harness can record the result")
				}
			}
			if len(refused) > 0 {
				b.WriteString("\nRefused (not valid check ids): " + strings.Join(refused, ", "))
			}
			b.WriteString("\nThe turn cannot end while any declared check is failing or has no evidence.")
			summary := "contract: " + strconv.Itoa(len(held)) + " checks"
			if failing > 0 {
				summary += ", " + strconv.Itoa(failing) + " not green"
			} else {
				summary += ", all green"
			}
			return tooldef.Result{Content: b.String(), Summary: summary}, nil
		},
	}
}

var errBadContractArgs = &contractArgsError{}

type contractArgsError struct{}

func (*contractArgsError) Error() string {
	return "contract: arguments must be {\"checks\": [\"<check-id>\", ...]}"
}

// zeroRankAction is the turn-end contract verdict. Note rides the
// transcript as a system note; Continue re-opens the loop with the
// residual set; Stop ends the turn (budget exhausted) instead of letting
// an uncertifiable "done" stand as if it were clean.
type zeroRankAction struct {
	Note     string
	Continue bool
	Stop     bool
}

// zeroRankCheck evaluates the turn's declared contract at the moment the
// model stops calling tools — the VGR zero-rank stop. With no declared
// checks (or the gate disabled) it is a no-op: the harness cannot guess
// what "done" means for an undeclared task, so it enforces exactly what
// was declared and nothing more.
//
// The judgment consumes last-known ledger evidence: a check that passed
// and later regressed reads failing, a never-run check reads no-evidence.
// The harness does not fabricate freshness — if the model mutated code
// after its last green run, the residual note tells it to re-verify, and
// the bounded budget keeps a stubborn model from parking there forever.
func (engine *Engine) zeroRankCheck(roundsUsed *int) zeroRankAction {
	checks := engine.turnContract.snapshot()
	if len(checks) == 0 {
		return zeroRankAction{}
	}
	budget := int(engine.zeroRankBudget.Load())
	if budget <= 0 {
		return zeroRankAction{} // zero-rank disabled: the model's done stands
	}
	residual := engine.evidence.Residual(checks)
	if len(residual) == 0 {
		// Zero-rank certificate: every declared check has current pass
		// evidence. The turn may end clean.
		return zeroRankAction{Note: certificateNote(checks)}
	}
	if roundsUsed != nil && *roundsUsed < budget {
		*roundsUsed++
		return zeroRankAction{Note: residualNote(residual, *roundsUsed, budget), Continue: true}
	}
	return zeroRankAction{Note: exhaustedNote(residual, budget), Stop: true}
}

func residualNote(residual []evidence.CheckStatus, used, budget int) string {
	var b strings.Builder
	b.WriteString(
		"System note: [contract] the turn cannot end yet — declared done-criteria are not all green (verification round ",
	)
	b.WriteString(strconv.Itoa(used))
	b.WriteString(" of ")
	b.WriteString(strconv.Itoa(budget))
	b.WriteString("):")
	b.WriteString(formatResidual(residual))
	b.WriteString(
		"\nFix or run these checks, then finish. Do not delete, skip, or weaken the failing checks to go green.",
	)
	return b.String()
}

func exhaustedNote(residual []evidence.CheckStatus, budget int) string {
	var b strings.Builder
	b.WriteString("System note: [contract] stopping this turn — verification budget (")
	b.WriteString(strconv.Itoa(budget))
	b.WriteString(" extra rounds) exhausted with declared checks still not green:")
	b.WriteString(formatResidual(residual))
	b.WriteString(
		"\nThe work is NOT certified complete. The user should review the residual set above before re-prompting.",
	)
	return b.String()
}

func certificateNote(checks []string) string {
	var b strings.Builder
	b.WriteString("System note: [contract] turn certified complete — all ")
	b.WriteString(strconv.Itoa(len(checks)))
	b.WriteString(" declared checks passing: ")
	b.WriteString(strings.Join(checks, ", "))
	b.WriteString(".")
	return b.String()
}

func formatResidual(residual []evidence.CheckStatus) string {
	var b strings.Builder
	for _, st := range residual {
		b.WriteString("\n- ")
		switch st.State {
		case evidence.CheckFailing:
			b.WriteString("FAILING: ")
			b.WriteString(st.Detail)
		default:
			b.WriteString("NO EVIDENCE: ")
			b.WriteString(st.Check)
			b.WriteString(" — never observed passing; run it")
		}
	}
	return b.String()
}
