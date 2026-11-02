package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/skillstats"
	"github.com/damien1141/a1/internal/telemetry"
	"github.com/damien1141/a1/internal/tools"

	"github.com/damien1141/a1/internal/vault"
)

// eventSink collects every telemetry event the recorder forwards to the
// global sink (the SQLite sink drops tool_call pre-events, so tests assert
// on the raw stream instead).
type eventSink struct {
	mu     sync.Mutex
	events []telemetry.Event
}

func (s *eventSink) Record(e telemetry.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *eventSink) snapshot() []telemetry.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]telemetry.Event(nil), s.events...)
}

// TestExecutorTelemetryDurationAndSession pins the Bet 07 event fields:
// terminal tool events carry the run's wall-clock duration and the session
// id, so the SQLite sink can attribute latency per session.
func TestExecutorTelemetryDurationAndSession(t *testing.T) {
	sink := &eventSink{}
	telemetry.SetGlobalSink(sink)
	defer telemetry.SetGlobalSink(nil)

	reg := tools.Registry{
		"bash": {
			Definition: llm.ToolDefinition{Name: "bash"},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				// A real pause: time.Since after a sleep is never shorter,
				// so the assertion cannot flake on scheduling.
				time.Sleep(20 * time.Millisecond)
				return tools.Result{Content: "ok"}, nil
			},
		},
		"read": {
			Definition: llm.ToolDefinition{Name: "read"},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				return tools.Result{}, assert.AnError
			},
		},
	}
	ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)
	ex.SetMeta("sess-42", t.TempDir())

	_, _, _ = ex.Run(t.Context(), []llm.ToolCall{
		{ID: "ok1", Type: "function", Function: llm.Function{Name: "bash", Arguments: `{"command":"true"}`}},
		{ID: "err1", Type: "function", Function: llm.Function{Name: "read", Arguments: `{"path":"x"}`}},
	}, func(session.ToolData) bool { return true })

	events := sink.snapshot()
	var ok, errEvt *telemetry.Event
	for i := range events {
		switch events[i].Kind {
		case telemetry.EventToolOK:
			ok = &events[i]
		case telemetry.EventToolErr:
			errEvt = &events[i]
		}
	}
	require.NotNil(t, ok, "tool_ok event recorded")
	require.NotNil(t, errEvt, "tool_error event recorded")
	assert.Equal(t, "sess-42", ok.SessionID)
	assert.Equal(t, "sess-42", errEvt.SessionID)
	assert.GreaterOrEqual(t, ok.DurationMS, int64(15), "duration covers the tool's sleep")
	assert.GreaterOrEqual(t, errEvt.DurationMS, int64(0))
	assert.NotEmpty(t, errEvt.Err)
}

// TestExecutorRecordsSkillPick pins the bandit's pick inlet: a successful
// read of a skill's SKILL.md lands a pick row in the vault's store.
func TestExecutorRecordsSkillPick(t *testing.T) {
	root := t.TempDir()
	v, err := vault.Open(root)
	require.NoError(t, err)
	t.Cleanup(func() { v.Close() })
	session.SetDefaultVault(v)
	t.Cleanup(func() { session.SetDefaultVault(nil) })

	reg := tools.Registry{
		"read": {
			Definition: llm.ToolDefinition{Name: "read"},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				return tools.Result{Content: "skill body"}, nil
			},
		},
	}
	ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)

	skillDir := filepath.Join(root, "skills", "go-test")
	_, _, _ = ex.Run(t.Context(), []llm.ToolCall{
		{ID: "r1", Type: "function", Function: llm.Function{Name: "read",
			Arguments: `{"path":` + jsonEscape(filepath.Join(skillDir, "SKILL.md")) + `}`}},
		// An ordinary read must not record a pick.
		{ID: "r2", Type: "function", Function: llm.Function{Name: "read",
			Arguments: `{"path":` + jsonEscape(filepath.Join(root, "main.go")) + `}`}},
	}, func(session.ToolData) bool { return true })

	rows, err := skillstats.Stats(v.DB())
	require.NoError(t, err)
	require.Len(t, rows, 1, "only the SKILL.md read records a pick")
	assert.Equal(t, "go-test", rows[0].Skill)
	assert.Equal(t, 1, rows[0].Picks)
}

// TestExecutorSkillPickFileLayoutNoop pins the degraded path: without a
// vault there is nothing to record and nothing breaks.
func TestExecutorSkillPickFileLayoutNoop(t *testing.T) {
	session.SetDefaultVault(nil)
	reg := tools.Registry{
		"read": {
			Definition: llm.ToolDefinition{Name: "read"},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				return tools.Result{Content: "x"}, nil
			},
		},
	}
	ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)
	msgs, _, _ := ex.Run(t.Context(), []llm.ToolCall{
		{ID: "r1", Type: "function", Function: llm.Function{Name: "read", Arguments: `{"path":"/skills/go-test/SKILL.md"}`}},
	}, func(session.ToolData) bool { return true })
	require.Len(t, msgs, 1)
	assert.NotEmpty(t, msgs[0].Content, "the tool result is unaffected")
}

func jsonEscape(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
