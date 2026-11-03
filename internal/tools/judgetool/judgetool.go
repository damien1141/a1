// Package judgetool is the agent-facing JEV surface: ask the decision
// model named questions about a piece of state or about files, without
// loading either into the main model's context.
//
// The tool routes through the configured decision model (the jev: section
// of config.yaml — hosted api or local ollama — or the TYPESAFE_API_KEY
// fallback) via internal/llm/jev, which owns the noul/choice/score
// question primitives and their validation spine. Nothing resolving is a
// configuration error, not a silent no-op: the model called the tool and
// deserves an answer that says why there is none.
//
// The files mode is the point of the tool: "does this file contain a
// todo?" is a classification, and classification is cheap. The agent
// passes a glob, the tool reads the matched files (bounded), asks the same
// questions about each file's content in parallel, and returns compact
// per-file answers — the file bytes go to the decision model, not into the
// context window the main model pays for on every turn.
package judgetool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/debuglog"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/llm/jev"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

const (
	// judgeDefaultLimit bounds how many glob-matched files one call judges
	// when the caller passes no limit.
	judgeDefaultLimit = 20
	// judgeMaxLimit is the hard ceiling on limit — a classification fan-out
	// is cheap, not free, and a runaway glob (*.go on a monorepo) should
	// answer, not drown the provider.
	judgeMaxLimit = 100
	// judgeFileCap bounds the bytes of one file sent as decision state.
	judgeFileCap = 24_000
	// judgeCallTimeout bounds one decision call against the hosted API.
	// The Ollama provider raises this ceiling (a local model can take
	// seconds), and jev.timeout_sec overrides both — see jev.EffectiveTimeout.
	judgeCallTimeout = 8 * time.Second
	// judgeConcurrency bounds the parallel per-file calls: enough to keep
	// the hosted API busy, few enough that a local Ollama queues instead
	// of thrashing.
	judgeConcurrency = 4
)

var judgeDescription = `Ask the decision model (JEV) named questions about state or files — classification without loading content into context.

questions is a JSON object mapping question names to definitions:
  {"todo": {"type": "noul", "criteria": {"true": "the text mentions a TODO/FIXME task"}}}
  {"layer": {"type": "choice", "criteria": ["api", "ui", "tests", "docs"]}}
  {"quality": {"type": "score", "criteria": ["broken", "rough", "solid", "exemplary"]}}
Each definition takes an optional "instructions" string. choice criteria is a list of
alternative names (or an object mapping name to when-it-applies); score criteria is an
ordered list of level descriptions, best last.

Evaluate either state (a JSON document or text; one call) or files (comma-separated
globs resolved from the working directory; every matched file is judged independently
and in parallel, answers come back per file). limit caps the file count (default 20,
max 100). Answers carry confidence; report them, do not re-derive them by reading the
files yourself.`

// JudgeTool returns the decision-model judgment tool definition + handler.
func JudgeTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "judge",
			Description: judgeDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"questions": llm.Object{
						"type":        "string",
						"description": `Questions JSON. Example: {"todo": {"type": "noul"}, "layer": {"type": "choice", "criteria": ["api", "tests"]}}`,
					},
					"state": llm.Object{
						"type":        "string",
						"description": `State to evaluate, as a JSON document or plain text. Example: {"risk": 0.8, "urgency": "high"}`,
					},
					"files": llm.Object{
						"type":        "string",
						"description": "Comma-separated file globs to judge (each matched file judged independently). Example: ./internal/agent/*.go",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum files to judge (default %d, max %d).", judgeDefaultLimit, judgeMaxLimit),
					},
					"model": llm.Object{
						"type":        "string",
						"description": "Optional model override (api alias or ollama tag).",
					},
				},
				Required: []string{"questions"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in judgeInput
			_ = json.Unmarshal(input, &in)
			if f := strings.TrimSpace(in.Files); f != "" {
				return "judge " + truncateFor(f, 60)
			}
			if s := strings.TrimSpace(in.State); s != "" {
				return "judge " + truncateFor(s, 60)
			}
			if q := strings.TrimSpace(in.Questions); q != "" {
				return "judge " + truncateFor(q, 60)
			}
			return "judge"
		},
		Run: func(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
			r := newJudgeRunner()
			if r == nil {
				return tooldef.Result{}, errors.New("no decision model configured: set the jev: section in config.yaml (provider: api or ollama) or export TYPESAFE_API_KEY")
			}
			return runJudge(r, ctx, input)
		},
	}
}

type judgeInput struct {
	Questions string `json:"questions"`
	State     string `json:"state"`
	Files     string `json:"files"`
	Limit     int    `json:"limit"`
	Model     string `json:"model"`
}

// judgeRunner is one resolved decision-model backend.
type judgeRunner struct {
	asker   jev.Asker
	timeout time.Duration
}

// newJudgeRunner resolves the backend per call: the jev: section (or
// TYPESAFE_API_KEY), else nil. Nil is the caller's cue to answer with the
// configuration error; building per call means a config edit applies to
// the next judgment without a rebuild.
func newJudgeRunner() *judgeRunner {
	if !jev.Configured() {
		return nil
	}
	return &judgeRunner{asker: jev.NewConfiguredAsker(), timeout: jev.EffectiveTimeout(judgeCallTimeout)}
}

// runJudge is the tool body against an injected runner, so tests script
// the backend without touching process-wide configuration.
func runJudge(r *judgeRunner, ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	if r == nil || r.asker == nil {
		return tooldef.Result{}, errors.New("no decision model configured")
	}
	var in judgeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse judge arguments: %w", err)
	}

	questions, err := parseQuestions(in.Questions)
	if err != nil {
		return tooldef.Result{}, err
	}
	opts := jev.Options{}
	if m := strings.TrimSpace(in.Model); m != "" {
		opts.Model = m
	}

	files := strings.TrimSpace(in.Files)
	state := strings.TrimSpace(in.State)
	switch {
	case files != "":
		return r.judgeFiles(ctx, in, questions, opts)
	case state != "":
		return r.judgeState(ctx, state, questions, opts)
	default:
		return tooldef.Result{}, errors.New("provide either state (a JSON document or text) or files (comma-separated globs) to evaluate")
	}
}

// structuredState keeps JSON states structured on the wire: the decision
// model reads the object, not a string quoting an object.
func structuredState(state string) any {
	trimmed := strings.TrimSpace(state)
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded
	}
	return trimmed
}

// judgeState asks the questions about one state value.
func (r *judgeRunner) judgeState(ctx context.Context, state string, questions jev.Questions, opts jev.Options) (tooldef.Result, error) {
	resp, err := r.ask(ctx, structuredState(state), questions, opts)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("judge: %w", err)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d answers via jev\n\n", len(resp.Answers))
	writeAnswers(&sb, resp)
	return tooldef.Result{
		Content: sb.String(),
		Detail:  "judge",
		Summary: fmt.Sprintf("%d answers via jev", len(resp.Answers)),
		Output:  sb.String(),
	}, nil
}

// fileVerdict is one file's classification in the fan-out.
type fileVerdict struct {
	path    string
	resp    jev.Response
	err     error
	skipped string // non-empty: the file was not judged (binary), reason inside
}

// judgeFiles resolves the globs, reads the matched files, and asks the
// same questions about each file's content in parallel.
func (r *judgeRunner) judgeFiles(ctx context.Context, in judgeInput, questions jev.Questions, opts jev.Options) (tooldef.Result, error) {
	paths, matched, err := expandGlobs(ctx, in.Files)
	if err != nil {
		return tooldef.Result{}, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = judgeDefaultLimit
	}
	if limit > judgeMaxLimit {
		limit = judgeMaxLimit
	}
	capped := false
	if len(paths) > limit {
		paths = paths[:limit]
		capped = true
	}

	verdicts := make([]fileVerdict, len(paths))
	sem := make(chan struct{}, judgeConcurrency)
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			verdicts[i] = r.judgeOneFile(ctx, path, questions, opts)
		}(i, path)
	}
	wg.Wait()

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d files × %d questions via jev\n\n", len(paths), len(questions))
	relBase := displayBase(ctx)
	for _, v := range verdicts {
		sb.WriteString(relPath(relBase, v.path))
		sb.WriteString("\n")
		switch {
		case v.skipped != "":
			fmt.Fprintf(&sb, "  skipped: %s\n", v.skipped)
		case v.err != nil:
			fmt.Fprintf(&sb, "  error: %v\n", v.err)
		default:
			writeAnswers(&sb, v.resp)
		}
		sb.WriteString("\n")
	}
	if capped {
		fmt.Fprintf(&sb, "(matched %d files; judged the first %d — raise limit to see more)\n", matched, limit)
	}

	summary := fmt.Sprintf("%d files · %d questions · jev", len(paths), len(questions))
	return tooldef.Result{Content: sb.String(), Detail: "judge " + in.Files, Summary: summary, Output: sb.String()}, nil
}

// judgeOneFile reads one file and asks the questions about its content.
func (r *judgeRunner) judgeOneFile(ctx context.Context, path string, questions jev.Questions, opts jev.Options) fileVerdict {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileVerdict{path: path, err: fmt.Errorf("read: %w", err)}
	}
	if idx := indexOfNUL(data, 8_000); idx >= 0 {
		return fileVerdict{path: path, skipped: "binary file"}
	}
	content := string(data)
	if len(content) > judgeFileCap {
		content = content[:judgeFileCap] + "\n[... file truncated ...]"
	}
	state := map[string]any{"path": filepath.ToSlash(path), "content": content}
	resp, err := r.ask(ctx, state, questions, opts)
	if err != nil {
		debuglog.Logf("judge: %s: %v", path, err)
		return fileVerdict{path: path, err: err}
	}
	return fileVerdict{path: path, resp: resp}
}

// ask runs one bounded decision call.
func (r *judgeRunner) ask(ctx context.Context, state any, questions jev.Questions, opts jev.Options) (jev.Response, error) {
	qCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.asker.SystemOneWithOptions(qCtx, state, questions, opts)
}

// expandGlobs splits a comma-separated glob list, expands each against the
// session cwd, dedupes, and sorts. The second return is the full match
// count before any limit is applied.
func expandGlobs(ctx context.Context, files string) ([]string, int, error) {
	cwd, err := tooldef.Cwd(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("resolve working directory: %w", err)
	}
	seen := make(map[string]bool)
	var paths []string
	for _, pattern := range strings.Split(files, ",") {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(cwd, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, 0, fmt.Errorf("bad glob %q: %w", pattern, err)
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil || info.IsDir() {
				continue
			}
			if !seen[m] {
				seen[m] = true
				paths = append(paths, m)
			}
		}
	}
	if len(paths) == 0 {
		return nil, 0, fmt.Errorf("no files matched %s", files)
	}
	sort.Strings(paths)
	return paths, len(paths), nil
}

// displayBase returns the directory to relativize output paths against.
func displayBase(ctx context.Context) string {
	cwd, err := tooldef.Cwd(ctx)
	if err != nil {
		return ""
	}
	return cwd
}

// relPath renders a path relative to base when inside it, else absolute.
func relPath(base, path string) string {
	if base == "" {
		return filepath.ToSlash(path)
	}
	if rel, err := filepath.Rel(base, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

// indexOfNUL reports the index of the first NUL byte within the first
// limit bytes, or -1 — the cheap binary sniff.
func indexOfNUL(data []byte, limit int) int {
	if len(data) < limit {
		limit = len(data)
	}
	for i := 0; i < limit; i++ {
		if data[i] == 0 {
			return i
		}
	}
	return -1
}

// questionSpec is one entry of the questions argument.
type questionSpec struct {
	Type         string          `json:"type"`
	Instructions string          `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria"`
}

// parseQuestions accepts the questions argument either as a JSON string
// (the declared schema — models habitually quote nested documents) or as
// an embedded object, and builds the jev question set.
func parseQuestions(raw string) (jev.Questions, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("questions is required: provide a JSON object mapping question names to definitions")
	}
	if strings.HasPrefix(trimmed, "{") {
		// Already an object — use it directly.
	} else if strings.HasPrefix(trimmed, `"`) {
		var unquoted string
		if err := json.Unmarshal([]byte(trimmed), &unquoted); err != nil {
			return nil, fmt.Errorf("questions must be a JSON object (optionally quoted): %w", err)
		}
		trimmed = strings.TrimSpace(unquoted)
	} else {
		return nil, errors.New("questions must be a JSON object mapping question names to definitions")
	}
	var specs map[string]questionSpec
	if err := json.Unmarshal([]byte(trimmed), &specs); err != nil {
		return nil, fmt.Errorf("invalid questions JSON: %w", err)
	}
	if len(specs) == 0 {
		return nil, errors.New("questions must define at least one question")
	}
	questions := make(jev.Questions, len(specs))
	for name, spec := range specs {
		q, err := buildQuestion(name, spec)
		if err != nil {
			return nil, err
		}
		questions[name] = q
	}
	return questions, nil
}

// buildQuestion converts one spec into its jev primitive.
func buildQuestion(name string, spec questionSpec) (jev.Question, error) {
	bad := func(format string, args ...any) (jev.Question, error) {
		return nil, fmt.Errorf("question %q: %s", name, fmt.Sprintf(format, args...))
	}
	instructions := strings.TrimSpace(spec.Instructions)
	var instr any
	if instructions != "" {
		instr = instructions
	}
	switch strings.ToLower(strings.TrimSpace(spec.Type)) {
	case "noul":
		q := jev.Noul{Instructions: instr}
		if len(spec.Criteria) > 0 {
			var crit map[string]any
			if err := json.Unmarshal(spec.Criteria, &crit); err != nil {
				return bad(`noul criteria must be an object like {"true": "...", "false": "..."}`)
			}
			q.Criteria = &jev.NoulCriteria{True: crit["true"], False: crit["false"]}
		}
		return q, nil
	case "choice":
		if len(spec.Criteria) == 0 {
			return bad(`choice requires "criteria": the alternative names`)
		}
		criteria, err := choiceCriteria(spec.Criteria)
		if err != nil {
			return bad("%v", err)
		}
		return jev.Choice{Instructions: instr, Criteria: criteria}, nil
	case "score":
		if len(spec.Criteria) == 0 {
			return bad(`score requires "criteria": the ordered level descriptions, best last`)
		}
		var levels []any
		if err := json.Unmarshal(spec.Criteria, &levels); err != nil {
			return bad(`score criteria must be an ordered array of level descriptions`)
		}
		if len(levels) == 0 {
			return bad(`score criteria must list at least one level`)
		}
		return jev.Score{Instructions: instr, Criteria: levels}, nil
	default:
		return bad(`unknown type %q (want "noul", "choice", or "score")`, spec.Type)
	}
}

// choiceCriteria accepts the alternative names either as an array of
// strings or as an object mapping name to when-it-applies.
func choiceCriteria(raw json.RawMessage) (map[string]any, error) {
	var names []string
	if err := json.Unmarshal(raw, &names); err == nil {
		if len(names) == 0 {
			return nil, errors.New(`choice criteria must list at least one alternative`)
		}
		criteria := make(map[string]any, len(names))
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n == "" {
				return nil, errors.New(`choice criteria names must be non-empty`)
			}
			criteria[n] = nil
		}
		return criteria, nil
	}
	var described map[string]any
	if err := json.Unmarshal(raw, &described); err == nil && len(described) > 0 {
		return described, nil
	}
	return nil, errors.New(`choice criteria must be an array of alternative names or an object mapping name to description`)
}

// writeAnswers renders one answer set, sorted by question name so the
// output is deterministic.
func writeAnswers(sb *strings.Builder, resp jev.Response) {
	names := make([]string, 0, len(resp.Answers))
	for name := range resp.Answers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sb.WriteString("  ")
		sb.WriteString(renderAnswer(name, resp.Answers[name]))
		sb.WriteString("\n")
	}
}

// renderAnswer renders one answer compactly: the verdict plus the numbers
// that let the model weigh it.
func renderAnswer(name string, a jev.Answer) string {
	conf := func(s string) string {
		if a.Confidence > 0 {
			return fmt.Sprintf("%s, conf %.2f)", s, a.Confidence)
		}
		return s + ")"
	}
	switch a.Type {
	case jev.AnswerNoul:
		verdict := "no"
		if a.Noul >= 0.5 {
			verdict = "yes"
		}
		return conf(fmt.Sprintf("%s: %s (p %.2f", name, verdict, a.Noul))
	case jev.AnswerChoice:
		return conf(fmt.Sprintf("%s: %s (", name, a.Choice))
	case jev.AnswerScore:
		level := int(a.Score)
		out := fmt.Sprintf("%s: %d", name, level)
		if label := legendLabel(a.Legend, level); label != "" {
			out += " — " + label
		}
		return conf(out + " (")
	default:
		return fmt.Sprintf("%s: [%s]", name, a.Type)
	}
}

// legendLabel extracts a human label for one score level from the answer's
// legend, whose keys are decimal strings and whose values are the criteria
// entries as sent.
func legendLabel(legend map[string]any, level int) string {
	if legend == nil {
		return ""
	}
	entry, ok := legend[fmt.Sprintf("%d", level)]
	if !ok {
		return ""
	}
	switch value := entry.(type) {
	case string:
		return truncateFor(value, 60)
	case map[string]any:
		for _, key := range []string{"label", "description", "name"} {
			if s, ok := value[key].(string); ok && s != "" {
				return truncateFor(s, 60)
			}
		}
	}
	return ""
}

// truncateFor caps a preview string for display.
func truncateFor(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
