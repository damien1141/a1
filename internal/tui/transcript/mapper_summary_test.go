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

func summarySnap() session.Snapshot {
	return session.Snapshot{
		Messages: []session.Message{{
			ID: "a1", Role: session.RoleAssistant, State: session.StateComplete,
			Content: []session.ContentBlock{
				{Type: session.BlockToolUse, ID: "f1", Name: "fetch", Input: "u", Complete: true},
			},
		}},
		Tools: map[string]session.ToolRun{
			"f1": {
				ToolUseID: "f1", Name: "fetch", Status: session.ToolDone,
				Detail:  "https://example.com/docs",
				Summary: "200 OK · 42.5 KB · 1.2s · example.com",
				Output:  "Status: 200 OK",
			},
		},
	}
}

func TestMapperToolBlockShowsSummary(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	entries, _, _ := m.Sync(nil, nil, summarySnap())
	require.Len(t, entries, 1)
	tb, ok := entries[0].(*block.ToolBlock)
	require.True(t, ok, "got %T", entries[0])

	got := components.SurfaceText(tb.Draw(components.DrawContext{
		Max: components.Size{Width: 90, Height: 20},
	}))
	assert.Contains(t, got, "200 OK · 42.5 KB · 1.2s",
		"the tool's own summary line, not a generic line count")
	assert.NotContains(t, got, "(1 line)",
		"the fallback line count yields to a real summary")
	assert.Contains(t, got, "https://example.com/docs", "the identity is untouched")
}

func TestMapperToolBlockFallsBackToLineCount(t *testing.T) {
	snap := summarySnap()
	snap.Tools["f1"] = session.ToolRun{
		ToolUseID: "f1", Name: "fetch", Status: session.ToolDone,
		Detail: "https://example.com", Output: "a\nb\nc",
	}
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	entries, _, _ := m.Sync(nil, nil, snap)
	tb := entries[0].(*block.ToolBlock)
	got := components.SurfaceText(tb.Draw(components.DrawContext{
		Max: components.Size{Width: 90, Height: 20},
	}))
	assert.Contains(t, got, "(3 lines)", "tools without a summary keep the old behavior")
}

func TestMapperToolBlockSummarySurvivesPatch(t *testing.T) {
	m := transcript.NewMapper(components.DefaultTheme(), nil, nil)
	first, ids, _ := m.Sync(nil, nil, summarySnap())
	require.Len(t, first, 1)

	second, _, dirty := m.Sync(first, ids, summarySnap())
	require.Len(t, second, 1)
	assert.Same(t, first[0], second[0], "patched, not rebuilt")
	assert.Empty(t, dirty, "an unchanged summary is not dirty")
}
