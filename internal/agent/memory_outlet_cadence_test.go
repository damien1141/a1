package agent

import (
        "context"
        "fmt"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/job"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/memstore"
        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/permission"
)

// memory_outlet_cadence_test.go — the local-endpoint consolidation policy.
//
// A local LLM server keeps the conversation's prompt cache warm only while
// nothing else touches it: consolidation is a different-prefix request, so
// on a single-slot server it evicts the cache and the next user turn
// re-prefills everything. The auto policy batches the outlet to every
// localConsolidationInterval-th completed turn when the ops endpoint is
// local, and keeps per-turn consolidation everywhere else.

func TestConsolidationIntervalAutoPolicy(t *testing.T) {
        t.Run("remote endpoint consolidates per turn", func(t *testing.T) {
                sess, err := NewSession(WithCwd(t.TempDir()))
                require.NoError(t, err)
                eng, err := NewEngine(
                        llm.ModelConfig{Name: "fake", BaseURL: "https://api.example.com/v1", APIKey: "x"},
                        sess,
                        WithGate(permission.AllowAll{}),
                )
                require.NoError(t, err)
                assert.Equal(t, 1, eng.consolidationInterval)
        })

        t.Run("local endpoint batches", func(t *testing.T) {
                sess, err := NewSession(WithCwd(t.TempDir()))
                require.NoError(t, err)
                eng, err := NewEngine(
                        llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:1234/v1", APIKey: "x"},
                        sess,
                        WithGate(permission.AllowAll{}),
                )
                require.NoError(t, err)
                assert.Equal(t, localConsolidationInterval, eng.consolidationInterval)
        })

        t.Run("pinned interval wins over locality", func(t *testing.T) {
                sess, err := NewSession(WithCwd(t.TempDir()))
                require.NoError(t, err)
                eng, err := NewEngine(
                        llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:1234/v1", APIKey: "x"},
                        sess,
                        WithGate(permission.AllowAll{}),
                        WithConsolidationInterval(3),
                )
                require.NoError(t, err)
                assert.Equal(t, 3, eng.consolidationInterval)
        })

        t.Run("private-network hosts count as local", func(t *testing.T) {
                sess, err := NewSession(WithCwd(t.TempDir()))
                require.NoError(t, err)
                eng, err := NewEngine(
                        llm.ModelConfig{Name: "fake", BaseURL: "http://192.168.1.20:8080/v1", APIKey: "x"},
                        sess,
                        WithGate(permission.AllowAll{}),
                )
                require.NoError(t, err)
                assert.Equal(t, localConsolidationInterval, eng.consolidationInterval)
        })

        t.Run("LocalOpsEndpoint follows the persona memory model routing", func(t *testing.T) {
                root := t.TempDir()
                pstore := persona.NewStore(fmt.Sprintf("%s/personas", root))
                p, err := pstore.Create("solo", "solo persona", "")
                require.NoError(t, err)
                require.NoError(t, pstore.SetActive(p.ID))

                sess, err := NewSession(WithPersonaStore(pstore), WithCwd(t.TempDir()))
                require.NoError(t, err)

                // Main model local, no memory model: ops are local.
                eng, err := NewEngine(
                        llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:1234/v1", APIKey: "x"},
                        sess,
                        WithGate(permission.AllowAll{}),
                )
                require.NoError(t, err)
                assert.True(t, eng.LocalOpsEndpoint())

                // A persona memory model routed to a remote endpoint takes ops
                // off the local conversation server: per-turn consolidation and
                // LLM naming return.
                _, err = pstore.SetMemoryModel(p.ID, "ops-remote")
                require.NoError(t, err)
                eng2, err := NewEngine(
                        llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:1234/v1", APIKey: "x"},
                        sess,
                        WithGate(permission.AllowAll{}),
                        WithPersonaModelResolver(func(name string) (llm.ModelConfig, bool) {
                                if name == "ops-remote" {
                                        return llm.ModelConfig{Name: name, BaseURL: "https://api.example.com/v1", APIKey: "x"}, true
                                }
                                return llm.ModelConfig{}, false
                        }),
                )
                require.NoError(t, err)
                assert.False(t, eng2.LocalOpsEndpoint(), "ops routed off the local endpoint")
                assert.Equal(t, 1, eng2.consolidationInterval)
        })
}

// The e2e pin: against a loopback server with the auto policy, the outlet
// fires exactly once per localConsolidationInterval completed turns — the
// skipped turns send no request at all (nothing to evict the cache).
func TestLoopConsolidationBatchesOnLocalEndpoint(t *testing.T) {
        root := t.TempDir()
        pstore := persona.NewStore(fmt.Sprintf("%s/personas", root))
        p, err := pstore.Create("solo", "solo persona", "")
        require.NoError(t, err)
        require.NoError(t, pstore.SetActive(p.ID))

        srv, requests := memoryLLMServer(nil, solisOps) // every round answers plain text
        defer srv.Close()

        sess, err := NewSession(WithPersonaStore(pstore), WithCwd(t.TempDir()))
        require.NoError(t, err)
        // NOTE: no WithConsolidationInterval — the auto policy must read the
        // loopback fake server as a local endpoint and batch. Learn intervals
        // are pinned high so the memory-review fork (whose background request
        // shares this server) cannot add an extra stream round at turn 10.
        eng, err := NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: srv.URL, APIKey: "x"},
                sess,
                WithGate(permission.AllowAll{}),
                WithEmbedder(memstore.FakeEmbedder{DimCount: 64}),
                WithMemoryNudgeInterval(1000),
                WithSkillNudgeInterval(1000),
        )
        require.NoError(t, err)
        require.Equal(t, localConsolidationInterval, eng.consolidationInterval,
                "the loopback fake server must resolve as a local ops endpoint")

        for i := 0; i < localConsolidationInterval; i++ {
                require.NoError(t, drainLoop(t, eng, fmt.Sprintf("hey turn %d — my name is Solis and I build Go agent harnesses", i)))
        }

        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        require.NoError(t, eng.WaitForOutstandingOutlets(ctx))

        streamReqs, compactReqs := 0, 0
        for _, rq := range requests() {
                if rq.Stream {
                        streamReqs++
                } else {
                        compactReqs++
                }
        }
        require.Equal(t, localConsolidationInterval, streamReqs,
                "every turn still drives its conversation round")
        require.Equal(t, 1, compactReqs,
                "exactly one consolidation fires per localConsolidationInterval completed turns — per-turn outlet calls evict a local server's prompt cache and re-prefill the whole context every turn")
}

func stopManager(t *testing.T, mgr *job.Manager) {
        t.Helper()
        ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
        defer cancel()
        require.NoError(t, mgr.Close(ctx))
}

// The fleet mirror: a local main model collapses fleet concurrency to one
// slot (interleaved child prefixes thrash a single-slot server's cache);
// remote keeps the default.
func TestFleetConcurrencyLocalPolicy(t *testing.T) {
        t.Run("local model caps concurrency at one", func(t *testing.T) {
                mgr, err := NewJobManager(t.TempDir(), nil,
                        llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:1234/v1", APIKey: "x"},
                        nil, nil, nil)
                require.NoError(t, err)
                stopManager(t, mgr)
                assert.Equal(t, 1, mgr.MaxConcurrent())
        })

        t.Run("remote model keeps the default", func(t *testing.T) {
                mgr, err := NewJobManager(t.TempDir(), nil,
                        llm.ModelConfig{Name: "fake", BaseURL: "https://api.example.com/v1", APIKey: "x"},
                        nil, nil, nil)
                require.NoError(t, err)
                stopManager(t, mgr)
                assert.Equal(t, 4, mgr.MaxConcurrent())
        })

        t.Run("locality resolves through the modelFn when Model is unset", func(t *testing.T) {
                mgr, err := NewJobManager(t.TempDir(), nil,
                        llm.ModelConfig{Name: "fake"},
                        func(role job.Role) llm.ModelConfig {
                                return llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:1234/v1", APIKey: "x"}
                        },
                        nil, nil)
                require.NoError(t, err)
                stopManager(t, mgr)
                assert.Equal(t, 1, mgr.MaxConcurrent())
        })
}

// IsLocalBaseURL is the predicate every local-aware behavior keys on —
// pin its edges directly.
func TestIsLocalBaseURLEdges(t *testing.T) {
        assert.True(t, llm.IsLocalBaseURL("http://127.0.0.1:1234/v1"))
        assert.True(t, llm.IsLocalBaseURL("http://localhost:1234"))
        assert.True(t, llm.IsLocalBaseURL("http://[::1]:8080/v1"))
        assert.True(t, llm.IsLocalBaseURL("http://192.168.1.20:8080"))
        assert.True(t, llm.IsLocalBaseURL("http://10.0.0.5:11434/v1"))
        assert.False(t, llm.IsLocalBaseURL("https://api.openai.com/v1"))
        assert.False(t, llm.IsLocalBaseURL("https://api.example.com"))
        assert.False(t, llm.IsLocalBaseURL(""), "no endpoint is not local — remote is the safe default")
        assert.False(t, llm.IsLocalBaseURL("not a url"))
}
