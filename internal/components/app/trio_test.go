package app

import (
	"math/rand"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

func TestAppNilReceiverGuards_Negative(t *testing.T) {
	var nilApp *App
	assert.NotPanics(t, func() {
		nilApp.RequestRedraw()
		nilApp.RequestFocus(nil)
		nilApp.SetExitCode(3)
	})
	assert.Equal(t, 0, nilApp.ExitCode(), "a nil App has no exit code")

	// A zero App (no Vaxis, no loop, no root) still answers accessors.
	var a App
	assert.Equal(t, 0, a.ExitCode())
	a.SetExitCode(7)
	assert.Equal(t, 7, a.ExitCode())

	// Without a loop, RequestRedraw falls back to the redraw flag.
	assert.False(t, a.redraw)
	a.RequestRedraw()
	assert.True(t, a.redraw, "no loop installed: redraw flag is set directly")

	// RequestFocus(nil) means "give focus back to root".
	root := &stubWidget{}
	a.root = root
	a.focused = &stubWidget{}
	a.redraw = false
	a.RequestFocus(nil)
	assert.Equal(t, root, a.focused, "nil widget focuses the root")
	assert.True(t, a.redraw, "focus changes request a redraw")
}

func TestAppHandleEventHostileState_Negative(t *testing.T) {
	// A zero App has no root, no painted surface, and no focused widget:
	// events must fall through without a panic and without quitting.
	var a App
	assert.NotPanics(t, func() {
		assert.False(t, a.handleEvent(xui.KeyEvent{Code: xui.KeyRune, Rune: 'j', Press: true}))
		assert.False(t, a.handleEvent(xui.KeyEvent{Code: xui.KeyRune, Rune: 'j'}), "key release is a no-op")
		assert.False(t, a.handleEvent(xui.MouseEvent{
			X: 1 << 20, Y: -1 << 20, Button: xui.MouseLeft, Action: xui.MousePress,
		}), "mouse press far outside the (empty) surface")
		assert.False(t, a.handleEvent(xui.FocusEvent{Focused: true}))
	})
	assert.False(t, a.redraw, "key events on a rootless app request nothing")

	// Ticks only set the redraw request; they never quit.
	assert.False(t, a.handleEvent(xui.TickEvent{}), "ticks must not quit")
	assert.True(t, a.redraw, "tick requests a redraw")

	// Ctrl+C still quits even from a degenerate state (the one hard exit).
	assert.True(t, a.handleEvent(xui.KeyEvent{Code: xui.KeyRune, Rune: 'c', Mods: xui.ModCtrl, Press: true}))
}

// oracleHasWidget is an iterative worklist membership check: it walks the
// whole surface tree (breadth-first) and compares widget identity, instead of
// the early-return recursion surfaceHasWidget uses.
func oracleHasWidget(s components.Surface, w components.Widget) bool {
	if w == nil {
		return false
	}
	queue := []components.Surface{s}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.Widget == w {
			return true
		}
		for _, ch := range cur.Children {
			queue = append(queue, ch.Surface)
		}
	}
	return false
}

// oracle: the worklist reference above on randomized surface trees — random
// nesting, z-order, origins, and widget pointers drawn from a pool (plus one
// widget guaranteed absent).
func TestSurfaceHasWidgetMatchesWorklist_Differential(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	pool := make([]components.Widget, 6)
	for i := range pool {
		pool[i] = &stubWidget{}
	}
	absent := &stubWidget{}

	var build func(depth int) components.Surface
	build = func(depth int) components.Surface {
		wIdx := rng.Intn(len(pool) + 1) // +1: sometimes no widget at all
		s := components.Surface{
			Size:   components.Size{Width: rng.Intn(9), Height: rng.Intn(9)},
			Widget: nil,
		}
		if wIdx < len(pool) {
			s.Widget = pool[wIdx]
		}
		if depth > 0 {
			for n := rng.Intn(4); n > 0; n-- {
				s.Children = append(s.Children, components.SubSurface{
					Origin:  components.Point{X: rng.Intn(5) - 2, Y: rng.Intn(5) - 2},
					Z:       rng.Intn(3) - 1,
					Surface: build(depth - 1),
				})
			}
		}
		return s
	}

	for i := range 200 {
		tree := build(3)
		candidates := append([]components.Widget{}, pool...)
		candidates = append(candidates, absent, nil)
		for _, w := range candidates {
			assert.Equal(t, oracleHasWidget(tree, w), surfaceHasWidget(tree, w),
				"case %d: membership of widget %v in tree of %d children", i, w, len(tree.Children))
		}
	}

	// The empty surface has nothing, not even the root pointer it lacks.
	require.False(t, surfaceHasWidget(components.Surface{}, pool[0]))
	require.False(t, surfaceHasWidget(components.Surface{}, nil))
}
