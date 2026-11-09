package llm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatAPIError(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		status   int
		body     string
		want     string
	}{
		{
			name:     "openai envelope",
			provider: "LLM",
			status:   401,
			body:     `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`,
			want:     "LLM API error (401): Incorrect API key provided",
		},
		{
			name:     "anthropic envelope",
			provider: "anthropic",
			status:   400,
			body:     `{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 210000 > 200000 tokens"}}`,
			want:     "anthropic API error (400): prompt is too long: 210000 > 200000 tokens",
		},
		{
			name:     "gemini envelope",
			provider: "gemini",
			status:   400,
			body:     `{"error":{"code":400,"message":"API key not valid. Please pass a valid API key.","status":"INVALID_ARGUMENT"}}`,
			want:     "gemini API error (400): API key not valid. Please pass a valid API key.",
		},
		{
			name:     "error is a bare string",
			provider: "gemini",
			status:   404,
			body:     `{"error":"model not found"}`,
			want:     "gemini API error (404): model not found",
		},
		{
			name:     "flat message field",
			provider: "LLM",
			status:   413,
			body:     `{"message":"request body too large"}`,
			want:     "LLM API error (413): request body too large",
		},
		{
			name:     "non-json body falls back to raw text",
			provider: "LLM",
			status:   502,
			body:     "gateway unavailable\nplease retry",
			want:     "LLM API error (502): gateway unavailable\nplease retry",
		},
		{
			name:     "json without envelope falls back to raw body",
			provider: "LLM",
			status:   404,
			body:     `{"detail":"not found"}`,
			want:     "LLM API error (404): {\"detail\":\"not found\"}",
		},
		{
			name:     "empty error object falls back to raw body",
			provider: "LLM",
			status:   400,
			body:     `{"error":{}}`,
			want:     "LLM API error (400): {\"error\":{}}",
		},
		{
			name:     "empty body",
			provider: "LLM",
			status:   502,
			body:     "",
			want:     "LLM API error (502): empty error response",
		},
		{
			name:     "whitespace-only body",
			provider: "LLM",
			status:   502,
			body:     "  \n ",
			want:     "LLM API error (502): empty error response",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := FormatAPIError(tt.provider, tt.status, []byte(tt.body))
			require.Error(t, err)
			assert.Equal(t, tt.want, err.Error())
		})
	}
}

func TestFormatAPIErrorCapsRawFallback(t *testing.T) {
	big := strings.Repeat("x", maxAPIErrorBodyChars+500)
	err := FormatAPIError("LLM", 502, []byte(big))

	msg := err.Error()
	assert.Equal(t, "LLM API error (502): "+strings.Repeat("x", maxAPIErrorBodyChars)+"…", msg)
}

// TestAPIErrorOf pins the structured surface of FormatAPIError: the status
// and the extracted message survive as data (overflow recovery branches on
// them), wrapped chains resolve, and ordinary errors do not.
func TestAPIErrorOf(t *testing.T) {
	err := FormatAPIError("LLM", 400, []byte(`{"error":{"message":"Provider returned error"}}`))
	apiErr, ok := APIErrorOf(err)
	require.True(t, ok)
	assert.Equal(t, "LLM", apiErr.Provider)
	assert.Equal(t, 400, apiErr.Status)
	assert.Equal(t, "Provider returned error", apiErr.Message)

	_, ok = APIErrorOf(fmt.Errorf("turn: %w", err))
	assert.True(t, ok, "a wrapped APIError still resolves")

	_, ok = APIErrorOf(errors.New("boom"))
	assert.False(t, ok, "an ordinary error is not an APIError")

	_, ok = APIErrorOf(nil)
	assert.False(t, ok)
}

// TestFormatAPIErrorHeadersRetryAfter pins the header-aware formatter: a
// Retry-After riding on the response survives into the APIError the
// credential pool rotates on, in both wire forms (delta-seconds and
// HTTP-date), and absent or bogus values read as no hint.
func TestFormatAPIErrorHeadersRetryAfter(t *testing.T) {
	mkHeader := func(v string) http.Header {
		h := http.Header{}
		if v != "\x00absent" {
			h.Set("Retry-After", v)
		}
		return h
	}

	tests := []struct {
		name string
		hv   string
		want time.Duration
	}{
		{"delta seconds", "120", 120 * time.Second},
		{"zero is no hint", "0", 0},
		{"negative is no hint", "-5", 0},
		{"absent is no hint", "\x00absent", 0},
		{"garbage is no hint", "soon-ish", 0},
		{"past date is no hint", time.Now().UTC().Add(-time.Hour).Format(http.TimeFormat), 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := FormatAPIErrorHeaders("LLM", 429, []byte(`{"error":{"message":"rate limited"}}`), mkHeader(tc.hv))
			apiErr, ok := APIErrorOf(err)
			require.True(t, ok)
			assert.Equal(t, 429, apiErr.Status)
			assert.Equal(t, tc.want, apiErr.RetryAfter)
		})
	}

	// An HTTP-date in the future resolves to the remaining wait.
	err := FormatAPIErrorHeaders("LLM", 429, nil, mkHeader(time.Now().UTC().Add(90*time.Minute).Format(http.TimeFormat)))
	apiErr, _ := APIErrorOf(err)
	assert.InDelta(t, 90*time.Minute, apiErr.RetryAfter, float64(time.Minute), "future HTTP-date resolves to the remaining wait")

	// A nil header map reads as no hint, not a panic.
	err = FormatAPIErrorHeaders("LLM", 429, nil, nil)
	apiErr, _ = APIErrorOf(err)
	assert.Zero(t, apiErr.RetryAfter)

	// The plain formatter keeps its old shape: no hint even on 429.
	err = FormatAPIError("LLM", 429, nil)
	apiErr, _ = APIErrorOf(err)
	assert.Zero(t, apiErr.RetryAfter)
}
