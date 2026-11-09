package agenttool_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/tools"
)

// ---------- differential ----------

// naiveParseAgentResult re-derives ParseAgentResult through a map-based
// encoding/json decode — a different unmarshal path than the production
// struct decode — under the same contract: only trimmed `{…}` payloads that
// decode with string-valued (or absent/null) known fields, and carry at
// least one of job_id/status/summary, count as agent tool results.
func naiveParseAgentResult(output string) tools.AgentResult {
	out := strings.TrimSpace(output)
	if out == "" || out[0] != '{' {
		return tools.AgentResult{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		return tools.AgentResult{}
	}
	// naiveValue mimics encoding/json's case-insensitive struct-field
	// fallback: an exact key wins, otherwise the first equal-folding key.
	naiveValue := func(key string) (any, bool) {
		if v, ok := m[key]; ok {
			return v, true
		}
		for k, v := range m {
			if strings.EqualFold(k, key) {
				return v, true
			}
		}
		return nil, false
	}
	str := func(key string) (string, bool) {
		v, present := naiveValue(key)
		if !present || v == nil {
			return "", true
		}
		s, ok := v.(string)
		return s, ok
	}
	var res tools.AgentResult
	for _, key := range [...]string{"job_id", "status", "summary", "error"} {
		s, ok := str(key)
		if !ok {
			return tools.AgentResult{}
		}
		switch key {
		case "job_id":
			res.JobID = s
		case "status":
			res.Status = s
		case "summary":
			res.Summary = s
		case "error":
			res.Error = s
		}
	}
	if res.JobID == "" && res.Status == "" && res.Summary == "" {
		return tools.AgentResult{}
	}
	res.OK = true
	return res
}

// oracle: naive reference implementation in this test — a map-based
// encoding/json decode plus independently written field-extraction rules,
// compared over well-formed, hostile, and near-miss agent tool payloads.
func TestParseAgentResult_Differential(t *testing.T) {
	longSummary := "## Findings\n\n" + strings.Repeat("- detail line\n", 30)

	cases := []struct {
		name   string
		output string
	}{
		{"all fields pretty", "{\n  \"job_id\": \"job_42\",\n  \"status\": \"completed\",\n  \"error\": \"\",\n  \"summary\": \"did the thing\"\n}"},
		{"all fields compact", `{"job_id":"j","status":"failed","error":"boom","summary":"halfway"}`},
		{"status only", `{"status":"running"}`},
		{"summary only", "{\"summary\":\"" + longSummary + "\"}"},
		{"error only is not an agent result", `{"error":"worker exploded"}`},
		{"padded output is trimmed", "   {\"job_id\":\"j\"}\n\n"},
		{"non-json text", "agent finished with summary: all good"},
		{"empty output", "   "},
		{"json array", `[{"job_id":"j"}]`},
		{"json null", `null`},
		{"json string", `{"job_id":"j"}{}`},
		{"truncated json", "{\"job_id\":"},
		{"wrong field type fails the whole parse", `{"job_id":"j","status":5}`},
		{"nested field type", `{"summary":{"deep":"nope"}}`},
		{"null fields are tolerated", `{"job_id":null,"status":"completed","summary":null}`},
		{"unknown fields ignored", `{"job_id":"j","status":"queued","weather":"rain"}`},
		{"duplicate keys take the last", `{"status":"running","status":"completed"}`},
		{"uppercase keys still decode", `{"JOB_ID":"j","Status":"completed","SUMMARY":"done"}`},
		{"empty object", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := naiveParseAgentResult(tc.output)
			got := tools.ParseAgentResult(tc.output)
			assert.Equal(t, want.OK, got.OK)
			assert.Equal(t, want.JobID, got.JobID)
			assert.Equal(t, want.Status, got.Status)
			assert.Equal(t, want.Summary, got.Summary)
			assert.Equal(t, want.Error, got.Error)
		})
	}
}

// oracle: hand-written golden terminal-status set (the job package's terminal
// fold is completed/failed/cancelled/timed_out/stalled) plus a naive
// strings.TrimSpace gate — the renderable summary must appear only for
// recognized terminal statuses of successfully parsed results.
func TestRenderableSummary_Differential(t *testing.T) {
	terminal := []string{"completed", "failed", "cancelled", "timed_out", "stalled"}
	nonTerminal := []string{"", "starting", "queued", "pending", "running", "finished", "Completed", "done"}

	naive := func(r tools.AgentResult) string {
		isTerminal := false
		for _, s := range terminal {
			if r.Status == s {
				isTerminal = true
				break
			}
		}
		if !r.OK || !isTerminal {
			return ""
		}
		return strings.TrimSpace(r.Summary)
	}

	for _, status := range terminal {
		t.Run("terminal/"+status, func(t *testing.T) {
			r := tools.ParseAgentResult(`{"job_id":"j","status":"` + status + `","summary":"  padded summary  "}`)
			require.True(t, r.OK)
			assert.Equal(t, naive(r), r.RenderableSummary())
			assert.Equal(t, "padded summary", r.RenderableSummary())
		})
	}
	for _, status := range nonTerminal {
		t.Run("non-terminal/"+status, func(t *testing.T) {
			if status == "" {
				r := tools.ParseAgentResult(`{"summary":"nobody home"}`)
				require.True(t, r.OK)
				assert.Empty(t, r.RenderableSummary())
				return
			}
			r := tools.ParseAgentResult(`{"job_id":"j","status":"` + status + `","summary":"not yet"}`)
			require.True(t, r.OK)
			assert.Equal(t, naive(r), r.RenderableSummary())
			assert.Empty(t, r.RenderableSummary())
		})
	}

	t.Run("blank summary on terminal status", func(t *testing.T) {
		r := tools.ParseAgentResult(`{"job_id":"j","status":"failed","summary":"   "}`)
		require.True(t, r.OK)
		assert.Equal(t, naive(r), r.RenderableSummary())
		assert.Empty(t, r.RenderableSummary())
	})

	t.Run("unparsed result never renders", func(t *testing.T) {
		r := tools.ParseAgentResult("not json")
		require.False(t, r.OK)
		assert.Equal(t, naive(r), r.RenderableSummary())
	})
}
