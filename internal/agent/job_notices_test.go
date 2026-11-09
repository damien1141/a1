package agent

import (
        "context"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/http/httptest"
        "strings"
        "sync"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/job"
        "github.com/damien1141/a1/internal/llm"
)

// TestBuildJobNoticesRendersDeliveries verifies the notice renderer: jobs
// addressed to this session render with status, role and trimmed summary;
// the collection itself consumes the rows (manager-level behavior, asserted
// here end-to-end through the engine).
func TestBuildJobNoticesRendersDeliveries(t *testing.T) {
        mgr, err := job.New(job.Options{
                Root: t.TempDir(),
                Runner: job.RunnerFunc(func(_ context.Context, env job.RunEnv) (string, error) {
                        return "the auth config lives in internal/auth", nil
                }),
                Recovery: job.RecoverRequeue,
        })
        require.NoError(t, err)
        t.Cleanup(func() { _ = mgr.Close(t.Context()) })

        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        engine, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:9", APIKey: "x"},
                sess,
                WithJobs(mgr),
        )
        require.NoError(t, err)

        // Before any job: no notice.
        assert.Nil(t, engine.buildJobNotices())

        // A finished job addressed to this session.
        info, err := mgr.Spawn(context.Background(), job.SpawnRequest{
                Prompt:      "find auth",
                ParentID:    engine.SessionID(),
                Description: "find auth config",
        })
        require.NoError(t, err)
        _, err = mgr.Wait(context.Background(), info.ID)
        require.NoError(t, err)

        msg := engine.buildJobNotices()
        require.NotNil(t, msg)
        assert.Equal(t, llm.RoleSystem, msg.Role)
        assert.Contains(t, msg.Content, "find auth config")
        assert.Contains(t, msg.Content, info.ID)
        assert.Contains(t, msg.Content, "the auth config lives in internal/auth")

        // Presented exactly once: the collection consumed the row.
        assert.Nil(t, engine.buildJobNotices())
}

// TestLoopInjectsJobNoticesBeforeLastUser drives a full Loop turn against
// a scripted SSE server that captures the request body: the delivered
// notice must ride the wire inside the runtime rider — the user message
// immediately before the last user message — and the row it consumed must
// not ride twice.
func TestLoopInjectsJobNoticesBeforeLastUser(t *testing.T) {
        var mu sync.Mutex
        var bodies []string
        snapshotBodies := func() []string {
                mu.Lock()
                defer mu.Unlock()
                return append([]string(nil), bodies...)
        }
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.URL.Path != "/chat/completions" {
                        http.NotFound(w, r)
                        return
                }
                data, _ := io.ReadAll(r.Body)
                mu.Lock()
                bodies = append(bodies, string(data))
                mu.Unlock()
                w.Header().Set("Content-Type", "text/event-stream")
                _, _ = fmt.Fprint(w, sseTextChunk("done"))
                _, _ = fmt.Fprint(w, "data: [DONE]\n\n")
        }))
        defer server.Close()

        mgr, err := job.New(job.Options{
                Root: t.TempDir(),
                Runner: job.RunnerFunc(func(_ context.Context, env job.RunEnv) (string, error) {
                        return "found it", nil
                }),
                Recovery: job.RecoverRequeue,
        })
        require.NoError(t, err)
        t.Cleanup(func() { _ = mgr.Close(t.Context()) })

        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        engine, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"},
                sess,
                WithJobs(mgr),
        )
        require.NoError(t, err)

        // A finished job addressed to this session, before the turn runs.
        info, err := mgr.Spawn(context.Background(), job.SpawnRequest{
                Prompt: "find auth", ParentID: engine.SessionID(), Description: "find auth config",
        })
        require.NoError(t, err)
        _, err = mgr.Wait(context.Background(), info.ID)
        require.NoError(t, err)

        for _, err := range engine.Loop(t.Context(), "what found?", LoopOpts{}) {
                require.NoError(t, err)
        }

        snap := snapshotBodies()
        require.Len(t, snap, 1, "one request per turn")
        var req struct {
                Messages []struct {
                        Role    string `json:"role"`
                        Content string `json:"content"`
                } `json:"messages"`
        }
        require.NoError(t, json.Unmarshal([]byte(snap[0]), &req))
        require.GreaterOrEqual(t, len(req.Messages), 2)

        lastUser := -1
        for i := len(req.Messages) - 1; i >= 0; i-- {
                if req.Messages[i].Role == "user" {
                        lastUser = i
                        break
                }
        }
        require.GreaterOrEqual(t, lastUser, 0, "request carries the user message")

        // The notice rides inside the runtime rider: the user message
        // immediately before the real user message, carrying this turn's
        // runtime context.
        riderIdx := lastUser - 1
        require.GreaterOrEqual(t, riderIdx, 0, "a runtime rider precedes the user message")
        assert.Equal(t, "user", req.Messages[riderIdx].Role,
                "the rider is a user message — system text gets hoisted into the cached system block")
        assert.Contains(t, req.Messages[riderIdx].Content, "Background sub-agent job results",
                "the notice rides the rider")
        assert.Contains(t, req.Messages[riderIdx].Content, "find auth config")

        // A second turn: the row was consumed — no second presentation.
        for _, err := range engine.Loop(t.Context(), "thanks", LoopOpts{}) {
                require.NoError(t, err)
        }
        snap2 := snapshotBodies()
        require.Len(t, snap2, 2)
        var req2 struct {
                Messages []struct {
                        Role    string `json:"role"`
                        Content string `json:"content"`
                } `json:"messages"`
        }
        require.NoError(t, json.Unmarshal([]byte(snap2[1]), &req2))
        // The row was consumed: with no fresh notices (and no fresh memories)
        // the new turn rides NO rider at all — the empty-rider skip keeps
        // zero-content user messages out of history. The turn-1 rider stays
        // in the request by design — a delivered result is frozen history
        // the model keeps seeing, not a transient flash that vanishes on the
        // next prompt (the old transient-injection defect).
        secondUserIdx := -1
        for i := len(req2.Messages) - 1; i >= 0; i-- {
                if req2.Messages[i].Role == "user" && strings.Contains(req2.Messages[i].Content, "thanks") {
                        secondUserIdx = i
                        break
                }
        }
        require.GreaterOrEqual(t, secondUserIdx, 0, "the second user message rides the wire")
        prev := req2.Messages[secondUserIdx-1]
        assert.Equal(t, "assistant", prev.Role,
                "with nothing fresh to say, no rider rides — the message before the new prompt is turn 1's final assistant")
        assert.NotContains(t, prev.Content, "Background sub-agent job results",
                "the notice row presents exactly once — nothing re-presents it before the new prompt")
        frozenNotice := false
        for i := 0; i < secondUserIdx-1; i++ {
                if strings.Contains(req2.Messages[i].Content, "Background sub-agent job results") {
                        frozenNotice = true
                        break
                }
        }
        assert.True(t, frozenNotice,
                "the turn-1 rider stays frozen in history for the model to keep reading")
}
