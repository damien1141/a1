package block

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
)

func TestExtractUserMessageSkipsRuleChrome(t *testing.T) {
	ub := &UserBlock{Text: "13个技能 你把这个 skills挪动过去", Theme: components.DefaultTheme()}
	s := ub.Draw(components.DrawContext{Max: components.Size{Width: 60, Height: 5}, Method: xui.WidthUnicode})
	got := components.ExtractSurfaceText(s, 0, 0, 59, 0)
	require.NotContains(t, got, "▎")
	require.Equal(t, ub.Text, got)
}
