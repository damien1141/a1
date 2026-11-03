package jev

import (
        "encoding/json"
        "fmt"
        "net/http"
        "net/http/httptest"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// fakeOllamaServer stands in for a local Ollama server that predates the
// native endpoint: /v1/systemone 404s and the chat wire answers. It records
// the chat request and replies with canned assistant content on one NDJSON
// line, the shape the api client expects for stream=false.
func fakeOllamaServer(t *testing.T, content string) (*httptest.Server, *map[string]any) {
        t.Helper()
        got := map[string]any{}
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path == systemOnePath {
                        http.NotFound(w, r)
                        return
                }
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

// TestOllamaServerErrorSurfaces: a non-2xx chat response becomes an error
// naming the backend, so the debuglog line at the call site says what
// failed. The native endpoint 404s first, isolating the chat wire.
func TestOllamaServerErrorSurfaces(t *testing.T) {
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path == systemOnePath {
                        http.NotFound(w, r)
                        return
                }
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

// fakeNativeOllamaServer stands in for Ollama >= 0.35: it answers the
// native /v1/systemone endpoint and records the request.
func fakeNativeOllamaServer(t *testing.T, reply string) (*httptest.Server, *map[string]any) {
        t.Helper()
        got := map[string]any{}
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                require.Equal(t, systemOnePath, r.URL.Path)
                require.Equal(t, http.MethodPost, r.Method)
                require.Empty(t, r.Header.Get("Authorization"), "the local endpoint is unauthenticated")
                require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
                w.Header().Set("Content-Type", "application/json")
                fmt.Fprint(w, reply)
        }))
        t.Cleanup(srv.Close)
        return srv, &got
}

// TestOllamaNativeSystemOne pins the native wire against a captured reply
// from a live Ollama >= 0.35 server (the reference script's TEST 1): the
// request is {model, state, questions} — the same schema the hosted
// provider sends — and the reply's facts are the server's, not a guess:
// the echoed model names the model that answered, not the alias asked for;
// a noul answer is a bare probability with no confidence beside it; a
// choice answer carries its own confidence and per-alternative
// probabilities; and the usage of a decision model bills no output tokens.
func TestOllamaNativeSystemOne(t *testing.T) {
        reply := `{
        "model": "jevos-v5",
        "answers": {
                "sentiment": {
                        "type": "choice",
                        "choice": "mixed",
                        "probabilities": {
                                "positive": 0.006936471196406346,
                                "negative": 0.39528015447974413,
                                "neutral": 0.00682414966025108,
                                "mixed": 0.5909592246635984
                        },
                        "confidence": 0.45461229955146454
                },
                "has_churn_number": {
                        "type": "noul",
                        "noul": 0.9928918126946713
                }
        },
        "usage": {
                "input_tokens": 342,
                "output_tokens": 0
        }
}`
        srv, got := fakeNativeOllamaServer(t, reply)
        client := NewOllamaClient(OllamaConfig{BaseURL: srv.URL, Model: "d1:3b"})

        state := "q3 revenue hit 4.2m, but churn spiked to 8% due to the new pricing tier."
        resp, err := client.SystemOneWithOptions(t.Context(), state, Questions{
                "sentiment": Choice{
                        Instructions: "what is the primary sentiment?",
                        Criteria: map[string]any{
                                "positive": "revenue is up and things are good",
                                "negative": "churn spiked and things are bad",
                                "neutral":  "just stating facts",
                                "mixed":    "contains both positive revenue and negative churn",
                        },
                },
                "has_churn_number": Noul{
                        Instructions: "is a specific churn percentage mentioned?",
                        Criteria: &NoulCriteria{
                                True:  "a specific churn percentage is mentioned",
                                False: "no specific churn percentage is mentioned",
                        },
                },
        }, Options{})
        require.NoError(t, err)

        // The request is the reference schema, key for key.
        assert.Equal(t, "d1:3b", (*got)["model"])
        assert.Equal(t, state, (*got)["state"])
        assert.Equal(t, map[string]any{
                "sentiment": map[string]any{
                        "type":         "choice",
                        "instructions": "what is the primary sentiment?",
                        "criteria": map[string]any{
                                "positive": "revenue is up and things are good",
                                "negative": "churn spiked and things are bad",
                                "neutral":  "just stating facts",
                                "mixed":    "contains both positive revenue and negative churn",
                        },
                },
                "has_churn_number": map[string]any{
                        "type":         "noul",
                        "instructions": "is a specific churn percentage mentioned?",
                        "criteria": map[string]any{
                                "true":  "a specific churn percentage is mentioned",
                                "false": "no specific churn percentage is mentioned",
                        },
                },
        }, (*got)["questions"])

        // The reply's model names the model that answered (jevos-v5), not
        // the alias asked for (d1:3b); nothing validates the echo.
        assert.Equal(t, "jevos-v5", resp.Model)
        assert.Equal(t, Usage{InputTokens: 342, OutputTokens: 0}, resp.Usage)
        assert.JSONEq(t, reply, string(resp.RawBody))

        choice := resp.Answers["sentiment"]
        assert.Equal(t, AnswerChoice, choice.Type)
        assert.Equal(t, "mixed", choice.Choice)
        assert.InDelta(t, 0.45461229955146454, choice.Confidence, 1e-9)
        assert.Equal(t, map[string]float64{
                "positive": 0.006936471196406346,
                "negative": 0.39528015447974413,
                "neutral":  0.00682414966025108,
                "mixed":    0.5909592246635984,
        }, choice.Probabilities)

        noul := resp.Answers["has_churn_number"]
        assert.Equal(t, AnswerNoul, noul.Type)
        assert.InDelta(t, 0.9928918126946713, noul.Noul, 1e-9)
        assert.Zero(t, noul.Confidence, "the native wire sends no confidence for a noul")
        assert.Empty(t, noul.Probabilities)
        // The certainty of a bare noul is its probability: the model put
        // 99.3% on "true".
        assert.InDelta(t, 0.9928918126946713, noul.Certainty(), 1e-9)
}

// TestOllamaNativeDependencyTrap pins the reference script's TEST 2, a
// captured reply from the same live server: numeric-string choice labels,
// both answers carrying confidence and probabilities, and the semantic the
// probe demonstrates — every question is evaluated independently against
// the same static state, so a question whose answer lives in another
// question's output (not in the state) comes back as the model's best
// parallel guess: 0.61 confidence against the 0.96 of the question the
// state actually answers. a1's call sites respect this by construction:
// one state, independent questions, never a chain.
func TestOllamaNativeDependencyTrap(t *testing.T) {
        reply := `{
        "model": "jevos-v5",
        "answers": {
                "delay_days": {
                        "type": "choice",
                        "choice": "14",
                        "probabilities": {
                                "7": 0.01860639621364553,
                                "14": 0.9763682469765433,
                                "21": 0.00502535680981116
                        },
                        "confidence": 0.964552370464815
                },
                "multiplied_by_5": {
                        "type": "choice",
                        "choice": "70",
                        "probabilities": {
                                "35": 0.20456392517387065,
                                "70": 0.7416039021043925,
                                "105": 0.05383217272173676
                        },
                        "confidence": 0.6124058531565888
                }
        },
        "usage": {
                "input_tokens": 392,
                "output_tokens": 0
        }
}`
        srv, got := fakeNativeOllamaServer(t, reply)
        client := NewOllamaClient(OllamaConfig{BaseURL: srv.URL, Model: "d1:3b"})

        resp, err := client.SystemOneWithOptions(t.Context(), "the project was delayed by two weeks.", Questions{
                "delay_days": Choice{
                        Instructions: "what is the delay duration in days?",
                        Criteria:     map[string]any{"7": "one week", "14": "two weeks", "21": "three weeks"},
                },
                "multiplied_by_5": Choice{
                        Instructions: "what is the delay duration multiplied by 5?",
                        Criteria:     map[string]any{"35": "7 * 5", "70": "14 * 5", "105": "21 * 5"},
                },
        }, Options{})
        require.NoError(t, err)

        // The request matches the reference probe key for key — numeric-string
        // alternative names and all.
        assert.Equal(t, "d1:3b", (*got)["model"])
        assert.Equal(t, "the project was delayed by two weeks.", (*got)["state"])
        assert.Equal(t, map[string]any{
                "delay_days": map[string]any{
                        "type":         "choice",
                        "instructions": "what is the delay duration in days?",
                        "criteria":     map[string]any{"7": "one week", "14": "two weeks", "21": "three weeks"},
                },
                "multiplied_by_5": map[string]any{
                        "type":         "choice",
                        "instructions": "what is the delay duration multiplied by 5?",
                        "criteria":     map[string]any{"35": "7 * 5", "70": "14 * 5", "105": "21 * 5"},
                },
        }, (*got)["questions"])

        assert.Equal(t, "jevos-v5", resp.Model)
        assert.Equal(t, Usage{InputTokens: 392, OutputTokens: 0}, resp.Usage)

        delay := resp.Answers["delay_days"]
        assert.Equal(t, AnswerChoice, delay.Type)
        assert.Equal(t, "14", delay.Choice)
        assert.InDelta(t, 0.964552370464815, delay.Confidence, 1e-9)

        multiplied := resp.Answers["multiplied_by_5"]
        assert.Equal(t, AnswerChoice, multiplied.Type)
        assert.Equal(t, "70", multiplied.Choice)
        assert.InDelta(t, 0.6124058531565888, multiplied.Confidence, 1e-9)
        assert.Equal(t, map[string]float64{
                "35":  0.20456392517387065,
                "70":  0.7416039021043925,
                "105": 0.05383217272173676,
        }, multiplied.Probabilities)
}

// TestOllamaNative404FallsBackToChat: a 404 from /v1/systemone means the
// server predates the endpoint (Ollama < 0.35); the chat emulation answers
// instead, so an old setup keeps working.
func TestOllamaNative404FallsBackToChat(t *testing.T) {
        var native, chat int
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                switch r.URL.Path {
                case systemOnePath:
                        native++
                        http.NotFound(w, r)
                case "/api/chat":
                        chat++
                        w.Header().Set("Content-Type", "application/x-ndjson")
                        _ = json.NewEncoder(w).Encode(map[string]any{
                                "model":             "qwen3:4b",
                                "message":           map[string]any{"role": "assistant", "content": `{"engine": {"choice": "mdn", "confidence": 0.9}}`},
                                "done":              true,
                                "prompt_eval_count": 41,
                                "eval_count":        7,
                        })
                default:
                        t.Errorf("unexpected path %q", r.URL.Path)
                }
        }))
        t.Cleanup(srv.Close)
        client := NewOllamaClient(OllamaConfig{BaseURL: srv.URL, Model: "qwen3:4b"})

        resp, err := client.SystemOneWithOptions(t.Context(), "find docs", Questions{
                "engine": Choice{Criteria: map[string]any{"mdn": "web docs", "github": "repos"}},
        }, Options{})
        require.NoError(t, err)
        assert.Equal(t, "mdn", resp.Answers["engine"].Choice)
        assert.Equal(t, "qwen3:4b", resp.Model, "the chat wire answered")
        assert.Equal(t, 1, native, "the native endpoint was probed once")
        assert.Equal(t, 1, chat, "the chat wire was used exactly once")
}

// TestOllamaNativeErrorDoesNotFallBack: a non-404 failure from the native
// endpoint is an error, not a fallback — the endpoint exists and is broken,
// and a chat retry would mask that.
func TestOllamaNativeErrorDoesNotFallBack(t *testing.T) {
        var chat int
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path == "/api/chat" {
                        chat++
                }
                w.WriteHeader(http.StatusBadRequest)
                _, _ = w.Write([]byte(`{"error": "unsupported question type"}`))
        }))
        t.Cleanup(srv.Close)
        client := NewOllamaClient(OllamaConfig{BaseURL: srv.URL, Model: "d1:3b"})

        _, err := client.SystemOneWithOptions(t.Context(), "q", Questions{
                "engine": Choice{Criteria: map[string]any{"mdn": "docs"}},
        }, Options{})
        require.Error(t, err)
        assert.Contains(t, err.Error(), "ollama systemone API error (400)")
        assert.Contains(t, err.Error(), "unsupported question type")
        assert.Zero(t, chat, "a native error must not retry over the chat wire")
}

// TestOllamaNativeContractViolations: the native wire enforces the same
// spine — every question answered, choices from the criteria, confidence
// present, answer types agreeing with the questions.
func TestOllamaNativeContractViolations(t *testing.T) {
        questions := Questions{
                "sentiment":        Choice{Criteria: map[string]any{"positive": "good", "negative": "bad"}},
                "has_churn_number": Noul{Criteria: &NoulCriteria{True: "mentioned", False: "absent"}},
                "outcome":          Score{Criteria: []any{"failed", "unclear", "passed"}},
        }
        cases := []struct {
                name   string
                reply  string
                errHas string
        }{
                {
                        name:   "unanswered question",
                        reply:  `{"model": "d1:3b", "answers": {"sentiment": {"type": "choice", "choice": "negative", "confidence": 0.9}}}`,
                        errHas: `left question "has_churn_number" unanswered`,
                },
                {
                        name:   "choice outside the criteria",
                        reply:  `{"model": "d1:3b", "answers": {"sentiment": {"type": "choice", "choice": "angry", "confidence": 0.9}, "has_churn_number": {"type": "noul", "noul": 0.5}, "outcome": {"type": "score", "score": 2, "confidence": 0.9}}}`,
                        errHas: "not one of the named alternatives",
                },
                {
                        name:   "missing confidence",
                        reply:  `{"model": "d1:3b", "answers": {"sentiment": {"type": "choice", "choice": "negative"}, "has_churn_number": {"type": "noul", "noul": 0.5}, "outcome": {"type": "score", "score": 2, "confidence": 0.9}}}`,
                        errHas: `has no "confidence"`,
                },
                {
                        name:   "answer type contradicts the question",
                        reply:  `{"model": "d1:3b", "answers": {"sentiment": {"type": "noul", "noul": 0.5}, "has_churn_number": {"type": "noul", "noul": 0.5}, "outcome": {"type": "score", "score": 2, "confidence": 0.9}}}`,
                        errHas: `has type "noul", want "choice"`,
                },
                {
                        name:   "score outside the rubric",
                        reply:  `{"model": "d1:3b", "answers": {"sentiment": {"type": "choice", "choice": "negative", "confidence": 0.9}, "has_churn_number": {"type": "noul", "noul": 0.5}, "outcome": {"type": "score", "score": 3, "confidence": 0.9}}}`,
                        errHas: "outside the rubric range 0..2",
                },
                {
                        name:   "reply is not JSON",
                        reply:  `the answer is mixed`,
                        errHas: "not valid JSON",
                },
        }
        for _, tc := range cases {
                t.Run(tc.name, func(t *testing.T) {
                        srv, _ := fakeNativeOllamaServer(t, tc.reply)
                        client := NewOllamaClient(OllamaConfig{BaseURL: srv.URL, Model: "d1:3b"})
                        _, err := client.SystemOneWithOptions(t.Context(), "q", questions, Options{})
                        require.Error(t, err)
                        assert.Contains(t, err.Error(), tc.errHas)
                })
        }
}

// TestOllamaNativeFractionalScoreAccepted: an interpolated rubric position
// is a valid verdict (the hosted wire pins fractional scores); legend and
// probabilities ride along when sent, and their absence is not an error.
func TestOllamaNativeFractionalScoreAccepted(t *testing.T) {
        reply := `{
        "model": "d1:3b",
        "answers": {
                "outcome": {"type": "score", "score": 1.5, "confidence": 0.8,
                        "legend": {"0": "failed", "1": "unclear", "2": "passed"},
                        "probabilities": {"0": 0.1, "1": 0.6, "2": 0.3}},
                "severity": {"type": "score", "score": 0, "confidence": 0.7}
        }
}`
        srv, _ := fakeNativeOllamaServer(t, reply)
        client := NewOllamaClient(OllamaConfig{BaseURL: srv.URL, Model: "d1:3b"})

        resp, err := client.SystemOneWithOptions(t.Context(), "q", Questions{
                "outcome":  Score{Criteria: []any{"failed", "unclear", "passed"}},
                "severity": Score{Criteria: []any{"low", "high"}},
        }, Options{})
        require.NoError(t, err)

        outcome := resp.Answers["outcome"]
        assert.InDelta(t, 1.5, outcome.Score, 1e-9)
        assert.InDelta(t, 0.8, outcome.Confidence, 1e-9)
        assert.Equal(t, "unclear", outcome.Legend["1"])
        assert.Equal(t, map[string]float64{"0": 0.1, "1": 0.6, "2": 0.3}, outcome.Probabilities)

        severity := resp.Answers["severity"]
        assert.InDelta(t, 0, severity.Score, 1e-9)
        assert.Empty(t, severity.Legend, "legend is optional on the native wire")
        assert.Empty(t, severity.Probabilities, "probabilities are optional on the native wire")
}

// TestOllamaNativeModelOverride: Options.Model wins on the native wire too
// — the same override semantics every wire honors.
func TestOllamaNativeModelOverride(t *testing.T) {
        srv, got := fakeNativeOllamaServer(t,
                `{"model": "nimble", "answers": {"engine": {"type": "choice", "choice": "mdn", "confidence": 0.9}}}`)
        client := NewOllamaClient(OllamaConfig{BaseURL: srv.URL, Model: "d1:3b"})

        _, err := client.SystemOneWithOptions(t.Context(), "q", Questions{
                "engine": Choice{Criteria: map[string]any{"mdn": "docs"}},
        }, Options{Model: "nimble"})
        require.NoError(t, err)
        assert.Equal(t, "nimble", (*got)["model"])
}
