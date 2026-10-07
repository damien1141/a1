package permission

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRuleBasedResolver_MatchCommand(t *testing.T) {
	resolver := NewRuleBasedResolver([]Rule{
		{MatchCommand: "rm", Decision: Deny, Reason: "rm forbidden"},
		{MatchCommand: "echo", Decision: Allow, Reason: "echo allowed"},
	})

	dec, reason := resolver.Resolve(context.Background(), "h1", Request{Tool: "bash", Command: "rm -rf /"})
	assert.Equal(t, Deny, dec)
	assert.Contains(t, reason, "rm forbidden")

	dec, reason = resolver.Resolve(context.Background(), "h1", Request{Tool: "bash", Command: "echo hello"})
	assert.Equal(t, Allow, dec)
	assert.Contains(t, reason, "echo allowed")
}

func TestRuleBasedResolver_MatchTool(t *testing.T) {
	resolver := NewRuleBasedResolver([]Rule{
		{MatchTool: "write", Decision: Ask, Reason: "write needs review"},
	})

	dec, reason := resolver.Resolve(context.Background(), "h1", Request{Tool: "write", Paths: []string{"/tmp/a"}})
	assert.Equal(t, Ask, dec)
	assert.Contains(t, reason, "write needs review")

	dec, _ = resolver.Resolve(context.Background(), "h1", Request{Tool: "read"})
	assert.Equal(t, Ask, dec, "no match should return Ask")
}

func TestHumanReviewResolver_AsyncCallback(t *testing.T) {
	var called bool
	var gotHash string
	resolver := &HumanReviewResolver{
		Callback: func(ctx context.Context, callHash string, req Request, dec Decision, reason string) {
			called = true
			gotHash = callHash
			_ = ctx
			_ = req
			_ = dec
			_ = reason
		},
	}

	dec, reason := resolver.Resolve(context.Background(), "h1", Request{Tool: "bash", Command: "sudo rm"})
	assert.Equal(t, Ask, dec)
	assert.Contains(t, reason, "pending human review")

	// Give the goroutine a moment.
	time.Sleep(10 * time.Millisecond)
	assert.True(t, called, "callback should be invoked asynchronously")
	assert.Equal(t, "h1", gotHash)
}

func TestLLMJudgeResolver_ValidatesMandate(t *testing.T) {
	resolver := &LLMJudgeResolver{
		Mandate: "SAFE",
		Judge: func(ctx context.Context, prompt string) (Decision, string) {
			_ = ctx
			_ = prompt
			return Allow, "SAFE: command is safe"
		},
	}

	dec, reason := resolver.Resolve(context.Background(), "h1", Request{Tool: "bash", Command: "ls"})
	assert.Equal(t, Allow, dec)
	assert.Contains(t, reason, "SAFE")
}

func TestLLMJudgeResolver_RejectsMissingMandate(t *testing.T) {
	resolver := &LLMJudgeResolver{
		Mandate: "SAFE",
		Judge: func(ctx context.Context, prompt string) (Decision, string) {
			_ = ctx
			_ = prompt
			return Deny, "unsafe"
		},
	}

	dec, reason := resolver.Resolve(context.Background(), "h1", Request{Tool: "bash", Command: "rm -rf /"})
	assert.Equal(t, Ask, dec)
	assert.Contains(t, reason, "missing mandate")
}
