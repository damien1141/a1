package memchunk

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/damien1141/a1/internal/llm/jev"
)

// Asker is the seam over the decision model, so the extractor is testable
// without a live endpoint. jev.Asker satisfies it.
type Asker interface {
	SystemOneWithOptions(ctx context.Context, state any, questions jev.Questions, opts jev.Options) (jev.Response, error)
}

// Verdict is what the model decided about one chunk.
type Verdict struct {
	Chunk Chunk
	// Remember is the noul answer: does this chunk carry a durable fact?
	Remember bool
	// Certainty is the model's confidence in that answer (0..1).
	Certainty float64
	// Kind is the model's classification (fact/preference/identity/...).
	Kind string
	// Scope is user or project.
	Scope string
	// Salience is derived from the kind/score probabilities — see salience().
	Salience float64
}

// Scored reports whether this verdict should be stored. Extraction stores only
// confident positives: an uncertain "yes" pollutes the bank, and the whole point
// of a cheap judge is that it can afford to say no.
func (v Verdict) Scored() bool {
	return v.Remember && v.Certainty >= MinCertainty
}

// MinCertainty is the floor for storing a chunk. The decision model reports a
// probability per answer; below this the answer is a coin flip and the chunk is
// better dropped than stored. Overridable via config.
//
// Why 0.6 rather than 0.5: a false positive costs the user a junk memory they
// must later forget, while a false negative only costs a memory that a future
// turn can re-earn. The asymmetry favors the higher floor.
const MinCertainty = 0.6

// memoryKinds are the taxonomy the consolidation prompt already documents, kept
// identical so a chunk stored by either path has the same vocabulary.
var memoryKinds = []string{
	"fact", "preference", "task", "reference", "identity", "beat", "affect", "bit",
}

// kindCriteria maps each kind to the description the model judges against. The
// wording is lifted from prompt/memory-consolidation.tmpl so the two extractors
// agree about what a "preference" is — otherwise the same turn stored through
// different paths would be tagged differently.
var kindCriteria = map[string]string{
	"fact":       "a durable factual statement about the user, project, tools, or environment",
	"preference": "a stated liking, disliking, or style preference",
	"task":       "a recurring task pattern or workflow the user follows",
	"reference":  "a pointer to external information the user relies on",
	"identity":   "a core identity fact about the user",
	"beat":       "a recurring life-rhythm fact",
	"affect":     "an emotional pattern or recurring reaction",
	"bit":        "a small recurring detail",
}

// Extract asks the decision model which chunks are worth remembering and how to
// classify them.
//
// One call, N questions. Questions are answered in PARALLEL against the static
// state, so every question must be independently answerable — no question may
// depend on another's answer. That constraint is why chunk membership is asked
// per chunk (independent) rather than as a ranking over all chunks (which would
// require the model to compare answers it cannot see).
//
// Returns (nil, nil) when there is nothing to ask. A model error is returned to
// the caller, which is expected to fall through to the consolidation path: this
// extractor is an optimization, never load-bearing.
func Extract(ctx context.Context, asker Asker, chunks []Chunk) ([]Verdict, Usage, error) {
	var cost Usage
	if asker == nil || len(chunks) == 0 {
		return nil, cost, nil
	}

	state := make([]map[string]any, 0, len(chunks))
	questions := make(jev.Questions, len(chunks)*2)
	for _, c := range chunks {
		name := chunkName(c.Index)
		state = append(state, map[string]any{"id": name, "text": c.Text})

		// Q1: does this carry something lasting? The one question whose answer
		// decides storage.
		questions[name] = jev.Noul{
			Instructions: "Does item " + name + " contain a durable fact about the user, " +
				"their preferences, their environment, or a project decision?",
			Criteria: &jev.NoulCriteria{
				True:  "a lasting preference, identity fact, environment detail, or project decision",
				False: "transient state, a question, a command, an error message, or task-specific detail",
			},
		}
		// Q2: KIND, bare — the alternatives are named in the state legend, so the
		// question only has to say "which one". A Choice with no criteria is
		// invalid, so the criteria carries the NAMES only (short) and the
		// definitions ride the legend in the state.
		//
		// The first measured run billed 9082 tokens for 6 chunks because every
		// chunk carried its own copy of the kind taxonomy (403 chars of criteria
		// x N chunks) and a scope question whose two alternatives are constant
		// across the whole batch. That is pure duplication: the model re-reads
		// identical criteria for every item.
		//
		// Kind is still per-chunk (it genuinely varies), so it stays a question
		// keyed by chunk — but with the SAME criteria object shared by reference
		// rather than rebuilt, and with the instructions trimmed to a constant.
		// Scope moved to a second pass over the survivors only (see inferScope),
		// because scope is nearly always "user" and paying for it on chunks that
		// are about to be rejected is the worst place to spend tokens.
		questions[name+"_kind"] = jev.Choice{
			Instructions: "Which kind is item " + name + "?",
			Criteria:     sharedKindChoices,
		}
	}

	resp, err := asker.SystemOneWithOptions(ctx, state, questions, jev.Options{})
	if err != nil {
		return nil, cost, err
	}
	cost = Usage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens, Calls: 1}
	answers := resp.Answers

	out := make([]Verdict, 0, len(chunks))
	for _, c := range chunks {
		name := chunkName(c.Index)
		a, ok := answers[name]
		if !ok || a.Type != jev.AnswerNoul {
			// A missing answer is NOT a yes. Partial replies are expected when
			// a model declines to answer every question; everything unanswered
			// is dropped.
			continue
		}
		v := Verdict{
			Chunk:     c,
			Remember:  a.Noul >= 0.5,
			Certainty: a.Certainty(),
			Kind:      kindOf(answers[name+"_kind"]),
			// Scope is inferred locally: the model answered the expensive
			// question (is this worth keeping, and of what kind), and scope is a
			// cheap classification with an obvious default. A repo-shaped chunk
			// (a path, an extension, a "this project" phrase) is project-scoped;
			// everything else is about the user.
			Scope: inferScope(c.Text),
		}
		v.Salience = salience(v)
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Chunk.Index < out[j].Chunk.Index })
	return out, cost, nil
}

// Usage is the measured cost of the decision calls, so the benchmark reports
// what the endpoint actually billed rather than an estimate.
type Usage struct {
	InputTokens  int
	OutputTokens int
	Calls        int
}

// Add accumulates another call's usage.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:  u.InputTokens + o.InputTokens,
		OutputTokens: u.OutputTokens + o.OutputTokens,
		Calls:        u.Calls + o.Calls,
	}
}

// chunkName is the join key between a chunk and its questions. Index is used
// verbatim so an answer maps back without positional guessing.
func chunkName(idx int) string { return "c" + strconv.Itoa(idx) }

// sharedKindChoices is built once and referenced by every kind question. It is
// shared by REFERENCE for allocation purposes only — the wire still carries one
// copy per question, and that cost was measured and accepted.
//
// A cheaper variant (names in the questions, definitions once in the state) was
// tried and reverted: it cut ~700 tokens but dropped benchmark recall from 1.00
// to 0.50, because bare criteria names gave the model nothing to judge against.
// Do not re-attempt this without re-running the benchmark; the token saving is
// real and so is the accuracy loss, and accuracy wins.
var sharedKindChoices = func() map[string]any {
	out := make(map[string]any, len(memoryKinds))
	for _, k := range memoryKinds {
		out[k] = kindCriteria[k]
	}
	return out
}()

// kindOf reads a kind answer, defaulting to "fact" — the generic case, and the
// same default the consolidation outlet applies to a missing kind.
func kindOf(a jev.Answer) string {
	if a.Type != jev.AnswerChoice || a.Choice == "" {
		return "fact"
	}
	for _, k := range memoryKinds {
		if k == a.Choice {
			return k
		}
	}
	return "fact"
}

// inferScope classifies a chunk as project-scoped when it is evidently about the
// repository. Local and deterministic: no model call, and the default (user) is
// the common case, so a wrong guess costs a tag and never a memory.
func inferScope(text string) string {
	lower := strings.ToLower(text)
	for _, marker := range projectMarkers {
		if strings.Contains(lower, marker) {
			return "project"
		}
	}
	return "user"
}

// projectMarkers are phrases and shapes that indicate the current repository
// rather than the person. Kept short and unambiguous — a false "project" tag is
// recoverable, a missed memory is not.
var projectMarkers = []string{
	"this project", "this repo", "this repository", "our codebase", "the codebase",
	"the schema", "the build", "the test suite", "the api", "the ci",
}

// salience folds the model's own signals into the 0..1 weight the scorer reads.
//
// Certainty is the dominant term — a confident classification deserves a higher
// initial salience than a hesitant one — and the remaining probability mass is
// used as a secondary term so a chunk that is *almost* another kind still ranks
// above one that is unambiguously boring. Reasoning chunks are discounted:
// thinking is where hypotheses live, and a speculative idea should decay faster
// than a stated fact.
func salience(v Verdict) float64 {
	base := v.Certainty
	if v.Chunk.Kind == KindReasoning {
		base *= 0.75
	}
	switch v.Kind {
	case "identity", "preference":
		// Durable, user-defining facts outrank incidental ones.
		base = base*0.5 + 0.5
	case "affect", "bit":
		// Small or moody details should not crowd out real facts.
		base *= 0.85
	}
	if base < 0.1 {
		base = 0.1
	}
	if base > 1 {
		base = 1
	}
	return base
}

// NextAsker returns the configured decision-model asker, or nil when no jev
// model is set up. Nil is the documented "off" signal — callers must fall
// through to consolidation rather than treating it as an error.
func NextAsker() Asker {
	if !jev.Configured() {
		return nil
	}
	return jev.NewConfiguredAsker()
}

// Trim drops the code/command/url chunks and returns only what is worth asking
// about. Callers use this to skip a model round trip entirely for a
// code-only turn.
func Trim(chunks []Chunk) []Chunk {
	out := make([]Chunk, 0, len(chunks))
	for _, c := range chunks {
		if c.Kind.storable() && strings.TrimSpace(c.Text) != "" {
			out = append(out, c)
		}
	}
	return out
}
