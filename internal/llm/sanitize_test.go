package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepairToolCallArguments(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"valid object stays verbatim", `{"path": "/tmp/x"}`, `{"path": "/tmp/x"}`},
		{"valid object with whitespace padding stays verbatim", "  {\"a\": 1}  ", "  {\"a\": 1}  "},
		{"valid non-object JSON stays verbatim", `"/etc/hosts"`, `"/etc/hosts"`},
		{"empty object stays verbatim", `{}`, `{}`},
		{"blank becomes {}", "", "{}"},
		{"whitespace becomes {}", "   ", "{}"},
		{"null becomes {}", "null", "{}"},
		{"truncated object becomes {}", `{"path": "/home/x/README.md`, "{}"},
		{"truncated string becomes {}", `"/home/x`, "{}"},
		{"concatenated objects become {}", `{"a":1}{"a":1}`, "{}"},
		{"object with trailing garbage becomes {}", `{"a":1} oops`, "{}"},
		{"lone brace becomes {}", "{", "{}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, RepairToolCallArguments(tc.in))
		})
	}
}

func TestSanitizeMessages(t *testing.T) {
	poisoned := []Message{
		{Role: RoleUser, Content: "rewrite the readme"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "c1", Function: Function{Name: "write", Arguments: `{"path": "/tmp/a"}`}},
			{ID: "c2", Function: Function{Name: "write", Arguments: `{"path": "/tmp/b`}},
		}},
		{Role: RoleTool, ToolCallID: "c1", Content: "ok"},
		{Role: RoleTool, ToolCallID: "c2", Content: "refused"},
	}

	got := SanitizeMessages(poisoned)

	require.Len(t, got, 4)
	assert.Equal(t, `{"path": "/tmp/a"}`, got[1].ToolCalls[0].Function.Arguments, "valid arguments ride verbatim")
	assert.Equal(t, "{}", got[1].ToolCalls[1].Function.Arguments, "truncated arguments are repaired")
	assert.Equal(t, `{"path": "/tmp/b`, poisoned[1].ToolCalls[1].Function.Arguments, "the input slice is never rewritten")

	// The repaired slice must serialize: this is the property the wire needs.
	body, err := json.Marshal(got[1].ToolCalls)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"arguments":"{}"`)
}

func TestSanitizeMessagesCleanSliceIsReturnedUnchanged(t *testing.T) {
	clean := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "c1", Function: Function{Name: "read", Arguments: `{"path": "/tmp/a"}`}},
		}},
	}

	got := SanitizeMessages(clean)

	assert.Same(t, &clean[0], &got[0], "a clean history is passed through without copying")
}

func TestSanitizeMessagesBlankArgumentsRepaired(t *testing.T) {
	msgs := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "c1", Function: Function{Name: "write", Arguments: ``}},
		}},
	}

	got := SanitizeMessages(msgs)

	assert.Equal(t, "{}", got[0].ToolCalls[0].Function.Arguments)
}
