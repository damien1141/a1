package permission

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultTrustForToolArgs(t *testing.T) {
	assert.Equal(t, Label{Trust: User, ReaderSet: []string{"user"}}, DefaultTrustForToolArgs("read"))
	assert.Equal(t, Label{Trust: User, ReaderSet: []string{"user"}}, DefaultTrustForToolArgs("grep"))
	assert.Equal(t, Label{Trust: Unknown, ReaderSet: []string{}}, DefaultTrustForToolArgs("bash"))
	assert.Equal(t, Label{Trust: Unknown, ReaderSet: []string{}}, DefaultTrustForToolArgs("unknown"))
}

func TestDefaultTrustForToolOutput(t *testing.T) {
	assert.Equal(t, Label{Trust: Untrusted, ReaderSet: []string{}}, DefaultTrustForToolOutput("bash"))
	assert.Equal(t, Label{Trust: User, ReaderSet: []string{"user"}}, DefaultTrustForToolOutput("read"))
	assert.Equal(t, Label{Trust: Unknown, ReaderSet: []string{}}, DefaultTrustForToolOutput("unknown"))
}

func TestExtractAttachesInputLabels(t *testing.T) {
	req, err := Extract("bash", []byte(`{"command":"ls"}`))
	require.NoError(t, err)
	assert.Equal(t, []Label{{Trust: Unknown, ReaderSet: []string{}}}, req.InputLabels)

	req, err = Extract("read", []byte(`{"path":"/tmp/x"}`))
	require.NoError(t, err)
	assert.Equal(t, []Label{{Trust: User, ReaderSet: []string{"user"}}}, req.InputLabels)
}

func TestCheckUnlabeledRequestPreservesBehavior(t *testing.T) {
	ws := t.TempDir()
	g, err := NewGate(DefaultPolicy(), ws)
	require.NoError(t, err)

	dec, _ := g.Check(t.Context(), Request{Action: ActionBash, Command: "git status"})
	assert.Equal(t, Allow, dec)

	dec, _ = g.Check(t.Context(), Request{Action: ActionBash, Command: "curl https://example.com"})
	assert.Equal(t, Ask, dec)

	dec, _ = g.Check(t.Context(), Request{Action: ActionWrite, Paths: []string{ws + "/a.txt"}})
	assert.Equal(t, Allow, dec)
}
