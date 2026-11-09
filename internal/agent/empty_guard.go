package agent

import (
        "math/rand/v2"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/llm"
)

// Empty-response guard, ported from hermes-agent's
// agent/empty_response_guard.py: on an empty completion (a successful
// stream that produced no visible text, no reasoning, and no tool calls)
// each retry re-bills the full input, so the retries are budgeted — and a
// streak that PROVES the emptiness is deterministic (same model, usage
// showing zero generated tokens, twice in a row) skips the remaining
// retries and fails the turn fast instead of burning paid attempts on a
// portal-proxied refusal that will not clear.
//
// The guard fails OPEN: ambiguous evidence (missing usage, any generated
// tokens, a mid-streak model switch) keeps the full retry budget. A
// gateway that reports no usage at all gets the benefit of the doubt.

// EmptyResponseSentinel is persisted as the assistant reply when the
// ladder gives up. A blank assistant row between user turns is what
// re-seeds empty loops on replay (the next request carries an assistant
// turn that said nothing, and models answer emptiness with emptiness);
// the sentinel keeps the history honest without inventing content.
const EmptyResponseSentinel = "(empty)"

// maxEmptyResponseRetries bounds the empty-response ladder: one initial
// attempt plus this many retries, each re-billing the full input.
const maxEmptyResponseRetries = 2

// emptyAttempt is one observed empty completion within the current turn.
type emptyAttempt struct {
        model string
        // usagePresent: the provider reported believable usage (input tokens
        // above zero — a present-but-empty usage object is no evidence).
        usagePresent bool
        // zeroOutput: usage proves nothing was generated. Reasoning tokens
        // count as generation, so a thinking-only response is never a
        // deterministic empty.
        zeroOutput bool
        // observedGeneration: non-reasoning evidence that the model produced
        // something this attempt (reasoning text seen by the engine).
        observedGeneration bool
}

// emptyResponseGuard tracks the empty streak for one user turn.
type emptyResponseGuard struct {
        attempts []emptyAttempt
        retries  int
}

// observe records one empty completion and reports whether the round
// should be retried (true) or the ladder is done (false: deterministic
// emptiness proven, or the budget spent).
func (g *emptyResponseGuard) observe(model string, usage llm.Usage, observedGeneration bool) bool {
        // Usage proof, failing open: no input tokens means the provider (or
        // proxy) reported nothing believable.
        usagePresent := usage.PromptTokens > 0
        zeroOutput := usagePresent && usage.CompletionTokens == 0
        g.attempts = append(g.attempts, emptyAttempt{
                model:              model,
                usagePresent:       usagePresent,
                zeroOutput:         zeroOutput,
                observedGeneration: observedGeneration,
        })
        if g.deterministic() {
                return false
        }
        if g.retries >= maxEmptyResponseRetries {
                return false
        }
        g.retries++
        return true
}

// deterministic reports whether the streak proves the emptiness is the
// model's settled behavior: at least two consecutive attempts from the
// same model, where either every attempt's usage proves zero output, or
// every attempt reported no usage AND showed no generation. Reasoning
// counts as generation on both paths — a thinking-only response is never
// a deterministic empty. Mixed evidence fails open so ambiguous
// transients keep their retries.
func (g *emptyResponseGuard) deterministic() bool {
        if len(g.attempts) < 2 {
                return false
        }
        first := g.attempts[0].model
        usageProves := true
        responseProves := true
        for _, a := range g.attempts {
                if a.model != first {
                        return false
                }
                if !a.usagePresent || !a.zeroOutput || a.observedGeneration {
                        usageProves = false
                }
                if a.usagePresent || a.observedGeneration {
                        responseProves = false
                }
        }
        return usageProves || responseProves
}

// emptyBackoffDelay is the engine's sleep for empty retries; a var so
// tests can collapse it instead of waiting out real seconds.
var emptyBackoffDelay = emptyBackoff

// emptyBackoff is the wait before the n-th empty retry (1-based), with
// jitter so concurrent sessions don't synchronize their retries.
func emptyBackoff(n int) time.Duration {
        base := time.Duration(1<<uint(min(n, 3))) * 2 * time.Second // 4s, 8s, 16s
        wait := base + time.Duration(rand.Int64N(int64(base)/2))
        return min(wait, 30*time.Second)
}

// emptyCompletion reports whether a completed assistant message is an
// empty completion: no visible text, no reasoning, no tool calls. This is
// the ladder's entry condition — a thinking-only response is generation
// (and a different recovery's problem), and a tool-call round is work.
func emptyCompletion(msg llm.Message) bool {
        return len(msg.ToolCalls) == 0 &&
                strings.TrimSpace(msg.Content) == "" &&
                strings.TrimSpace(msg.ReasoningContent) == ""
}
