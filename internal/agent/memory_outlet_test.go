package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseConsolidationOps_ValidJSON verifies that a well-formed JSON
// response with a non-empty ops array parses correctly.
func TestParseConsolidationOps_ValidJSON(t *testing.T) {
	raw := `{"ops":[{"operation":"CREATE","id":"","content":"User prefers terse responses","kind":"preference","scope":"user"}]}`
	ops, err := parseConsolidationOps(raw)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, "CREATE", ops[0].Operation)
	assert.Equal(t, "User prefers terse responses", ops[0].Content)
	assert.Equal(t, "preference", ops[0].Kind)
	assert.Equal(t, "user", ops[0].Scope)
}

// TestParseConsolidationOps_MarkdownFenced verifies that JSON wrapped in
// ```json...``` fences (a common LLM output pattern) parses correctly.
// The fence-stripping logic must handle both ```json and bare ``` openers.
func TestParseConsolidationOps_MarkdownFenced(t *testing.T) {
	cases := []string{
		"```json\n{\"ops\":[{\"operation\":\"CREATE\",\"id\":\"\",\"content\":\"User is 19\",\"kind\":\"identity\",\"scope\":\"user\"}]}\n```",
		"```\n{\"ops\":[{\"operation\":\"CREATE\",\"id\":\"\",\"content\":\"User is 19\",\"kind\":\"identity\",\"scope\":\"user\"}]}\n```",
	}
	for i, raw := range cases {
		ops, err := parseConsolidationOps(raw)
		require.NoError(t, err, "case %d", i)
		require.Len(t, ops, 1, "case %d", i)
		assert.Equal(t, "CREATE", ops[0].Operation, "case %d", i)
		assert.Equal(t, "User is 19", ops[0].Content, "case %d", i)
	}
}

// TestParseConsolidationOps_PrefaceText verifies that preface text before
// the JSON object (e.g. "Here are the operations:") is stripped.
func TestParseConsolidationOps_PrefaceText(t *testing.T) {
	raw := `Here are the operations to perform:
{"ops":[{"operation":"DELETE","id":"mem-1","content":""}]}
Let me know if you need anything else.`
	ops, err := parseConsolidationOps(raw)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, "DELETE", ops[0].Operation)
	assert.Equal(t, "mem-1", ops[0].ID)
}

// TestParseConsolidationOps_InvalidJSON verifies that malformed JSON
// returns an error (the caller must skip consolidation on parse failure
// rather than persist garbage).
func TestParseConsolidationOps_InvalidJSON(t *testing.T) {
	cases := []string{
		"not json at all",
		"{ops:}",             // malformed
		`{"ops":"not an array"}`, // ops is wrong type
		`{`,                  // truncated
		`}`,                  // closing without opening
	}
	for _, raw := range cases {
		_, err := parseConsolidationOps(raw)
		assert.Error(t, err, "expected error for input: %q", raw)
	}
}

// TestParseConsolidationOps_EmptyOps verifies that {"ops":[]} returns an
// empty (but non-nil) slice with no error.
func TestParseConsolidationOps_EmptyOps(t *testing.T) {
	raw := `{"ops":[]}`
	ops, err := parseConsolidationOps(raw)
	require.NoError(t, err)
	assert.Empty(t, ops)
}

// TestParseConsolidationOps_MultipleOps verifies a multi-op response parses
// correctly with all fields preserved.
func TestParseConsolidationOps_MultipleOps(t *testing.T) {
	raw := `{"ops":[
		{"operation":"CREATE","id":"","content":"User prefers tabs","kind":"preference","scope":"user"},
		{"operation":"UPDATE","id":"mem-2","content":"Project uses Go 1.22 (upgraded today)","kind":"fact","scope":"project"},
		{"operation":"DELETE","id":"mem-3","content":""}
	]}`
	ops, err := parseConsolidationOps(raw)
	require.NoError(t, err)
	require.Len(t, ops, 3)
	assert.Equal(t, "CREATE", ops[0].Operation)
	assert.Equal(t, "UPDATE", ops[1].Operation)
	assert.Equal(t, "mem-2", ops[1].ID)
	assert.Equal(t, "DELETE", ops[2].Operation)
	assert.Equal(t, "mem-3", ops[2].ID)
}

// TestParseConsolidationOps_MissingFields verifies that ops with missing
// optional fields (kind, scope) parse — the struct uses zero values.
func TestParseConsolidationOps_MissingFields(t *testing.T) {
	raw := `{"ops":[{"operation":"CREATE","id":"","content":"Some fact"}]}`
	ops, err := parseConsolidationOps(raw)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, "CREATE", ops[0].Operation)
	assert.Equal(t, "", ops[0].Kind, "missing kind should default to empty string")
	assert.Equal(t, "", ops[0].Scope, "missing scope should default to empty string")
}

// TestParseConsolidationOps_WhitespaceOnly verifies that a whitespace-only
// response (LLM produced no JSON) returns an error.
func TestParseConsolidationOps_WhitespaceOnly(t *testing.T) {
	_, err := parseConsolidationOps("   \n\t  ")
	assert.Error(t, err, "whitespace-only input should return error")
}

// TestParseConsolidationOps_TrailingJsonInsideCodeFenceWithExtraText
// verifies the fence + preface combo (the most common LLM pattern).
func TestParseConsolidationOps_TrailingJsonInsideCodeFenceWithExtraText(t *testing.T) {
	raw := `Sure, here are the operations:

` + "```json" + `
{"ops":[{"operation":"CREATE","id":"","content":"User loves RISC-V","kind":"identity","scope":"user"}]}
` + "```" + `

Let me know if you need more!`
	ops, err := parseConsolidationOps(raw)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, "User loves RISC-V", ops[0].Content)
}
