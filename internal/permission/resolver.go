package permission

import (
	"context"
	"fmt"
	"strings"
)

// Resolver evaluates a call-scoped authority request and returns a Ruling.
// Implementations may be deterministic, human-in-the-loop, or LLM-as-judge.
type Resolver interface {
	// Resolve evaluates req and returns a terminal Allow/Deny decision with reason.
	// Returning Ask means the resolver cannot decide (e.g., pending human review).
	Resolve(ctx context.Context, callHash string, req Request) (Decision, string)
}

// RuleBasedResolver applies deterministic rules to authority decisions.
// It matches the request against a list of rules in order; the first match wins.
type RuleBasedResolver struct {
	rules []Rule
}

// Rule is a single deterministic authority rule.
type Rule struct {
	// MatchCommand is a regex matched against req.Command (bash actions).
	// Empty means match all.
	MatchCommand string
	// MatchTool is an exact tool name match. Empty means match all.
	MatchTool string
	// Decision is the rule's outcome.
	Decision Decision
	// Reason is the explanation for the decision.
	Reason string
}

// NewRuleBasedResolver creates a resolver with the given rules.
func NewRuleBasedResolver(rules []Rule) *RuleBasedResolver {
	return &RuleBasedResolver{rules: rules}
}

// Resolve evaluates req against rules and returns the first match.
func (r *RuleBasedResolver) Resolve(ctx context.Context, callHash string, req Request) (Decision, string) {
	_ = ctx
	_ = callHash
	for _, rule := range r.rules {
		if !r.matches(rule, req) {
			continue
		}
		return rule.Decision, rule.Reason
	}
	return Ask, "no matching rule"
}

func (r *RuleBasedResolver) matches(rule Rule, req Request) bool {
	if rule.MatchTool != "" && !strings.EqualFold(rule.MatchTool, req.Tool) {
		return false
	}
	if rule.MatchCommand != "" && req.Command != "" {
		// Simple substring match for now; full regex can be added later.
		if !strings.Contains(req.Command, rule.MatchCommand) {
			return false
		}
	}
	return true
}

// HumanReviewResolver defers to an external async callback.
// It returns Ask immediately and invokes callback in the background.
type HumanReviewResolver struct {
	// Callback is invoked asynchronously with the ruling result.
	// It must be thread-safe.
	Callback func(ctx context.Context, callHash string, req Request, dec Decision, reason string)
	// Timeout is ignored; human review is always async.
	_ int
}

// Resolve returns Ask immediately and fires the callback asynchronously.
func (h *HumanReviewResolver) Resolve(ctx context.Context, callHash string, req Request) (Decision, string) {
	if h.Callback != nil {
		go func() {
			h.Callback(ctx, callHash, req, Ask, "pending human review")
		}()
	}
	return Ask, "pending human review"
}

// LLMJudgeResolver consults an LLM judge with a mandate check.
// The mandate is a simple string that must appear in the judge's response
// for the decision to be accepted.
type LLMJudgeResolver struct {
	// Mandate is the required phrase the judge must include.
	Mandate string
	// Judge is the LLM judge function. It should return a decision and reason.
	Judge func(ctx context.Context, prompt string) (Decision, string)
}

// Resolve consults the LLM judge and validates the mandate.
func (l *LLMJudgeResolver) Resolve(ctx context.Context, callHash string, req Request) (Decision, string) {
	_ = callHash
	prompt := fmt.Sprintf("Evaluate authority request for tool=%s command=%q. Mandate: %s",
		req.Tool, req.Command, l.Mandate)
	if l.Judge == nil {
		return Ask, "no judge configured"
	}
	dec, reason := l.Judge(ctx, prompt)
	if !strings.Contains(reason, l.Mandate) {
		return Ask, fmt.Sprintf("judge response missing mandate %q", l.Mandate)
	}
	return dec, reason
}
