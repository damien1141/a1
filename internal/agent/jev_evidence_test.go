package agent

import (
        "context"
        "encoding/json"
        "errors"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/llm/jev"
)

// fakeJevAsker scripts one System One answer per call.
type fakeJevAsker struct {
        resp    jev.Response
        err     error
        calls   int
        lastCtx context.Context
}

func (f *fakeJevAsker) SystemOneWithOptions(
        ctx context.Context, _ any, _ jev.Questions, _ jev.Options,
) (jev.Response, error) {
        f.calls++
        f.lastCtx = ctx
        return f.resp, f.err
}

func scoreAnswer(score float64, confidence float64) jev.Answer {
        return jev.Answer{
                Type:       jev.AnswerScore,
                Score:      score,
                Confidence: confidence,
        }
}

// TestJevEvidenceAdvisorRubricMapping pins the decision table: 0 (not a
// check) and 2 (unclear) record nothing; 1 fails; 3 and 4 pass.
func TestJevEvidenceAdvisorRubricMapping(t *testing.T) {
        cases := []struct {
                name       string
                score      float64
                confidence float64
                wantOK     bool
                wantStatus evidence.Status
        }{
                {"not a check", 0, 0.99, false, ""},
                {"clearly failed", 1, 0.9, true, evidence.Fail},
                {"unclear", 2, 0.99, false, ""},
                {"likely passed", 3, 0.8, true, evidence.Pass},
                {"clearly passed", 4, 0.95, true, evidence.Pass},
                {"pass but unconfident", 4, 0.5, false, ""},
                {"fail but unconfident", 1, 0.59, false, ""},
        }
        for _, tc := range cases {
                t.Run(tc.name, func(t *testing.T) {
                        asker := &fakeJevAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                                "outcome": scoreAnswer(tc.score, tc.confidence),
                        }}}
                        a := &jevEvidenceAdvisor{asker: asker, timeout: jevEvidenceTimeout}
                        status, _, ok := a.judge(t.Context(), "make check", "all good", 0, false)
                        assert.Equal(t, tc.wantOK, ok)
                        assert.Equal(t, tc.wantStatus, status)
                })
        }
}

// TestJevEvidenceAdvisorPreFilter: commands that name no verification
// concept never pay the round-trip.
func TestJevEvidenceAdvisorPreFilter(t *testing.T) {
        asker := &fakeJevAsker{}
        a := &jevEvidenceAdvisor{asker: asker, timeout: jevEvidenceTimeout}
        for _, cmd := range []string{"ls -la", "cat main.go", "git status", "echo hi"} {
                _, _, ok := a.judge(t.Context(), cmd, "out", 0, false)
                assert.False(t, ok, "command %q must not reach the advisor", cmd)
        }
        assert.Equal(t, 0, asker.calls)
        for _, cmd := range []string{"make check", "npm run test:unit", "./scripts/verify.sh", "cargo build"} {
                asker.resp = jev.Response{Answers: map[string]jev.Answer{
                        "outcome": scoreAnswer(4, 0.9),
                }}
                _, _, ok := a.judge(t.Context(), cmd, "ok", 0, false)
                assert.True(t, ok, "command %q is check-like and must reach the advisor", cmd)
        }
        assert.Equal(t, 4, asker.calls)
}

// TestJevEvidenceAdvisorErrorRecordsNothing: any asker failure is a silent
// fallback to the deterministic-only behavior.
func TestJevEvidenceAdvisorErrorRecordsNothing(t *testing.T) {
        a := &jevEvidenceAdvisor{
                asker:   &fakeJevAsker{err: errors.New("typesafe API error (503)")},
                timeout: jevEvidenceTimeout,
        }
        _, _, ok := a.judge(t.Context(), "make check", "out", 0, false)
        assert.False(t, ok)
}

// TestJevEvidenceAdvisorTimeoutBounds: a wedged endpoint cannot stall the
// tool round — the per-call deadline cancels it.
func TestJevEvidenceAdvisorTimeoutBounds(t *testing.T) {
        blocking := &blockingJevAsker{}
        a := &jevEvidenceAdvisor{asker: blocking, timeout: 50 * time.Millisecond}
        _, _, ok := a.judge(t.Context(), "make check", "out", 0, false)
        assert.False(t, ok, "a timed-out verdict records nothing")
}

type blockingJevAsker struct{}

func (b *blockingJevAsker) SystemOneWithOptions(
        ctx context.Context, _ any, _ jev.Questions, _ jev.Options,
) (jev.Response, error) {
        <-ctx.Done()
        return jev.Response{}, ctx.Err()
}

// TestJevFailDetailCarriesFirstLineAndProvenance: the fail detail follows
// the classifiers' first-error-line convention and names the JEV source.
func TestJevFailDetailCarriesFirstLineAndProvenance(t *testing.T) {
        asker := &fakeJevAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                "outcome": scoreAnswer(1, 0.91),
        }}}
        a := &jevEvidenceAdvisor{asker: asker, timeout: jevEvidenceTimeout}
        status, detail, ok := a.judge(t.Context(), "make check", "\n\nerror: boom\nstack trace", 1, true)
        require.True(t, ok)
        assert.Equal(t, evidence.Fail, status)
        assert.Equal(t, "error: boom [jev verdict, confidence 0.91]", detail)
}

// TestRecordEvidenceJevFallback: an unclassified check-like command records
// jev evidence through the executor hook, ratchets, and notes on regression;
// with no advisor the same command records nothing.
func TestRecordEvidenceJevFallback(t *testing.T) {
        e, ledger := newTestExecutorWithLedger(t)
        a := &jevEvidenceAdvisor{
                asker: &fakeJevAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                        "outcome": scoreAnswer(4, 0.9),
                }}},
                timeout: jevEvidenceTimeout,
        }
        e.SetJevAdvisor(a)

        note := e.recordEvidence(t.Context(), json.RawMessage(`{"command":"make check"}`), "all tests passed")
        assert.Empty(t, note, "first jev pass is new evidence, not a regression")
        st := ledger.CheckStatus("jev:make-check")
        assert.Equal(t, evidence.CheckPassing, st.State)

        // Regression: the advisor now says failed.
        a.asker = &fakeJevAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                "outcome": scoreAnswer(1, 0.9),
        }}}
        note = e.recordEvidence(t.Context(), json.RawMessage(`{"command":"make check"}`), "error: boom\n(exit error: exit status 1)")
        require.NotEmpty(t, note, "a protected jev check regressing must note")
        assert.Contains(t, note, "jev:make-check")
        st = ledger.CheckStatus("jev:make-check")
        assert.Equal(t, evidence.CheckFailing, st.State)
}

// TestRecordEvidenceJevSkippedWhenDeterministicPathSpoke: the advisor is
// consulted only when the deterministic classifiers recorded nothing.
func TestRecordEvidenceJevSkippedWhenDeterministicPathSpoke(t *testing.T) {
        e, ledger := newTestExecutorWithLedger(t)
        asker := &fakeJevAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                "outcome": scoreAnswer(4, 0.9),
        }}}
        e.SetJevAdvisor(&jevEvidenceAdvisor{asker: asker, timeout: jevEvidenceTimeout})

        // go test is deterministically classified; the advisor must not fire.
        note := e.recordEvidence(t.Context(), json.RawMessage(`{"command":"go test ./internal/auth/"}`), evidenceGoTestPass)
        assert.Empty(t, note)
        st := ledger.CheckStatus("go-test:github.com/damien1141/a1/internal/auth")
        assert.Equal(t, evidence.CheckPassing, st.State)
        assert.Equal(t, 0, asker.calls, "the advisor must not be consulted when deterministic evidence exists")
        assert.Equal(t, 1, ledger.ProtectedCount(), "only the deterministic record exists")
}

// TestRecordEvidenceNoAdvisorRecordsNothing: the fallback default — no
// advisor, no key — leaves unclassified commands invisible, exactly as
// before the JEV wiring.
func TestRecordEvidenceNoAdvisorRecordsNothing(t *testing.T) {
        e, ledger := newTestExecutorWithLedger(t)
        note := e.recordEvidence(t.Context(), json.RawMessage(`{"command":"make check"}`), "all good")
        assert.Empty(t, note)
        assert.Equal(t, 0, ledger.ProtectedCount())
        assert.Equal(t, evidence.CheckNoEvidence, ledger.CheckStatus("jev:make-check").State)
}

// TestNewJevEvidenceAdvisorNilWithoutKey: no TYPESAFE_API_KEY means no
// advisor — the entire JEV surface is opt-in.
func TestNewJevEvidenceAdvisorNilWithoutKey(t *testing.T) {
        t.Setenv("TYPESAFE_API_KEY", "")
        assert.Nil(t, newJevEvidenceAdvisor())
}
