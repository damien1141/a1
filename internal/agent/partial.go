package agent

import (
	"errors"
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/debuglog"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
)

// ErrRepetitionLoop marks a reply cut mid-stream or refused at completion
// for degenerate repetition. Typed so surfaces can distinguish "the model
// broke" from "the transport broke" — only the latter is ever retried.
var ErrRepetitionLoop = errors.New("repetition loop in model output")

// maxEngineTransientRetries bounds the engine-layer transient retry ladder
// (408 statuses, mid-stream drops, transport errors the HTTP layer cannot
// see): two retries after the initial attempt, each backing off per the
// classified verdict.
const maxEngineTransientRetries = 2

// settleCompleteText re-stamps a built (not yet yielded) complete event so
// its text block says text instead of what the model produced. Used by the
// empty-response sentinel and the repetition placeholder: the row the UI
// settles on — and the row that persists — must carry the replacement, not
// the blank or looped original.
func settleCompleteText(ev session.Event, text string) session.Event {
	if cm, ok := ev.(session.AssistantMessageUpdate); ok {
		cm.Message.Text = text
		cm.Message.Content = []session.ContentBlock{{Type: session.BlockText, Text: text}}
		return cm
	}
	return ev
}

// persistDeliveredPartial implements the delivered-partial contract
// (hermes partial_result): when a terminal stream failure already
// delivered text to the consumer, that text is the reply body — persist it
// as the assistant row instead of discarding it alongside the error, so a
// restart and the next turn's context replay what was actually said. The
// live UI already showed the partial in its error-state row (streamTurn
// emits it); this closes the gap where the transcript lost it.
//
// Assistant rows with tool calls are NEVER persisted here: a stream that
// died mid-tool-call leaves un-executed tool_use blocks, and replaying
// those dangles against every provider (tool_use without tool_result is a
// 400 on Anthropic). The repetition interrupt also routes through here by
// design — its message carries only the placeholder, never the looped
// bytes (hermes #112764).
func (engine *Engine) persistDeliveredPartial(msg llm.Message) {
	if len(msg.ToolCalls) > 0 {
		return
	}
	if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.ReasoningContent) == "" {
		return
	}
	if err := engine.session.Append(msg); err != nil {
		// Best-effort by design: the error still surfaces to the consumer,
		// and losing the partial's persistence must not compound the
		// failure it accompanies.
		debuglog.Logf("agent: persisting delivered partial failed: %v", err)
	}
}

// cutRepetition ends a degenerate repetition loop: the looped bytes are never
// persisted — replaying them re-seeds the loop on the next request (hermes
// #112764) — so the row settles on the placeholder and the typed error tells
// the consumer the reply degenerated and was interrupted. No retry follows:
// the same model on the same prompt loops again.
func (engine *Engine) cutRepetition(id string, yield func(session.Event, error) bool) (llm.Message, session.Event, error) {
	_ = yield(emitMessage(id, session.StateError, session.StopNone, "", llm.RepetitionLoopPlaceholder, nil, llm.Usage{}), nil)
	return llm.Message{Role: llm.RoleAssistant, Content: llm.RepetitionLoopPlaceholder}, nil, fmt.Errorf("agent: %w", ErrRepetitionLoop)
}
