package compaction

import (
        "context"
        "errors"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm/jev"
)

// fakeAsker scripts the decision-model backend for the classifier's own
// filtering tests.
type fakeAsker struct {
        state     any
        questions jev.Questions
        resp      jev.Response
        err       error
        calls     int
}

func (f *fakeAsker) SystemOneWithOptions(
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

func testClassifier(asker jev.Asker) *JevBoundaryClassifier {
        return &JevBoundaryClassifier{asker: asker, timeout: time.Second}
}

func jevCandidates() []BoundaryCandidate {
        return []BoundaryCandidate{
                {Index: 4, KeptTokens: 70, Preceding: "task one is done, tests pass", Following: "next: refactor the parser", PrecedingRole: "assistant", FollowingRole: "user"},
                {Index: 6, KeptTokens: 20, Preceding: "mid-chain, still calling tools", Following: "continuing the same fix", PrecedingRole: "assistant", FollowingRole: "assistant"},
                {Index: 2, KeptTokens: 90, Preceding: "another completed answer", Following: "a new request arrives", PrecedingRole: "assistant", FollowingRole: "user"},
        }
}

func TestJevBoundaryClassifier_ConfidenceFloorFiltersVerdicts(t *testing.T) {
        asker := &fakeAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                "boundary_0": {Type: jev.AnswerNoul, Noul: 0.9, Confidence: 0.8}, // clean, confident
                "boundary_1": {Type: jev.AnswerNoul, Noul: 0.9, Confidence: 0.3}, // clean, unsure → dropped
                "boundary_2": {Type: jev.AnswerNoul, Noul: 0.2, Confidence: 0.9}, // not clean → dropped
        }}}
        clf := testClassifier(asker)

        clean, err := clf.ClassifyBoundaries(t.Context(), jevCandidates())
        require.NoError(t, err)

        // Only the high-confidence yes survives; the question names map back to
        // candidate entry indices.
        assert.Equal(t, map[int]bool{4: true}, clean)
}

// TestJevBoundaryClassifier_NativeWireNoulWithoutConfidence: the native
// System One wire (Ollama >= 0.35) answers a noul with a bare probability
// and no confidence beside it. The floor must read the probability's own
// strength there — gating on the absent confidence field would leave the
// classifier unable to ever fire on a native server.
func TestJevBoundaryClassifier_NativeWireNoulWithoutConfidence(t *testing.T) {
        asker := &fakeAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                "boundary_0": {Type: jev.AnswerNoul, Noul: 0.9},  // clean, strongly → kept
                "boundary_1": {Type: jev.AnswerNoul, Noul: 0.51}, // clean, barely → dropped
                "boundary_2": {Type: jev.AnswerNoul, Noul: 0.2},  // not clean → dropped
        }}}
        clf := testClassifier(asker)

        clean, err := clf.ClassifyBoundaries(t.Context(), jevCandidates())
        require.NoError(t, err)

        assert.Equal(t, map[int]bool{4: true}, clean)
}

func TestJevBoundaryClassifier_ErrorPropagates(t *testing.T) {
        clf := testClassifier(&fakeAsker{err: errors.New("provider exploded")})

        clean, err := clf.ClassifyBoundaries(t.Context(), jevCandidates())
        require.Error(t, err)
        assert.Nil(t, clean)
}

func TestJevBoundaryClassifier_QuestionsAreSelfContained(t *testing.T) {
        asker := &fakeAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                "boundary_0": {Type: jev.AnswerNoul, Noul: 0.1},
        }}}
        clf := testClassifier(asker)
        candidates := jevCandidates()

        _, err := clf.ClassifyBoundaries(t.Context(), candidates)
        require.NoError(t, err)

        require.Len(t, asker.questions, len(candidates))
        for i, cand := range candidates {
                q, ok := asker.questions[boundaryQuestionName(i)].(jev.Noul)
                require.True(t, ok, "question %s should be a noul", boundaryQuestionName(i))
                // The excerpts travel with the question: a classifier without
                // reasoning never joins a name back to a list entry.
                instr, _ := q.Instructions.(string)
                assert.Contains(t, instr, cand.Preceding)
                assert.Contains(t, instr, cand.Following)
                assert.Contains(t, instr, string(cand.FollowingRole))
                require.NotNil(t, q.Criteria)
        }
        // The state stays tiny: sizes and indices are constraint bookkeeping,
        // not evidence for the model.
        state, ok := asker.state.(map[string]any)
        require.True(t, ok)
        assert.Contains(t, state, "purpose")
        assert.NotContains(t, state, "boundaries")
}

func TestJevBoundaryClassifier_NoCandidatesNoCall(t *testing.T) {
        asker := &fakeAsker{}
        clf := testClassifier(asker)

        clean, err := clf.ClassifyBoundaries(t.Context(), nil)
        require.NoError(t, err)
        assert.Empty(t, clean)
        assert.Zero(t, asker.calls)
}

func TestJevBoundaryClassifier_MidSessionDeconfigurationIsAMiss(t *testing.T) {
        // The classifier is built while a provider resolves, then the operator
        // removes it mid-session. The asker re-resolves per call, so the next
        // compaction must surface ErrNotConfigured — which refineCutPoint
        // folds into the positional cut, never a stale verdict or a break.
        t.Setenv("TYPESAFE_API_KEY", "")
        jev.Configure(jev.Settings{Provider: jev.ProviderAPI, APIKey: "sk-test"})
        clf := NewJevBoundaryClassifier()
        require.True(t, jev.Configured(), "sanity: the provider resolves at build time")
        t.Cleanup(func() { jev.Configure(jev.Settings{}) })

        jev.Configure(jev.Settings{})

        _, err := clf.ClassifyBoundaries(t.Context(), jevCandidates())
        require.ErrorIs(t, err, jev.ErrNotConfigured)
}
