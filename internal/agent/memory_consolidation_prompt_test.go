package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The consolidation template is go:embed-ed RAW (memory_outlet.go), never
// executed through text/template. It was nevertheless written with
// text/template escaping — every JSON example carried doubled braces — so the
// model was instructed to emit {{"ops":...}}, which json.Unmarshal rejects at
// the first '{'. The outlet then logged "parse failed" on every turn and
// harvested nothing: memory looked wired up and silently stayed empty.
//
// These pins assert the contract instead of the current text: every JSON
// example the prompt shows the model must be parseable by the parser that
// consumes the model's reply.
func TestConsolidationPrompt_EmitsParseableJSON_Negative(t *testing.T) {
	prompt := memoryConsolidationPrompt()

	// 1. No text/template escaping survives into the embedded text. Doubled
	//    braces are the smell: they mean the author expected a rendering pass
	//    that this template never gets.
	assert.NotContains(t, prompt, `{{`,
		"raw-embedded template must not carry text/template escaping")
	assert.NotContains(t, prompt, `}}`,
		"raw-embedded template must not carry text/template escaping")
}

// TestConsolidationPrompt_ExamplesParse_Positive is the differential: every
// JSON object the prompt tells the model to produce is fed back through the
// real parser. This is the oracle that would have caught the doubled braces
// on landing day — prompt text vs. the consumer of that text, not prompt text
// vs. itself.
func TestConsolidationPrompt_ExamplesParse_Positive(t *testing.T) {
	prompt := memoryConsolidationPrompt()

	// Extract every JSON object the prompt actually shows and feed each back
	// through the parser. Taking the examples FROM the prompt (rather than
	// transcribing them here) is what makes this a real oracle: the test
	// cannot drift from the text the model is handed.
	examples := promptJSONExamples(prompt)
	require.NotEmpty(t, examples, "prompt must show at least one JSON example")

	for _, example := range examples {
		_, err := parseConsolidationOps(example)
		assert.NoError(t, err, "the parser must accept a shape the prompt demands: %s", example)
	}
}

// promptJSONExamples pulls the JSON objects out of the consolidation prompt.
// Each example sits on its own line and runs to end-of-line, so the line
// slice starting at the first '{' is the whole object.
func promptJSONExamples(prompt string) []string {
	var out []string
	for _, line := range strings.Split(prompt, "\n") {
		idx := strings.Index(line, `{"ops"`)
		if idx < 0 {
			continue
		}
		if obj := strings.TrimSpace(line[idx:]); obj != "" {
			out = append(out, obj)
		}
	}
	return out
}

// TestConsolidationPrompt_RealReplyShapes_Differential pins the parser against
// the reply shapes models actually send — bare JSON, fenced JSON, and JSON
// with a prose preamble — so the prompt's "return strictly valid JSON"
// instruction is backed by a parser that tolerates the usual drift.
func TestConsolidationPrompt_RealReplyShapes_Differential(t *testing.T) {
	// oracle: the ops a compliant model would send for one created fact.
	const wantContent = "User prefers tabs over spaces"

	cases := map[string]string{
		"bare":     `{"ops":[{"operation":"CREATE","content":"` + wantContent + `","kind":"preference","scope":"user"}]}`,
		"fenced":   "```json\n{\"ops\":[{\"operation\":\"CREATE\",\"content\":\"" + wantContent + "\",\"kind\":\"preference\",\"scope\":\"user\"}]}\n```",
		"preamble": "Here are the operations:\n{\"ops\":[{\"operation\":\"CREATE\",\"content\":\"" + wantContent + "\",\"kind\":\"preference\",\"scope\":\"user\"}]}",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			ops, err := parseConsolidationOps(raw)
			require.NoError(t, err)
			require.Len(t, ops, 1)
			assert.Equal(t, "CREATE", strings.ToUpper(ops[0].Operation))
			assert.Equal(t, wantContent, ops[0].Content)
		})
	}
}
