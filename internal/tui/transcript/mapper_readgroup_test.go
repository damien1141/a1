package transcript_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/transcript"
)

func groupSnap() session.Snapshot {
	return session.Snapshot{
		Messages: []session.Message{{
			ID: "a1", Role: session.RoleAssistant, State: session.StateComplete,
			Content: []session.ContentBlock{
				{Type: session.BlockToolUse, ID: "r1", Name: "read", Input: "haiku.txt", Complete: true},
				{Type: session.BlockToolUse, ID: "g1", Name: "grep", Input: "cursor", Complete: true},
				{Type: session.BlockToolUse, ID: "r2", Name: "read", Input: "main.go", Complete: true},
			},
		}},
		Tools: map[string]session.ToolRun{
			"r1": {ToolUseID: "r1", Name: "read", Status: session.ToolDone, Detail: "haiku.txt"},
			"g1": {ToolUseID: "g1", Name: "grep", Status: session.ToolDone, Detail: `grep "cursor" in .`},
			"r2": {ToolUseID: "r2", Name: "read", Status: session.ToolDone, Detail: "main.go"},
		},
	}
}

func TestMapperRoutesReadGroup(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	entries, ids, _ := m.Sync(nil, nil, groupSnap())
	require.Len(t, entries, 1, "three reads are one row")
	assert.Equal(t, "tool-group-r1", ids[0])
	g, ok := entries[0].(*block.ReadGroupBlock)
	require.True(t, ok, "got %T", entries[0])
	assert.Equal(t, "Read 2 files, searched for 1 pattern", g.Detail)
	require.Len(t, g.Members(), 3)
	assert.Equal(t, `"cursor" in .`, g.Members()[1].Detail,
		"the leading verb is stripped so the row does not read grep grep")
}

func TestMapperReadGroupPatchesInPlace(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	first, ids, _ := m.Sync(nil, nil, groupSnap())
	require.Len(t, first, 1)

	// The last read finishes between syncs; the same widget must update rather
	// than rebuild, which would drop the row's scroll position.
	snap := groupSnap()
	running := snap.Tools["r2"]
	running.Status = session.ToolInProgress
	snap.Tools["r2"] = running
	second, _, dirty := m.Sync(first, ids, snap)
	require.Len(t, second, 1)
	assert.Same(t, first[0], second[0], "the row is patched, not replaced")
	assert.NotEmpty(t, dirty, "the change is dirty so the height refreshes")

	g := second[0].(*block.ReadGroupBlock)
	require.Len(t, g.Members(), 3, "members still resolve from the live snapshot")
	assert.Equal(t, "main.go", g.Members()[2].Detail)
}

func TestMapperErroredMemberSplitsGroup(t *testing.T) {
	// An error must never be folded into a count the user cannot act on, so a
	// member that failed re-enters the transcript as its own row.
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	snap := groupSnap()
	snap.Tools["r2"] = session.ToolRun{
		ToolUseID: "r2", Name: "read", Status: session.ToolError, Error: "no such file",
	}
	entries, _, _ := m.Sync(nil, nil, snap)
	require.Len(t, entries, 2, "the failed read is its own row")

	group, ok := entries[0].(*block.ReadGroupBlock)
	require.True(t, ok, "got %T", entries[0])
	assert.Equal(t, 2, len(group.Members()), "only the survivors stay in the group")

	_, isRead := entries[1].(*block.ReadBlock)
	assert.True(t, isRead, "the failure renders as a normal read card")
}

func TestMapperReadGroupAutoExpand(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.AutoExpand = true
	entries, _, _ := m.Sync(nil, nil, groupSnap())
	assert.True(t, entries[0].(*block.ReadGroupBlock).Expanded,
		"the summary alone would hide the cards the user asked to see")
}

func TestMapperReadGroupCollapsedByDefault(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	entries, _, _ := m.Sync(nil, nil, groupSnap())
	assert.False(t, entries[0].(*block.ReadGroupBlock).Expanded)
}

func TestMapperGroupMemberPrefersSummary(t *testing.T) {
	// A fetch member's status and size are the answer; its url is provenance.
	snap := session.Snapshot{
		Messages: []session.Message{{
			ID: "a1", Role: session.RoleAssistant, State: session.StateComplete,
			Content: []session.ContentBlock{
				{Type: session.BlockToolUse, ID: "f1", Name: "fetch", Input: "a", Complete: true},
				{Type: session.BlockToolUse, ID: "f2", Name: "fetch", Input: "b", Complete: true},
			},
		}},
		Tools: map[string]session.ToolRun{
			"f1": {
				ToolUseID: "f1", Name: "fetch", Status: session.ToolDone, Detail: "https://a.test/x",
				Summary: "200 OK · 12 KB · 0.4s · a.test",
			},
			"f2": {
				ToolUseID: "f2", Name: "fetch", Status: session.ToolDone, Detail: "https://b.test/y",
				Summary: "404 Not Found · 1 KB · 0.1s · b.test",
			},
		},
	}
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	m.AutoExpand = true
	entries, _, _ := m.Sync(nil, nil, snap)
	g, ok := entries[0].(*block.ReadGroupBlock)
	require.True(t, ok, "got %T", entries[0])

	members := g.Members()
	require.Len(t, members, 2)
	assert.Equal(t, "200 OK · 12 KB · 0.4s · a.test", members[0].Summary)

	got := components.SurfaceText(g.Draw(components.DrawContext{
		Max: components.Size{Width: 74, Height: 20},
	}))
	assert.Contains(t, got, "fetch 200 OK · 12 KB · 0.4s")
	assert.Contains(t, got, "fetch 404 Not Found · 1 KB · 0.1s",
		"a non-200 member stays visible in the group")
	assert.NotContains(t, got, "https://a.test/x",
		"the url is provenance; the expansion still carries it in each member")
}
