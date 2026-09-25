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
	// Should not panic even when default dir creation is needed.
	bank, err := OpenBank("sess-default", "")
	require.NoError(t, err)
	require.NotNil(t, bank)
	assert.Contains(t, bank.Path(), "sessions"+string(filepath.Separator)+"memory"+string(filepath.Separator)+"sess-default.jsonl")
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
