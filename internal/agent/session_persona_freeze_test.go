package agent

import (
        "path/filepath"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/persona"
)

// session_persona_freeze_test.go — the frozen persona injection.
//
// The persona block used to be flag-gated: personaInjected survived the
// per-Append context-cache invalidation, so the first rebuild after a bust
// silently dropped the persona's framing and memories from every later
// turn — the model lost its personality mid-session, and the request
// prefix changed shape between turns 1 and 2, re-billing (or, on a local
// server, re-prefilling) the whole context. The freeze pins both
// properties: the block survives busts byte-identically, and a persona
// switch re-freezes under the new personality.

func newFrozenPersonaSession(t *testing.T) (*Session, *persona.Store) {
        t.Helper()
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
        return sess, store
}

// The bug, pinned: a cache bust (what every Append causes) must not drop
// the persona block from the rebuilt context — and the rebuild must carry
// the exact same persona bytes, so the request prefix stays cache-stable.
func TestPersonaBlockSurvivesCacheBustByteIdentical(t *testing.T) {
        sess, _ := newFrozenPersonaSession(t)
        require.NoError(t, sess.AddUser("hello"))

        first := sess.BuildContext()
        require.NotEmpty(t, first, "the persona block lands ahead of history")
        var personaBytes []llm.Message
        for _, m := range first {
                if m.Role == llm.RoleSystem {
                        personaBytes = append(personaBytes, m)
                }
        }
        require.NotEmpty(t, personaBytes, "persona system messages present on the first build")
        assert.Contains(t, personaBytes[0].Content, "Lead with the outcome",
                "the persona prompt is injected")

        // The bust: appends invalidate the cache; the old flag-gate dropped
        // the persona on exactly this path.
        require.NoError(t, sess.Append(llm.Message{Role: llm.RoleAssistant, Content: "hi"}))
        require.NoError(t, sess.AddUser("again"))
        sess.invalidateContextCache()

        second := sess.BuildContext()
        var rebuiltPersona []llm.Message
        for _, m := range second {
                if m.Role == llm.RoleSystem {
                        rebuiltPersona = append(rebuiltPersona, m)
                }
        }
        require.NotEmpty(t, rebuiltPersona,
                "REGRESSION: the persona block vanished after a cache bust — the model loses its personality mid-session and the prefix re-bills")
        assert.Equal(t, personaBytes, rebuiltPersona,
                "the rebuilt persona block must be byte-identical to the frozen one")

        // Prefix stability: the rebuild extends the first build — everything
        // the first request carried is still the leading bytes of the second.
        require.GreaterOrEqual(t, len(second), len(first))
        assert.Equal(t, first, second[:len(first)],
                "consecutive contexts must be append-only — the property prefix caching bills against")
}

// A persona switch re-freezes: the new personality replaces the old bytes.
func TestPersonaSwitchReFreezesBlock(t *testing.T) {
        sess, store := newFrozenPersonaSession(t)
        require.NoError(t, sess.AddUser("hello"))

        first := sess.BuildContext()
        assert.Contains(t, first[0].Content, "Lead with the outcome")

        _, err := store.CreateInput(persona.Input{Name: "The Skeptic", Prompt: "Question every number."})
        require.NoError(t, err)
        p, err := store.ActivePersona()
        require.NoError(t, err)
        require.NoError(t, sess.SetPersona(p.ID))

        second := sess.BuildContext()
        joined := ""
        for _, m := range second {
                if m.Role == llm.RoleSystem {
                        joined += m.Content + "\n"
                }
        }
        assert.Contains(t, joined, "Question every number.", "the new persona's prompt is injected")
        assert.NotContains(t, joined, "Lead with the outcome", "the old persona's prompt is gone")
}

// The default persona injects nothing — an empty frozen block — and that
// must stay stable across busts too (empty stays empty).
func TestDefaultPersonaFreezesEmptyAndStaysStable(t *testing.T) {
        root := t.TempDir()
        store := persona.NewStore(filepath.Join(root, "personas"))
        sess, err := NewSession(WithPersonaStore(store))
        require.NoError(t, err)
        require.NoError(t, sess.AddUser("hello"))

        first := sess.BuildContext()
        require.NoError(t, sess.Append(llm.Message{Role: llm.RoleAssistant, Content: "hi"}))
        sess.invalidateContextCache()
        second := sess.BuildContext()

        // The append extends the context; the leading bytes (all of first)
        // must be untouched, and no persona framing may appear for the
        // default persona — empty stays empty across busts.
        require.GreaterOrEqual(t, len(second), len(first))
        assert.Equal(t, first, second[:len(first)],
                "no persona, no churn — builds stay append-only")
        for _, m := range second {
                assert.NotContains(t, m.Content, "Persona direction",
                        "the default persona injects no framing")
        }
}
