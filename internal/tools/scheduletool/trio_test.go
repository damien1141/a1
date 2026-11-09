package scheduletool

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// ---------- differential ----------

func runScheduleRaw(t *testing.T, tool tooldef.Tool, args map[string]any) tooldef.Result {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	res, err := tool.Run(t.Context(), raw)
	require.NoError(t, err)
	return res
}

func decodeScheduleBody(t *testing.T, res tooldef.Result) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &body))
	return body
}

// oracle: the internal/schedule package's Scheduler API plus hand-computed
// expectations — every field the tool's JSON reports is cross-checked against
// scheduler state read through the independent Get/List surface, and the
// one_time rendering against a UTC RFC3339 string derived from the input.
func TestCreateListCancelMirrorsScheduler_Differential(t *testing.T) {
	tool, sched := newTool(t)

	// --- create (fixed_rate, explicit role/workdir/timeout) ---
	res := runScheduleRaw(t, tool, map[string]any{
		"action":      "create",
		"prompt":      "run the nightly audit and post the summary",
		"description": "nightly audit",
		"fixed_rate":  "15m",
		"role":        "explore",
		"workdir":     "/custom",
		"timeout_sec": 90,
	})
	body := decodeScheduleBody(t, res)
	idA, _ := body["schedule_id"].(string)
	require.NotEmpty(t, idA)

	assert.Equal(t, "every 15m0s", body["trigger"], "trigger summary is the golden rendering")
	assert.Equal(t, "explore", body["role"])
	assert.Equal(t, true, body["enabled"])
	assert.NotEmpty(t, body["next_run_at"])
	assert.Equal(t, "nightly audit", res.Detail)
	assert.Equal(t, []any{"go-build", "go-test:..."}, body["contract"])

	gotA, ok := sched.Get(idA)
	require.True(t, ok)
	assert.Equal(t, gotA.Role, body["role"])
	assert.Equal(t, gotA.Enabled, body["enabled"])
	assert.Equal(t, gotA.NextRunAt.UTC().Format(time.RFC3339), body["next_run_at"])
	assert.Equal(t, "/custom", gotA.WorkDir, "workdir is carried into scheduler state")
	assert.Equal(t, 90*time.Second, gotA.Timeout)
	assert.Equal(t, "run the nightly audit and post the summary", gotA.Prompt)
	assert.Equal(t, "tool", gotA.Source)
	assert.Equal(t, []string{"go-build", "go-test:..."}, gotA.Contract)

	// --- create (one_time in the future, defaults everywhere else) ---
	fireAt := time.Now().Add(25 * time.Hour).Truncate(time.Second).UTC()
	res = runScheduleRaw(t, tool, map[string]any{
		"action":   "create",
		"prompt":   "one-shot follow-up",
		"one_time": fireAt.Format(time.RFC3339),
	})
	body = decodeScheduleBody(t, res)
	idB, _ := body["schedule_id"].(string)
	require.NotEmpty(t, idB)
	require.NotEqual(t, idA, idB)

	// The rendering is derived from the input timestamp, not from the tool.
	assert.Equal(t, fireAt.Format(time.RFC3339), body["next_run_at"])
	assert.Equal(t, "once "+fireAt.Format(time.RFC3339), body["trigger"])
	assert.Equal(t, "worker", body["role"], "an unspecified role defaults to worker")

	gotB, ok := sched.Get(idB)
	require.True(t, ok)
	assert.Equal(t, gotB.Role, body["role"])
	assert.Equal(t, gotB.NextRunAt.UTC().Format(time.RFC3339), body["next_run_at"])
	assert.Equal(t, "/repo", gotB.WorkDir, "an unspecified workdir falls back to the session cwd")
	assert.Zero(t, gotB.Timeout)

	// --- list (shape + agreement with Scheduler.List) ---
	res = runScheduleRaw(t, tool, map[string]any{"action": "list"})
	var listBody struct {
		Schedules []map[string]any `json:"schedules"`
		Count     int              `json:"count"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Content), &listBody))
	rows := sched.List()
	require.Len(t, rows, 2)
	require.Equal(t, len(rows), listBody.Count)
	require.Len(t, listBody.Schedules, 2)
	assert.Equal(t, "2 schedules", res.Detail)

	// List is ordered by next fire time: the 15m schedule outguns the 25h one.
	assert.Equal(t, idA, listBody.Schedules[0]["schedule_id"])
	assert.Equal(t, idB, listBody.Schedules[1]["schedule_id"])
	byID := map[string]map[string]any{}
	for _, sc := range listBody.Schedules {
		id, _ := sc["schedule_id"].(string)
		byID[id] = sc
	}
	for _, oracle := range rows {
		row := byID[oracle.ID]
		require.NotNil(t, row, "scheduler row %s missing from the tool list", oracle.ID)
		assert.Equal(t, oracle.NextRunAt.UTC().Format(time.RFC3339), row["next_run_at"])
		assert.Equal(t, oracle.LastRunAt.IsZero(), row["last_run_at"] == "")
		assert.Equal(t, oracle.Enabled, row["enabled"])
		assert.Equal(t, oracle.Role, row["role"])
		assert.Equal(t, oracle.Prompt, row["prompt"], "short prompts pass through untruncated")
		assert.Equal(t, oracle.Source, row["source"])
	}
	assert.Equal(t, "every 15m0s", byID[idA]["trigger"])
	assert.Equal(t, "once "+fireAt.Format(time.RFC3339), byID[idB]["trigger"])

	// --- cancel (disables; history stays) ---
	res = runScheduleRaw(t, tool, map[string]any{"action": "cancel", "schedule_id": idA})
	body = decodeScheduleBody(t, res)
	assert.Equal(t, idA, body["schedule_id"])
	assert.Equal(t, false, body["enabled"])
	assert.Equal(t, "", body["last_run_at"])
	assert.Equal(t, "cancelled "+idA, res.Detail)

	after, ok := sched.Get(idA)
	require.True(t, ok, "cancel disables, it does not delete")
	assert.False(t, after.Enabled)

	// Both surfaces agree afterwards: the row survives, disabled.
	res = runScheduleRaw(t, tool, map[string]any{"action": "list"})
	require.NoError(t, json.Unmarshal([]byte(res.Content), &listBody))
	require.Len(t, listBody.Schedules, 2)
	byID = map[string]map[string]any{}
	for _, sc := range listBody.Schedules {
		id, _ := sc["schedule_id"].(string)
		byID[id] = sc
	}
	assert.Equal(t, false, byID[idA]["enabled"])
	assert.Equal(t, true, byID[idB]["enabled"])
	rows = sched.List()
	for _, oracle := range rows {
		assert.Equal(t, oracle.Enabled, byID[oracle.ID]["enabled"])
	}
}

// naiveScheduleDetail rebuilds the transcript detail from the documented
// rules: trigger precedence cron > fixed_rate > one_time, the description
// over the prompt, and a byte-based 60-byte prompt clip.
func naiveScheduleDetail(input string) string {
	var in map[string]any
	_ = json.Unmarshal([]byte(input), &in)
	get := func(k string) string { s, _ := in[k].(string); return s }
	switch strings.ToLower(get("action")) {
	case "create":
		label := strings.TrimSpace(get("description"))
		if label == "" {
			p := get("prompt")
			if len(p) > 60 {
				p = p[:60] + "…"
			}
			label = p
		}
		which := get("cron")
		if which == "" {
			which = get("fixed_rate")
		}
		if which == "" {
			which = get("one_time")
		}
		return fmt.Sprintf("create %s · %s", which, label)
	case "cancel":
		return "cancel " + get("schedule_id")
	default:
		return "list schedules"
	}
}

// oracle: naive reference implementation in this test — the transcript
// detail line rebuilt from the documented rules and compared over
// table-driven arguments, including hostile and non-JSON payloads.
func TestScheduleDetailFromArgs_Differential(t *testing.T) {
	long := strings.Repeat("n", 80) // past the 60-byte clip
	cases := []struct {
		name  string
		input string
	}{
		{"create with cron and description", `{"action":"create","cron":"30 3 * * *","description":"nightly lint","prompt":"p"}`},
		{"create falls back to fixed_rate", `{"action":"create","fixed_rate":"15m","description":"tick","prompt":"p"}`},
		{"create falls back to one_time", `{"action":"create","one_time":"2026-12-01T09:00:00Z","prompt":"p"}`},
		{"create clips a long prompt", `{"action":"create","cron":"* * * * *","prompt":"` + long + `"}`},
		{"create with nothing", `{"action":"create"}`},
		{"create with non-string fields", `{"action":"create","cron":7,"prompt":[]}`},
		{"cancel with id", `{"action":"cancel","schedule_id":"abc123"}`},
		{"cancel without id", `{"action":"cancel"}`},
		{"list", `{"action":"list"}`},
		{"list with padded action", `{"action":" LIST "}`},
		{"unknown action", `{"action":"explode"}`},
		{"missing action", `{}`},
		{"not json", `nonsense`},
	}
	tool, _ := newTool(t)
	require.NotNil(t, tool.DetailFromArgs)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, naiveScheduleDetail(tc.input), tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}
