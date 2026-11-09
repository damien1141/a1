package tooldef

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// ---------- negative ----------

// The registry is the tool-name index the executor trusts; hostile inputs
// (nil slice, nameless tools, duplicate names) must produce well-defined
// behavior: an empty registry, an "" key, and last-write-wins shadowing.
func TestNewRegistry_Negative(t *testing.T) {
	t.Run("nil tools", func(t *testing.T) {
		assert.Empty(t, NewRegistry(nil))
	})

	t.Run("unknown name yields the zero tool", func(t *testing.T) {
		got, ok := NewRegistry(nil)["missing"]
		assert.False(t, ok)
		assert.Empty(t, got.Definition.Name)
		assert.Nil(t, got.Run)
	})

	t.Run("nameless tool lands under the empty key", func(t *testing.T) {
		tool := Tool{Run: func(context.Context, json.RawMessage) (Result, error) { return Result{}, nil }}
		reg := NewRegistry([]Tool{tool})
		got, ok := reg[""]
		assert.True(t, ok, "a tool with no name is indexed under the empty key")
		assert.NotNil(t, got.Run)
	})

	t.Run("duplicate names shadow silently", func(t *testing.T) {
		first := Tool{Definition: llm.ToolDefinition{Name: "dup"}, DetailFromArgs: func(json.RawMessage) string { return "first" }}
		second := Tool{Definition: llm.ToolDefinition{Name: "dup"}, DetailFromArgs: func(json.RawMessage) string { return "second" }}
		reg := NewRegistry([]Tool{first, second})
		require.Len(t, reg, 1)
		assert.Equal(t, "second", reg["dup"].DetailFromArgs(nil))
	})
}

// Definitions is a pure projection; nil and zero-value entries must pass
// through positionally intact instead of being dropped or reordered.
func TestDefinitions_Negative(t *testing.T) {
	assert.Empty(t, Definitions(nil))

	defs := Definitions([]Tool{{}, {Definition: llm.ToolDefinition{Name: "middle"}}})
	require.Len(t, defs, 2)
	assert.Empty(t, defs[0].Name)
	assert.Equal(t, "middle", defs[1].Name)
}

// Every From* helper tolerates a bare ctx (zero values, no panic); the
// guarded With* helpers wrap a Background parent, and the two unguarded ones
// (WithToolCallID with a non-empty id, WithScriptDispatch) panic with the
// stdlib "cannot create context from nil parent" — pinned as actual
// behavior so a future guard flipping it is a visible change.
func TestContextHelpers_NilCtx_Negative(t *testing.T) {
	t.Run("from helpers return zero values on nil ctx", func(t *testing.T) {
		assert.Empty(t, ToolCallID(nil))
		assert.Nil(t, QuestionFuncFrom(nil))
		assert.False(t, ImageEnabledFrom(nil))
		assert.Empty(t, PermissionModeFrom(nil))
		assert.Nil(t, ScriptDispatchFrom(context.Background()))
	})

	t.Run("passthrough with helpers return the nil ctx untouched", func(t *testing.T) {
		assert.Nil(t, WithToolCallID(nil, ""))
		assert.Nil(t, WithQuestionFunc(nil, func(context.Context, Question) (QuestionReply, error) {
			return QuestionReply{}, nil
		}))
	})

	t.Run("guarded with helpers wrap background", func(t *testing.T) {
		wd, err := os.Getwd()
		require.NoError(t, err)
		cwd, err := Cwd(WithCwd(nil, "   ")) // whitespace cwd is ignored
		require.NoError(t, err)
		assert.Equal(t, wd, cwd)

		assert.True(t, ImageEnabledFrom(WithImageEnabled(nil, true)))
		assert.Equal(t, "plan", PermissionModeFrom(WithPermissionMode(nil, " plan ")))
	})

	t.Run("unguarded with helpers panic on nil ctx", func(t *testing.T) {
		assert.PanicsWithValue(t, "cannot create context from nil parent", func() {
			_ = WithToolCallID(nil, "id")
		})
		assert.PanicsWithValue(t, "cannot create context from nil parent", func() {
			_ = WithScriptDispatch(nil, func(context.Context, string, json.RawMessage) (string, bool) {
				return "", false
			})
		})
	})

	t.Run("script dispatch from nil ctx panics", func(t *testing.T) {
		assert.Panics(t, func() { _ = ScriptDispatchFrom(nil) })
	})
}

// Cwd on a nil ctx falls back to the process working directory instead of
// panicking — the graceful degradation session-less callers rely on.
func TestCwd_NilCtx_Negative(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	got, err := Cwd(nil)
	require.NoError(t, err)
	assert.Equal(t, wd, got)
}

// A blank path is the boundary input every tool feeds ResolveToCwd when the
// model omits `path`: it must resolve to the session cwd itself, not "" or
// the filesystem root.
func TestResolveToCwd_BlankPath_Negative(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	for _, blank := range []string{"", "   "} {
		got, err := ResolveToCwd(t.Context(), blank)
		require.NoError(t, err)
		assert.Equal(t, dir, got)
	}
}
