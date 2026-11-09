package agent

import (
        "context"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/memstore"
)

// TestBuildRuntimeRider_EmptyWithoutSections pins the skip: a turn with no
// fresh memories and no job notices produces NO rider — an empty user
// message in history would cost a cache-write round for zero content. The
// clock and the forecast that used to guarantee a non-empty rider are fetch
// engines now, so the empty rider is the common case, not the exception.
func TestBuildRuntimeRider_EmptyWithoutSections(t *testing.T) {
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := NewEngine(llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:9", APIKey: "x"}, sess)
        require.NoError(t, err)

        rider, ok := eng.buildRuntimeRider(t.Context(), "hello")
        assert.False(t, ok, "no fresh memories, no job notices: no rider at all")
        assert.Empty(t, rider.Content)
}

// TestBuildRuntimeRider_MemoryChangeGate pins the change gate: a memory
// bundle identical to the last one persisted is NOT re-sent — it is already
// frozen in history from the turn that injected it, and re-sending it would
// duplicate the block forever. A cleared gate (persona switch) re-injects.
func TestBuildRuntimeRider_MemoryChangeGate(t *testing.T) {
        _, pstore, semStore := setupMemoryE2E(t)

        const memoryContent = "The user's name is Solis and they build Go agent harnesses."
        srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                require.Equal(t, "/chat/completions", r.URL.Path)
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"ops\":[{\"operation\":\"CREATE\",\"content\":\"` + memoryContent + `\",\"kind\":\"fact\",\"scope\":\"user\"}]}"},"finish_reason":"stop"}]}`))
        }))
        defer srv.Close()

        eng2 := newConsolidationEngine(t, pstore, srv.URL)
        eng2.consolidateSemanticMemories(
                "hey, my name is Solis and I build Go agent harnesses",
                "Nice to meet you, Solis — noted.",
                []string{"hey, my name is Solis and I build Go agent harnesses"},
        )
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        require.NoError(t, eng2.WaitForOutstandingOutlets(ctx))

        const prompt = "what do I do again? Solis things"

        first, ok := eng2.buildRuntimeRider(ctx, prompt)
        require.True(t, ok, "a stored personal fact makes the rider non-empty")
        assert.Equal(t, llm.RoleUser, first.Role, "the rider rides as a user message — system text gets hoisted into the cached system block")
        assert.True(t, first.Runtime, "the rider is marked so transcript surfaces skip it")
        assert.Contains(t, first.Content, "Solis", "a stored personal fact rides the rider")
        assert.Contains(t, first.Content, "memories about the user")

        second, ok := eng2.buildRuntimeRider(ctx, prompt)
        assert.False(t, ok,
                "an identical bundle is change-gated: it is already frozen in history, and with nothing else to say the rider is skipped")
        assert.NotContains(t, second.Content, "Solis")

        // The gate is the engine's last persisted bundle — clearing it (what a
        // persona switch does) re-opens injection.
        eng2.lastMemRider = ""
        third, ok := eng2.buildRuntimeRider(ctx, prompt)
        require.True(t, ok, "a cleared gate re-injects")
        assert.Contains(t, third.Content, "Solis", "a cleared gate re-injects")

        // The store still holds the memory; the gate only suppresses duplicates.
        traces, err := semStore.Retrieve(ctx, prompt, memstore.ModeCompanion, memstore.Cues{}, time.Now())
        require.NoError(t, err)
        require.NotEmpty(t, traces)
}

// TestFindGitRoot_NoGitDir verifies findGitRoot returns "" when no .git
// directory is found in any ancestor.
func TestFindGitRoot_NoGitDir(t *testing.T) {
        tmp := t.TempDir()
        // Walk up from tmp — should hit the OS root without finding .git
        // (unless this test is run inside a git checkout, which TempDir is not).
        got := findGitRoot(tmp)
        if got != "" {
                // Some CI environments run tests inside a git repo. In that case,
                // findGitRoot walking up will find the repo root. That's a legitimate
                // answer (the test isn't inside a fixture, but inside someone's
                // actual repo). Accept any non-empty answer that contains .git.
                if _, err := os.Stat(filepath.Join(got, ".git")); err != nil {
                        t.Errorf("findGitRoot(%q) returned %q which doesn't contain .git: %v", tmp, got, err)
                }
        }
}

// TestFindGitRoot_FindsRepoRoot creates a fake .git dir and verifies
// findGitRoot walks up to it.
func TestFindGitRoot_FindsRepoRoot(t *testing.T) {
        repoRoot := t.TempDir()
        require.NoError(t, os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755))
        subdir := filepath.Join(repoRoot, "src", "pkg", "internal")
        require.NoError(t, os.MkdirAll(subdir, 0o755))
        got := findGitRoot(subdir)
        assert.Equal(t, repoRoot, got, "findGitRoot should walk up to the .git-containing ancestor")
}
