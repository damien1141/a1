package bashtool

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBashOutputFormattingPreservesShortOutput(t *testing.T) {
	in := "a\nb\nc\n"
	got := formatBashOutput(in, false)
	require.Equal(t, in, got, "short output changed")
}

func TestBashOutputFormattingWritesTemp(t *testing.T) {
	var b strings.Builder
	for range BashMaxOutputLines + 20 {
		b.WriteString("line\n")
	}
	full := b.String()
	got := formatBashOutput(full, false)
	require.Contains(t, got, "Full output:", "missing full-output notice")
	require.Contains(t, got, "Showing lines", "missing range notice")
	// Extract path and confirm file exists with full content.
	_, rest, _ := strings.Cut(got, "Full output: ")
	path := strings.TrimSpace(strings.Split(rest, "]")[0])
	t.Cleanup(func() { _ = os.Remove(path) })
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, full, string(data), "temp file content mismatch")
}

func TestFormatCollectedBashOutputLabelsRetainedFile(t *testing.T) {
	var b strings.Builder
	for range BashMaxOutputLines + 20 {
		b.WriteString("line\n")
	}
	retained := b.String()

	got := formatBashOutput(retained, true)
	require.Contains(t, got, "Retained output:", "missing retained-output notice")
	require.NotContains(t, got, "Full output:", "collection-truncated output mislabeled as full")
	require.Contains(t, got, "Showing lines 21-1020 of 1020", "collection notice changed real line range")
	require.True(t, strings.HasSuffix(got, collectTruncationNote), "missing collection truncation note")
	_, rest, _ := strings.Cut(got, "Retained output: ")
	path := strings.TrimSpace(strings.Split(rest, "]")[0])
	require.NotEmpty(t, path, "missing retained output path")
	t.Cleanup(func() { _ = os.Remove(path) })
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, retained, string(data), "retained file contains display metadata")
}

func TestCollectionNoticeDoesNotConsumeDisplayBudget(t *testing.T) {
	output := strings.Repeat("x", BashMaxOutputBytes)
	got := formatBashOutput(output, true)
	require.Equal(t, output+collectTruncationNote, got, "collection notice altered output at display limit")
	require.False(
		t,
		strings.Contains(got, "Retained output:") || strings.Contains(got, "Showing lines"),
		"collection notice caused an unnecessary temp dump",
	)
}

func TestTruncateBashTailPreservesTailSemantics(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		maxLines  int
		maxBytes  int
		wantTail  string
		wantRange string
	}{
		{
			name:      "line limit with trailing newline",
			output:    "one\ntwo\nthree\nfour\n",
			maxLines:  3,
			maxBytes:  64,
			wantTail:  "two\nthree\nfour",
			wantRange: "Showing lines 2-4 of 4",
		},
		{
			name:      "byte limit prefers complete lines",
			output:    "aaaa\nbbbb\ncccc",
			maxLines:  100,
			maxBytes:  8,
			wantTail:  "cccc",
			wantRange: "Showing lines 3-3 of 3",
		},
		{
			name:      "single long line keeps byte tail",
			output:    "0123456789ABC",
			maxLines:  100,
			maxBytes:  10,
			wantTail:  "3456789ABC",
			wantRange: "Showing lines 1-1 of 1",
		},
		{
			name:      "final empty line",
			output:    "one\n\n",
			maxLines:  1,
			maxBytes:  64,
			wantTail:  "",
			wantRange: "Showing lines 2-2 of 2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			display, path := truncateBashTail(tc.output, tc.maxLines, tc.maxBytes, "Full output")
			require.NotEmpty(t, path, "missing temp output path")
			t.Cleanup(func() { _ = os.Remove(path) })
			require.True(
				t,
				strings.HasPrefix(display, tc.wantTail+"\n\n["),
				"display tail=%q, want prefix %q",
				display,
				tc.wantTail,
			)
			require.Contains(t, display, tc.wantRange, "display=%q, want range %q", display, tc.wantRange)
		})
	}
}
