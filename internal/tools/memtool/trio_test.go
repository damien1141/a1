package memtool

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/memstore"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// openTestStore builds a real memstore on a temp SQLite file with the
// deterministic FakeEmbedder — the same double memstore's own tests use — so
// the tools run end to end without any network.
func openTestStore(t *testing.T) *memstore.Store {
	t.Helper()
	s, err := memstore.Open(
		filepath.Join(t.TempDir(), "m.db"), memstore.FakeEmbedder{DimCount: 64}, memstore.DefaultParams())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func memoryTool(t *testing.T, store *memstore.Store, name string) tooldef.Tool {
	t.Helper()
	for _, tl := range Tools(store) {
		if tl.Definition.Name == name {
			return tl
		}
	}
	require.FailNowf(t, "memory tool missing", "tool %q not returned by Tools()", name)
	return tooldef.Tool{}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return raw
}

// ---------- positive ----------

func TestTools_Positive(t *testing.T) {
	store := openTestStore(t)
	tools := Tools(store)
	require.Len(t, tools, 4)

	required := map[string][]string{
		"remember": {"content", "kind", "scope"},
		"recall":   {"query"},
		"revise":   {"memory_id", "new_content", "reason"},
		"forget":   nil,
	}
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		require.NotNil(t, tl.Run, "%s must carry a Run handler", tl.Definition.Name)
		require.NotNil(t, tl.DetailFromArgs, "%s must carry DetailFromArgs", tl.Definition.Name)
		assert.Contains(t, required, tl.Definition.Name)
		assert.ElementsMatch(t, required[tl.Definition.Name], tl.Definition.Params.Required,
			"%s required params", tl.Definition.Name)
		names = append(names, tl.Definition.Name)
	}
	assert.ElementsMatch(t, []string{"remember", "recall", "revise", "forget"}, names)

	// recall is the one read-only tool; the other three mutate the store.
	assert.True(t, memoryTool(t, store, "recall").Definition.Readable)
	for _, name := range []string{"remember", "revise", "forget"} {
		assert.False(t, memoryTool(t, store, name).Definition.Readable, "%s must not be Readable", name)
	}
}

func TestRunRemember_Positive(t *testing.T) {
	store := openTestStore(t)
	remember := memoryTool(t, store, "remember")

	raw := mustJSON(t, map[string]any{
		"content":  "User prefers toml over yaml for configuration files",
		"kind":     "Preference",
		"scope":    " User ",
		"salience": 0.9,
	})
	res, err := remember.Run(t.Context(), raw)
	require.NoError(t, err)
	// kind/scope are normalized to lowercase/trimmed before the save message.
	assert.Equal(t, "Memory #1 saved (kind=preference, scope=user)", res.Content)
	assert.Equal(t, "preference", res.Detail)
	assert.Equal(t, res.Content, res.Output)

	// The same content again merges into #1 (cosine 1.0 ≥ 0.90 with the
	// bag-of-words FakeEmbedder) instead of creating a duplicate row.
	res, err = remember.Run(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "Memory #1 merged into existing (kind=preference, scope=user)", res.Content)
}

func TestRunRemember_DefaultScope_Positive(t *testing.T) {
	store := openTestStore(t)
	remember := memoryTool(t, store, "remember")

	res, err := remember.Run(t.Context(), mustJSON(t, map[string]any{
		"content": "Deploys happen on Fridays",
		"kind":    "fact",
	}))
	require.NoError(t, err)
	assert.Equal(t, "Memory #1 saved (kind=fact, scope=user)", res.Content)
}

func TestRunRecall_Positive(t *testing.T) {
	t.Run("matching query returns the ranked line", func(t *testing.T) {
		store := openTestStore(t)
		const content = "The quokka population on Rottnest Island prefers sheltered habitats"
		_, err := memoryTool(t, store, "remember").Run(t.Context(), mustJSON(t, map[string]any{
			"content": content, "kind": "fact", "scope": "user",
		}))
		require.NoError(t, err)

		res, err := memoryTool(t, store, "recall").Run(t.Context(),
			mustJSON(t, map[string]any{"query": "quokka habitat preferences"}))
		require.NoError(t, err)
		assert.Equal(t, "1 memories", res.Detail)
		assert.Equal(t, res.Content, res.Output)

		// Documented per-result format: [date · kind · P=0.xx] #id: content.
		want := regexp.MustCompile(
			`^\[\d{4}-\d{2}-\d{2} · fact · P=0\.\d{2}\] #1: ` + regexp.QuoteMeta(content) + `$`)
		lines := strings.Split(res.Content, "\n")
		require.Len(t, lines, 1, "one memory must render as one line")
		assert.Regexp(t, want, lines[0])
	})

	t.Run("empty store reports no matches", func(t *testing.T) {
		store := openTestStore(t)
		res, err := memoryTool(t, store, "recall").Run(t.Context(),
			mustJSON(t, map[string]any{"query": "anything at all"}))
		require.NoError(t, err)
		assert.Equal(t, "No memories matched", res.Content)
		assert.Equal(t, "0 memories", res.Detail)
		assert.Equal(t, "No memories matched", res.Output)
	})
}

func TestRunRevise_Positive(t *testing.T) {
	store := openTestStore(t)
	_, err := memoryTool(t, store, "remember").Run(t.Context(), mustJSON(t, map[string]any{
		"content": "Deploys use make release, never make deploy", "kind": "fact", "scope": "user",
	}))
	require.NoError(t, err)

	res, err := memoryTool(t, store, "revise").Run(t.Context(), mustJSON(t, map[string]any{
		"memory_id": 1, "new_content": "Deploys use goreleaser release, never make deploy", "reason": "pipeline migrated",
	}))
	require.NoError(t, err)
	assert.Equal(t, "Memory #1 revised", res.Content)
	assert.Equal(t, "revise #1", res.Detail)

	// The revised content is what recall surfaces afterwards.
	res, err = memoryTool(t, store, "recall").Run(t.Context(),
		mustJSON(t, map[string]any{"query": "goreleaser deploys"}))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "goreleaser")
	assert.NotContains(t, res.Content, "make release")
}

func TestRunForget_Positive(t *testing.T) {
	t.Run("by memory_id", func(t *testing.T) {
		store := openTestStore(t)
		_, err := memoryTool(t, store, "remember").Run(t.Context(), mustJSON(t, map[string]any{
			"content": "The greenhouse waters ferns at dawn", "kind": "fact", "scope": "user",
		}))
		require.NoError(t, err)

		res, err := memoryTool(t, store, "forget").Run(t.Context(),
			mustJSON(t, map[string]any{"memory_id": 1}))
		require.NoError(t, err)
		assert.Equal(t, "Memory #1 forgotten", res.Content)
		assert.Equal(t, "forget #1", res.Detail)

		// The memory is gone: recall finds nothing.
		res, err = memoryTool(t, store, "recall").Run(t.Context(),
			mustJSON(t, map[string]any{"query": "greenhouse ferns dawn"}))
		require.NoError(t, err)
		assert.Equal(t, "No memories matched", res.Content)
	})

	t.Run("by query", func(t *testing.T) {
		store := openTestStore(t)
		_, err := memoryTool(t, store, "remember").Run(t.Context(), mustJSON(t, map[string]any{
			"content": "The aviary keeps budgies behind glass", "kind": "fact", "scope": "user",
		}))
		require.NoError(t, err)

		res, err := memoryTool(t, store, "forget").Run(t.Context(),
			mustJSON(t, map[string]any{"query": "budgies aviary"}))
		require.NoError(t, err)
		assert.Equal(t, `Forgot 1 memories matching "budgies aviary"`, res.Content)
		assert.Equal(t, "forget 1", res.Detail)
	})
}

// ---------- negative ----------

// The toolaudit path: a nil store must yield no tools, not a panic or a
// half-built tool that would nil-deref on first Run.
func TestTools_Negative(t *testing.T) {
	assert.Nil(t, Tools(nil))
}

func TestRunRemember_Negative(t *testing.T) {
	remember := memoryTool(t, openTestStore(t), "remember")

	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"malformed json", `{"content":`, "failed to parse remember arguments"},
		{"wrong field type", `{"content": 42, "kind": "fact", "scope": "user"}`, "failed to parse remember arguments"},
		{"missing content", `{"kind": "fact", "scope": "user"}`, "content is required"},
		{"blank content", `{"content": "   ", "kind": "fact", "scope": "user"}`, "content is required"},
		{"missing kind", `{"content": "likes tea", "scope": "user"}`, "kind is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := remember.Run(t.Context(), json.RawMessage(tc.input))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
			assert.Empty(t, res.Content, "a rejected remember must not report saved content")
		})
	}
}

func TestRunRecall_Negative(t *testing.T) {
	recall := memoryTool(t, openTestStore(t), "recall")

	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"malformed json", `{"query"`, "failed to parse recall arguments"},
		{"missing query", `{}`, "query is required"},
		{"blank query", `{"query": "   "}`, "query is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := recall.Run(t.Context(), json.RawMessage(tc.input))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestRunRevise_Negative(t *testing.T) {
	store := openTestStore(t)
	_, err := memoryTool(t, store, "remember").Run(t.Context(), mustJSON(t, map[string]any{
		"content": "Seed memory for revise failures", "kind": "fact", "scope": "user",
	}))
	require.NoError(t, err)
	revise := memoryTool(t, store, "revise")

	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"malformed json", `{"memory_id"`, "failed to parse revise arguments"},
		{"zero id", `{"memory_id": 0, "new_content": "x", "reason": "r"}`, "memory_id is required"},
		{"negative id", `{"memory_id": -3, "new_content": "x", "reason": "r"}`, "memory_id is required"},
		{"blank new content", `{"memory_id": 1, "new_content": "  ", "reason": "r"}`, "new_content is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := revise.Run(t.Context(), json.RawMessage(tc.input))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}

	// An unknown id reaches the store, which fails the lookup: the raw
	// sql.ErrNoRows surfaces through the tool instead of a fake success.
	_, err = revise.Run(t.Context(), mustJSON(t, map[string]any{
		"memory_id": 424242, "new_content": "x", "reason": "r",
	}))
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestRunForget_Negative(t *testing.T) {
	forget := memoryTool(t, openTestStore(t), "forget")

	t.Run("malformed json", func(t *testing.T) {
		_, err := forget.Run(t.Context(), json.RawMessage(`{"query"`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse forget arguments")
	})

	t.Run("neither id nor query", func(t *testing.T) {
		_, err := forget.Run(t.Context(), json.RawMessage(`{}`))
		require.Error(t, err)
		assert.EqualError(t, err, "provide either memory_id or query")
	})

	// A query with nothing to delete is a graceful no-op, not an error.
	t.Run("query matches nothing", func(t *testing.T) {
		res, err := forget.Run(t.Context(), mustJSON(t, map[string]any{"query": "ghosts"}))
		require.NoError(t, err)
		assert.Equal(t, "No memories matched", res.Content)
		assert.Equal(t, "0 forgotten", res.Detail)
	})
}

// ---------- differential ----------

// naiveTruncate re-derives the 40-byte detail truncation independently.
func naiveTruncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 40 {
		s = s[:40] + "..."
	}
	return s
}

// naiveField decodes one string field through a generic map — a different
// encoding/json path than the tools' struct decode — tolerating hostile
// shapes the same way a failed struct unmarshal leaves a zero field.
func naiveField(input json.RawMessage, key string) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	s, _ := m[key].(string)
	return s
}

func naiveInt(input json.RawMessage, key string) int64 {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	f, ok := m[key].(float64)
	if !ok {
		return 0
	}
	return int64(f)
}

// naiveDetail is the reference implementation of the four detail lines.
func naiveDetail(tool string, input json.RawMessage) string {
	switch tool {
	case "remember":
		return fmt.Sprintf("remember %s: %s", naiveField(input, "kind"), naiveTruncate(naiveField(input, "content")))
	case "recall":
		return fmt.Sprintf("recall %q", naiveTruncate(naiveField(input, "query")))
	case "revise":
		return fmt.Sprintf("revise #%d", naiveInt(input, "memory_id"))
	case "forget":
		if id := naiveInt(input, "memory_id"); id > 0 {
			return fmt.Sprintf("forget #%d", id)
		}
		return "forget"
	}
	return ""
}

// oracle: naive reference implementation in this test — a map-based
// encoding/json decode (the tools decode into structs) plus an independently
// written truncation and formatting, compared over normal, edge, and hostile
// inputs including non-JSON payloads, which the detail helpers must survive.
func TestDetailFromArgs_Differential(t *testing.T) {
	long := strings.Repeat("passport", 10)   // 80 chars, past the 40-byte cut
	unicode := strings.Repeat("日本語テキスト", 12) // the byte cut lands mid-rune

	cases := []struct {
		name  string
		tool  string
		input string
	}{
		{"remember plain", "remember", `{"content":"short one","kind":"fact"}`},
		{"remember long content", "remember", `{"content":"` + long + `","kind":"preference"}`},
		{"remember padded content", "remember", `{"content":"   padded detail   ","kind":"task"}`},
		{"remember multibyte content", "remember", `{"content":"` + unicode + `","kind":"fact"}`},
		{"remember non-string kind", "remember", `{"kind": 3, "content": "x"}`},
		{"remember empty object", "remember", `{}`},
		{"remember not json", "remember", `not json at all`},
		{"remember null content", "remember", `{"content": null, "kind": "fact"}`},
		{"recall plain", "recall", `{"query":"auth flow"}`},
		{"recall long query", "recall", `{"query":"` + long + `"}`},
		{"recall non-string query", "recall", `{"query": 12}`},
		{"recall not json", "recall", `[]`},
		{"revise with id", "revise", `{"memory_id": 7, "new_content": "x", "reason": "r"}`},
		{"revise string id", "revise", `{"memory_id": "7"}`},
		{"revise empty", "revise", `{}`},
		{"forget with id", "forget", `{"memory_id": 3}`},
		{"forget negative id", "forget", `{"memory_id": -1}`},
		{"forget query only", "forget", `{"query": "anything"}`},
		{"forget empty", "forget", `{}`},
		{"forget not json", "forget", `nope`},
	}

	store := openTestStore(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := memoryTool(t, store, tc.tool).DetailFromArgs(json.RawMessage(tc.input))
			assert.Equal(t, naiveDetail(tc.tool, json.RawMessage(tc.input)), got)
		})
	}
}
