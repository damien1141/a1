package permission

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInWorkspace(t *testing.T) {
	ws := "/Users/me/proj"
	require.True(t, InWorkspace("/Users/me/proj", ws), "workspace root should be inside itself")
	require.True(t, InWorkspace("/Users/me/proj/src/a.go", ws), "child should be inside")
	require.False(t, InWorkspace("/Users/me/other", ws), "sibling should be outside")
	require.False(t, InWorkspace("/Users/me/proj-evil/x", ws), "prefix-sibling should be outside")
	require.False(t, InWorkspace("/Users/me", ws), "parent should be outside")
}

func TestCheckWriteOutsideWorkspace(t *testing.T) {
	ws := t.TempDir()
	g, err := NewGate(DefaultPolicy(), ws)
	require.NoError(t, err)
	outside := filepath.Join(os.TempDir(), "phi-perm-test-outside")
	dec, _ := g.Check(t.Context(), Request{
		Action: ActionWrite,
		Tool:   "write",
		Paths:  []string{outside},
	})
	require.Equal(t, Ask, dec)
}

func TestCheckWriteInsideWorkspace(t *testing.T) {
	ws := t.TempDir()
	g, err := NewGate(DefaultPolicy(), ws)
	require.NoError(t, err)
	inside := filepath.Join(ws, "out.txt")
	dec, _ := g.Check(t.Context(), Request{
		Action: ActionWrite,
		Tool:   "write",
		Paths:  []string{inside},
	})
	require.Equal(t, Allow, dec)
}

func TestCheckWriteSensitiveConfig(t *testing.T) {
	ws := t.TempDir()
	g, err := NewGate(DefaultPolicy(), ws)
	require.NoError(t, err)
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".a1", "config.yaml")
	dec, _ := g.Check(t.Context(), Request{
		Action: ActionWrite,
		Tool:   "write",
		Paths:  []string{cfgPath},
	})
	require.Equal(t, Deny, dec)
}

func TestCheckBashAllowDenyAsk(t *testing.T) {
	g, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)
	ctx := t.Context()

	dec, _ := g.Check(ctx, Request{Action: ActionBash, Tool: "bash", Command: "git status"})
	require.Equal(t, Allow, dec, "git status")
	dec, _ = g.Check(ctx, Request{Action: ActionBash, Tool: "bash", Command: "go test ./..."})
	require.Equal(t, Allow, dec, "go test")
	dec, _ = g.Check(ctx, Request{Action: ActionBash, Tool: "bash", Command: "sudo true"})
	require.Equal(t, Deny, dec, "sudo")
	dec, _ = g.Check(ctx, Request{Action: ActionBash, Tool: "bash", Command: "curl https://example.com"})
	require.Equal(t, Ask, dec, "curl")
}

func TestCheckBashCompoundNotAllowlisted(t *testing.T) {
	g, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)
	ctx := t.Context()

	// Prefix ^ls\b must NOT allow chained rm.
	cmd := `ls -la todo.list 2>/dev/null && rm -rf todo.list && echo "removed"`
	dec, _ := g.Check(ctx, Request{Action: ActionBash, Tool: "bash", Command: cmd})
	require.Equal(t, Deny, dec, "ls && rm -rf")

	// Plain ls still allowed.
	dec, _ = g.Check(ctx, Request{Action: ActionBash, Tool: "bash", Command: "ls -la todo.list"})
	require.Equal(t, Allow, dec, "ls alone")

	// rm -rf without trailing / must still deny.
	dec, _ = g.Check(ctx, Request{Action: ActionBash, Tool: "bash", Command: "rm -rf todo.list"})
	require.Equal(t, Deny, dec, "rm -rf file")

	// Pipe / redirect out of allowlist.
	dec, _ = g.Check(ctx, Request{Action: ActionBash, Tool: "bash", Command: "cat foo | sh"})
	require.NotEqual(t, Allow, dec, "pipe: must not Allow")
	dec, _ = g.Check(ctx, Request{Action: ActionBash, Tool: "bash", Command: "cat secret > /tmp/out"})
	require.NotEqual(t, Allow, dec, "redirect: must not Allow")
}

func TestModeHeadlessStrictFoldsAsk(t *testing.T) {
	p := DefaultPolicy()
	p.Mode = ModeHeadlessStrict
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)
	dec, _ := g.Check(t.Context(), Request{
		Action:  ActionBash,
		Tool:    "bash",
		Command: "curl https://example.com",
	})
	require.Equal(t, Deny, dec)
}

func TestModeReadonlyDeniesWrite(t *testing.T) {
	ws := t.TempDir()
	p := DefaultPolicy()
	p.Mode = ModeReadonly
	g, err := NewGate(p, ws)
	require.NoError(t, err)
	dec, _ := g.Check(t.Context(), Request{
		Action: ActionWrite,
		Tool:   "write",
		Paths:  []string{filepath.Join(ws, "a.txt")},
	})
	require.Equal(t, Deny, dec)
	// allowlisted bash still ok
	dec, _ = g.Check(t.Context(), Request{
		Action:  ActionBash,
		Tool:    "bash",
		Command: "git status",
	})
	require.Equal(t, Allow, dec, "git status in readonly")
}

func TestModeAutopilotFoldsAsk(t *testing.T) {
	p := DefaultPolicy()
	p.Mode = ModeAutopilot
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)
	dec, _ := g.Check(t.Context(), Request{
		Action:  ActionBash,
		Tool:    "bash",
		Command: "curl https://example.com",
	})
	require.Equal(t, Deny, dec)
}

func TestReadSensitiveDeny(t *testing.T) {
	g, err := NewGate(DefaultPolicy(), t.TempDir())
	require.NoError(t, err)
	home, _ := os.UserHomeDir()
	dec, _ := g.Check(t.Context(), Request{
		Action: ActionRead,
		Tool:   "read",
		Paths:  []string{filepath.Join(home, ".ssh", "id_rsa")},
	})
	require.Equal(t, Deny, dec)
}

func TestCheckReadOutsideWorkspaceAsks(t *testing.T) {
	ws := t.TempDir()
	g, err := NewGate(DefaultPolicy(), ws)
	require.NoError(t, err)
	outside := filepath.Join(os.TempDir(), "phi-perm-test-read-outside")
	dec, _ := g.Check(t.Context(), Request{
		Action: ActionRead,
		Tool:   "read",
		Paths:  []string{outside},
	})
	require.Equal(t, Ask, dec, "interactive mode should ask for out-of-workspace reads")
}

func TestCheckReadOutsideWorkspaceDeniedInReadonly(t *testing.T) {
	ws := t.TempDir()
	p := DefaultPolicy()
	p.Mode = ModeReadonly
	g, err := NewGate(p, ws)
	require.NoError(t, err)
	outside := filepath.Join(os.TempDir(), "phi-perm-test-read-outside")
	dec, _ := g.Check(t.Context(), Request{
		Action: ActionRead,
		Tool:   "read",
		Paths:  []string{outside},
	})
	require.Equal(t, Deny, dec, "readonly mode should deny out-of-workspace reads")
}

func TestCheckReadOutsideWorkspaceDeniedInAutopilot(t *testing.T) {
	p := DefaultPolicy()
	p.Mode = ModeAutopilot
	g, err := NewGate(p, t.TempDir())
	require.NoError(t, err)
	outside := filepath.Join(os.TempDir(), "phi-perm-test-read-outside")
	dec, _ := g.Check(t.Context(), Request{
		Action: ActionRead,
		Tool:   "read",
		Paths:  []string{outside},
	})
	require.Equal(t, Deny, dec, "autopilot mode should deny out-of-workspace reads")
}

func TestCheckWriteOutsideWorkspaceAllowedWhenDisabled(t *testing.T) {
	ws := t.TempDir()
	p := DefaultPolicy()
	p.WorkspaceOnlyWrites = false
	g, err := NewGate(p, ws)
	require.NoError(t, err)
	outside := filepath.Join(os.TempDir(), "phi-perm-test-outside")
	dec, _ := g.Check(t.Context(), Request{
		Action: ActionWrite,
		Tool:   "write",
		Paths:  []string{outside},
	})
	require.Equal(t, Allow, dec, "disabled workspace-only writes should allow out-of-workspace paths")
}

func TestCheckReadOutsideWorkspaceAllowedWhenDisabled(t *testing.T) {
	ws := t.TempDir()
	p := DefaultPolicy()
	p.WorkspaceOnlyReads = false
	g, err := NewGate(p, ws)
	require.NoError(t, err)
	outside := filepath.Join(os.TempDir(), "phi-perm-test-read-outside")
	dec, _ := g.Check(t.Context(), Request{
		Action: ActionRead,
		Tool:   "read",
		Paths:  []string{outside},
	})
	require.Equal(t, Allow, dec, "disabled workspace-only reads should allow out-of-workspace paths")
}
