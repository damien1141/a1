package jev

import (
        "encoding/json"
        "net/http"
        "net/http/httptest"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// fakeOllamaServer stands in for a local Ollama server: it records the
// chat request and replies with canned assistant content on one NDJSON
// line, the shape the api client expects for stream=false.
func fakeOllamaServer(t *testing.T, content string) (*httptest.Server, *map[string]any) {
        t.Helper()
        got := map[string]any{}
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                require.Equal(t, "/api/chat", r.URL.Path)
                require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
                w.Header().Set("Content-Type", "application/x-ndjson")
                _ = json.NewEncoder(w).Encode(map[string]any{
                        "model":             "qwen3:4b",
                        "message":           map[string]any{"role": "assistant", "content": content},
                        "done":              true,
                        "prompt_eval_count": 41,
                        "eval_count":        7,
                })
        }))
        t.Cleanup(srv.Close)
        return srv, &got
}

func testOllamaClient(t *testing.T, baseURL string) *OllamaClient {
        t.Helper()
        return NewOllamaClient(OllamaConfig{BaseURL: baseURL, Model: "qwen3:4b"})
}

// TestOllamaAnswersAllThreeQuestionTypes pins the happy path: one call,
// noul + choice + score questions, every answer parsed with its value and
// confidence, usage and model mapped from the chat response.
func TestOllamaAnswersAllThreeQuestionTypes(t *testing.T) {
        content := `{
                "risky": {"noul": 0.8, "confidence": 0.7},
                "engine": {"choice": "mdn", "confidence": 0.9},
                "outcome": {"score": 4, "confidence": 0.85}
        }`
        srv, got := fakeOllamaServer(t, content)
        client := testOllamaClient(t, srv.URL)

        resp, err := client.SystemOneWithOptions(t.Context(), "css flexbox", Questions{
                "risky":   Noul{Instructions: "is this risky"},
                "engine":  Choice{Criteria: map[string]any{"mdn": "web docs", "github": "repos"}},
                "outcome": Score{Criteria: []any{"not a check", "failed", "unclear", "likely passed", "clearly passed"}},
        }, Options{})
        require.NoError(t, err)

        assert.Equal(t, "qwen3:4b", resp.Model)
        assert.Equal(t, Usage{InputTokens: 41, OutputTokens: 7}, resp.Usage)

        noul := resp.Answers["risky"]
        assert.Equal(t, AnswerNoul, noul.Type)
        assert.InDelta(t, 0.8, noul.Noul, 1e-9)
        assert.InDelta(t, 0.7, noul.Confidence, 1e-9)

        choice := resp.Answers["engine"]
        assert.Equal(t, AnswerChoice, choice.Type)
        assert.Equal(t, "mdn", choice.Choice)
        assert.InDelta(t, 0.9, choice.Confidence, 1e-9)

        score := resp.Answers["outcome"]
        assert.Equal(t, AnswerScore, score.Type)
        assert.InDelta(t, 4, score.Score, 1e-9)
        assert.InDelta(t, 0.85, score.Confidence, 1e-9)

        // The wire contract: constrained JSON, no stream, no thinking, zero
        // temperature, bounded generation, and a decision prompt that carries
        // the state and the questions as JSON.
        assert.Equal(t, "qwen3:4b", (*got)["model"])
        assert.Equal(t, false, (*got)["stream"])
        assert.Equal(t, "json", (*got)["format"])
        assert.Equal(t, false, (*got)["think"])
        options := (*got)["options"].(map[string]any)
        assert.InDelta(t, 0.0, options["temperature"].(float64), 1e-9)
        assert.InDelta(t, float64(ollamaNumPredict), options["num_predict"].(float64), 1e-9)
        messages := (*got)["messages"].([]any)
        require.Len(t, messages, 2)
        system := messages[0].(map[string]any)
        user := messages[1].(map[string]any)
        assert.Equal(t, "system", system["role"])
        assert.Contains(t, system["content"], "decision model")
        assert.Equal(t, "user", user["role"])
        assert.Contains(t, user["content"], "STATE:")
        assert.Contains(t, user["content"], `"css flexbox"`)
        assert.Contains(t, user["content"], "QUESTIONS:")
        assert.Contains(t, user["content"], "mdn")
}

// TestOllamaFenceWrappedJSONParsed: format:"json" should make fences
// impossible, but a model that wraps anyway still parses — the strip is
// the backstop, the validation after it stays strict.
func TestOllamaFenceWrappedJSONParsed(t *testing.T) {
        srv, _ := fakeOllamaServer(t, "```json\n{\"engine\": {\"choice\": \"github\", \"confidence\": 0.8}}\n```")
        client := testOllamaClient(t, srv.URL)

        resp, err := client.SystemOneWithOptions(t.Context(), "find repo", Questions{
                "engine": Choice{Criteria: map[string]any{"mdn": "docs", "github": "repos"}},
        }, Options{})
        require.NoError(t, err)
        assert.Equal(t, "github", resp.Answers["engine"].Choice)
}

// TestOllamaChoiceOutsideCriteriaRejected: an invented alternative is an
// error, never a best-effort match — the call site falls back instead.
func TestOllamaChoiceOutsideCriteriaRejected(t *testing.T) {
        srv, _ := fakeOllamaServer(t, `{"engine": {"choice": "StackOverflow", "confidence": 0.99}}`)
        client := testOllamaClient(t, srv.URL)

        _, err := client.SystemOneWithOptions(t.Context(), "q", Questions{
                "engine": Choice{Criteria: map[string]any{"mdn": "docs", "github": "repos"}},
        }, Options{})
        require.Error(t, err)
        assert.Contains(t, err.Error(), "not one of the named alternatives")
}

// TestOllamaScoreValidationRejected: out-of-range and fractional rubric
// levels are errors.
func TestOllamaScoreValidationRejected(t *testing.T) {
        questions := Questions{
                "outcome": Score{Criteria: []any{"not a check", "failed", "unclear", "likely passed", "clearly passed"}},
        }
        for _, content := range []string{
                `{"outcome": {"score": 5, "confidence": 0.9}}`,
                `{"outcome": {"score": -1, "confidence": 0.9}}`,
                `{"outcome": {"score": 2.5, "confidence": 0.9}}`,
        } {
                srv, _ := fakeOllamaServer(t, content)
                client := testOllamaClient(t, srv.URL)
                _, err := client.SystemOneWithOptions(t.Context(), "q", questions, Options{})
                require.Error(t, err, "content: %s", content)
                assert.Contains(t, err.Error(), "rubric")
        }
}

// TestOllamaMissingConfidenceRejected: choice and score answers without a
// confidence are rejected — the same required-field contract the TypeSafe
// wire enforces, so an uncertain model cannot fake certainty by omission.
func TestOllamaMissingConfidenceRejected(t *testing.T) {
        srv, _ := fakeOllamaServer(t, `{"engine": {"choice": "mdn"}}`)
        client := testOllamaClient(t, srv.URL)
        _, err := client.SystemOneWithOptions(t.Context(), "q", Questions{
                "engine": Choice{Criteria: map[string]any{"mdn": "docs"}},
        }, Options{})
        require.Error(t, err)
        assert.Contains(t, err.Error(), `has no "confidence"`)

        srv2, _ := fakeOllamaServer(t, `{"outcome": {"score": 3}}`)
        client2 := testOllamaClient(t, srv2.URL)
        _, err = client2.SystemOneWithOptions(t.Context(), "q", Questions{
                "outcome": Score{Criteria: []any{"a", "b", "c", "d"}},
        }, Options{})
        require.Error(t, err)
        assert.Contains(t, err.Error(), `has no "confidence"`)
}

// TestOllamaUnansweredQuestionRejected: a reply that skips an asked
// question errors — mirroring the TypeSafe "left question unanswered".
func TestOllamaUnansweredQuestionRejected(t *testing.T) {
        srv, _ := fakeOllamaServer(t, `{"engine": {"choice": "mdn", "confidence": 0.9}}`)
        client := testOllamaClient(t, srv.URL)
        _, err := client.SystemOneWithOptions(t.Context(), "q", Questions{
                "engine":  Choice{Criteria: map[string]any{"mdn": "docs"}},
                "outcome": Score{Criteria: []any{"a", "b"}},
        }, Options{})
        require.Error(t, err)
        assert.Contains(t, err.Error(), `left question "outcome" unanswered`)
}

// TestOllamaNonJSONRejected: prose instead of JSON is an error, not a
// best-effort parse.
func TestOllamaNonJSONRejected(t *testing.T) {
        srv, _ := fakeOllamaServer(t, "The best engine is MDN because of documentation.")
        client := testOllamaClient(t, srv.URL)
        _, err := client.SystemOneWithOptions(t.Context(), "q", Questions{
                "engine": Choice{Criteria: map[string]any{"mdn": "docs"}},
        }, Options{})
        require.Error(t, err)
        assert.Contains(t, err.Error(), "not valid JSON")
}

// TestOllamaConfidenceRangeRejected: a confidence outside 0..1 is an error.
func TestOllamaConfidenceRangeRejected(t *testing.T) {
        srv, _ := fakeOllamaServer(t, `{"engine": {"choice": "mdn", "confidence": 1.5}}`)
        client := testOllamaClient(t, srv.URL)
        _, err := client.SystemOneWithOptions(t.Context(), "q", Questions{
                "engine": Choice{Criteria: map[string]any{"mdn": "docs"}},
        }, Options{})
        require.Error(t, err)
        assert.Contains(t, err.Error(), "outside 0..1")
}

// TestOllamaNilStateRejected mirrors the TypeSafe path's state contract.
func TestOllamaNilStateRejected(t *testing.T) {
        srv, _ := fakeOllamaServer(t, `{}`)
        client := testOllamaClient(t, srv.URL)
        _, err := client.SystemOneWithOptions(t.Context(), nil, Questions{
                "engine": Choice{Criteria: map[string]any{"mdn": "docs"}},
        }, Options{})
        require.ErrorIs(t, err, errStateRequired)
}

// TestOllamaServerErrorSurfaces: a non-2xx server response becomes an
// error naming the backend, so the debuglog line at the call site says
// what failed.
func TestOllamaServerErrorSurfaces(t *testing.T) {
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.WriteHeader(http.StatusServiceUnavailable)
                _, _ = w.Write([]byte(`{"error": "model is loading"}` + "\n"))
        }))
        t.Cleanup(srv.Close)
        client := testOllamaClient(t, srv.URL)
        _, err := client.SystemOneWithOptions(t.Context(), "q", Questions{
                "engine": Choice{Criteria: map[string]any{"mdn": "docs"}},
        }, Options{})
        require.Error(t, err)
        assert.Contains(t, err.Error(), "ollama chat")
}

// TestOllamaModelOverride: Options.Model wins over the configured tag —
// the same override semantics the TypeSafe client honors.
func TestOllamaModelOverride(t *testing.T) {
        srv, got := fakeOllamaServer(t, `{"engine": {"choice": "mdn", "confidence": 0.9}}`)
        client := testOllamaClient(t, srv.URL)
        _, err := client.SystemOneWithOptions(t.Context(), "q", Questions{
                "engine": Choice{Criteria: map[string]any{"mdn": "docs"}},
        }, Options{Model: "llama3.2:3b"})
        require.NoError(t, err)
        assert.Equal(t, "llama3.2:3b", (*got)["model"])
}

// TestResolveOllamaBaseURL pins the endpoint resolution: explicit config,
// then OLLAMA_HOST in its three accepted shapes, then the loopback
// default.
func TestResolveOllamaBaseURL(t *testing.T) {
        t.Setenv("OLLAMA_HOST", "")
        assert.Equal(t, "http://127.0.0.1:11434", resolveOllamaBaseURL("").String())

        t.Setenv("OLLAMA_HOST", "localhost:11500")
        assert.Equal(t, "http://localhost:11500", resolveOllamaBaseURL("").String(), "schemeless OLLAMA_HOST")

        t.Setenv("OLLAMA_HOST", "http://127.0.0.1:11501")
        assert.Equal(t, "http://127.0.0.1:11501", resolveOllamaBaseURL("").String())

        t.Setenv("OLLAMA_HOST", "myhost")
        assert.Equal(t, "http://myhost:11434", resolveOllamaBaseURL("").String(), "host without port gets the ollama default")

        // Explicit config wins over the environment.
        assert.Equal(t, "http://127.0.0.1:11502", resolveOllamaBaseURL("http://127.0.0.1:11502").String())

        // Schemeless config gets a scheme and the default port.
        assert.Equal(t, "http://127.0.0.1:11434", resolveOllamaBaseURL("127.0.0.1").String())
}

// TestStripFences covers the defensive fence strip.
func TestStripFences(t *testing.T) {
        assert.Equal(t, `{"a":1}`, stripFences("```json\n{\"a\":1}\n```"))
        assert.Equal(t, `{"a":1}`, stripFences("```\n{\"a\":1}\n```"))
        assert.Equal(t, `{"a":1}`, stripFences(`{"a":1}`))
        assert.Equal(t, "", stripFences("```"))
        assert.True(t, strings.HasPrefix(stripFences("```json\n{\"a\":1}"), "{"))
}
