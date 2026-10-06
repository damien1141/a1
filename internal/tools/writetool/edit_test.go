package writetool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunEditReplacesText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	original := "alpha\nbeta\ngamma"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

	tool := EditTool()
	raw, err := json.Marshal(editInput{
		Path:   path,
		OldStr: "beta",
		NewStr: "BETA",
	})
	require.NoError(t, err)

	res, err := tool.Run(t.Context(), raw)
	require.NoError(t, err)
	require.Contains(t, res.Content, "@file ")

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "alpha\nBETA\ngamma", string(got))
}

func TestRunEditFailsOnMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	require.NoError(t, os.WriteFile(path, []byte("alpha\nbeta\ngamma"), 0o644))

	tool := EditTool()
	raw, err := json.Marshal(editInput{
		Path:   path,
		OldStr: "delta",
		NewStr: "DELTA",
	})
	require.NoError(t, err)

	_, err = tool.Run(t.Context(), raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "old_str not found")
}

func TestRunEditFailsOnMultipleMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	require.NoError(t, os.WriteFile(path, []byte("alpha\nbeta\nalpha"), 0o644))

	tool := EditTool()
	raw, err := json.Marshal(editInput{
		Path:   path,
		OldStr: "alpha",
		NewStr: "ALPHA",
	})
	require.NoError(t, err)

	_, err = tool.Run(t.Context(), raw)
	require.Error(t, err)
	require.Contains(t, err.Error(), "appears 2 times")
}

func TestRunEditDeletesText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	require.NoError(t, os.WriteFile(path, []byte("alpha\nbeta\ngamma"), 0o644))

	tool := EditTool()
	raw, err := json.Marshal(editInput{
		Path:   path,
		OldStr: "beta\n",
		NewStr: "",
	})
	require.NoError(t, err)

	_, err = tool.Run(t.Context(), raw)
	require.NoError(t, err)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "alpha\ngamma", string(got))
}
