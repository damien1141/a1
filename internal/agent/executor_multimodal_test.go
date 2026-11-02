package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/evidence"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// imageResultTool builds a registry whose single tool returns a fixed
// content plus image blocks.
func imageResultTool() tools.Registry {
	return tools.Registry{
		"browser_read": {
			Definition: llm.ToolDefinition{Name: "browser_read"},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				return tools.Result{
					Content: "Screenshot saved to /tmp/shot.png",
					Images:  []llm.Image{{Data: "aGVsbG8=", MimeType: "image/png"}},
				}, nil
			},
		},
	}
}

func TestExecutorImagesRideToolMessageWhenEnabled(t *testing.T) {
	ex := NewExecutor(imageResultTool(), permission.AllowAll{}, nil, nil, nil)
	ex.SetImageEnabled(true)
	msgs, _, _ := ex.Run(t.Context(), []llm.ToolCall{{
		ID:       "c1",
		Function: llm.Function{Name: "browser_read", Arguments: "{}"},
	}}, func(session.ToolData) bool { return true })
	require.Len(t, msgs, 1)
	require.Len(t, msgs[0].Images, 1, "image-enabled models see the screenshot block")
	assert.Equal(t, "aGVsbG8=", msgs[0].Images[0].Data)
	assert.Equal(t, "image/png", msgs[0].Images[0].MimeType)
}

func TestExecutorImagesStrippedWhenBlindModel(t *testing.T) {
	ex := NewExecutor(imageResultTool(), permission.AllowAll{}, nil, nil, nil)
	ex.SetImageEnabled(false)
	msgs, _, _ := ex.Run(t.Context(), []llm.ToolCall{{
		ID:       "c1",
		Function: llm.Function{Name: "browser_read", Arguments: "{}"},
	}}, func(session.ToolData) bool { return true })
	require.Len(t, msgs, 1)
	assert.Empty(t, msgs[0].Images, "blind models keep the path-only text")
	assert.Contains(t, msgs[0].Content, "Screenshot saved to")
}

func TestExecutorImageEnabledFlagReachesTool(t *testing.T) {
	var sawFlag bool
	reg := tools.Registry{
		"browser_read": {
			Definition: llm.ToolDefinition{Name: "browser_read"},
			Run: func(ctx context.Context, _ json.RawMessage) (tools.Result, error) {
				sawFlag = tooldef.ImageEnabledFrom(ctx)
				return tools.Result{Content: "ok"}, nil
			},
		},
	}
	ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)
	ex.SetImageEnabled(true)
	_, _, _ = ex.Run(t.Context(), []llm.ToolCall{{
		ID:       "c1",
		Function: llm.Function{Name: "browser_read", Arguments: "{}"},
	}}, func(session.ToolData) bool { return true })
	assert.True(t, sawFlag, "tools consult the flag to skip image work")
}

func TestExecutorHandoffRoutesThroughAsk(t *testing.T) {
	var asked atomic.Int32
	reg := tools.Registry{
		"browser": {
			Definition: llm.ToolDefinition{Name: "browser"},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				return tools.Result{
					Content: "clicked checkout",
					Handoff: &tooldef.Handoff{Reason: "captcha challenge", URL: "https://shop.example.com/checkout"},
				}, nil
			},
		},
	}
	ask := func(context.Context, permission.Request, string) (permission.AskResult, error) {
		asked.Add(1)
		return permission.AskResult{Approved: true}, nil
	}
	ex := NewExecutor(reg, permission.AllowAll{}, ask, nil, nil)
	msgs, _, _ := ex.Run(t.Context(), []llm.ToolCall{{
		ID:       "c1",
		Function: llm.Function{Name: "browser", Arguments: `{"action":"click"}`},
	}}, func(session.ToolData) bool { return true })
	require.Len(t, msgs, 1)
	require.Equal(t, int32(1), asked.Load(), "the handoff went through the ask flow")
	assert.Contains(t, msgs[0].Content, "clicked checkout")
	assert.Contains(t, msgs[0].Content, "user accepted the handoff")
}

func TestExecutorHandoffDeclinedNotesFeedback(t *testing.T) {
	reg := tools.Registry{
		"browser": {
			Definition: llm.ToolDefinition{Name: "browser"},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				return tools.Result{
					Content: "opened login",
					Handoff: &tooldef.Handoff{Reason: "login wall"},
				}, nil
			},
		},
	}
	ask := func(context.Context, permission.Request, string) (permission.AskResult, error) {
		return permission.AskResult{Approved: false, Feedback: "skip this site"}, nil
	}
	ex := NewExecutor(reg, permission.AllowAll{}, ask, nil, nil)
	msgs, _, _ := ex.Run(t.Context(), []llm.ToolCall{{
		ID:       "c1",
		Function: llm.Function{Name: "browser", Arguments: `{"action":"open"}`},
	}}, func(session.ToolData) bool { return true })
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0].Content, "user declined the handoff")
	assert.Contains(t, msgs[0].Content, "skip this site")
}

func TestExecutorScreenshotFactFeedsLedger(t *testing.T) {
	dir := t.TempDir()
	shotPath := filepath.Join(dir, "cap.png")
	require.NoError(t, os.WriteFile(shotPath, []byte("\x89PNG\r\n\x1a\nfake"), 0o644))

	ledger := evidence.NewLedger(filepath.Join(dir, "evidence.jsonl"))
	require.True(t, ledger.Watch("screenshot:login"))

	reg := tools.Registry{
		"browser_read": {
			Definition: llm.ToolDefinition{Name: "browser_read"},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				return tools.Result{
					Content:    "Screenshot saved to " + shotPath,
					Screenshot: &tooldef.ScreenshotFact{Name: "login", Path: shotPath},
				}, nil
			},
		},
	}
	ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)
	ex.SetEvidenceLedger(ledger)
	msgs, _, _ := ex.Run(t.Context(), []llm.ToolCall{{
		ID:       "c1",
		Function: llm.Function{Name: "browser_read", Arguments: `{"mode":"screenshot","name":"login"}`},
	}}, func(session.ToolData) bool { return true })
	require.Len(t, msgs, 1)
	// The fake bytes are not a decodable PNG, so the check fails with the
	// decode detail rather than silently recording nothing.
	st := ledger.CheckStatus("screenshot:login")
	require.Equal(t, evidence.CheckFailing, st.State)
	assert.Contains(t, st.Detail, "not a decodable PNG")
}
