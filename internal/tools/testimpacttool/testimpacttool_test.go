package testimpacttool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTestImpactTool_Definition(t *testing.T) {
	tool := TestImpactTool()
	assert.Equal(t, "testimpact", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "test files")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunTestImpact_RequiresPath(t *testing.T) {
	raw, _ := json.Marshal(testImpactInput{})
	out, err := runTestImpact(t.Context(), raw)
	require.Error(t, err)
	assert.Empty(t, out.Content)
}

func TestRunTestImpact_NoImports(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func Main() {}
`)

	raw, _ := json.Marshal(testImpactInput{Path: "main.go", Root: root})
	out, err := runTestImpact(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No files import main.go", out.Content)
}

func TestRunTestImpact_FindsTestFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "auth.py", `def public(): pass
`)
	writeFile(t, root, "test_auth.py", `import auth

def test_public(): pass
`)

	raw, _ := json.Marshal(testImpactInput{Path: "auth.py", Root: root})
	out, err := runTestImpact(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "test_auth.py")
	assert.Equal(t, "1 test files affected by auth.py", out.Detail)
}

func TestFilterTestFiles(t *testing.T) {
	files := []string{
		"main.go",
		"main_test.go",
		"helper.py",
		"test_helper.py",
		"lib.rs",
		"test_lib.rs",
		"app.js",
		"app.test.js",
		"app.spec.ts",
	}
	result := filterTestFiles(files)
	assert.Len(t, result, 5)
	assert.Contains(t, result, "main_test.go")
	assert.Contains(t, result, "test_helper.py")
	assert.Contains(t, result, "test_lib.rs")
	assert.Contains(t, result, "app.test.js")
	assert.Contains(t, result, "app.spec.ts")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0o644)
	require.NoError(t, err)
}
