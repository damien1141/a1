package permission

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtractGitAt is the mode × param-shape table for the governed git tool.
// The mapping it locks in is the enforcement story of the roadmap item:
// reads never prompt, index writes carry paths, and the commit/push/pull/
// create tier is bash-class (Ask under the default interactive policy).
func TestExtractGitAt(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name       string
		args       string
		wantAction Action
		wantPaths  []string
		wantCmd    string
	}{
		{"status", `{"mode":"status"}`, ActionRead, nil, ""},
		{"diff", `{"mode":"diff","staged":true,"path":"cmd/x"}`, ActionRead, nil, ""},
		{"log", `{"mode":"log","n":5}`, ActionRead, nil, ""},
		{"show", `{"mode":"show","rev":"HEAD"}`, ActionRead, nil, ""},
		{"branch list", `{"mode":"branch"}`, ActionRead, nil, ""},
		{
			"branch create", `{"mode":"branch","name":"feat"}`, ActionWrite,
			[]string{root}, "",
		},
		{
			"add files", `{"mode":"add","files":["a.go","sub/b.go"]}`, ActionWrite,
			[]string{filepath.Join(root, "a.go"), filepath.Join(root, "sub", "b.go")}, "",
		},
		{
			"restore files staged", `{"mode":"restore","files":["a.go"],"staged":true}`, ActionWrite,
			[]string{filepath.Join(root, "a.go")}, "",
		},
		{"add without files falls back to bash", `{"mode":"add"}`, ActionBash, nil, "git add"},
		{"restore without files falls back to bash", `{"mode":"restore","files":[]}`, ActionBash, nil, "git restore"},
		{"commit", `{"mode":"commit","message":"fix"}`, ActionBash, nil, "git commit (message)"},
		{"commit amend", `{"mode":"commit","amend":true}`, ActionBash, nil, "git commit (message)"},
		{"push", `{"mode":"push"}`, ActionBash, nil, "git push origin"},
		{"push branch", `{"mode":"push","remote":"upstream","branch":"main"}`, ActionBash, nil, "git push upstream main"},
		{"pull", `{"mode":"pull","remote":"upstream"}`, ActionBash, nil, "git pull upstream"},
		{"pr list default", `{"mode":"pr"}`, ActionRead, nil, ""},
		{"pr view", `{"mode":"pr","action":"view","number":12}`, ActionRead, nil, ""},
		{"pr create", `{"mode":"pr","action":"create","title":"T"}`, ActionBash, nil, "gh pr create (title)"},
		{"issue list", `{"mode":"issue"}`, ActionRead, nil, ""},
		{"issue view", `{"mode":"issue","action":"view","number":7}`, ActionRead, nil, ""},
		{"issue create", `{"mode":"issue","action":"create","title":"T"}`, ActionBash, nil, "gh issue create (title)"},
		{"missing mode asks", `{}`, ActionBash, nil, "git"},
		{"unknown mode asks", `{"mode":"checkout"}`, ActionBash, nil, "git checkout"},
		{"mode case-insensitive", `{"mode":"STATUS"}`, ActionRead, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := ExtractAt("git", json.RawMessage(tt.args), root)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAction, req.Action)
			if tt.wantPaths == nil {
				assert.Empty(t, req.Paths)
			} else {
				assert.Equal(t, tt.wantPaths, req.Paths)
			}
			assert.Equal(t, tt.wantCmd, req.Command)
		})
	}
}

func TestExtractGit_BadJSON(t *testing.T) {
	_, err := Extract("git", json.RawMessage(`{"mode":`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git args")
}

// TestExtractGit_GateFolds locks the gate-level semantics the extraction
// feeds: interactive mode asks for a commit (the "no commit without explicit
// request" guarantee), readonly mode denies it, and reads pass everywhere.
func TestExtractGit_GateFolds(t *testing.T) {
	ws := t.TempDir()
	ctx := t.Context()

	commitReq, err := ExtractAt("git", json.RawMessage(`{"mode":"commit","message":"m"}`), ws)
	require.NoError(t, err)
	statusReq, err := ExtractAt("git", json.RawMessage(`{"mode":"status"}`), ws)
	require.NoError(t, err)
	addReq, err := ExtractAt("git", json.RawMessage(`{"mode":"add","files":["a.go"]}`), ws)
	require.NoError(t, err)

	interactive, err := NewGate(DefaultPolicy(), ws)
	require.NoError(t, err)
	dec, reason := interactive.Check(ctx, commitReq)
	assert.Equal(t, Ask, dec, "commit must ask in interactive mode: %s", reason)
	dec, _ = interactive.Check(ctx, statusReq)
	assert.Equal(t, Allow, dec, "status is a read")
	dec, _ = interactive.Check(ctx, addReq)
	assert.Equal(t, Allow, dec, "in-workspace add is a plain write")

	readonly, err := NewGate(ChildPolicy(ModeReadonly), ws)
	require.NoError(t, err)
	dec, _ = readonly.Check(ctx, commitReq)
	assert.Equal(t, Allow, dec, "ChildPolicy allows non-denied bash — same exposure as the bash tool a child already has")
	strictReadonly, err := NewGate(func() Policy {
		p := DefaultPolicy()
		p.Mode = ModeReadonly
		return p
	}(), ws)
	require.NoError(t, err)
	dec, _ = strictReadonly.Check(ctx, commitReq)
	assert.Equal(t, Deny, dec, "readonly mode denies commit")
	dec, _ = strictReadonly.Check(ctx, addReq)
	assert.Equal(t, Deny, dec, "readonly mode denies add")
	dec, _ = strictReadonly.Check(ctx, statusReq)
	assert.Equal(t, Allow, dec, "readonly mode still reads")
}
