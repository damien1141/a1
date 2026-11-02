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

// TestRunJournal_RecordAndRead is the regression test for #8: nothing in the
// codebase wrote .journal, so the read always returned "No journal entries
// found." The journal tool now accepts an `action` payload and appends a
// timestamped entry to .journal in the target directory.
func TestRunJournal_RecordAndRead(t *testing.T) {
        root := t.TempDir()
        writeFile(t, root, "main.go", "package main\n")

        // Record an entry. Use action+file+line+outcome.
        raw, _ := json.Marshal(journalInput{
                Path:    root,
                Action:  "edit",
                File:    filepath.Join(root, "main.go"),
                Line:    42,
                Outcome: "success",
        })
        out, err := runJournal(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "edit", "recorded entry should echo back the action")
        assert.Contains(t, out.Content, "success", "recorded entry should echo back the outcome")
        assert.Contains(t, out.Content, "main.go", "recorded entry should reference the file")

        // The .journal file must exist on disk now — that's the whole point.
        info, err := os.Stat(filepath.Join(root, ".journal"))
        require.NoError(t, err, ".journal file must be created on record")
        assert.Greater(t, info.Size(), int64(0), ".journal file must be non-empty")

        // A subsequent read must return the entry we just wrote — proving the
        // writer and reader agree on the line format.
        raw, _ = json.Marshal(journalInput{Path: root, Limit: 10})
        out, err = runJournal(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "edit")
        assert.Contains(t, out.Content, "main.go")
        assert.Contains(t, out.Content, "success")
}

func TestRunJournal_RecordAppendsNotOverwrites(t *testing.T) {
        root := t.TempDir()
        // Seed .journal with one pre-existing entry.
        writeFile(t, root, ".journal", "2024-01-15T10:30:00Z|edit|seed.go|1|success\n")

        raw, _ := json.Marshal(journalInput{
                Path:    root,
                Action:  "read",
                File:    filepath.Join(root, "seed.go"),
                Line:    5,
                Outcome: "success",
        })
        _, err := runJournal(t.Context(), raw)
        require.NoError(t, err)

        // Read back: both entries must be present (seed + new).
        raw, _ = json.Marshal(journalInput{Path: root, Limit: 10})
        out, err := runJournal(t.Context(), raw)
        require.NoError(t, err)
        lines := strings.Split(strings.TrimSpace(out.Content), "\n")
        assert.Len(t, lines, 2, "record must append, not overwrite")
}

func writeFile(t *testing.T, dir, name, content string) {
        t.Helper()
        require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
