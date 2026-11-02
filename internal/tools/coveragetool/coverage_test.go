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

        // coverprofile layout: file:startL.col,endL.col numStmt count
        // main.go: block 1 numStmt=1 count=5 (covered), block 2 numStmt=1 count=3 (covered)
        //   covered = 1+1 = 2, total = 1+1 = 2 → 100%
        // other.go: block 1 numStmt=1 count=0 (uncovered), block 2 numStmt=1 count=2 (covered)
        //   covered = 0+1 = 1, total = 1+1 = 2 → 50%
        assert.Contains(t, results[0].path, "other.go")
        assert.InDelta(t, 50.0, results[0].percent, 0.1)
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
        // high.go: 1+1=2 covered out of 1+1=2 total = 100%
        // low.go: 0 covered out of 1 total = 0%
        // med.go: 1 covered out of 1+1=2 total = 50%
        assert.Contains(t, results[0].path, "low.go")
        assert.InDelta(t, 0.0, results[0].percent, 0.1)
        assert.Contains(t, results[1].path, "med.go")
        assert.InDelta(t, 50.0, results[1].percent, 0.1)
        assert.Contains(t, results[2].path, "high.go")
        assert.InDelta(t, 100.0, results[2].percent, 0.1)
}

// TestParseCoverageProfile_WeightByNumStmt exercises the specific bug where the
// old code used end-line (m[4]) instead of numStmt (m[6]) as the block weight.
// A two-block file where one block spans many lines but has few statements must
// not skew the percentage toward whichever block happened to be longer.
func TestParseCoverageProfile_WeightByNumStmt(t *testing.T) {
        // Block A: spans 50 lines but contains 1 statement, covered (count=1).
        // Block B: spans 2 lines but contains 10 statements, uncovered (count=0).
        // If we (wrongly) weighted by end-line: covered=50, total=52 → 96.2%.
        // If we (correctly) weight by numStmt: covered=1, total=11 → 9.1%.
        profile := `mode: set
github.com/example/skew.go:10.2,60.1 1 1
github.com/example/skew.go:61.2,62.1 10 0
`

        tmpFile, err := os.CreateTemp("", "coverage-*.out")
        require.NoError(t, err)
        defer os.Remove(tmpFile.Name())

        _, err = tmpFile.WriteString(profile)
        require.NoError(t, err)
        tmpFile.Close()

        results, err := parseCoverageProfile(tmpFile.Name(), "/tmp", 10)
        require.NoError(t, err)
        require.Len(t, results, 1)
        assert.Contains(t, results[0].path, "skew.go")
        assert.InDelta(t, 9.09, results[0].percent, 0.1,
                "must weight blocks by numStmt (m[6]) — end-line weight would report ~96%%")
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
