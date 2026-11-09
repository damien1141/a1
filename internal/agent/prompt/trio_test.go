package prompt

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// oracle: hand-written golden literal — the MCP section for two known servers
// must render to exactly this text, and blank/whitespace-only server entries
// must be filtered before rendering.
func TestMCPBlockMatchesGoldenLiteral_Differential(t *testing.T) {
	golden := "# MCP\n" +
		"\n" +
		"MCP servers are configured for this session. Tool schemas stay out of context — discover on demand.\n" +
		"\n" +
		"## How to use MCP\n" +
		"- When a task needs an external capability these servers might provide (including docs/URLs the repo cannot answer), use the mcp_* tools.\n" +
		"- `mcp_list` with a server name → tool names on that server.\n" +
		"- `mcp_inspect` → compact params for one tool, then `mcp_call` to invoke.\n" +
		"- Do not guess tool names or arguments; list/inspect first.\n" +
		"\n" +
		"## Configured servers\n" +
		"- alpha\n" +
		"- beta"

	assert.Equal(t, golden, mcpBlock([]string{"alpha", "beta"}), "two servers must render the golden MCP section")
	assert.Equal(t, golden, mcpBlock([]string{"", "  ", "alpha", "beta", "\t"}),
		"blank server entries must be filtered before rendering")

	assert.Empty(t, mcpBlock(nil), "no servers must yield no MCP block")
	assert.Empty(t, mcpBlock([]string{"", "   "}), "only-blank servers must yield no MCP block")
}

// oracle: naive strings.ReplaceAll reference — with no context files and no
// git workspace in play, two Build runs from different cwds may differ only
// by the interpolated current-directory value.
func TestBuildCwdInterpolationVsReplaceAll_Differential(t *testing.T) {
	// Keep every environment input constant: an isolated HOME (so ~/.a1 has
	// no AGENTS.md) and an isolated TMPDIR base for the two cwds.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TMPDIR", home)

	d1 := t.TempDir()
	t.Chdir(d1)
	out1 := Build("", false, 0, nil)
	require.Contains(t, out1, "- Current directory: "+d1, "cwd must be interpolated into the prompt")
	require.Contains(t, out1, "- Workspace: \n", "no .git ancestor means an empty workspace")

	d2 := t.TempDir()
	t.Chdir(d2)
	out2 := Build("", false, 0, nil)
	require.Contains(t, out2, "- Current directory: "+d2)

	assert.Equal(t, strings.ReplaceAll(out1, d1, d2), out2,
		"the two prompts must differ exactly by the cwd interpolation")
}

// refFormatProjectContext re-derives the project-context rendering from the
// documented spec with fmt.Sprintf blocks: one wrapper, one block per file,
// a 2KB content cap with the truncation marker, and newline normalization.
func refFormatProjectContext(files []ContextFile) string {
	if len(files) == 0 {
		return ""
	}
	const maxContentBytes = 2048
	block := func(f ContextFile) string {
		content := f.Content
		if len(content) > maxContentBytes {
			content = content[:maxContentBytes] +
				"\n[... truncated — read the full file for remaining instructions]\n"
		}
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return fmt.Sprintf("<project_instructions path=%q>\n%s</project_instructions>\n\n", f.Path, content)
	}
	var b strings.Builder
	b.WriteString("<project_context>\n\n")
	b.WriteString("Project-specific instructions and guidelines:\n\n")
	for _, f := range files {
		b.WriteString(block(f))
	}
	b.WriteString("</project_context>")
	return b.String()
}

// oracle: naive reference implementation (above) over boundary and randomized
// inputs — the 2048-byte cap (exact, off-by-one, multibyte byte-cut), the
// trailing-newline normalization, empty and multiple files.
func TestFormatProjectContextVsNaiveReference_Differential(t *testing.T) {
	fixed := [][]ContextFile{
		nil,
		{},
		{{Path: "/tmp/AGENTS.md", Content: "use gofmt"}},
		{{Path: "/tmp/AGENTS.md", Content: "use gofmt\n"}},
		{{Path: "/tmp/AGENTS.md", Content: ""}},
		{{Path: "/tmp/dir with spaces/AGENTS.md", Content: "x\ny\n"}},
		{
			{Path: "/a/AGENTS.md", Content: "first"},
			{Path: "/b/CLAUDE.md", Content: "second\n"},
		},
		// Exactly at the cap: no truncation.
		{{Path: "/a/AGENTS.md", Content: strings.Repeat("a", 2048)}},
		// One byte over: truncation marker kicks in.
		{{Path: "/a/AGENTS.md", Content: strings.Repeat("a", 2049)}},
		// Multibyte content whose 2048-byte cut lands mid-rune: the cap is
		// byte-based, so both sides must cut the same bytes.
		{{Path: "/a/AGENTS.md", Content: strings.Repeat("日", 700)}},
	}
	for i, files := range fixed {
		assert.Equal(t, refFormatProjectContext(files), formatProjectContext(files), "fixed case %d", i)
	}

	rng := rand.New(rand.NewSource(13))
	alphabet := []string{"a", "B", "\n", "é", "日", " ", "\t"}
	for i := range 100 {
		var b strings.Builder
		for n := rng.Intn(2100); n > 0; n-- {
			b.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		files := []ContextFile{
			{Path: fmt.Sprintf("/w%d/AGENTS.md", i), Content: b.String()},
			{Path: fmt.Sprintf("/w%d/CLAUDE.md", i), Content: "tail"},
		}
		assert.Equal(t, refFormatProjectContext(files), formatProjectContext(files), "random case %d", i)
	}
}

// ---------- negative ----------

// Unreadable or malformed candidate files must be skipped gracefully: a
// directory named AGENTS.md (ReadFile fails with EISDIR) falls through to
// the next candidate, a dir with no readable candidate yields nil, and a
// nonexistent dir in the walk contributes nothing — no panics anywhere.
func TestLoadContextFileUnreadableCandidates_Negative(t *testing.T) {
	// A directory squatting on the AGENTS.md name: ReadFile errors, the
	// loader must fall through to CLAUDE.md in the same dir.
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "AGENTS.md"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("fallback\n"), 0o644))
	f := loadContextFileFromDir(dir)
	require.NotNil(t, f)
	assert.Equal(t, filepath.Join(dir, "CLAUDE.md"), f.Path)
	assert.Equal(t, "fallback\n", f.Content)

	// Every candidate unreadable (all squatting directories): nil, no panic.
	all := t.TempDir()
	for _, name := range contextFileCandidates {
		require.NoError(t, os.Mkdir(filepath.Join(all, name), 0o755))
	}
	assert.Nil(t, loadContextFileFromDir(all))

	// Nonexistent directory: nil.
	assert.Nil(t, loadContextFileFromDir(filepath.Join(t.TempDir(), "does-not-exist")))

	// The walk over a cwd chain containing unreadable-only dirs and a
	// nonexistent agentDir yields no files without panicking.
	chain := t.TempDir()
	nested := filepath.Join(chain, "a", "b")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.Mkdir(filepath.Join(chain, "a", "AGENTS.md"), 0o755))
	got := loadProjectContextFiles(nested, filepath.Join(chain, "missing-agent-dir"))
	assert.Empty(t, got)
}
