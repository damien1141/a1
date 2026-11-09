package agent

import (
        "encoding/json"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/session/memory"
)

func TestSessionPersistFlush(t *testing.T) {
        dir := t.TempDir()
        sess, err := NewSession(
                WithCwd(dir),
                WithSessionDir(dir),
                WithPersist(true),
        )
        require.NoError(t, err)
        assert.NotEmpty(t, sess.ID())
        assert.NotEmpty(t, sess.File())

        require.NoError(t, sess.Append(llm.Message{Role: llm.RoleUser, Content: "hi"}))
        _, err = os.Stat(sess.File())
        assert.True(t, os.IsNotExist(err), "should not flush before first assistant")

        require.NoError(t, sess.Append(llm.Message{Role: llm.RoleAssistant, Content: "hello"}))
        require.FileExists(t, sess.File())

        b, err := os.ReadFile(sess.File())
        require.NoError(t, err)
        var header struct {
                Type string `json:"type"`
                ID   string `json:"id"`
        }
        first := splitFirstJSONL(b)
        require.NoError(t, json.Unmarshal(first, &header))
        assert.Equal(t, "EntrySession", header.Type)
        assert.Equal(t, sess.ID(), header.ID)
}

func TestSessionPersistFalseNoDisk(t *testing.T) {
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        assert.Empty(t, sess.File())
        require.NoError(t, sess.Append(llm.Message{Role: llm.RoleUser, Content: "a"}))
        require.NoError(t, sess.Append(llm.Message{Role: llm.RoleAssistant, Content: "b"}))
        assert.Empty(t, sess.File())
}

func TestEngineSetModelKeepsSession(t *testing.T) {
        dir := t.TempDir()
        sess, err := NewSession(
                WithCwd(dir),
                WithSessionDir(dir),
                WithPersist(true),
        )
        require.NoError(t, err)
        eng, err := NewEngine(
                llm.ModelConfig{Name: "model-a", APIKey: "k", BaseURL: "http://example"},
                sess,
        )
        require.NoError(t, err)

        id := eng.SessionID()
        file := eng.SessionFile()
        require.NotEmpty(t, id)
        require.NotEmpty(t, file)

        require.NoError(t, eng.session.Append(llm.Message{Role: llm.RoleUser, Content: "keep me"}))
        require.NoError(t, eng.session.Append(llm.Message{Role: llm.RoleAssistant, Content: "ok"}))
        n := eng.session.Len()

        eng.SetModel(llm.ModelConfig{
                Name:          "model-b",
                APIKey:        "k",
                BaseURL:       "http://example",
                ContextWindow: 8192,
                SkillPath:     dir,
        })
        assert.Equal(t, id, eng.SessionID())
        assert.Equal(t, file, eng.SessionFile())
        assert.Equal(t, n, eng.session.Len())
        assert.Equal(t, 8192, eng.modelCfg.ContextWindow)
        assert.Equal(t, dir, eng.modelCfg.SkillPath)
}

func splitFirstJSONL(b []byte) []byte {
        for i, c := range b {
                if c == '\n' {
                        return b[:i]
                }
        }
        return b
}

func TestBuildContextInjectsMemoryOnce(t *testing.T) {
        dir := t.TempDir()
        sess, err := NewSession(WithCwd(dir), WithSessionDir(dir), WithPersist(true))
        require.NoError(t, err)

        bank, err := memory.OpenBank(sess.ID(), dir)
        require.NoError(t, err)
        require.NoError(t, bank.Append(memory.EntryContext, "remember this"))
        sess.memoryBank = bank
        require.NoError(t, sess.Append(llm.Message{Role: llm.RoleUser, Content: "hi"}))

        first := sess.BuildContext()
        require.True(t, len(first) > 0)
        // The clock no longer leads the context: a per-build system stamp
        // re-billed the whole request as a cache write every round (see
        // runtime_rider.go). BuildContext is now append-only history.
        for _, m := range first {
                assert.NotEqual(t, llm.RoleSystem, m.Role,
                        "no system messages ride the context — runtime context rides the persisted rider")
                assert.NotContains(t, m.Content, "Current date and time",
                        "the clock lives in the runtime rider, not the context")
        }

        // JSONL memory bank injection is now disabled — the semantic memory
        // inlet (engine.injectSemanticMemories) handles auto-injection with
        // vector retrieval. The JSONL bank entries should NOT appear in the
        // context.
        for _, m := range first {
                assert.NotContains(t, m.Content, "Session memory",
                        "JSONL memory bank should not be injected — semantic memory inlet handles it")
                assert.NotContains(t, m.Content, "remember this",
                        "JSONL bank entry should not be injected")
        }

        second := sess.BuildContext()
        assert.Equal(t, first, second)
}

// BuildContext is pure history now: no clock stamp, no weather, nothing
// regenerated per build. The wall clock rides the runtime rider the engine
// persists at each turn boundary (runtime_rider.go) — fresh bytes at the
// tail, frozen once written — so consecutive builds of an unchanged
// session are byte-identical, which is the property prefix caching bills
// against. A session that stays open across a turn boundary still gets a
// fresh clock: the next turn's rider carries it.
func TestBuildContextIsByteStableAcrossBuilds(t *testing.T) {
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        require.NoError(t, sess.Append(llm.Message{Role: llm.RoleUser, Content: "hi"}))
        sess.invalidateContextCache()

        first := sess.BuildContext()
        require.Len(t, first, 1, "pure history: the one user message, nothing else")
        assert.Equal(t, llm.RoleUser, first[0].Role)
        assert.Equal(t, "hi", first[0].Content)

        // A cache bust (what a tool-result append causes mid-turn) must not
        // change a single byte: no re-stamping, no re-ordering.
        sess.invalidateContextCache()
        second := sess.BuildContext()
        assert.Equal(t, first, second, "consecutive builds of an unchanged session must be byte-identical")
}

func TestBuildContextSkipsMemoryWhenBankEmpty(t *testing.T) {
        dir := t.TempDir()
        sess, err := NewSession(WithCwd(dir), WithSessionDir(dir), WithPersist(true))
        require.NoError(t, err)

        bank, err := memory.OpenBank(sess.ID(), dir)
        require.NoError(t, err)
        sess.memoryBank = bank

        ctx := sess.BuildContext()
        for _, m := range ctx {
                if m.Role != llm.RoleSystem {
                        continue
                }
                assert.NotContains(t, m.Content, "Session memory",
                        "no system memory message when bank is empty")
        }
}

// The persona form's "Personality" textarea used to be stored but never
// injected: BuildContext only surfaced the one-line tagline summary, so
// switching personas changed the label but not the model's behavior.
func TestBuildContextInjectsPersonaPrompt(t *testing.T) {
        root := t.TempDir()
        store := persona.NewStore(filepath.Join(root, "personas"))
        _, err := store.CreateInput(persona.Input{
                Name:    "The Closer",
                Tagline: "Sells the value.",
                Prompt:  "Lead with the outcome and the number that proves it.",
        })
        require.NoError(t, err) // CreateInput also activates the persona

        sess, err := NewSession(WithPersonaStore(store))
        require.NoError(t, err)
        require.NoError(t, sess.AddUser("hello"))

        msgs := sess.BuildContext()
        var promptInjected, summaryInjected bool
        for _, m := range msgs {
                if m.Role != llm.RoleSystem {
                        continue
                }
                if strings.Contains(m.Content, "Lead with the outcome and the number that proves it.") {
                        promptInjected = true
                }
                if strings.Contains(m.Content, "Active persona: The Closer.") {
                        summaryInjected = true
                }
        }
        assert.True(t, promptInjected, "persona SystemPrompt missing from context")
        assert.True(t, summaryInjected, "persona summary missing from context")
}

// A persona switch mid-session must retarget the injection: the next
// BuildContext carries the new persona's prompt and drops the old one.
func TestSetPersonaReinjectsPrompt(t *testing.T) {
        root := t.TempDir()
        store := persona.NewStore(filepath.Join(root, "personas"))
        first, err := store.CreateInput(persona.Input{Name: "A", Prompt: "voice A"})
        require.NoError(t, err)
        second, err := store.CreateInput(persona.Input{Name: "B", Prompt: "voice B"})
        require.NoError(t, err)

        sess, err := NewSession(WithPersonaStore(store))
        require.NoError(t, err)
        require.NoError(t, sess.AddUser("hello"))

        _ = sess.BuildContext() // burn the cache with A active
        require.NoError(t, sess.SetPersona(first.ID))
        _ = sess.BuildContext() // reinject with A (switch away from B)
        require.NoError(t, sess.SetPersona(second.ID))

        msgs := sess.BuildContext()
        var hasA, hasB bool
        for _, m := range msgs {
                switch {
                case strings.Contains(m.Content, "voice A"):
                        hasA = true
                case strings.Contains(m.Content, "voice B"):
                        hasB = true
                }
        }
        assert.False(t, hasA, "stale persona prompt survived the switch")
        assert.True(t, hasB, "new persona prompt missing after switch")
}
