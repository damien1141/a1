package components

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultThemeIsDark(t *testing.T) {
	assert.Equal(t, DarkTheme().ToolName.Fg, DefaultTheme().ToolName.Fg)
	assert.Equal(t, DarkTheme().Identity.Fg, DefaultTheme().Identity.Fg)
}

func TestThemesSeparateIdentityFromSuccess(t *testing.T) {
	for _, name := range ThemeNames() {
		th, ok := ThemeByName(name)
		require.True(t, ok, name)
		assert.NotEqual(t, th.Success.Fg, th.Identity.Fg, "%s: Identity must not equal Success", name)
	}
}

func TestTitleOrForeground(t *testing.T) {
	th := DarkTheme()
	st := th.TitleOrForeground()
	assert.Equal(t, th.Title.Fg, st.Fg)
	assert.True(t, st.Bold)
}
