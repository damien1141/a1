package permission

import (
        "context"
        "time"

        "github.com/damien1141/a1/internal/debuglog"
        "github.com/damien1141/a1/internal/llm/jev"
)

// riskCallTimeout bounds one risk-classification call against the hosted
// API. The ambiguity zone sits on the tool-dispatch path, so the ceiling
// is tight; jev.EffectiveTimeout raises it for slower providers (a local
// Ollama model) and jev.timeout_sec overrides both.
const riskCallTimeout = 3 * time.Second

// JevRiskClassifier asks the configured decision model whether one bash
// command is destructive and hard to reverse: one round trip, two noul
// questions. Callers gate on jev.Configured() before wiring it (NewGate
// does); the asker re-resolves the provider per call, so a config edit
// applies to the next command without a gate rebuild.
type JevRiskClassifier struct {
        asker   jev.Asker
        timeout time.Duration
}

// NewJevRiskClassifier returns the decision-model risk classifier over
// the configured provider.
func NewJevRiskClassifier() *JevRiskClassifier {
        return &JevRiskClassifier{
                asker:   jev.NewConfiguredAsker(),
                timeout: jev.EffectiveTimeout(riskCallTimeout),
        }
}

// wireRiskClassifier attaches the decision-model risk classifier when the
// policy asks for it and a provider resolves right now. Beside the jev
// implementation on purpose, so gate.go stays free of the provider
// import; gates built while jev is unconfigured stay classifier-free even
// with the flag set (a later mode switch rebuilds the gate and rechecks).
func wireRiskClassifier(g *StaticGate, policy Policy) {
        if !policy.BashRiskGuard {
                return
        }
        if !jev.Configured() {
                // The operator asked for the guard and no provider resolves. The
                // flag is fail-open by design, but the silence deserves a name —
                // same discipline as the resolver's own bad-provider note, so a
                // misconfigured guard is findable in the debug log instead of
                // looking like a classifier that never misses.
                debuglog.Logf("permission: bash_risk_guard is set but no decision model is configured (jev: section or TYPESAFE_API_KEY) — the guard is inactive")
                return
        }
        g.riskClassifier = NewJevRiskClassifier()
}

// AskRisk implements BashRiskClassifier. Errors propagate; the gate's
// contract is to fall through to the policy default on any miss.
func (c *JevRiskClassifier) AskRisk(ctx context.Context, command string) (BashRiskVerdict, error) {
        qCtx, cancel := context.WithTimeout(ctx, c.timeout)
        defer cancel()

        state := map[string]any{
                "purpose": "pre-execution permission check for an autonomous coding agent's bash command",
                "command": command,
        }
        resp, err := c.asker.SystemOneWithOptions(qCtx, state, riskQuestions(), jev.Options{})
        if err != nil {
                return BashRiskVerdict{}, err
        }
        destructive, okD := resp.Answers["destructive"]
        reversible, okR := resp.Answers["reversible"]
        if !okD || !okR {
                // The jev layer already rejects a missing answer per question, so
                // this is defense in depth for providers with odd response shapes.
                return BashRiskVerdict{}, nil
        }
        return BashRiskVerdict{
                Destructive:  destructive.Type == jev.AnswerNoul && destructive.Noul >= 0.5,
                Irreversible: reversible.Type == jev.AnswerNoul && reversible.Noul < 0.5,
                // The blocking judgment leans on both answers; it is only as
                // confident as the weaker of the two. The native wire sends a
                // noul as a bare probability with no confidence beside it, so
                // Certainty reads the probability's own strength there.
                Confidence: min(destructive.Certainty(), reversible.Certainty()),
        }, nil
}

// riskQuestions builds the two-question set: the user's exact sketch —
// destructive intent and reversibility — as separate noul questions, so
// a classifier without reasoning never has to weigh both dimensions in
// one answer.
func riskQuestions() jev.Questions {
        return jev.Questions{
                "destructive": jev.Noul{
                        Instructions: "does this command destroy, overwrite, or irrecoverably mutate state: delete files or directories, force-push rewritten history, drop data, overwrite configuration, reformat disks, kill processes outside the workspace?",
                        Criteria: &jev.NoulCriteria{
                                True:  "the command deletes, overwrites, force-pushes, drops, or otherwise destroys existing state",
                                False: "the command only reads, builds, tests, or writes new state that does not replace anything",
                        },
                },
                "reversible": jev.Noul{
                        Instructions: "if this command runs, could its effect be undone cheaply: restored from version control, recreated from source, or rolled back without manual data recovery?",
                        Criteria: &jev.NoulCriteria{
                                True:  "the effect is cheaply undoable: git reflog, re-create from source, undo a migration, re-run a build",
                                False: "the effect is not cheaply undoable: deleted untracked files, force-pushed public history, dropped data, overwritten configs with no backup",
                        },
                },
        }
}
