package mention

import (
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

func TestPickerAccept(t *testing.T) {
	var got string
	p := &Picker{
		Items: []Item{{Path: "go.mod"}, {Path: "a/b.go"}},
		OnAccept: func(item Item) {
			got = item.Path
		},
	}
	p.Show()
	p.Selected = 1
	require.True(t, p.Accept(), "accept failed")
	require.Equal(t, "a/b.go", got)
	require.False(t, p.Open, "should be closed")
}

func TestPickerHandleNav(t *testing.T) {
	p := &Picker{
		Items: []Item{{Path: "a"}, {Path: "b"}, {Path: "c"}},
	}
	p.Show()
	require.True(t, p.HandleNav(xui.KeyEvent{Press: true, Code: xui.KeyDown}), "expected consume")
	require.Equal(t, 1, p.Selected)
	require.True(t, p.HandleNav(xui.KeyEvent{Press: true, Code: xui.KeyEscape}), "expected consume")
	require.False(t, p.Open, "should close on escape")
}

func TestPickerTabCompletesWithoutAccepting(t *testing.T) {
	var accepted, completed string
	p := &Picker{
		Items:      []Item{{Path: "clear"}, {Path: "compact"}},
		OnAccept:   func(item Item) { accepted = item.Path },
		OnComplete: func(item Item) { completed = item.Path },
	}
	p.Show()
	p.Selected = 1

	require.True(t, p.HandleNav(xui.KeyEvent{Press: true, Code: xui.KeyTab}), "expected consume")
	assert.Equal(t, "compact", completed, "Tab should complete the highlighted item")
	assert.Empty(t, accepted, "Tab must not accept")
	assert.Equal(t, 1, p.Selected, "Tab must not move the selection")
	assert.False(t, p.Open, "should be closed")
}

func TestPickerShiftTabCompletes(t *testing.T) {
	var completed string
	p := &Picker{
		Items:      []Item{{Path: "a"}, {Path: "b"}},
		OnComplete: func(item Item) { completed = item.Path },
	}
	p.Show()
	p.Selected = 1

	require.True(t, p.HandleNav(xui.KeyEvent{Press: true, Code: xui.KeyTab, Mods: xui.ModShift}))
	assert.Equal(t, "b", completed, "Shift+Tab should no longer step back a row")
}

func TestPickerTabFallsBackToAccept(t *testing.T) {
	var got string
	p := &Picker{
		Items:    []Item{{Path: "go.mod"}},
		OnAccept: func(item Item) { got = item.Path },
	}
	p.Show()

	require.True(t, p.HandleNav(xui.KeyEvent{Press: true, Code: xui.KeyTab}))
	assert.Equal(t, "go.mod", got, "without OnComplete, Tab completes via OnAccept")
}

func TestPickerTabClosesOnEmptyResults(t *testing.T) {
	accepted := false
	p := &Picker{
		Status:   "No matching commands",
		OnAccept: func(Item) { accepted = true },
	}
	p.Show()

	require.True(t, p.HandleNav(xui.KeyEvent{Press: true, Code: xui.KeyTab}))
	assert.False(t, accepted, "nothing to complete")
	assert.False(t, p.Open, "Tab on an empty picker should close it, not stick")
}

func TestPickerDrawClosed(t *testing.T) {
	p := &Picker{Theme: components.DefaultTheme()}
	surf := p.Draw(components.DrawContext{
		Max:    components.Size{Width: 80, Height: 24},
		Method: xui.WidthUnicode,
	})
	require.Empty(t, surf.Children, "closed picker should have no children")
}

func TestPickerDrawOpen(t *testing.T) {
	p := &Picker{
		Theme:         components.DefaultTheme(),
		Items:         []Item{{Path: "go.mod"}, {Path: "internal/x.go"}},
		AnchorBottomY: 20,
		AnchorWidth:   60,
		AnchorX:       0,
	}
	p.Show()
	surf := p.Draw(components.DrawContext{
		Max:    components.Size{Width: 80, Height: 24},
		Method: xui.WidthUnicode,
	})
	require.Len(t, surf.Children, 1)
	child := surf.Children[0]
	require.LessOrEqual(t, child.Origin.Y+child.Surface.Size.Height, 20,
		"panel should sit above anchor: oy=%d h=%d", child.Origin.Y, child.Surface.Size.Height)
}
