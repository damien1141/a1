package testimpacttool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// naivePyImports extracts imported module names with plain prefix checks on
// each line — no regex, no shared code with the graph's parsers.
func naivePyImports(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var mods []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		switch {
		case strings.HasPrefix(trimmed, "from "):
			if len(fields) >= 2 {
				mods = append(mods, fields[1])
			}
		case strings.HasPrefix(trimmed, "import "):
			if len(fields) >= 2 {
				mods = append(mods, strings.TrimSuffix(fields[1], ","))
			}
		}
	}
	return mods
}

// naivePyTestImporters computes the test files that import module with the
// naive extractor plus the documented Python test-file filter (test_* or
// *_test.py), in directory listing order.
func naivePyTestImporters(t *testing.T, root, module string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	var importers []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".py") {
			continue
		}
		for _, m := range naivePyImports(t, filepath.Join(root, e.Name())) {
			if m == module {
				if strings.HasPrefix(e.Name(), "test_") || strings.Contains(e.Name(), "_test.py") {
					importers = append(importers, e.Name())
				}
				break
			}
		}
	}
	return importers
}

// naiveGoTestImporters computes the test files containing the quoted Go
// import path, in directory listing order, filtered to _test.go files.
func naiveGoTestImporters(t *testing.T, root string, dirs []string, importPath string) []string {
	t.Helper()
	quoted := `"` + importPath + `"`
	var importers []string
	for _, dir := range append([]string{root}, dirs...) {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			data, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
			require.NoError(t, rerr)
			if strings.Contains(string(data), quoted) && strings.HasSuffix(e.Name(), "_test.go") {
				importers = append(importers, e.Name())
			}
		}
	}
	return importers
}

var impactFileRe = regexp.MustCompile(`(?m)^\d+\. @file (.+)$`)

func runImpactTool(t *testing.T, in testImpactInput) (content, detail string) {
	t.Helper()
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	res, rerr := runTestImpact(t.Context(), raw)
	require.NoError(t, rerr)
	return res.Content, res.Detail
}

// oracle: naive reference implementation in this test — reverse dependencies
// are recomputed with plain string ops over the fixture (line prefix checks
// for import extraction, a hand-rolled test-file filter) and compared with
// the graph-backed tool's affected test list, order included.
func TestRunTestImpact_PythonFixtureVsNaiveReverseDeps_Differential(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "auth.py", "def login(): pass\n")
	writeFile(t, root, "consumer.py", "import auth\n\n\ndef use(): auth.login()\n")
	writeFile(t, root, "test_auth.py", "import auth\n\n\ndef test_login(): assert auth.login()\n")
	writeFile(t, root, "test_auth_deep.py", "from auth import login\n\n\ndef test_deep(): login()\n")
	writeFile(t, root, "util.py", "def helper(): pass\n")
	writeFile(t, root, "test_util.py", "import util\n\n\ndef test_helper(): pass\n")

	want := naivePyTestImporters(t, root, "auth")
	require.Equal(t, []string{"test_auth.py", "test_auth_deep.py"}, want,
		"reference sanity: both auth tests, not the consumer or util tests")

	content, detail := runImpactTool(t, testImpactInput{Path: "auth.py", Root: root})
	assert.Equal(t, []string{"test_auth.py", "test_auth_deep.py"}, impactPaths(content))
	assert.Equal(t, "2 test files affected by auth.py", detail)
	assert.Contains(t, content, "Test files affected by changes to auth.py")
}

func impactPaths(content string) []string {
	var out []string
	for _, m := range impactFileRe.FindAllStringSubmatch(content, -1) {
		out = append(out, m[1])
	}
	return out
}

// oracle: hand-written golden literal — the tool follows direct importers
// only (one hop): a chain base ← mid ← test_mid yields no test files for a
// change to base, even though a test transitively depends on it.
func TestRunTestImpact_OneHopOnlyVsGolden_Differential(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "base.py", "def core(): pass\n")
	writeFile(t, root, "mid.py", "import base\n\n\ndef wrap(): base.core()\n")
	writeFile(t, root, "test_mid.py", "import mid\n\n\ndef test_wrap(): mid.wrap()\n")

	// Reference: direct importers of "base" are [mid.py]; none is a test.
	direct := naivePyTestImporters(t, root, "base")
	require.Empty(t, direct, "reference sanity: only mid.py imports base directly")

	content, detail := runImpactTool(t, testImpactInput{Path: "base.py", Root: root})
	assert.Equal(t, "No test files import base.py", content)
	assert.Equal(t, "0 tests", detail)
}

// oracle: naive reference implementation in this test — Go fixtures are
// scanned for the quoted import path with strings.Contains and filtered to
// _test.go, then compared with the tool's output for both the import-path
// query form (which resolves through the graph's raw import keys) and the
// file-path form (which the raw keying cannot resolve — pinned as the
// documented current limitation).
func TestRunTestImpact_GoFixtureVsNaiveReverseDeps_Differential(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.mod", "module example.com/mod\n\ngo 1.26\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "auth"), 0o755))
	writeFile(t, root, filepath.Join("auth", "auth.go"), "package auth\n\nfunc Login() {}\n")
	writeFile(t, root, filepath.Join("auth", "auth_test.go"),
		"package auth\n\nimport (\n\t\"testing\"\n\n\t\"example.com/mod/auth\"\n)\n\nfunc TestLogin(t *testing.T) { auth.Login() }\n")
	writeFile(t, root, "main.go", "package main\n\nimport \"example.com/mod/auth\"\n\nfunc main() { auth.Login() }\n")

	t.Run("import-path query", func(t *testing.T) {
		want := naiveGoTestImporters(t, root, []string{filepath.Join(root, "auth")}, "example.com/mod/auth")
		require.Equal(t, []string{"auth_test.go"}, want, "reference sanity: only the _test.go file imports it")

		content, detail := runImpactTool(t, testImpactInput{Path: "example.com/mod/auth", Root: root})
		assert.Equal(t, []string{filepath.Join("auth", "auth_test.go")}, impactPaths(content))
		assert.Equal(t, "1 test files affected by example.com/mod/auth", detail)
		// The suggested command uses the first test file's directory.
		assert.Contains(t, content, "go test ./... -run ./auth")
	})

	t.Run("file-path query misses module-path imports", func(t *testing.T) {
		// The graph keys reverse edges by the raw import string, so a
		// file-path query cannot reach "example.com/mod/auth" importers —
		// the naive reference agrees: no import is keyed "auth/auth.go".
		content, detail := runImpactTool(t, testImpactInput{Path: filepath.Join("auth", "auth.go"), Root: root})
		assert.Equal(t, "No files import "+filepath.Join("auth", "auth.go"), content)
		assert.Equal(t, "0 tests", detail)
	})
}

// naiveImpactDetail re-derives DetailFromArgs independently: a map-based
// encoding/json decode, TrimSpace, and the empty-path fallback.
func naiveImpactDetail(input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	p, _ := m["path"].(string)
	p = strings.TrimSpace(p)
	if p == "" {
		return "testimpact"
	}
	return "testimpact " + p
}

// oracle: naive reference implementation in this test — a map-based JSON
// decode plus hand-rolled formatting, compared over normal, edge, and
// hostile inputs.
func TestDetailFromArgs_Differential(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"path plain", `{"path":"internal/auth/auth.go"}`},
		{"path padded", `{"path":"  internal/auth/auth.go  "}`},
		{"empty path", `{"path":""}`},
		{"blank path", `{"path":"   "}`},
		{"missing path", `{}`},
		{"non-string path", `{"path": true}`},
		{"not json", `nope`},
	}
	tool := TestImpactTool()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, naiveImpactDetail(json.RawMessage(tc.input)), tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}
