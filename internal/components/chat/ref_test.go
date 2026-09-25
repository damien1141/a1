package chat

import (
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

func TestRefLabel(t *testing.T) {
	tests := []struct {
		name string
		ref  Ref
		want string
	}{
		{"single line", Ref{Path: "main.go", Start: 12, End: 12}, "main.go:12"},
		{"range", Ref{Path: "main.go", Start: 12, End: 18}, "main.go:12-18"},
		{"zero start clamps to one", Ref{Path: "main.go", Start: 0, End: 0}, "main.go:1"},
		{"backwards range collapses", Ref{Path: "main.go", Start: 9, End: 4}, "main.go:9"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.ref.Label())
		})
	}
}

func TestRefBlock(t *testing.T) {
	withLang := Ref{Path: "main.go", Start: 12, End: 13, Lang: "go", Text: "a := 1\nb := 2"}
	assert.Equal(t, "main.go:12-13\n```go\na := 1\nb := 2\n```", withLang.Block())

	bare := Ref{Path: "main.go", Start: 12, End: 12, Text: "x"}
	assert.Equal(t, "main.go:12\n```\nx\n```", bare.Block())

	// A selection that already ends with a newline must not gain a blank line.
	trailing := Ref{Path: "main.go", Start: 1, End: 1, Text: "x\n"}
	assert.Equal(t, "main.go:1\n```\nx\n```", trailing.Block())
}

func TestChatInputAddPendingRefRejectsAndDedups(t *testing.T) {
	c := &ChatInput{}
	c.AddPendingRef(Ref{Start: 1, End: 2, Text: "x"})
	c.AddPendingRef(Ref{Path: "main.go", Start: 1, End: 2})
	require.Empty(t, c.PendingRefs, "empty path or text carries nothing")

	ref := Ref{Path: "main.go", Start: 3, End: 4, Text: "x"}
	c.AddPendingRef(ref)
	c.AddPendingRef(ref)
	c.AddPendingRef(Ref{Path: "main.go", Start: 3, End: 5, Text: "x"})
	assert.Len(t, c.PendingRefs, 2)
}

func TestChatInputPendingRefsRow(t *testing.T) {
	c := &ChatInput{
		MinBodyRows:   3,
		PendingRefs:   []Ref{{Path: "main.go", Start: 12, End: 18, Lang: "go", Text: "x"}},
		PendingSkills: []string{"building-plugins"},
		Theme:         components.DefaultTheme(),
	}
	method := xui.WidthUnicode
	require.Equal(t, 7, c.PreferredHeight(60, method), "preferred height with a ref and a skill")

	s := c.Draw(components.DrawContext{Max: components.Size{Width: 60, Height: 10}, Method: method})
	require.Equal(t, 7, s.Size.Height, "draw height")
	// Refs paint before skills so the pending rows stay contiguous.
	require.Contains(t, rowString(s, 1), "Refs:")
	require.Contains(t, rowString(s, 1), "main.go:12-18")
	require.Contains(t, rowString(s, 2), "Skills:")
	require.NotNil(t, s.Cursor, "expected cursor below the pending rows")
	require.Equal(t, 3, s.Cursor.Y, "cursor below refs and skills")
}

func TestChatInputPopAndClearPendingRefs(t *testing.T) {
	c := &ChatInput{MinBodyRows: 3}
	base := c.PreferredHeight(40, xui.WidthUnicode)
	c.AddPendingRef(Ref{Path: "main.go", Start: 1, End: 2, Text: "x"})
	require.Equal(t, base+1, c.PreferredHeight(40, xui.WidthUnicode), "one row per pending refs")

	notified := 0
	c.OnPendingRefsChange = func([]Ref) { notified++ }
	require.True(t, c.PopPendingRef())
	require.False(t, c.PopPendingRef(), "nothing left to pop")
	require.Equal(t, 1, notified)
	require.Equal(t, base, c.PreferredHeight(40, xui.WidthUnicode))

	c.AddPendingRef(Ref{Path: "main.go", Start: 1, End: 2, Text: "x"})
	c.ClearPendingRefs()
	require.Empty(t, c.PendingRefs)
	require.Equal(t, 3, notified, "add and clear notify once each")
	c.ClearPendingRefs()
	require.Equal(t, 3, notified, "clearing an empty list stays silent")
}

func TestChatInputBackspacePopsPendingRef(t *testing.T) {
	c := &ChatInput{MinBodyRows: 3}
	c.AddPendingRef(Ref{Path: "main.go", Start: 1, End: 2, Text: "x"})
	c.Handle(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyBackspace})
	require.Empty(t, c.PendingRefs)
}
