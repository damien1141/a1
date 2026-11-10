package llm

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyExtraHeaders(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/chat", nil)
	req.Header.Set("Authorization", "Bearer built-in")

	ApplyExtraHeaders(req, map[string]string{
		"X-Provider":    "munito",
		"authorization": "Bearer override", // canonical form collides with the default
		"  X-Trimmed  ": " padded value kept verbatim ",
		"":              "dropped",
		"   ":           "dropped too",
	})

	assert.Equal(t, "munito", req.Header.Get("X-Provider"), "extra routing headers are added")
	assert.Equal(t, "Bearer override", req.Header.Get("Authorization"),
		"configured headers land after the client defaults, so they can override them")
	assert.Equal(t, " padded value kept verbatim ", req.Header.Get("X-Trimmed"),
		"names are trimmed, values go out verbatim")
	assert.Equal(t, []string(nil), req.Header.Values(""), "blank header names are dropped")
}

func TestApplyExtraHeadersNoop(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/chat", nil)
	before := req.Header.Clone()

	ApplyExtraHeaders(req, nil)
	assert.Equal(t, before, req.Header, "nil map keeps the request untouched")

	ApplyExtraHeaders(req, map[string]string{})
	assert.Equal(t, before, req.Header, "empty map keeps the request untouched")
}
