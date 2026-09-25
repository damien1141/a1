package compaction

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// captureCompactor records the prompts and caps handed to the LLM so tests can
// assert on what actually reaches the provider. Mid-turn compaction summarizes
// the history and the turn prefix in parallel, hence the mutex.
type captureCompactor struct {
	mu        sync.Mutex
	prompts   []string
	maxTokens []int
	text      string
	truncated bool
}

func (c *captureCompactor) Compact(_ context.Context, req llm.CompactRequest) (llm.CompactResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prompts = append(c.prompts, req.Prompt)
	c.maxTokens = append(c.maxTokens, req.MaxTokens)
	text := c.text
	if text == "" {
		text = "SUMMARY"
	}
	return llm.CompactResult{Text: text, Truncated: c.truncated}, nil
}

func TestGenerateTurnPrefixSummary_IncludesPrompt(t *testing.T) {
	c := &captureCompactor{}

	got, err := generateTurnPrefixSummary(
		t.Context(),
		c,
		[]llm.Message{{Role: llm.RoleUser, Content: "fix the parser"}},
		1024,
	)

	require.NoError(t, err)
	assert.Equal(t, "SUMMARY", got)
	require.Len(t, c.prompts, 1)
	assert.Contains(t, c.prompts[0], "</conversation>\n\n"+compactionTurnPrefixPrompt,
		"the turn-prefix prompt must follow the conversation")
	assert.Contains(t, c.prompts[0], "## Original Request")
	assert.Contains(t, c.prompts[0], "## Context for Suffix")
	assert.Equal(t, []int{1024}, c.maxTokens)
}

func TestGenerateSummary_IncludesPrompt(t *testing.T) {
	c := &captureCompactor{}

	_, err := generateSummary(
		t.Context(),
		c,
		[]llm.Message{{Role: llm.RoleUser, Content: "hello"}},
		"",
		2048,
	)

	require.NoError(t, err)
	require.Len(t, c.prompts, 1)
	assert.Contains(t, c.prompts[0], "[User]: hello")
	assert.Contains(t, c.prompts[0], compactionSummaryPrompt)
	assert.Equal(t, []int{2048}, c.maxTokens)
}

// A summary that stopped at the token cap is a prefix of what the model meant
// to write. Persisting it as the session summary would drop the history it
// replaced, so both summary paths must fail instead.
func TestGenerateSummary_Truncated_ReturnsError(t *testing.T) {
	c := &captureCompactor{truncated: true}

	_, err := generateSummary(t.Context(), c, []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, "", 2048)

	require.Error(t, err)
	assert.Equal(t, "Summarization failed: generation hit the token cap, summary incomplete", err.Error())
}

func TestGenerateTurnPrefixSummary_Truncated_ReturnsError(t *testing.T) {
	c := &captureCompactor{truncated: true}

	_, err := generateTurnPrefixSummary(t.Context(), c, []llm.Message{{Role: llm.RoleUser, Content: "hi"}}, 1024)

	require.Error(t, err)
	assert.Equal(t, "Turn prefix summarization failed: generation hit the token cap, summary incomplete", err.Error())
}

func TestSummarizationCap(t *testing.T) {
	// Default reserveTokens is 16384: 0.8 for history, 0.5 for the turn prefix.
	assert.Equal(t, 13107, summarizationCap(16384, historySummaryRatio))
	assert.Equal(t, 8192, summarizationCap(16384, turnPrefixSummaryRatio))
	assert.Equal(t, 0, summarizationCap(0, historySummaryRatio), "no budget means the provider default")
}

// A mid-turn cut summarizes two buckets at once, each with its own cap.
func TestSummarizeMidTurnCut_CapsEachSummary(t *testing.T) {
	c := &captureCompactor{}

	summary, err := summarizeMidTurnCut(t.Context(), CompactionPreparation{
		MessagesToSummarize: []llm.Message{{Role: llm.RoleUser, Content: "older history"}},
		TurnPrefixMessages:  []llm.Message{{Role: llm.RoleUser, Content: "turn prefix"}},
		ReserveTokens:       16384,
	}, c)

	require.NoError(t, err)
	assert.Contains(t, summary, "SUMMARY")
	assert.Contains(t, summary, "Turn Context (mid-turn cut)")
	assert.ElementsMatch(t, []int{13107, 8192}, c.maxTokens)
}
