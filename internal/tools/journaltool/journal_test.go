package journaltool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJournalTool_Definition(t *testing.T) {
	tool := JournalTool()
	assert.Equal(t, "journal", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "action")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunJournal_NoJournalFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")

	raw, _ := json.Marshal(journalInput{Path: root, Limit: 10})
	out, err := runJournal(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No journal entries found", out.Content)
}

func TestRunJournal_ReadsJournalFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".journal", `2024-01-15T10:30:00Z|edit|main.go|42|success
2024-01-15T10:31:00Z|read|helper.go|10|success
`)
	writeFile(t, root, "main.go", "package main\n")

	raw, _ := json.Marshal(journalInput{Path: root, Limit: 10})
	out, err := runJournal(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "edit")
	assert.Contains(t, out.Content, "read")
	assert.Contains(t, out.Content, "main.go")
	assert.Contains(t, out.Content, "helper.go")
}

func TestRunJournal_Limit(t *testing.T) {
	root := t.TempDir()
	var lines []string
	for i := 0; i < 5; i++ {
		ts := time.Now().Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		lines = append(lines, fmt.Sprintf("%s|action%d|file%d.go|%d|success", ts, i, i, i))
	}
	writeFile(t, root, ".journal", strings.Join(lines, "\n")+"\n")
	writeFile(t, root, "main.go", "package main\n")

	raw, _ := json.Marshal(journalInput{Path: root, Limit: 3})
	out, err := runJournal(t.Context(), raw)
	require.NoError(t, err)
	linesOut := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, linesOut, 3, "should respect limit")
}

func TestRunJournal_EmptyPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(journalInput{Path: root, Limit: 10})
	out, err := runJournal(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No journal entries found", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
