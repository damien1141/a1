package llm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/llm"
)

// TestSameOrigin_SeparatesPorts_Differential is the regression pin for smart
// session titles being disabled on a jev-style memory model.
//
// The naming guard used IsLocalBaseURL, which asks "is the ops endpoint local".
// The hazard it guards — a different-prefix request evicting the conversation's
// prompt cache — only exists when the ops call hits the SAME server. A memory
// model at 127.0.0.1:8017 with the chat at 127.0.0.1:8080 is local but a
// different server: no shared KV cache, nothing to evict, and gating naming on
// the weaker predicate turned the feature off for no benefit.
func TestSameOrigin_SeparatesPorts_Differential(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"same origin", "http://127.0.0.1:8080/v1", "http://127.0.0.1:8080", true},
		{"same origin, different path", "http://127.0.0.1:8080/v1", "http://127.0.0.1:8080/v1/other", true},
		// 127.0.0.1 and localhost are the same machine but NOT the same origin
		// string, and SameOrigin compares origins literally. That is the
		// conservative direction: it only waves a call through when the endpoints
		// are spelled identically, which is what a config author does.
		{"distinct host spellings", "http://127.0.0.1:8080", "http://localhost:8080", false},
		{"different port, both loopback", "http://127.0.0.1:8017/v1", "http://127.0.0.1:8080/v1", false},
		{"loopback vs private", "http://127.0.0.1:8080", "http://192.168.1.9:8080", false},
		{"local vs remote", "http://127.0.0.1:8080", "https://api.example.com", false},
		{"both remote", "https://api.example.com", "https://api.example.com", false},
		{"empty", "", "http://127.0.0.1:8080", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, llm.SameOrigin(tc.a, tc.b))
		})
	}
}

// TestSameOrigin_LocalIsNotEnough_Positive is the exact shape of the reported
// bug: both URLs local, but different servers, so ops must NOT be treated as
// sharing the conversation's cache.
func TestSameOrigin_LocalIsNotEnough_Positive(t *testing.T) {
	const ops = "http://127.0.0.1:8017/v1"
	const chat = "http://127.0.0.1:8080/v1"

	assert.True(t, llm.IsLocalBaseURL(ops), "precondition: the ops endpoint IS local")
	assert.False(t, llm.SameOrigin(ops, chat),
		"but it is a different server, so naming must not be gated off")
}
