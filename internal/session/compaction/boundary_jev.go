package compaction

import (
        "context"
        "fmt"
        "time"

        "github.com/damien1141/a1/internal/llm/jev"
)

// boundaryCallTimeout bounds one boundary-classification call against the
// hosted API. jev.EffectiveTimeout raises it for slower providers (a local
// Ollama model) and jev.timeout_sec overrides both.
const boundaryCallTimeout = 4 * time.Second

// boundaryConfidenceFloor is the minimum confidence a "clean" verdict needs
// before the cut moves. The classifier is a refinement, not an authority:
// an unsure verdict leaves the positional cut alone.
const boundaryConfidenceFloor = 0.55

// JevBoundaryClassifier asks the configured decision model which candidate
// boundaries are clean fold points. One round trip; one self-contained
// noul question per candidate, so a classifier without reasoning never has
// to join a question name back to a list entry.
type JevBoundaryClassifier struct {
        asker   jev.Asker
        timeout time.Duration
}

// NewJevBoundaryClassifier returns the decision-model classifier over the
// configured provider. Callers gate on jev.Configured() first; the asker
// itself re-resolves the provider per call, so a config edit applies to
// the next compaction without a restart.
func NewJevBoundaryClassifier() *JevBoundaryClassifier {
        return &JevBoundaryClassifier{
                asker:   jev.NewConfiguredAsker(),
                timeout: jev.EffectiveTimeout(boundaryCallTimeout),
        }
}

// ClassifyBoundaries implements BoundaryClassifier. An error propagates to
// the caller, whose contract is to fall back to the positional cut.
func (c *JevBoundaryClassifier) ClassifyBoundaries(
        ctx context.Context,
        candidates []BoundaryCandidate,
) (map[int]bool, error) {
        if len(candidates) == 0 {
                return nil, nil
        }
        qCtx, cancel := context.WithTimeout(ctx, c.timeout)
        defer cancel()

        questions := make(jev.Questions, len(candidates))
        for i, cand := range candidates {
                questions[boundaryQuestionName(i)] = boundaryQuestion(cand)
        }
        state := map[string]any{
                "purpose": "choosing where to fold session history during compaction; a clean fold point keeps whole tasks on one side of the boundary",
        }
        resp, err := c.asker.SystemOneWithOptions(qCtx, state, questions, jev.Options{})
        if err != nil {
                return nil, err
        }
        clean := make(map[int]bool, len(candidates))
        for i, cand := range candidates {
                answer, ok := resp.Answers[boundaryQuestionName(i)]
                if !ok {
                        // An unanswered question is a non-verdict, not an error: the
                        // answer spine already rejects missing answers at the jev
                        // layer, so this is defense in depth for odd providers.
                        continue
                }
                if answer.Type == jev.AnswerNoul &&
                        answer.Noul >= 0.5 &&
                        // The native wire sends a noul as a bare probability
                        // with no confidence beside it; Certainty reads the
                        // probability's own strength there and the reported
                        // confidence wherever one was sent.
                        answer.Certainty() >= boundaryConfidenceFloor {
                        clean[cand.Index] = true
                }
        }
        return clean, nil
}

// boundaryQuestionName is the stable question name for candidate i.
func boundaryQuestionName(i int) string { return fmt.Sprintf("boundary_%d", i) }

// boundaryQuestion builds the self-contained noul question for one
// candidate: the excerpts travel with the question, and the roles are
// named because "follows is a fresh user request" is a strong cleanness
// signal the classifier should see.
func boundaryQuestion(cand BoundaryCandidate) jev.Question {
        return jev.Noul{
                Instructions: fmt.Sprintf(
                        "precedes (the %s message that would be the last thing summarized):\n%s\n\nfollows (the %s message that would be the first thing kept):\n%s\n\nIs this a clean fold point?",
                        cand.PrecedingRole, cand.Preceding, cand.FollowingRole, cand.Following,
                ),
                Criteria: &jev.NoulCriteria{
                        True:  "the work in precedes reaches a natural completion — a task finished, a question answered, a coherent step closed — and follows starts something new; folding here will not split a task across the summary boundary",
                        False: "the boundary falls mid-task: precedes ends mid-thought, mid tool-chain, or mid-exchange, and follows continues the same unit of work",
                },
        }
}
