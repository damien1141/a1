package jev

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "maps"
        "net"
        "net/http"
        "net/url"
        "slices"
        "strconv"
        "strings"

        ollama "github.com/ollama/ollama/api"

        "github.com/damien1141/a1/internal/util"
)

// The Ollama provider: a local chat model standing in for the hosted
// decision model. The questions are the same noul/choice/score primitives;
// the difference is the wire. System One answers structurally (the response
// schema is the contract); a chat model answers textually, so the contract
// moves into the prompt and the parse:
//
//   - `format: "json"` turns on Ollama's constrained decoding, so the reply
//     is guaranteed parseable JSON — no fence-stripping gymnastics in the
//     common case (a defensive strip still runs for servers/models that
//     ignore the format).
//   - temperature 0 and thinking off: a decision must be reproducible, and
//     a reasoning preamble is latency with no verdict in it.
//   - num_predict capped: the reply is a few dozen tokens of JSON; a model
//     that starts rambling gets cut off instead of burning the timeout.
//
// Validation is the same spine the TypeSafe wire enforces (see
// wireAnswer.answer): every asked question answered, the choice drawn from
// the question's own alternatives, the score inside the rubric range,
// confidence present for choice/score. Anything else is an error, and the
// wired call sites fall back to their deterministic paths — a bad local
// model can never do worse than no JEV at all.

// ollamaDecisionSystem is the system prompt that turns a chat model into a
// decision model for the length of one call.
const ollamaDecisionSystem = `You are a decision model. You answer questions about a piece of state with fast, structured decisions — never prose, never explanation.

Each question names its type:
- noul (yes/no): answer {"noul": <probability the answer is yes, 0.0 to 1.0>, "confidence": <certainty, 0.0 to 1.0>}
- choice (pick one): answer {"choice": "<exactly one alternative name>", "confidence": <certainty, 0.0 to 1.0>}
- score (rubric): answer {"score": <index of the best-matching level, 0-based>, "confidence": <certainty, 0.0 to 1.0>}

Rules:
- "confidence" is your calibrated certainty in the answer. An answer below 0.6 is discarded — report an honest low confidence rather than guessing.
- For choice, use exactly one of the named alternatives; never invent, translate, or abbreviate a name.
- For score, choose the single best-matching level index.
- Reply with ONE JSON object mapping each question name to its answer object. Nothing else: no markdown fences, no commentary, no keys beyond the questions asked.`

const (
        // ollamaDefaultBaseURL matches the Ollama client default and
        // internal/vector's: loopback IP, not "localhost", to sidestep IPv6
        // resolution surprises.
        ollamaDefaultBaseURL = "http://127.0.0.1:11434"
        // ollamaNumPredict caps generation. The verdict JSON is a few dozen
        // tokens; the cap turns a runaway model into a truncation error
        // instead of a timeout burn.
        ollamaNumPredict = 512
)

// OllamaConfig is one Ollama endpoint connection: the sibling of Config
// on the hosted provider.
type OllamaConfig struct {
        // BaseURL targets the server; empty falls back to OLLAMA_HOST and
        // then ollamaDefaultBaseURL.
        BaseURL string
        // Model is the local model tag. Required — the caller (resolveAsker)
        // enforces it, so an empty model here is a programming error.
        Model string
        // HTTPClient is the transport; nil means util.DefaultHTTPClient.
        HTTPClient *http.Client
}

// OllamaClient asks System One questions to a local Ollama model. It is
// safe for concurrent use.
type OllamaClient struct {
        client *ollama.Client
        model  string
}

// NewOllamaClient resolves cfg against the environment (base URL falls
// back to OLLAMA_HOST, then the loopback default) and builds the client.
func NewOllamaClient(cfg OllamaConfig) *OllamaClient {
        httpClient := cfg.HTTPClient
        if httpClient == nil {
                httpClient = util.DefaultHTTPClient()
        }
        base := resolveOllamaBaseURL(cfg.BaseURL)
        return &OllamaClient{
                client: ollama.NewClient(base, httpClient),
                model:  strings.TrimSpace(cfg.Model),
        }
}

// resolveOllamaBaseURL picks the endpoint: an explicit config URL, else the
// OLLAMA_HOST environment variable, else the loopback default. OLLAMA_HOST
// accepts "host", "host:port", and "scheme://host[:port]" — the same forms
// Ollama's own client accepts — normalized here so one code path parses
// all three.
func resolveOllamaBaseURL(configured string) *url.URL {
        raw := strings.TrimSpace(configured)
        if raw == "" {
                raw = strings.TrimSpace(envValue("OLLAMA_HOST"))
        }
        if raw == "" {
                return mustParseURL(ollamaDefaultBaseURL)
        }
        if !strings.Contains(raw, "://") {
                raw = "http://" + raw
        }
        if u, err := url.Parse(raw); err == nil && u.Host != "" {
                if u.Port() == "" {
                        switch u.Scheme {
                        case "https":
                                u.Host = net.JoinHostPort(u.Hostname(), "443")
                        default:
                                u.Host = net.JoinHostPort(u.Hostname(), "11434")
                        }
                }
                return u
        }
        // Unparseable: fall back to the default rather than disabling — the
        // request will fail loudly with a connection error naming the host.
        return mustParseURL(ollamaDefaultBaseURL)
}

func mustParseURL(raw string) *url.URL {
        u, err := url.Parse(raw)
        if err != nil { // unreachable: the default is a constant
                panic("jev: parse default ollama url: " + err.Error())
        }
        return u
}

// SystemOneWithOptions asks the questions to the local model and parses
// the reply under the same validation spine as the TypeSafe wire. Per-call
// Options.Model overrides the configured tag; the TypeSafe-only Header and
// ExtraBody overrides have no Ollama meaning and are ignored.
func (c *OllamaClient) SystemOneWithOptions(
        ctx context.Context,
        state any,
        questions Questions,
        opts Options,
) (Response, error) {
        encoded, err := encodeQuestions(questions)
        if err != nil {
                return Response{}, err
        }
        if state == nil {
                return Response{}, errStateRequired
        }

        model := c.model
        if override := strings.TrimSpace(opts.Model); override != "" {
                model = override
        }

        req := &ollama.ChatRequest{
                Model: model,
                Messages: []ollama.Message{
                        {Role: "system", Content: ollamaDecisionSystem},
                        {Role: "user", Content: ollamaDecisionPrompt(state, encoded)},
                },
                Stream: &streamFalse,
                // Constrained decoding: the reply must be JSON. Some models wrap
                // anyway; stripFences runs before the parse as a backstop.
                Format: json.RawMessage(`"json"`),
                Options: map[string]any{
                        "temperature": 0,
                        "num_predict": ollamaNumPredict,
                },
                // A decision model must not think out loud: the preamble is pure
                // latency, and thinking text would pollute the JSON contract.
                Think: &ollama.ThinkValue{Value: false},
        }

        var reply ollama.ChatResponse
        err = c.client.Chat(ctx, req, func(resp ollama.ChatResponse) error {
                reply = resp
                return nil
        })
        if err != nil {
                return Response{}, fmt.Errorf("ollama chat: %w", err)
        }

        answers, err := parseOllamaAnswers(reply.Message.Content, encoded)
        if err != nil {
                return Response{}, err
        }
        resp := Response{
                Model:   reply.Model,
                Usage:   Usage{InputTokens: reply.PromptEvalCount, OutputTokens: reply.EvalCount},
                Answers: answers,
                RawBody: json.RawMessage(reply.Message.Content),
        }
        if err := resp.validate(encoded); err != nil {
                return Response{}, err
        }
        return resp, nil
}

// errStateRequired mirrors the TypeSafe path's nil-state rejection.
var errStateRequired = errors.New("system one requires a state value")

// streamFalse backs ChatRequest.Stream — the decision is one JSON object,
// never a token stream.
var streamFalse = false

// ollamaDecisionPrompt renders the state and the encoded questions as the
// user message. Both sides are JSON so the model reads structure, not
// prose about structure.
func ollamaDecisionPrompt(state any, encoded map[string]any) string {
        var sb strings.Builder
        sb.WriteString("STATE:\n")
        encodeIndented(&sb, state)
        sb.WriteString("\n\nQUESTIONS:\n")
        for _, name := range sortedKeys(encoded) {
                sb.WriteString(strconv.Quote(name))
                sb.WriteString(": ")
                encodeIndented(&sb, encoded[name])
                sb.WriteString("\n")
        }
        return sb.String()
}

// encodeIndented writes v as two-space-indented JSON, one line of
// fallback text on a marshal error (state values that cannot marshal are
// rejected before this point; the fallback keeps the prompt builder total).
func encodeIndented(sb *strings.Builder, v any) {
        data, err := json.MarshalIndent(v, "", "  ")
        if err != nil {
                fmt.Fprintf(sb, "%v", v)
                return
        }
        sb.Write(data)
}

func sortedKeys(m map[string]any) []string { return slices.Sorted(maps.Keys(m)) }

// ollamaReplyAnswer is one answer object inside the model's JSON reply.
// Pointers distinguish "absent" from zero: a choice answer that omits
// "choice", or a score answer that omits "confidence", is rejected exactly
// like the same omission on the TypeSafe wire.
type ollamaReplyAnswer struct {
        Noul       *float64 `json:"noul"`
        Choice     string   `json:"choice"`
        Score      *float64 `json:"score"`
        Confidence *float64 `json:"confidence"`
}

// parseOllamaAnswers parses the model's JSON reply against the questions
// actually asked. The reply maps each question name to its answer object;
// every asked question must be present with a type-valid value.
func parseOllamaAnswers(content string, encoded map[string]any) (map[string]Answer, error) {
        text := stripFences(strings.TrimSpace(content))
        var reply map[string]ollamaReplyAnswer
        if err := json.Unmarshal([]byte(text), &reply); err != nil {
                return nil, fmt.Errorf("ollama reply is not valid JSON: %w", err)
        }

        answers := make(map[string]Answer, len(encoded))
        for name, question := range encoded {
                asked, _ := question.(map[string]any)
                want, _ := asked["type"].(string)
                raw, ok := reply[name]
                if !ok {
                        return nil, fmt.Errorf("ollama reply left question %q unanswered", name)
                }
                answer, err := raw.answer(name, want, asked)
                if err != nil {
                        return nil, err
                }
                answers[name] = answer
        }
        return answers, nil
}

// answer converts one reply object into an Answer, checking the value
// against the question it claims to answer.
func (a ollamaReplyAnswer) answer(name, want string, asked map[string]any) (Answer, error) {
        missing := func(field string) (Answer, error) {
                return Answer{}, fmt.Errorf("ollama answer for question %q has no %q", name, field)
        }
        bad := func(format string, args ...any) (Answer, error) {
                return Answer{}, fmt.Errorf("ollama answer for question %q: %s", name, fmt.Sprintf(format, args...))
        }

        answer := Answer{Type: want}
        switch want {
        case AnswerNoul:
                if a.Noul == nil {
                        return missing("noul")
                }
                if *a.Noul < 0 || *a.Noul > 1 {
                        return bad("noul %g is outside 0..1", *a.Noul)
                }
                answer.Noul = *a.Noul
                // Confidence is optional for a noul on the TypeSafe wire; an
                // omitted one reads as zero there, so it does here too.
                if a.Confidence != nil {
                        answer.Confidence = *a.Confidence
                }
        case AnswerChoice:
                switch {
                case a.Choice == "":
                        return missing("choice")
                case a.Confidence == nil:
                        return missing("confidence")
                }
                if !choiceInCriteria(a.Choice, asked) {
                        return bad("choice %q is not one of the named alternatives", a.Choice)
                }
                if *a.Confidence < 0 || *a.Confidence > 1 {
                        return bad("confidence %g is outside 0..1", *a.Confidence)
                }
                answer.Choice, answer.Confidence = a.Choice, *a.Confidence
        case AnswerScore:
                switch {
                case a.Score == nil:
                        return missing("score")
                case a.Confidence == nil:
                        return missing("confidence")
                }
                levels := scoreLevels(asked)
                if *a.Score < 0 || *a.Score > float64(levels-1) {
                        return bad("score %g is outside the rubric range 0..%d", *a.Score, levels-1)
                }
                if *a.Score != float64(int(*a.Score)) {
                        return bad("score %g is not a whole rubric level", *a.Score)
                }
                if *a.Confidence < 0 || *a.Confidence > 1 {
                        return bad("confidence %g is outside 0..1", *a.Confidence)
                }
                answer.Score, answer.Confidence = *a.Score, *a.Confidence
        default:
                return bad("question type %q is not a noul, choice, or score", want)
        }
        return answer, nil
}

// choiceInCriteria reports whether choice is one of the question's named
// alternatives. The encoded criteria for a choice question is a JSON
// object whose keys are the alternative names.
func choiceInCriteria(choice string, asked map[string]any) bool {
        criteria, _ := asked["criteria"].(map[string]any)
        if criteria == nil {
                return false
        }
        _, ok := criteria[choice]
        return ok
}

// scoreLevels counts a score question's rubric levels (position zero
// first). Zero means the question carried no usable criteria — every score
// then fails the range check, which is the honest outcome for a question
// that cannot be answered.
func scoreLevels(asked map[string]any) int {
        criteria, ok := asked["criteria"].([]any)
        if !ok {
                return 0
        }
        return len(criteria)
}

// stripFences removes a markdown code fence a model wrapped its JSON in.
// With format:"json" this never fires; it exists for the server or model
// that ignores the format flag — the parse after it stays strict.
func stripFences(text string) string {
        if !strings.HasPrefix(text, "```") {
                return text
        }
        if _, rest, ok := strings.Cut(text, "\n"); ok {
                text = rest
        }
        if end := strings.LastIndex(text, "```"); end >= 0 {
                text = text[:end]
        }
        return strings.TrimSpace(text)
}
