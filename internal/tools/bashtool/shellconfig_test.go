package bashtool

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

func TestIsLegacyWslBashPath(t *testing.T) {
	for _, p := range []string{
		`C:\Windows\System32\bash.exe`,
		`c:\windows\system32\bash.exe`,
		`C:/Windows/Sysnative/bash.exe`,
	} {
		assert.True(t, isLegacyWslBashPath(p), "want WSL shim for %q", p)
	}
	for _, p := range []string{
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\WSL\bash.exe`,
		`/bin/bash`,
		`C:\Windows\System32\wsl.exe`,
	} {
		assert.False(t, isLegacyWslBashPath(p), "not a WSL shim for %q", p)
	}
}

func TestGitBashPaths(t *testing.T) {
	t.Setenv("ProgramFiles", `C:\ProgramFiles`)
	t.Setenv("ProgramFiles(x86)", `C:\ProgramFiles(x86)`)
	t.Setenv("LocalAppData", `C:\Users\x\AppData\Local`)

	// A Git on PATH may append one more candidate; the env-var roots lead.
	got := gitBashPaths()
	require.GreaterOrEqual(t, len(got), 3)
	assert.Equal(t, filepath.Join(`C:\ProgramFiles`, "Git", "bin", "bash.exe"), got[0])
	assert.Equal(t, filepath.Join(`C:\ProgramFiles(x86)`, "Git", "bin", "bash.exe"), got[1])
	assert.Equal(t, filepath.Join(`C:\Users\x\AppData\Local`, "Git", "bin", "bash.exe"), got[2])

	// An unset root drops out instead of yielding a relative path.
	t.Setenv("ProgramFiles", "")
	got = gitBashPaths()
	require.GreaterOrEqual(t, len(got), 2)
	assert.Equal(t, filepath.Join(`C:\ProgramFiles(x86)`, "Git", "bin", "bash.exe"), got[0])
	assert.Equal(t, filepath.Join(`C:\Users\x\AppData\Local`, "Git", "bin", "bash.exe"), got[1])
}

// fakeGitTree lays out the Git for Windows shape - <root>\cmd\git.exe plus
// <root>\bin\bash.exe - and puts cmd on PATH so LookPath finds it. Off
// Windows the stub drops the .exe, since LookPath has no PATHEXT to lean on.
func fakeGitTree(t *testing.T, withBash bool) (bash string) {
	t.Helper()
	root := t.TempDir()
	cmd := filepath.Join(root, "cmd")
	require.NoError(t, os.MkdirAll(cmd, 0o755))
	git := "git"
	if runtime.GOOS == "windows" {
		git = "git.exe"
	}
	require.NoError(t, os.WriteFile(filepath.Join(cmd, git), []byte("MZ"), 0o755))
	bash = filepath.Join(root, "bin", "bash.exe")
	if withBash {
		require.NoError(t, os.MkdirAll(filepath.Dir(bash), 0o755))
		require.NoError(t, os.WriteFile(bash, []byte("MZ"), 0o755))
	}
	t.Setenv("PATH", cmd+string(os.PathListSeparator)+os.Getenv("PATH"))
	return bash
}

func TestGitBashFromGitOnPath(t *testing.T) {
	want := fakeGitTree(t, true)

	assert.Equal(t, want, gitBashFromGitOnPath())
}

func TestGitBashFromGitOnPathRejectsShim(t *testing.T) {
	fakeGitTree(t, false) // e.g. a scoop shim: no bin\bash.exe next to it

	assert.Empty(t, gitBashFromGitOnPath())
}

func TestConfigForShell(t *testing.T) {
	cfg := configForShell(`C:\Program Files\Git\bin\bash.exe`)
	require.False(t, cfg.stdinMode, "git bash should not use stdin mode")
	require.Len(t, cfg.args, 1)
	require.Equal(t, "-c", cfg.args[0])

	cfg = configForShell(`C:\Windows\System32\bash.exe`)
	require.True(t, cfg.stdinMode, "WSL shim must use stdin transport")
	require.Len(t, cfg.args, 1)
	require.Equal(t, "-s", cfg.args[0])
}

func TestResolveShellConfig(t *testing.T) {
	cfg, err := resolveShellConfig()
	require.NoError(t, err)
	require.NotEmpty(t, cfg.shell, "empty shell")
	if runtime.GOOS == "windows" {
		require.False(t, !cfg.stdinMode && (len(cfg.args) != 1 || cfg.args[0] != "-c"), "windows config: %+v", cfg)
	}
}

func TestPrependPathEntry(t *testing.T) {
	sep := string(os.PathListSeparator)
	got := prependPathEntry([]string{"PATH=/usr/bin" + sep + "/bin"}, "/x/bin")
	want := "PATH=/x/bin" + sep + "/usr/bin" + sep + "/bin"
	require.Equal(t, want, got[0])

	// Already present → unchanged.
	existing := "PATH=/usr/bin" + sep + "/x/bin"
	got = prependPathEntry([]string{existing}, "/x/bin")
	require.Len(t, got, 1)
	require.Equal(t, existing, got[0])

	// Windows-style key casing is matched case-insensitively.
	got = prependPathEntry([]string{"Path=C:\\Windows"}, `C:\Phi\bin`)
	require.True(t, strings.HasPrefix(got[0], "Path="))
	require.Contains(t, got[0], `C:\Phi\bin`)

	// No PATH entry → appended.
	got = prependPathEntry([]string{"HOME=/home/x"}, "/x/bin")
	require.Len(t, got, 2)
	require.Equal(t, "PATH=/x/bin", got[1])
}

func TestBuildShellCommand(t *testing.T) {
	cmd, err := buildShellCommand(t.Context(), "echo hi")
	require.NoError(t, err)
	require.NotNil(t, cmd.SysProcAttr, "expected process-group syscall attr")
	require.NotNil(t, cmd.Cancel, "expected tree-kill cancel")
	require.Equal(t, shellWaitDelay, cmd.WaitDelay)
	require.NotEmpty(t, cmd.Env, "expected enriched env")
}

func TestBuildShellCommandUsesContextCwd(t *testing.T) {
	dir := t.TempDir()
	cmd, err := buildShellCommand(tooldef.WithCwd(t.Context(), dir), "echo hi")
	require.NoError(t, err)
	require.Equal(t, dir, cmd.Dir)
}
