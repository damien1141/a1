package contextbreakdown_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/contextbreakdown"
	"github.com/damien1141/a1/internal/session"
)

func TestBuildCategories(t *testing.T) {
	snap := session.Snapshot{
		Messages: []session.Message{
			{ID: "u1", Role: session.RoleUser, Text: "read the parser"},
			{
				ID: "a1", Role: session.RoleAssistant, State: session.StateComplete,
				Content: []session.ContentBlock{
					{Type: session.BlockText, Text: "checking"},
					{Type: session.BlockThinking, Text: "thinking about it"},
					{Type: session.BlockToolUse, ID: "t1", Name: "read", Input: "main.go"},
				},
			},
		},
		Tools: map[string]session.ToolRun{
			"t1": {
				ToolUseID: "t1", Name: "read", Status: session.ToolDone,
				Output: "package main", Detail: "main.go",
			},
		},
	}
	b := contextbreakdown.Build(snap, 200000, 9000, 12)

	names := []string{}
	for _, c := range b.Categories {
		names = append(names, c.Name)
	}
	assert.Equal(t, []string{"System prompt", "Tools", "Messages", "Thinking", "Tool output"}, names)

	// Every category has a positive estimate.
	for _, c := range b.Categories {
		assert.Positive(t, c.Tokens, "%s is estimated", c.Name)
	}

	// The tools row carries its note.
	assert.Equal(t, "12 registered", b.Categories[1].Note)

	used := b.UsedPercent()
	assert.Greater(t, used, 0)
	assert.LessOrEqual(t, used, 100, "the window bounds the percentage")
}

func TestTotalExcludesNotedCategories(t *testing.T) {
	b := contextbreakdown.Breakdown{
		Categories: []contextbreakdown.Category{
			{Name: "Real", Tokens: 100},
			{Name: "Deferred", Tokens: 5000, Note: "not in the window"},
		},
	}
	assert.Equal(t, 100, b.Total(),
		"a category with a note is informational and does not consume context")
}

func TestToolOutputCountedOnce(t *testing.T) {
	// The same tool run seen through many messages must not be multiplied.
	snap := session.Snapshot{
		Messages: []session.Message{
			{
				ID: "a1", Role: session.RoleAssistant, State: session.StateComplete,
				Content: []session.ContentBlock{{Type: session.BlockText, Text: "one"}},
			},
			{
				ID: "a2", Role: session.RoleAssistant, State: session.StateComplete,
				Content: []session.ContentBlock{{Type: session.BlockText, Text: "two"}},
			},
		},
		Tools: map[string]session.ToolRun{
			"t1": {ToolUseID: "t1", Name: "read", Status: session.ToolDone, Output: strings.Repeat("x", 400)},
		},
	}
	b := contextbreakdown.Build(snap, 0, 0, 0)
	for _, c := range b.Categories {
		if c.Name == "Tool output" {
			assert.Equal(t, 100, c.Tokens, "400 bytes once, not once per message")
		}
	}
}

func TestEstimateStatesItIsAnEstimate(t *testing.T) {
	snap := session.Snapshot{Messages: []session.Message{{ID: "u1", Role: session.RoleUser, Text: "hi"}}}
	got := contextbreakdown.Build(snap, 200000, 4000, 10).Estimate()
	assert.Contains(t, got, "estimate", "the header says so")
	assert.Contains(t, got, "~4 bytes/token", "the ratio is stated, not hidden")
	assert.Contains(t, got, "System prompt")
	assert.Contains(t, got, "200,000", "the window is formatted")
}

func TestEstimateUnknownWindow(t *testing.T) {
	got := contextbreakdown.Build(session.Snapshot{}, 0, 0, 0).Estimate()
	assert.Contains(t, got, "window unknown",
		"a missing window is reported, not treated as zero percent")
}

func TestEmptySnapshotHasNoMessageCategories(t *testing.T) {
	b := contextbreakdown.Build(session.Snapshot{}, 1000, 0, 0)
	for _, c := range b.Categories {
		assert.NotEqual(t, "Messages", c.Name)
		assert.NotEqual(t, "Tool output", c.Name)
	}
	// The system prompt is always present.
	require.NotEmpty(t, b.Categories)
	assert.Equal(t, "System prompt", b.Categories[0].Name)
}

func TestUsedPercentCapsSanely(t *testing.T) {
	b := contextbreakdown.Breakdown{
		Categories:    []contextbreakdown.Category{{Name: "a", Tokens: 5000}},
		ContextWindow: 100,
	}
	assert.LessOrEqual(t, b.UsedPercent(), 999, "an over-window breakdown does not print nonsense")
}
