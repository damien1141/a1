package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

func TestEmptyGuardDeterministicSkip(t *testing.T) {
	var g emptyResponseGuard
	// First empty with usage proof: retry.
	require.True(t, g.observe("m", llm.Usage{PromptTokens: 100, CompletionTokens: 0}, false),
		"first empty keeps the budget")
	// Second empty, same model, same proof: deterministic — skip.
	require.False(t, g.observe("m", llm.Usage{PromptTokens: 100, CompletionTokens: 0}, false),
		"two usage-proven zeros from one model are deterministic")
	require.Len(t, g.attempts, 2)
}

func TestEmptyGuardUsageAbsentResponseProves(t *testing.T) {
	var g emptyResponseGuard
	// No usage at all and nothing generated: hermes's second proof path.
	require.True(t, g.observe("m", llm.Usage{}, false))
	require.False(t, g.observe("m", llm.Usage{}, false),
		"two no-usage no-generation empties are deterministic")
}

func TestEmptyGuardFailOpenOnMixedEvidence(t *testing.T) {
	var g emptyResponseGuard
	// First empty with no usage; second with usage proving generated
	// tokens (whitespace/think-stripped output): mixed evidence — the
	// budget stands.
	require.True(t, g.observe("m", llm.Usage{}, false))
	require.True(t, g.observe("m", llm.Usage{PromptTokens: 100, CompletionTokens: 7}, false),
		"mixed evidence fails open")
	// Budget spent (2 retries used): terminal without determinism.
	require.False(t, g.observe("m", llm.Usage{PromptTokens: 100, CompletionTokens: 7}, false))
}

func TestEmptyGuardReasoningCountsAsGeneration(t *testing.T) {
	var g emptyResponseGuard
	// Usage says zero, but reasoning text was observed: reasoning is
	// generation — never a deterministic empty.
	require.True(t, g.observe("m", llm.Usage{PromptTokens: 100, CompletionTokens: 0}, true))
	require.True(t, g.observe("m", llm.Usage{PromptTokens: 100, CompletionTokens: 0}, true),
		"observed reasoning breaks the usage proof")
}

func TestEmptyGuardModelSwitchBreaksSignature(t *testing.T) {
	var g emptyResponseGuard
	require.True(t, g.observe("m1", llm.Usage{PromptTokens: 10, CompletionTokens: 0}, false))
	require.True(t, g.observe("m2", llm.Usage{PromptTokens: 10, CompletionTokens: 0}, false),
		"a mid-streak model switch breaks the signature")
}

func TestEmptyCompletionShape(t *testing.T) {
	require.True(t, emptyCompletion(llm.Message{Role: llm.RoleAssistant}))
	require.True(t, emptyCompletion(llm.Message{Role: llm.RoleAssistant, Content: "   \n\t"}))
	require.False(t, emptyCompletion(llm.Message{Role: llm.RoleAssistant, Content: "hi"}))
	require.False(t, emptyCompletion(llm.Message{Role: llm.RoleAssistant, ReasoningContent: "thinking"}))
	require.False(t, emptyCompletion(llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{}}}))
}

func TestEmptyBackoffBounded(t *testing.T) {
	for n := 1; n <= 6; n++ {
		d := emptyBackoff(n)
		require.Greater(t, d, time.Duration(0))
		require.LessOrEqual(t, d, 30*time.Second)
	}
}
