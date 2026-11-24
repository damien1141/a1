// Package chrome locks TUI visual grammar: glyphs, hints, borders, and
// decision-row chrome. Call sites should not invent parallel dialects.
//
// Color roles live on components.Theme; this package only picks the right
// role for a job (ModalBorder, PanelTitle, DecisionPrimary, ToolIcon, …).
package chrome

import (
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/status"
)

// Status / expand glyphs.
const (
	Ok       = "✓"
	Err      = "✗"
	Stop     = "⊘"
	Busy     = "⋯"
	Expand   = " ▶"
	Collapse = " ▼"
)

// Decision-row glyphs (permission / continue / confirm).
const (
	SelectArrow = "▸"
	DotOn       = "●"
	DotOff      = "○"
)

// Prompt glyphs.
const (
	FilterPrompt = ">"
	SoftPrompt   = "› "
	BashPrompt   = "$ "
)

// Sep joins hint fragments. One separator for the whole product.
const Sep = " · "

// ListHint is the default bottom-border caption for filterable list overlays.
func ListHint(action string) string {
	if action == "" {
		action = "select"
	}
	return " ↑↓ move" + Sep + "⏎ " + action + Sep + "esc close "
}

// ListHintShort is the compact fallback when the full ListHint does not fit.
func ListHintShort(action string) string {
	if action == "" {
		action = "select"
	}
	return " ⏎ " + action + Sep + "esc close "
}

// AskHint builds muted footer copy for decision panels.
// nav is typically "↑↓ move" or "←→ move"; action/dismiss are verb phrases
// after the key (e.g. "select", "cancel").
func AskHint(nav, action, dismiss string) string {
	parts := make([]string, 0, 3)
	if nav != "" {
		parts = append(parts, nav)
	}
	if action != "" {
		parts = append(parts, "Enter "+action)
	}
	if dismiss != "" {
		parts = append(parts, "Esc "+dismiss)
	}
	return strings.Join(parts, Sep)
}

// NavHint is THE navigation string for every decision panel. Every one of them
// accepts Up/Left and Down/Right (see overlays.go's key switches), so naming a
// single axis — as ConfirmHint used to — taught a key that was not the only one.
const NavHint = "↑↓ move"

// ConfirmHint is the confirm-panel footer (includes Y/N chords).
func ConfirmHint() string {
	return NavHint + Sep + "Enter confirm" + Sep + "Y yes" + Sep + "N/Esc cancel"
}

// FeedbackHint is the deny-with-feedback footer.
func FeedbackHint() string {
	return "Enter send" + Sep + "Esc cancel"
}

// DecisionPrimary is the cool action accent for selected decision rows.
func DecisionPrimary(th components.Theme) xui.Style {
	if th.ToolName.Fg.Kind != 0 {
		return th.ToolName
	}
	return th.Success
}

// ModalBorder is the elevated border for ask/confirm panels (structure, not alarm).
func ModalBorder(th components.Theme) xui.Style {
	if th.Title.Fg.Kind != 0 {
		return th.Title
	}
	return th.Border
}

// PanelTitle styles overlay / palette titles.
func PanelTitle(th components.Theme) xui.Style {
	return th.TitleOrForeground()
}

// OptionLine paints one ▸● /  ○ decision row.
func OptionLine(
	th components.Theme,
	primary xui.Style,
	label string,
	selected bool,
	innerW int,
	method xui.WidthMethod,
) []components.RichLine {
	arrow, dot := " ", DotOff
	labelSt, dotSt := th.Foreground, th.Muted
	if selected {
		arrow, dot = SelectArrow, DotOn
		labelSt = xui.Style{Bold: true, Fg: primary.Fg}
		dotSt = primary
	}
	return components.WrapSpans([]components.Span{
		{Text: arrow, Style: primary},
		{Text: dot, Style: dotSt},
		{Text: " " + label, Style: labelSt},
	}, innerW, method)
}

// ExpandArrow returns the collapsed/expanded disclosure glyph.
func ExpandArrow(expanded bool) string {
	if expanded {
		return Collapse
	}
	return Expand
}

// ToolIcon returns the status glyph + style for a tool / agent title row.
func ToolIcon(st status.ToolStatus, th components.Theme, spin *status.Spinner) (string, xui.Style) {
	icon := Ok
	iconSt := th.Success
	switch st {
	case status.ToolRunning, status.ToolQueued:
		icon = Busy
		iconSt = th.ToolName
		if spin != nil {
			icon = spin.Glyph()
		}
	case status.ToolError:
		icon = Err
		iconSt = th.Destructive
	case status.ToolCancelled:
		icon = Stop
		iconSt = th.Muted
	case status.ToolRejected:
		icon = Stop
		iconSt = th.Destructive
	}
	return icon, iconSt
}

// ChildIcon is the compact nested-tree status mark (no spinner).
func ChildIcon(st status.ToolStatus) string {
	switch st {
	case status.ToolRunning, status.ToolQueued:
		return Busy
	case status.ToolError:
		return Err
	case status.ToolCancelled, status.ToolRejected:
		return Stop
	default:
		return Ok
	}
}

// StatusSuffix is the muted parenthetical for cancelled / rejected rows.
func StatusSuffix(st status.ToolStatus) string {
	switch st {
	case status.ToolCancelled:
		return " (cancelled)"
	case status.ToolRejected:
		return " (rejected)"
	default:
		return ""
	}
}

// TurnEndLabel renders the turn-end state for the composer's status slot: a
// glyph, the state word, and a style that actually distinguishes outcomes.
//
// It lives here because the footer used to render the bare word
// state.String() in the SUCCESS colour unconditionally — a failed turn and a
// clean one looked identical, at the one place a user's eye is trained to
// check. The glyph/colour mapping is the same vocabulary ToolIcon already uses,
// so a failed turn reads like a failed tool row rather than like prose.
//
// state is the session.State's string form; keeping it a string is deliberate.
// chrome must not import session (the dependency runs components → chrome), and
// the four words are a stable contract.
func TurnEndLabel(state string, th components.Theme) (glyph string, st xui.Style, word string) {
	switch state {
	case "error":
		return Err, th.Destructive, "error"
	case "cancelled":
		return Stop, th.Muted, "cancelled"
	case "streaming":
		return Busy, th.ToolName, "streaming"
	case "complete":
		return Ok, th.Success, "complete"
	default:
		// An unexpected state word is still shown — hiding it would be worse
		// than showing an unadorned one — but without a glyph that would imply
		// an outcome we do not know.
		if state == "" {
			return "", th.Muted, ""
		}
		return "", th.Muted, state
	}
}
