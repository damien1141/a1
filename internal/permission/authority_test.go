package permission

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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
