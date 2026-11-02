package readtool

import (
        "encoding/json"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

func TestRunRead_RelativeHeader(t *testing.T) {
        root := t.TempDir()
        require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
        require.NoError(t, os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n"), 0o644))
        t.Chdir(root)

        raw, err := json.Marshal(readInput{Path: "src/main.go"})
        require.NoError(t, err)
        out, err := runRead(t.Context(), raw)
        require.NoError(t, err)
        assert.True(t, strings.HasPrefix(out.Content, "@file src/main.go\n"))
        assert.Equal(t, "src/main.go", out.Detail)
}

// === mode dispatch (Tier A.3) ===

func TestReadTool_SchemaHasModeEnum(t *testing.T) {
        tool := ReadTool()
        modeParam, ok := tool.Definition.Params.Properties["mode"].(map[string]any)
        require.True(t, ok, "mode param should be present")
        enum, _ := modeParam["enum"].([]string)
        assert.Equal(t, []string{"file", "ls", "watch"}, enum)
}

func TestRunRead_DefaultModeIsFile(t *testing.T) {
        // No mode field — defaults to "file" (backward compat).
        root := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(root, "x.txt"), []byte("hello\n"), 0o644))
        t.Chdir(root)

        tool := ReadTool()
        b, _ := json.Marshal(map[string]any{"path": "x.txt"})
        out, err := tool.Run(t.Context(), b)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "@file x.txt")
        assert.Contains(t, out.Content, "hello")
}

func TestRunRead_ModeFileExplicit(t *testing.T) {
        root := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(root, "x.txt"), []byte("explicit\n"), 0o644))
        t.Chdir(root)

        tool := ReadTool()
        b, _ := json.Marshal(map[string]any{"mode": "file", "path": "x.txt"})
        out, err := tool.Run(t.Context(), b)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "@file x.txt")
        assert.Contains(t, out.Content, "explicit")
}

func TestRunRead_ModeLsDispatchesToLsTool(t *testing.T) {
        // mode=ls should route through lstool.LsTool().Run — output
        // should be a directory listing, not the "@file" file header.
        root := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644))
        require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
        t.Chdir(root)

        tool := ReadTool()
        b, _ := json.Marshal(map[string]any{"mode": "ls", "path": "."})
        out, err := tool.Run(t.Context(), b)
        require.NoError(t, err)
        // ls output mentions the entries, not the "@file" file header.
        assert.Contains(t, out.Content, "a.txt")
        assert.Contains(t, out.Content, "sub")
        assert.NotContains(t, out.Content, "@file")
}

func TestRunRead_UnknownModeErrors(t *testing.T) {
        tool := ReadTool()
        b, _ := json.Marshal(map[string]any{"mode": "stat", "path": "."})
        _, err := tool.Run(t.Context(), b)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "unknown read mode")
        assert.Contains(t, err.Error(), "stat")
}
