package agent

import (
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/http/httptest"
        "path/filepath"
        "sync"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/tools"
)

// engine_cache_stability_openai_test.go — the OpenAI-wire half of the
// prompt-cache contract. engine_cache_stability_test.go pins the seven
// contracts against the Anthropic wire; this file pins the ones that keep
// an OpenAI-compatible session on the prefix-caching read path — which is
// not just a billing matter: a local server (llama.cpp, Ollama, LM Studio)
// derives its prompt cache from the byte-identical prefix, so a single
// unstable byte between turns turns a sub-second incremental prefill into
// a full 15K-token re-prefill at hardware speed.
//
//   - the folded system message (client system + every RoleSystem history
//     message, persona block included) is byte-identical across every
//     request — no clock, no per-turn churn;
//   - the persona block rides that system message in EVERY request — the
//     old flag-gated injection vanished after the first cache bust;
//   - history is append-only: each request's messages extend the previous
//     request's messages element for element;
//   - the tools array is byte-identical across requests.

// capturedOpenAIRequest is one chat-completions body as the server saw it.
type capturedOpenAIRequest struct {
        Messages []json.RawMessage `json:"messages"`
        Tools    json.RawMessage   `json:"tools"`
        Model    string            `json:"model"`
}

// openAISystemContent extracts messages[0].content when it is the folded
// system message.
func openAISystemContent(t *testing.T, raw json.RawMessage) string {
        t.Helper()
        var m struct {
                Role    string          `json:"role"`
                Content json.RawMessage `json:"content"`
        }
        require.NoError(t, json.Unmarshal(raw, &m))
        require.Equal(t, "system", m.Role, "the folded system message must be messages[0] on the OpenAI wire")
        var s string
        require.NoError(t, json.Unmarshal(m.Content, &s))
        return s
}

func TestOpenAIRequestsAreCacheStable(t *testing.T) {
        var mu sync.Mutex
        var bodies []capturedOpenAIRequest

        n := 0
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                data, _ := io.ReadAll(r.Body)
                var req capturedOpenAIRequest
                require.NoError(t, json.Unmarshal(data, &req))
                mu.Lock()
                bodies = append(bodies, req)
                n++
                round := n
                mu.Unlock()

                w.Header().Set("Content-Type", "text/event-stream")
                switch round {
                case 1: // turn 1, round 1: call the tool
                        _, _ = fmt.Fprint(w, sseToolCallChunk("call_1", "bash", `{}`)+"data: [DONE]\n\n")
                case 2: // turn 1, round 2: finish with text
                        _, _ = fmt.Fprint(w, sseTextChunk("done one")+"data: [DONE]\n\n")
                case 3: // turn 2, round 1: plain answer
                        _, _ = fmt.Fprint(w, sseTextChunk("done two")+"data: [DONE]\n\n")
                default:
                        _, _ = fmt.Fprint(w, sseTextChunk("unexpected")+"data: [DONE]\n\n")
                }
        }))
        defer server.Close()

        // A non-default persona exercises the frozen-block injection on the
        // wire: its bytes must ride the folded system message in every request.
        root := t.TempDir()
        pstore := persona.NewStore(filepath.Join(root, "personas"))
        _, err := pstore.CreateInput(persona.Input{
                Name:   "The Closer",
                Prompt: "Lead with the outcome and the number that proves it.",
        })
        require.NoError(t, err)

        sess, err := NewSession(WithPersonaStore(pstore), WithCwd(t.TempDir()))
        require.NoError(t, err)
        engine, err := NewEngine(
                llm.ModelConfig{
                        Name:          "gpt-fake",
                        API:           llm.OpenAI,
                        BaseURL:       server.URL,
                        APIKey:        "x",
                        ContextWindow: 200000,
                },
                sess,
                WithGate(permission.AllowAll{}),
                WithTools([]tools.Tool{stubBashTool(func() string { return "tool output" })}),
                WithConsolidationInterval(1),
        )
        require.NoError(t, err)

        require.NoError(t, drainLoop(t, engine, "first prompt"))
        require.NoError(t, drainLoop(t, engine, "second prompt"))

        mu.Lock()
        defer mu.Unlock()
        require.Len(t, bodies, 3, "turn 1 ran two rounds (tool + finish), turn 2 one")

        // Contract 1 — one folded system message, byte-identical everywhere.
        systems := make([]string, len(bodies))
        for i := range bodies {
                require.NotEmpty(t, bodies[i].Messages, "request %d carries messages", i+1)
                systems[i] = openAISystemContent(t, bodies[i].Messages[0])
        }
        for i := 1; i < len(systems); i++ {
                assert.Equal(t, systems[0], systems[i],
                        "request %d: the folded system message must be byte-identical to request 1 — one unstable byte re-prefills the whole context on a local server", i+1)
        }
        assert.NotContains(t, systems[0], "Current date and time",
                "the clock must never ride the system message")

        // Contract 2 — the persona block rides the system message in EVERY
        // request. The old flag-gated injection vanished after the first
        // cache bust (turn 2 rebuilt without it), silently dropping the
        // personality and reshaping the prefix.
        for i, s := range systems {
                assert.Contains(t, s, "Lead with the outcome and the number that proves it.",
                        "request %d: the frozen persona block must ride the folded system message", i+1)
        }

        // Contract 3 — history is append-only: request i+1's messages begin
        // with request i's messages, element for element.
        for i := 1; i < len(bodies); i++ {
                prev, cur := bodies[i-1].Messages, bodies[i].Messages
                require.GreaterOrEqual(t, len(cur), len(prev),
                        "request %d must extend request %d, not shrink it", i+1, i)
                for j := range prev {
                        assert.JSONEq(t, string(prev[j]), string(cur[j]),
                                "request %d element %d differs from request %d — history must be append-only on the wire", i+1, j, i)
                }
        }

        // Contract 4 — the tools array is byte-identical across requests.
        for i := 1; i < len(bodies); i++ {
                if assert.NotEmpty(t, bodies[i].Tools, "request %d carries tools", i+1) {
                        assert.JSONEq(t, string(bodies[0].Tools), string(bodies[i].Tools),
                                "request %d: the tools array must be byte-identical to request 1", i+1)
                }
        }
}
