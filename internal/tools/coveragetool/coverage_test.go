package coveragetool

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverageTool_Definition(t *testing.T) {
	tool := CoverageTool()
	assert.Equal(t, "coverage", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "coverage")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestParseCoverageProfile_Basic(t *testing.T) {
	profile := `mode: set
github.com/example/main.go:10.2,5.1 1 5
github.com/example/main.go:20.2,3.1 1 3
github.com/example/other.go:5.2,10.1 1 0
github.com/example/other.go:30.2,2.1 1 2
`

	tmpFile, err := os.CreateTemp("", "coverage-*.out")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(profile)
	require.NoError(t, err)
	tmpFile.Close()

	results, err := parseCoverageProfile(tmpFile.Name(), "/tmp", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// main.go: 5 + 3 = 8 covered out of 5 + 3 = 8 total = 100%
	// other.go: 0 + 2 = 2 covered out of 10 + 2 = 12 total = 16.67%
	assert.Contains(t, results[0].path, "other.go")
	assert.InDelta(t, 16.67, results[0].percent, 0.1)
	assert.Contains(t, results[1].path, "main.go")
	assert.InDelta(t, 100.0, results[1].percent, 0.1)
}

func TestParseCoverageProfile_Sorting(t *testing.T) {
	profile := `mode: set
github.com/example/high.go:10.2,2.1 1 2
github.com/example/high.go:20.2,3.1 1 3
github.com/example/low.go:10.2,5.1 1 0
github.com/example/med.go:10.2,2.1 1 2
github.com/example/med.go:20.2,3.1 1 0
`

	tmpFile, err := os.CreateTemp("", "coverage-*.out")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(profile)
	require.NoError(t, err)
	tmpFile.Close()

	results, err := parseCoverageProfile(tmpFile.Name(), "/tmp", 10)
	require.NoError(t, err)
	require.Len(t, results, 3)
	// high.go: 2+3=5 covered out of 2+3=5 total = 100%
	// low.go: 0 covered out of 5 total = 0%
	// med.go: 2 covered out of 2+3=5 total = 40%
	assert.Contains(t, results[0].path, "low.go")
	assert.InDelta(t, 0.0, results[0].percent, 0.1)
	assert.Contains(t, results[1].path, "med.go")
	assert.InDelta(t, 40.0, results[1].percent, 0.1)
	assert.Contains(t, results[2].path, "high.go")
	assert.InDelta(t, 100.0, results[2].percent, 0.1)
}

func TestRunCoverage_NoGoFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(coverageInput{Path: root, Limit: 10})
	out, err := runCoverage(t.Context(), raw)
	// No go.mod, so go test will fail. The tool should return an error.
	require.Error(t, err)
	assert.Empty(t, out.Content)
}

func TestRunCoverage_PathNotFound(t *testing.T) {
	raw, _ := json.Marshal(coverageInput{Path: "/nonexistent/path/12345", Limit: 10})
	out, err := runCoverage(t.Context(), raw)
	require.Error(t, err)
	assert.Empty(t, out.Content)
}

func TestRunCoverage_Integration(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func PublicFunc() int {
	return 42
}
`)
	writeFile(t, root, "main_test.go", `package main

import "testing"
func TestPublicFunc(t *testing.T) {
	if PublicFunc() != 42 {
		t.Error("expected 42")
	}
}
`)

	initGoMod(t, root)

	raw, _ := json.Marshal(coverageInput{Path: root, Limit: 10})
	out, err := runCoverage(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.go")
	assert.NotContains(t, out.Content, "No coverage data found")
}

func initGoMod(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("go", "mod", "init", "example.com/test")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
