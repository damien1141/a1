package diffview

import (
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/util/diffreview"
)

func TestPaintUnifiedDiff(t *testing.T) {
	doc, err := diffreview.Parse(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1 +1 @@
-old
+new
`)
	require.NoError(t, err)
	rows := doc.Rows()
	vis := make([]Vis, 0, len(rows))
	for i := range rows {
		vis = append(vis, Vis{Kind: VisRow, Doc: i, Active: i == 0})
	}
	surf := Paint(components.DrawContext{Max: components.Size{Width: 80, Height: 16}, Method: xui.WidthUnicode}, Model{
		Theme:       components.DefaultTheme(),
		Title:       "diff · working tree",
		Rows:        rows,
		Vis:         vis,
		Highlighted: HighlightRows(rows, components.DefaultTheme()),
	})
	text := components.SurfaceText(surf)
	assert.Contains(t, text, "working tree")
	assert.Contains(t, text, "main.go")
}

func TestPaintDoesNotMutateCachedHighlightSpans(t *testing.T) {
	doc, err := diffreview.Parse(`diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1 +1 @@
-oldValue
+newValue
`)
	require.NoError(t, err)
	rows := doc.Rows()
	th := components.DefaultTheme()
	highlighted := HighlightRows(rows, th)
	require.NotEmpty(t, highlighted)

	var codeDoc int
	for i, row := range rows {
		if row.Kind != diffreview.RowAdd && row.Kind != diffreview.RowDelete {
			continue
		}
		if _, ok := highlighted[i]; ok {
			codeDoc = i
			break
		}
	}
	before := append([]components.Span(nil), highlighted[codeDoc]...)

	vis := []Vis{{Kind: VisRow, Doc: codeDoc, Active: true}}
	ctx := components.DrawContext{Max: components.Size{Width: 80, Height: 8}, Method: xui.WidthUnicode}
	_ = Paint(ctx, Model{Theme: th, Rows: rows, Vis: vis, Highlighted: highlighted})
	vis[0].Active = false
	_ = Paint(ctx, Model{Theme: th, Rows: rows, Vis: vis, Highlighted: highlighted})

	got := highlighted[codeDoc]
	require.Len(t, got, len(before))
	for i := range got {
		assert.Equal(t, before[i].Style.Bg, got[i].Style.Bg, "span %d bg mutated after cursor moved", i)
	}
}

func TestPaintHelpDoesNotUseSelectionBg(t *testing.T) {
	th := components.DefaultTheme()
	surf := Paint(components.DrawContext{Max: components.Size{Width: 80, Height: 16}, Method: xui.WidthUnicode}, Model{
		Theme: th,
		Help:  true,
	})
	require.NotEmpty(t, surf.Children)
	for _, c := range surf.Children[0].Surface.Buffer {
		assert.NotEqual(t, th.SelectionBg.Bg, c.Style.Bg)
	}
}

func TestApplyInlineMarksChangedToken(t *testing.T) {
	code := "foo := oldValue"
	spans := []components.Span{{Text: code, Style: xui.Style{}}}
	out := applyInline(spans, code, []diffreview.InlineSpan{{Start: 7, End: 15}}, xui.Style{Reverse: true})
	require.GreaterOrEqual(t, len(out), 2)
	joined := ""
	for _, sp := range out {
		joined += sp.Text
	}
	assert.Equal(t, code, joined)
}
