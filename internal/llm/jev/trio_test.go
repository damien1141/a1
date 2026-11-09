package jev

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// oracle: round-trip through genuinely distinct code paths — the hosted
// TypeSafe client (client.go) and the Ollama native wire (ollama.go) must put
// the same {model, state, questions} core on the wire for the same questions,
// and must map the same answers payload back to identical Response values.
// Only transport-level concerns (auth, extra body) may differ between them.
func TestSystemOne_HostedVsOllamaNativeCoreBody_Differential(t *testing.T) {
	const sharedModel = "shared-model"
	questions := Questions{
		"billing": Noul{
			Instructions: "Is this about billing?",
			Criteria:     &NoulCriteria{True: "a charge", False: "anything else"},
		},
		"tone": Choice{Criteria: map[string]any{"calm": nil, "angry": "a hostile message"}},
		"urgency": Score{
			Instructions: "How urgent?",
			Criteria:     []any{"Can wait", "Today"},
		},
	}
	const answers = `{"model":"m","usage":{"input_tokens":3,"output_tokens":4},` +
		`"answers":{"billing":{"type":"noul","noul":0.9},` +
		`"tone":{"type":"choice","choice":"calm","confidence":0.8,"probabilities":{"angry":0.2,"calm":0.8}},` +
		`"urgency":{"type":"score","score":1,"confidence":0.7,"legend":{"0":"Can wait","1":"Today"},` +
		`"probabilities":{"0":0.4,"1":0.6}}}}`

	capture := func(sink *map[string]any) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, systemOnePath, r.URL.Path)
			require.NoError(t, json.NewDecoder(r.Body).Decode(sink))
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, answers)
		}
	}

	var hostedBody, ollamaBody map[string]any
	hostedSrv := httptest.NewServer(capture(&hostedBody))
	defer hostedSrv.Close()
	ollamaSrv := httptest.NewServer(capture(&ollamaBody))
	defer ollamaSrv.Close()

	clearEnv(t)
	hosted, err := NewClient(Config{
		APIKey:     "k",
		BaseURL:    hostedSrv.URL,
		Model:      sharedModel,
		HTTPClient: hostedSrv.Client(),
	})
	require.NoError(t, err)
	state := map[string]any{"subject": "Duplicate charge"}
	hostedResp, err := hosted.SystemOne(t.Context(), state, questions)
	require.NoError(t, err)

	ollama := NewOllamaClient(OllamaConfig{
		BaseURL:    ollamaSrv.URL,
		Model:      sharedModel,
		HTTPClient: ollamaSrv.Client(),
	})
	ollamaResp, err := ollama.SystemOneWithOptions(t.Context(), state, questions, Options{})
	require.NoError(t, err)

	require.NotNil(t, hostedBody, "hosted wire must have captured a body")
	require.NotNil(t, ollamaBody, "ollama wire must have captured a body")
	assert.Equal(t, hostedBody["model"], ollamaBody["model"], "both wires send the same model")
	assert.Equal(t, hostedBody["state"], ollamaBody["state"], "both wires send the same state")
	assert.Equal(t, hostedBody["questions"], ollamaBody["questions"], "both wires send the same encoded questions")

	assert.Equal(t, hostedResp.Model, ollamaResp.Model, "both wires map the same model back")
	assert.Equal(t, hostedResp.Usage, ollamaResp.Usage, "both wires map usage identically")
	assert.Equal(t, hostedResp.Answers, ollamaResp.Answers, "both wires map the answers payload identically")
}

// naiveParseOllamaAnswers re-derives the chat-wire answers with generic
// encoding/json decodes and hand-rolled range/membership checks, mirroring the
// documented decision contract instead of ollamaReplyAnswer's typed pointers.
func naiveParseOllamaAnswers(t *testing.T, content string, encoded map[string]any) (map[string]Answer, error) {
	t.Helper()
	text := strings.TrimSpace(content)
	if strings.HasPrefix(text, "```") {
		if _, rest, ok := strings.Cut(text, "\n"); ok {
			text = rest
		}
		if end := strings.LastIndex(text, "```"); end >= 0 {
			text = text[:end]
		}
		text = strings.TrimSpace(text)
	}
	var reply map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		return nil, err
	}
	answers := make(map[string]Answer, len(encoded))
	for name, question := range encoded {
		asked, _ := question.(map[string]any)
		want, _ := asked["type"].(string)
		raw, ok := reply[name]
		if !ok {
			return nil, fmt.Errorf("reply left question %q unanswered", name)
		}
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err != nil {
			return nil, err
		}
		answer := Answer{Type: want}
		switch want {
		case AnswerNoul:
			v, present := obj["noul"]
			if !present || v == nil {
				return nil, fmt.Errorf("answer for %q has no noul", name)
			}
			f, _ := v.(float64)
			if f < 0 || f > 1 {
				return nil, fmt.Errorf("answer for %q: noul out of range", name)
			}
			answer.Noul = f
			if c, ok := obj["confidence"].(float64); ok {
				answer.Confidence = c
			}
		case AnswerChoice:
			choice, ok := obj["choice"].(string)
			if !ok || choice == "" {
				return nil, fmt.Errorf("answer for %q has no choice", name)
			}
			conf, ok := obj["confidence"].(float64)
			if !ok {
				return nil, fmt.Errorf("answer for %q has no confidence", name)
			}
			criteria, _ := asked["criteria"].(map[string]any)
			if _, named := criteria[choice]; !named {
				return nil, fmt.Errorf("answer for %q: choice not among alternatives", name)
			}
			if conf < 0 || conf > 1 {
				return nil, fmt.Errorf("answer for %q: confidence out of range", name)
			}
			answer.Choice, answer.Confidence = choice, conf
		case AnswerScore:
			v, present := obj["score"]
			if !present || v == nil {
				return nil, fmt.Errorf("answer for %q has no score", name)
			}
			score, _ := v.(float64)
			conf, ok := obj["confidence"].(float64)
			if !ok {
				return nil, fmt.Errorf("answer for %q has no confidence", name)
			}
			criteria, _ := asked["criteria"].([]any)
			if score < 0 || score > float64(len(criteria)-1) {
				return nil, fmt.Errorf("answer for %q: score out of rubric", name)
			}
			if score != float64(int(score)) {
				return nil, fmt.Errorf("answer for %q: fractional score", name)
			}
			if conf < 0 || conf > 1 {
				return nil, fmt.Errorf("answer for %q: confidence out of range", name)
			}
			answer.Score, answer.Confidence = score, conf
		default:
			return nil, fmt.Errorf("answer for %q: unknown type", name)
		}
		answers[name] = answer
	}
	return answers, nil
}

// oracle: naive reference implementation in this test — every reply corpus is
// parsed twice, once by parseOllamaAnswers's typed pointers and once by a
// generic map decode with hand-rolled contract checks; values must agree, and
// both must reject the same malformed replies.
func TestParseOllamaAnswers_VsNaiveMapDecode_Differential(t *testing.T) {
	encoded := map[string]any{
		"risk":   map[string]any{"type": "noul"},
		"engine": map[string]any{"type": "choice", "criteria": map[string]any{"mdn": nil, "w3c": "the standards body"}},
		"grade":  map[string]any{"type": "score", "criteria": []any{"weak", "ok", "strong"}},
	}

	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "plain valid",
			content: `{"risk":{"noul":0.8,"confidence":0.7},"engine":{"choice":"mdn","confidence":0.9},"grade":{"score":2,"confidence":0.85}}`,
		},
		{
			name:    "fence wrapped",
			content: "```json\n{\"risk\":{\"noul\":0.4,\"confidence\":0.6},\"engine\":{\"choice\":\"w3c\",\"confidence\":0.75},\"grade\":{\"score\":0,\"confidence\":0.5}}\n```",
		},
		{
			name:    "noul without confidence",
			content: `{"risk":{"noul":0.31},"engine":{"choice":"mdn","confidence":0.9},"grade":{"score":1,"confidence":0.6}}`,
		},
		{
			name:    "unknown answer keys ignored",
			content: `{"risk":{"noul":0.5,"confidence":0.6,"why":"because"},"engine":{"choice":"mdn","confidence":0.9,"verbose":true},"grade":{"score":2,"confidence":0.7}}`,
		},
		{
			name:    "null noul is missing",
			content: `{"risk":{"noul":null},"engine":{"choice":"mdn","confidence":0.9},"grade":{"score":2,"confidence":0.7}}`,
		},
		{
			name:    "fractional score",
			content: `{"risk":{"noul":0.5,"confidence":0.6},"engine":{"choice":"mdn","confidence":0.9},"grade":{"score":1.5,"confidence":0.7}}`,
		},
		{
			name:    "choice outside criteria",
			content: `{"risk":{"noul":0.5,"confidence":0.6},"engine":{"choice":"what","confidence":0.9},"grade":{"score":2,"confidence":0.7}}`,
		},
		{
			name:    "question left unanswered",
			content: `{"risk":{"noul":0.5,"confidence":0.6},"engine":{"choice":"mdn","confidence":0.9}}`,
		},
		{
			name:    "answer is not an object",
			content: `{"risk":5,"engine":{"choice":"mdn","confidence":0.9},"grade":{"score":2,"confidence":0.7}}`,
		},
		{
			name:    "reply is not json",
			content: `nope`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, wantErr := naiveParseOllamaAnswers(t, tc.content, encoded)
			got, gotErr := parseOllamaAnswers(tc.content, encoded)
			if wantErr != nil {
				require.Error(t, gotErr, "the typed parse must reject what the naive decode rejects")
				return
			}
			require.NoError(t, gotErr, "the typed parse must accept what the naive decode accepts")
			assert.Equal(t, want, got, "both parses must derive the same answers")
		})
	}
}

// oracle: naive reference implementation in this test — the decision prompt is
// rebuilt from scratch with plain string building and json.MarshalIndent, so
// the STATE/QUESTIONS framing and the sorted question order are checked
// independently of ollamaDecisionPrompt.
func TestOllamaDecisionPrompt_VsNaiveReference_Differential(t *testing.T) {
	encoded := map[string]any{
		"risk":   map[string]any{"type": "noul"},
		"engine": map[string]any{"type": "choice", "criteria": map[string]any{"mdn": nil}},
		"grade":  map[string]any{"type": "score", "criteria": []any{"weak", "ok"}},
	}
	names := make([]string, 0, len(encoded))
	for name := range encoded {
		names = append(names, name)
	}
	sort.Strings(names)
	require.Equal(t, []string{"engine", "grade", "risk"}, names, "fixture sanity: sorted question names")

	for _, state := range []any{
		map[string]any{"topic": "css flexbox", "tags": []any{"ui", "layout"}},
		"plain text state",
	} {
		var want strings.Builder
		want.WriteString("STATE:\n")
		stateJSON, err := json.MarshalIndent(state, "", "  ")
		require.NoError(t, err)
		want.Write(stateJSON)
		want.WriteString("\n\nQUESTIONS:\n")
		for _, name := range names {
			want.WriteString(strconv.Quote(name))
			want.WriteString(": ")
			questionJSON, err := json.MarshalIndent(encoded[name], "", "  ")
			require.NoError(t, err)
			want.Write(questionJSON)
			want.WriteString("\n")
		}

		assert.Equal(t, want.String(), ollamaDecisionPrompt(state, encoded), "prompt for state %v", state)
	}
}
