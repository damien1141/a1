package stacktool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// oracle: hand-written golden literal — a four-language trace whose expected
// frame set (Go first, then Python, Node, and the Rust panic/at pair merged
// by the file:line dedup) and whose full rendered context (with the exact
// ">>"/"  " markers and %4d line numbers) were derived by hand from the
// fixture file contents and a context window of 1.
func TestRunStack_MultiLanguageGolden_Differential(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "main.go", "package main\nfunc a() { b() }\nfunc b() { a() }\n")
	writeFile(t, root, "script.py", "def main():\n    foo()\ndef foo():\n    raise ValueError('oops')\n")
	writeFile(t, root, "app.js", "function main() {\n  throw new Error('x');\n}\n")
	writeFile(t, root, "main.rs", "fn main() {\n    panic!(\"x\");\n}\n")

	trace := strings.Join([]string{
		"goroutine 1 [running]:",
		"main.a()",
		"\t" + filepath.Join(root, "main.go") + ":2 +0x10",
		"Traceback (most recent call last):",
		"  File \"" + filepath.Join(root, "script.py") + "\", line 3, in foo",
		"    raise ValueError('oops')",
		"Error: x",
		"    at main (" + filepath.Join(root, "app.js") + ":2:11)",
		"thread 'main' panicked at 'x', " + filepath.Join(root, "main.rs") + ":2:5",
		"             at " + filepath.Join(root, "main.rs") + ":2:5",
		"",
	}, "\n")

	raw, err := json.Marshal(stackInput{Text: trace, Context: 1})
	require.NoError(t, err)
	res, rerr := runStack(t.Context(), raw)
	require.NoError(t, rerr)

	want := strings.Join([]string{
		// Go frame: main.go:2.
		"@file main.go",
		"main.go:     1|package main",
		"main.go:>>   2|func a() { b() }",
		"main.go:     3|func b() { a() }",
		// Python frame: script.py:3 (context 1 → lines 2-4).
		"@file script.py",
		"script.py:     2|    foo()",
		"script.py:>>   3|def foo():",
		"script.py:     4|    raise ValueError('oops')",
		// Node frame: app.js:2.
		"@file app.js",
		"app.js:     1|function main() {",
		"app.js:>>   2|  throw new Error('x');",
		"app.js:     3|}",
		// Rust: the panic line, the Node-style overlap, and the at-line all
		// resolve to main.rs:2 — deduplicated to one frame.
		"@file main.rs",
		"main.rs:     1|fn main() {",
		"main.rs:>>   2|    panic!(\"x\");",
		"main.rs:     3|}",
	}, "\n")
	assert.Equal(t, want, res.Content)
	assert.Equal(t, "4 frames", res.Detail)
	assert.Equal(t, res.Content, res.Output)
}

// oracle: the Go runtime itself — a real debug.Stack traceback is parsed by
// the tool, and the frame pointing back at this test file is compared with
// runtime.Caller for the location and with a direct os.ReadFile of the
// source for the rendered context window.
func TestRunStack_RealTracebackVsRuntimeCaller_Differential(t *testing.T) {
	// runtime.Caller(0) reports its own line; debug.Stack on the very next
	// line reports that line as this frame's location.
	_, file, line, _ := runtime.Caller(0)
	trace := string(debug.Stack())
	frameLine := line + 1
	require.NotZero(t, line)

	raw, err := json.Marshal(stackInput{Text: trace, Context: 2})
	require.NoError(t, err)
	res, rerr := runStack(t.Context(), raw)
	require.NoError(t, rerr)
	require.Contains(t, res.Detail, "frames")

	// Reference context window derived by reading this very source file.
	data, rerr := os.ReadFile(file)
	require.NoError(t, rerr)
	src := strings.Split(string(data), "\n")

	cwd, werr := os.Getwd()
	require.NoError(t, werr)
	rel, rerr := filepath.Rel(cwd, file)
	require.NoError(t, rerr)

	start := max(1, frameLine-2)
	end := min(len(src), frameLine+2)
	wantLines := map[int]string{}
	for ln := start; ln <= end; ln++ {
		prefix := "  "
		if ln == frameLine {
			prefix = ">>"
		}
		wantLines[ln] = fmt.Sprintf("%s:%s%4d|%s", rel, prefix, ln, src[ln-1])
	}

	rowRe := regexp.MustCompile(`^` + regexp.QuoteMeta(rel) + `:(>>|  ) *(\d+)\|(.*)$`)
	gotLines := map[int]string{}
	for _, outLine := range strings.Split(res.Content, "\n") {
		if m := rowRe.FindStringSubmatch(outLine); m != nil {
			ln, aerr := strconv.Atoi(m[2])
			require.NoError(t, aerr)
			gotLines[ln] = outLine
		}
	}
	assert.Equal(t, wantLines, gotLines,
		"the runtime traceback's frame for this file must render with runtime.Caller's location and the file's real context")
}

// naiveStackDetail re-derives DetailFromArgs independently: a map-based
// encoding/json decode, TrimSpace, the 40-byte cut, and the "stack" prefix.
func naiveStackDetail(input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	s, _ := m["text"].(string)
	s = strings.TrimSpace(s)
	if len(s) > 40 {
		s = s[:40] + "..."
	}
	return "stack " + s
}

// oracle: naive reference implementation in this test — a map-based JSON
// decode plus hand-rolled truncation, compared over normal, edge, and
// hostile inputs.
func TestDetailFromArgs_Differential(t *testing.T) {
	long := strings.Repeat("goroutine", 8)
	unicode := strings.Repeat("パニック発生", 10)

	cases := []struct {
		name  string
		input string
	}{
		{"text plain", `{"text":"panic: boom"}`},
		{"text padded", `{"text":"  panic: boom  "}`},
		{"text long", `{"text":"` + long + `"}`},
		{"text multibyte", `{"text":"` + unicode + `"}`},
		{"empty text", `{"text":""}`},
		{"missing text", `{}`},
		{"non-string text", `{"text": 7}`},
		{"not json", `nope`},
	}
	tool := StackTool()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, naiveStackDetail(json.RawMessage(tc.input)), tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}
