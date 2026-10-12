package components_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

func TestBylineDropsEmptyItems(t *testing.T) {
	// The whole contract: a caller assembles candidates, Byline renders
	// whichever exist with no dangling separator.
	assert.Equal(t, "grep", components.Byline("grep", "", "  "))
	assert.Equal(t, "grep · 3 matches", components.Byline("grep", "", "3 matches"))
	assert.Equal(t, "a · b · c", components.Byline("a", "b", "c"))
	assert.Equal(t, "a", components.Byline("a"))
	assert.Equal(t, "", components.Byline("", "", ""))
	assert.Equal(t, "", components.Byline())
}

func TestBylineTrimsButKeepsInnerSpaces(t *testing.T) {
	// A grep phrase has spaces in it; only the edges get trimmed.
	assert.Equal(t, "read · cursor in .", components.Byline("  read  ", " cursor in . "))
}

func TestBylineSpansInterleavesSeparator(t *testing.T) {
	th := components.DefaultTheme()
	got := components.BylineSpans([]components.BylineItem{
		{Text: "read", Style: th.ToolName},
		{Text: "haiku.txt", Style: th.Muted},
	}, th.Muted)
	require.Len(t, got, 3, "two items and the separator between them")
	assert.Equal(t, "read", got[0].Text)
	assert.Equal(t, components.BylineSeparator, got[1].Text)
	assert.Equal(t, "haiku.txt", got[2].Text)
	assert.Equal(t, th.ToolName, got[0].Style, "each item keeps its own style")
}

func TestBylineSpansSkipsEmptyItems(t *testing.T) {
	th := components.DefaultTheme()
	got := components.BylineSpans([]components.BylineItem{
		{Text: "read", Style: th.ToolName},
		{Text: "", Style: th.Muted},
		{Text: "   ", Style: th.Muted},
		{Text: "haiku.txt", Style: th.Muted},
	}, th.Muted)
	require.Len(t, got, 3, "empty items contribute no separator either")
	assert.Equal(t, "read", got[0].Text)
	assert.Equal(t, components.BylineSeparator, got[1].Text)
	assert.Equal(t, "haiku.txt", got[2].Text)
}

func TestBylineSpansAllEmpty(t *testing.T) {
	th := components.DefaultTheme()
	assert.Empty(t, components.BylineSpans([]components.BylineItem{
		{Text: "", Style: th.Muted},
		{Text: " ", Style: th.Muted},
	}, th.Muted))
}
