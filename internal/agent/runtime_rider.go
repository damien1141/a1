package agent

import (
	"context"
	"strings"

	"github.com/damien1141/a1/internal/llm"
)

// runtime_rider.go — the per-turn runtime-context rider.
//
// Providers bill prompt caching by the byte: a cache entry is a prefix of the
// request exactly as serialized, and any byte that changes anywhere in that
// prefix re-bills everything after it as a fresh cache write (1.25–2x the
// base input price) instead of a cache read (0.1x). The old shape of runtime
// context — clock, weather, memory bundle, job notices — was regenerated per
// turn (the clock per round, to the second) and delivered as RoleSystem
// messages, which every provider builder hoists into the leading system
// block: the one block the cache is pinned to. The result was that no
// request ever read the previous one's cache; a whole agentic session paid
// write price on its full context, every round.
//
// The rider fixes the shape, not the content: the runtime context is
// assembled once at each user-turn boundary and PERSISTED as a user message
// immediately before the user's own message. Fresh bytes land exactly where
// the cache was about to write the new turn anyway; once written they are
// frozen history, replayed byte-identically by every later request, so the
// prefix through any past turn is stable forever. Memories and notices
// written mid-conversation still surface on the next user message — the
// freshness contract the transient injections existed for, kept without the
// churn that made caching impossible.
//
// The clock and the weather forecast moved out of the rider entirely: they
// are fetch engines now ("what time is it", "weather in Reykjavik"), so the
// model asks for them when — and only when — it needs them, and ambient
// context costs nothing on turns that do not. What remains is the
// change-gated memory bundle and delivered job notices, the two injections
// whose freshness contract genuinely requires per-turn delivery. A rider
// with nothing to say is skipped: an empty user message in history would
// cost a cache-write round for zero content.

// buildRuntimeRider assembles the runtime-context rider for one user turn.
// userPrompt is the post-extension prompt text (the same string the memory
// inlet's skip detector and retrieval cues run on). The returned message is
// ready to persist; ok is false when no section applied (nothing new to
// say), and callers must not append an empty rider.
func (engine *Engine) buildRuntimeRider(ctx context.Context, userPrompt string) (llm.Message, bool) {
	var sections []string

	// Semantic memory bundle, change-gated: only a bundle that differs from
	// the last one persisted rides again. Unchanged bundles are already in
	// history from the turn that injected them; re-sending an identical block
	// would duplicate it forever at read price for nothing.
	if mem := engine.injectSemanticMemories(ctx, userPrompt); mem != nil && mem.Content != engine.lastMemRider {
		engine.lastMemRider = mem.Content
		sections = append(sections, mem.Content)
	}

	// Delivered job notices: background results claimed-and-acked at this
	// boundary. Persisting them also fixes their old vanishing act — the
	// transient injection meant the model was told a job finished and then
	// never saw it again on the next turn.
	if notices := engine.buildJobNotices(); notices != nil {
		if text := strings.TrimSpace(notices.Content); text != "" {
			sections = append(sections, text)
		}
	}

	if len(sections) == 0 {
		return llm.Message{}, false
	}
	return llm.Message{
		Role:    llm.RoleUser,
		Runtime: true,
		Content: strings.Join(sections, "\n\n"),
	}, true
}
