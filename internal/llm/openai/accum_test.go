package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// A gateway that streams argument fragments and then echoes the complete
// message in a final chunk must not produce concatenated arguments: the echo
// replaces the accumulated fragments (the same reset semantics content
// already has), or every call from that gateway would arrive as
// `{"a":1}{"a":1}` — invalid JSON.
func TestAccumulatorMessageEchoReplacesToolArguments(t *testing.T) {
	acc := newStreamAccumulator()
	acc.applyDelta(llm.StreamDelta{ToolCalls: []llm.ToolCall{{
		Index:    0,
		ID:       "c1",
		Function: llm.Function{Name: "write", Arguments: `{"pa`},
	}}})
	acc.applyDelta(llm.StreamDelta{ToolCalls: []llm.ToolCall{{
		Index:    0,
		Function: llm.Function{Arguments: `th": "a"}`},
	}}})
	acc.applyMessage(&llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID:       "c1",
			Function: llm.Function{Name: "write", Arguments: `{"path": "a"}`},
		}},
	})

	msg := acc.message()
	require.Len(t, msg.ToolCalls, 1)
	assert.Equal(t, `{"path": "a"}`, msg.ToolCalls[0].Function.Arguments, "the echo replaces the fragments instead of appending")
}

// A message echo with several calls assigns slots positionally — message
// echoes carry no stream index, and a full echo lists every call in order.
func TestAccumulatorMessageEchoMultipleCalls(t *testing.T) {
	acc := newStreamAccumulator()
	acc.applyMessage(&llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{
			{ID: "c1", Function: llm.Function{Name: "read", Arguments: `{"path": "a"}`}},
			{ID: "c2", Function: llm.Function{Name: "read", Arguments: `{"path": "b"}`}},
		},
	})

	msg := acc.message()
	require.Len(t, msg.ToolCalls, 2)
	assert.Equal(t, "c1", msg.ToolCalls[0].ID)
	assert.Equal(t, `{"path": "a"}`, msg.ToolCalls[0].Function.Arguments)
	assert.Equal(t, "c2", msg.ToolCalls[1].ID)
	assert.Equal(t, `{"path": "b"}`, msg.ToolCalls[1].Function.Arguments)
}

// Delta fragments alone keep the append semantics they always had.
func TestAccumulatorDeltaFragmentsStillAppend(t *testing.T) {
	acc := newStreamAccumulator()
	acc.applyDelta(llm.StreamDelta{ToolCalls: []llm.ToolCall{{
		Index:    0,
		ID:       "c1",
		Function: llm.Function{Name: "write", Arguments: `{"path"`},
	}}})
	acc.applyDelta(llm.StreamDelta{ToolCalls: []llm.ToolCall{{
		Index:    0,
		Function: llm.Function{Arguments: `: "a"}`},
	}}})

	msg := acc.message()
	require.Len(t, msg.ToolCalls, 1)
	assert.Equal(t, `{"path": "a"}`, msg.ToolCalls[0].Function.Arguments)
}
