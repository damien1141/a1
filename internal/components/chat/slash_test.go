package chat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActiveSlash(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		cursor int
		ok     bool
		query  string
	}{
		{"empty", "", 0, false, ""},
		{"slash alone", "/", 1, true, ""},
		{"prefix", "/resu", 5, true, "resu"},
		{"mid prefix", "/resume", 4, true, "res"},
		{"cursor at start", "/sessions", 0, false, ""},
		{"after space arg", "/resume abc", 11, false, ""},
		{"in cmd before space", "/resume abc", 7, true, "resume"},
		{"not at start", "hi /sessions", 12, false, ""},
		{"plain text", "hello", 5, false, ""},
		{"at mention", "@file", 5, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, start, end, ok := ActiveSlash(tt.value, tt.cursor)
			require.Equal(t, tt.ok, ok)
			if !ok {
				return
			}
			require.Equal(t, tt.query, q)
			require.Equal(t, 0, start, "start")
			require.Equal(t, tt.cursor, end, "end")
			require.True(t, strings.HasPrefix(tt.value, "/"), "expected slash prefix")
		})
	}
}
