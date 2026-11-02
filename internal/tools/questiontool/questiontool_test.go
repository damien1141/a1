package questiontool_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/tools"
	"github.com/damien1141/a1/internal/tools/questiontool"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// asker runs the tool with a scripted answer, mirroring how the controller
// supplies the TUI-backed handler through the turn context.
func runWith(t *testing.T, args string, fn tooldef.QuestionFunc) (string, error) {
	t.Helper()
	reg := tools.NewRegistry([]tools.Tool{questiontool.QuestionTool()})
	ctx := tooldef.WithQuestionFunc(context.Background(), fn)
	out, err := reg["ask_user"].Run(ctx, []byte(args))
	if err != nil {
		return "", err
	}
	return out.Content, nil
}

func TestAskUserReturnsChoice(t *testing.T) {
	var got tooldef.Question
	content, err := runWith(t, `{
		"header": "Approach",
		"question": "Pick a parser:",
		"options": [
			{"label": "csv-aware (Recommended)", "description": "Handles quoted commas."},
			{"label": "split on comma"}
		]
	}`, func(_ context.Context, q tooldef.Question) (tooldef.QuestionReply, error) {
		got = q
		return tooldef.QuestionReply{
			Choice:  "csv-aware (Recommended)",
			Choices: []string{"csv-aware (Recommended)"},
		}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "csv-aware (Recommended)", content)
	assert.Equal(t, "Approach", got.Header)
	assert.Equal(t, "Pick a parser:", got.Question)
	require.Len(t, got.Options, 2)
	assert.Equal(t, "csv-aware (Recommended)", got.Options[0].Label)
	assert.Equal(t, "Handles quoted commas.", got.Options[0].Description)
}

func TestAskUserRejectsTooFewOptions(t *testing.T) {
	_, err := runWith(t, `{"question":"?","options":[{"label":"only"}]}`, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 to 4 options")
	// The message must say why, not just that the count was wrong.
	assert.Contains(t, err.Error(), "decision you should make yourself")
}

func TestAskUserRejectsTooManyOptions(t *testing.T) {
	_, err := runWith(t, `{"question":"?","options":[
		{"label":"a"},{"label":"b"},{"label":"c"},{"label":"d"},{"label":"e"}]}`, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2 to 4 options")
}

func TestAskUserWithoutHandlerFailsClosed(t *testing.T) {
	reg := tools.NewRegistry([]tools.Tool{questiontool.QuestionTool()})
	_, err := reg["ask_user"].Run(context.Background(), []byte(`{
		"question":"?","options":[{"label":"a"},{"label":"b"}]}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no question handler",
		"a hang would look like a hung model; say what is wrong instead")
}

func TestAskUserCancelledTellsModelNotToReask(t *testing.T) {
	content, err := runWith(t, `{"question":"?","options":[{"label":"a"},{"label":"b"}]}`,
		func(context.Context, tooldef.Question) (tooldef.QuestionReply, error) {
			return tooldef.QuestionReply{Cancelled: true}, nil
		})
	require.NoError(t, err, "a cancel is an answer, not a tool error")
	assert.Contains(t, content, "Do not re-ask immediately")
}

func TestAskUserMultiSelectListsAll(t *testing.T) {
	content, err := runWith(t, `{
		"question":"Which?","multi_select":true,
		"options":[{"label":"a"},{"label":"b"}]}`,
		func(_ context.Context, q tooldef.Question) (tooldef.QuestionReply, error) {
			assert.True(t, q.MultiSelect, "the flag must reach the UI")
			return tooldef.QuestionReply{Choices: []string{"a", "b"}}, nil
		})
	require.NoError(t, err)
	assert.Contains(t, content, "a, b")
	assert.Contains(t, content, "selected 2")
}

func TestAskUserTruncatesLongHeader(t *testing.T) {
	var got tooldef.Question
	_, err := runWith(t, `{"header":"this header is far too long for a chip","question":"?",
		"options":[{"label":"a"},{"label":"b"}]}`,
		func(_ context.Context, q tooldef.Question) (tooldef.QuestionReply, error) {
			got = q
			return tooldef.QuestionReply{Choice: "a", Choices: []string{"a"}}, nil
		})
	require.NoError(t, err)
	assert.LessOrEqual(t, len([]rune(got.Header)), 12, "the chip has a hard width")
}

func TestAskUserDetailIsTheQuestion(t *testing.T) {
	reg := tools.NewRegistry([]tools.Tool{questiontool.QuestionTool()})
	detail := reg["ask_user"].DetailFromArgs([]byte(`{"question":"Pick a parser:",
		"options":[{"label":"a"},{"label":"b"}]}`))
	assert.Equal(t, "Pick a parser:", detail,
		"the transcript row's identity is the question, stable across the run")
}
