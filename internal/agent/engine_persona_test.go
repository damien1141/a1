package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/persona"
)

// A persona marked "replace the default system prompt" owns the prompt at the
// engine level. The session must not re-inject its prompt (the "Persona
// direction… follow it over your default voice" wrapper) or the tagline
// summary — that framing is exactly what the override removes — while the
// persona's memories still ride along as context.
func TestBuildContextOverridePersonaSkipsFramingButKeepsMemories(t *testing.T) {
	root := t.TempDir()
	store := persona.NewStore(filepath.Join(root, "personas"))
	on := true
	p, err := store.CreateInput(persona.Input{
		Name:                 "Sherlock",
		Tagline:              "A brilliant detective persona.",
		Prompt:               "You are Sherlock Holmes, the world's only consulting detective.",
		OverrideSystemPrompt: &on,
	})
	require.NoError(t, err) // CreateInput also activates the persona

	bank, err := store.MemoryBank(p)
	require.NoError(t, err)
	require.NoError(t, bank.Append("memory", "Watson is a trusted colleague."))

	sess, err := NewSession(WithPersonaStore(store))
	require.NoError(t, err)
	require.NoError(t, sess.AddUser("hello"))

	memoriesInjected := false
	for _, m := range sess.BuildContext() {
		if m.Role != llm.RoleSystem {
			continue
		}
		assert.NotContains(t, m.Content, "Persona direction",
			"override persona must not be re-injected as advice")
		assert.NotContains(t, m.Content, "follow it over your default voice")
		assert.NotContains(t, m.Content, "Active persona: Sherlock",
			"summary framing is redundant once the persona owns the system prompt")
		if strings.Contains(m.Content, "Watson is a trusted colleague.") {
			memoriesInjected = true
		}
	}
	assert.True(t, memoriesInjected, "persona memories must still be injected")
}

// A non-override persona keeps the legacy behavior: prompt injected as a
// direction ahead of the tagline summary.
func TestBuildContextNonOverridePersonaStillInjectsDirection(t *testing.T) {
	root := t.TempDir()
	store := persona.NewStore(filepath.Join(root, "personas"))
	_, err := store.CreateInput(persona.Input{
		Name:    "Mycroft",
		Tagline: "The other Holmes.",
		Prompt:  "You are Mycroft; you never leave the Diogenes Club.",
	})
	require.NoError(t, err)

	sess, err := NewSession(WithPersonaStore(store))
	require.NoError(t, err)
	require.NoError(t, sess.AddUser("hello"))

	direction, summary := false, false
	for _, m := range sess.BuildContext() {
		if m.Role != llm.RoleSystem {
			continue
		}
		if strings.Contains(m.Content, "Persona direction for Mycroft") {
			direction = true
		}
		if strings.Contains(m.Content, "Active persona: Mycroft") {
			summary = true
		}
	}
	assert.True(t, direction, "non-override persona keeps its direction injection")
	assert.True(t, summary, "non-override persona keeps its summary injection")
}

func boolPtr(b bool) *bool { return &b }

// The engine's system prompt is what the LLM client actually sends: when the
// active persona opts in, its prompt replaces the built-in a1 template
// verbatim. Switching personas rebinds the prompt live.
func TestEngineSystemPromptPersonaOverride(t *testing.T) {
	root := t.TempDir()
	store := persona.NewStore(filepath.Join(root, "personas"))
	moriarty, err := store.CreateInput(persona.Input{
		Name:                 "Moriarty",
		Prompt:               "You are Professor Moriarty.",
		OverrideSystemPrompt: boolPtr(true),
	})
	require.NoError(t, err)

	sess, err := NewSession(WithPersonaStore(store))
	require.NoError(t, err)
	eng, err := NewEngine(llm.ModelConfig{}, sess)
	require.NoError(t, err)

	// CreateInput activated Moriarty, so the engine was born with the takeover.
	require.Equal(t, "You are Professor Moriarty.", eng.systemPrompt())

	// Switching to the default persona hands the built-in prompt back.
	require.NoError(t, eng.SetPersona(persona.DefaultPersonaID))
	assert.Contains(t, eng.systemPrompt(), "You are A1",
		"default persona must restore the built-in system prompt")

	// And back on — the engine-level switch must rebuild the client prompt.
	require.NoError(t, eng.SetPersona(moriarty.ID))
	require.Equal(t, "You are Professor Moriarty.", eng.systemPrompt())
}

// An override persona with an empty prompt cannot replace anything — the
// built-in prompt stays (an empty system prompt would lobotomize the agent).
func TestEngineSystemPromptOverrideWithoutPromptKeepsDefault(t *testing.T) {
	root := t.TempDir()
	store := persona.NewStore(filepath.Join(root, "personas"))
	_, err := store.CreateInput(persona.Input{
		Name:                 "Ghost",
		OverrideSystemPrompt: boolPtr(true), // but no prompt
	})
	require.NoError(t, err)

	sess, err := NewSession(WithPersonaStore(store))
	require.NoError(t, err)
	eng, err := NewEngine(llm.ModelConfig{}, sess)
	require.NoError(t, err)

	assert.Contains(t, eng.systemPrompt(), "You are A1",
		"override with an empty prompt keeps the default system prompt")
}
