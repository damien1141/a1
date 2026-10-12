package session_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/session"
)

// readBlock builds a read tool_use block.
func readBlock(id string) session.ContentBlock {
	return session.ContentBlock{Type: session.BlockToolUse, ID: id, Name: "read", Input: id + ".go"}
}

func grepBlock(id string) session.ContentBlock {
	return session.ContentBlock{Type: session.BlockToolUse, ID: id, Name: "grep", Input: "cursor"}
}

func TestCollapseGroupsConsecutiveReads(t *testing.T) {
	blocks := []session.ContentBlock{
		readBlock("r1"), readBlock("r2"), readBlock("r3"),
		grepBlock("g1"), grepBlock("g2"),
	}
	items := session.CollapseGroups(blocks, map[string]session.ToolRun{})
	require.Len(t, items, 1, "five reads collapse to one row")

	run := items[0].ToolRun
	assert.Equal(t, "read_group", run.Name)
	require.NotNil(t, run.Group)
	assert.Equal(t, 3, run.Group.Reads)
	assert.Equal(t, 2, run.Group.Searches)
	assert.Len(t, run.Group.Members, 5, "every member id is kept for expansion")
	assert.Equal(t, "Read 3 files, searched for 2 patterns", run.Detail)
}

func TestCollapseGroupsBreaksOnText(t *testing.T) {
	blocks := []session.ContentBlock{
		readBlock("r1"), readBlock("r2"),
		{Type: session.BlockText, Text: "now I see"},
		readBlock("r3"),
	}
	items := session.CollapseGroups(blocks, nil)
	require.Len(t, items, 2, "text between reads splits the run")
	assert.Equal(t, "read_group", items[0].ToolRun.Name, "two reads collapse")
	// The trailing lone read is not a group; it keeps its own row.
	assert.Equal(t, "read", items[1].ToolRun.Name)
	assert.Nil(t, items[1].ToolRun.Group)
}

func TestCollapseGroupsBreaksOnWrite(t *testing.T) {
	blocks := []session.ContentBlock{
		readBlock("r1"), readBlock("r2"),
		{Type: session.BlockToolUse, ID: "w1", Name: "write", Input: "out.go"},
		readBlock("r3"),
	}
	items := session.CollapseGroups(blocks, nil)
	require.Len(t, items, 3, "a write is never folded into a read run")
	assert.Equal(t, "write", items[1].ToolRun.Name)
}

func TestCollapseGroupsKeepsErroredReadSeparate(t *testing.T) {
	// A failure must be its own row: folded into a count it is unactionable.
	blocks := []session.ContentBlock{readBlock("r1"), readBlock("r2")}
	tools := map[string]session.ToolRun{
		"r2": {ToolUseID: "r2", Name: "read", Status: session.ToolError, Error: "no such file"},
	}
	items := session.CollapseGroups(blocks, tools)
	require.Len(t, items, 2, "the failed read stays on its own")
	assert.Equal(t, "read", items[0].ToolRun.Name)
	assert.Equal(t, "read", items[1].ToolRun.Name)
	assert.Equal(t, session.ToolError, items[1].ToolRun.Status)
}

func TestCollapseGroupsGroupStatusIsInProgressWhileAnyMemberIs(t *testing.T) {
	blocks := []session.ContentBlock{readBlock("r1"), readBlock("r2")}
	tools := map[string]session.ToolRun{
		"r1": {ToolUseID: "r1", Name: "read", Status: session.ToolDone},
	}
	// r2 is absent from the map, so its fallback is ToolInProgress.
	items := session.CollapseGroups(blocks, tools)
	require.Len(t, items, 1)
	assert.Equal(t, session.ToolInProgress, items[0].ToolRun.Status,
		"a group with a member still running is not done")
	assert.Equal(t, 2, items[0].ToolRun.Group.Reads,
		"both reads are members even though one has no live run yet")
}

func TestCollapseGroupsHintIsLatestTarget(t *testing.T) {
	blocks := []session.ContentBlock{readBlock("r1"), readBlock("r2")}
	tools := map[string]session.ToolRun{
		"r1": {ToolUseID: "r1", Name: "read", Status: session.ToolDone, Detail: "first.go"},
		"r2": {ToolUseID: "r2", Name: "read", Status: session.ToolDone, Detail: "last.go"},
	}
	items := session.CollapseGroups(blocks, tools)
	assert.Equal(t, "last.go", items[0].ToolRun.Group.Hint)
}

func TestCollapseGroupsCaseInsensitiveToolNames(t *testing.T) {
	// The replay path restores the model's casing.
	blocks := []session.ContentBlock{
		{Type: session.BlockToolUse, ID: "r1", Name: "Read"},
		{Type: session.BlockToolUse, ID: "r2", Name: "Grep"},
	}
	items := session.CollapseGroups(blocks, nil)
	require.Len(t, items, 1)
	assert.Equal(t, 1, items[0].ToolRun.Group.Reads)
	assert.Equal(t, 1, items[0].ToolRun.Group.Searches)
}

func TestCollapseGroupsPluralizes(t *testing.T) {
	one := session.CollapseGroups([]session.ContentBlock{readBlock("r1"), readBlock("r2")}, nil)
	assert.Equal(t, "Read 2 files", one[0].ToolRun.Detail)

	// A lone grep is not collapsed, so it is not summarized.
	two := session.CollapseGroups([]session.ContentBlock{grepBlock("g1"), grepBlock("g2")}, nil)
	assert.Equal(t, "searched for 2 patterns", two[0].ToolRun.Detail)
}

func TestProjectCollapsesReadRunInsideMessage(t *testing.T) {
	snap := session.Snapshot{
		Messages: []session.Message{{
			ID: "a1", Role: session.RoleAssistant, State: session.StateComplete,
			Content: []session.ContentBlock{
				{Type: session.BlockText, Text: "checking"},
				readBlock("r1"), readBlock("r2"), readBlock("r3"),
				{Type: session.BlockText, Text: "found it"},
			},
		}},
	}
	items := session.Project(snap)
	require.Len(t, items, 3, "text, one collapsed group, text")
	assert.Equal(t, session.ItemAssistant, items[0].Kind)
	assert.Equal(t, "checking", items[0].Text)
	assert.Equal(t, "read_group", items[1].ToolRun.Name)
	assert.Equal(t, session.ItemAssistant, items[2].Kind)
	assert.Equal(t, "found it", items[2].Text, "text after the run keeps its position")
}

func TestProjectSingleReadIsNotCollapsed(t *testing.T) {
	snap := session.Snapshot{
		Messages: []session.Message{{
			ID: "a1", Role: session.RoleAssistant, State: session.StateComplete,
			Content: []session.ContentBlock{readBlock("r1")},
		}},
	}
	items := session.Project(snap)
	require.Len(t, items, 1, "one read stays its own row")
	assert.Equal(t, "read", items[0].ToolRun.Name)
	assert.Nil(t, items[0].ToolRun.Group)
}

// stripLeadingVerb lives in the mapper, but its correctness is a session-adjacent
// concern: the group summary depends on DetailFromArgs phrases staying unique.
func TestMemberDetailKeepsNonVerbText(t *testing.T) {
	// Covered properly by the mapper tests; this guards the summary text that
	// the group row prints, which comes from here.
	snap := session.Snapshot{
		Messages: []session.Message{{
			ID: "a1", Role: session.RoleAssistant, State: session.StateComplete,
			Content: []session.ContentBlock{
				{Type: session.BlockToolUse, ID: "r1", Name: "read", Input: "haiku.txt", Complete: true},
				{Type: session.BlockToolUse, ID: "g1", Name: "grep", Input: "cursor", Complete: true},
			},
		}},
		Tools: map[string]session.ToolRun{
			"r1": {ToolUseID: "r1", Name: "read", Status: session.ToolDone, Detail: "haiku.txt"},
			"g1": {ToolUseID: "g1", Name: "grep", Status: session.ToolDone, Detail: `grep "cursor" in .`},
		},
	}
	items := session.Project(snap)
	require.Len(t, items, 1)
	assert.Equal(t, "Read 1 file, searched for 1 pattern", items[0].ToolRun.Detail,
		"the summary is counts only — the per-tool phrases stay in the expansion")
	assert.Equal(t, `grep "cursor" in .`, items[0].ToolRun.Group.Hint,
		"the hint is the latest member's display phrase, which reads better raw")
}

func TestCollapseGroupsFoldsFetchButNotBrowser(t *testing.T) {
	// A turn that pulls several docs is several rows otherwise, and each is a
	// single glanceable line.
	blocks := []session.ContentBlock{
		readBlock("r1"),
		{Type: session.BlockToolUse, ID: "f1", Name: "fetch", Input: "https://a.test/docs"},
		{Type: session.BlockToolUse, ID: "g1", Name: "grep", Input: "cursor"},
		{Type: session.BlockToolUse, ID: "f2", Name: "fetch", Input: "https://b.test/x"},
	}
	items := session.CollapseGroups(blocks, nil)
	require.Len(t, items, 1, "reads, fetches and searches fold together")

	g := items[0].ToolRun.Group
	require.NotNil(t, g)
	assert.Equal(t, 1, g.Reads)
	assert.Equal(t, 1, g.Searches)
	assert.Equal(t, 2, g.Fetched)
	assert.Len(t, g.Members, 4)
	assert.Equal(t, "Read 1 file, searched for 1 pattern, fetched 2 pages", items[0].ToolRun.Detail)
}

// browser drives one page through a sequential chain — open, navigate, click,
// type — where each step depends on the previous. Folding it into a count would
// hide the order and what each step did, so it stays its own rows.
func TestCollapseGroupsDoesNotFoldBrowser(t *testing.T) {
	blocks := []session.ContentBlock{
		{Type: session.BlockToolUse, ID: "b1", Name: "browser", Input: "open"},
		{Type: session.BlockToolUse, ID: "b2", Name: "browser", Input: "click"},
		{Type: session.BlockToolUse, ID: "b3", Name: "browser", Input: "type"},
	}
	items := session.CollapseGroups(blocks, nil)
	require.Len(t, items, 3, "each browser action is its own row, in order")

	for i, want := range []string{"open", "click", "type"} {
		assert.Equal(t, "browser", items[i].ToolRun.Name)
		assert.Contains(t, items[i].ToolRun.Detail, want,
			"step %d keeps its identity so the sequence reads left to right", i)
	}
}

// A browser call between two fetches must break the fetch run, not be absorbed.
func TestCollapseGroupsBrowserBreaksFetchRun(t *testing.T) {
	blocks := []session.ContentBlock{
		{Type: session.BlockToolUse, ID: "f1", Name: "fetch", Input: "https://a.test"},
		{Type: session.BlockToolUse, ID: "f2", Name: "fetch", Input: "https://b.test"},
		{Type: session.BlockToolUse, ID: "b1", Name: "browser", Input: "content"},
		{Type: session.BlockToolUse, ID: "f3", Name: "fetch", Input: "https://c.test"},
		{Type: session.BlockToolUse, ID: "f4", Name: "fetch", Input: "https://d.test"},
	}
	items := session.CollapseGroups(blocks, nil)
	require.Len(t, items, 3, "fetch pair · browser · fetch pair")
	assert.Equal(t, "read_group", items[0].ToolRun.Name)
	assert.Equal(t, 2, items[0].ToolRun.Group.Fetched)
	assert.Equal(t, "browser", items[1].ToolRun.Name)
	assert.Equal(t, "read_group", items[2].ToolRun.Name)
	assert.Equal(t, 2, items[2].ToolRun.Group.Fetched)
}

func TestCollapseGroupsFetchIsCaseInsensitive(t *testing.T) {
	blocks := []session.ContentBlock{
		{Type: session.BlockToolUse, ID: "f1", Name: "Fetch"},
		{Type: session.BlockToolUse, ID: "f2", Name: "Fetch"},
	}
	items := session.CollapseGroups(blocks, nil)
	require.Len(t, items, 1)
	assert.Equal(t, 2, items[0].ToolRun.Group.Fetched)
}

func TestCollapseGroupsFailedFetchStaysSeparate(t *testing.T) {
	// A dead url is actionable on its own row and meaningless inside a count.
	blocks := []session.ContentBlock{
		{Type: session.BlockToolUse, ID: "f1", Name: "fetch"},
		{Type: session.BlockToolUse, ID: "f2", Name: "fetch"},
	}
	tools := map[string]session.ToolRun{
		"f2": {ToolUseID: "f2", Name: "fetch", Status: session.ToolError, Error: "connection refused"},
	}
	items := session.CollapseGroups(blocks, tools)
	require.Len(t, items, 2, "the failed fetch is its own row")
	assert.Equal(t, session.ToolError, items[1].ToolRun.Status)
}
