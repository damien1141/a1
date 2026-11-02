package memory

import (
        "os"
        "path/filepath"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

func TestOpenBank(t *testing.T) {
        dir := t.TempDir()
        bank, err := OpenBank("sess-1", dir)
        require.NoError(t, err)
        require.NotNil(t, bank)
        assert.Equal(t, filepath.Join(dir, "sess-1.jsonl"), bank.Path())
}

func TestOpenBankEmptySessionID(t *testing.T) {
        bank, err := OpenBank("", t.TempDir())
        assert.Error(t, err)
        assert.Nil(t, bank)
}

func TestAppendAndRecent(t *testing.T) {
        dir := t.TempDir()
        bank, err := OpenBank("sess-1", dir)
        require.NoError(t, err)

        require.NoError(t, bank.Append(EntryContext, "first"))
        require.NoError(t, bank.Append(EntryError, "oops"))
        require.NoError(t, bank.Append(EntryDecision, "chose tool X"))

        recent, err := bank.Recent(2)
        require.NoError(t, err)
        require.Len(t, recent, 2)
        assert.Equal(t, EntryDecision, recent[0].Type)
        assert.Equal(t, EntryError, recent[1].Type)
        assert.Equal(t, "sess-1", recent[0].SessionID)
}

func TestRecentClampsToAvailable(t *testing.T) {
        dir := t.TempDir()
        bank, err := OpenBank("sess-1", dir)
        require.NoError(t, err)

        require.NoError(t, bank.Append(EntryContext, "only one"))

        recent, err := bank.Recent(10)
        require.NoError(t, err)
        require.Len(t, recent, 1)
}

func TestPersistenceAcrossReopen(t *testing.T) {
        dir := t.TempDir()
        path := filepath.Join(dir, "sess-1.jsonl")

        bank, err := OpenBank("sess-1", dir)
        require.NoError(t, err)
        require.NoError(t, bank.Append(EntryContext, "persist me"))

        bank2, err := OpenBank("sess-1", dir)
        require.NoError(t, err)
        recent, err := bank2.Recent(10)
        require.NoError(t, err)
        require.Len(t, recent, 1)
        assert.Equal(t, "persist me", recent[0].Content)

        // Verify file was actually written.
        _, statErr := os.Stat(path)
        assert.NoError(t, statErr)
}

func TestLen(t *testing.T) {
        dir := t.TempDir()
        bank, err := OpenBank("sess-1", dir)
        require.NoError(t, err)

        assert.Equal(t, 0, bank.Len())
        require.NoError(t, bank.Append(EntryContext, "a"))
        require.NoError(t, bank.Append(EntryContext, "b"))
        assert.Equal(t, 2, bank.Len())
}

func TestOpenBankDefaultDir(t *testing.T) {
        // The default dir resolves through os.UserHomeDir at call time, so pin
        // HOME to a throwaway dir — otherwise this opens (and creates) a bank
        // under the developer's real ~/.a1/sessions/memory.
        home := t.TempDir()
        t.Setenv("HOME", home)
        t.Setenv("USERPROFILE", home)

        // Should not panic even when default dir creation is needed.
        bank, err := OpenBank("sess-default", "")
        require.NoError(t, err)
        require.NotNil(t, bank)
        assert.Contains(
                t,
                bank.Path(),
                "sessions"+string(filepath.Separator)+"memory"+string(filepath.Separator)+"sess-default.jsonl",
        )
}

func TestMemoryEntryTimestamp(t *testing.T) {
        before := time.Now().UTC()
        dir := t.TempDir()
        bank, err := OpenBank("ts-sess", dir)
        require.NoError(t, err)
        require.NoError(t, bank.Append(EntryContext, "time check"))
        after := time.Now().UTC()

        recent, err := bank.Recent(1)
        require.NoError(t, err)
        require.Len(t, recent, 1)
        assert.False(t, recent[0].Timestamp.Before(before))
        assert.False(t, recent[0].Timestamp.After(after))
}

func TestIsTraceShape(t *testing.T) {
        assert.True(t, IsTrace(Entry{Type: EntryTrace, Content: "bash(ls) => files"}))
        // Legacy banks stored executor appends as plain context entries in the
        // "name(args) => result" shape; they must still classify as traces.
        assert.True(t, IsTrace(Entry{Type: EntryContext, Content: "read(main.go) => package a1"}))
        assert.False(t, IsTrace(Entry{Type: EntryContext, Content: "user prefers concise answers"}))
        assert.False(t, IsTrace(Entry{Type: EntryError, Content: "bash(ls) failed: no such file"}))
        assert.False(t, IsTrace(Entry{Type: EntryMemory, Content: "deploy goes out Thursday"}))
}

func TestMemoryCountAndRecentMemoriesSkipTraces(t *testing.T) {
        dir := t.TempDir()
        bank, err := OpenBank("persona:test", dir)
        require.NoError(t, err)

        // A realistic mix: tool spam between the facts that matter.
        require.NoError(t, bank.Append(EntryContext, "bash(go test ./...) => ok"))
        require.NoError(t, bank.Append(EntryDecision, "keep the TUI as reference"))
        require.NoError(t, bank.Append(EntryContext, "read(main.go) => package main"))
        require.NoError(t, bank.Append(EntryError, "bash(make lint) failed: 1 issue"))
        require.NoError(t, bank.Append(EntryMemory, "user's timezone is UTC"))

        assert.Equal(t, 3, bank.MemoryCount())

        recent, err := bank.RecentMemories(10)
        require.NoError(t, err)
        require.Len(t, recent, 3)
        // Newest-first, traces gone.
        assert.Equal(t, EntryMemory, recent[0].Type)
        assert.Equal(t, EntryError, recent[1].Type)
        assert.Equal(t, EntryDecision, recent[2].Type)
}

func TestListMemoriesIndexedKeepsRawIndices(t *testing.T) {
        dir := t.TempDir()
        bank, err := OpenBank("persona:test", dir)
        require.NoError(t, err)

        require.NoError(t, bank.Append(EntryTrace, "bash(ls) => ."))
        require.NoError(t, bank.Append(EntryMemory, "fact one"))
        require.NoError(t, bank.Append(EntryContext, "read(x) => y"))
        require.NoError(t, bank.Append(EntryMemory, "fact two"))

        indexed, err := bank.ListMemoriesIndexed()
        require.NoError(t, err)
        require.Len(t, indexed, 2)
        // Indices point into the FULL bank so DeleteAt(round-tripped id) drops
        // the right line — a filtered position would forget the wrong memory.
        assert.Equal(t, 1, indexed[0].Index)
        assert.Equal(t, "fact one", indexed[0].Entry.Content)
        assert.Equal(t, 3, indexed[1].Index)
        assert.Equal(t, "fact two", indexed[1].Entry.Content)

        require.NoError(t, bank.DeleteAt(indexed[1].Index))
        assert.Equal(t, 1, bank.MemoryCount())
}
