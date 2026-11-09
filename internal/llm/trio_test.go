package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// refToolParamsJSON builds the tool-parameter schema the way a provider
// expects it, using a plain map and encoding/json — a separate code path from
// the struct tags MarshalToolParams uses.
func refToolParamsJSON(t *testing.T, params *FunctionParameters) string {
	t.Helper()
	m := map[string]any{
		"type":       params.Type,
		"properties": params.Properties,
	}
	if len(params.Required) > 0 {
		m["required"] = params.Required
	}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	return string(b)
}

// oracle: encoding/json equivalence — the struct-tagged marshal is compared
// field-for-field against a map-based encoding/json reference, and nil params
// fall back to the caller's empty literal.
func TestMarshalToolParamsVsMapReference_Differential(t *testing.T) {
	t.Run("full schema", func(t *testing.T) {
		params := &FunctionParameters{
			Type:       "object",
			Properties: Object{"path": Object{"type": "string"}, "mode": Object{"type": "string", "enum": []string{"r", "w"}}},
			Required:   []string{"path"},
		}
		got := MarshalToolParams(params, "{}")
		assert.JSONEq(t, refToolParamsJSON(t, params), string(got))
	})

	t.Run("nil properties and no required", func(t *testing.T) {
		params := &FunctionParameters{Type: "object"}
		got := MarshalToolParams(params, "{}")
		assert.JSONEq(t, refToolParamsJSON(t, params), string(got), "nil properties marshal as null, required is omitted")
	})

	t.Run("empty required slice is omitted like a missing one", func(t *testing.T) {
		params := &FunctionParameters{Type: "object", Properties: Object{}, Required: []string{}}
		assert.JSONEq(t, refToolParamsJSON(t, params), string(MarshalToolParams(params, "{}")))
	})

	t.Run("nil params use the provider empty literal", func(t *testing.T) {
		assert.Equal(t, `{}`, string(MarshalToolParams(nil, "{}")))
		assert.Equal(t, `{"type":"object"}`, string(MarshalToolParams(nil, `{"type":"object"}`)))
		assert.Equal(t, `{}`, string(MarshalToolParams(nil, "")), "an empty fallback defaults to {}")
	})
}

// oracle: hand-rolled SHA-256 composition — the documented pck_ recipe
// (sha256 over scope \x00 system \x00 name-sorted tools JSON, first 12 bytes
// hex) is recomputed here over a hand-written tools literal, pinning the
// separator bytes, the sort, and the truncation independently of the
// implementation's own marshaling.
func TestPromptCacheKeyComposition_Differential(t *testing.T) {
	// Hand-written JSON of the tools after name sorting: a first, b second.
	// parameters is null because Params is nil; readable is never serialized.
	const goldenTools = `[{"name":"a","description":"A tool","parameters":null},` +
		`{"name":"b","description":"B tool","parameters":null}]`
	sum := sha256.Sum256([]byte("scope-7\x00be brief\nstay safe\x00" + goldenTools))
	want := "pck_" + hex.EncodeToString(sum[:12])

	got := PromptCacheKey("scope-7", "be brief\nstay safe", []ToolDefinition{
		{Name: "b", Description: "B tool"},
		{Name: "a", Description: "A tool"},
	})
	assert.Equal(t, want, got, "the key is the documented hash of scope, system, and sorted tools")
	assert.Regexp(t, `^pck_[0-9a-f]{24}$`, got, "prefix plus 24 hex chars from the first 12 bytes")
}

// refRepairArguments re-derives the repair contract with encoding/json's
// Decoder instead of json.Valid: exactly one JSON document, decoded
// successfully, with nothing but whitespace after it.
func refRepairArguments(args string) string {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" || trimmed == "null" {
		return "{}"
	}
	dec := json.NewDecoder(strings.NewReader(trimmed))
	var v any
	if err := dec.Decode(&v); err != nil {
		return "{}"
	}
	if err := dec.Decode(&v); err != io.EOF {
		return "{}"
	}
	return args
}

// oracle: encoding/json equivalence via a distinct stdlib path — the
// json.Valid check is cross-examined with a Decoder-based one-document read
// over normal, edge, and hostile inputs.
func TestRepairToolCallArgumentsVsDecoder_Differential(t *testing.T) {
	corpus := []string{
		`{"path": "/tmp/x"}`,
		"  {\"a\": 1}  ",
		`"/etc/hosts"`,
		`42`,
		`true`,
		`[]`,
		`{}`,
		`{ }`,
		"{\n  \"a\": 1\n}",
		`{"nested":{"deep":[1,2,3]}}`,
		`{"a":"\u00e9"}`,
		`{"path": "/home/x/README.md`,
		`"/home/x`,
		`{"a":1}{"a":1}`,
		`12 34`,
		`{"a":1} oops`,
		`{"a":1} `,
		`{`,
		``,
		`   `,
		`null`,
		` null `,
		`nulll`,
		`nul`,
		`tru`,
		"\ufeff{}",
	}
	for _, args := range corpus {
		assert.Equal(t, refRepairArguments(args), RepairToolCallArguments(args), "input %q", args)
	}
}

// oracle: hand-written known-good JSON literal — the normalized Message wire
// shape, byte for byte. Usage never serializes, runtime only when true,
// tool_call_id only when set, and a zero tool-call index is omitted.
func TestMessageWireJSONGolden_Differential(t *testing.T) {
	t.Run("assistant turn with everything", func(t *testing.T) {
		msg := Message{
			Role:             RoleAssistant,
			Content:          "done",
			ReasoningContent: "because",
			ToolCalls: []ToolCall{
				{Index: 2, ID: "c1", Type: "function", Function: Function{Name: "read", Arguments: `{}`}},
				{ID: "z", Function: Function{Name: "grep"}},
			},
			Images:  []Image{{Data: "QQ==", MimeType: "image/png"}},
			Runtime: true,
			Usage:   Usage{TotalTokens: 99},
		}
		body, err := json.Marshal(msg)
		require.NoError(t, err)
		want := `{"role":"assistant","content":"done","reasoning_content":"because",` +
			`"tool_calls":[{"index":2,"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}},` +
			`{"id":"z","type":"","function":{"name":"grep","arguments":""}}],` +
			`"images":[{"data":"QQ==","mimeType":"image/png"}],"runtime":true}`
		assert.Equal(t, want, string(body)) //nolint:testifylint // byte-exact golden
	})

	t.Run("minimal user turn", func(t *testing.T) {
		body, err := json.Marshal(Message{Role: RoleUser, Content: "hi"})
		require.NoError(t, err)
		assert.Equal(t, `{"role":"user","content":"hi"}`, string(body)) //nolint:testifylint // byte-exact golden
	})

	t.Run("tool result turn", func(t *testing.T) {
		body, err := json.Marshal(Message{Role: RoleTool, Content: "ok", ToolCallID: "t9"})
		require.NoError(t, err)
		assert.Equal(t, `{"role":"tool","content":"ok","tool_call_id":"t9"}`,
			string(body)) //nolint:testifylint // byte-exact golden
	})

	t.Run("marshal round-trips through decode unchanged", func(t *testing.T) {
		msg := Message{
			Role:    RoleAssistant,
			Content: "done",
			ToolCalls: []ToolCall{{
				Index: 3, ID: "c1", Type: "function",
				Function: Function{Name: "read", Arguments: `{"path":"a"}`},
			}},
			Images: []Image{{Data: "QQ==", MimeType: "image/png"}},
		}
		first, err := json.Marshal(msg)
		require.NoError(t, err)
		var back Message
		require.NoError(t, json.Unmarshal(first, &back))
		second, err := json.Marshal(back)
		require.NoError(t, err)
		assert.Equal(t, string(first), string(second), "encode → decode → encode must be byte-stable")
	})
}

// oracle: round-trip through a genuinely distinct code path — the per-message
// byte sum is checked against whole-slice encoding/json arithmetic: the slice
// marshal is the per-message marshals plus n-1 commas and 2 brackets.
func TestRequestBytesVsWholeSliceMarshal_Differential(t *testing.T) {
	cases := [][]Message{
		nil,
		{},
		{{Role: RoleUser, Content: "hi"}},
		{
			{Role: RoleSystem, Content: "be terse"},
			{Role: RoleUser, Content: "look", Images: []Image{{Data: "QUJD", MimeType: "image/png"}}},
			{Role: RoleAssistant, Content: "done",
				ToolCalls: []ToolCall{{ID: "c1", Type: "function",
					Function: Function{Name: "read", Arguments: `{"path":"a"}`}}},
				Usage: Usage{TotalTokens: 7}},
			{Role: RoleTool, ToolCallID: "c1", Content: "ok", Runtime: true},
		},
	}
	for i, msgs := range cases {
		all, err := json.Marshal(msgs)
		require.NoError(t, err, "case %d", i)

		var ref int64
		if len(msgs) > 0 {
			ref = int64(len(all)) - int64(len(msgs)) - 1
		}
		assert.Equal(t, ref, RequestBytes(msgs), "case %d: sum of per-message marshals vs whole-slice arithmetic", i)
	}
}
