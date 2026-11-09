package skilltool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/session"
)

// ---------- negative ----------

// list_skills degrades gracefully on every hostile shape of configuration
// and input: blank path, missing directory, empty directory, a directory
// holding only unparseable skills, and garbage JSON arguments (the tool
// takes no arguments, so it must ignore them rather than fail).
func TestRunListSkills_Negative(t *testing.T) {
	session.SetDefaultVault(nil)

	t.Run("blank skill path", func(t *testing.T) {
		for _, path := range []string{"", "   "} {
			res, err := ListSkillsTool(path).Run(t.Context(), json.RawMessage(`{}`))
			require.NoError(t, err)
			assert.Equal(t, "No skill directory configured.", res.Content)
			assert.Equal(t, "list", res.Detail)
			assert.Equal(t, "0 skills", res.Summary)
		}
	})

	t.Run("missing directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "does-not-exist")
		res, err := ListSkillsTool(path).Run(t.Context(), json.RawMessage(`{}`))
		require.NoError(t, err)
		assert.Equal(t, "No skills found in "+path+".", res.Content)
		assert.Equal(t, "0 skills", res.Summary)
	})

	t.Run("empty directory", func(t *testing.T) {
		dir := t.TempDir()
		res, err := ListSkillsTool(dir).Run(t.Context(), json.RawMessage(`{}`))
		require.NoError(t, err)
		assert.Equal(t, "No skills found in "+dir+".", res.Content)
		assert.Equal(t, "0 skills", res.Summary)
	})

	t.Run("only broken skills are skipped", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "nofront"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "nofront", "SKILL.md"),
			[]byte("just some markdown, no frontmatter\n"), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "unclosed"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "unclosed", "SKILL.md"),
			[]byte("---\nname: never closed\n"), 0o644))

		res, err := ListSkillsTool(dir).Run(t.Context(), json.RawMessage(`{}`))
		require.NoError(t, err)
		assert.Equal(t, "No skills found in "+dir+".", res.Content,
			"a bad skill file must not fail the listing; nothing valid is left to show")
		assert.Equal(t, "0 skills", res.Summary)
	})

	t.Run("hostile arguments are ignored", func(t *testing.T) {
		dir := t.TempDir()
		writeSkill(t, dir, "alpha", "a1")
		res, err := ListSkillsTool(dir).Run(t.Context(), json.RawMessage(`not json at all`))
		require.NoError(t, err)
		assert.Contains(t, res.Content, "### alpha")
	})
}

// ---------- differential ----------

// oracle: hand-written golden literal — the catalog format the model
// consumes (### name, then the trigger list, or the description fallback
// when a skill declares no triggers), written out by hand for a two-skill
// fixture.
func TestRunListSkills_Catalog_Differential(t *testing.T) {
	session.SetDefaultVault(nil)
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "go, golang")
	writeSkill(t, dir, "beta", "")

	res, err := ListSkillsTool(dir).Run(t.Context(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, `### alpha

triggers: go, golang

### beta

description: beta skill

`, res.Content)
	assert.Equal(t, "2 skills", res.Summary)
	assert.Equal(t, res.Content, res.Output)
}

// naiveSkillNames walks with os.ReadDir and extracts the frontmatter name of
// every SKILL.md that starts with a closed --- block — a strings-only scan
// sharing no code with skills.LoadSkills.
func naiveSkillNames(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, naiveSkillNames(t, filepath.Join(dir, e.Name()))...)
			continue
		}
		if e.Name() != "SKILL.md" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		content := string(b)
		if !strings.HasPrefix(content, "---") {
			continue
		}
		parts := strings.SplitN(content, "---", 3)
		if len(parts) < 3 {
			continue
		}
		for _, line := range strings.Split(parts[1], "\n") {
			if name, ok := strings.CutPrefix(strings.TrimSpace(line), "name:"); ok {
				names = append(names, strings.TrimSpace(name))
				break
			}
		}
	}
	return names
}

// oracle: os.ReadDir reference — the expected catalog names are re-derived
// with an independent ReadDir recursion plus a naive frontmatter scan,
// skipping non-SKILL.md files and files without valid frontmatter, and
// compared (order included) against the ### headers the tool emits.
func TestRunListSkills_Names_Differential(t *testing.T) {
	session.SetDefaultVault(nil)
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "go")
	writeSkill(t, dir, "beta", "")

	// A broken skill must not hide the rest.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "broken"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken", "SKILL.md"),
		[]byte("no frontmatter\n"), 0o644))

	// A skill nested deeper than one level is still discovered.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "deep", "inner"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "deep", "inner", "SKILL.md"),
		[]byte("---\nname: nested-one\ndescription: inner skill\nmetadata:\n  triggers: deep\n---\n\nbody\n"), 0o644))

	// Distractions that are not skills.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "notaskill"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notaskill", "README.md"), []byte("# readme\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "loose.md"), []byte("# loose\n"), 0o644))

	res, err := ListSkillsTool(dir).Run(t.Context(), json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "3 skills", res.Summary)

	var got []string
	for _, line := range strings.Split(res.Content, "\n") {
		if name, ok := strings.CutPrefix(line, "### "); ok {
			got = append(got, name)
		}
	}
	assert.Equal(t, naiveSkillNames(t, dir), got)
}
