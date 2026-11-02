package chat

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActiveSlashArgs(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		cursor   int
		ok       bool
		command  string
		query    string
		cmdStart int
		cmdEnd   int
		argStart int
		argEnd   int
	}{
		{"no slash", "hello", 5, false, "", "", 0, 0, 0, 0},
		{"no args", "/clear", 6, false, "", "", 0, 0, 0, 0},
		{"arg cursor at space", "/diff ", 6, true, "diff", "", 1, 5, 6, 6},
		{"first arg mid", "/diff --stat", 10, true, "diff", "--st", 1, 5, 6, 12},
		{"first arg end", "/diff --staged", 14, true, "diff", "--staged", 1, 5, 6, 14},
		{"second arg", "/diff --stat HEAD", 17, true, "diff", "HEAD", 1, 5, 13, 17},
		{"cursor before first arg", "/diff --stat", 5, true, "diff", "", 1, 5, 6, 6},
		{"multiple spaces", "/permission   trajectory", 24, true, "permission", "trajectory", 1, 11, 14, 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, q, cs, ce, as, ae, ok := ActiveSlashArgs(tt.value, tt.cursor)
			require.Equal(t, tt.ok, ok, "ok")
			if !tt.ok {
				return
			}
			require.Equal(t, tt.command, cmd)
			require.Equal(t, tt.query, q)
			require.Equal(t, tt.cmdStart, cs, "cmdStart")
			require.Equal(t, tt.cmdEnd, ce, "cmdEnd")
			require.Equal(t, tt.argStart, as, "argStart")
			require.Equal(t, tt.argEnd, ae, "argEnd")
		})
	}
}
