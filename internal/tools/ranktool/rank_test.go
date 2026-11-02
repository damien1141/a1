package ranktool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRankTool_Definition(t *testing.T) {
	tool := RankTool()
	assert.Equal(t, "rank", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "importance")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunRank_Basic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	writeFile(t, root, "helper.go", "package main\n")
	writeFile(t, root, "main_test.go", "package main\n")
	writeFile(t, root, "README.md", "# title\n")

	raw, _ := json.Marshal(rankInput{Path: root, Glob: "*", Limit: 10})
	out, err := runRank(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.go")
	assert.Contains(t, out.Content, "helper.go")
	assert.Contains(t, out.Content, "README.md")
}

func TestRunRank_TestPenalty(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	writeFile(t, root, "main_test.go", "package main\n")

	raw, _ := json.Marshal(rankInput{Path: root, Glob: "*", Limit: 10})
	out, err := runRank(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(out.Content, "\n")
	var mainScore, testScore float64
	for _, line := range lines {
		if strings.Contains(line, "main.go") && !strings.Contains(line, "test") {
			parts := strings.Split(line, "\t")
			mainScore = parseScore(parts[0])
		}
		if strings.Contains(line, "main_test.go") {
			parts := strings.Split(line, "\t")
			testScore = parseScore(parts[0])
		}
	}
	assert.Greater(t, mainScore, testScore, "non-test file should rank higher than test file")
}

func TestRunRank_SkipsHidden(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	os.Mkdir(filepath.Join(root, ".git"), 0o755)
	writeFile(t, filepath.Join(root, ".git"), "config", "")

	raw, _ := json.Marshal(rankInput{Path: root, Glob: "*", Limit: 10})
	out, err := runRank(t.Context(), raw)
	require.NoError(t, err)
	assert.NotContains(t, out.Content, ".git")
}

func TestRunRank_Empty(t *testing.T) {
	root := t.TempDir()
	raw, _ := json.Marshal(rankInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runRank(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No files matched", out.Content)
}

func TestRunRank_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, root, fmt.Sprintf("file%d.go", i), "package main\n")
	}
	raw, _ := json.Marshal(rankInput{Path: root, Glob: "*.go", Limit: 3})
	out, err := runRank(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 3, "should respect limit")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func parseScore(s string) float64 {
	var f float64
	_, _ = fmt.Sscanf(s, "%f", &f)
	return f
}
