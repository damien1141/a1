package gemini

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// ---------- differential ----------

// oracle: hand-written golden literal — the wire body BuildRequest produces for
// a full conversation (merged system text, images, tool calls, consecutive tool
// results folded into one user turn) is compared against JSON written by hand
// from the Gemini REST shapes, so field names, nesting, and the merge rules are
// pinned independently of the builder.
func TestBuildRequest_WireBodyVsGoldenLiteral_Differential(t *testing.T) {
	tools := []llm.ToolDefinition{{
		Name:        "bash",
		Description: "run a shell command",
		Params: &llm.FunctionParameters{
			Type:       "object",
			Properties: llm.Object{"cmd": map[string]any{"type": "string"}},
			Required:   []string{"cmd"},
		},
	}}
	messages := []llm.Message{
		{Role: llm.RoleUser, Content: "hi", Images: []llm.Image{{MimeType: "image/png", Data: "aGk="}}},
		{Role: llm.RoleAssistant, Content: "sure", ToolCalls: []llm.ToolCall{{
			ID:       "call_1",
			Type:     "function",
			Function: llm.Function{Name: "bash", Arguments: `{"cmd":"ls -la","all":true}`},
		}}},
		{Role: llm.RoleTool, ToolCallID: "bash", Content: "file one\nfile two"},
		{Role: llm.RoleTool, ToolCallID: "grep", Content: "no match"},
		{Role: llm.RoleSystem, Content: "  session memory  "},
		{Role: llm.RoleUser, Content: "go on"},
	}

	body, err := json.Marshal(BuildRequest("You are terse.", messages, tools))
	require.NoError(t, err)

	const golden = `{
		"systemInstruction": {"parts": [{"text": "You are terse.\n\nsession memory"}]},
		"contents": [
			{"role": "user", "parts": [{"text": "hi"}, {"inlineData": {"mimeType": "image/png", "data": "aGk="}}]},
			{"role": "model", "parts": [{"text": "sure"}, {"functionCall": {"name": "bash", "args": {"all": true, "cmd": "ls -la"}}}]},
			{"role": "user", "parts": [
				{"functionResponse": {"name": "bash", "response": {"output": "file one\nfile two"}}},
				{"functionResponse": {"name": "grep", "response": {"output": "no match"}}}
			]},
			{"role": "user", "parts": [{"text": "go on"}]}
		],
		"tools": [{"functionDeclarations": [{
			"name": "bash",
			"description": "run a shell command",
			"parameters": {"type": "object", "properties": {"cmd": {"type": "string"}}, "required": ["cmd"]}
		}]}]
	}`
	assert.JSONEq(t, golden, string(body), "wire body must equal the hand-written Gemini shape")
}

type naiveGeminiTurn struct {
	text      string
	reasoning string
	toolCalls []llm.ToolCall
	usage     llm.Usage
}

// naiveGeminiStream re-derives the final turn from an SSE corpus with the
// standard library alone: lines are split by hand, each payload decodes into a
// generic map, and text/thought/functionCall parts plus usage are accumulated
// with plain loops. Deliberately independent of processStream's typed structs.
func naiveGeminiStream(t *testing.T, sse string) naiveGeminiTurn {
	t.Helper()
	var turn naiveGeminiTurn
	for _, line := range strings.Split(sse, "\n") {
		payload, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		payload = strings.TrimSpace(payload)
		if payload == "" {
			continue
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // same tolerance as the provider: junk lines are skipped
		}
		if u, ok := chunk["usageMetadata"].(map[string]any); ok {
			num := func(key string) int {
				f, _ := u[key].(float64)
				return int(f)
			}
			if num("totalTokenCount") > 0 || num("promptTokenCount") > 0 {
				cached := num("cachedContentTokenCount")
				turn.usage = llm.Usage{
					PromptTokens:     max(num("promptTokenCount")-cached, 0),
					CompletionTokens: num("candidatesTokenCount") + num("thoughtsTokenCount"),
					TotalTokens:      num("totalTokenCount"),
				}
				if cached > 0 {
					turn.usage.PromptTokensDetails = &llm.PromptTokensDetails{CachedTokens: cached}
				}
			}
		}
		candidates, _ := chunk["candidates"].([]any)
		for _, cand := range candidates {
			c, _ := cand.(map[string]any)
			content, _ := c["content"].(map[string]any)
			parts, _ := content["parts"].([]any)
			for _, raw := range parts {
				p, _ := raw.(map[string]any)
				text, _ := p["text"].(string)
				thought, _ := p["thought"].(bool)
				if thought && text != "" {
					turn.reasoning += text
					continue
				}
				if text != "" {
					turn.text += text
				}
				fc, _ := p["functionCall"].(map[string]any)
				if fc == nil {
					continue
				}
				name, _ := fc["name"].(string)
				if name == "" {
					continue
				}
				args, _ := json.Marshal(fc["args"])
				turn.toolCalls = append(turn.toolCalls, llm.ToolCall{
					Index:    len(turn.toolCalls),
					ID:       name,
					Type:     "function",
					Function: llm.Function{Name: name, Arguments: string(args)},
				})
			}
		}
	}
	return turn
}

// oracle: naive reference implementation in this test — the SSE corpus is
// re-decoded with strings.Split + encoding/json into generic maps, and the
// accumulated text, reasoning, tool calls, and usage buckets are compared
// against processStream's typed-struct parse of the same corpus.
func TestProcessStream_VsNaiveJSONDecode_Differential(t *testing.T) {
	corpus := strings.Join([]string{
		`data: {"candidates":[{"content":{"parts":[{"text":"Hel"}]}}]}`,
		``,
		`event: ping`,
		`data:`,
		`data: {"candidates":[{"content":{"parts":[{"thought":true,"text":"pondering"}]}},{"content":{"parts":[{"text":"lo"}]}}]}`,
		``,
		`data: not json at all`,
		``,
		`data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"bash","args":{"cmd":"ls"}}}]}}]}`,
		``,
		`data: {"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":7,"cachedContentTokenCount":5,"thoughtsTokenCount":3,"totalTokenCount":22}}`,
		``,
		`data: {"candidates":[{"content":{"parts":[{"text":"!","functionCall":{"name":"grep","args":{"q":"x"}}}]},"finishReason":"STOP"}]}`,
		``,
		`data: {"candidates":[{"content":{"parts":[{"thought":true}]}}]}`,
		``,
	}, "\n")

	want := naiveGeminiStream(t, corpus)
	require.Equal(t, "Hello!", want.text, "naive reference sanity: accumulated text")
	require.Equal(t, "pondering", want.reasoning, "naive reference sanity: reasoning")
	require.Len(t, want.toolCalls, 2, "naive reference sanity: tool calls")
	require.Equal(t, 7, want.usage.PromptTokens, "naive reference sanity: uncached prompt tokens")

	var (
		deltaText      strings.Builder
		deltaReasoning strings.Builder
		toolDeltas     int
		done           *llm.StreamEvent
	)
	for _, ev := range processForTest(corpus) {
		require.NotEqual(t, llm.StreamEventTypeError, ev.Type, "stream error: %s", ev.Err)
		switch ev.Type {
		case llm.StreamEventTypeDelta:
			deltaText.WriteString(ev.Delta.Content)
			deltaReasoning.WriteString(ev.Delta.ReasoningContent)
			if len(ev.Delta.ToolCalls) > 0 {
				toolDeltas++
			}
		case llm.StreamEventTypeDone:
			done = &ev
		}
	}
	require.NotNil(t, done, "stream must end with a done event")
	final := done.Final
	require.NotNil(t, final)

	assert.Equal(t, want.text, final.Content, "final text must equal the naive decode")
	assert.Equal(t, want.reasoning, final.ReasoningContent, "final reasoning must equal the naive decode")
	assert.Equal(t, want.toolCalls, final.ToolCalls, "final tool calls must equal the naive decode")
	assert.Equal(t, want.usage, final.Usage, "usage buckets must equal the naive decode")
	assert.Equal(t, want.text, deltaText.String(), "content deltas must concatenate to the same text")
	assert.Equal(t, want.reasoning, deltaReasoning.String(), "reasoning deltas must concatenate to the same text")
	assert.Equal(t, len(want.toolCalls), toolDeltas, "one tool-call delta per functionCall part")
}
