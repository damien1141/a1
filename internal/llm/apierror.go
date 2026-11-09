package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxAPIErrorBodyChars caps the raw-body fallback in FormatAPIError so a
// non-JSON error page can never flood the terminal or the session history.
const maxAPIErrorBodyChars = 2000

// APIError is a non-2xx provider response. FormatAPIError returns one, so
// the status code and the extracted message survive as data — callers that
// must branch on them (overflow recovery) no longer re-parse the formatted
// string. Error() keeps the exact "<provider> API error (<status>):
// <message>" rendering these errors always had.
type APIError struct {
	Provider string
	Status   int
	Message  string
	// RetryAfter is the provider's Retry-After hint when the response
	// carried one (429s usually do). Zero means no hint. The credential
	// pool honors it verbatim as the failing key's cooldown.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s API error (%d): %s", e.Provider, e.Status, e.Message)
}

// APIErrorOf unwraps err to its *APIError, reporting whether it is one.
// Wrapped chains (fmt.Errorf("%w")) are followed.
func APIErrorOf(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// FormatAPIError builds a compact, human-readable error for a non-2xx
// provider response: "<provider> API error (<status>): <message>". Provider
// JSON error envelopes are unwrapped to their message so raw JSON never
// reaches the UI; the trimmed raw body (capped) is the fallback for bodies
// with no recognizable envelope.
func FormatAPIError(provider string, status int, body []byte) error {
	msg := apiErrorMessage(body)
	if msg == "" {
		msg = strings.TrimSpace(string(body))
		if r := []rune(msg); len(r) > maxAPIErrorBodyChars {
			msg = string(r[:maxAPIErrorBodyChars]) + "…"
		}
	}
	if msg == "" {
		msg = "empty error response"
	}
	return &APIError{Provider: provider, Status: status, Message: msg}
}

// FormatAPIErrorHeaders is FormatAPIError with the response headers in
// hand, so a provider Retry-After survives into the APIError the
// credential pool rotates on. A nil header map reads as no hint.
func FormatAPIErrorHeaders(provider string, status int, body []byte, header http.Header) error {
	err := FormatAPIError(provider, status, body)
	if apiErr, ok := err.(*APIError); ok && header != nil {
		apiErr.RetryAfter = ParseRetryAfter(header.Get("Retry-After"))
	}
	return err
}

// ParseRetryAfter reads a Retry-After header value: delta-seconds or an
// HTTP-date. It returns zero when the value is absent, unparsable, or
// already past — never a negative wait.
func ParseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// apiErrorMessage extracts the human-readable message from a provider error
// JSON body. OpenAI-compatible, Anthropic, and Gemini APIs all nest the
// reason under "error" as either an object with a "message" field or a plain
// string; some proxies use a flat top-level {"message": …}. It returns ""
// when the body has no recognizable message.
func apiErrorMessage(body []byte) string {
	var env struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ""
	}
	if len(env.Error) > 0 && !bytes.Equal(bytes.TrimSpace(env.Error), []byte("null")) {
		if msg := errorObjectMessage(env.Error); msg != "" {
			return msg
		}
	}
	return env.Message
}

// errorObjectMessage reads a nested "error" value that is either an object
// with a "message" field ({"error":{"message": …}}) or a bare string
// ({"error":"…"}).
func errorObjectMessage(raw json.RawMessage) string {
	var obj struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Message != "" {
		return obj.Message
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	return ""
}
