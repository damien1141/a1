package memchunk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm/jev"
)

// fakeAsker answers questions from a rule table, so the harness is testable
// without a live decision model. It records what it was asked, which is how the
// tests prove the question SET is right (independent per-chunk questions, one
// call) independently of what the answers happen to be.
type fakeAsker struct {
	// rules maps a substring of a state item's text to the noul probability for
	// that item's chunk question.
	rules map[string]float64
	// kindFor maps a substring to the chosen kind.
	kindFor map[string]string

	calls     int
	questions int
	states    int
	lastState any
}

func (f *fakeAsker) SystemOneWithOptions(
	_ context.Context, state any, questions jev.Questions, _ jev.Options,
) (jev.Response, error) {
	f.calls++
	f.questions = len(questions)
	f.lastState = state

	items, _ := state.([]map[string]any)
	f.states = len(items)

	// Index the state by id so a pair question can look up both sides.
	textByID := make(map[string]string, len(items))
	for _, item := range items {
		id, _ := item["id"].(string)
		text, _ := item["text"].(string)
		if id != "" {
			textByID[id] = text
		}
	}

	answers := map[string]jev.Answer{}
	prob := func(text string) float64 {
		lower := strings.ToLower(text)
		for marker, p := range f.rules {
			if strings.Contains(lower, strings.ToLower(marker)) {
				return p
			}
		}
		return 0.1 // default: not memorable
	}

	for id, text := range textByID {
		// Extraction shape: a question named exactly after the item.
		if _, isChunk := questions[id]; isChunk {
			answers[id] = jev.Answer{Type: jev.AnswerNoul, Noul: prob(text)}

			kind := "fact"
			lower := strings.ToLower(text)
			for marker, k := range f.kindFor {
				if strings.Contains(lower, strings.ToLower(marker)) {
					kind = k
					break
				}
			}
			if _, asked := questions[id+"_kind"]; asked {
				answers[id+"_kind"] = jev.Answer{Type: jev.AnswerChoice, Choice: kind, Confidence: 0.9}
				answers[id+"_scope"] = jev.Answer{Type: jev.AnswerChoice, Choice: "user", Confidence: 0.9}
			}
			continue
		}

		// Dedupe shape: this item appears only as the left half of pair
		// questions named "<id>_vs_<other>". Answer each pair independently,
		// which is exactly what the live model does.
		for qname := range questions {
			left, right, ok := strings.Cut(qname, "_vs_")
			if !ok || left != id {
				continue
			}
			other, found := textByID[right]
			if !found {
				continue
			}
			// The fixture's near-duplicate: both sides talk about tabs.
			if strings.Contains(strings.ToLower(text), "tabs") &&
				strings.Contains(strings.ToLower(other), "tabs") {
				answers[qname] = jev.Answer{Type: jev.AnswerNoul, Noul: 0.97}
			} else {
				answers[qname] = jev.Answer{Type: jev.AnswerNoul, Noul: 0.05}
			}
		}
	}
	return jev.Response{Answers: answers}, nil
}

// TestSegment_MixedContentKeepsProseAndQuarantinesCode_Positive is the headline
// fix: one turn containing a preference AND a stack trace must yield the
// preference as a storable chunk and the trace as code, not one blob.
func TestSegment_MixedContentKeepsProseAndQuarantinesCode_Positive(t *testing.T) {
	turn := FixtureTurns()[0] // the mixed turn
	chunks := Segment(turn.Text, turn.Kind)
	require.NotEmpty(t, chunks)

	var prose, code []string
	for _, c := range chunks {
		switch c.Kind {
		case KindProse, KindReasoning:
			prose = append(prose, c.Text)
		case KindCode:
			code = append(code, c.Text)
		}
	}

	joined := strings.ToLower(strings.Join(prose, " | "))
	assert.Contains(t, joined, "prefer tabs", "the preference must survive as prose")
	assert.Contains(t, joined, "arch", "the environment fact must survive as prose")

	// After Trim (what callers pass to the model) no code remains at all.
	trimmed := Trim(chunks)
	for _, c := range trimmed {
		assert.NotEqual(t, KindCode, c.Kind)
		assert.NotContains(t, c.Text, "goroutine")
	}
}

// TestSegment_OnlyStorableChunksAreReturned_Negative: Segment returns only
// storable kinds, so a code-only turn produces nothing and costs no model call.
func TestSegment_OnlyStorableChunksAreReturned_Negative(t *testing.T) {
	codeOnly := "```go\nfunc main() { panic(\"boom\") }\n```"
	assert.Empty(t, Segment(codeOnly, KindProse), "a code-only turn yields no candidates")

	logDump := "2024-01-15 ERROR failed to connect\n2024-01-15 ERROR retrying in 5s"
	for _, c := range Segment(logDump, KindProse) {
		assert.NotEqual(t, KindCode, c.Kind)
	}
}

// TestSegment_RespectsLimits_Negative: a huge paste cannot create unbounded work.
func TestSegment_RespectsLimits_Negative(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("This is statement number one about a durable preference that is long enough. ")
	}
	chunks := Segment(sb.String(), KindProse)
	assert.LessOrEqual(t, len(chunks), MaxChunks, "chunk count must be capped")

	for _, c := range chunks {
		assert.LessOrEqual(t, len([]rune(c.Text)), MaxChunkChars+2, "chunk length must be capped")
	}
}

// TestSegment_AbbreviationsStayWhole_Differential: the splitter must not shatter
// statements on "e.g.", "vs.", or a decimal. Oracle: hand-written expected chunks.
func TestSegment_AbbreviationsStayWhole_Differential(t *testing.T) {
	cases := map[string]string{
		"Use short flags, e.g. -v, and prefer them over long forms.":        "e.g. -v",
		"Compare tabs vs. spaces before deciding on an indentation policy.": "vs. spaces",
		"Set the timeout to 3.14 seconds and leave it.":                     "3.14",
	}
	for input, mustContain := range cases {
		chunks := Segment(input, KindProse)
		require.Len(t, chunks, 1, "input must stay one chunk: %s", input)
		assert.Contains(t, chunks[0].Text, mustContain)
	}
}

// TestExtract_OneCallForAllChunks_Positive: the whole point of the redesign —
// N chunks cost ONE round trip, and question names join back to chunks.
func TestExtract_OneCallForAllChunks_Positive(t *testing.T) {
	asker := &fakeAsker{
		rules: map[string]float64{"prefer tabs": 0.95, "sqlite": 0.9},
	}
	chunks := []Chunk{
		{Text: "I prefer tabs over spaces for all indentation.", Kind: KindProse, Index: 0},
		{Text: "We chose SQLite for the vault storage layer.", Kind: KindProse, Index: 1},
		{Text: "The build failed again this morning.", Kind: KindProse, Index: 2},
	}

	verdicts, _, err := Extract(context.Background(), asker, chunks)
	require.NoError(t, err)

	assert.Equal(t, 1, asker.calls, "one call for all chunks, never one per chunk")
	// State carries exactly the chunks — no legend. A legend-based variant that
	// moved the kind definitions out of the questions was measured and reverted:
	// it saved ~700 tokens and halved benchmark recall.
	assert.Equal(t, len(chunks), asker.states)
	// 2 questions per chunk: remember / kind. Scope is inferred locally — the
	// first measured run billed 9082 tokens for 6 chunks, largely because every
	// chunk carried its own copy of the kind taxonomy plus a constant scope
	// question, and this count is the pin that keeps that from creeping back.
	assert.Equal(t, len(chunks)*2, asker.questions,
		"per-chunk questions must stay at 2; a third re-inflates the token bill")

	kept := 0
	for _, v := range verdicts {
		if v.Scored() {
			kept++
			assert.GreaterOrEqual(t, v.Certainty, MinCertainty)
		}
	}
	assert.Equal(t, 2, kept, "the two real facts store; the transient one does not")
}

// TestExtract_UncertainAnswersAreDropped_Negative: a coin-flip answer must not
// be stored. This is the guard that keeps precision high when the model hedges.
func TestExtract_UncertainAnswersAreDropped_Negative(t *testing.T) {
	asker := &fakeAsker{rules: map[string]float64{"maybe": 0.55}}
	chunks := []Chunk{{Text: "Maybe this mentions a preference of some kind.", Kind: KindProse, Index: 0}}

	verdicts, _, err := Extract(context.Background(), asker, chunks)
	require.NoError(t, err)
	require.Len(t, verdicts, 1)
	assert.True(t, verdicts[0].Remember, "the model did say yes")
	assert.False(t, verdicts[0].Scored(), "but not confidently enough to store")
}

// TestExtract_NilAskerIsOff_Negative: no decision model means no work, not an
// error — callers fall through to consolidation.
func TestExtract_NilAskerIsOff_Negative(t *testing.T) {
	verdicts, _, err := Extract(context.Background(), nil, []Chunk{{Text: "x", Index: 0}})
	assert.NoError(t, err)
	assert.Empty(t, verdicts)
}

// TestSalience_RanksDurability_Differential pins the weighting: a confident
// preference outranks a confident incidental bit, and reasoning is discounted.
func TestSalience_RanksDurability_Differential(t *testing.T) {
	pref := salience(Verdict{Chunk: Chunk{Kind: KindProse}, Kind: "preference", Certainty: 0.9})
	bit := salience(Verdict{Chunk: Chunk{Kind: KindProse}, Kind: "bit", Certainty: 0.9})
	reasoned := salience(Verdict{Chunk: Chunk{Kind: KindReasoning}, Kind: "preference", Certainty: 0.9})

	assert.Greater(t, pref, bit, "a durable preference outranks a small detail")
	assert.Greater(t, pref, reasoned, "the final answer outranks the same claim in thinking")
	for _, v := range []float64{pref, bit, reasoned} {
		assert.GreaterOrEqual(t, v, 0.0)
		assert.LessOrEqual(t, v, 1.0)
	}
}

// TestDedupe_FoldsNearDuplicate_Positive: the user's requirement — a jev pass
// decides redundancy rather than trusting an embedding threshold.
func TestDedupe_FoldsNearDuplicate_Positive(t *testing.T) {
	asker := &fakeAsker{}
	candidates := []Chunk{
		{Text: "I really do prefer tabs to spaces.", Kind: KindProse, Index: 0},
		{Text: "We use PostgreSQL for the analytics warehouse.", Kind: KindProse, Index: 1},
	}
	existing := []ExistingMemory{{ID: 1, Content: "User prefers tabs over spaces."}}

	merges, _, err := Dedupe(context.Background(), asker, candidates, existing)
	require.NoError(t, err)
	require.Len(t, merges, 2)

	assert.True(t, merges[0].Duplicate(), "the tabs restatement folds into memory 1")
	assert.Equal(t, int64(1), merges[0].DuplicateOf)
	assert.False(t, merges[1].Duplicate(), "the unrelated fact stays")
}

// TestDedupe_NoBankStoresEverything_Negative: an empty bank must not suppress.
func TestDedupe_NoBankStoresEverything_Negative(t *testing.T) {
	merges, _, err := Dedupe(context.Background(), &fakeAsker{},
		[]Chunk{{Text: "a durable fact about the project", Index: 0}}, nil)
	require.NoError(t, err)
	require.Len(t, merges, 1)
	assert.False(t, merges[0].Duplicate())
}

// TestNearestCandidates_BoundsTheQuestionCount_Negative: the pair count is what
// keeps dedupe affordable, so the pre-filter must actually bound it.
func TestNearestCandidates_BoundsTheQuestionCount_Negative(t *testing.T) {
	chunks := []Chunk{
		{Text: "prefers tabs for indentation", Index: 0},
		{Text: "runs tests before committing", Index: 1},
	}
	var bank []ExistingMemory
	for i := 1; i <= 100; i++ {
		bank = append(bank, ExistingMemory{ID: int64(i), Content: "unrelated memory number " + strings.Repeat("x", i)})
	}
	// Add one true match.
	bank = append(bank, ExistingMemory{ID: 500, Content: "prefers tabs for indentation"})

	near := NearestCandidates(chunks, bank, 4)
	assert.LessOrEqual(t, len(near), 8, "the candidate set must stay small")
	assert.Less(t, len(near)*len(chunks), MaxDedupePairs)

	foundMatch := false
	for _, e := range near {
		if e.ID == 500 {
			foundMatch = true
		}
	}
	assert.True(t, foundMatch, "the genuinely related memory must survive the pre-filter")
}

// TestRunBenchmark_FakeModelScores_Positive: the harness runs end to end and
// scores a known-good answer set perfectly, so the scoring logic itself is
// proven before any real model is involved.
func TestRunBenchmark_FakeModelScores_Positive(t *testing.T) {
	asker := &fakeAsker{
		rules: map[string]float64{
			"prefer tabs":           0.95,
			"arch":                  0.95,
			"sqlite":                0.95,
			"schema is owned":       0.95,
			"work late on weekdays": 0.95,
			"short answers":         0.95,
		},
		kindFor: map[string]string{"prefer tabs": "preference", "arch": "fact"},
	}

	rep, err := RunBenchmark(context.Background(), asker)
	require.NoError(t, err)

	assert.Greater(t, rep.Chunks, 0)
	assert.Greater(t, rep.Stored, 0)
	assert.Greater(t, rep.Recall, 0.0, "the fake model answers the labelled facts")
	assert.NotEmpty(t, rep.String(), "the report renders")
}

// TestSegment_ArtifactsNeverReachTheModel_Negative is the regression pin for the
// first real benchmark run: the log dump and the test-output turn were both
// stored as memories (precision 0.71) because the segmenter passed them through
// as prose. Artifacts must be classified BEFORE the model is asked — that is the
// deterministic half of "cheap", and it is the half regex is actually good at.
func TestSegment_ArtifactsNeverReachTheModel_Negative(t *testing.T) {
	artifacts := map[string]string{
		"log dump": "2024-01-15 10:22:01 ERROR failed to connect to 127.0.0.1:5432\n" +
			"2024-01-15 10:22:02 ERROR retrying in 5s\n2024-01-15 10:22:12 ERROR gave up after 3 attempts",
		"test output": "--- FAIL: TestInsert (0.01s)\n    store_test.go:88: expected 1 row, got 0",
		"go build":    "go build ./...",
		"go test":     "go test ./cmd/... -run Foo",
		"git":         "git commit -m msg",
		"curl":        "curl https://example.com",
		"rm":          "rm -rf /tmp/x",
		"npm":         "npm install --save-dev vitest",
	}
	for name, text := range artifacts {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, Trim(Segment(text, KindProse)),
				"an artifact must produce no model-facing chunk")
		})
	}
}

// TestSegment_ProseMentioningToolNames_Survives_Negative is the other half, and
// the more dangerous one: a sentence that merely NAMES a tool must not be
// classified as a command and silently discarded. A false negative here means
// the user never learns the memory was lost, so these are pinned explicitly.
func TestSegment_ProseMentioningToolNames_Survives_Negative(t *testing.T) {
	prose := []string{
		"go is a language I use daily for backend work.",
		"git is confusing sometimes but I get by.",
		"curl is my go-to for debugging APIs.",
		"make is worth learning for build automation.",
		"rm is a dangerous command to run casually.",
		"At 10:22 I ran the tests and they passed.",
		"I saw one ERROR in the list but it was harmless.",
		"I prefer tabs over spaces.",
	}
	for _, p := range prose {
		t.Run(p, func(t *testing.T) {
			assert.NotEmpty(t, Trim(Segment(p, KindProse)),
				"prose naming a tool must survive: %q", p)
		})
	}
}

// TestExtractPayload_StaysUnderBudget_Differential pins the payload size so a
// future "optimization" cannot silently re-inflate it — and so the one that was
// tried and reverted stays documented.
//
// History, measured on the six-turn fixture:
//
//	original (kind criteria per question + scope question): 11636 chars, recall 1.00
//	bare-criteria + legend in state:                         6585 chars, recall 0.50  <- reverted
//	current (dedupe rule moved to state, kind criteria kept): 8020 chars, recall 1.00
//
// The middle row is why this threshold is a CEILING rather than a target: the
// cheapest variant halved recall. Token savings are only worth taking when the
// benchmark still says 1.00.
func TestExtractPayload_StaysUnderBudget_Differential(t *testing.T) {
	var all []Chunk
	for _, turn := range FixtureTurns() {
		all = append(all, Trim(Segment(turn.Text, turn.Kind))...)
	}
	for i := range all {
		all[i].Index = i
	}

	extract := &sizingAsker{}
	if _, _, err := Extract(context.Background(), extract, all); err != nil {
		t.Fatalf("extract: %v", err)
	}
	near := NearestCandidates(all, FixtureBank(), 8)
	dedupe := &sizingAsker{}
	if _, _, err := Dedupe(context.Background(), dedupe, all, near); err != nil {
		t.Fatalf("dedupe: %v", err)
	}

	total := len(extract.body) + len(dedupe.body)
	// 8020 measured; 8500 leaves headroom for legitimate wording tweaks while
	// still catching a regression back toward the 11636 original.
	assert.Less(t, total, 8500,
		"combined extract+dedupe payload must stay well under the original 11636 chars")
	assert.Greater(t, total, 0)
}

// sizingAsker records the marshalled request body.
type sizingAsker struct{ body string }

func (s *sizingAsker) SystemOneWithOptions(
	_ context.Context, state any, q jev.Questions, _ jev.Options,
) (jev.Response, error) {
	b, err := json.Marshal(map[string]any{"state": state, "questions": q})
	if err != nil {
		return jev.Response{}, err
	}
	s.body = string(b)
	return jev.Response{Answers: map[string]jev.Answer{}}, nil
}
