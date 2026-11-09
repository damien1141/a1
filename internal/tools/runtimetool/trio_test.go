package runtimetool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- negative ----------

// Malformed arguments fail at the parse boundary, before any subprocess is
// spawned — the documented guard for hostile input.
func TestRunRuntime_Negative(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"malformed json", `{"path"`, "failed to parse runtime arguments"},
		{"wrong path type", `{"path": 42}`, "failed to parse runtime arguments"},
		{"wrong run type", `{"run": 7}`, "failed to parse runtime arguments"},
		{"wrong limit type", `{"limit": "ten"}`, "failed to parse runtime arguments"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := RuntimeTool().Run(t.Context(), json.RawMessage(tc.input))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Empty(t, res.Content)
		})
	}
}

// A canceled context kills the go test subprocess before it produces any
// output; the handler's documented degradation is "parse what we got", so
// empty output becomes a clean zero-failure result rather than an error.
// This is also hermetic: the canceled context means nothing actually runs.
func TestRunRuntime_CanceledContext_Negative(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	res, err := RuntimeTool().Run(ctx, json.RawMessage(`{"path": "./..."}`))
	require.NoError(t, err)
	assert.Equal(t, "No test failures found.\n", res.Content)
	assert.Equal(t, "0 failures", res.Detail)
	assert.Equal(t, "No test failures found.\n", res.Output)
}

// parseTestEvents must survive hostile raw output: non-JSON lines, bare
// scalars, arrays, and wrong-typed fields are all skipped, while well-typed
// events (unknown fields included) survive.
func TestParseTestEvents_Negative(t *testing.T) {
	raw := "garbage before any json\n" +
		`{"Action":"run","Package":"pkg","Name":"TestA"}` + "\n" +
		"\n" +
		"42\n" +
		`[]` + "\n" +
		`"quoted"` + "\n" +
		`{"Action":123}` + "\n" +
		`{"Action":"output","Output":12}` + "\n" +
		`{"Action":"pass","Package":"pkg","Name":"TestA","Extra":"ignored"}` + "\n" +
		"trailing garbage\n"

	events, err := parseTestEvents(raw)
	require.NoError(t, err)
	require.Len(t, events, 2, "only the two well-typed events survive")
	assert.Equal(t, testEvent{Action: "run", Package: "pkg", Name: "TestA"}, events[0])
	assert.Equal(t, testEvent{Action: "pass", Package: "pkg", Name: "TestA"}, events[1])
}

// ---------- differential ----------

// oracle: generic decoding — every line is decoded into map[string]any (the
// implementation decodes into a typed struct) and only counts as an event
// when each known field that is present is a string; unknown fields are
// ignored. Compared over a corpus mixing valid, hostile, and edge lines.
func TestParseTestEvents_Differential(t *testing.T) {
	raw := strings.Join([]string{
		`{"Action":"run","Package":"example.com/m/pkg","Name":"TestA"}`,
		`{"Action":"output","Package":"example.com/m/pkg","Name":"TestA","Output":"=== RUN   TestA\n"}`,
		`{"Action":"pass","Package":"example.com/m/pkg","Name":"TestA","Elapsed":0.1}`,
		`{"Action":"fail","Package":"example.com/m/pkg","Name":"TestB"}`,
		`{"Action":"output","Output":""}`,
		`{"Action":"skip","Package":"example.com/m/pkg","Name":"TestSkipped"}`,
		`{"Package":"no-action-field"}`,
		`{"Action":"run","Package":99}`,
		`{"Action":"run","Output":true}`,
		`{"Action":"run","Name":"TestX","Unknown":{"nested":1}}`,
		`{"Action":"run","Package":"pkg","Name":"TestA","Action":"pass"}`,
		`not json`,
		``,
		`   {"Action":"run","Package":"pad","Name":"TestP"}   `,
		`{"Action":"run","Package":"crlf","Name":"TestC"}` + "\r",
	}, "\n")

	events, err := parseTestEvents(raw)
	require.NoError(t, err)

	// Naive reference: per-line map decode with manual string-field checks.
	type tuple struct{ action, pkg, name, output string }
	var want []tuple
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		var ev tuple
		bad := false
		for _, f := range []struct {
			key string
			dst *string
		}{
			{"Action", &ev.action}, {"Package", &ev.pkg}, {"Name", &ev.name}, {"Output", &ev.output},
		} {
			v, ok := m[f.key]
			if !ok {
				continue
			}
			s, isStr := v.(string)
			if !isStr {
				bad = true
				break
			}
			*f.dst = s
		}
		if !bad {
			want = append(want, ev)
		}
	}

	got := make([]tuple, 0, len(events))
	for _, ev := range events {
		got = append(got, tuple{ev.Action, ev.Package, ev.Name, ev.Output})
	}
	assert.Equal(t, want, got)
}

// runtimeStream is a hand-written go test -json transcript: one passing
// test, two failing tests with output lines before and after each fail
// event, and a package-level fail carrying no test name.
const runtimeStream = `{"Action":"run","Package":"example.com/m/pkg","Name":"TestOk"}
{"Action":"output","Package":"example.com/m/pkg","Name":"TestOk","Output":"=== RUN   TestOk\n"}
{"Action":"pass","Package":"example.com/m/pkg","Name":"TestOk"}
{"Action":"run","Package":"example.com/m/pkg","Name":"TestFirst"}
{"Action":"output","Package":"example.com/m/pkg","Name":"TestFirst","Output":"    x_test.go:12: boom\n"}
{"Action":"fail","Package":"example.com/m/pkg","Name":"TestFirst"}
{"Action":"output","Package":"example.com/m/pkg","Name":"TestFirst","Output":"--- FAIL: TestFirst (0.00s)\n"}
{"Action":"run","Package":"example.com/m/pkg","Name":"TestSecond"}
{"Action":"fail","Package":"example.com/m/pkg","Name":"TestSecond"}
{"Action":"output","Package":"example.com/m/pkg","Name":"TestSecond","Output":"--- FAIL: TestSecond (0.00s)\n"}
{"Action":"fail","Package":"example.com/m/pkg"}
`

// oracle: hand-written golden literal — the expected failure report for the
// transcript above, derived by reading the go test -json event semantics
// (output before a fail event is not yet attached; a fail event with a test
// already in progress is a no-op), not by running the renderer.
func TestCollectFailures_Render_Differential(t *testing.T) {
	events, err := parseTestEvents(runtimeStream)
	require.NoError(t, err)
	require.Len(t, events, 11)

	t.Run("all failures", func(t *testing.T) {
		failures := collectFailures(events, 20)
		rendered := renderRuntimeResults(failures)
		assert.Equal(t, `Runtime test failures:
============================================================

1. example.com/m/pkg — TestFirst
   --- FAIL: TestFirst (0.00s)

2. example.com/m/pkg — TestSecond
   --- FAIL: TestSecond (0.00s)

`, rendered)
	})

	t.Run("limit one", func(t *testing.T) {
		failures := collectFailures(events, 1)
		require.Len(t, failures, 1)
		assert.Equal(t, "TestFirst", failures[0].Name)
		assert.Equal(t, `Runtime test failures:
============================================================

1. example.com/m/pkg — TestFirst
   --- FAIL: TestFirst (0.00s)

`, renderRuntimeResults(failures))
	})
}

// oracle: naive reference implementation in this test — a map-based decode
// with independent trim and default handling over normal, edge, and hostile
// inputs, which the detail helper must survive.
func TestRuntimeDetailFromArgs_Differential(t *testing.T) {
	cases := []struct{ name, input string }{
		{"path", `{"path": "./internal/..."}`},
		{"padded path", `{"path": "  ./pkg  "}`},
		{"empty object", `{}`},
		{"empty path", `{"path": ""}`},
		{"whitespace path", `{"path": "   "}`},
		{"null path", `{"path": null}`},
		{"non-string path", `{"path": 42}`},
		{"run and limit only", `{"run": "TestX", "limit": 5}`},
		{"not json", `nope`},
	}
	tool := RuntimeTool()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			_ = json.Unmarshal([]byte(tc.input), &m)
			p, _ := m["path"].(string)
			p = strings.TrimSpace(p)
			want := "runtime"
			if p != "" {
				want = "runtime " + p
			}
			assert.Equal(t, want, tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}
