package webserver

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/session"
)

func TestTranscriptFromSnapshotProjectsRoles(t *testing.T) {
	snap := session.Snapshot{
		Messages: []session.Message{
			{Role: session.RoleUser, Text: "fix the flake"},
			{Role: session.RoleAssistant, Text: "", Content: []session.ContentBlock{
				{Type: session.BlockThinking, Text: "looking at the test"},
				{Type: session.BlockToolUse, ID: "t1", Name: "bash"},
				{Type: session.BlockText, Text: "**fixed** — see `store.go`"},
			}},
			{Role: session.RoleCompaction, TokensBefore: 12345},
		},
		Tools: map[string]session.ToolRun{
			"t1": {ToolUseID: "t1", Name: "bash", Status: session.ToolDone, Detail: "go test ./..."},
		},
	}

	resp := transcriptFromSnapshot("sess-1", snap)
	require.Equal(t, "sess-1", resp.SessionID)
	require.Len(t, resp.Messages, 3)

	assert.Equal(t, "user", resp.Messages[0].Role)
	assert.Equal(t, "fix the flake", resp.Messages[0].Text)

	a := resp.Messages[1]
	assert.Equal(t, "assistant", a.Role)
	assert.Equal(t, "looking at the test", a.Reasoning)
	assert.Equal(t, "**fixed** — see `store.go`", a.Text)
	require.Len(t, a.Tools, 1)
	assert.Equal(t, "bash", a.Tools[0].Name)
	assert.Equal(t, "done", a.Tools[0].Status)
	assert.Equal(t, "go test ./...", a.Tools[0].Detail)

	// Compaction cuts are metadata, not turns — they ride as system rows so
	// the client can render them as dividers.
	assert.Equal(t, "system", resp.Messages[2].Role)
	assert.Contains(t, resp.Messages[2].Text, "12345")

	// Wire names must match the client's ToolRun shape (camelCase toolUseId).
	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"toolUseId":"t1"`)
}

func TestTranscriptFromSnapshotAttachesOrphanTools(t *testing.T) {
	// A tool run whose tool_use block was cut by compaction still belongs in
	// the trace: it lands on the last assistant bubble instead of vanishing.
	snap := session.Snapshot{
		Messages: []session.Message{
			{Role: session.RoleUser, Text: "go"},
			{Role: session.RoleAssistant, Text: "done"},
		},
		Tools: map[string]session.ToolRun{
			"orphan": {ToolUseID: "orphan", Name: "read", Status: session.ToolDone},
		},
	}

	resp := transcriptFromSnapshot("sess-1", snap)
	require.Len(t, resp.Messages, 2)
	require.Len(t, resp.Messages[1].Tools, 1)
	assert.Equal(t, "read", resp.Messages[1].Tools[0].Name)
}

func TestTranscriptFromSnapshotEmptyStaysArray(t *testing.T) {
	resp := transcriptFromSnapshot("sess-1", session.Snapshot{})
	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"messages":[]`)
}
