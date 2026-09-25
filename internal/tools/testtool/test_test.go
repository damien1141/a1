package testtool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func TestTestTool_Definition(t *testing.T) {
	tool := TestTool()
	assert.Equal(t, "test", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "test")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunTest_GoFailure(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "main_test.go", "package main\nimport \"testing\"\nfunc TestMain(t *testing.T) {\n\tt.Errorf(\"assertion failed\")\n}\n")

	output := `=== RUN   TestMain
=== FAIL: TestMain (0.00s)
    main_test.go:42: assertion failed
FAIL
FAIL	github.com/example	0.123s
`
	raw, _ := json.Marshal(testInput{Output: output})
	out, err := runTest(t.Context(), raw)
	require.NoError(t, err)
	t.Logf("Content: %q", out.Content)
	t.Logf("Detail: %q", out.Detail)
	assert.Contains(t, out.Content, "FAIL: TestMain")
	assert.Contains(t, out.Content, "main_test.go")
	assert.Contains(t, out.Detail, "1 failure")
}

func TestRunTest_PyTestFailure(t *testing.T) {
	output := `FAILED test_main.py::test_login - AssertionError: expected 200
`
	raw, _ := json.Marshal(testInput{Output: output})
	out, err := runTest(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "FAILED")
	assert.Contains(t, out.Content, "test_main.py")
}

func TestRunTest_CargoFailure(t *testing.T) {
	output := `test result: FAILED. 1 passed; 1 failed; 0 ignored
---- test_basic stdout ----
thread 'test_basic' panicked at 'assertion failed', src/lib.rs:42:9
note: run with RUST_BACKTRACE=1 for more information
`
	raw, _ := json.Marshal(testInput{Output: output})
	out, err := runTest(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "FAIL: test_basic")
	assert.Contains(t, out.Content, "lib.rs")
}

func TestRunTest_NoFailures(t *testing.T) {
	output := `PASS
ok  	github.com/example	0.123s
`
	raw, _ := json.Marshal(testInput{Output: output})
	out, err := runTest(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No test failures detected in output", out.Content)
}

func TestRunTest_EmptyOutput(t *testing.T) {
	raw, _ := json.Marshal(testInput{Output: ""})
	out, err := runTest(t.Context(), raw)
	require.Error(t, err)
	assert.Empty(t, out.Content)
}

func TestRunTest_Deduplicates(t *testing.T) {
	output := `=== RUN   TestMain
=== FAIL: TestMain (0.00s)
    main_test.go:42: assertion failed
    main_test.go:43: another failure
FAIL
`
	raw, _ := json.Marshal(testInput{Output: output})
	out, err := runTest(t.Context(), raw)
	require.NoError(t, err)
	// Should deduplicate same file:line
	assert.Contains(t, out.Content, "main_test.go")
}
