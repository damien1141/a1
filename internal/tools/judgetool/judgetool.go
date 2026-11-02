package judgetool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

const (
	judgeDefaultModel   = "llama3.2"
	judgeDefaultBaseURL = "http://127.0.0.1:11434"
)

var judgeDescription = `Local judgment and evaluation using Ollama.

Sends state and named questions to a local Ollama model and returns structured
answers. Supports noul (yes/no), choice, and score question types. No external
API calls; fully local.`

// JudgeTool returns the local judgment tool definition + handler.
func JudgeTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "judge",
			Description: judgeDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"state": llm.Object{
						"type":        "string",
						"description": "State to evaluate, as JSON string. Example: {\"risk\": 0.8, \"urgency\": \"high\"}",
					},
					"questions": llm.Object{
						"type":        "string",
						"description": "Questions JSON string. Example: {\"safe\": {\"type\": \"noul\"}, \"priority\": {\"type\": \"choice\", \"criteria\": [\"low\", \"medium\", \"high\"]}}",
					},
					"model": llm.Object{
						"type":        "string",
						"description": fmt.Sprintf("Ollama model to use. Example: %s (default)", judgeDefaultModel),
					},
				},
				Required: []string{"state", "questions"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in judgeInput
			_ = json.Unmarshal(input, &in)
			q := strings.TrimSpace(in.Questions)
			if q == "" {
				return "judge"
			}
			return fmt.Sprintf("judge %s", q)
		},
		Run: runJudge,
	}
}

type judgeInput struct {
	State     string `json:"state"`
	Questions string `json:"questions"`
	Model     string `json:"model"`
}

type judgeQuestion struct {
	Type      string   `json:"type"`
	Criteria  []string `json:"criteria,omitempty"`
	Threshold float64  `json:"threshold,omitempty"`
}

type judgeResponse struct {
	Answers map[string]judgeAnswer `json:"answers"`
}

type judgeAnswer struct {
	Type    string  `json:"type"`
	Noul    *bool   `json:"noul,omitempty"`
	Choice  string  `json:"choice,omitempty"`
	Score   float64 `json:"score,omitempty"`
	Message string  `json:"message,omitempty"`
}

func runJudge(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in judgeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse judge arguments: %w", err)
	}

	state := strings.TrimSpace(in.State)
	if state == "" {
		return tooldef.Result{}, fmt.Errorf("state is required: provide JSON to evaluate")
	}

	questionsRaw := strings.TrimSpace(in.Questions)
	if questionsRaw == "" {
		return tooldef.Result{}, fmt.Errorf("questions is required: provide JSON mapping question names to types")
	}

	var questions map[string]judgeQuestion
	if err := json.Unmarshal([]byte(questionsRaw), &questions); err != nil {
		return tooldef.Result{}, fmt.Errorf("invalid questions JSON: %w", err)
	}

	model := strings.TrimSpace(in.Model)
	if model == "" {
		model = judgeDefaultModel
	}

	baseURL := judgeDefaultBaseURL
	if proj := project.GetDefaultProject(); proj != nil {
		if cfg := proj.Config(); cfg != nil {
			if m := strings.TrimSpace(cfg.Judge.Model); m != "" {
				model = m
			}
			if u := strings.TrimSpace(cfg.Judge.OllamaBaseURL); u != "" {
				baseURL = u
			}
		}
	}

	client, err := newOllamaClient(baseURL)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("judge: ollama not available: %w", err)
	}

	prompt := buildJudgePrompt(state, questions)
	resp, err := client.generate(ctx, model, prompt)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("judge: ollama generation failed: %w", err)
	}

	answers, err := parseJudgeResponse(resp, questions)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("judge: parse response: %w", err)
	}

	content := renderJudgeResults(answers)
	detail := fmt.Sprintf("%d answers from %s", len(answers), model)
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func buildJudgePrompt(state string, questions map[string]judgeQuestion) string {
	var sb strings.Builder
	sb.WriteString("You are a strict local judge. Evaluate the provided state against each question.\n")
	sb.WriteString("Respond with ONLY a JSON object matching this schema:\n")
	sb.WriteString(`{"answers": {"<question_name>": {"type": "<noul|choice|score>", "value": ...}}}`)
	sb.WriteString("\n\n")
	sb.WriteString("State:\n")
	sb.WriteString(state)
	sb.WriteString("\n\nQuestions:\n")
	for name, q := range questions {
		sb.WriteString(fmt.Sprintf("- %s: type=%s", name, q.Type))
		if len(q.Criteria) > 0 {
			sb.WriteString(fmt.Sprintf(" criteria=%s", strings.Join(q.Criteria, ", ")))
		}
		if q.Threshold > 0 {
			sb.WriteString(fmt.Sprintf(" threshold=%g", q.Threshold))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nRespond with JSON only. No markdown, no explanation.")
	return sb.String()
}

func parseJudgeResponse(raw string, questions map[string]judgeQuestion) (map[string]judgeAnswer, error) {
	// Strip markdown code fences if present.
	text := raw
	if idx := strings.Index(text, "```"); idx >= 0 {
		text = text[idx:]
	}
	if idx := strings.LastIndex(text, "```"); idx > 0 {
		text = text[:idx]
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "json") {
		text = strings.TrimSpace(text[4:])
	}

	var resp judgeResponse
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		return nil, fmt.Errorf("response is not valid JSON: %w\nraw: %s", err, raw)
	}

	answers := make(map[string]judgeAnswer, len(resp.Answers))
	for name, q := range questions {
		ans, ok := resp.Answers[name]
		if !ok {
			return nil, fmt.Errorf("missing answer for question %q", name)
		}
		if ans.Type != q.Type {
			return nil, fmt.Errorf("question %q has type %q, want %q", name, ans.Type, q.Type)
		}
		switch q.Type {
		case "noul":
			if ans.Noul == nil {
				return nil, fmt.Errorf("noul answer for %q has no noul", name)
			}
		case "choice":
			if ans.Choice == "" {
				return nil, fmt.Errorf("choice answer for %q has no choice", name)
			}
			for _, c := range q.Criteria {
				if ans.Choice == c {
					goto valid
				}
			}
			return nil, fmt.Errorf("choice answer %q for %q is not in criteria %v", ans.Choice, name, q.Criteria)
		case "score":
			if ans.Score == 0 && q.Threshold > 0 {
				return nil, fmt.Errorf("score answer for %q has no score", name)
			}
		}
	valid:
		answers[name] = ans
	}
	return answers, nil
}

func renderJudgeResults(answers map[string]judgeAnswer) string {
	var sb strings.Builder
	sb.WriteString("Judgment results:\n")
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	for name, ans := range answers {
		switch ans.Type {
		case "noul":
			val := "no"
			if ans.Noul != nil && *ans.Noul {
				val = "yes"
			}
			sb.WriteString(fmt.Sprintf("%s: %s", name, val))
			if ans.Message != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", ans.Message))
			}
			sb.WriteString("\n")
		case "choice":
			sb.WriteString(fmt.Sprintf("%s: %s", name, ans.Choice))
			if ans.Message != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", ans.Message))
			}
			sb.WriteString("\n")
		case "score":
			sb.WriteString(fmt.Sprintf("%s: %g", name, ans.Score))
			if ans.Message != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", ans.Message))
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// ollamaClient is a minimal Ollama chat client for local judgment.
type ollamaClient struct {
	baseURL string
}

func newOllamaClient(baseURL string) (*ollamaClient, error) {
	if baseURL == "" {
		baseURL = judgeDefaultBaseURL
	}
	return &ollamaClient{baseURL: baseURL}, nil
}

func (c *ollamaClient) generate(ctx context.Context, model, prompt string) (string, error) {
	u := c.baseURL + "/api/generate"
	body := map[string]any{
		"model":  model,
		"prompt": prompt,
		"stream": false,
		"options": map[string]any{
			"temperature": 0.0,
			"num_predict": 512,
		},
	}
	client := http.DefaultClient
	req, err := http.NewRequestWithContext(ctx, "POST", u, jsonBody(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.Error != "" {
		return "", fmt.Errorf("ollama error: %s", out.Error)
	}
	return out.Response, nil
}

func jsonBody(v any) *strings.Reader {
	b, _ := json.Marshal(v)
	return strings.NewReader(string(b))
}
