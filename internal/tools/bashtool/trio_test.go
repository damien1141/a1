package bashtool

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// ---------- differential ----------

// directShell is the oracle: the same command through raw os/exec against the
// same resolved shell, with the stdlib's own fork/exec, capture, and
// exit-code machinery. It returns the combined output and the exit status
// code (0 on clean exit).
func directShell(t *testing.T, ctx context.Context, command string, dir string) (string, int) {
	t.Helper()
	cfg, err := resolveShellConfig()
	require.NoError(t, err)
	var cmd *exec.Cmd
	if cfg.stdinMode {
		cmd = exec.CommandContext(ctx, cfg.shell, cfg.args...)
		cmd.Stdin = strings.NewReader(command)
	} else {
		cmd = exec.CommandContext(ctx, cfg.shell, append(cfg.args, command)...)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		code = exitErr.ExitCode()
	} else {
		require.NoError(t, runErr, "oracle run must fail only via exit status")
	}
	return buf.String(), code
}

// oracle: raw os/exec running the identical command in the same resolved
// shell — output bytes and exit codes must match ExecShell's wrapped run
// (capped collection + display formatting must be transparent below the
// display limits).
func TestExecShell_Differential(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"plain echo", "echo hello"},
		{"multiline printf", "printf 'a\\nb\\n'"},
		{"no trailing newline", "printf '%s' partial"},
		{"unicode", "printf '%s' 'héllo → 世界'"},
		{"pipeline", "printf 'b\\na\\n' | sort"},
		{"command substitution", `echo "$(printf x)y$(printf z)"`},
		{"stderr only", "echo oops >&2"},
		{"clean exit zero", "true"},
		{"nonzero exit", "exit 7"},
		{"failure builtin", "false"},
		{"subshell exit", "(exit 3)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantOut, wantCode := directShell(t, t.Context(), tc.command, "")
			res, err := ExecShell(t.Context(), tc.command, ShellExecOptions{})
			require.NoError(t, err)
			require.False(t, res.Canceled, "nothing cancels these commands")
			assert.Equal(t, wantOut, res.Output, "combined output must match raw os/exec")
			assert.Equal(t, wantCode, res.ExitCode, "exit code must match raw os/exec")
		})
	}

	t.Run("environment is inherited", func(t *testing.T) {
		t.Setenv("A1_TRIO_DIFF", "marker-value")
		const command = `printf '%s' "$A1_TRIO_DIFF"`
		wantOut, wantCode := directShell(t, t.Context(), command, "")
		res, err := ExecShell(t.Context(), command, ShellExecOptions{})
		require.NoError(t, err)
		assert.Equal(t, wantOut, res.Output)
		assert.Equal(t, wantCode, res.ExitCode)
		assert.Equal(t, "marker-value", res.Output)
	})

	t.Run("context cwd is the working directory", func(t *testing.T) {
		dir := t.TempDir()
		ctx := tooldef.WithCwd(t.Context(), dir)
		const command = "pwd"
		wantOut, wantCode := directShell(t, ctx, command, dir)
		res, err := ExecShell(ctx, command, ShellExecOptions{})
		require.NoError(t, err)
		assert.Equal(t, wantOut, res.Output)
		assert.Equal(t, wantCode, res.ExitCode)
	})

	t.Run("both streams are captured", func(t *testing.T) {
		const command = "printf out; printf err >&2"
		wantOut, _ := directShell(t, t.Context(), command, "")
		res, err := ExecShell(t.Context(), command, ShellExecOptions{})
		require.NoError(t, err)
		// The two streams interleave nondeterministically, so compare by
		// content and total size rather than byte order.
		assert.Contains(t, res.Output, "out")
		assert.Contains(t, res.Output, "err")
		assert.Len(t, res.Output, len(wantOut))
	})
}
