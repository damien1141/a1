package chat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActiveQuestion(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		cursor int
		ok     bool
		query  string
	}{
		{"empty", "", 0, false, ""},
		{"question alone", "?", 1, true, ""},
		{"prefix", "?ctrl", 5, true, "ctrl"},
		{"cursor at start", "?help", 0, false, ""},
		{"after space", "?help me", 8, false, ""},
		{"not at start", "hi?", 3, false, ""},
		{"slash", "/sessions", 9, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, start, end, ok := ActiveQuestion(tt.value, tt.cursor)
			require.Equal(t, tt.ok, ok, "ok")
			if !ok {
				return
			}
			assert.Equal(t, tt.query, q, "query")
			assert.Equal(t, 0, start, "start")
			assert.Equal(t, tt.cursor, end, "end")
			assert.True(t, strings.HasPrefix(tt.value, "?"), "expected ? prefix")
		})
	}
}
