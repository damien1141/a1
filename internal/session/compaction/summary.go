package compaction

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/damien1141/a1/internal/llm"
)

var (
	//go:embed compaction-summary.tmpl
	compactionSummaryPrompt string
	//go:embed compaction-update-summary.tmpl
	compactionUpdateSummaryPrompt string
	//go:embed compaction-turn-prefix.tmpl
	compactionTurnPrefixPrompt string
)

// Summaries are capped at a fraction of the headroom compaction frees
// (reserveTokens). A summary larger than the headroom it creates does not pay
// for itself, and the turn-prefix budget is smaller because it only recovers
// the tail of one turn.
const (
	historySummaryRatio    = 0.8
	turnPrefixSummaryRatio = 0.5
)

// summarizationCap returns the output cap for one summary. Zero means the
// provider default (no budget to derive a cap from).
func summarizationCap(reserveTokens int, ratio float64) int {
	if reserveTokens <= 0 {
		return 0
	}
	return int(float64(reserveTokens) * ratio)
}

// errTruncatedSummary reports a summary that hit the output cap: its text is a
// prefix of the summary the model meant to write, so persisting it would drop
// the history it replaced.
func errTruncatedSummary(label string) error {
	return fmt.Errorf("%s failed: generation hit the token cap, summary incomplete", label)
}

// generateSummary generates a summary of the conversation history using an LLM.
// If a previousSummary is provided, it will update the existing summary with new messages.
// Otherwise, it generates an initial summary from the currentMessages.
func generateSummary(
	ctx context.Context,
	compactor llm.Compactor,
	currentMessages []llm.Message,
	previousSummary string,
	maxTokens int,
) (string, error) {
	basePrompt := compactionSummaryPrompt
	if previousSummary != "" {
		basePrompt = compactionUpdateSummaryPrompt
	}

	conversation := SerializeConversation(currentMessages)
	promptText := fmt.Sprintf("<conversation>\n%s\n</conversation>", conversation)
	if previousSummary != "" {
		promptText += fmt.Sprintf("<previous-summary>\n%s\n</previous-summary>", previousSummary)
	}
	promptText += basePrompt
	return compact(ctx, compactor, promptText, maxTokens, "Summarization")
}

// generateTurnPrefixSummary generates a summary specifically for turn prefixes
// (prefix-based compaction): the prefix of a turn whose suffix is kept. Without
// the prompt the model is handed a bare conversation and no instruction, and its
// continuation gets persisted as the session summary.
func generateTurnPrefixSummary(
	ctx context.Context,
	compactor llm.Compactor,
	messages []llm.Message,
	maxTokens int,
) (string, error) {
	conversation := SerializeConversation(messages)
	promptText := fmt.Sprintf("<conversation>\n%s\n</conversation>\n\n", conversation)
	promptText += compactionTurnPrefixPrompt
	return compact(ctx, compactor, promptText, maxTokens, "Turn prefix summarization")
}

// compact runs one summarization call and rejects a capped answer: a truncated
// summary would silently drop the history it is meant to stand in for.
func compact(
	ctx context.Context,
	compactor llm.Compactor,
	prompt string,
	maxTokens int,
	label string,
) (string, error) {
	res, err := compactor.Compact(ctx, llm.CompactRequest{Prompt: prompt, MaxTokens: maxTokens})
	if err != nil {
		return "", err
	}
	if res.Truncated {
		return "", errTruncatedSummary(label)
	}
	return res.Text, nil
}
