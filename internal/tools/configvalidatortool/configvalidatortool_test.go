package configvalidatortool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/project"
)

func TestConfigValidatorTool_Definition(t *testing.T) {
	tool := ConfigValidatorTool()
	assert.Equal(t, "config_validate", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "config")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunConfigValidate_MissingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	raw, _ := encode(configValidatorInput{Path: filepath.Join(home, ".a1", "config.yaml")})
	out, err := runConfigValidate(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "not found")
}

func TestRunConfigValidate_ValidConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cfg := filepath.Join(home, ".a1", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
	require.NoError(t, os.WriteFile(cfg, []byte(`models:
  - name: test-model
    api_key: sk-test
`), 0o644))

	proj, err := project.Discover("")
	require.NoError(t, err)
	require.NoError(t, proj.LoadConfig())
	project.SetDefaultProject(proj)

	raw, _ := encode(configValidatorInput{Path: cfg})
	out, err := runConfigValidate(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Config is valid")
}

func TestRunConfigValidate_MissingAPIKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cfg := filepath.Join(home, ".a1", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
	require.NoError(t, os.WriteFile(cfg, []byte(`models:
  - name: test-model
`), 0o644))

	proj, err := project.Discover("")
	require.NoError(t, err)
	project.SetDefaultProject(proj)

	raw, _ := encode(configValidatorInput{Path: cfg})
	out, err := runConfigValidate(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "missing api_key")
}

func encode(v any) ([]byte, error) {
	return json.Marshal(v)
}
