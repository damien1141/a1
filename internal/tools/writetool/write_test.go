package writetool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunWriteCreatesAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "out.txt")
	tool := WriteTool()

	created, err := tool.Run(t.Context(), mustWriteArgs(t, path, "first\n"))
	require.NoError(t, err)
	require.Contains(t, created.Content, "Created")
	require.Contains(t, created.Content, "+first", "a new file shows its content as an addition")
	require.NotContains(t, created.Content, "@file ", "no stale hash anchor")
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "first\n", string(got))

	overwritten, err := tool.Run(t.Context(), mustWriteArgs(t, path, "second\n"))
	require.NoError(t, err)
	require.Contains(t, overwritten.Content, "Updated")
	require.Contains(t, overwritten.Content, "-first", "an overwrite shows what changed")
	require.Contains(t, overwritten.Content, "+second")
	require.NotContains(t, overwritten.Content, "@file ", "no stale hash anchor")
	got, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "second\n", string(got))
}

func TestRunWriteRelativePathInResult(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tool := WriteTool()

	created, err := tool.Run(t.Context(), mustWriteArgs(t, "nested/out.txt", "first\n"))
	require.NoError(t, err)
	require.Contains(t, created.Content, "Created")
	require.Equal(t, "nested/out.txt", created.Detail)
}

func TestRunWriteRequiresPath(t *testing.T) {
	tool := WriteTool()
	_, err := tool.Run(t.Context(), []byte(`{"path":"","content":"x"}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "path is required")
}

func mustWriteArgs(t *testing.T, path, content string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{"path": path, "content": content})
	require.NoError(t, err)
	return b
}
