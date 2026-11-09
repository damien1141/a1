package configvalidatortool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeHomeConfig installs a pristine HOME whose ~/.a1/config.yaml holds yaml,
// neutralizes every A1_*/PHI_* env override so runs are deterministic, and
// returns the config path. The validator loads through project.Discover, so
// the fixture must live under HOME, not just at the passed path.
func writeHomeConfig(t *testing.T, yaml string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{
		"A1_MODEL", "PHI_MODEL", "A1_API_KEY", "PHI_API_KEY", "A1_API_KEYS",
		"A1_BASE_URL", "PHI_BASE_URL", "A1_SKILL_PATH", "PHI_SKILL_PATH",
		"A1_THINK_LEVEL", "PHI_THINK_LEVEL", "A1_THINK_ENABLED", "PHI_THINK_ENABLED",
	} {
		t.Setenv(k, "")
	}
	cfgPath := filepath.Join(home, ".a1", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o755))
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0o644))
	return cfgPath
}

// inputJSON marshals the tool input, failing the test on an impossible error.
func inputJSON(t *testing.T, in configValidatorInput) []byte {
	t.Helper()
	raw, err := encode(in)
	require.NoError(t, err)
	return raw
}

// issueLines pulls the rendered "- [sev] msg" lines out of a verdict.
func issueLines(content string) []string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "- [") {
			lines = append(lines, line)
		}
	}
	return lines
}

// naiveDetail re-derives the summary line by counting severities over the
// expected issue list — never by running the tool.
func naiveDetail(expected []string) string {
	errs := 0
	for _, l := range expected {
		if strings.HasPrefix(l, "- [error]") {
			errs++
		}
	}
	return fmt.Sprintf("%d issues (%d errors)", len(expected), errs)
}

// oracle: hand-written golden verdict lines — for each fixture the expected
// issue set (severity + full message) is written out from the documented
// validation rules, and the detail summary is re-derived by counting over
// the golden lines, so the tool is never used to compute its own expectation.
func TestRunConfigValidate_Differential(t *testing.T) {
	t.Run("healthy single model", func(t *testing.T) {
		cfg := writeHomeConfig(t, `models:
  - name: main
    api_key: sk-1
`)

		out, err := runConfigValidate(t.Context(), inputJSON(t, configValidatorInput{Path: cfg}))
		require.NoError(t, err)
		assert.Equal(t, "Config is valid.\n", out.Content)
		assert.Equal(t, "0 issues", out.Detail)
		assert.Empty(t, issueLines(out.Content))
	})

	t.Run("non-default model missing api_key", func(t *testing.T) {
		cfg := writeHomeConfig(t, `models:
  - name: main
    api_key: sk-1
    default: true
  - name: spare
`)

		expected := []string{`- [error] model "spare" missing api_key`}
		out, err := runConfigValidate(t.Context(), inputJSON(t, configValidatorInput{Path: cfg}))
		require.NoError(t, err)
		assert.Equal(t, expected, issueLines(out.Content))
		assert.Equal(t, naiveDetail(expected), out.Detail)
	})

	t.Run("invalid permissions mode", func(t *testing.T) {
		cfg := writeHomeConfig(t, `models:
  - name: main
    api_key: sk-1
permissions:
  mode: sometimes
`)

		expected := []string{`- [error] invalid permissions.mode: "sometimes"`}
		out, err := runConfigValidate(t.Context(), inputJSON(t, configValidatorInput{Path: cfg}))
		require.NoError(t, err)
		assert.Equal(t, expected, issueLines(out.Content))
		assert.Equal(t, naiveDetail(expected), out.Detail)
	})

	t.Run("duplicate default model names", func(t *testing.T) {
		cfg := writeHomeConfig(t, `models:
  - name: twin
    api_key: sk-1
    default: true
  - name: twin
    api_key: sk-2
`)

		expected := []string{`- [warn] multiple default models configured; using "twin"`}
		out, err := runConfigValidate(t.Context(), inputJSON(t, configValidatorInput{Path: cfg}))
		require.NoError(t, err)
		assert.Equal(t, expected, issueLines(out.Content))
		assert.Equal(t, naiveDetail(expected), out.Detail)
	})

	t.Run("A1_API_KEY env is reported as info", func(t *testing.T) {
		cfg := writeHomeConfig(t, `models:
  - name: main
    api_key: sk-1
`)
		t.Setenv("A1_API_KEY", "sk-env")

		expected := []string{`- [info] A1_API_KEY is set`}
		out, err := runConfigValidate(t.Context(), inputJSON(t, configValidatorInput{Path: cfg}))
		require.NoError(t, err)
		assert.Equal(t, expected, issueLines(out.Content))
		assert.Equal(t, naiveDetail(expected), out.Detail)
	})

	t.Run("missing file", func(t *testing.T) {
		cfg := writeHomeConfig(t, `models:
  - name: main
    api_key: sk-1
`)
		ghost := filepath.Join(filepath.Dir(cfg), "ghost.yaml")

		expected := []string{fmt.Sprintf("- [error] config file not found: %s", ghost)}
		out, err := runConfigValidate(t.Context(), inputJSON(t, configValidatorInput{Path: ghost}))
		require.NoError(t, err)
		assert.Equal(t, expected, issueLines(out.Content))
		assert.Equal(t, naiveDetail(expected), out.Detail)

		// The render contract: a header, a 60-char ruler, then the lines.
		lines := strings.Split(out.Content, "\n")
		require.GreaterOrEqual(t, len(lines), 3)
		assert.Equal(t, "Config validation results:", lines[0])
		assert.Equal(t, strings.Repeat("=", 60), lines[1])
	})
}
