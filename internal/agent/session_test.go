package agent

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
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

	first := sess.BuildContext()
	require.True(t, len(first) > 0)
	assert.Equal(t, llm.RoleSystem, first[0].Role)
	assert.Contains(t, first[0].Content, "remember this")

	second := sess.BuildContext()
	assert.Equal(t, first, second)

	systemCount := 0
	for _, m := range second {
		if m.Role == llm.RoleSystem && strings.Contains(m.Content, "Session memory") {
			systemCount++
		}
	}
	assert.Equal(t, 1, systemCount, "memory system message should appear exactly once")
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
		assert.NotEqual(t, llm.RoleSystem, m.Role, "no system memory message when bank is empty")
	}
}
