package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompileIncludesWorkspaceTempAndCaches(t *testing.T) {
	ws := t.TempDir()
	p := Compile(ws, false, nil)
	found := map[string]bool{}
	for _, dir := range p.ReadWrite {
		found[dir] = true
	}
	assert.True(t, found[ws], "workspace is read-write")
	assert.True(t, found[os.TempDir()], "temp dir is read-write")
	assert.False(t, p.Network, "network denied by default")
}

func TestCompileNetworkToggle(t *testing.T) {
	assert.False(t, Compile(t.TempDir(), false, nil).Network)
	assert.True(t, Compile(t.TempDir(), true, nil).Network)
}

func TestCompileExcludesSensitivePrefixes(t *testing.T) {
	ws := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Deny the whole home: every cache candidate under it drops out.
	p := Compile(ws, false, []string{home})
	for _, dir := range p.ReadWrite {
		assert.NotContains(t, dir, home, "no candidate under a denied prefix")
	}
	assert.NotEmpty(t, p.ReadWrite, "workspace and temp survive the deny")
}

func TestCompileDenyPrefixExpandsTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// ~/.cargo denied via the tilde form; the rest of the caches stay.
	p := Compile(t.TempDir(), false, []string{"~/.cargo"})
	for _, dir := range p.ReadWrite {
		assert.NotEqual(t, filepath.Join(home, ".cargo"), dir)
	}
}

func TestCompileDedupesAndCleans(t *testing.T) {
	ws := t.TempDir()
	p := Compile(ws+string(os.PathSeparator), false, nil)
	assert.Contains(t, p.ReadWrite, filepath.Clean(ws))
	seen := map[string]int{}
	for _, dir := range p.ReadWrite {
		seen[dir]++
	}
	for dir, n := range seen {
		assert.Equal(t, 1, n, "dir %s appears once", dir)
	}
}

// TestCompileDropsMissingCandidates pins the machine-dependence bug: a
// Landlock rule anchors on an O_PATH open of its directory, and one missing
// candidate used to fail that open and kill EVERY sandboxed start — total
// bash failure on any machine that never touched a toolchain (.cargo on a
// Rust-free box, .a1/browser-profiles before the first browser spawn).
// Every RW anchor that survives Compile must now exist on disk.
func TestCompileDropsMissingCandidates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Create exactly one of the home candidates; the rest stay missing.
	require.NoError(t, os.Mkdir(filepath.Join(home, ".cache"), 0o755))
	ws := t.TempDir()
	p := Compile(ws, false, nil)
	require.NotEmpty(t, p.ReadWrite, "workspace and temp survive")
	for _, dir := range p.ReadWrite {
		st, err := os.Stat(dir)
		require.NoError(t, err, "every RW anchor must exist: %s", dir)
		assert.True(t, st.IsDir(), "RW anchors are directories: %s", dir)
	}
	assert.Contains(t, p.ReadWrite, filepath.Join(home, ".cache"), "the existing cache is kept")
	assert.NotContains(t, p.ReadWrite, filepath.Join(home, ".cargo"), "the missing cache is dropped")
	assert.NotContains(t, p.ReadWrite, filepath.Join(home, ".a1", "browser-profiles"), "undone ~/.a1 subdirs are dropped")
}

// TestCompileKeepsWorkspaceWhenCachesMissing: an empty home (fresh machine)
// still yields a usable profile — the workspace and the temp dir carry the
// build's writes; caches appear as their toolchains get installed.
func TestCompileKeepsWorkspaceWhenCachesMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // empty: every home candidate is missing
	ws := t.TempDir()
	p := Compile(ws, false, nil)
	assert.Contains(t, p.ReadWrite, ws)
	assert.Contains(t, p.ReadWrite, os.TempDir())
	assert.Len(t, p.ReadWrite, 2, "workspace and temp, nothing else")
}

func TestSeatbeltProfileShape(t *testing.T) {
	p := Profile{
		ReadWrite: []string{"/ws", "/tmp"},
		Network:   false,
	}
	s := seatbeltProfile(&p)
	lines := strings.Split(strings.TrimSpace(s), "\n")
	require.GreaterOrEqual(t, len(lines), 5)
	assert.Equal(t, "(version 1)", lines[0])
	assert.Equal(t, "(allow default)", lines[1])
	assert.Equal(t, "(deny file-write*)", lines[2])
	assert.Contains(t, s, `(allow file-write* (subpath "/ws"))`)
	assert.Contains(t, s, `(allow file-write* (subpath "/tmp"))`)
	assert.Contains(t, s, "(deny network*)")
	// The RW re-allow must come after the blanket deny — last match wins.
	assert.Greater(t, strings.Index(s, `(subpath "/ws")`), strings.Index(s, "(deny file-write*)"))
}

func TestSeatbeltProfileNetworkAllowed(t *testing.T) {
	s := seatbeltProfile(&Profile{ReadWrite: []string{"/ws"}, Network: true})
	assert.NotContains(t, s, "(deny network*)")
}

// TestStartWithoutProfileIsPlainExec proves the degradation path: nil
// profile starts the command untouched.
func TestStartWithoutProfileIsPlainExec(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a process")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "marker.txt")
	cmd := markerCommand(t, out)
	require.NoError(t, Start(cmd, nil))
	require.NoError(t, cmd.Wait())
	_, err := os.Stat(out)
	require.NoError(t, err, "plain exec ran and wrote the marker")
}

// TestStartSandboxedContainsWritesOutsideRW is the enforcement round-trip.
// Skipped on hosts without Landlock (containers without the syscall, macOS
// without sandbox-exec, or any other unsupported platform) — a skipped
// containment test is an honest report, not a pass.
func TestStartSandboxedContainsWritesOutsideRW(t *testing.T) {
	if !supported() {
		t.Skip("no sandbox backend on this host")
	}
	ws := t.TempDir()
	outside := t.TempDir()
	// A hand-built narrow profile, not Compile: Compile grants RW on
	// os.TempDir() (builds need their temp files), and both t.TempDir()
	// dirs live under it — the "outside" probe would land inside the RW
	// set and the containment assertion below would be vacuous.
	p := Profile{ReadWrite: []string{ws}, Network: false}

	// Inside the RW set: writes succeed.
	cmd := markerCommand(t, filepath.Join(ws, "ok.txt"))
	require.NoError(t, Start(cmd, &p))
	require.NoError(t, cmd.Wait())
	assert.FileExists(t, filepath.Join(ws, "ok.txt"))

	// Outside the RW set: the write fails.
	cmd = markerCommand(t, filepath.Join(outside, "nope.txt"))
	require.NoError(t, Start(cmd, &p))
	err := cmd.Wait()
	assert.Error(t, err, "a write outside the RW set must fail under the sandbox")
	assert.NoFileExists(t, filepath.Join(outside, "nope.txt"))
}

// markerCommand builds a shell command that touches a file, so tests can
// observe containment as file existence.
func markerCommand(t *testing.T, path string) *exec.Cmd {
	t.Helper()
	return exec.Command("sh", "-c", "echo x > "+quoteShell(path))
}

func quoteShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
