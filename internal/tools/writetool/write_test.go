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

// === mode dispatch ===

func TestRunWrite_DefaultModeIsWrite(t *testing.T) {
        // No mode field — defaults to "write" (backward compat with old
        // callers that only sent path + content).
        dir := t.TempDir()
        path := filepath.Join(dir, "out.txt")
        tool := WriteTool()

        _, err := tool.Run(t.Context(), mustWriteArgs(t, path, "hello\n"))
        require.NoError(t, err)
        got, err := os.ReadFile(path)
        require.NoError(t, err)
        require.Equal(t, "hello\n", string(got))
}

func TestRunWrite_ModeWriteExplicit(t *testing.T) {
        dir := t.TempDir()
        path := filepath.Join(dir, "out.txt")
        tool := WriteTool()

        b, _ := json.Marshal(map[string]any{"mode": "write", "path": path, "content": "explicit\n"})
        _, err := tool.Run(t.Context(), b)
        require.NoError(t, err)
        got, err := os.ReadFile(path)
        require.NoError(t, err)
        require.Equal(t, "explicit\n", string(got))
}

func TestRunWrite_ModeEditDispatchesToEdit(t *testing.T) {
        // mode=edit should route through the edit handler — it expects
        // old_str + new_str, replaces in place, and returns "Updated".
        dir := t.TempDir()
        path := filepath.Join(dir, "out.txt")
        require.NoError(t, os.WriteFile(path, []byte("line one\nline two\nline three\n"), 0o644))

        tool := WriteTool()
        b, _ := json.Marshal(map[string]any{
                "mode":    "edit",
                "path":    path,
                "old_str": "line two",
                "new_str": "line TWO",
        })
        out, err := tool.Run(t.Context(), b)
        require.NoError(t, err)
        require.Contains(t, out.Content, "Updated")
        got, err := os.ReadFile(path)
        require.NoError(t, err)
        require.Equal(t, "line one\nline TWO\nline three\n", string(got))
}

func TestRunWrite_UnknownModeErrors(t *testing.T) {
        tool := WriteTool()
        b, _ := json.Marshal(map[string]any{"mode": "append", "path": "/tmp/x", "content": "x"})
        _, err := tool.Run(t.Context(), b)
        require.Error(t, err)
        require.Contains(t, err.Error(), "unknown write mode")
        require.Contains(t, err.Error(), "append")
}

func TestWriteTool_SchemaHasModeEnum(t *testing.T) {
        tool := WriteTool()
        modeParam, ok := tool.Definition.Params.Properties["mode"].(map[string]any)
        require.True(t, ok, "mode param should be present")
        enum, _ := modeParam["enum"].([]string)
        require.Equal(t, []string{"write", "edit"}, enum)
}
