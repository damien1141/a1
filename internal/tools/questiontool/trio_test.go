package questiontool_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// ---------- differential ----------

// oracle: hand-written golden literal — the exact tooldef.Question the tool
// must hand the asker for this argument JSON, including the trimmed header,
// question, labels, and descriptions.
func TestAskUserQuestionPassedToAsker_Differential(t *testing.T) {
	var got tooldef.Question
	_, err := runWith(t, `{
		"header": "  DB choice  ",
		"question": "  Which driver?  ",
		"multi_select": true,
		"options": [
			{"label": "  sqlite (Recommended)  ", "description": "  Zero-config, embedded  "},
			{"label": "postgres", "description": ""}
		]
	}`, func(_ context.Context, q tooldef.Question) (tooldef.QuestionReply, error) {
		got = q
		return tooldef.QuestionReply{Choice: "sqlite (Recommended)", Choices: []string{"sqlite (Recommended)"}}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, tooldef.Question{
		Header:   "DB choice",
		Question: "Which driver?",
		Options: []tooldef.QuestionOption{
			{Label: "sqlite (Recommended)", Description: "Zero-config, embedded"},
			{Label: "postgres"},
		},
		MultiSelect: true,
	}, got)
}

// naiveHeader clips independently: trim surrounding space, then keep the
// first 11 runes plus an ellipsis once the 12-rune chip width is exceeded.
func naiveHeader(raw string) string {
	h := strings.TrimSpace(raw)
	r := []rune(h)
	if len(r) <= 12 {
		return h
	}
	return string(r[:11]) + "…"
}

// oracle: naive runes-based truncation reference written in this test — the
// chip limit is defined in runes, not bytes, so multibyte headers are where a
// byte-slicing bug would hide.
func TestAskUserHeaderTruncation_Differential(t *testing.T) {
	cases := []struct {
		name   string
		header string
	}{
		{"empty", ""},
		{"padded short", "  Approach  "},
		{"exactly twelve runes", "twelve-runes"},
		{"thirteen ascii runes", "thirteen-rune"},
		{"cjk under the limit", "構成設定その2"},
		{"cjk over the limit", "このヘッダーは長すぎますね"},
		{"emoji cluster", "🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got tooldef.Question
			args := fmt.Sprintf(`{"header":%q,"question":"?","options":[{"label":"a"},{"label":"b"}]}`, tc.header)
			_, err := runWith(t, args, func(_ context.Context, q tooldef.Question) (tooldef.QuestionReply, error) {
				got = q
				return tooldef.QuestionReply{Choice: "a", Choices: []string{"a"}}, nil
			})
			require.NoError(t, err)
			assert.Equal(t, naiveHeader(tc.header), got.Header)
		})
	}
}

// naiveAnswerBody formats the reply independently from the documented
// contract: the single choice, the comma-joined choices, the multi-select
// count suffix, and the two fixed no-answer messages.
func naiveAnswerBody(reply tooldef.QuestionReply, multiSelect bool) string {
	if reply.Cancelled {
		return "The user dismissed the question without answering. " +
			"Do not re-ask immediately; continue with your best judgment, or state plainly that you need the answer to proceed."
	}
	answer := reply.Choice
	if answer == "" && len(reply.Choices) > 0 {
		answer = reply.Choices[0]
	}
	if answer == "" {
		return "The user answered without choosing an option. " +
			"Treat it as an instruction to continue and note what you did."
	}
	if len(reply.Choices) > 1 {
		joined := strings.Join(reply.Choices, ", ")
		if multiSelect {
			return fmt.Sprintf("%s (selected %d)", joined, len(reply.Choices))
		}
		return joined
	}
	return answer
}

// oracle: naive reference implementation in this test — the reply-to-body
// formatting rebuilt from the documented rules and compared over single,
// multi, mismatched, empty, and cancelled replies.
func TestAskUserAnswerBody_Differential(t *testing.T) {
	cases := []struct {
		name  string
		multi bool
		reply tooldef.QuestionReply
	}{
		{"single choice with echo", false, tooldef.QuestionReply{Choice: "a", Choices: []string{"a"}}},
		{"choice without choices slice", false, tooldef.QuestionReply{Choice: "postgres"}},
		{"first choice when choice empty", false, tooldef.QuestionReply{Choices: []string{"sqlite"}}},
		{"multiple choices multi-select input", true, tooldef.QuestionReply{Choices: []string{"a", "b", "c"}}},
		{"multiple choices single-select input", false, tooldef.QuestionReply{Choices: []string{"a", "b"}}},
		{"choice overridden by multiple choices", false, tooldef.QuestionReply{Choice: "x", Choices: []string{"a", "b"}}},
		{"no answer at all", false, tooldef.QuestionReply{}},
		{"cancelled", false, tooldef.QuestionReply{Cancelled: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := `{"question":"?","options":[{"label":"a"},{"label":"b"}]}`
			if tc.multi {
				args = `{"question":"?","multi_select":true,"options":[{"label":"a"},{"label":"b"}]}`
			}
			content, err := runWith(t, args, func(context.Context, tooldef.Question) (tooldef.QuestionReply, error) {
				return tc.reply, nil
			})
			require.NoError(t, err)
			assert.Equal(t, naiveAnswerBody(tc.reply, tc.multi), content)
		})
	}
}
