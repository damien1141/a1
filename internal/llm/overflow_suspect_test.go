package llm

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsSuspectedOverflow pins the classifier for provider refusals that
// could be context overflow but cannot be proven one: the opaque 400s a
// reason-swallowing gateway answers with ("Provider returned error") and
// 413s, versus every ordinary error that must stay ordinary.
func TestIsSuspectedOverflow(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("boom"), false},
		{
			// The exact brick: a long session behind a gateway that
			// swallows the upstream reason. Every turn re-sends the
			// same oversized history and fails with this same text.
			name: "the gateway brick",
			err:  FormatAPIError("LLM", http.StatusBadRequest, []byte(`{"error":{"message":"Provider returned error"}}`)),
			want: true,
		},
		{
			name: "provider error lowercase",
			err:  FormatAPIError("LLM", http.StatusBadRequest, []byte(`{"error":{"message":"provider error"}}`)),
			want: true,
		},
		{
			name: "upstream provider error",
			err:  FormatAPIError("LLM", http.StatusBadRequest, []byte(`{"error":{"message":"Upstream provider error"}}`)),
			want: true,
		},
		{
			name: "bare bad request body",
			err:  FormatAPIError("LLM", http.StatusBadRequest, []byte(`Bad Request`)),
			want: true,
		},
		{
			name: "wrapped opaque 400 still resolves",
			err:  fmt.Errorf("stream: %w", FormatAPIError("LLM", http.StatusBadRequest, []byte(`{"error":{"message":"Provider returned error"}}`))),
			want: true,
		},
		{
			name: "413 is always a too-large condition",
			err:  FormatAPIError("LLM", http.StatusRequestEntityTooLarge, []byte(`<html>413</html>`)),
			want: true,
		},
		{
			name: "413 with rate-limit text is not suspected",
			err:  FormatAPIError("LLM", http.StatusRequestEntityTooLarge, []byte(`{"error":{"message":"rate limit exceeded"}}`)),
			want: false,
		},
		{
			name: "informative 400 stays ordinary",
			err:  FormatAPIError("LLM", http.StatusBadRequest, []byte(`{"error":{"message":"Invalid model: nope"}}`)),
			want: false,
		},
		{
			name: "401 is auth, never overflow",
			err:  FormatAPIError("LLM", http.StatusUnauthorized, []byte(`{"error":{"message":"Incorrect API key provided"}}`)),
			want: false,
		},
		{
			name: "429 is throttling",
			err:  FormatAPIError("LLM", http.StatusTooManyRequests, []byte(`{"error":{"message":"Too many requests"}}`)),
			want: false,
		},
		{
			name: "proven overflow is not suspected — it is known",
			err:  FormatAPIError("LLM", http.StatusBadRequest, []byte(`{"error":{"message":"prompt is too long: 210000 > 200000 tokens"}}`)),
			want: false,
		},
		{
			name: "413 with overflow text is a known overflow, not a suspect",
			err:  FormatAPIError("LLM", http.StatusRequestEntityTooLarge, []byte(`{"error":{"message":"request body too large"}}`)),
			want: false,
		},
		{
			name: "empty-body 400 has nothing to judge",
			err:  FormatAPIError("LLM", http.StatusBadRequest, []byte(``)),
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsSuspectedOverflow(tc.err))
		})
	}
}

// TestIsContextOverflow413Body pins the request-size phrasings as proven
// overflows: a 413 that says why is ground truth, not a suspicion.
func TestIsContextOverflow413Body(t *testing.T) {
	assert.True(t, IsContextOverflow(FormatAPIError("LLM", 413, []byte(`{"error":{"message":"request body too large"}}`))))
	assert.True(t, IsContextOverflow(FormatAPIError("LLM", 413, []byte(`{"error":{"message":"Payload Too Large"}}`))))
	assert.True(t, IsContextOverflow(FormatAPIError("LLM", 400, []byte(`{"error":{"message":"request entity too large for the upstream"}}`))))
}
