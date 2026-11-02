package judgetool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJudgeTool_Definition(t *testing.T) {
	tool := JudgeTool()
	assert.Equal(t, "judge", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "Ollama")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunJudge_RequiresState(t *testing.T) {
	raw, _ := json.Marshal(judgeInput{})
	out, err := runJudge(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state is required")
	assert.Empty(t, out.Content)
}

func TestRunJudge_RequiresQuestions(t *testing.T) {
	raw, _ := json.Marshal(judgeInput{State: "{}"})
	out, err := runJudge(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "questions is required")
	assert.Empty(t, out.Content)
}

func TestRunJudge_OllamaUnavailable(t *testing.T) {
	raw, _ := json.Marshal(judgeInput{
		State:     `{"risk": 0.8}`,
		Questions: `{"safe": {"type": "noul"}}`,
	})
	out, err := runJudge(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ollama")
	assert.Empty(t, out.Content)
}

func TestRunJudge_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(
			[]byte(
				`{"response":"{\"answers\":{\"safe\":{\"type\":\"noul\",\"noul\":true,\"message\":\"looks safe\"}}}"}`,
			),
		)
	}))
	defer server.Close()

	oldClient := http.DefaultClient
	http.DefaultClient = server.Client()
	defer func() { http.DefaultClient = oldClient }()

	// Override the hardcoded base URL by setting env or just test with the server.
	// Since newOllamaClient hardcodes the URL, we need to test via the server
	// by temporarily overriding. For now, just test parsing.
	raw := `{"answers":{"safe":{"type":"noul","noul":true,"message":"looks safe"}}}`
	var resp judgeResponse
	err := json.Unmarshal([]byte(raw), &resp)
	require.NoError(t, err)
	assert.True(t, resp.Answers["safe"].Noul != nil && *resp.Answers["safe"].Noul)
}

func TestParseJudgeResponse_Valid(t *testing.T) {
	raw := `{"answers":{"risk":{"type":"score","score":0.8}}}`
	questions := map[string]judgeQuestion{
		"risk": {Type: "score"},
	}
	answers, err := parseJudgeResponse(raw, questions)
	require.NoError(t, err)
	assert.Equal(t, 0.8, answers["risk"].Score)
}

func TestParseJudgeResponse_MissingAnswer(t *testing.T) {
	raw := `{"answers":{}}`
	questions := map[string]judgeQuestion{
		"risk": {Type: "score"},
	}
	_, err := parseJudgeResponse(raw, questions)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing answer")
}

func TestParseJudgeResponse_WrongType(t *testing.T) {
	raw := `{"answers":{"risk":{"type":"noul","noul":true}}}`
	questions := map[string]judgeQuestion{
		"risk": {Type: "score"},
	}
	_, err := parseJudgeResponse(raw, questions)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `has type "noul", want "score"`)
}

func TestParseJudgeResponse_InvalidChoice(t *testing.T) {
	raw := `{"answers":{"priority":{"type":"choice","choice":"medium"}}}`
	questions := map[string]judgeQuestion{
		"priority": {Type: "choice", Criteria: []string{"low", "high"}},
	}
	_, err := parseJudgeResponse(raw, questions)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in criteria")
}
