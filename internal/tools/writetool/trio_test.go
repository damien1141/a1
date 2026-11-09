package writetool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// oracle: os.ReadFile — every payload the tool writes is read back with
// plain stdlib and compared byte-for-byte against the literal built here,
// independent of the tool's own result body and diff rendering.
func TestRunWrite_BytesOnDisk_Differential(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	tool := WriteTool()

	cases := []struct {
		name    string
		content string
	}{
		{"plain", "hello\n"},
		{"empty", ""},
		{"no trailing newline", "no trailing newline"},
		{"crlf preserved", "crlf\r\nlines\r\n"},
		{"multibyte", "héllo wörld ✓ 日本語\n"},
		{"nul byte", "a\x00b\n"},
		{"json-shaped payload", "{\"looks\":\"like json\"}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A distinct nested path per case also exercises parent-dir
			// creation.
			path := filepath.Join(dir, "case", tc.name, "out.txt")
			res, err := tool.Run(t.Context(), mustWriteArgs(t, path, tc.content))
			require.NoError(t, err)
			assert.Equal(t, filepath.ToSlash(filepath.Join("case", tc.name, "out.txt")), res.Detail)

			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, []byte(tc.content), got, "file on disk must match the requested bytes exactly")
		})
	}
}

// naiveNormalize is the documented CRLF/CR → LF normalization, re-derived
// with strings.ReplaceAll instead of the util package's helpers.
func naiveNormalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// naiveSplice replaces exactly one occurrence of old with new via index
// slicing — not strings.Replace, which the implementation uses.
func naiveSplice(s, old, new string) string {
	i := strings.Index(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

// oracle: naive reference implementation in this test — the edit contract
// (normalize line endings, replace the single unique occurrence) re-derived
// with strings.Index slice splicing, verified on disk via os.ReadFile.
func TestRunEdit_Splice_Differential(t *testing.T) {
	dir := t.TempDir()
	tool := EditTool()

	cases := []struct {
		name     string
		original string
		oldStr   string
		newStr   string
	}{
		{"middle line", "alpha\nbeta\ngamma\n", "beta", "BETA"},
		{"first line", "head\nbody\n", "head", "HEAD"},
		{"last line no newline", "a\nb\ntail", "tail", "TAIL"},
		{"delete to empty", "keep\ndrop\nkeep2\n", "drop\n", ""},
		{"multiline old_str", "one\ntwo\nthree\n", "one\ntwo", "uno\ndos"},
		{"multibyte", "日本語\nテキスト\n", "テキスト", "文章"},
		{"crlf file", "a\r\nb\r\nc\r\n", "b", "B"},
		{"crlf old_str", "a\r\nb\r\nc\r\n", "b\r\n", "B\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".txt")
			require.NoError(t, os.WriteFile(path, []byte(tc.original), 0o644))

			raw, err := json.Marshal(editInput{Path: path, OldStr: tc.oldStr, NewStr: tc.newStr})
			require.NoError(t, err)
			_, err = tool.Run(t.Context(), raw)
			require.NoError(t, err)

			want := naiveSplice(naiveNormalize(tc.original), naiveNormalize(tc.oldStr), naiveNormalize(tc.newStr))
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, want, string(got))
		})
	}
}
