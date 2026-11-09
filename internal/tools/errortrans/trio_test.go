package errortrans

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// translation is one golden expectation: the (language, pattern-name) pairs
// the translator must emit, in library order, for a fixture text.
type translation struct {
	name  string
	text  string
	lang  string
	limit int
	want  [][2]string
}

// parseTranslations decodes the TSV render back into comparable pairs: one
// line per entry, fields language/pattern/normalized/confidence/fix/language.
func parseTranslations(t *testing.T, content string) [][2]string {
	t.Helper()
	var out [][2]string
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		require.Len(t, fields, 6, "each translation renders six TSV fields: %q", line)
		out = append(out, [2]string{fields[0], fields[1]})
	}
	return out
}

// oracle: hand-written golden literal pairs — for each fixture error text the
// expected (language, pattern) translations were derived by hand from the
// library's documented per-language phrase table, its switch-order language
// detection, and library-order emission; never by running the translator.
func TestRunErrtrans_Differential(t *testing.T) {
	cases := []translation{
		{
			name:  "rust two patterns in library order",
			text:  "error[E0382]: cannot move out of borrowed data",
			lang:  "rust",
			limit: 20,
			want:  [][2]string{{"rust", "borrowed data"}, {"rust", "cannot move"}},
		},
		{
			name:  "rust cannot move alone",
			text:  "cannot move out of `x` which doesn't implement `Copy`",
			lang:  "rust",
			limit: 20,
			want:  [][2]string{{"rust", "cannot move"}},
		},
		{
			name:  "python explicit lang",
			text:  "Traceback (most recent call last):\nTypeError: unsupported operand type(s) for +",
			lang:  "python",
			limit: 20,
			want:  [][2]string{{"python", "typeerror"}},
		},
		{
			name:  "python autodetect via marker word",
			text:  "python3.11: SyntaxError: invalid syntax",
			limit: 20,
			want:  [][2]string{{"python", "syntaxerror"}},
		},
		{
			name:  "bash autodetect",
			text:  "bash: line 1: rg: command not found",
			limit: 20,
			want:  [][2]string{{"bash", "command not found"}},
		},
		{
			name:  "go autodetect via go: marker",
			text:  "main.go:12:5: undefined: foo",
			limit: 20,
			want:  [][2]string{{"go", "undefined:"}},
		},
		{
			name:  "lua autodetect",
			text:  "attempt to index a nil value (local 'x')",
			limit: 20,
			want:  [][2]string{{"lua", "attempt to index"}},
		},
		{
			// No detection marker fires, so lang falls through to "generic" —
			// and a generic query consults every language's table: the rust
			// and lua phrases both translate, tagged with their own languages.
			name:  "generic lang consults all tables",
			text:  "mismatched types; module not found in path",
			limit: 20,
			want:  [][2]string{{"rust", "mismatched types"}, {"lua", "module not found"}},
		},
		{
			// Detection is switch-ordered: "cargo" claims the text for rust,
			// so the embedded bash phrase is never consulted.
			name:  "cargo marker wins over bash marker",
			text:  "cargo build failed; bash: foo: command not found",
			limit: 20,
			want:  nil,
		},
		{
			name:  "lang hint normalized from padded uppercase",
			text:  "CANNOT MOVE",
			lang:  " Rust ",
			limit: 20,
			want:  [][2]string{{"rust", "cannot move"}},
		},
		{
			name:  "limit keeps library order",
			text:  "cannot move out of borrowed data",
			lang:  "rust",
			limit: 1,
			want:  [][2]string{{"rust", "borrowed data"}},
		},
		{
			name:  "python three patterns in library order",
			text:  "ModuleNotFoundError: No module named 'x'; KeyError: 'y'; IndentationError: unexpected indent",
			lang:  "python",
			limit: 20,
			want: [][2]string{
				{"python", "modulenotfounderror"},
				{"python", "indentationerror"},
				{"python", "keyerror"},
			},
		},
		{
			name:  "substring inside a larger word is not translated",
			text:  "class MyImportErrorHelper: pass",
			lang:  "python",
			limit: 20,
			want:  nil,
		},
		{
			name:  "unrelated text translates nothing",
			text:  "the build finished cleanly",
			lang:  "go",
			limit: 20,
			want:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(errtransInput{Text: tc.text, Lang: tc.lang, Limit: tc.limit})
			require.NoError(t, err)
			res, err := runErrtrans(t.Context(), raw)
			require.NoError(t, err)

			if len(tc.want) == 0 {
				assert.Equal(t, "No translations matched", res.Content)
				assert.Equal(t, "0 translations", res.Detail)
				return
			}
			got := parseTranslations(t, res.Content)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, fmt.Sprintf("%d translations", len(tc.want)), res.Detail)
		})
	}
}
