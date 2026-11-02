package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.skill")
	content := `---
name: Test Skill
description: This is a test skill
license: MIT
---
This is the instruction body.
It can have multiple lines.
`
	err := os.WriteFile(testFile, []byte(content), 0o644)
	assert.NoError(t, err)

	skill, err := Parse(testFile)
	assert.NoError(t, err)
	assert.NotNil(t, skill)
	assert.Equal(t, "Test Skill", skill.Name)
	assert.Equal(t, "This is a test skill", skill.Description)
	assert.Equal(t, "MIT", skill.License)
	assert.Equal(t, "This is the instruction body.\nIt can have multiple lines.", skill.Body)
	assert.Equal(t, testFile, skill.SkillFilePath)
	assert.Equal(t, filepath.Dir(testFile), skill.Path)
}

func TestParse_NoFrontmatter(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.skill")
	content := `Just some text without frontmatter.`
	err := os.WriteFile(testFile, []byte(content), 0o644)
	assert.NoError(t, err)

	_, err = Parse(testFile)
	assert.ErrorIs(t, err, ErrInvalidFrontmatter)
}

func TestParse_UnclosedFrontmatter(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.skill")
	content := `---
name: Incomplete
description: Missing closing delimiter
`
	err := os.WriteFile(testFile, []byte(content), 0o644)
	assert.NoError(t, err)

	_, err = Parse(testFile)
	assert.ErrorIs(t, err, ErrFrontmatterNotClosed)
}

func TestParse_InvalidYAML(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.skill")
	content := `---
name: "unclosed quote
---
body
`
	err := os.WriteFile(testFile, []byte(content), 0o644)
	assert.NoError(t, err)

	_, err = Parse(testFile)
	assert.ErrorIs(t, err, ErrInvalidYAML)
}

func TestFind(t *testing.T) {
	list := []*Skill{
		{Name: "Example Skill", Path: "/skills/example-skill"},
		{Name: "building-plugins", Path: "/skills/building-plugins"},
	}
	assert.Equal(t, "Example Skill", Find(list, "Example Skill").Name)
	assert.Equal(t, "Example Skill", Find(list, "example skill").Name)
	assert.Equal(t, "Example Skill", Find(list, "example-skill").Name)
	assert.Nil(t, Find(list, "missing"))
	assert.Nil(t, Find(nil, "x"))
}

func TestLoadSkills_NonExistentDir(t *testing.T) {
	skills, err := LoadSkills("/non/existent/directory")
	assert.NoError(t, err)
	assert.Nil(t, skills)
}

func TestLoadSkills_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	skills, err := LoadSkills(dir)
	assert.NoError(t, err)
	assert.Nil(t, skills)
}

func TestLoadSkills_SingleSkill(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	err := os.MkdirAll(skillDir, 0o755)
	assert.NoError(t, err)

	content := `---
name: My Skill
description: Does something useful
---
Follow these instructions when the task matches.
`
	err = os.WriteFile(filepath.Join(skillDir, SkillFileName), []byte(content), 0o644)
	assert.NoError(t, err)

	skills, err := LoadSkills(dir)
	assert.NoError(t, err)
	assert.Len(t, skills, 1)
	assert.Equal(t, "My Skill", skills[0].Name)
	assert.Equal(t, "Does something useful", skills[0].Description)
	assert.Equal(t, "Follow these instructions when the task matches.", skills[0].Body)
}

func TestLoadSkills_MultipleSkills(t *testing.T) {
	dir := t.TempDir()

	sk1 := filepath.Join(dir, "skill-a", SkillFileName)
	sk2 := filepath.Join(dir, "sub", "skill-b", SkillFileName)

	assert.NoError(t, os.MkdirAll(filepath.Dir(sk1), 0o755))
	assert.NoError(t, os.MkdirAll(filepath.Dir(sk2), 0o755))
	assert.NoError(t, os.WriteFile(sk1, []byte("---\nname: A\n---\nbody a"), 0o644))
	assert.NoError(t, os.WriteFile(sk2, []byte("---\nname: B\n---\nbody b"), 0o644))

	skills, err := LoadSkills(dir)
	assert.NoError(t, err)
	assert.Len(t, skills, 2)
}

func TestLoadSkills_SkipsNonSkillFiles(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	assert.NoError(t, os.MkdirAll(skillDir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(skillDir, SkillFileName), []byte("---\nname: OK\n---\nbody"), 0o644))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("not a skill"), 0o644))

	skills, err := LoadSkills(dir)
	assert.NoError(t, err)
	assert.Len(t, skills, 1)
}

func TestParse_FoldedDescription(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "SKILL.md")
	content := `---
name: collect-and-rank
description: >-
  排行榜：收集候选与指标。
  Use when the user wants a leaderboard.
---
Do the work.
`
	assert.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))
	skill, err := Parse(testFile)
	assert.NoError(t, err)
	assert.Equal(t, "collect-and-rank", skill.Name)
	assert.Contains(t, skill.Description, "排行榜")
	assert.Contains(t, skill.Description, "leaderboard")
	assert.Equal(t, "Do the work.", skill.Body)
}

func TestLoadSkills_SkipsInvalidSkill(t *testing.T) {
	dir := t.TempDir()
	okDir := filepath.Join(dir, "ok")
	badDir := filepath.Join(dir, "bad")
	assert.NoError(t, os.MkdirAll(okDir, 0o755))
	assert.NoError(t, os.MkdirAll(badDir, 0o755))
	assert.NoError(t, os.WriteFile(filepath.Join(okDir, SkillFileName), []byte("---\nname: OK\n---\nbody"), 0o644))
	assert.NoError(t, os.WriteFile(filepath.Join(badDir, SkillFileName), []byte("not a skill"), 0o644))

	list, err := LoadSkills(dir)
	assert.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "OK", list[0].Name)
}

func TestToPromptMarkdown_Empty(t *testing.T) {
	assert.Empty(t, ToPromptMarkdown(nil))
	assert.Empty(t, ToPromptMarkdown([]*Skill{}))
}

// The catalog renders the routing surface, not the prose: the prompt tells the
// model to match against triggers, so shipping the description instead left the
// prompt and the data disagreeing. Triggers are also far longer-lived content
// than the Location line the old renderer printed on every row.
func TestToPromptMarkdown_RendersTriggersNotProse(t *testing.T) {
	skills := []*Skill{
		{
			Name:          "golang-pro",
			Description:   "Use when writing Go 1.22+ that must pass go vet ...",
			SkillFilePath: "/home/user/.a1/skills/golang-pro/SKILL.md",
			Triggers:      []string{"go", "golang", "goroutine", "golangci-lint"},
		},
	}

	result := ToPromptMarkdown(skills)
	assert.Contains(t, result, "### golang-pro")
	assert.Contains(t, result, "triggers: go, golang, goroutine, golangci-lint")
	assert.NotContains(t, result, "must pass go vet", "the prose description is not shipped")
	assert.NotContains(t, result, "SKILL.md", "the location line is derivable from the name")
}

// A hand-written skill with no triggers still gets a row: falling back to its
// description keeps a third-party skill visible instead of silently invisible.
func TestToPromptMarkdown_FallsBackToDescription(t *testing.T) {
	skills := []*Skill{{Name: "homegrown", Description: "Does our house style"}}

	result := ToPromptMarkdown(skills)
	assert.Contains(t, result, "### homegrown")
	assert.Contains(t, result, "description: Does our house style")
	assert.NotContains(t, result, "triggers:")
}

func TestToPromptMarkdown_Multiple(t *testing.T) {
	skills := []*Skill{
		{Name: "Skill One", Description: "First", Triggers: []string{"alpha"}},
		{Name: "Skill Two", Description: "Second", Triggers: []string{"beta"}},
	}

	result := ToPromptMarkdown(skills)
	assert.Contains(t, result, "### Skill One")
	assert.Contains(t, result, "### Skill Two")
	assert.Contains(t, result, "triggers: alpha")
	assert.Contains(t, result, "triggers: beta")
}

// metadata.triggers is the field the router runs on, so the parser must read it
// out of the nested metadata: map it used to skip entirely.
func TestParse_ReadsMetadataTriggers(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.skill")
	content := `---
name: Test Skill
description: A test skill
license: MIT
metadata:
  author: someone
  version: "1.0.0"
  domain: language
  triggers: "go,golang,goroutine"
  role: specialist
---
Body here.
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))

	skill, err := Parse(testFile)
	require.NoError(t, err)
	assert.Equal(t, []string{"go", "golang", "goroutine"}, skill.Triggers)
	assert.Equal(t, "Test Skill", skill.Name, "the flat keys after metadata still parse")
	assert.Equal(t, "Body here.", skill.Body, "the body still parses")
}

// A block-scalar trigger list must not swallow the rest of the frontmatter.
func TestParse_TriggersAsBlockScalar(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.skill")
	content := `---
name: Test Skill
description: A test skill
metadata:
  triggers: >
    go,
    golang
license: MIT
---
Body here.
`
	require.NoError(t, os.WriteFile(testFile, []byte(content), 0o644))

	skill, err := Parse(testFile)
	require.NoError(t, err)
	assert.Equal(t, []string{"go", "golang"}, skill.Triggers)
	assert.Equal(t, "MIT", skill.License, "keys after the block still parse")
}

// Every shipped skill must carry a usable trigger list, or the router silently
// loses that entry.
func TestEmbeddedSkillsAllCarryTriggers(t *testing.T) {
	dir := t.TempDir()
	_, err := InstallDefault(dir)
	require.NoError(t, err)

	list, err := LoadSkills(dir)
	require.NoError(t, err)
	require.NotEmpty(t, list)

	var missing []string
	for _, s := range list {
		if len(s.Triggers) == 0 {
			missing = append(missing, s.Name)
		}
	}
	assert.Empty(t, missing, "every shipped skill must expose triggers for routing")
}

// TestParse_LoadsEmbeddedDefaults verifies the shipped skill library parses
// end-to-end: every SKILL.md under the embedded default/ tree must yield a
// usable Skill. Without this, a parser regression that rejects nested YAML
// blocks (like the metadata: map every shipped skill carries) silently
// returns an empty list and the skills palette shows "No skills found".
func TestParse_LoadsEmbeddedDefaults(t *testing.T) {
	dir := t.TempDir()
	files, err := InstallDefault(dir)
	require.NoError(t, err)
	require.Greater(t, files, 0, "InstallDefault should copy the embedded skills")

	list, err := LoadSkills(dir)
	require.NoError(t, err)
	// InstallDefault counts every file copied (frontmatter + references),
	// while LoadSkills counts parsed SKILL.md files; both must be non-zero
	// and the parsed count must not exceed the copied count.
	assert.Greater(t, len(list), 0, "default skills must parse")
	assert.LessOrEqual(t, len(list), files)
	for _, s := range list {
		assert.NotEmpty(t, s.Name, "skill must have a name")
		assert.NotEmpty(t, s.Description, "skill %q must have a description", s.Name)
		assert.NotEmpty(t, s.Body, "skill %q must have a body", s.Name)
	}
}
