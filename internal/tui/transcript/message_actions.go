package transcript

import (
	"fmt"
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
)

// Message-action mode: a cursor over the transcript that can copy any block.
//
// Claude Code binds shift+up to enter the mode, then j/k to move, Enter to act
// (expand a group, or edit a user message), c to copy, and Esc to leave. The
// shape is what matters rather than the letters: every block already computes
// CopyText, but without a cursor there is no keyboard path to it — only a mouse
// selection, which does not work over ssh or in a plain terminal.
type MessageActions struct {
	// Active reports whether the mode owns the keyboard. While inactive every
	// key falls through to the composer, so typing is never intercepted.
	Active bool

	cursor int
	pane   *TranscriptPane
}

// NewMessageActions builds cursor state bound to a pane.
func NewMessageActions(pane *TranscriptPane) *MessageActions {
	return &MessageActions{cursor: -1, pane: pane}
}

// Enter starts the mode, placing the cursor on the most recent entry: that is
// the block a user almost always wants, and it costs them zero keystrokes.
func (a *MessageActions) Enter() bool {
	if a == nil || a.pane == nil || len(a.pane.list.Entries) == 0 {
		return false
	}
	a.Active = true
	a.cursor = len(a.pane.list.Entries) - 1
	a.pane.list.Selected = a.cursor
	a.pane.list.ScrollToEntry(a.cursor)
	return true
}

// Exit leaves the mode and clears the highlight.
func (a *MessageActions) Exit() {
	if a == nil {
		return
	}
	a.Active = false
	a.cursor = -1
	if a.pane != nil {
		a.pane.list.Selected = -1
	}
}

// Move steps the cursor, clamping at both ends rather than wrapping — wrapping
// across a long transcript makes it easy to lose your place. Returns whether
// the cursor actually moved, so the caller can skip a redraw.
func (a *MessageActions) Move(delta int) bool {
	if a == nil || !a.Active || a.pane == nil {
		return false
	}
	n := len(a.pane.list.Entries)
	if n == 0 {
		return false
	}
	next := min(max(a.cursor+delta, 0), n-1)
	if next == a.cursor {
		return false
	}
	a.cursor = next
	a.pane.list.Selected = a.cursor
	a.pane.list.ScrollToEntry(a.cursor)
	return true
}

// Current returns the entry under the cursor, or nil.
func (a *MessageActions) Current() components.Widget {
	if a == nil || !a.Active || a.pane == nil {
		return nil
	}
	if a.cursor < 0 || a.cursor >= len(a.pane.list.Entries) {
		return nil
	}
	return a.pane.list.Entries[a.cursor]
}

// CopyText is the copy payload for the entry under the cursor. User messages
// keep their raw text rather than a re-rendered block, because that is what gets
// pasted back.
func (a *MessageActions) CopyText() string {
	w := a.Current()
	if w == nil {
		return ""
	}
	if ub, ok := w.(interface{ CopyText() string }); ok {
		if text := ub.CopyText(); strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}

// CursorIndex returns the 0-based entry index under the cursor, or -1 when the
// mode is inactive. Exposed for tests and for the footer hint.
func (a *MessageActions) CursorIndex() int {
	if a == nil || !a.Active {
		return -1
	}
	return a.cursor
}

// Position reports the cursor's 1-based position and the entry count, for the
// footer hint.
func (a *MessageActions) Position() string {
	if a == nil || !a.Active || a.pane == nil || a.cursor < 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", a.cursor+1, len(a.pane.list.Entries))
}

// ItemID returns the session item id under the cursor, which is what an edit
// request has to name. Empty when the cursor is not on a user message.
func (a *MessageActions) ItemID() string {
	w := a.Current()
	if w == nil {
		return ""
	}
	type ided interface{ ItemIDString() string }
	if iw, ok := w.(ided); ok {
		return iw.ItemIDString()
	}
	return ""
}

// Handle routes a key while the mode is active. It returns true when it consumed
// the key, so the composer never sees a keystroke meant for the cursor.
//
// The copy chord is accepted in two shapes because the reference binds a bare
// key once you are inside the mode: no modifier, just c.
func (a *MessageActions) Handle(ctx *components.EventContext, e xui.KeyEvent) bool {
	if a == nil || !a.Active || !e.Press {
		return false
	}
	switch e.Code {
	case xui.KeyEscape:
		a.Exit()
		return true
	case xui.KeyUp:
		return a.Move(-1)
	case xui.KeyDown:
		return a.Move(1)
	case xui.KeyRune:
		if e.Mods.Has(xui.ModCtrl) || e.Mods.Has(xui.ModAlt) {
			return false
		}
		switch e.Rune {
		case 'k', 'K':
			return a.Move(-1)
		case 'j', 'J':
			return a.Move(1)
		case 'c', 'C':
			a.copy()
			return true
		}
	}
	return false
}

// copy copies the current entry, leaving the mode active so the user can copy
// several blocks in a row rather than re-entering for each.
func (a *MessageActions) copy() {
	if a.pane == nil {
		return
	}
	text := a.CopyText()
	if strings.TrimSpace(text) == "" {
		return
	}
	a.pane.CopyBlock(text)
}
