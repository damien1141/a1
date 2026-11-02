package app

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/components"
)

type stubWidget struct {
	got int
}

func (s *stubWidget) Handle(ctx *components.EventContext, ev xui.Event) {
	if _, ok := ev.(xui.KeyEvent); ok {
		s.got++
		ctx.Consume = true
	}
}

func (s *stubWidget) Draw(components.DrawContext) components.Surface {
	return components.NewSurface(1, 1, s)
}

func TestDispatchSkipsFocusMissingFromSurface(t *testing.T) {
	root := &stubWidget{}
	chat := &stubWidget{}
	a := &App{
		root:     root,
		focused:  chat,
		lastSurf: components.NewSurface(8, 4, root),
	}
	a.dispatch(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: 'j'})
	assert.Equal(t, 0, chat.got)
	assert.Equal(t, 1, root.got)
	assert.Equal(t, root, a.focused)
}

func TestDispatchKeepsFocusWhenPainted(t *testing.T) {
	root := &stubWidget{}
	chat := &stubWidget{}
	surf := components.NewSurface(8, 4, root)
	surf.Children = []components.SubSurface{{Surface: components.NewSurface(8, 2, chat)}}
	a := &App{root: root, focused: chat, lastSurf: surf}
	a.dispatch(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: 'j'})
	assert.Equal(t, 1, chat.got)
	assert.Equal(t, 0, root.got)
}
