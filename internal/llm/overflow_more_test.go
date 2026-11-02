package llm

import (
	"errors"
	"testing"
)

// TestIsContextOverflowMorePatterns covers the overflow phrasings added for
// local-server compatibility: ollama's "input length (N) exceeds context
// length (M)" and the context-size/limit variants smaller daemons emit.
// Recovery from these errors is the difference between auto-compaction
// rescuing the turn and the turn dying on streak exhaustion.
func TestIsContextOverflowMorePatterns(t *testing.T) {
	overflows := []string{
		"input length (12000) exceeds context length (8192)",
		"input length exceeds context length",
		"request exceeds the context size",
		"the request exceeds context size of the model",
		"prompt exceeds the context limit of 32768 tokens",
		"request exceeds context limit",
	}
	for _, msg := range overflows {
		if !IsContextOverflow(errors.New(msg)) {
			t.Errorf("IsContextOverflow(%q) = false, want true", msg)
		}
	}
	notOverflows := []string{
		"rate limit exceeded: too many tokens per minute",
		"Throttling error: too many tokens",
		"connection refused",
	}
	for _, msg := range notOverflows {
		if IsContextOverflow(errors.New(msg)) {
			t.Errorf("IsContextOverflow(%q) = true, want false", msg)
		}
	}
}

// TestParseContextLimitOllamaStyle extracts the LIMIT (the second number),
// never the request size, from ollama's parenthesized form.
func TestParseContextLimitOllamaStyle(t *testing.T) {
	limit, ok := ParseContextLimit(errors.New("input length (12000) exceeds context length (8192)"))
	if !ok {
		t.Fatal("ParseContextLimit(ollama form) ok = false, want true")
	}
	if limit != 8192 {
		t.Errorf("ParseContextLimit(ollama form) = %d, want 8192", limit)
	}
}
