package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damien1141/a1/internal/llm/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSkillPathDefaultsToGlobalSkillsDir verifies the todo item "wire default
// skills directory": when config.yaml omits skill_path, the resolved model
// config points at ~/.a1/skills, and that directory is populated with the
// embedded default skills on first discovery.
func TestSkillPathDefaultsToGlobalSkillsDir(t *testing.T) {
	// Redirect the home dir to a temp dir so the test never touches the real
	// ~/.a1 — os.UserHomeDir uses HOME on Unix and USERPROFILE on Windows.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	// Write a minimal config so LoadConfig succeeds.
	cfgPath := filepath.Join(home, ".a1", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
models:
  - name: test-model
    api_key: test-key
    default: true
`), 0o644))

	proj, err := Discover("")
	require.NoError(t, err)

	// Discover must have installed the default skills into ~/.a1/skills.
	skillsDir := proj.Global().SkillsDir()
	entries, err := os.ReadDir(skillsDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries, "Discover should install default skills into %s", skillsDir)

	require.NoError(t, proj.LoadConfig())
	cfg := proj.Config()
	require.Equal(t, skillsDir, cfg.SkillPath, "SkillPath must default to the global skills dir")
	m := cfg.Model()
	require.Equal(t, skillsDir, m.SkillPath, "model config must carry the default SkillPath")

	// The skills palette must be able to enumerate what was installed.
	skillList, err := skills.LoadSkills(m.SkillPath)
	require.NoError(t, err)
	assert.NotEmpty(t, skillList, "default skills must be loadable from the resolved SkillPath")
}