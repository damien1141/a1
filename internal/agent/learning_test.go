package agent

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "sync"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/memstore"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/tools"
        "github.com/damien1141/a1/internal/tools/readtool"
        "github.com/damien1141/a1/internal/tools/searchtool"
        "github.com/damien1141/a1/internal/tools/writetool"
)

// --- the learning loop (hermes steal) --------------------------------------
//
// Counters tick on opportunity and reset on actual use; an elapsed interval
// ends the turn with a background review fork that is cache-identical to the
// parent (same tools[] bytes, same transcript prefix) but permission-restricted
// at dispatch. These tests drive the real Loop against a scripted SSE server —
// the fork's requests hit the same server, so the scripts below interleave
// main-turn rounds and fork rounds in one sequence.

// learnCapMsg captures what the learning assertions need from one request
// message: its role, text, and any tool calls it carries.
type learnCapMsg struct {
        Role       string `json:"role"`
        Content    string `json:"content"`
        ToolCalls  []struct {
                ID       string `json:"id"`
                Function struct {
                        Name      string `json:"name"`
                        Arguments string `json:"arguments"`
                } `json:"function"`
        } `json:"tool_calls"`
}

type learnCapturedRequest struct {
        Stream   bool           `json:"stream"`
        Messages []learnCapMsg  `json:"messages"`
        Tools    []map[string]any `json:"tools"`
}

// learnLLMServer scripts the streaming rounds (shared counter across main
// turns and fork rounds, exactly like memoryLLMServer) and answers the memory
// outlet's non-streaming Compact calls with an empty ops batch. Every request
// body is captured with its full message list and advertised tools.
func learnLLMServer(rounds []scriptedRound) (*httptest.Server, func() []learnCapturedRequest) {
        var mu sync.Mutex
        var captured []learnCapturedRequest
        streamN := 0
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                body, _ := io.ReadAll(r.Body)
                var streamOnly struct {
                        Stream bool `json:"stream"`
                }
                _ = json.Unmarshal(body, &streamOnly)
                var cap learnCapturedRequest
                _ = json.Unmarshal(body, &cap)
                mu.Lock()
                captured = append(captured, cap)
                mu.Unlock()

                if !streamOnly.Stream {
                        // The memory outlet's consolidation call: reply with zero ops.
                        // It consumes NO scripted round — only streaming requests draw
                        // from the sequence, so an outlet goroutine racing the fork can
                        // never eat a scripted round out from under it.
                        w.Header().Set("Content-Type", "application/json")
                        _ = json.NewEncoder(w).Encode(map[string]any{
                                "choices": []map[string]any{{
                                        "message":       map[string]string{"role": "assistant", "content": `{"ops":[]}`},
                                        "finish_reason": "stop",
                                }},
                        })
                        return
                }
                mu.Lock()
                streamN++
                n := streamN
                mu.Unlock()
                w.Header().Set("Content-Type", "text/event-stream")
                var out string
                if n <= len(rounds) {
                        round := rounds[n-1]
                        if round.Tool != "" {
                                out = sseToolCallChunk(fmt.Sprintf("call_%d", n), round.Tool, round.Args)
                        } else {
                                out = sseTextChunk(round.Text)
                        }
                } else {
                        out = sseTextChunk("done")
                }
                _, _ = fmt.Fprint(w, out)
                _, _ = fmt.Fprint(w, "data: [DONE]\n\n")
        }))
        return srv, func() []learnCapturedRequest {
                mu.Lock()
                defer mu.Unlock()
                return append([]learnCapturedRequest(nil), captured...)
        }
}

// learningTools builds a minimal real tool surface: the real read/write/search
// (the fork's whitelist names them) plus any extras (a bash stub for deny
// tests). Contract/budget ride along via buildCoreTools as always.
func learningTools(extra ...tools.Tool) []tools.Tool {
        var out []tools.Tool
        for _, t := range tools.DefaultTools() {
                switch t.Definition.Name {
                case "read", "write", "search":
                        out = append(out, t)
                }
        }
        return append(out, extra...)
}

// reviewRequests filters captured requests down to the review fork's calls:
// streaming requests whose final user message carries the review instruction.
func reviewRequests(reqs []learnCapturedRequest) []learnCapturedRequest {
        var out []learnCapturedRequest
        for _, rq := range reqs {
                if !rq.Stream || len(rq.Messages) == 0 {
                        continue
                }
                last := rq.Messages[len(rq.Messages)-1]
                if last.Role == "user" && contains(last.Content, "Review the conversation above") {
                        out = append(out, rq)
                }
        }
        return out
}

func contains(s, sub string) bool {
        return len(s) >= len(sub) && (func() bool {
                for i := 0; i+len(sub) <= len(s); i++ {
                        if s[i:i+len(sub)] == sub {
                                return true
                        }
                }
                return false
        })()
}

// openLearningStore builds a bare semantic store for engines without a persona
// store (memStoreForActivePersona falls back to engine.memStore).
func openLearningStore(t *testing.T) *memstore.Store {
        t.Helper()
        store, err := memstore.Open(filepath.Join(t.TempDir(), "bank.sqlite"), memstore.FakeEmbedder{DimCount: 64}, memstore.DefaultParams())
        require.NoError(t, err)
        return store
}

// TestLearningCounterSemantics pins the nudge counters themselves, with no LLM
// in sight: ticks advance, the fire resets, actual use resets, and the
// substrate gates keep inert engines silent.
func TestLearningCounterSemantics(t *testing.T) {
        store := openLearningStore(t)
        skills := t.TempDir()
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "m", SkillPath: skills},
                sess,
                WithMemoryStore(store),
                WithMemoryNudgeInterval(3),
                WithSkillNudgeInterval(3),
        )
        require.NoError(t, err)

        // Memory: two ticks stay quiet, the third fires and resets.
        assert.False(t, eng.tickMemoryNudge())
        assert.False(t, eng.tickMemoryNudge())
        assert.True(t, eng.tickMemoryNudge())

        // Actual use resets: tick twice after the fire, then a remember dispatch
        // zeroes the window (without the reset the next tick would fire again).
        assert.False(t, eng.tickMemoryNudge())
        assert.False(t, eng.tickMemoryNudge())
        eng.learningSink("remember", json.RawMessage(`{"content":"x","kind":"fact","scope":"user"}`))
        assert.False(t, eng.tickMemoryNudge(), "the remember dispatch must have reset the turn counter")

        // Skill iterations: three rounds fire at the turn-end check.
        eng.tickSkillIteration()
        eng.tickSkillIteration()
        assert.False(t, eng.checkSkillNudge())
        eng.tickSkillIteration()
        assert.True(t, eng.checkSkillNudge())
        assert.False(t, eng.checkSkillNudge(), "fire must reset the iteration counter")

        // A SKILL.md read resets mid-window.
        eng.tickSkillIteration()
        skillFile := filepath.Join(skills, "alpha", "SKILL.md")
        eng.learningSink("read", json.RawMessage(fmt.Sprintf(`{"path":%q}`, skillFile)))
        assert.False(t, eng.checkSkillNudge(), "the SKILL.md read must have reset the iteration counter")

        // Substrate gates: memory off silences the memory tick even at interval.
        eng.SetMemoryEnabled(false)
        assert.False(t, eng.tickMemoryNudge())
        eng.SetMemoryEnabled(true)
}

// TestLearningMemoryNudgeFiresCacheParityFork is the flagship wiring proof:
// two quiet turns arm the memory counter; the second turn ends with a review
// fork whose request is byte-identical to the parent's prefix (same system
// messages, same history, same advertised tools[]) plus exactly one trailing
// review message — the cache-parity contract.
func TestLearningMemoryNudgeFiresCacheParityFork(t *testing.T) {
        srv, requests := learnLLMServer([]scriptedRound{
                {Text: "first answer"},
                {Text: "second answer"},
                {Text: "Nothing to save."},
        })
        defer srv.Close()

        store := openLearningStore(t)
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools(learningTools()),
                WithMemoryStore(store),
                WithMemoryNudgeInterval(2),
        )
        require.NoError(t, err)

        require.NoError(t, drainLoop(t, eng, "remember me: I like short answers"))
        require.NoError(t, drainLoop(t, eng, "another turn without memory use"))

        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        require.NoError(t, eng.WaitForOutstandingOutlets(ctx))

        reqs := requests()
        streamReqs := 0
        for _, rq := range reqs {
                if rq.Stream {
                        streamReqs++
                }
        }
        require.Equal(t, 3, streamReqs, "two main turns + one fork round")

        reviews := reviewRequests(reqs)
        require.Len(t, reviews, 1, "exactly one review fork")
        fork := reviews[0]

        // The prompt scopes to memory only (no skills directory configured).
        last := fork.Messages[len(fork.Messages)-1]
        assert.Equal(t, "user", last.Role)
        assert.Contains(t, last.Content, "**Memory**")
        assert.NotContains(t, last.Content, "**Skills**")
        assert.Contains(t, last.Content, "harden into refusals", "the do-not-capture list must ride the prompt")

        // Cache parity: the fork's advertised tools are the parent's, byte for
        // byte, and its message prefix is the parent's last request exactly.
        var mainReqs []learnCapturedRequest
        for _, rq := range reqs {
                if rq.Stream && len(rq.Messages) > 0 {
                        lastMain := rq.Messages[len(rq.Messages)-1]
                        if lastMain.Role == "user" && !contains(lastMain.Content, "Review the conversation above") {
                                mainReqs = append(mainReqs, rq)
                        }
                }
        }
        require.Len(t, mainReqs, 2)
        assert.Equal(t, mainReqs[1].Tools, fork.Tools, "the fork must advertise the parent's exact tools[]")
        require.Greater(t, len(fork.Messages), len(mainReqs[1].Messages))
        assert.Equal(t, mainReqs[1].Messages, fork.Messages[:len(mainReqs[1].Messages)],
                "the fork's prefix must equal the parent's last request message-for-message")
        assert.Equal(t, 2, len(fork.Messages)-len(mainReqs[1].Messages),
                "beyond the parent's request: the final assistant row + the review message")
}

// TestLearningMemoryUseResetsNudge: a turn whose model actually calls the
// memory tool must not burn the window — the dispatch resets the counter, so
// the next interval counts from that use.
func TestLearningMemoryUseResetsNudge(t *testing.T) {
        srv, requests := learnLLMServer([]scriptedRound{
                {Tool: "remember", Args: `{"content":"the user likes short answers","kind":"preference","scope":"user"}`},
                {Text: "saved it"},
                {Text: "second turn"},
                {Text: "unused"},
        })
        defer srv.Close()

        store := openLearningStore(t)
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools(learningTools()),
                WithMemoryStore(store),
                WithMemoryNudgeInterval(2),
        )
        require.NoError(t, err)

        require.NoError(t, drainLoop(t, eng, "please remember I like short answers"))
        require.NoError(t, drainLoop(t, eng, "a second turn"))

        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        require.NoError(t, eng.WaitForOutstandingOutlets(ctx))

        // Turn 1 ticked the counter to 1, then the remember dispatch reset it.
        // Turn 2 ticked it to 1 again — below the interval of 2. No fork.
        time.Sleep(150 * time.Millisecond)
        assert.Empty(t, reviewRequests(requests()), "actual memory use must reset the nudge window")
}

// TestLearningSkillIterationsFireReview: the skill counter ticks once per
// model round, so a single tool-heavy turn can cross the interval on its own.
func TestLearningSkillIterationsFireReview(t *testing.T) {
        skills := t.TempDir()
        require.NoError(t, os.MkdirAll(filepath.Join(skills, "alpha"), 0o755))
        require.NoError(t, os.WriteFile(filepath.Join(skills, "alpha", "SKILL.md"), []byte("---\nname: alpha\ndescription: test skill\n---\nbody\n"), 0o644))

        srv, requests := learnLLMServer([]scriptedRound{
                {Tool: "bash", Args: `{"command":"ls"}`},
                {Tool: "bash", Args: `{"command":"ls -la"}`},
                {Text: "wrap-up"},
                {Text: "Nothing to save."},
        })
        defer srv.Close()

        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x", SkillPath: skills},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools(learningTools(stubBashTool(func() string { return "ok" }))),
                WithSkillNudgeInterval(3),
        )
        require.NoError(t, err)

        require.NoError(t, drainLoop(t, eng, "check the directory twice"))
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        require.NoError(t, eng.WaitForOutstandingOutlets(ctx))

        reviews := reviewRequests(requests())
        require.Len(t, reviews, 1, "three model rounds must fire the skill review")
        last := reviews[0].Messages[len(reviews[0].Messages)-1]
        assert.Contains(t, last.Content, "**Skills**")
        assert.Contains(t, last.Content, skills, "the prompt must name the skills root")
        assert.NotContains(t, last.Content, "**Memory**", "no memory store configured: memory section must be absent")
}

// TestLearningSkillReadResetsIterations: consulting the library (reading a
// SKILL.md) is skill use — the iteration counter resets, so a two-round turn
// that read a skill stays under an interval of 2.
func TestLearningSkillReadResetsIterations(t *testing.T) {
        skills := t.TempDir()
        skillFile := filepath.Join(skills, "alpha", "SKILL.md")
        require.NoError(t, os.MkdirAll(filepath.Join(skills, "alpha"), 0o755))
        require.NoError(t, os.WriteFile(skillFile, []byte("---\nname: alpha\ndescription: test skill\n---\nbody\n"), 0o644))

        srv, requests := learnLLMServer([]scriptedRound{
                {Tool: "read", Args: fmt.Sprintf(`{"path":%q}`, skillFile)},
                {Text: "consulted the skill, done"},
                {Text: "second turn answer"},
                {Text: "Nothing to save."},
        })
        defer srv.Close()

        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x", SkillPath: skills},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools(learningTools()),
                WithSkillNudgeInterval(2),
        )
        require.NoError(t, err)

        require.NoError(t, drainLoop(t, eng, "use the alpha skill"))
        // Turn 1 ticked once, the read reset it, the wrap-up ticked once: 1 < 2.
        time.Sleep(150 * time.Millisecond)
        assert.Empty(t, reviewRequests(requests()), "a consulted skill must reset the iteration window")

        require.NoError(t, drainLoop(t, eng, "a quiet follow-up"))
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        require.NoError(t, eng.WaitForOutstandingOutlets(ctx))
        // Turn 2 ticked from 1 to 2: the review fires now.
        assert.Len(t, reviewRequests(requests()), 1)
}

// TestLearningForkWhitelistDeniesAndRedirects: a non-whitelisted tool call in
// the fork is denied at dispatch — never executed — and the denial the model
// reads names the substitutes so one round redirects it.
func TestLearningForkWhitelistDeniesAndRedirects(t *testing.T) {
        srv, requests := learnLLMServer([]scriptedRound{
                {Text: "turn answer"},
                {Tool: "bash", Args: `{"command":"rm -rf /"}`},
                {Text: "understood, nothing to save"},
        })
        defer srv.Close()

        var bashRuns int
        var mu sync.Mutex
        bash := tools.Tool{
                Definition: llm.ToolDefinition{Name: "bash", Params: &llm.FunctionParameters{Type: "object"}},
                Run: func(context.Context, json.RawMessage) (tools.Result, error) {
                        mu.Lock()
                        bashRuns++
                        mu.Unlock()
                        return tools.Result{Content: "ran"}, nil
                },
        }

        store := openLearningStore(t)
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools(learningTools(bash)),
                WithMemoryStore(store),
                WithMemoryNudgeInterval(1),
        )
        require.NoError(t, err)

        require.NoError(t, drainLoop(t, eng, "one turn is enough at interval 1"))
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        require.NoError(t, eng.WaitForOutstandingOutlets(ctx))

        mu.Lock()
        assert.Zero(t, bashRuns, "the denied bash call must never execute")
        mu.Unlock()

        // TEMPORARY DEBUG: dump captured requests.
        for i, rq := range requests() {
                var roles []string
                for _, m := range rq.Messages {
                        roles = append(roles, m.Role)
                }
                t.Logf("req %d: stream=%v msgs=%d roles=%v", i, rq.Stream, len(rq.Messages), roles)
                for j, m := range rq.Messages {
                        if len(m.Content) > 0 || len(m.ToolCalls) > 0 {
                                c := m.Content
                                if len(c) > 100 {
                                        c = c[:100]
                                }
                                t.Logf("  msg %d: role=%s content=%q toolcalls=%d", j, m.Role, c, len(m.ToolCalls))
                        }
                }
        }

        // The round after the denial carries the tool result the model read:
        // the redirect message, not a bare refusal.
        reqs := requests()
        var denySeen string
        for _, rq := range reqs {
                for _, msg := range rq.Messages {
                        if msg.Role == "tool" && contains(msg.Content, "background review can only use learning tools") {
                                denySeen = msg.Content
                        }
                }
        }
        require.NotEmpty(t, denySeen, "the denial must ride the tool result path")
        assert.Contains(t, denySeen, "read/search", "the denial must name the substitutes")
        assert.Contains(t, denySeen, "Do not retry 'bash'")
}

// TestLearningForkWriteScopedAndReadBeforeWrite: the fork's write tool is
// confined to the skills directory, and an existing file must be read inside
// the review before it may be overwritten.
func TestLearningForkWriteScopedAndReadBeforeWrite(t *testing.T) {
        cwd := t.TempDir()
        skills := filepath.Join(cwd, "skills")
        skillFile := filepath.Join(skills, "alpha", "SKILL.md")
        require.NoError(t, os.MkdirAll(filepath.Join(skills, "alpha"), 0o755))
        require.NoError(t, os.WriteFile(skillFile, []byte("---\nname: alpha\ndescription: test skill\n---\nold body\n"), 0o644))

        srv, requests := learnLLMServer([]scriptedRound{
                {Text: "turn answer"},
                {Tool: "write", Args: `{"path":"outside.txt","content":"must not land"}`},
                {Tool: "write", Args: fmt.Sprintf(`{"path":%q,"content":"---\nname: alpha\ndescription: test skill\n---\nnew body\n"}`, skillFile)},
                {Tool: "read", Args: fmt.Sprintf(`{"path":%q}`, skillFile)},
                {Tool: "write", Args: fmt.Sprintf(`{"path":%q,"content":"---\nname: alpha\ndescription: test skill\n---\nnew body\n"}`, skillFile)},
                {Text: "skill updated"},
        })
        defer srv.Close()

        sess, err := NewSession(WithCwd(cwd))
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x", SkillPath: skills},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools(learningTools()),
                WithSkillNudgeInterval(1),
        )
        require.NoError(t, err)

        require.NoError(t, drainLoop(t, eng, "learn from this"))
        ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
        defer cancel()
        require.NoError(t, eng.WaitForOutstandingOutlets(ctx))

        // Scope: the outside write never landed.
        _, statErr := os.Stat(filepath.Join(cwd, "outside.txt"))
        assert.True(t, os.IsNotExist(statErr), "a fork write outside the skills directory must not execute")

        // Read-before-write: the blind overwrite was refused, the post-read
        // write landed.
        got, err := os.ReadFile(skillFile)
        require.NoError(t, err)
        assert.Contains(t, string(got), "new body", "the read-then-write sequence must succeed")

        var scopeRefusal, readFirstRefusal string
        for _, rq := range requests() {
                for _, msg := range rq.Messages {
                        if msg.Role != "tool" {
                                continue
                        }
                        if contains(msg.Content, "may only write inside the skills directory") {
                                scopeRefusal = msg.Content
                        }
                        if contains(msg.Content, "read-before-write") {
                                readFirstRefusal = msg.Content
                        }
                }
        }
        assert.Contains(t, scopeRefusal, "skills directory", "the scope refusal must explain where writes may land")
        assert.Contains(t, readFirstRefusal, "was not read during this review", "the guard must tell the model to read first")
}

// TestLearningForkForgetsNothing: the forget tool is advertised (parity) but
// not whitelisted — an unattended review adds and fixes, it never destroys.
func TestLearningForkForgetsNothing(t *testing.T) {
        srv, requests := learnLLMServer([]scriptedRound{
                {Text: "turn answer"},
                {Tool: "forget", Args: `{"id":"1"}`},
                {Text: "fine, nothing to remove"},
        })
        defer srv.Close()

        store := openLearningStore(t)
        _, _, err := store.Insert(t.Context(), memstore.NewTrace{Content: "a durable fact", Kind: "fact", Scope: "user", Salience: 0.3})
        require.NoError(t, err)

        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools(learningTools()),
                WithMemoryStore(store),
                WithMemoryNudgeInterval(1),
        )
        require.NoError(t, err)

        require.NoError(t, drainLoop(t, eng, "one turn"))
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        require.NoError(t, eng.WaitForOutstandingOutlets(ctx))

        denied := false
        for _, rq := range requests() {
                for _, msg := range rq.Messages {
                        if msg.Role == "tool" && contains(msg.Content, "background review can only use learning tools") {
                                denied = true
                        }
                }
        }
        assert.True(t, denied, "forget must be denied at dispatch in the review fork")
}

// TestLearningAbortedTurnNeverReviews: a turn that dies mid-stream is not a
// completed turn — no consolidation, no learning review.
func TestLearningAbortedTurnNeverReviews(t *testing.T) {
        var mu sync.Mutex
        var captured []learnCapturedRequest
        streamN := 0
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                body, _ := io.ReadAll(r.Body)
                var cap learnCapturedRequest
                _ = json.Unmarshal(body, &cap)
                mu.Lock()
                captured = append(captured, cap)
                streamN++
                mu.Unlock()
                http.Error(w, "boom", http.StatusBadRequest)
        }))
        defer srv.Close()

        store := openLearningStore(t)
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
                sess,
                WithGate(permission.AllowAll{}),
                WithTools(learningTools()),
                WithMemoryStore(store),
                WithMemoryNudgeInterval(1),
        )
        require.NoError(t, err)

        require.Error(t, drainLoop(t, eng, "this turn will die"))

        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        // No outlets and no reviews were armed; the drain returns immediately.
        require.NoError(t, eng.WaitForOutstandingOutlets(ctx))
        mu.Lock()
        defer mu.Unlock()
        assert.Empty(t, reviewRequests(captured), "an aborted turn must not fire the review fork")
}

// TestLearningReviewPromptCarriesTheContracts pins the review prompt's
// load-bearing lines — the ones hermes paid incidents for: the do-not-capture
// list (negative tool claims harden into self-cited refusals), fix-in-place
// over append-UPDATE, and the escape hatch that keeps the pass honest.
func TestLearningReviewPromptCarriesTheContracts(t *testing.T) {
        both, err := buildLearningReviewPrompt(learningReviewPromptData{
                ReviewMemory: true,
                ReviewSkills: true,
                SkillRoot:    "/skills",
        })
        require.NoError(t, err)
        for _, want := range []string{
                "harden into refusals the agent cites against itself",
                "edit the sentence that misled",
                "UPDATE: actually",
                "The same lesson learned twice is ONE rule",
                "Nothing to save.",
                "Read-before-write",
                "UPDATE A SKILL CONSULTED THIS SESSION",
                "Unresolved failures",
                "forget is denied here",
        } {
                assert.Contains(t, both, want)
        }

        memoryOnly, err := buildLearningReviewPrompt(learningReviewPromptData{ReviewMemory: true})
        require.NoError(t, err)
        assert.Contains(t, memoryOnly, "**Memory**")
        assert.NotContains(t, memoryOnly, "**Skills**")
        assert.NotContains(t, memoryOnly, "list_skills")

        skillsOnly, err := buildLearningReviewPrompt(learningReviewPromptData{ReviewSkills: true, SkillRoot: "/skills"})
        require.NoError(t, err)
        assert.Contains(t, skillsOnly, "**Skills**")
        assert.NotContains(t, skillsOnly, "**Memory**")
        assert.NotContains(t, skillsOnly, "remember/revise")
}

// TestLearningForkDenyMessageNamesSubstitutes: the deny message adapts to the
// live substrates so it never advertises a tool the fork cannot call.
func TestLearningForkDenyMessageNamesSubstitutes(t *testing.T) {
        withBoth := forkDenyMessage("bash", "/skills", true)
        assert.Contains(t, withBoth, "read/search")
        assert.Contains(t, withBoth, "list_skills")
        assert.Contains(t, withBoth, "write (mode=write|edit, under /skills only)")
        assert.Contains(t, withBoth, "remember/revise")

        memoryOnly := forkDenyMessage("bash", "", true)
        assert.Contains(t, memoryOnly, "remember/revise")
        assert.NotContains(t, memoryOnly, "list_skills")

        neither := forkDenyMessage("bash", "", false)
        assert.Contains(t, neither, "read/search to inspect")
        assert.NotContains(t, neither, "list_skills")
        assert.NotContains(t, neither, "remember/revise")
}

// TestLearningWriteGuardsAreWrapperOnly: the scoping wrappers must live only
// in the fork's registry — the parent's write tool stays untouched.
func TestLearningWriteGuardsAreWrapperOnly(t *testing.T) {
        marks := &forkReadMarks{}
        root := t.TempDir()
        scoped := wrapForkWrite(writetool.WriteTool(), marks, root)
        target := filepath.Join(root, "plain.txt")
        require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))

        // Outside the root: refused by the wrapper...
        _, err := scoped.Run(t.Context(), json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"y"}`, filepath.Join(t.TempDir(), "other.txt"))))
        require.Error(t, err)
        assert.Contains(t, err.Error(), "skills directory")

        // ...while the parent's real tool writes it without complaint.
        _, err = writetool.WriteTool().Run(t.Context(), json.RawMessage(fmt.Sprintf(`{"path":%q,"content":"y"}`, filepath.Join(t.TempDir(), "other.txt"))))
        require.NoError(t, err)
}

// TestLearningPathUnderDir pins the containment predicate at its edges.
func TestLearningPathUnderDir(t *testing.T) {
        root := t.TempDir()
        assert.True(t, pathUnderDir(filepath.Join(root, "a", "SKILL.md"), root))
        assert.True(t, pathUnderDir(root+"/x", root))
        assert.False(t, pathUnderDir(root+"-sibling/x", root), "a sibling directory sharing the prefix must not pass")
        assert.False(t, pathUnderDir(filepath.Dir(root), root), "the parent must not pass")
        assert.False(t, pathUnderDir(filepath.Join(root, "..", "escape"), root))
        assert.False(t, pathUnderDir("/elsewhere/x", root))
}

// Silence unused-import churn when suites run selectively.
var _ = readtool.ReadTool
var _ = searchtool.SearchTool
