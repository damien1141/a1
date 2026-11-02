package agent

import (
        "context"
        "fmt"
        "regexp"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/debuglog"
        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/llm/jev"
)

// JEV evidence admission: the decision model as the long-tail classifier.
//
// The deterministic classifiers (internal/evidence) recognize the major
// runners — go, pytest, cargo, npm/jest/vitest, mypy, ruff, eslint, clippy,
// dotnet, plus declared watches. Everything else (`make check`, `just
// verify`, `./ci.sh`, a repo's custom script) records nothing, so those
// verification rounds are invisible to the verdict gate: the batch is not a
// candidate, regressions on them are never caught, and contracts that
// reference them read NO EVIDENCE forever.
//
// The JEV model (TypeSafe System One: it does not generate text, it makes
// decisions fast) closes that tail. When a completed bash command produced
// no deterministic evidence, the advisor asks one Score question — was this
// a verification run, and did it pass? A confident pass/fail becomes a
// jev:<command-slug> ledger record that ratchets like any other.
//
// Fallback discipline (the user's rule: jev is optional, never load-bearing
// alone): the advisor is nil unless a decision model is configured (the
// jev: section of config.yaml — api or ollama — or the TYPESAFE_API_KEY
// fallback); a network error, timeout, low-confidence answer, or "not a
// check" verdict records nothing; and the deterministic path always wins
// — the advisor is consulted only when classification produced zero
// records.

const (
        // jevEvidenceTimeout bounds one verdict call against the hosted API.
        // The advisor runs inside the tool round; a wedged endpoint must not
        // stall the turn. The Ollama provider raises this ceiling (a local
        // model can take seconds), and jev.timeout_sec overrides both — see
        // jev.EffectiveTimeout.
        jevEvidenceTimeout = 4 * time.Second
        // jevMinConfidence is the admission bar. A JEV record can protect (a
        // pass ratchets; a later fail regresses and can roll back), so an
        // uncertain verdict records nothing rather than a coin flip.
        jevMinConfidence = 0.6
        // jevOutputCap bounds the output sent as decision state.
        jevOutputCap = 8000
        // jevFailDetailCap bounds the first-error-line detail for failures.
        jevFailDetailCap = 120
)

// jevAsker is the seam over the jev backends the tests fake.
type jevAsker = jev.Asker

// jevEvidenceAdvisor judges otherwise-unclassified bash runs.
type jevEvidenceAdvisor struct {
        asker   jevAsker
        timeout time.Duration
}

// newJevEvidenceAdvisor returns nil unless a decision model is
// configured (jev: section in config.yaml — api or ollama — or the
// TYPESAFE_API_KEY fallback). Nil means "deterministic classifiers only"
// — the engine and executor treat it as absent. The asker resolves its
// backend per call, so a mid-session config reload (the web GUI's
// save→reload→next-turn flow) reaches this advisor without a rebuild.
func newJevEvidenceAdvisor() *jevEvidenceAdvisor {
        if !jev.Configured() {
                return nil
        }
        return &jevEvidenceAdvisor{asker: jev.NewConfiguredAsker(), timeout: jev.EffectiveTimeout(jevEvidenceTimeout)}
}

// jevCheckishRe is the cheap pre-filter that keeps ordinary commands (cat,
// ls, git status, curl) from paying a network round-trip: only commands
// that name a verification concept are worth asking about. False negatives
// (a check whose name mentions none of these) simply stay unclassified —
// the status quo — and the deterministic classifiers cover the common
// runners regardless.
var jevCheckishRe = regexp.MustCompile(`(?i)\b(tests?|check|lint|vet|verify|build|audit|validate|typecheck|spec|e2e|ci|bench|quality|gate)\b`)

// judge asks the decision model about one completed command. It returns
// (status, detail, ok): ok=false records nothing (not a check, unclear,
// unconfident, errored, or timed out); detail is the pre-rendered record
// detail (first error line + provenance for failures, "" for passes).
func (a *jevEvidenceAdvisor) judge(ctx context.Context, command, output string, exitCode int, failed bool) (evidence.Status, string, bool) {
        if a == nil || a.asker == nil {
                return "", "", false
        }
        if !jevCheckishRe.MatchString(command) {
                return "", "", false
        }
        if len(output) > jevOutputCap {
                output = output[:jevOutputCap] + "\n[... output truncated ...]"
        }
        exit := any(exitCode)
        if exitCode < 0 {
                exit = "unknown"
        }
        state := map[string]any{
                "command":   command,
                "exit_code": exit,
                "failed":    failed,
                "output":    output,
        }
        qCtx, cancel := context.WithTimeout(ctx, a.timeout)
        defer cancel()
        resp, err := a.asker.SystemOneWithOptions(qCtx, state, jev.Questions{
                "outcome": jev.Score{
                        Instructions: "Judge whether this shell command was a verification run (a check with a pass/fail outcome) and, if it was, whether it passed. exit_code is the process exit status ('unknown' when the harness could not parse one); failed reports whether the run terminated non-zero. Answer 0 rather than guessing when the command has no pass/fail meaning; answer 2 when the output is genuinely ambiguous.",
                        Criteria: []any{
                                "Not a verification command — no pass/fail meaning (listing, reading, writing, server start, git status, ...).",
                                "A verification command that clearly FAILED (errors, test failures, failing exit).",
                                "A verification command whose outcome is UNCLEAR from this output.",
                                "Likely PASSED — success indicators present but not fully conclusive.",
                                "Clearly PASSED — explicit success markers, or a clean exit 0 for a check.",
                        },
                },
        }, jev.Options{})
        if err != nil {
                debuglog.Logf("agent: jev evidence judge %q: %v", command, err)
                return "", "", false
        }
        ans, ok := resp.Answers["outcome"]
        if !ok || ans.Type != jev.AnswerScore || ans.Confidence < jevMinConfidence {
                return "", "", false
        }
        switch ans.Score {
        case 1:
                detail := firstOutputLine(output)
                if len(detail) > jevFailDetailCap {
                        detail = detail[:jevFailDetailCap]
                }
                if detail != "" {
                        detail += " "
                }
                return evidence.Fail, detail + fmt.Sprintf("[jev verdict, confidence %.2f]", ans.Confidence), true
        case 3, 4:
                return evidence.Pass, "", true
        }
        // 0 (not a check) and 2 (unclear): no record.
        return "", "", false
}

// firstOutputLine returns the first non-empty line of output — the same
// "first error line" convention the deterministic classifiers use.
func firstOutputLine(output string) string {
        for _, line := range strings.Split(output, "\n") {
                if l := strings.TrimSpace(line); l != "" {
                        return l
                }
        }
        return ""
}
