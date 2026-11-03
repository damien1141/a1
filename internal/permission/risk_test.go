package permission

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRiskClassifier scripts the decision model for the gate-side tests.
type fakeRiskClassifier struct {
	verdict BashRiskVerdict
	err     error
	calls   int
	lastCmd string
}

func (f *fakeRiskClassifier) AskRisk(_ context.Context, command string) (BashRiskVerdict, error) {
	f.calls++
	f.lastCmd = command
	if f.err != nil {
		return BashRiskVerdict{}, f.err
	}
	return f.verdict, nil
}

func destructiveVerdict() BashRiskVerdict {
	return BashRiskVerdict{
		Destructive:  true,
		Irreversible: true,
		Confidence:   0.92,
		Reason:       "force-push rewrites shared history",
	}
}

// riskTestGate builds an interactive gate (sandbox probe pinned off so the
// containment branch does not answer for the ambiguity zone) with the fake
// classifier installed.
func riskTestGate(t *testing.T, mode Mode, bashDefault Decision, clf *fakeRiskClassifier) *StaticGate {
	t.Helper()
	withSandboxSupport(t, false)
	policy := DefaultPolicy()
	policy.Mode = mode
	policy.BashDefault = bashDefault
	g, err := NewGate(policy, "")
	require.NoError(t, err)
	// Assign only for a non-nil clf: a typed nil pointer stored in the
	// interface field is not interface-nil, and the guard would call it.
	if clf != nil {
		g.riskClassifier = clf
	}
	return g
}

func bashRequest(cmd string) Request {
	return Request{Action: ActionBash, Command: cmd}
}

// The fixture command must match neither regex list: it is the ambiguity
// zone the classifier guards.
const ambiguousCmd = "git push --force origin main"

func TestCheckBashRisk_InteractiveAsksWithAnnotation(t *testing.T) {
	clf := &fakeRiskClassifier{verdict: destructiveVerdict()}
	g := riskTestGate(t, ModeInteractive, Ask, clf)

	dec, reason := g.checkBash(t.Context(), bashRequest(ambiguousCmd))

	assert.Equal(t, Ask, dec, "interactive keeps the human in the loop")
	assert.Contains(t, reason, "risk model")
	assert.Contains(t, reason, "confidence 0.92")
	assert.Contains(t, reason, "force-push rewrites shared history")
	assert.Contains(t, reason, "requires approval")
	assert.Equal(t, 1, clf.calls)
	assert.Equal(t, ambiguousCmd, clf.lastCmd)
}

func TestCheckBashRisk_InteractiveAskSurvivesFullCheck(t *testing.T) {
	clf := &fakeRiskClassifier{verdict: destructiveVerdict()}
	g := riskTestGate(t, ModeInteractive, Ask, clf)

	dec, _ := g.Check(t.Context(), bashRequest(ambiguousCmd))

	// The decision survives the full path. The reason may be recomposed by
	// the trajectory layer when narrowing gaps apply — the same thing it
	// does to every other Ask reason from checkBash, so the annotation is
	// best-effort, never load-bearing.
	assert.Equal(t, Ask, dec)
}

func TestCheckBashRisk_AutonomousModesDeny(t *testing.T) {
	for _, mode := range []Mode{ModeAutopilot, ModeHeadlessStrict, ModeReadonly} {
		t.Run(string(mode), func(t *testing.T) {
			clf := &fakeRiskClassifier{verdict: destructiveVerdict()}
			g := riskTestGate(t, mode, Allow, clf)

			dec, reason := g.checkBash(t.Context(), bashRequest(ambiguousCmd))

			// The sub-agent hole: BashDefault=Allow would have run this
			// silently; the classifier's verdict tightens it to Deny.
			assert.Equal(t, Deny, dec)
			assert.Contains(t, reason, "risk model")
			assert.Contains(t, reason, "confidence 0.92")
		})
	}
}

func TestCheckBashRisk_YoloSkipsClassifier(t *testing.T) {
	clf := &fakeRiskClassifier{verdict: destructiveVerdict()}
	g := riskTestGate(t, ModeYolo, Allow, clf)

	dec, _ := g.checkBash(t.Context(), bashRequest(ambiguousCmd))

	assert.Equal(t, Allow, dec, "yolo opted out; the regex deny list still fires first")
	assert.Zero(t, clf.calls, "the classifier must not even be consulted in yolo")
}

func TestCheckBashRisk_ClassifierMissesFallThrough(t *testing.T) {
	cases := []struct {
		name    string
		clf     *fakeRiskClassifier
		wantDec Decision
	}{
		{"error", &fakeRiskClassifier{err: errors.New("provider down")}, Ask},
		{"low confidence", &fakeRiskClassifier{verdict: BashRiskVerdict{Destructive: true, Irreversible: true, Confidence: 0.60}}, Ask},
		{"not destructive", &fakeRiskClassifier{verdict: BashRiskVerdict{Irreversible: true, Confidence: 0.95}}, Ask},
		{"reversible", &fakeRiskClassifier{verdict: BashRiskVerdict{Destructive: true, Irreversible: false, Confidence: 0.95}}, Ask},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := riskTestGate(t, ModeInteractive, Ask, tc.clf)

			dec, reason := g.checkBash(t.Context(), bashRequest(ambiguousCmd))

			// Every miss is exactly today's behavior: the policy default.
			assert.Equal(t, tc.wantDec, dec)
			assert.NotContains(t, reason, "risk model")
			assert.Equal(t, 1, tc.clf.calls, "the classifier was consulted once")
		})
	}
}

func TestCheckBashRisk_OnlyAmbiguityZonePaysTheCall(t *testing.T) {
	clf := &fakeRiskClassifier{verdict: destructiveVerdict()}
	g := riskTestGate(t, ModeInteractive, Ask, clf)

	cases := []struct {
		name string
		cmd  string
		dec  Decision
	}{
		{"allowlisted", "ls -la", Allow},
		{"denylisted", "sudo rm -rf /tmp/x", Deny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, _ := g.checkBash(t.Context(), bashRequest(tc.cmd))
			assert.Equal(t, tc.dec, dec)
		})
	}
	assert.Zero(t, clf.calls, "listed commands never reach the classifier")
}

func TestCheckBashRisk_SandboxContainmentAnswersFirst(t *testing.T) {
	clf := &fakeRiskClassifier{verdict: destructiveVerdict()}
	withSandboxSupport(t, true)
	policy := DefaultPolicy()
	policy.Mode = ModeInteractive
	g, err := NewGate(policy, "")
	require.NoError(t, err)
	g.riskClassifier = clf

	dec, reason := g.checkBash(t.Context(), bashRequest(ambiguousCmd))

	assert.Equal(t, Allow, dec, "containment substitutes for approval")
	assert.Contains(t, reason, "sandboxed")
	assert.Zero(t, clf.calls, "contained commands never reach the classifier")
}

func TestCheckBashRisk_NoClassifierUnchanged(t *testing.T) {
	g := riskTestGate(t, ModeInteractive, Ask, nil)

	dec, reason := g.checkBash(t.Context(), bashRequest(ambiguousCmd))

	assert.Equal(t, Ask, dec)
	assert.NotContains(t, reason, "risk model")
}

func TestForkChildGate_InheritsRiskClassifier(t *testing.T) {
	clf := &fakeRiskClassifier{verdict: destructiveVerdict()}
	parent := riskTestGate(t, ModeInteractive, Ask, clf)
	child := ForkChildGate(parent, ModeAutopilot)

	dec, reason := child.checkBash(t.Context(), bashRequest(ambiguousCmd))

	// ChildPolicy defaults bash to Allow; the inherited classifier is what
	// stops a novel destructive command in a spawned branch.
	assert.Equal(t, Deny, dec)
	assert.Contains(t, reason, "risk model")
	assert.Equal(t, 1, clf.calls)
}

func TestCheckBashRisk_EmptyModeTreatedAsInteractive(t *testing.T) {
	clf := &fakeRiskClassifier{verdict: destructiveVerdict()}
	g := riskTestGate(t, "", Ask, clf)

	dec, reason := g.checkBash(t.Context(), bashRequest(ambiguousCmd))

	assert.Equal(t, Ask, dec, "an unset mode folds as interactive")
	assert.Contains(t, reason, "requires approval")
}
