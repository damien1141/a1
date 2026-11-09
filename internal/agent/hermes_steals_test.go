package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/toolguard"
	"github.com/damien1141/a1/internal/tools"
)

// This file pins the second hermes steal batch: the empty-response guard
// (deterministic-empty short-circuit, fail-open ladder), the repetition
// guard (mid-stream runaway cut + stop-path check, never persisting the
// looped bytes), the delivered-partial contract (streamed text survives a
// terminal failure as the reply), and the executor-side tool loop
// guardrails (warnings, stubs, halts).

// collapseEmptyBackoff swaps the empty-retry sleep for a no-op so ladder
// tests don't wait out real seconds.
func collapseEmptyBackoff(t *testing.T) {
	t.Helper()
	prev := emptyBackoffDelay
	emptyBackoffDelay = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { emptyBackoffDelay = prev })
}

// sseRaw encodes one SSE data line from an arbitrary payload.
func sseRaw(v any) string {
	payload, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return "data: " + string(payload) + "\n\n"
}

// sseEmptyWithUsage streams a completed-but-empty assistant turn. usage
// non-nil attaches token usage to the final chunk (OpenAI's shape: usage
// rides the last chunk before [DONE]).
func sseEmptyWithUsage(usage map[string]any) string {
	chunk := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{}}}}
	if usage != nil {
		chunk["usage"] = usage
	}
	return sseRaw(chunk) + "data: [DONE]\n\n"
}

func assistantTexts(t *testing.T, sess *Session) []string {
	t.Helper()
	var out []string
	for _, m := range sess.BuildContext() {
		if m.Role == llm.RoleAssistant {
			out = append(out, m.Content)
		}
	}
	return out
}

func TestLoopEmptyResponseDeterministicSkipsRetries(t *testing.T) {
	collapseEmptyBackoff(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		// Two zero-output empties with believable usage: deterministic.
		_, _ = fmt.Fprint(w, sseEmptyWithUsage(map[string]any{
			"prompt_tokens": 50, "completion_tokens": 0, "total_tokens": 50,
		}))
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 32768},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.Error(t, lastErr, "a deterministic empty streak fails the turn")
	require.Contains(t, lastErr.Error(), "no content")
	require.Equal(t, int32(2), hits.Load(),
		"the second usage-proven empty skips the remaining retries")
	texts := assistantTexts(t, sess)
	require.Len(t, texts, 1)
	require.Equal(t, EmptyResponseSentinel, texts[0],
		"the sentinel is persisted, never a blank assistant row")
}

func TestLoopEmptyResponseAmbiguousKeepsBudget(t *testing.T) {
	collapseEmptyBackoff(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		// First attempt: no usage at all. Later attempts: usage proving
		// generated tokens (whitespace/think-stripped output) — mixed
		// evidence, so the full retry budget stands.
		if n == 1 {
			_, _ = fmt.Fprint(w, sseEmptyWithUsage(nil))
			return
		}
		_, _ = fmt.Fprint(w, sseEmptyWithUsage(map[string]any{
			"prompt_tokens": 50, "completion_tokens": 9, "total_tokens": 59,
		}))
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 32768},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.Error(t, lastErr)
	require.Equal(t, int32(3), hits.Load(),
		"ambiguous empties keep the full ladder: initial + 2 retries")
	require.Contains(t, assistantTexts(t, sess)[0], EmptyResponseSentinel)
}

func TestLoopEmptyResponseRecoversAfterRetry(t *testing.T) {
	collapseEmptyBackoff(t)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, sseEmptyWithUsage(nil))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, sseTextChunk("recovered"))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 32768},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var finalText string
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		require.NoError(t, err)
		if update, ok := ev.(session.AssistantMessageUpdate); ok && update.Message.State == session.StateComplete {
			finalText = update.Message.FlatText()
		}
	}
	require.Equal(t, "recovered", finalText, "a transient empty recovers on retry")
	require.Equal(t, int32(2), hits.Load())
	texts := assistantTexts(t, sess)
	require.Len(t, texts, 1)
	require.Equal(t, "recovered", texts[0], "no sentinel and no blank row on recovery")
}

// runawayUnit is a loop fragment comfortably past the repeat window.
const runawayUnit = "The build failed so I will retry the exact same step again without changing anything at all. "

func TestLoopRunawayStreamCutNeverPersistsLoopedBytes(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		// ~40k chars of a pure echo loop: the watch's first check runs at
		// 16k and cuts the stream.
		for i := 0; i < 400; i++ {
			if _, err := fmt.Fprint(w, sseTextChunk(runawayUnit)); err != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 32768},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.Error(t, lastErr)
	require.True(t, errors.Is(lastErr, ErrRepetitionLoop),
		"the cut surfaces the typed repetition error, got: %v", lastErr)
	require.Equal(t, int32(1), hits.Load(), "no retry after a repetition cut")
	texts := assistantTexts(t, sess)
	require.Len(t, texts, 1)
	require.Equal(t, llm.RepetitionLoopPlaceholder, texts[0],
		"the placeholder is persisted — never the looped bytes")
}

func TestLoopStopPathRunawayReplacedByPlaceholder(t *testing.T) {
	// A provider that delivers the whole reply in one non-streamed
	// message: the mid-stream watch never fed, so only the completed
	// answer can be judged.
	loop := strings.Repeat(runawayUnit, 350) // ~29k chars
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, sseRaw(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"role": "assistant", "content": loop},
			}},
		}))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 32768},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.Error(t, lastErr)
	require.True(t, errors.Is(lastErr, ErrRepetitionLoop), "got: %v", lastErr)
	texts := assistantTexts(t, sess)
	require.Len(t, texts, 1)
	require.Equal(t, llm.RepetitionLoopPlaceholder, texts[0])
}

func TestLoopAskedForRepetitionDelivered(t *testing.T) {
	// A legitimately repetitive answer below the stop-path minimum is
	// delivered verbatim: the scale gate exists to protect exactly this.
	small := strings.Repeat("I will not repeat myself in code review. ", 80) // ~3.6k chars
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, sseTextChunk(small))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 32768},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var finalText string
	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		if err != nil {
			lastErr = err
			break
		}
		if update, ok := ev.(session.AssistantMessageUpdate); ok && update.Message.State == session.StateComplete {
			finalText = update.Message.FlatText()
		}
	}
	require.NoError(t, lastErr)
	require.Equal(t, small, finalText)
	require.Equal(t, small, assistantTexts(t, sess)[0])
}

func TestLoopDeliveredPartialPersistedOnStreamCut(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, sseTextChunk("partial answer the user already watched stream"))
		if fl != nil {
			fl.Flush()
		}
		// Cut the connection mid-stream: terminal failure after delivery.
		panic(http.ErrAbortHandler)
	}))
	defer server.Close()

	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x", ContextWindow: 32768},
		sess,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{}),
	)
	require.NoError(t, err)

	var lastErr error
	for ev, err := range engine.Loop(t.Context(), "go", LoopOpts{}) {
		_ = ev
		if err != nil {
			lastErr = err
			break
		}
	}
	require.Error(t, lastErr, "the mid-stream cut surfaces")
	require.Equal(t, int32(1), hits.Load(), "delivered partials are never retried")
	texts := assistantTexts(t, sess)
	require.Len(t, texts, 1)
	require.Equal(t, "partial answer the user already watched stream", texts[0],
		"the delivered text is persisted as the reply body, not discarded with the error")
}

// --- Executor-side tool loop guardrails ---

func guardTestCall(id string) llm.ToolCall {
	return llm.ToolCall{ID: id, Function: llm.Function{Name: "flaky", Arguments: `{"path":"/etc/hosts"}`}}
}

func TestExecutorLoopGuardWarnsOnIdenticalFailures(t *testing.T) {
	reg := tools.Registry{
		"flaky": {
			Definition: llm.ToolDefinition{Name: "flaky", Readable: true},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				return tools.Result{}, errors.New("connection refused")
			},
		},
	}
	ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)
	ex.SetLoopGuard(toolguard.New(toolguard.Config{Warnings: true}))
	emit := func(session.ToolData) bool { return true }

	m1, _, _ := ex.Run(t.Context(), []llm.ToolCall{guardTestCall("c1")}, emit)
	require.Equal(t, "connection refused", m1[0].Content, "first failure carries no guidance yet")

	m2, _, _ := ex.Run(t.Context(), []llm.ToolCall{guardTestCall("c2")}, emit)
	require.Contains(t, m2[0].Content, "connection refused")
	require.Contains(t, m2[0].Content, "Tool loop warning",
		"the second identical failure warns through the tool result")
	require.Contains(t, m2[0].Content, "looks like a loop")
}

func TestExecutorLoopGuardStubsByteIdenticalLargeResults(t *testing.T) {
	big := strings.Repeat("payload line\n", 80) // > 512 chars
	reg := tools.Registry{
		"flaky": {
			Definition: llm.ToolDefinition{Name: "flaky", Readable: true},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				return tools.Result{Content: big}, nil
			},
		},
	}
	ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)
	ex.SetLoopGuard(toolguard.New(toolguard.Config{Warnings: true}))
	emit := func(session.ToolData) bool { return true }

	m1, _, _ := ex.Run(t.Context(), []llm.ToolCall{guardTestCall("c1")}, emit)
	require.Equal(t, big, m1[0].Content, "the first result passes through in full")

	m2, _, _ := ex.Run(t.Context(), []llm.ToolCall{guardTestCall("c2")}, emit)
	require.Contains(t, m2[0].Content, "byte-identical",
		"the second byte-identical large result becomes a reference stub")
	require.Contains(t, m2[0].Content, "c1", "the stub names the call holding the result")
	require.NotContains(t, m2[0].Content, "payload line", "the duplicate payload is deduplicated")
}

func TestExecutorLoopGuardHaltStopsTurn(t *testing.T) {
	var n atomic.Int32
	reg := tools.Registry{
		"flaky": {
			Definition: llm.ToolDefinition{Name: "flaky", Readable: true},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				return tools.Result{}, fmt.Errorf("attempt %d failed", n.Add(1))
			},
		},
	}
	ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)
	// Hard stops armed — the unattended (job runner) configuration.
	ex.SetLoopGuard(toolguard.New(toolguard.Config{Warnings: true, HardStop: true}))
	emit := func(session.ToolData) bool { return true }

	var halted bool
	var reason string
	for i := 0; i < 8; i++ {
		call := llm.ToolCall{
			ID:       fmt.Sprintf("c%d", i),
			Function: llm.Function{Name: "flaky", Arguments: fmt.Sprintf(`{"path":"/x/%d"}`, i)},
		}
		msgs, stop, stopReason := ex.Run(t.Context(), []llm.ToolCall{call}, emit)
		if stop {
			halted, reason = true, stopReason
			break
		}
		require.NotEmpty(t, msgs[0].Content)
	}
	require.True(t, halted, "8 varied-args failures on a non-tolerant tool halt the turn")
	require.Contains(t, reason, "tool loop guardrail")
}

func TestExecutorLoopGuardRefusalFeedsStreak(t *testing.T) {
	// The malformed-args shape: some models genuinely believe they sent
	// the arguments, so the retry is identical and the rows accumulate.
	// The refusal counts as the failure the streak needs.
	reg := tools.Registry{
		"flaky": {
			Definition: llm.ToolDefinition{
				Name:    "flaky",
				Readable: true,
				Params:  &llm.FunctionParameters{Type: "object", Properties: llm.Object{}, Required: []string{"path"}},
			},
			Run: func(context.Context, json.RawMessage) (tools.Result, error) {
				return tools.Result{Content: "never reached"}, nil
			},
		},
	}
	ex := NewExecutor(reg, permission.AllowAll{}, nil, nil, nil)
	ex.SetLoopGuard(toolguard.New(toolguard.Config{Warnings: true}))
	emit := func(session.ToolData) bool { return true }

	emptyCall := llm.ToolCall{ID: "c0", Function: llm.Function{Name: "flaky", Arguments: `{}`}}
	m1, _, _ := ex.Run(t.Context(), []llm.ToolCall{emptyCall}, emit)
	require.Contains(t, m1[0].Content, "empty arguments")

	emptyCall2 := llm.ToolCall{ID: "c1", Function: llm.Function{Name: "flaky", Arguments: `{}`}}
	m2, _, _ := ex.Run(t.Context(), []llm.ToolCall{emptyCall2}, emit)
	require.Contains(t, m2[0].Content, "Tool loop warning",
		"an identical re-ask of a refused call warns on the second round")
}
