package transcript_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/transcript"
)

const editOutput = `@file internal/foo.go#A1B2

--- a/internal/foo.go
+++ b/internal/foo.go
@@ -1,4 +1,5 @@
 package foo
+
+// Bar does nothing.
 func Bar() {}
`

func editSnapshot(name, status string) session.Snapshot {
	return session.Snapshot{
		Messages: []session.Message{{
			ID:    "m1",
			Role:  session.RoleAssistant,
			State: session.StateComplete,
			Content: []session.ContentBlock{{
				Type:     session.BlockToolUse,
				ID:       "call_edit",
				Name:     name,
				Input:    `{"path":"internal/foo.go"}`,
				Complete: true,
			}},
		}},
		Tools: map[string]session.ToolRun{
			"call_edit": {
				ToolUseID: "call_edit",
				Name:      name,
				Status:    session.ParseToolStatus(status),
				Detail:    "internal/foo.go",
				Output:    editOutput,
			},
		},
	}
}

func TestMapperRoutesEditToolsToDiffBlock(t *testing.T) {
	for _, name := range []string{"write", "edit"} {
		t.Run(name, func(t *testing.T) {
			m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
			entries, _, _ := m.Sync(nil, nil, editSnapshot(name, "done"))
			require.Len(t, entries, 1)
			d, ok := entries[0].(*block.DiffBlock)
			require.True(t, ok, "expected *block.DiffBlock, got %T", entries[0])
			assert.Equal(t, "internal/foo.go", d.Path)
			assert.Contains(t, d.Diff, "+// Bar does nothing.", "the diff body is carried through")
			added, removed := d.Stat()
			assert.Equal(t, 2, added)
			assert.Equal(t, 0, removed)
		})
	}
}

// Tools that are neither edits, readers, nor bash keep the generic row.
// (bash has its own BashBlock, so it is not the fallthrough case.)
func TestMapperKeepsGenericToolBlockForOtherTools(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	snap := editSnapshot("vector_search", "done")
	snap.Tools["call_edit"] = session.ToolRun{
		ToolUseID: "call_edit",
		Name:      "vector_search",
		Status:    session.ToolDone,
		Detail:    "auth flow",
		Output:    "internal/auth/auth.go:42",
	}
	entries, _, _ := m.Sync(nil, nil, snap)
	require.Len(t, entries, 1)
	tb, ok := entries[0].(*block.ToolBlock)
	require.True(t, ok, "vector_search stays a generic tool row, got %T", entries[0])
	assert.Equal(t, "vector_search", tb.Name)
}

func TestMapperDiffBlockStartsCollapsed(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	entries, _, _ := m.Sync(nil, nil, editSnapshot("edit", "done"))
	d := entries[0].(*block.DiffBlock)
	assert.False(t, d.Expanded, "the diff is collapsed until the user expands it")
}

func TestMapperDiffBlockApprovesRejectIntoDiffBlock(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	entries, _, _ := m.Sync(nil, nil, editSnapshot("edit", "done"))

	// A rejected edit keeps the row authoritative and shows the reason.
	run := editSnapshot("edit", "rejected").Tools["call_edit"]
	run.Error = "user denied the edit"
	run.Output = ""
	snap := session.Snapshot{
		Messages: editSnapshot("edit", "rejected").Messages,
		Tools:    map[string]session.ToolRun{"call_edit": run},
	}
	entries, _, dirty := m.Sync(entries, []string{"call_edit"}, snap)
	require.NotEmpty(t, dirty, "rejecting the edit dirties the row")
	rejected, ok := entries[0].(*block.DiffBlock)
	require.True(t, ok, "widget type must survive the patch, got %T", entries[0])
	assert.Equal(t, "user denied the edit", rejected.Error)
	assert.Empty(t, rejected.Diff, "a rejected edit carries no diff")
}

func TestMapperAutoExpandOpensToolRows(t *testing.T) {
	plain := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	auto := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	auto.AutoExpand = true

	entries, _, _ := plain.Sync(nil, nil, editSnapshot("edit", "done"))
	plainCollapsed, ok := entries[0].(*block.DiffBlock)
	require.True(t, ok)
	assert.False(t, plainCollapsed.Expanded, "default config starts collapsed")

	entries, _, _ = auto.Sync(nil, nil, editSnapshot("edit", "done"))
	autoOpen, ok := entries[0].(*block.DiffBlock)
	require.True(t, ok)
	assert.True(t, autoOpen.Expanded, "auto-expand opens the diff without a keypress")
}

func TestMapperAutoExpandYieldsToUserToggle(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.AutoExpand = true

	entries, _, _ := m.Sync(nil, nil, editSnapshot("edit", "done"))
	d := entries[0].(*block.DiffBlock)
	require.True(t, d.Expanded, "auto-expand opens it first")

	// The user collapses; a later re-sync must not silently re-open it.
	d.Expanded = false
	d.OnToggle(false)
	entries, _, _ = m.Sync(entries, []string{"call_edit"}, editSnapshot("edit", "done"))
	stillCollapsed, ok := entries[0].(*block.DiffBlock)
	require.True(t, ok)
	assert.False(t, stillCollapsed.Expanded, "the user's choice beats the config default")
}

// Bash rows are closed under auto-expand, but a closed bash row still renders a
// tail of its output — closing bounds the volume, it does not hide the outcome.
func TestMapperAutoExpandBashRowShowsTailForm(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.AutoExpand = true

	snap := session.Snapshot{
		Messages: []session.Message{{
			ID: "m1", Role: session.RoleAssistant, State: session.StateComplete,
			Content: []session.ContentBlock{{
				Type: session.BlockToolUse, ID: "b1", Name: "bash",
				Input: `{"command":"go test ./..."}`, Complete: true,
			}},
		}},
		Tools: map[string]session.ToolRun{
			"b1": {
				ToolUseID: "b1", Name: "bash", Status: session.ToolDone,
				Detail: "go test ./...", Output: "ok\n",
			},
		},
	}
	entries, _, _ := m.Sync(nil, nil, snap)
	b, ok := entries[0].(*block.BashBlock)
	require.True(t, ok, "bash routes to BashBlock, got %T", entries[0])
	assert.False(t, b.Expanded, "the row is closed so a flood stays bounded")
	assert.NotEmpty(t, b.Output, "the output is still carried for the collapsed preview")
}

func readSnap() session.Snapshot {
	return session.Snapshot{
		Messages: []session.Message{{
			ID: "m1", Role: session.RoleAssistant, State: session.StateComplete,
			Content: []session.ContentBlock{{
				Type: session.BlockToolUse, ID: "call_read", Name: "read",
				Input: `{"path":"haiku.txt"}`, Complete: true,
			}},
		}},
		Tools: map[string]session.ToolRun{
			"call_read": {
				ToolUseID: "call_read", Name: "read", Status: session.ToolDone, Detail: "haiku.txt",
				Output: "@file haiku.txt\n     1\tcold bytes on the wire\n     2\ta green cursor waits alone",
			},
		},
	}
}

func TestMapperRoutesReadToolsToReadBlock(t *testing.T) {
	for _, name := range []string{"read", "grep"} {
		t.Run(name, func(t *testing.T) {
			m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
			snap := readSnap()
			run := snap.Tools["call_read"]
			run.Name = name
			snap.Tools["call_read"] = run
			entries, _, _ := m.Sync(nil, nil, snap)
			require.Len(t, entries, 1)
			r, ok := entries[0].(*block.ReadBlock)
			require.True(t, ok, "expected *block.ReadBlock, got %T", entries[0])
			assert.Equal(t, name, r.Name)
			assert.NotContains(t, r.Body, "@file ", "the @file header line is stripped")
			assert.Contains(t, r.Body, "cold bytes on the wire", "the file content survives")
		})
	}
}

// Reads are deliberately collapsed even when auto_expand_tool_output is on:
// they are the highest-volume row and the least urgent content. Writes/edits
// still open (a diff must not be missed), which is why this is a per-family
// policy and not a global one.
func TestMapperReadBlockStaysCollapsedUnderAutoExpand(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.AutoExpand = true
	entries, _, _ := m.Sync(nil, nil, readSnap())
	r := entries[0].(*block.ReadBlock)
	assert.False(t, r.Expanded, "reads stay collapsed even with auto-expand on")
	assert.NotEmpty(t, r.Body, "the content is still carried for expansion")
}

func TestMapperReadBlockCollapsedByDefault(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	entries, _, _ := m.Sync(nil, nil, readSnap())
	r := entries[0].(*block.ReadBlock)
	assert.False(t, r.Expanded, "rows start collapsed when auto-expand is off")
}

// Reads are the one family that stays collapsed even with auto-expand on:
// they are the highest-volume, least-urgent rows, and a 30-file exploration is
// 30 cards of file content otherwise. Writes/edits stay open — a diff is the
// thing you must not miss.
func TestMapperAutoExpandCollapsesReadsButOpensEdits(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.AutoExpand = true

	entries, _, _ := m.Sync(nil, nil, readSnap())
	r, ok := entries[0].(*block.ReadBlock)
	require.True(t, ok, "read routes to ReadBlock, got %T", entries[0])
	assert.False(t, r.Expanded, "auto-expand must not open a read row")

	entries, _, _ = m.Sync(nil, nil, editSnapshot("edit", "done"))
	d, ok := entries[0].(*block.DiffBlock)
	require.True(t, ok, "edit routes to DiffBlock, got %T", entries[0])
	assert.True(t, d.Expanded, "a diff still opens under auto-expand")
}

// A collapsed read row is a one-line summary; the body is one Enter away, and
// the user's choice must survive every later sync.
func TestMapperReadUserToggleSurvivesAutoExpand(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.AutoExpand = true

	entries, _, _ := m.Sync(nil, nil, readSnap())
	r := entries[0].(*block.ReadBlock)
	require.False(t, r.Expanded, "starts closed")

	r.Expanded = true
	r.OnToggle(true)
	entries, _, _ = m.Sync(entries, []string{"call_read"}, readSnap())
	again, ok := entries[0].(*block.ReadBlock)
	require.True(t, ok)
	assert.True(t, again.Expanded, "a user-opened read row stays open")

	again.Expanded = false
	again.OnToggle(false)
	entries, _, _ = m.Sync(entries, []string{"call_read"}, readSnap())
	reClosed, ok := entries[0].(*block.ReadBlock)
	require.True(t, ok)
	assert.False(t, reClosed.Expanded, "a user-closed row is not silently reopened")
}

// A running bash row keeps streaming visible (the existing rule), and a user's
// toggle wins over that too.
func TestMapperBashRunningRowFollowsOutput(t *testing.T) {
	snap := session.Snapshot{
		Messages: []session.Message{{
			ID: "m1", Role: session.RoleAssistant, State: session.StateStreaming,
			Content: []session.ContentBlock{{
				Type: session.BlockToolUse, ID: "b1", Name: "bash",
				Input: `{"command":"go test ./..."}`, Complete: false,
			}},
		}},
		Tools: map[string]session.ToolRun{
			"b1": {
				ToolUseID: "b1", Name: "bash", Status: session.ToolInProgress,
				Detail: "go test ./...", Output: "ok  \tfoo\t0.1s\n",
			},
		},
	}
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	entries, _, _ := m.Sync(nil, nil, snap)
	b, ok := entries[0].(*block.BashBlock)
	require.True(t, ok, "bash routes to BashBlock, got %T", entries[0])
	assert.True(t, b.Expanded, "a streaming bash row opens as output arrives")
}

// A user "!cmd" run still starts open: it ran in the user's own shell and they
// asked to see it.
func TestMapperLocalBashStaysOpen(t *testing.T) {
	snap := session.Snapshot{
		Messages: []session.Message{{
			ID: "lm1", Role: session.RoleLocalBash, Text: "git status",
		}},
		Tools: map[string]session.ToolRun{
			"lm1": {
				ToolUseID: "lm1", Name: "bash", Status: session.ToolDone, Local: true,
				Detail: "git status", Output: "On branch main\n",
			},
		},
	}
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	entries, _, _ := m.Sync(nil, nil, snap)
	b, ok := entries[0].(*block.BashBlock)
	require.True(t, ok, "local bash routes to BashBlock, got %T", entries[0])
	assert.True(t, b.Expanded, "a direct !cmd run starts open")
}

// The row must survive repeated syncs (the patch path) without flipping open
// or re-expanding on its own, and the collapsed trailer must stay accurate.
func TestBashRowStaysCollapsedAcrossSyncs(t *testing.T) {
	var out strings.Builder
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(&out, "line %d\n", i)
	}
	snap := session.Snapshot{
		Messages: []session.Message{{
			ID: "m1", Role: session.RoleAssistant, State: session.StateComplete,
			Content: []session.ContentBlock{{
				Type: session.BlockToolUse, ID: "b1", Name: "bash",
				Input: `{"command":"go test"}`, Complete: true,
			}},
		}},
		Tools: map[string]session.ToolRun{
			"b1": {ToolUseID: "b1", Name: "bash", Status: session.ToolDone, Detail: "go test", Output: out.String()},
		},
	}
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.AutoExpand = true
	entries, _, _ := m.Sync(nil, nil, snap)
	for range 5 {
		entries, _, _ = m.Sync(entries, []string{"b1"}, snap)
	}
	assert.False(t, entries[0].(*block.BashBlock).Expanded, "stays closed across repeated syncs")
}

// A user's expansion must survive the status transition (running -> done), or
// expanding a long-running command mid-flight would silently collapse.
func TestBashUserExpandSurvivesCompletion(t *testing.T) {
	mk := func(st session.ToolStatus) session.Snapshot {
		return session.Snapshot{
			Messages: []session.Message{{
				ID: "m1", Role: session.RoleAssistant, State: session.StateComplete,
				Content: []session.ContentBlock{{
					Type: session.BlockToolUse, ID: "b1", Name: "bash",
					Input: `{"command":"go test"}`, Complete: true,
				}},
			}},
			Tools: map[string]session.ToolRun{
				"b1": {ToolUseID: "b1", Name: "bash", Status: st, Detail: "go test", Output: "line1\nline2\n"},
			},
		}
	}
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.AutoExpand = true
	entries, _, _ := m.Sync(nil, nil, mk(session.ToolInProgress))
	b := entries[0].(*block.BashBlock)
	b.Expanded = true
	b.OnToggle(true)
	entries, _, _ = m.Sync(entries, []string{"b1"}, mk(session.ToolDone))
	still, ok := entries[0].(*block.BashBlock)
	require.True(t, ok, "widget type must survive the patch, got %T", entries[0])
	assert.True(t, still.Expanded, "user expansion survives the run finishing")
}
