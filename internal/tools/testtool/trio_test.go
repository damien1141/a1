package testtool

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// goEnv neutralizes ambient Go flags and forbids network access: the fixture
// module has no dependencies, so GOPROXY=off and GOTOOLCHAIN=local keep the
// child `go test` hermetic and fast. GOCACHE stays ambient when the
// environment provides one (or a HOME to derive it from); a fully homeless
// environment gets a temp cache.
func goEnv(t *testing.T) []string {
	t.Helper()
	env := append(os.Environ(),
		"GOFLAGS=",
		"GO111MODULE=on",
		"GOWORK=off",
		"GOPROXY=off",
		"GOTOOLCHAIN=local",
	)
	if os.Getenv("GOCACHE") == "" && os.Getenv("HOME") == "" {
		env = append(env, "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	}
	return env
}

// runGoTest executes `go test` in a fixture module and returns its combined
// output plus the exit outcome.
func runGoTest(t *testing.T, dir string, args ...string) (string, bool) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", append([]string{"test"}, args...)...)
	cmd.Dir = dir
	cmd.Env = goEnv(t)
	out, err := cmd.CombinedOutput()
	ran := err == nil
	if ee, ok := err.(*exec.ExitError); ok {
		ran = ee.ExitCode() == 0
	} else {
		require.NoError(t, err, "go test must run")
	}
	return string(out), ran
}

// oracle: the go toolchain itself — a real `go test` run over a fixture
// module (exec.Command, no shared code with the parser) is fed to the tool,
// and the reported failure must match the fixture's failing line, derived
// from the source text, with context read straight from the file on disk.
func TestRunTest_RealGoTestVsExecOracle_Differential(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not in PATH")
	}

	t.Run("t.Errorf failure", func(t *testing.T) {
		fixture := t.TempDir()
		t.Chdir(fixture)
		writeFile(t, fixture, "go.mod", "module fixture.example/x\n\ngo 1.21\n")
		src := strings.Join([]string{
			"package x",
			"",
			"import \"testing\"",
			"",
			"func TestPasses(t *testing.T) {",
			"\tif 1+1 != 2 {",
			"\t\tt.Errorf(\"never\")",
			"\t}",
			"}",
			"",
			"func TestFails(t *testing.T) {",
			"\t\tt.Errorf(\"boom assertion failed\")",
			"}",
			"",
		}, "\n")
		writeFile(t, fixture, "main_test.go", src)

		// Reference: the failing assertion's line number comes from the
		// source text, not from any parser.
		srcLines := strings.Split(strings.TrimSuffix(src, "\n"), "\n")
		failLine := 0
		for i, l := range srcLines {
			if strings.Contains(l, "boom assertion failed") {
				failLine = i + 1
				break
			}
		}
		require.Equal(t, 12, failLine)

		out, passed := runGoTest(t, fixture, "-v", "./...")
		require.False(t, passed, "the fixture's failing test must fail the real go test")
		require.Contains(t, out, "boom assertion failed")

		raw, err := json.Marshal(testInput{Output: out, Context: 2})
		require.NoError(t, err)
		res, rerr := runTest(t.Context(), raw)
		require.NoError(t, rerr)

		// The real "--- FAIL:" header does not match the tool's "=== FAIL"
		// pattern; the failure is still caught through the indented
		// file:line body, without the test name.
		assert.Equal(t, "1 failures", res.Detail)
		assert.Contains(t, res.Content, "// boom assertion failed")
		assert.Contains(t, res.Content, fmt.Sprintf("main_test.go:>>%4d|%s", failLine, srcLines[failLine-1]))
		assert.Contains(t, res.Content, fmt.Sprintf("main_test.go:  %4d|%s", failLine-1, srcLines[failLine-2]))
		assert.Contains(t, res.Content, fmt.Sprintf("main_test.go:  %4d|%s", failLine+1, srcLines[failLine]))
	})

	t.Run("t.Fail failure without a message is invisible", func(t *testing.T) {
		// Pinned limitation: a test that fails without printing a file:line
		// leaves nothing for any parser pattern to anchor on, so the tool
		// reports a clean bill of health while the oracle (go test's exit
		// code) says the package failed.
		fixture := t.TempDir()
		writeFile(t, fixture, "go.mod", "module fixture.example/x\n\ngo 1.21\n")
		writeFile(t, fixture, "silent_test.go", strings.Join([]string{
			"package x",
			"",
			"import \"testing\"",
			"",
			"func TestSilent(t *testing.T) {",
			"\tt.Fail()",
			"}",
			"",
		}, "\n"))

		out, passed := runGoTest(t, fixture, "./...")
		require.False(t, passed, "oracle: go test must exit non-zero")

		raw, err := json.Marshal(testInput{Output: out})
		require.NoError(t, err)
		res, rerr := runTest(t.Context(), raw)
		require.NoError(t, rerr)
		assert.Equal(t, "No test failures detected in output", res.Content)
		assert.Equal(t, "0 failures", res.Detail)
	})
}

// oracle: hand-written golden literal — pytest FAILED lines and a cargo
// panic section are pinned against fixture files whose expected rendering
// (>> frame, context windows, the line-0 window quirk, and the cargo column
// captured as the failure text) was derived by hand.
func TestRunTest_PytestCargoGolden_Differential(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "test_main.py", strings.Join([]string{
		"def test_login():",
		"    resp = call()",
		"    assert resp.ok",
		"    # pad",
		"    # pad",
	}, "\n"))

	var lib []string
	for i := 1; i <= 45; i++ {
		if i == 42 {
			lib = append(lib, "    assert_eq!(left, right);")
		} else {
			lib = append(lib, fmt.Sprintf("// padding line %d", i))
		}
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	writeFile(t, root, filepath.Join("src", "lib.rs"), strings.Join(lib, "\n")+"\n")

	output := strings.Join([]string{
		"================= test session starts =================",
		"FAILED test_main.py::test_login - AssertionError: expected 200",
		"================== 1 failed in 0.01s ==================",
		"",
		"running 1 test",
		"---- test_basic stdout ----",
		"thread 'test_basic' panicked at 'assertion failed', src/lib.rs:42:9",
		"note: run with RUST_BACKTRACE=1 for more information",
		"",
	}, "\n")

	raw, err := json.Marshal(testInput{Output: output})
	require.NoError(t, err)
	res, rerr := runTest(t.Context(), raw)
	require.NoError(t, rerr)

	want := strings.Join([]string{
		"// FAILED: test_main.py::test_login — AssertionError: expected 200",
		// pytest failures carry no line: the window falls back to lines 1-3
		// with no >> marker.
		"@file test_main.py",
		"test_main.py:     1|def test_login():",
		"test_main.py:     2|    resp = call()",
		"test_main.py:     3|    assert resp.ok",
		// The cargo pattern captures the column as the failure text.
		"// FAIL: test_basic — 9",
		"@file src/lib.rs",
		"src/lib.rs:    39|// padding line 39",
		"src/lib.rs:    40|// padding line 40",
		"src/lib.rs:    41|// padding line 41",
		"src/lib.rs:>>  42|    assert_eq!(left, right);",
		"src/lib.rs:    43|// padding line 43",
		"src/lib.rs:    44|// padding line 44",
		"src/lib.rs:    45|// padding line 45",
	}, "\n")
	assert.Equal(t, want, res.Content)
	assert.Equal(t, "2 failures", res.Detail)
	assert.Equal(t, res.Content, res.Output)
}

// naiveTestDetail re-derives DetailFromArgs independently: a map-based
// encoding/json decode, TrimSpace, the 40-byte cut, and the "test" prefix.
func naiveTestDetail(input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	s, _ := m["output"].(string)
	s = strings.TrimSpace(s)
	if len(s) > 40 {
		s = s[:40] + "..."
	}
	return "test " + s
}

// oracle: naive reference implementation in this test — a map-based JSON
// decode plus hand-rolled truncation, compared over normal, edge, and
// hostile inputs.
func TestDetailFromArgs_Differential(t *testing.T) {
	long := strings.Repeat("FAIL", 15)
	unicode := strings.Repeat("テスト失敗", 10)

	cases := []struct {
		name  string
		input string
	}{
		{"output plain", `{"output":"--- FAIL: TestMain"}`},
		{"output padded", `{"output":"  --- FAIL: TestMain  "}`},
		{"output long", `{"output":"` + long + `"}`},
		{"output multibyte", `{"output":"` + unicode + `"}`},
		{"empty output", `{"output":""}`},
		{"missing output", `{}`},
		{"non-string output", `{"output": []}`},
		{"not json", `nope`},
	}
	tool := TestTool()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, naiveTestDetail(json.RawMessage(tc.input)), tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}
