package permission

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPartialLabelMeet(t *testing.T) {
	a := NewPartialLabel(Label{Trust: User, ReaderSet: []string{"user"}}, []string{"src1"})
	b := NewPartialLabel(Label{Trust: Untrusted, ReaderSet: []string{"user", "ext"}}, []string{"src2"})

	m := a.Meet(b)
	assert.Equal(t, Untrusted, m.Established.Trust, "meet should take the more restrictive trust")
	assert.Equal(t, []string{"user"}, m.Established.ReaderSet, "meet should intersect reader sets")
	assert.Equal(t, []string{"src1", "src2"}, m.Unresolved, "meet should union unresolved sources")
}

func TestPartialLabelMerge(t *testing.T) {
	a := NewPartialLabel(Label{Trust: User, ReaderSet: []string{"user"}}, []string{"src1"})
	b := NewPartialLabel(Label{Trust: Trusted, ReaderSet: []string{"user", "admin"}}, []string{"src2"})

	m := a.Merge(b)
	assert.Equal(t, Trusted, m.Established.Trust, "merge should take the less restrictive trust")
	assert.ElementsMatch(t, []string{"user", "admin"}, m.Established.ReaderSet, "merge should union reader sets")
	assert.ElementsMatch(t, []string{"src1", "src2"}, m.Unresolved, "merge should union unresolved sources")
}

func TestPartialLabelResolve(t *testing.T) {
	p := NewPartialLabel(Label{Trust: User, ReaderSet: []string{"user"}}, []string{"web", "file"})
	resolved := p.Resolve("web", Label{Trust: Untrusted, ReaderSet: []string{}})
	assert.Equal(t, 1, resolved.UnresolvedCount(), "resolved source should be removed from unresolved set")
	assert.Equal(t, Untrusted, resolved.Established.Trust, "User ∧ Untrusted = Untrusted under meet")

	// Resolving a non-existent source is a no-op.
	noop := resolved.Resolve("missing", Label{Trust: User})
	assert.Equal(t, 1, noop.UnresolvedCount(), "missing source should not change unresolved count")
	assert.Equal(t, Untrusted, noop.Established.Trust, "established should be unchanged")
}

func TestTrustChainExcludesUnknown(t *testing.T) {
	// Unknown is not on the trust chain: it is a separate dimension of the
	// partial label, so trusting it would invent an ordering the paper
	// explicitly rejects ("there is no ordering trusted < unknown < suspicious").
	assert.Equal(t, -1, TrustOrder(Trust(99)), "unknown trust rank is off-chain")
	assert.False(t, IsKnownTrust(Trust(99)), "off-chain trust is not a known chain value")
	for _, known := range []Trust{Trusted, User, Untrusted} {
		assert.True(t, IsKnownTrust(known), "%s is on the chain", known)
	}
	// The chain itself is totally ordered by restrictiveness.
	assert.Less(t, TrustOrder(Trusted), TrustOrder(User))
	assert.Less(t, TrustOrder(User), TrustOrder(Untrusted))
}

func TestUnannotatedContributionFoldsNoTrust(t *testing.T) {
	// An unannotated tool declares no established trust, so folding it into a
	// label must not fabricate a rank.
	unannotated := Label{}
	assert.False(t, IsKnownTrust(unannotated.Trust), "zero Trust is off-chain")
	assert.Empty(t, unannotated.ReaderSet, "unannotated label carries no readers")
}

func TestWideningRequiresAcceptance(t *testing.T) {
	current := Label{Trust: Trusted, ReaderSet: []string{"user", "admin"}}
	widening := Label{Trust: User, ReaderSet: []string{"user"}}
	identical := Label{Trust: Trusted, ReaderSet: []string{"user", "admin"}}

	assert.True(t, RequiresNarrowing(current, widening), "widening trust level should require acceptance")
	assert.False(t, RequiresNarrowing(current, identical), "identical label should not require acceptance")
}

func TestStaticGateTrajectoryNarrowsAndAccepts(t *testing.T) {
	p := DefaultPolicy()
	p.BashDefault = Ask
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	// Top label: trusted. bash contract is Untrusted, so this narrows.
	approved := false
	g.SetAcceptNarrowing(func(ctx context.Context, req Request, reason string) (bool, error) {
		approved = true
		return true, nil
	})

	dec, reason := g.Check(
		context.Background(),
		Request{Action: ActionBash, Tool: "bash", Command: "curl https://example.com"},
	)
	assert.Equal(t, Ask, dec, "narrowing call should require approval")
	assert.Contains(t, reason, "bash requires approval", "mode-folded reason should be preserved after acceptance")
	assert.True(t, approved, "acceptance callback should be invoked")

	// After acceptance, a second narrowing call still needs approval because
	// the acceptance is per-call, not global.
	dec, _ = g.Check(context.Background(), Request{Action: ActionBash, Tool: "bash", Command: "curl https://other.com"})
	assert.Equal(t, Ask, dec, "subsequent narrowing calls still need approval")
}

func TestContractPreconditionsBlockDispatch(t *testing.T) {
	p := DefaultPolicy()
	p.BashDefault = Ask
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	// Register a contract requiring prior effect token "setup_done".
	g.contracts.Register("bash", Contract{
		Contribution: Label{Trust: Untrusted, ReaderSet: []string{}},
		EffectTokens: []string{"bash_run"},
		Requires: []Precondition{
			{Kind: PreconditionPrior, Token: "setup_done"},
		},
	})

	// Missing prior should fail.
	dec, reason := g.Check(
		context.Background(),
		Request{Action: ActionBash, Tool: "bash", Command: "curl https://example.com"},
	)
	assert.Equal(t, Ask, dec, "missing prior should gate dispatch")
	assert.Contains(t, reason, "recovery:")

	// Commit the prior effect.
	g.trajectory.Effects.Commit("setup", "setup_done")

	dec, reason = g.Check(
		context.Background(),
		Request{Action: ActionBash, Tool: "bash", Command: "curl https://example.com"},
	)
	assert.Equal(t, Ask, dec, "prior satisfied; narrowing should require acceptance because bash contributes Untrusted")
	assert.Contains(t, reason, "recovery:")
}

func TestAdmitFoldsContributionAndCommitsEffects(t *testing.T) {
	p := DefaultPolicy()
	p.RequiredEffects = []string{"bash_run"}
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	req := Request{
		Tool:        "bash",
		OutputLabel: Label{Trust: Untrusted, ReaderSet: []string{}},
		Succeeded:   true,
	}

	dec, reason := g.Admit(context.Background(), req)
	assert.Equal(t, Ask, dec, "admission should require effect-log verification when policy demands it")
	assert.Contains(t, reason, "effect-log verification")

	// Commit the required effect and retry admission.
	g.trajectory.Effects.Commit("bash", "bash_run")
	dec, _ = g.Admit(context.Background(), req)
	assert.Equal(t, Allow, dec, "admission should pass once required effects are present")

	traj := g.Trajectory()
	assert.Equal(t, Untrusted, traj.Label.Established.Trust, "admission should fold tool contribution into label")
	assert.Equal(t, 1, traj.Effects.Count("bash_run"), "admission should commit effect tokens")
}

func TestAdmitDoesNotCommitEffectsWithoutSuccess(t *testing.T) {
	p := DefaultPolicy()
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	req := Request{
		Tool:        "write",
		OutputLabel: Label{Trust: User, ReaderSet: []string{"user"}},
		Succeeded:   false, // failed or indeterminate outcome
	}

	dec, _ := g.Admit(context.Background(), req)
	require.Equal(t, Allow, dec, "a failed write still reports its realized output")
	assert.Zero(t, g.Trajectory().Effects.Count("write"),
		"APPA §3: failed or indeterminate outcomes contribute no effect tokens")
}

func TestAdmitUnannotatedOutputRecordsUnresolvedSource(t *testing.T) {
	p := DefaultPolicy()
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	// A tool with no declared output label: the realized output must enter the
	// partial label as an unresolved source, not as the declared contribution.
	before := g.Trajectory().Label
	req := Request{
		Tool:        "mystery_tool",
		OutputLabel: Label{Trust: Untrusted, ReaderSet: []string{}},
		Succeeded:   true,
	}
	dec, _ := g.Admit(context.Background(), req)
	require.Equal(t, Allow, dec)

	after := g.Trajectory().Label
	assert.Equal(t, before.Established, after.Established,
		"an unannotated tool must not lower the established label")
	assert.Equal(t, before.UnresolvedCount()+1, after.UnresolvedCount(),
		"the realized output becomes an unresolved source")
	assert.True(t, after.HasUnresolved(), "unresolved source is recorded")
}

func TestCheckBlocksOnProspectiveGapEvenWhenHardGateAllows(t *testing.T) {
	p := DefaultPolicy()
	p.BashDefault = Allow // the conventional check would Allow this call
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	// A contract whose release-side requirement is unsatisfied prospectively:
	// cτ(L) fails the trust floor even though L itself passes it.
	g.contracts.Register("bash", Contract{
		Tool:         "bash",
		Contribution: Label{Trust: Untrusted, ReaderSet: []string{}},
		Requires:     []Precondition{{Kind: PreconditionTrustFloor, Trust: Trusted}},
	})

	dec, reason := g.Check(
		context.Background(),
		Request{Action: ActionBash, Tool: "bash", Command: "curl https://example.com"},
	)
	assert.Equal(t, Ask, dec, "prospective evaluation must block regardless of the hard-gate decision")
	assert.Contains(t, reason, "trust floor", "the gap is reported")
	assert.Contains(t, reason, "recovery:", "a recovery path is offered")
}

func TestModeSwitchPreservesTrajectory(t *testing.T) {
	p := DefaultPolicy()
	p.Mode = ModeInteractive
	p.BashDefault = Ask
	inner, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	// Build a trajectory with some state.
	inner.Trajectory().Effects.Commit("bash", "setup_done")
	inner.Trajectory().Label = NewPartialLabel(Label{Trust: Untrusted, ReaderSet: []string{}}, []string{"src1"})

	var allow atomic.Bool
	wrapped := &SessionAllowGate{Inner: inner, Enabled: &allow}

	// Simulate mode switch: preserve trajectory pointer, then transplant into new gate.
	prev := inner.Trajectory()
	newInner, err := NewGate(policyInMode(ModeReadonly), t.TempDir())
	require.NoError(t, err)
	newInner.SetTrajectory(prev)
	wrapped.Inner = newInner

	// The new gate should see the transplanted state.
	if sg, ok := wrapped.Inner.(*StaticGate); ok {
		assert.True(t, sg.Trajectory().Effects.Has("setup_done"), "trajectory effects should survive mode switch")
		assert.Equal(t, 1, len(sg.Trajectory().Label.Unresolved), "unresolved sources should survive mode switch")
	}
}
