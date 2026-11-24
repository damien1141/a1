package transcript

import (
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/components"
)

// startsTurn is a widget that declares itself a turn boundary.
type startsTurn struct{ height int }

func (s *startsTurn) Draw(components.DrawContext) components.Surface {
	return components.NewSurface(10, s.height, s)
}
func (s *startsTurn) Handle(*components.EventContext, xui.Event) {}
func (s *startsTurn) StartsTurn() bool                           { return true }

// plainRow is an ordinary entry inside a turn.
type plainRow struct{ height int }

func (p *plainRow) Draw(components.DrawContext) components.Surface {
	return components.NewSurface(10, p.height, p)
}
func (p *plainRow) Handle(*components.EventContext, xui.Event) {}

// TestTurnSpacing_GroupsTurns_Positive: a turn boundary gets a larger gap above
// it than the entries inside the turn. Flat spacing made a long transcript read
// as one undifferentiated wall.
func TestTurnSpacing_GroupsTurns_Positive(t *testing.T) {
	list := &MessageList{
		Entries: []components.Widget{
			&startsTurn{height: 1}, // turn 1
			&plainRow{height: 1},   // its tool row
			&plainRow{height: 1},
			&startsTurn{height: 1}, // turn 2
			&plainRow{height: 1},
		},
		ItemSpacing: 1,
		TurnSpacing: 2,
	}
	_ = list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 40}})
	assert.Equal(t, []bool{true, false, false, true, false}, list.TurnStart,
		"boundaries are derived from the widgets")

	first, ok := list.EntryOrigin(0)
	assert.True(t, ok)
	second, _ := list.EntryOrigin(1)
	turn2, _ := list.EntryOrigin(3)

	// Express the contract as a RELATION rather than absolute rows: the gap above
	// a turn must be strictly larger than the gap inside one. Absolute arithmetic
	// here is just a re-derivation of the layout and fails on every tweak.
	insideGap := second.Y - (first.Y + 1)
	turnGap := turn2.Y - (second.Y + 1)
	assert.Equal(t, 1, insideGap, "within a turn: ordinary spacing")
	assert.Greater(t, turnGap, insideGap,
		"between turns the gap must be visibly larger, or turns do not group")
}

// TestTurnSpacing_UniformWhenUndeclared_Negative: with no boundaries the layout
// is exactly what it was — no silent spacing change for callers that never opt
// in.
func TestTurnSpacing_UniformWhenUndeclared_Negative(t *testing.T) {
	list := &MessageList{
		Entries:     []components.Widget{&plainRow{1}, &plainRow{1}, &plainRow{1}},
		ItemSpacing: 1,
	}
	_ = list.Draw(components.DrawContext{Max: components.Size{Width: 40, Height: 40}})
	assert.Equal(t, []bool{false, false, false}, list.TurnStart)

	a, _ := list.EntryOrigin(0)
	b, _ := list.EntryOrigin(1)
	assert.Equal(t, 1, b.Y-a.Y-1, "every gap is the ordinary spacing")
}
