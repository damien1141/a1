package scheduletool

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/schedule"
	"github.com/damien1141/a1/internal/tools/tooldef"

	_ "modernc.org/sqlite"
)

func newTool(t *testing.T) (tooldef.Tool, *schedule.Scheduler) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/s.db")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	s, err := schedule.New(db, func(context.Context, schedule.Schedule) error { return nil })
	require.NoError(t, err)
	tool := ScheduleTool(Deps{
		Scheduler: s,
		WorkDir:   func() string { return "/repo" },
		FleetContract: func() []string {
			return []string{"go-build", "go-test:..."}
		},
	})
	require.NotEmpty(t, tool.Definition.Name)
	return tool, s
}

func runTool(t *testing.T, tool tooldef.Tool, args map[string]any) (map[string]any, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	res, err := tool.Run(context.Background(), raw)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &body))
	return body, nil
}

func TestCreateListCancel(t *testing.T) {
	tool, sched := newTool(t)

	// Create: inherits the live fleet contract, defaults to worker
	// (headless-strict), resolves the workdir from the session.
	body, err := runTool(t, tool, map[string]any{
		"action":      "create",
		"prompt":      "run make lint and fix new warnings",
		"description": "nightly lint",
		"cron":        "30 3 * * *",
	})
	require.NoError(t, err)
	id, _ := body["schedule_id"].(string)
	require.NotEmpty(t, id)
	assert.Equal(t, "cron 30 3 * * *", body["trigger"])
	assert.Equal(t, "worker", body["role"])
	assert.NotEmpty(t, body["next_run_at"])
	assert.Equal(t, []any{"go-build", "go-test:..."}, body["contract"])

	got, ok := sched.Get(id)
	require.True(t, ok)
	assert.Equal(t, "/repo", got.WorkDir)
	assert.Equal(t, "tool", got.Source)

	// List shows it.
	body, err = runTool(t, tool, map[string]any{"action": "list"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, body["count"])

	// Cancel disables it.
	body, err = runTool(t, tool, map[string]any{
		"action":      "cancel",
		"schedule_id": id,
	})
	require.NoError(t, err)
	assert.EqualValues(t, false, body["enabled"])

	// Double-cancel errors.
	_, err = runTool(t, tool, map[string]any{"action": "cancel", "schedule_id": "nope"})
	assert.Error(t, err)
}

func TestCreateValidation(t *testing.T) {
	tool, _ := newTool(t)

	_, err := runTool(t, tool, map[string]any{"action": "create", "prompt": "x"})
	assert.Error(t, err) // no trigger

	_, err = runTool(t, tool, map[string]any{"action": "create", "prompt": "x", "cron": "bad"})
	assert.Error(t, err)

	_, err = runTool(t, tool, map[string]any{
		"action": "create", "prompt": "x", "cron": "* * * * *", "fixed_rate": "5m",
	})
	assert.Error(t, err) // two triggers

	_, err = runTool(t, tool, map[string]any{"action": "explode"})
	assert.Error(t, err) // bad action

	// one_time in the past: accepted, flagged, disables on first pass.
	body, err := runTool(t, tool, map[string]any{
		"action":   "create",
		"prompt":   "backdated",
		"one_time": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)
	assert.Empty(t, body["next_run_at"])
	assert.Contains(t, body["note"], "past")
}
