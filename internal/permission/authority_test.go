package permission

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallHashDeterministic(t *testing.T) {
	req := Request{
		Action: ActionWrite,
		Tool:   "write",
		Paths:  []string{"/tmp/b", "/tmp/a"},
		InputLabels: []Label{
			{Trust: User, ReaderSet: []string{"user"}, EffectTokens: []string{"t1"}},
		},
	}
	h1 := CallHash(req)
	h2 := CallHash(req)
	assert.Equal(t, h1, h2, "call hash must be deterministic")
	assert.NotEmpty(t, h1)
}

func TestCallHashDistinguishesArgs(t *testing.T) {
	r1 := Request{Tool: "bash", Command: "ls"}
	r2 := Request{Tool: "bash", Command: "rm -rf /"}
	assert.NotEqual(t, CallHash(r1), CallHash(r2), "different commands must produce different hashes")
}

func TestAuthorityFuncAdaptsFunction(t *testing.T) {
	f := AuthorityFunc(func(ctx context.Context, callHash string, req Request) (Decision, string) {
		return Allow, "authorized:" + callHash
	})
	dec, reason := f.Authorize(nil, "abc123", Request{})
	assert.Equal(t, Allow, dec)
	assert.Contains(t, reason, "abc123")
}

func TestRulingLogAppendAndFilter(t *testing.T) {
	log := NewRulingLog()
	log.Append(Ruling{CallHash: "h1", GapsCovered: []string{"narrowing"}, Decision: Allow})
	log.Append(Ruling{CallHash: "h2", GapsCovered: []string{"trust_floor"}, Decision: Deny})

	assert.Equal(t, 2, log.Len())
	assert.True(t, log.CoveredFor("h1", "narrowing"))
	assert.False(t, log.CoveredFor("h1", "trust_floor"))
	assert.False(t, log.CoveredFor("h3", "narrowing"))
}

func TestConsultAuthorityLogsRuling(t *testing.T) {
	g, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)

	g.Policy.RequiresAuthority = true
	g.Policy.Authority = AuthorityFunc(func(ctx context.Context, callHash string, req Request) (Decision, string) {
		return Allow, "ok:" + callHash
	})

	req := Request{Tool: "bash", Action: ActionBash, Command: "whoami"}
	dec, reason := g.Check(t.Context(), req)
	assert.Equal(t, Ask, dec)

	dec, _ = g.ConsultAuthority(t.Context(), dec, reason, req)
	assert.Equal(t, Allow, dec)

	traj := g.Trajectory()
	assert.Equal(t, 1, traj.Rulings.Len())
	assert.Equal(t, Allow, traj.Rulings.All()[0].Decision)
}

func TestCheckSurfacesRecoveryPaths(t *testing.T) {
	p := DefaultPolicy()
	p.BashAllow = nil
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	g.contracts.Register("failing_tool", Contract{
		Tool:         "failing_tool",
		Contribution: Label{Trust: User},
		Requires: []Precondition{
			{Kind: PreconditionPrior, Token: "missing_effect"},
		},
	})

	dec, reason := g.Check(t.Context(), Request{Tool: "failing_tool", Action: ActionBash, Command: "whoami"})
	assert.Equal(t, Ask, dec)
	assert.Contains(t, reason, "recovery:")
}
