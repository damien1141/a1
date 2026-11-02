package permission

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractBash(t *testing.T) {
	req, err := Extract("bash", json.RawMessage(`{"command":"git status"}`))
	require.NoError(t, err)
	require.Equal(t, ActionBash, req.Action)
	require.Equal(t, "git status", req.Command)
}

func TestExtractWritePath(t *testing.T) {
	req, err := Extract("write", json.RawMessage(`{"path":"out.txt","content":"x"}`))
	require.NoError(t, err)
	require.Equal(t, ActionWrite, req.Action)
	require.Len(t, req.Paths, 1)
	require.True(t, filepath.IsAbs(req.Paths[0]))
}

func TestExtractAtUsesExplicitCwd(t *testing.T) {
	root := t.TempDir()
	req, err := ExtractAt("write", json.RawMessage(`{"path":"out.txt","content":"x"}`), root)
	require.NoError(t, err)
	want := filepath.Join(root, "out.txt")
	require.Len(t, req.Paths, 1)
	require.Equal(t, want, req.Paths[0])
}

func TestExtractEditFilePath(t *testing.T) {
	req, err := Extract("edit", json.RawMessage(`{"file_path":"a.go","edits":[]}`))
	require.NoError(t, err)
	require.Equal(t, ActionEdit, req.Action)
	require.Len(t, req.Paths, 1)
}
