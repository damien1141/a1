package judgetool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/damien1141/a1/internal/llm/jev"
	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAsker scripts the decision model for tests: it records every
// (state, questions, options) it is asked about and answers from the canned
// response or error. The tool's routing — parsing, globbing, fan-out,
// rendering — is what these tests exercise, not the network.
type fakeAsker struct {
	mu        sync.Mutex
	states    []any
	questions []jev.Questions
	options   []jev.Options
	resp      jev.Response
	err       error
}

func (f *fakeAsker) SystemOneWithOptions(
	_ context.Context,
	state any,
	questions jev.Questions,
	opts jev.Options,
) (jev.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, state)
	f.questions = append(f.questions, questions)
	f.options = append(f.options, opts)
	if f.err != nil {
		return jev.Response{}, f.err
	}
	return f.resp, nil
}

func (f *fakeAsker) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.states)
}

// testRunner wraps a scripted asker with a short timeout and a stable source
// label, so runJudge tests never touch process-wide configuration.
func testRunner(a jev.Asker) *judgeRunner {
	return &judgeRunner{asker: a, timeout: time.Second, source: "test"}
}

func TestJudgeTool_Definition(t *testing.T) {
	tool := JudgeTool()
	assert.Equal(t, "judge", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "JEV")
	assert.Contains(t, tool.Definition.Description, "questions")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
	assert.NotNil(t, tool.Definition.Params)
	assert.Contains(t, tool.Definition.Params.Required, "questions")
}

func TestRunJudge_NilRunnerFailsClosed(t *testing.T) {
	raw, _ := json.Marshal(judgeInput{
		Questions: `{"todo": {"type": "noul"}}`,
		State:     "hello",
	})
	out, err := runJudge(nil, t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no decision model configured")
	assert.Empty(t, out.Content)
}

func TestRunJudge_RequiresQuestions(t *testing.T) {
	raw, _ := json.Marshal(judgeInput{State: "hello"})
	out, err := runJudge(testRunner(&fakeAsker{}), t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "questions is required")
	assert.Empty(t, out.Content)
}

func TestRunJudge_RequiresStateOrFiles(t *testing.T) {
	raw, _ := json.Marshal(judgeInput{Questions: `{"todo": {"type": "noul"}}`})
	out, err := runJudge(testRunner(&fakeAsker{}), t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provide either state")
	assert.Empty(t, out.Content)
}

func TestRunJudge_BadArgumentsJSON(t *testing.T) {
	out, err := runJudge(testRunner(&fakeAsker{}), t.Context(), json.RawMessage(`{not json`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse judge arguments")
	assert.Empty(t, out.Content)
}

func TestRunJudge_StateSuccess(t *testing.T) {
	asker := &fakeAsker{resp: jev.Response{
		Answers: map[string]jev.Answer{
			"todo":  {Type: jev.AnswerNoul, Noul: 0.91, Confidence: 0.8},
			"layer": {Type: jev.AnswerChoice, Choice: "api", Confidence: 0.7},
		},
	}}
	raw, _ := json.Marshal(judgeInput{
		Questions: `{"todo": {"type": "noul"}, "layer": {"type": "choice", "criteria": ["api", "ui"]}}`,
		State:     `{"risk": 0.8}`,
	})
	out, err := runJudge(testRunner(asker), t.Context(), raw)
	require.NoError(t, err)

	assert.Contains(t, out.Content, "2 answers via test")
	assert.Contains(t, out.Content, "todo: yes (p 0.91, conf 0.80)")
	assert.Contains(t, out.Content, "layer: api")
	assert.Equal(t, "judge", out.Detail)
	assert.Equal(t, "2 answers via test", out.Summary)
	assert.Equal(t, out.Content, out.Output)

	// A JSON state stays structured on the wire: the decision model reads
	// the object, not a string quoting one.
	require.Len(t, asker.states, 1)
	state, ok := asker.states[0].(map[string]any)
	require.True(t, ok, "structured state should decode to an object")
	assert.InDelta(t, 0.8, state["risk"], 1e-9)
}

func TestRunJudge_StatePlainText(t *testing.T) {
	asker := &fakeAsker{resp: jev.Response{
		Answers: map[string]jev.Answer{
			"todo": {Type: jev.AnswerNoul, Noul: 0.1},
		},
	}}
	raw, _ := json.Marshal(judgeInput{
		Questions: `{"todo": {"type": "noul"}}`,
		State:     "plain text, not JSON",
	})
	out, err := runJudge(testRunner(asker), t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "todo: no")
	require.Len(t, asker.states, 1)
	assert.Equal(t, "plain text, not JSON", asker.states[0])
}

func TestRunJudge_QuestionsAsQuotedJSON(t *testing.T) {
	asker := &fakeAsker{resp: jev.Response{
		Answers: map[string]jev.Answer{
			"todo": {Type: jev.AnswerNoul, Noul: 0.9},
		},
	}}
	// Models habitually quote the nested document; the tool accepts both.
	raw, _ := json.Marshal(judgeInput{
		Questions: `"{\"todo\": {\"type\": \"noul\"}}"`,
		State:     "hello",
	})
	_, err := runJudge(testRunner(asker), t.Context(), raw)
	require.NoError(t, err)
	require.Len(t, asker.questions, 1)
	assert.Contains(t, asker.questions[0], "todo")
}

func TestRunJudge_ModelOverride(t *testing.T) {
	asker := &fakeAsker{resp: jev.Response{
		Answers: map[string]jev.Answer{
			"todo": {Type: jev.AnswerNoul, Noul: 0.9},
		},
	}}
	raw, _ := json.Marshal(judgeInput{
		Questions: `{"todo": {"type": "noul"}}`,
		State:     "hello",
		Model:     "jev-mini",
	})
	_, err := runJudge(testRunner(asker), t.Context(), raw)
	require.NoError(t, err)
	require.Len(t, asker.options, 1)
	assert.Equal(t, "jev-mini", asker.options[0].Model)
}

func TestRunJudge_StateErrorIsJudgedError(t *testing.T) {
	asker := &fakeAsker{err: errors.New("provider exploded")}
	raw, _ := json.Marshal(judgeInput{
		Questions: `{"todo": {"type": "noul"}}`,
		State:     "hello",
	})
	out, err := runJudge(testRunner(asker), t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "judge: provider exploded")
	assert.Empty(t, out.Content)
}

func TestParseQuestions(t *testing.T) {
	t.Run("noul choice score", func(t *testing.T) {
		questions, err := parseQuestions(`{
                        "todo": {"type": "noul", "criteria": {"true": "mentions a TODO"}},
                        "layer": {"type": "choice", "criteria": ["api", "ui"]},
                        "quality": {"type": "score", "criteria": ["broken", "solid"]}
                }`)
		require.NoError(t, err)
		require.Len(t, questions, 3)
		noul, ok := questions["todo"].(jev.Noul)
		require.True(t, ok)
		assert.NotNil(t, noul.Criteria)
		assert.Equal(t, "mentions a TODO", noul.Criteria.True)
		choice, ok := questions["layer"].(jev.Choice)
		require.True(t, ok)
		assert.Len(t, choice.Criteria, 2)
		score, ok := questions["quality"].(jev.Score)
		require.True(t, ok)
		assert.Len(t, score.Criteria, 2)
	})

	t.Run("unknown type", func(t *testing.T) {
		_, err := parseQuestions(`{"q": {"type": "maybe"}}`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown type "maybe"`)
	})

	t.Run("choice without criteria", func(t *testing.T) {
		_, err := parseQuestions(`{"q": {"type": "choice"}}`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `choice requires "criteria"`)
	})

	t.Run("score without criteria", func(t *testing.T) {
		_, err := parseQuestions(`{"q": {"type": "score"}}`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `score requires "criteria"`)
	})

	t.Run("empty object", func(t *testing.T) {
		_, err := parseQuestions(`{}`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at least one question")
	})

	t.Run("not an object", func(t *testing.T) {
		_, err := parseQuestions(`noul please`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a JSON object")
	})

	t.Run("quoted JSON accepted", func(t *testing.T) {
		questions, err := parseQuestions(`"{\"todo\": {\"type\": \"noul\"}}"`)
		require.NoError(t, err)
		assert.Contains(t, questions, "todo")
	})
}

func TestRunJudge_FilesMode(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n// TODO: fix me\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.md"), []byte("# notes\nnothing pending\n"), 0o644))
	bin := make([]byte, 64)
	bin[10] = 0 // NUL inside the first 8k: the cheap binary sniff
	require.NoError(t, os.WriteFile(filepath.Join(dir, "img.dat"), bin, 0o644))

	asker := &fakeAsker{resp: jev.Response{
		Answers: map[string]jev.Answer{
			"todo": {Type: jev.AnswerNoul, Noul: 0.9, Confidence: 0.6},
		},
	}}
	raw, _ := json.Marshal(judgeInput{
		Questions: `{"todo": {"type": "noul"}}`,
		Files:     "*.go, *.md, *.dat",
	})
	ctx := tooldef.WithCwd(t.Context(), dir)
	out, err := runJudge(testRunner(asker), ctx, raw)
	require.NoError(t, err)

	// Header counts every judged path; the binary is judged-then-skipped.
	assert.Contains(t, out.Content, "3 files × 1 questions via test")
	assert.Contains(t, out.Content, "a.go")
	assert.Contains(t, out.Content, "todo: yes (p 0.90, conf 0.60)")
	assert.Contains(t, out.Content, "b.md")
	assert.Contains(t, out.Content, "img.dat")
	assert.Contains(t, out.Content, "skipped: binary file")
	assert.Equal(t, "judge *.go, *.md, *.dat", out.Detail)
	assert.Equal(t, "3 files · 1 questions · test", out.Summary)

	// Only the two text files reached the decision model.
	assert.Equal(t, 2, asker.calls(), "binary file must not be asked")
	paths := map[string]bool{}
	for _, state := range asker.states {
		m, ok := state.(map[string]any)
		require.True(t, ok, "file state should be an object with path and content")
		paths[filepath.Base(m["path"].(string))] = true
		assert.Contains(t, m, "content")
	}
	assert.True(t, paths["a.go"])
	assert.True(t, paths["b.md"])
	assert.False(t, paths["img.dat"])
}

func TestRunJudge_FilesLimitCapsFanOut(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("package x\n"), 0o644))
	}
	asker := &fakeAsker{resp: jev.Response{
		Answers: map[string]jev.Answer{
			"todo": {Type: jev.AnswerNoul, Noul: 0.1},
		},
	}}
	raw, _ := json.Marshal(judgeInput{
		Questions: `{"todo": {"type": "noul"}}`,
		Files:     "*.go",
		Limit:     2,
	})
	ctx := tooldef.WithCwd(t.Context(), dir)
	out, err := runJudge(testRunner(asker), ctx, raw)
	require.NoError(t, err)

	assert.Contains(t, out.Content, "2 files × 1 questions via test")
	assert.Contains(t, out.Content, "(matched 3 files; judged the first 2 — raise limit to see more)")
	assert.Equal(t, 2, asker.calls())
}

func TestRunJudge_NoFilesMatched(t *testing.T) {
	dir := t.TempDir()
	raw, _ := json.Marshal(judgeInput{
		Questions: `{"todo": {"type": "noul"}}`,
		Files:     "*.nomatch",
	})
	ctx := tooldef.WithCwd(t.Context(), dir)
	out, err := runJudge(testRunner(&fakeAsker{}), ctx, raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files matched")
	assert.Empty(t, out.Content)
}

func TestDetailFromArgs(t *testing.T) {
	tool := JudgeTool()
	detail := tool.DetailFromArgs(json.RawMessage(`{"files": "./internal/**/*.go"}`))
	assert.Equal(t, "judge ./internal/**/*.go", detail)
	detail = tool.DetailFromArgs(json.RawMessage(`{"state": "{\"risk\": 0.8}"}`))
	assert.Equal(t, `judge {"risk": 0.8}`, detail)
	detail = tool.DetailFromArgs(json.RawMessage(`{}`))
	assert.Equal(t, "judge", detail)
}
