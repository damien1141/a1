package permission

import (
        "context"
        "errors"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm/jev"
)

// fakeRiskAsker scripts the decision-model backend for the classifier's
// own verdict-mapping tests.
type fakeRiskAsker struct {
        state     any
        questions jev.Questions
        resp      jev.Response
        err       error
        calls     int
}

func (f *fakeRiskAsker) SystemOneWithOptions(
        _ context.Context,
        state any,
        questions jev.Questions,
        _ jev.Options,
) (jev.Response, error) {
        f.calls++
        f.state, f.questions = state, questions
        if f.err != nil {
                return jev.Response{}, f.err
        }
        return f.resp, nil
}

func testRiskClassifier(asker jev.Asker) *JevRiskClassifier {
        return &JevRiskClassifier{asker: asker, timeout: time.Second}
}

func TestJevRiskClassifier_VerdictMapping(t *testing.T) {
        cases := []struct {
                name string
                resp jev.Response
                want BashRiskVerdict
        }{
                {
                        name: "destructive and irreversible",
                        resp: jev.Response{Answers: map[string]jev.Answer{
                                "destructive": {Type: jev.AnswerNoul, Noul: 0.95, Confidence: 0.9},
                                "reversible":  {Type: jev.AnswerNoul, Noul: 0.10, Confidence: 0.85},
                        }},
                        want: BashRiskVerdict{Destructive: true, Irreversible: true, Confidence: 0.85},
                },
                {
                        name: "destructive but reversible",
                        resp: jev.Response{Answers: map[string]jev.Answer{
                                "destructive": {Type: jev.AnswerNoul, Noul: 0.95, Confidence: 0.9},
                                "reversible":  {Type: jev.AnswerNoul, Noul: 0.90, Confidence: 0.8},
                        }},
                        want: BashRiskVerdict{Destructive: true, Irreversible: false, Confidence: 0.8},
                },
                {
                        name: "benign command",
                        resp: jev.Response{Answers: map[string]jev.Answer{
                                "destructive": {Type: jev.AnswerNoul, Noul: 0.05, Confidence: 0.95},
                                "reversible":  {Type: jev.AnswerNoul, Noul: 0.90, Confidence: 0.9},
                        }},
                        want: BashRiskVerdict{Destructive: false, Irreversible: false, Confidence: 0.9},
                },
                {
                        // The native System One wire (Ollama >= 0.35) answers
                        // a noul with a bare probability and no confidence
                        // beside it; the blocking judgment's confidence is
                        // then the probability each answer put on the side it
                        // asserted — without this, the guard could never fire
                        // on a native server.
                        name: "destructive and irreversible, native wire (no reported confidence)",
                        resp: jev.Response{Answers: map[string]jev.Answer{
                                "destructive": {Type: jev.AnswerNoul, Noul: 0.95},
                                "reversible":  {Type: jev.AnswerNoul, Noul: 0.10},
                        }},
                        want: BashRiskVerdict{Destructive: true, Irreversible: true, Confidence: 0.90},
                },
        }
        for _, tc := range cases {
                t.Run(tc.name, func(t *testing.T) {
                        clf := testRiskClassifier(&fakeRiskAsker{resp: tc.resp})

                        verdict, err := clf.AskRisk(t.Context(), "some command")
                        require.NoError(t, err)
                        assert.Equal(t, tc.want, verdict)
                })
        }
}

func TestJevRiskClassifier_ErrorPropagates(t *testing.T) {
        clf := testRiskClassifier(&fakeRiskAsker{err: errors.New("provider exploded")})

        verdict, err := clf.AskRisk(t.Context(), "some command")
        require.Error(t, err)
        assert.Empty(t, verdict)
}

func TestJevRiskClassifier_QuestionShape(t *testing.T) {
        asker := &fakeRiskAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                "destructive": {Type: jev.AnswerNoul, Noul: 0.1},
                "reversible":  {Type: jev.AnswerNoul, Noul: 0.9},
        }}}
        clf := testRiskClassifier(asker)

        _, err := clf.AskRisk(t.Context(), "git push --force origin main")
        require.NoError(t, err)

        // The user's sketch as two separate noul questions: destructive intent
        // and reversibility, one round trip.
        require.Len(t, asker.questions, 2)
        for _, name := range []string{"destructive", "reversible"} {
                q, ok := asker.questions[name].(jev.Noul)
                require.True(t, ok, "%s should be a noul", name)
                assert.NotNil(t, q.Criteria)
                assert.NotEmpty(t, q.Instructions)
        }
        // The command travels as state, not baked into the questions.
        state, ok := asker.state.(map[string]any)
        require.True(t, ok)
        assert.Equal(t, "git push --force origin main", state["command"])
}

func TestJevRiskClassifier_MissingAnswerIsNoOpinion(t *testing.T) {
        // A provider that answers only one of the two questions must not
        // produce a half-verdict: the empty verdict blocks nothing.
        clf := testRiskClassifier(&fakeRiskAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                "destructive": {Type: jev.AnswerNoul, Noul: 0.99, Confidence: 0.99},
        }}})

        verdict, err := clf.AskRisk(t.Context(), "some command")
        require.NoError(t, err)
        assert.False(t, verdict.Destructive)
        assert.False(t, verdict.Irreversible)
}

// --- wiring ---

// riskWireGate builds a gate from a policy the way production does, with
// the sandbox probe pinned off so the ambiguity zone is reachable.
func riskWireGate(t *testing.T, policy Policy) *StaticGate {
        t.Helper()
        withSandboxSupport(t, false)
        g, err := NewGate(policy, "")
        require.NoError(t, err)
        return g
}

func TestWireRiskClassifier_FlagWithoutProviderStaysOff(t *testing.T) {
        // Pin both resolution paths off: zero settings and no env key.
        t.Setenv("TYPESAFE_API_KEY", "")
        jev.Configure(jev.Settings{})
        t.Cleanup(func() { jev.Configure(jev.Settings{}) })

        policy := DefaultPolicy()
        policy.BashRiskGuard = true
        g := riskWireGate(t, policy)

        // Fail-open: the flag alone must not wire a classifier, and the gate
        // answers exactly as it would without the flag.
        assert.Nil(t, g.riskClassifier)
        dec, reason := g.checkBash(t.Context(), bashRequest(ambiguousCmd))
        assert.Equal(t, policy.BashDefault, dec, "the ambiguity zone falls through to the policy default")
        assert.NotContains(t, reason, "risk model")
}

func TestWireRiskClassifier_FlagWithProviderWires(t *testing.T) {
        t.Setenv("TYPESAFE_API_KEY", "")
        jev.Configure(jev.Settings{Provider: jev.ProviderAPI, APIKey: "sk-test"})
        t.Cleanup(func() { jev.Configure(jev.Settings{}) })

        policy := DefaultPolicy()
        policy.BashRiskGuard = true
        g := riskWireGate(t, policy)

        require.NotNil(t, g.riskClassifier, "flag + provider wires the guard")
        _, isJev := g.riskClassifier.(*JevRiskClassifier)
        assert.True(t, isJev)
}

func TestWireRiskClassifier_FlagUnsetNeverWires(t *testing.T) {
        t.Setenv("TYPESAFE_API_KEY", "")
        jev.Configure(jev.Settings{Provider: jev.ProviderAPI, APIKey: "sk-test"})
        t.Cleanup(func() { jev.Configure(jev.Settings{}) })

        g := riskWireGate(t, DefaultPolicy())

        assert.Nil(t, g.riskClassifier, "no flag, no classifier — even with a provider")
}

func TestJevRiskClassifier_MidSessionDeconfigurationIsAMiss(t *testing.T) {
        // The guard is built while a provider resolves, then the operator
        // removes it (jev: section deleted from the web GUI). The asker
        // re-resolves per call, so the next classification must surface
        // ErrNotConfigured — a miss the gate folds into the policy default,
        // never a stale verdict from the dead provider.
        t.Setenv("TYPESAFE_API_KEY", "")
        jev.Configure(jev.Settings{Provider: jev.ProviderAPI, APIKey: "sk-test"})
        clf := NewJevRiskClassifier()
        require.True(t, jev.Configured(), "sanity: the provider resolves at build time")
        t.Cleanup(func() { jev.Configure(jev.Settings{}) })

        jev.Configure(jev.Settings{})

        _, err := clf.AskRisk(t.Context(), ambiguousCmd)
        require.ErrorIs(t, err, jev.ErrNotConfigured)
}
