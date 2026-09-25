package diffpane

import (
	"path/filepath"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
)

const sampleDiff = `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@
 package main
-old
+new
`

func TestPaneOpenCommentSearchAndSend(t *testing.T) {
	dir := t.TempDir()
	var submitted string
	p := New(components.DefaultTheme(), dir, func(s string) { submitted = s }, nil, nil)
	p.OpenText(sampleDiff, nil)
	require.True(t, p.Active())
	require.NotEmpty(t, p.rows)

	ctx := &components.EventContext{}
	key := func(r rune) {
		p.Handle(ctx, xui.KeyEvent{Press: true, Code: xui.KeyRune, Rune: r})
	}

	// land on the added line
	for i := 0; i < 20 && (p.cursor >= len(p.rows) || p.rows[p.cursor].Code != "new"); i++ {
		key('j')
	}
	require.Equal(t, "new", p.rows[p.cursor].Code)

	key('i')
	require.True(t, p.commentEdit)
	for _, r := range "please rename" {
		key(r)
	}
	p.Handle(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEnter})
	require.False(t, p.commentEdit)
	require.Len(t, p.drafts, 1)
	assert.FileExists(t, filepath.Join(dir, ".a1", "review.json"))

	key('/')
	for _, r := range "new" {
		key(r)
	}
	p.Handle(ctx, xui.KeyEvent{Press: true, Code: xui.KeyEnter})
	assert.False(t, p.searchMode)
	assert.NotEmpty(t, p.searchMatches)

	key('s')
	assert.True(t, p.sideBySide)

	key('a')
	assert.False(t, p.Active())
	assert.Contains(t, submitted, "main.go")
	assert.Contains(t, submitted, "please rename")
}

func TestPaneEscCloses(t *testing.T) {
	p := New(components.DefaultTheme(), t.TempDir(), nil, nil, nil)
	p.OpenText(sampleDiff, nil)
	require.True(t, p.Active())
	p.Handle(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyEscape})
	assert.False(t, p.Active())
}

func TestPaneDraw(t *testing.T) {
	p := New(components.DefaultTheme(), t.TempDir(), nil, nil, nil)
	p.OpenText(sampleDiff, nil)
	surf := p.Draw(components.DrawContext{Max: components.Size{Width: 80, Height: 24}, Method: xui.WidthUnicode})
	text := components.SurfaceText(surf)
	assert.Contains(t, text, "diff")
	assert.Contains(t, text, "main.go")
}
