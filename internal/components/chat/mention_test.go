package chat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActiveMention(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		cursor int
		ok     bool
		query  string
		start  int
	}{
		{"empty", "", 0, false, "", 0},
		{"at alone", "@", 1, true, "", 0},
		{"at query", "@man", 4, true, "man", 0},
		{"mid query", "@manager", 4, true, "man", 0},
		{"after space", "see @go", 7, true, "go", 4},
		{"email", "a@b.com", 7, false, "", 0},
		{"after newline", "hi\n@x", 5, true, "x", 3},
		{"after paren", "(@file", 6, true, "file", 1},
		{"space ends", "@a b", 2, true, "a", 0},
		{"cursor before at", "@a", 0, false, "", 0},
		{"closed by space at cursor", "look @path ", 11, false, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, start, end, ok := ActiveMention(tt.value, tt.cursor)
			require.Equal(t, tt.ok, ok, "q=%q", q)
			if !ok {
				return
			}
			require.Equal(t, tt.query, q, "query")
			require.Equal(t, tt.start, start, "start")
			require.Equal(t, tt.cursor, end, "end")
		})
	}
}

func TestReplaceRange(t *testing.T) {
	c := &ChatInput{Value: "see @man", Cursor: 8}
	c.ReplaceRange(4, 8, "@internal/session/manager.go")
	require.Equal(t, "see @internal/session/manager.go", c.Value)
	require.Equal(t, len(c.Value), c.Cursor)
}
