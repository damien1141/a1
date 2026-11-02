//go:build windows

package clipboard

import (
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
)

func TestEncodeWindowsClipboardTextPreservesUnicode(t *testing.T) {
	want := "你好世界\n中文 + English\nemoji 😀🚀\né ñ ü\n多行\n文本"

	got, err := encodeWindowsClipboardText(want)
	require.NoError(t, err)
	require.NotEmpty(t, got)
	require.Equal(t, uint16(0), got[len(got)-1])
	require.Equal(t, want, string(utf16.Decode(got[:len(got)-1])))
}
