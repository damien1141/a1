package gittool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

func TestGitTool_Definition(t *testing.T) {
	tool := GitTool()
	assert.Equal(t, "git", tool.Definition.Name)
	assert.False(t, tool.Definition.Readable,
		"add/restore/commit mutate shared git state — a parallel [add, commit] batch could misorder")
	assert.NotNil(t, tool.Run)
	assert.NotNil(t, tool.DetailFromArgs)
	require.NotNil(t, tool.Definition.Params)
	assert.Equal(t, []string{"mode"}, tool.Definition.Params.Required)
}

func TestBuildArgs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		binary   string
		wantArgv []string
	}{
		{"status", `{"mode":"status"}`, "git", []string{"status", "--porcelain=v1", "-b"}},
		{"diff default", `{"mode":"diff"}`, "git", []string{"diff"}},
		{"diff staged", `{"mode":"diff","staged":true}`, "git", []string{"diff", "--cached"}},
		{"diff staged path", `{"mode":"diff","staged":true,"path":"cmd/x"}`, "git",
			[]string{"diff", "--cached", "--", "cmd/x"}},
		{"log default n", `{"mode":"log"}`, "git", []string{"log", "-n", "20"}},
		{"log n oneline path", `{"mode":"log","n":5,"oneline":true,"path":"cmd/x"}`, "git",
			[]string{"log", "-n", "5", "--oneline", "--", "cmd/x"}},
		{"show", `{"mode":"show","rev":"v1.2.3"}`, "git", []string{"show", "v1.2.3"}},
		{"branch list", `{"mode":"branch"}`, "git", []string{"branch", "-vv"}},
		{"branch create", `{"mode":"branch","name":"feat"}`, "git", []string{"branch", "feat"}},
		{"branch create from start", `{"mode":"branch","name":"feat","start":"main"}`, "git",
			[]string{"branch", "feat", "main"}},
		{"add", `{"mode":"add","files":["a.go","b.go"]}`, "git",
			[]string{"add", "--", "a.go", "b.go"}},
		{"restore", `{"mode":"restore","files":["a.go"]}`, "git",
			[]string{"restore", "--", "a.go"}},
		{"restore staged", `{"mode":"restore","files":["a.go"],"staged":true}`, "git",
			[]string{"restore", "--staged", "--", "a.go"}},
		{"commit staged only", `{"mode":"commit","message":"fix"}`, "git",
			[]string{"commit", "-m", "fix"}},
		{"commit with files", `{"mode":"commit","message":"fix","files":["a.go","b.go"]}`, "git",
			[]string{"commit", "-m", "fix", "--", "a.go", "b.go"}},
		{"commit amend with message", `{"mode":"commit","message":"better","amend":true}`, "git",
			[]string{"commit", "--amend", "-m", "better"}},
		{"commit amend keeps old message", `{"mode":"commit","amend":true}`, "git",
			[]string{"commit", "--amend", "--no-edit"}},
		{"push default remote", `{"mode":"push"}`, "git", []string{"push", "origin"}},
		{"push remote and branch", `{"mode":"push","remote":"upstream","branch":"main"}`, "git",
			[]string{"push", "upstream", "main"}},
		{"pull default remote", `{"mode":"pull"}`, "git", []string{"pull", "origin"}},
		{"pull remote", `{"mode":"pull","remote":"upstream"}`, "git", []string{"pull", "upstream"}},
		{"pr list default", `{"mode":"pr"}`, "gh", []string{"pr", "list"}},
		{"pr view", `{"mode":"pr","action":"view","number":12}`, "gh", []string{"pr", "view", "12"}},
		{"pr create", `{"mode":"pr","action":"create","title":"T","body":"B","base":"main"}`, "gh",
			[]string{"pr", "create", "--title", "T", "--body", "B", "--base", "main"}},
		{"pr create no base", `{"mode":"pr","action":"create","title":"T"}`, "gh",
			[]string{"pr", "create", "--title", "T", "--body", ""}},
		{"issue list", `{"mode":"issue"}`, "gh", []string{"issue", "list"}},
		{"issue view", `{"mode":"issue","action":"view","number":7}`, "gh", []string{"issue", "view", "7"}},
		{"issue create no base field", `{"mode":"issue","action":"create","title":"T","body":"B","base":"x"}`, "gh",
			[]string{"issue", "create", "--title", "T", "--body", "B"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var in gitInput
			require.NoError(t, json.Unmarshal([]byte(tt.input), &in))
			mode := strings.ToLower(strings.TrimSpace(in.Mode))
			binary, argv, err := buildArgs(mode, in)
			require.NoError(t, err)
			assert.Equal(t, tt.binary, binary)
			assert.Equal(t, tt.wantArgv, argv)
		})
	}
}

func TestBuildArgs_Errors(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSubstr string
	}{
		{"missing mode", `{}`, "mode is required"},
		{"unknown mode", `{"mode":"checkout"}`, `unknown git mode "checkout"`},
		{"show without rev", `{"mode":"show"}`, "show requires rev"},
		{"add without files", `{"mode":"add"}`, "files is required"},
		{"add with blank files", `{"mode":"add","files":["","  "]}`, "files is required"},
		{"restore without files", `{"mode":"restore","files":[]}`, "files is required"},
		{"commit without message", `{"mode":"commit"}`, "commit requires message"},
		{"pr view without number", `{"mode":"pr","action":"view"}`, "view requires number"},
		{"issue view without number", `{"mode":"issue","action":"view"}`, "view requires number"},
		{"pr create without title", `{"mode":"pr","action":"create"}`, "create requires title"},
		{"issue create without title", `{"mode":"issue","action":"create","body":"b"}`, "create requires title"},
		{"unknown pr action", `{"mode":"pr","action":"merge"}`, `unknown pr action "merge"`},
		{"branch start without name", `{"mode":"branch","start":"main"}`, "start requires name"},
		{"rev looks like a flag", `{"mode":"show","rev":"--amend"}`, "must not start with '-'"},
		{"path looks like a flag", `{"mode":"diff","path":"--cached"}`, "must not start with '-'"},
		{"branch name looks like a flag", `{"mode":"branch","name":"-d"}`, "must not start with '-'"},
		{"remote looks like a flag", `{"mode":"push","remote":"--all"}`, "must not start with '-'"},
		{"push branch looks like a flag", `{"mode":"push","branch":"--force"}`, "must not start with '-'"},
		{"base looks like a flag", `{"mode":"pr","action":"create","title":"t","base":"--x"}`, "must not start with '-'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var in gitInput
			require.NoError(t, json.Unmarshal([]byte(tt.input), &in))
			mode := strings.ToLower(strings.TrimSpace(in.Mode))
			_, _, err := buildArgs(mode, in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSubstr)
		})
	}
}

// stubRunner replaces the exec boundary and records the last invocation.
func stubRunner(t *testing.T, out string, err error) *[]string {
	t.Helper()
	var calls []string
	orig := runCommand
	runCommand = func(_ context.Context, binary string, argv []string, dir string) (string, error) {
		calls = append(calls, fmt.Sprintf("%s %s @%s", binary, strings.Join(argv, " "), dir))
		return out, err
	}
	t.Cleanup(func() { runCommand = orig })
	return &calls
}

func TestRunGit_RunsInSessionCwd(t *testing.T) {
	calls := stubRunner(t, "## main...origin/main\n", nil)
	ctx := tooldef.WithCwd(t.Context(), "/repo/session")
	res, err := runGit(ctx, json.RawMessage(`{"mode":"status"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "## main")
	assert.Equal(t, "status", res.Detail)
	require.Len(t, *calls, 1)
	assert.Equal(t, "git status --porcelain=v1 -b @/repo/session", (*calls)[0])
}

func TestRunGit_EmptyOutput(t *testing.T) {
	stubRunner(t, "", nil)
	res, err := runGit(t.Context(), json.RawMessage(`{"mode":"add","files":["a.go"]}`))
	require.NoError(t, err)
	assert.Equal(t, "(no output)", res.Content)
}

func TestRunGit_CapsOutput(t *testing.T) {
	big := strings.Repeat("x", maxOutputBytes+4096)
	stubRunner(t, big, nil)
	res, err := runGit(t.Context(), json.RawMessage(`{"mode":"log"}`))
	require.NoError(t, err)
	// Head kept (git orders output most-relevant-first), notice appended,
	// total bounded at the cap plus the notice line.
	assert.True(t, strings.HasPrefix(res.Content, "xxxx"), "head of output must be kept")
	assert.Contains(t, res.Content, "[output truncated: kept the first 64 KiB")
	assert.LessOrEqual(t, len(res.Content), maxOutputBytes+200)
}

func TestRunGit_RunnerErrorSurfaces(t *testing.T) {
	// The runner (real or stubbed) owns error formatting — output plus the
	// "(exit error: …)" line — and the handler passes it through verbatim.
	stubRunner(t, "", errors.New("fatal: not a git repository\n(exit error: exit status 128)"))
	_, err := runGit(t.Context(), json.RawMessage(`{"mode":"status"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fatal: not a git repository")
	assert.Contains(t, err.Error(), "(exit error: exit status 128)")
}

func TestRunGit_ParseError(t *testing.T) {
	_, err := runGit(t.Context(), json.RawMessage(`{"mode":`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse git arguments")
}

func TestDetailFromArgs(t *testing.T) {
	tool := GitTool()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"status", `{"mode":"status"}`, "status"},
		{"missing mode", `{}`, "git"},
		{"diff with path", `{"mode":"diff","path":"cmd/x"}`, "diff cmd/x"},
		{"show", `{"mode":"show","rev":"HEAD"}`, "show HEAD"},
		{"branch list", `{"mode":"branch"}`, "branch list"},
		{"branch create", `{"mode":"branch","name":"feat"}`, "branch feat"},
		{"add files", `{"mode":"add","files":["a.go","b.go"]}`, "add a.go b.go"},
		{"commit message", `{"mode":"commit","message":"fix the auth loop"}`, "commit fix the auth loop"},
		{"long message truncated", `{"mode":"commit","message":"` + strings.Repeat("m", 60) + `"}`,
			"commit " + strings.Repeat("m", 40) + "…"},
		{"commit amend no message", `{"mode":"commit","amend":true}`, "commit --amend"},
		{"push", `{"mode":"push","branch":"main"}`, "push origin main"},
		{"push custom remote", `{"mode":"push","remote":"upstream","branch":"main"}`, "push upstream main"},
		{"pull", `{"mode":"pull"}`, "pull origin"},
		{"pr list", `{"mode":"pr"}`, "pr list"},
		{"pr view", `{"mode":"pr","action":"view","number":12}`, "pr view 12"},
		{"pr create", `{"mode":"pr","action":"create","title":"Add thing"}`, "pr create Add thing"},
		{"issue view", `{"mode":"issue","action":"view","number":7}`, "issue view 7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tool.DetailFromArgs(json.RawMessage(tt.input)))
		})
	}
}

// --- End-to-end against the real git binary (skipped when git is absent) ---

// initRepo creates a hermetic repo: global config is neutralized so host
// commit.gpgsign / user identity cannot leak in (same trick as the
// internal/util/diffreview git tests), and author/committer identity comes
// from the environment.
func initRepo(t *testing.T) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("GIT_AUTHOR_NAME", "a1-test")
	t.Setenv("GIT_AUTHOR_EMAIL", "a1-test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "a1-test")
	t.Setenv("GIT_COMMITTER_EMAIL", "a1-test@example.com")

	dir := t.TempDir()
	// exec.Command is fine in tests: the production runner stays injectable.
	for _, argv := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "commit.gpgsign", "false"},
		{"config", "user.name", "a1-test"},
		{"config", "user.email", "a1-test@example.com"},
	} {
		cmd := exec.Command(gitPath, argv...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", argv, out)
	}
	return dir
}

func runInRepo(t *testing.T, dir, input string) (tooldef.Result, error) {
	t.Helper()
	return runGit(tooldef.WithCwd(t.Context(), dir), json.RawMessage(input))
}

func TestGitTool_E2E_StatusAddCommitLog(t *testing.T) {
	dir := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0o644))

	res, err := runInRepo(t, dir, `{"mode":"status"}`)
	require.NoError(t, err)
	// Before the first commit the branch line reads "## No commits yet on main".
	assert.Contains(t, res.Content, "No commits yet on main")
	assert.Contains(t, res.Content, "?? hello.txt")

	_, err = runInRepo(t, dir, `{"mode":"add","files":["hello.txt"]}`)
	require.NoError(t, err)

	res, err = runInRepo(t, dir, `{"mode":"status"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "A  hello.txt")

	_, err = runInRepo(t, dir, `{"mode":"commit","message":"add hello"}`)
	require.NoError(t, err)

	res, err = runInRepo(t, dir, `{"mode":"status"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "## main", "after the first commit the branch line is plain")
	assert.NotContains(t, res.Content, "hello.txt", "committed file must leave the status list")

	res, err = runInRepo(t, dir, `{"mode":"log","oneline":true}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "add hello")

	res, err = runInRepo(t, dir, `{"mode":"show","rev":"HEAD"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "add hello")
}

func TestGitTool_E2E_CommitWithFiles(t *testing.T) {
	dir := initRepo(t)
	write := func(name, content string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	write("a.txt", "a\n")
	write("b.txt", "b\n")
	_, err := runInRepo(t, dir, `{"mode":"add","files":["a.txt","b.txt"]}`)
	require.NoError(t, err)
	_, err = runInRepo(t, dir, `{"mode":"commit","message":"seed"}`)
	require.NoError(t, err)

	// Pathspec commits cover tracked files, so modify both, then commit only a.
	write("a.txt", "a2\n")
	write("b.txt", "b2\n")
	_, err = runInRepo(t, dir, `{"mode":"commit","message":"only a","files":["a.txt"]}`)
	require.NoError(t, err)

	res, err := runInRepo(t, dir, `{"mode":"status"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, " M b.txt", "b.txt change must stay uncommitted")
	assert.NotContains(t, res.Content, "a.txt", "a.txt change must be committed")

	res, err = runInRepo(t, dir, `{"mode":"log","oneline":true}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "only a")
	assert.Contains(t, res.Content, "seed")
}

func TestGitTool_E2E_BranchCreateAndList(t *testing.T) {
	dir := initRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f.txt"), []byte("f\n"), 0o644))
	_, err := runInRepo(t, dir, `{"mode":"add","files":["f.txt"]}`)
	require.NoError(t, err)
	_, err = runInRepo(t, dir, `{"mode":"commit","message":"seed"}`)
	require.NoError(t, err)

	_, err = runInRepo(t, dir, `{"mode":"branch","name":"feat"}`)
	require.NoError(t, err)

	res, err := runInRepo(t, dir, `{"mode":"branch"}`)
	require.NoError(t, err)
	assert.Contains(t, res.Content, "* main")
	assert.Contains(t, res.Content, "feat")
}

func TestGitTool_E2E_FailedCallIsErrorWithOutput(t *testing.T) {
	dir := initRepo(t)
	_, err := runInRepo(t, dir, `{"mode":"show","rev":"does-not-exist"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}
