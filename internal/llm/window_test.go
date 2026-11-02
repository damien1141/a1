package llm

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "net/http"
        "net/http/httptest"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

func TestParseContextLimit(t *testing.T) {
        tests := []struct {
                name string
                err  error
                want int
                ok   bool
        }{
                {name: "nil", err: nil, want: 0, ok: false},
                {name: "no numbers", err: errors.New("request exceeds the available context size"), want: 0, ok: false},
                {
                        name: "openai maximum context length",
                        err:  errors.New("This model's maximum context length is 16384 tokens. However, you requested 17000 tokens"),
                        want: 16384, ok: true,
                },
                {
                        name: "comma grouped",
                        err:  errors.New("this model's maximum context length is 1,048,576 tokens"),
                        want: 1048576, ok: true,
                },
                {
                        name: "anthropic prompt too long",
                        err:  errors.New("prompt is too long: 210000 > 200000 tokens"),
                        want: 200000, ok: true,
                },
                {
                        name: "gemini allowed",
                        err:  errors.New("The input token count (1196265) exceeds the maximum number of tokens allowed (1048575)"),
                        want: 1048575, ok: true,
                },
                {
                        name: "exceeds the limit of",
                        err:  errors.New("input length exceeds the limit of 8192"),
                        want: 8192, ok: true,
                },
                {
                        name: "model with N maximum context length",
                        err:  errors.New("prompt is too large for model with 131072 maximum context length"),
                        want: 131072, ok: true,
                },
                {
                        name: "request-size capture rejected as too small",
                        err:  errors.New("greater than the context length (100)"),
                        want: 0, ok: false,
                },
        }
        for _, tt := range tests {
                t.Run(tt.name, func(t *testing.T) {
                        got, ok := ParseContextLimit(tt.err)
                        assert.Equal(t, tt.ok, ok)
                        assert.Equal(t, tt.want, got)
                })
        }
}

func TestProbeContextWindowLlamaProps(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/props" {
                        http.NotFound(w, r)
                        return
                }
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{
                        "total_slots": 1,
                        "default_generation_settings": {"id": 0, "n_ctx": 65536, "is_processing": false},
                        "model": {"vocab_size": 128256}
                }`))
        }))
        defer server.Close()

        got := ProbeContextWindow(t.Context(), server.Client(), server.URL, "qwen3-coder")
        assert.Equal(t, 65536, got)
}

func TestProbeContextWindowOllamaShow(t *testing.T) {
        var gotModel string
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                switch r.URL.Path {
                case "/props":
                        http.NotFound(w, r)
                case "/api/show":
                        var body struct {
                                Model string `json:"model"`
                        }
                        if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
                                http.Error(w, err.Error(), http.StatusBadRequest)
                                return
                        }
                        gotModel = body.Model
                        w.Header().Set("Content-Type", "application/json")
                        _, _ = w.Write([]byte(`{
                                "model_info": {"qwen3.context_length": 131072, "general.architecture": "qwen3"},
                                "capabilities": []
                        }`))
                default:
                        http.NotFound(w, r)
                }
        }))
        defer server.Close()

        got := ProbeContextWindow(t.Context(), server.Client(), server.URL, "qwen3:32b")
        assert.Equal(t, 131072, got)
        assert.Equal(t, "qwen3:32b", gotModel)
}

func TestProbeContextWindowRefusesPublicHosts(t *testing.T) {
        calls := 0
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
                calls++
                http.NotFound(w, nil)
        }))
        defer server.Close()

        // The httptest server is loopback, but the URL we pass claims a public
        // host — the probe must refuse before any request is made.
        got := ProbeContextWindow(t.Context(), server.Client(), "https://api.example.com/v1", "m")
        assert.Equal(t, 0, got)
        assert.Equal(t, 0, calls)
}

func TestProbeContextWindowNothingFound(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"ok": true}`))
        }))
        defer server.Close()

        got := ProbeContextWindow(t.Context(), server.Client(), server.URL, "m")
        assert.Equal(t, 0, got)
}

func TestProbeContextWindowDeadServer(t *testing.T) {
        server := httptest.NewServer(http.NotFoundHandler())
        url := server.URL
        server.Close() // port now closed; every request fails fast

        got := ProbeContextWindow(context.Background(), http.DefaultClient, url, "m")
        assert.Equal(t, 0, got)
}

func TestParseContextLimitViaFormatAPIError(t *testing.T) {
        err := FormatAPIError(
                "LLM",
                400,
                []byte(`{"error":{"message":"This model's maximum context length is 65536 tokens. However, you requested 70000 tokens"}}`),
        )
        limit, ok := ParseContextLimit(err)
        require.True(t, ok)
        assert.Equal(t, 65536, limit)

        // Wrapped errors still parse — the engine wraps stream errors.
        wrapped := fmt.Errorf("stream: %w", err)
        limit, ok = ParseContextLimit(wrapped)
        require.True(t, ok)
        assert.Equal(t, 65536, limit)
}
