package agent

import (
        "context"
        "encoding/json"
        "regexp"
        "strconv"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/tools/tooldef"
)

// Markers mirroring bashtool's result formatting. The bash tool embeds the
// outcome in the returned content rather than in a structured field, so the
// evidence hook reads the same signals the model sees — the ledger and the
// model can never disagree about whether a command failed.
const (
        bashFailureMarker = "\n(exit error: "
        bashCancelMarker  = "(command canceled or timed out)"
)

// exitStatusRe extracts the process exit code bashtool embeds on failure:
// "\n(exit error: exit status 2)". Signal deaths ("signal: killed") leave
// no code; those observations run with an unknown code (-1).
var exitStatusRe = regexp.MustCompile(`\(exit error: exit status (\d+)\)`)

// recordEvidence folds one completed bash call into the evidence ledger.
// It returns an advisory note for protected regressions ("" otherwise).
//
// Rules:
//   - an ungoverned session records nothing at all — the latch is the
//     chat-mode ergonomics guard, so an incidental build command in a chat
//     turn neither grows the ledger nor arms regression notes for later
//     casual turns in the same session;
//   - only bash calls with a parseable "command" argument are observed;
//   - canceled or timed-out runs are never recorded — incomplete output
//     would poison the ledger with false failures;
//   - everything else goes through evidence classification, which is
//     conservative about what it recognizes;
//   - when nothing deterministic recognized the command AND a JEV advisor
//     is configured, the decision model judges whether it was a check run
//     and how it came out (jev_evidence.go). The fallback can only add
//     evidence for otherwise-invisible commands — never override a
//     deterministic verdict, and never record on error, timeout, or low
//     confidence.
func (e *Executor) recordEvidence(ctx context.Context, args json.RawMessage, output string) string {
        if e == nil || e.evidence == nil {
                return ""
        }
        if e.governed != nil && !e.governed() {
                return ""
        }
        var in struct {
                Command string `json:"command"`
                Mode    string `json:"mode"`
        }
        if err := json.Unmarshal(args, &in); err != nil {
                return ""
        }
        // Background bash modes never reach the ledger: a start returns before
        // the command completes (no exit code exists yet) and attaches return a
        // partial tail. Recording either would fabricate evidence — a suite that
        // is still running must never read as passing.
        if in.Mode != "" && in.Mode != "run" {
                return ""
        }
        command := strings.TrimSpace(in.Command)
        if command == "" {
                return ""
        }
        if strings.Contains(output, bashCancelMarker) {
                return ""
        }
        failed := strings.Contains(output, bashFailureMarker)
        exitCode := -1
        if m := exitStatusRe.FindStringSubmatch(output); m != nil {
                if n, err := strconv.Atoi(m[1]); err == nil {
                        exitCode = n
                }
        }
        verdict := e.evidence.ObserveExit(command, output, exitCode, failed, time.Now())
        // JEV fallback (decision-model evidence admission): only when the
        // deterministic path recorded nothing, only when configured. An
        // admitted verdict replaces the empty one wholesale, so the
        // candidate marking and regression-note logic below treat it
        // exactly like a deterministic record.
        if !verdict.Any() && e.jevAdv != nil {
                if status, detail, ok := e.jevAdv.judge(ctx, command, output, exitCode, failed); ok {
                        verdict = e.evidence.ObserveJev(command, status, detail, time.Now())
                }
        }
        // Every non-empty verdict is collected for the engine's verdict gate
        // (Phase 2): its presence marks the batch as a candidate, and the gate
        // drains the slice after Run. Neutral verdicts matter too — re-running
        // green checks is a verification round, not "nothing happened".
        if verdict.Any() {
                e.evidenceMu.Lock()
                e.evidenceVerdicts = append(e.evidenceVerdicts, verdict)
                e.evidenceMu.Unlock()
        }
        if !verdict.Regression() {
                return ""
        }
        return evidence.RegressionNote(verdict.Regressed)
}

// recordScreenshotEvidence grades a named browser capture against its
// declared screenshot:<name> watch. Same rules as the bash hook: an
// ungoverned session records nothing, and only a protected regression
// produces an advisory note for the model.
func (e *Executor) recordScreenshotEvidence(fact *tooldef.ScreenshotFact) string {
        if e == nil || e.evidence == nil {
                return ""
        }
        if e.governed != nil && !e.governed() {
                return ""
        }
        if fact == nil || fact.Name == "" || fact.Path == "" {
                return ""
        }
        verdict := e.evidence.ObserveScreenshot(fact.Name, fact.Path, time.Now())
        if verdict.Any() {
                e.evidenceMu.Lock()
                e.evidenceVerdicts = append(e.evidenceVerdicts, verdict)
                e.evidenceMu.Unlock()
        }
        if !verdict.Regression() {
                return ""
        }
        return evidence.RegressionNote(verdict.Regressed)
}
