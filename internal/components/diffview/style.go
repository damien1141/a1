package diffview

import (
	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/util/diffreview"
)

func rowStyle(th components.Theme, kind diffreview.RowKind) xui.Style {
	switch kind {
	case diffreview.RowAdd:
		st := th.Success
		st.Bold = false
		return st
	case diffreview.RowDelete:
		st := th.Destructive
		st.Bold = false
		return st
	case diffreview.RowHunk:
		return th.ToolName
	case diffreview.RowFile:
		return th.TitleOrForeground()
	case diffreview.RowCommitHeader:
		return th.IdentityOrSuccess()
	case diffreview.RowCommitMeta, diffreview.RowCommitTrailer, diffreview.RowMeta, diffreview.RowNoNewline:
		return th.Muted
	case diffreview.RowDiffStatSummary:
		return th.TitleOrForeground()
	default:
		return th.Foreground
	}
}

func gutterStyle(th components.Theme, kind diffreview.RowKind) xui.Style {
	switch kind {
	case diffreview.RowAdd:
		st := th.Success
		st.Bold = true
		return st
	case diffreview.RowDelete:
		st := th.Destructive
		st.Bold = true
		return st
	default:
		return th.Muted
	}
}

func inlineStyle(th components.Theme, kind diffreview.RowKind) xui.Style {
	st := rowStyle(th, kind)
	st.Reverse = true
	st.Bold = true
	return st
}
