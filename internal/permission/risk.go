package permission

import (
        "context"
        "fmt"
        "strings"

        "github.com/damien1141/a1/internal/debuglog"
)

// Bash risk classification: the second net for the commands the regex
// lists miss.
//
// The deny list is instant, deterministic, and correct for the knowns
// (sudo, rm -rf, curl|sh). But it is a list, and a novel destructive
// command — git push --force, find / -delete, chmod -R 777 / — walks
// past it into the ambiguity zone: no list matched, containment is not
// enforcing, and the policy default (often Allow for sub-agents) is all
// that is left. That is the hole the risk classifier fills.
//
// The contract is fail-closed toward today's behavior, not toward
// permissiveness:
//
//   - The classifier runs ONLY in the ambiguity zone. Allowlisted and
//     denied commands never pay the round trip; sandbox-contained
//     commands are already answered by containment.
//   - A verdict only tightens. Destructive + irreversible + confident
//     maps to Ask in interactive mode (the human decides, with the
//     verdict as a pre-annotated prompt) and Deny everywhere else
//     (autopilot, headless-strict, readonly — where Ask would fold to
//     Deny anyway, the direct Deny keeps the real reason).
//   - Nothing maps to Allow. A "looks safe" verdict changes nothing:
//     the allowlist and containment own the clearly-safe fast path, and
//     they are deterministic and free.
//   - Every miss — classifier unset, unconfigured, errored, timed out,
//     low confidence, or inconclusive — falls through to exactly the
//     decision today's policy would have made.
//   - Yolo skips the classifier entirely: the operator said stop
//     asking, and the regex deny list still fires first.

// riskConfidenceFloor is the minimum confidence a blocking verdict needs.
// Higher than the compaction boundary floor on purpose: a Deny in an
// autonomous context needs more than a nudge, and a false positive costs
// a turn while a false negative costs the workspace.
const riskConfidenceFloor = 0.70

// BashRiskVerdict is a risk classifier's judgment of one bash command:
// raw signals plus the confidence of the blocking judgment. The gate,
// not the classifier, applies the confidence floor and the mode mapping.
type BashRiskVerdict struct {
        // Destructive reports whether the command plausibly destroys,
        // overwrites, or mutates state (files, history, data, configuration).
        Destructive bool
        // Irreversible reports whether the effect is not cheaply undoable
        // (no reflog, no trash, no re-create).
        Irreversible bool
        // Confidence is the classifier's confidence in the verdict pair.
        Confidence float64
        // Reason is a short human-readable basis, shown in the Ask prompt.
        Reason string
}

// BashRiskClassifier judges one bash command before execution.
// Implementations ask a decision model; an error means "no opinion" —
// callers fall through to the policy default.
type BashRiskClassifier interface {
        AskRisk(ctx context.Context, command string) (BashRiskVerdict, error)
}

// checkBashRisk runs the ambiguity-zone classifier and maps a blocking
// verdict to the mode-appropriate decision. ok=false means "no opinion":
// the caller proceeds exactly as it would have without a classifier.
func (g *StaticGate) checkBashRisk(ctx context.Context, cmd string) (dec Decision, reason string, ok bool) {
        if g.riskClassifier == nil || g.Policy.Mode == ModeYolo {
                return Allow, "", false
        }
        verdict, err := g.riskClassifier.AskRisk(ctx, cmd)
        if err != nil {
                // The miss is logged, not silent: an operator who enabled the
                // guard and sees an ambiguous command pass deserves the trail —
                // same discipline as the fetch router and evidence advisor.
                debuglog.Logf("permission: risk classifier miss on %q: %v", truncate(cmd, 120), err)
                return Allow, "", false
        }
        if !verdict.Destructive || !verdict.Irreversible || verdict.Confidence < riskConfidenceFloor {
                return Allow, "", false
        }
        reason = fmt.Sprintf("risk model: destructive and hard to reverse (confidence %.2f)", verdict.Confidence)
        if r := strings.TrimSpace(verdict.Reason); r != "" {
                reason += ": " + r
        }
        mode := g.Policy.Mode
        if mode == "" {
                mode = ModeInteractive
        }
        if mode == ModeInteractive {
                // The human is available: annotate the prompt instead of deciding
                // for them. Same interruption as today's unannotated Ask, with the
                // verdict doing the triage.
                return Ask, reason + " — requires approval", true
        }
        // Autonomous or folded modes: Ask would fold to Deny anyway; the
        // direct Deny keeps the real reason in the audit trail.
        return Deny, reason, true
}
