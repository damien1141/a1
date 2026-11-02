package llm

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsContextOverflow(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "empty", err: errors.New(""), want: false},
		{name: "unrelated", err: errors.New("connection reset"), want: false},
		{
			name: "claude prompt too long",
			err: FormatAPIError(
				"anthropic",
				400,
				[]byte(`{"error":{"message":"prompt is too long: 210000 > 200000 tokens"}}`),
			),
			want: true,
		},
		{
			name: "openai-compatible context window",
			err: FormatAPIError(
				"LLM",
				400,
				[]byte(`{"error":{"message":"Your input exceeds the context window of this model"}}`),
			),
			want: true,
		},
		{
			name: "gemini token count",
			err: FormatAPIError(
				"gemini",
				400,
				[]byte(
					`{"error":{"message":"The input token count (1196265) exceeds the maximum number of tokens allowed (1048575)"}}`,
				),
			),
			want: true,
		},
		{
			name: "generic context_length_exceeded",
			err:  errors.New("context_length_exceeded"),
			want: true,
		},
		{
			name: "rate limit excluded",
			err:  errors.New("rate limit: too many tokens"),
			want: false,
		},
		{
			name: "throttling excluded",
			err:  errors.New("throttling error: too many tokens, please wait before trying again"),
			want: false,
		},
		{
			name: "wrapped overflow",
			err:  fmt.Errorf("stream: %w", errors.New("prompt is too long: 1 > 0")),
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsContextOverflow(tt.err))
		})
	}
}
