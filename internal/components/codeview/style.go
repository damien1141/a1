package codeview

import (
	"github.com/pulseaiclub/xui"

	"github.com/pulseaiclub/phi/internal/components"
)

// rowStyle is the row background. The cursor line carries the selection wash —
// the same convention diffview uses for its active row — so the eye finds the
// caret line without a second background role creeping into the theme.
func rowStyle(th components.Theme, cursor bool) xui.Style {
	st := th.Foreground
	if cursor {
		st.Bg = th.SelectionBg.Bg
	}
	return st
}

// gutterStyle keeps line numbers quiet so the code stays the subject, while
// inheriting the row wash: a washed cursor line must not break at the gutter.
func gutterStyle(th components.Theme, row xui.Style) xui.Style {
	st := th.Muted
	st.Bg = row.Bg
	return st
}

// ruleStyle is the separator drawn between the line number and the code.
func ruleStyle(th components.Theme, row xui.Style) xui.Style {
	st := th.Border
	st.Bg = row.Bg
	return st
}
