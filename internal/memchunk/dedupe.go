package memchunk

import (
	"context"
	"sort"
	"strconv"

	"github.com/damien1141/a1/internal/llm/jev"
)

// DedupeMerge is what the model decided for one incoming chunk.
type DedupeMerge struct {
	// Chunk is the candidate being stored.
	Chunk Chunk
	// DuplicateOf is the existing memory's id when the model matched one; zero
	// means "store it fresh".
	DuplicateOf int64
	// Confidence in that match, 0..1.
	Confidence float64
}

// Duplicate reports whether this chunk should be folded into an existing
// memory instead of stored.
func (d DedupeMerge) Duplicate() bool {
	return d.DuplicateOf != 0 && d.Confidence >= MinCertainty
}

// DedupeDecision is the model's verdict for one (candidate, existing) pair.
type DedupeDecision int

const (
	// Distinct means the candidate adds information the existing memory lacks.
	Distinct DedupeDecision = iota
	// Same means the candidate restates the existing memory.
	Same
	// Update means the candidate contradicts or supersedes the existing memory.
	Update
)

// Dedupe asks the decision model whether each candidate duplicates an existing
// memory.
//
// Why a model and not cosine: Insert's 0.90 similarity merge is a lexical
// proxy. "Prefers tabs" and "hates spaces" embed close enough to merge, while
// "uses Neovim" and "uses Neovim with a custom Lua config" sit farther apart
// than they should. A decision model reads meaning, and the cost is one call
// with 0 output tokens.
//
// The pair question is INDEPENDENT by construction: each question names its
// candidate and one existing memory, and both are in the state. Nothing depends
// on another answer, which is the constraint the model imposes (it evaluates
// every question against the static state, in parallel).
//
// existing is capped by the caller — the question count is
// len(candidates) x len(existing), so a caller passing 200 memories and 30
// chunks asks 6000 questions. Bound it with NearestCandidates.
func Dedupe(
	ctx context.Context,
	asker Asker,
	candidates []Chunk,
	existing []ExistingMemory,
) ([]DedupeMerge, Usage, error) {
	var cost Usage
	if asker == nil || len(candidates) == 0 || len(existing) == 0 {
		// No existing bank: everything is new. Still returns one entry per
		// candidate so the caller's loop is uniform.
		out := make([]DedupeMerge, 0, len(candidates))
		for _, c := range candidates {
			out = append(out, DedupeMerge{Chunk: c})
		}
		return out, cost, nil
	}

	// The judgment rule is stated ONCE here rather than repeated in every pair
	// question. The first measured run billed 7684 tokens for 6 chunks because
	// 18 pair questions each carried their own copy of the same 160-char
	// criteria block — the dedupe payload was larger than the extraction
	// payload. A rule in the state is read by the model once per request;
	// criteria repeated per question is paid per question.
	state := make([]map[string]any, 0, len(candidates)+len(existing)+1)
	state = append(state, map[string]any{
		"rule": "Two items are the SAME if they express the same underlying fact, " +
			"even when worded differently or one is more detailed. They are DIFFERENT " +
			"if the new one adds information the old one lacks.",
	})
	for _, c := range candidates {
		state = append(state, map[string]any{"id": chunkName(c.Index), "text": c.Text})
	}
	for _, e := range existing {
		state = append(state, map[string]any{"id": existingName(e.ID), "text": e.Content})
	}

	// Bare noul questions: the criteria is optional, and the rule above already
	// defines both outcomes.
	questions := make(jev.Questions, len(candidates)*len(existing))
	for _, c := range candidates {
		for _, e := range existing {
			questions[pairName(c.Index, e.ID)] = jev.Noul{
				Instructions: chunkName(c.Index) + " and " + existingName(e.ID) + " are the same fact?",
			}
		}
	}

	resp, err := asker.SystemOneWithOptions(ctx, state, questions, jev.Options{})
	if err != nil {
		return nil, cost, err
	}
	cost = Usage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens, Calls: 1}
	answers := resp.Answers

	out := make([]DedupeMerge, 0, len(candidates))
	for _, c := range candidates {
		best := DedupeMerge{Chunk: c}
		for _, e := range existing {
			a, ok := answers[pairName(c.Index, e.ID)]
			if !ok || a.Type != jev.AnswerNoul {
				continue
			}
			// The model may judge a pair redundant without being sure. Only a
			// confident match folds; the rest stay distinct, so ambiguity
			// resolves toward storing (a duplicate is recoverable by the user;
			// a dropped fact is not).
			certainty := a.Certainty()
			if a.Noul >= 0.5 && certainty >= MinCertainty && certainty > best.Confidence {
				best.DuplicateOf = e.ID
				best.Confidence = certainty
			}
		}
		out = append(out, best)
	}
	return out, cost, nil
}

// ExistingMemory is the projection of a stored memory the dedupe pass needs.
// Deliberately minimal: id and text are all the model is asked about.
type ExistingMemory struct {
	ID      int64
	Content string
}

func existingName(id int64) string { return "m" + strconv.FormatInt(id, 10) }

func pairName(chunkIdx int, memID int64) string {
	return chunkName(chunkIdx) + "_vs_" + existingName(memID)
}

// MaxDedupePairs caps the pair count so a large bank cannot explode the question
// set. When len(candidates)*len(existing) would exceed it, callers should
// pre-filter with NearestCandidates.
const MaxDedupePairs = 400

// NearestCandidates narrows the existing memories a dedupe pass has to consider,
// keeping the pair count bounded.
//
// It is a cheap pre-filter, not the judgment: the model still decides. The
// ranking is token overlap, which is deliberately dumb — the goal is only to
// make sure the obviously-unrelated memories never reach the model, so the
// question budget goes to plausible matches.
func NearestCandidates(chunks []Chunk, existing []ExistingMemory, perChunk int) []ExistingMemory {
	if len(chunks) == 0 || len(existing) == 0 {
		return nil
	}
	if perChunk <= 0 {
		perChunk = 8
	}
	seen := make(map[int64]bool, len(existing))
	var out []ExistingMemory
	for _, c := range chunks {
		tokens := tokenSet(c.Text)
		ranked := make([]scoredMemory, 0, len(existing))
		for _, e := range existing {
			ranked = append(ranked, scoredMemory{mem: e, score: overlap(tokens, tokenSet(e.Content))})
		}
		// Best matches first, then id, so the result is deterministic.
		sort.SliceStable(ranked, func(i, j int) bool {
			if ranked[i].score != ranked[j].score {
				return ranked[i].score > ranked[j].score
			}
			return ranked[i].mem.ID < ranked[j].mem.ID
		})
		for i := 0; i < len(ranked) && i < perChunk; i++ {
			if ranked[i].score == 0 && i >= perChunk/2 {
				break // no overlap at all: not worth a question
			}
			if !seen[ranked[i].mem.ID] {
				seen[ranked[i].mem.ID] = true
				out = append(out, ranked[i].mem)
			}
		}
		if len(out)*len(chunks) > MaxDedupePairs {
			break
		}
	}
	return out
}

// scoredMemory is one candidate's overlap score against an existing memory.
type scoredMemory struct {
	mem   ExistingMemory
	score int
}
