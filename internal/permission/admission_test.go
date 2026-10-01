package permission

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdmitUnlabeledRequestAllowsByDefault(t *testing.T) {
	g, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)

	dec, _ := g.Admit(t.Context(), Request{Action: ActionBash, Command: "ls"})
	assert.Equal(t, Allow, dec)
}

func TestAdmitBlocksUntrustedOutputWhenConfigured(t *testing.T) {
	p := DefaultPolicy()
	p.NoUntrustedOutput = true
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	dec, _ := g.Admit(t.Context(), Request{
		Action: ActionBash,
		OutputLabel: Label{Trust: Untrusted},
	})
	assert.Equal(t, Deny, dec)

	dec, _ = g.Admit(t.Context(), Request{
		Action: ActionBash,
		OutputLabel: Label{Trust: User},
	})
	assert.Equal(t, Allow, dec)
}

func TestAdmitRequiredEffectsAskWhenNoLog(t *testing.T) {
	p := DefaultPolicy()
	p.RequiredEffects = []string{"ticket_created"}
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	dec, _ := g.Admit(t.Context(), Request{
		Action:      ActionWrite,
		OutputLabel: Label{EffectTokens: []string{"ticket_created"}},
	})
	assert.Equal(t, Ask, dec)
}

func TestAllowAllImplementsAdmissionGate(t *testing.T) {
	var a AllowAll
	dec, _ := a.Admit(t.Context(), Request{})
	assert.Equal(t, Allow, dec)
}
