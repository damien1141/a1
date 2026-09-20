package codeview

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/components"
)

func TestHighlightGoFileReturnsSpans(t *testing.T) {
	th := components.DefaultTheme()
	lines := []string{"package main", "", `func main() { println("hi") }`}
	got := Highlight("internal/app/main.go", lines, th)
	require.NotNil(t, got)

	spans, ok := got[0]
	require.True(t, ok, "first line is tokenised")
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Text)
	}
	assert.Equal(t, lines[0], b.String(), "spans cover the whole line")
	assert.Equal(t, th.ToolName.Fg, spans[0].Style.Fg, "the package keyword gets the tool accent")
}

func TestHighlightUnknownLanguageIsNil(t *testing.T) {
	th := components.DefaultTheme()
	assert.Nil(t, Highlight("notes.zzz", []string{"hello"}, th))
	assert.Nil(t, Highlight("", []string{"hello"}, th))
}

func TestHighlightEmptyInputIsNil(t *testing.T) {
	assert.Nil(t, Highlight("main.go", nil, components.DefaultTheme()))
}

func TestHighlightHugeFileIsNil(t *testing.T) {
	lines := make([]string, highlightRowLimit+1)
	for i := range lines {
		lines[i] = "x"
	}
	assert.Nil(t, Highlight("main.go", lines, components.DefaultTheme()))

	atLimit := lines[:highlightRowLimit]
	assert.NotNil(t, Highlight("main.go", atLimit, components.DefaultTheme()))
}
