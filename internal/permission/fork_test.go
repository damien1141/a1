package permission

import (
	"context"
	"os"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForkChildGateInheritsParentLabel(t *testing.T) {
	parent, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)

	// The parent folds in an untrusted contribution, so its label descends.
	parent.Trajectory().Label = parent.Trajectory().Label.Meet(PartialLabel{
		Established: Label{Trust: Untrusted, ReaderSet: TopReaderSet},
		Unresolved:  []string{},
	})

	child := ForkChildGate(parent, ModeReadonly)

	require.NotNil(t, child.Trajectory().Label.Established.Trust)
	assert.Equal(t, Untrusted, child.Trajectory().Label.Established.Trust,
		"L0c := Lp: the child starts from the parent's label, not a fresh top")
	assert.Equal(t, parent.Trajectory().Label.Established.ReaderSet, child.Trajectory().Label.Established.ReaderSet,
		"the reader set is inherited too")
}

func TestForkChildGateSharesEffectLog(t *testing.T) {
	parent, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)
	parent.Trajectory().Effects.Commit("bash", "setup_done")

	child := ForkChildGate(parent, ModeReadonly)
	assert.True(t, child.Trajectory().Effects.Has("setup_done"),
		"E is shared tree-wide: the child sees the parent's committed effects")
}

func TestForkChildGateDoesNotInheritAcceptances(t *testing.T) {
	parent, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)
	parent.Trajectory().RecordNarrowingAcceptance("some-call")

	child := ForkChildGate(parent, ModeReadonly)
	assert.False(t, child.Trajectory().HasAcceptedNarrowing("some-call"),
		"acceptances are scope-bound and do not cross the fork boundary")
	assert.Empty(t, child.Trajectory().Rulings.All(),
		"parent rulings do not cross into the child")
}

func TestForkChildGateDoesNotInheritAllowAll(t *testing.T) {
	p := DefaultPolicy()
	p.DangerouslyAllowAll = true
	parent, err := NewGate(p, t.TempDir())
	require.NoError(t, err)

	child := ForkChildGate(parent, ModeReadonly)
	assert.False(t, child.Policy.DangerouslyAllowAll, "a child must still answer to the algebra")
	dec, _ := child.Check(
		context.Background(),
		Request{Action: ActionWrite, Tool: "write", Paths: []string{t.TempDir() + "/x"}},
	)
	assert.Equal(t, Deny, dec, "readonly child denies writes even when the parent is allow-all")
}

func TestForkChildGateLeavesParentLabelUnchanged(t *testing.T) {
	parent, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)
	before := parent.Trajectory().Label

	child := ForkChildGate(parent, ModeReadonly)
	child.Trajectory().Label = child.Trajectory().Label.Meet(PartialLabel{
		Established: Label{Trust: Untrusted, ReaderSet: TopReaderSet},
		Unresolved:  []string{},
	})
	for _, token := range []string{"a", "b"} {
		child.Trajectory().Effects.Commit("bash", token)
	}

	assert.Equal(t, before.Established.Trust, parent.Trajectory().Label.Established.Trust,
		"child folding must not descend the parent label (Theorem 8)")
	// E is shared tree-wide on purpose: a child that commits an effect before
	// being abandoned still leaves it recorded, so no_prior checks cannot be
	// evaded by forking. The parent *label* is what stays isolated.
	assert.True(t, parent.Trajectory().Effects.Has("a"),
		"shared effect log: child commits persist across the fork")
}

func TestStaticGateOfUnwrapsWrappers(t *testing.T) {
	inner, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)

	var allow atomic.Bool
	allow.Store(true)
	assert.Same(t, inner, StaticGateOf(&SessionAllowGate{Inner: inner, Enabled: &allow}))
	assert.Same(t, inner, StaticGateOf(&ConfinementGate{Inner: inner, Policy: ConfinementPolicy{}}))
	assert.Same(t, inner, StaticGateOf(inner))
	assert.Nil(t, StaticGateOf(AllowAll{}), "AllowAll has no trajectory")
	assert.Nil(t, StaticGateOf(nil))
}

func TestNewGateAtResolvesWorkspaceFromCwd(t *testing.T) {
	root := t.TempDir()
	sub := root + "/pkg/inner"
	require.NoError(t, mkdirAll(sub))

	g, err := NewGateAt(DefaultPolicy(), sub)
	require.NoError(t, err)
	assert.Equal(t, sub, g.Workspace,
		"no .git in the temp tree, so the session cwd is the workspace root")

	// A path outside the session cwd is gated, even though it is inside the
	// process cwd's repo.
	dec, _ := g.Check(t.Context(), Request{Action: ActionWrite, Tool: "write", Paths: []string{root + "/x.go"}})
	assert.Equal(t, Ask, dec, "write outside the session workspace asks")
}

func TestWorkspaceRootAtFindsGitRoot(t *testing.T) {
	root := t.TempDir()
	sub := root + "/a/b"
	require.NoError(t, mkdirAll(sub))
	require.NoError(t, mkdirAll(root+"/.git"))

	assert.Equal(t, root, WorkspaceRootAt(sub), "the gate root is the repository root, not the cwd")
}

func TestWorkspaceRootAtFallsBackForMissingDir(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, mkdirAll(root))
	assert.Equal(t, root, WorkspaceRootAt(root+"/gone/deeper"),
		"a deleted cwd falls back to the nearest existing ancestor")
}

// mkdirAll is a local shim so the test file needs no os import for one call.
func mkdirAll(dir string) error { return os.MkdirAll(dir, 0o755) }
