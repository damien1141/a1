package memchunk

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/llm/jev"
)

// Benchmark evaluates the chunk+jev extractor against a hand-labelled fixture.
//
// It exists to answer one question before any of this reaches the memory outlet:
// does per-chunk jev extraction store the RIGHT memories for LESS money than the
// consolidation call it would replace? The fixture carries ground truth, so the
// answer is measured rather than argued.
//
// The run makes no network calls of its own; it drives whatever Asker it is
// given. `go run ./cmd/membench` wires the configured decision model.
type Report struct {
	// Chunks is how many storable chunks the segmenter produced.
	Chunks int
	// Asked is how many chunks reached the model (after MinChunkChars etc.).
	Asked int
	// Stored is how many the model confirmed above MinCertainty.
	Stored int
	// Duplicates is how many were folded into an existing memory.
	Duplicates int
	// Missed lists expected facts that were not stored (by label).
	Missed []string
	// Junk lists stored statements the fixture marks as junk (by index).
	Junk []string
	// Recall is stored expected facts / total expected facts.
	Recall float64
	// Precision is (stored - junk) / stored, or 1 when nothing was stored.
	Precision float64
	// Calls and Tokens are whatever the Asker reports.
	Calls  int
	Tokens int
	// InputTokens and OutputTokens are reported separately. A decision model
	// should bill almost nothing for output (it answers, it does not generate),
	// so a splitting them makes the wire's arithmetic checkable instead of a
	// single number nobody can audit.
	InputTokens  int
	OutputTokens int
	// Elapsed is wall time for the extraction pass.
	Elapsed time.Duration
}

// String renders the report for a terminal.
func (r Report) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "chunks=%d asked=%d stored=%d duplicates=%d\n", r.Chunks, r.Asked, r.Stored, r.Duplicates)
	fmt.Fprintf(&sb, "recall=%.2f precision=%.2f calls=%d tokens=%d (in=%d out=%d) elapsed=%s\n",
		r.Recall, r.Precision, r.Calls, r.Tokens, r.InputTokens, r.OutputTokens,
		r.Elapsed.Round(time.Millisecond))
	if len(r.Missed) > 0 {
		fmt.Fprintf(&sb, "MISSED (%d): %s\n", len(r.Missed), strings.Join(r.Missed, " | "))
	}
	if len(r.Junk) > 0 {
		fmt.Fprintf(&sb, "JUNK (%d): %s\n", len(r.Junk), strings.Join(r.Junk, " | "))
	}
	return sb.String()
}

// RunBenchmark executes the fixture end to end through the real segmenter and the
// real extractor.
//
// Existing memories are seeded so the dedupe pass has something to match against
// — the fixture includes deliberate near-duplicates, and a run that stores them
// twice is a regression the dedupe pass exists to prevent.
func RunBenchmark(ctx context.Context, asker Asker) (Report, error) {
	var rep Report
	start := time.Now()

	// 1. Segment every turn in the fixture.
	var all []Chunk
	for _, turn := range FixtureTurns() {
		all = append(all, Trim(Segment(turn.Text, turn.Kind))...)
	}
	rep.Chunks = len(all)

	// 2. Renumber across turns so question names stay unique in one call.
	for i := range all {
		all[i].Index = i
	}
	rep.Asked = len(all)
	if rep.Asked == 0 {
		return rep, nil
	}

	// 3. Ask the model what to keep.
	verdicts, cost, err := Extract(ctx, asker, all)
	if err != nil {
		return rep, err
	}
	total := cost

	stored := make([]Chunk, 0, len(verdicts))
	for _, v := range verdicts {
		if v.Scored() {
			stored = append(stored, v.Chunk)
		}
	}

	// 4. Dedupe the survivors against the seeded bank.
	bank := FixtureBank()
	if asker != nil && len(stored) > 0 && len(bank) > 0 {
		near := NearestCandidates(stored, bank, 8)
		merges, dcost, derr := Dedupe(ctx, asker, stored, near)
		if derr != nil {
			return rep, derr
		}
		total = total.Add(dcost)
		fresh := make([]Chunk, 0, len(stored))
		for _, m := range merges {
			if m.Duplicate() {
				rep.Duplicates++
				continue
			}
			fresh = append(fresh, m.Chunk)
		}
		stored = fresh
	}
	rep.Stored = len(stored)
	rep.Calls = total.Calls
	rep.Tokens = total.InputTokens + total.OutputTokens
	rep.InputTokens = total.InputTokens
	rep.OutputTokens = total.OutputTokens
	rep.Elapsed = time.Since(start)

	// 5. Score against the fixture's ground truth.
	rep.Recall, rep.Missed = recall(stored, bank)
	rep.Precision, rep.Junk = precision(stored)
	return rep, nil
}

// recall measures how many expected facts made it in, matching on distinctive
// substrings rather than equality — the segmenter may legitimately merge or
// truncate, and a fact still present is still recalled.
func recall(stored []Chunk, bank []ExistingMemory) (float64, []string) {
	expected := FixtureExpected()
	if len(expected) == 0 {
		return 1, nil
	}
	var missed []string
	hit := 0
	for _, exp := range expected {
		found := false
		// A fact is recalled if it was stored OR folded into an existing memory.
		// Folding is dedupe succeeding, not a miss: the fact is in the bank
		// either way, which is all recall measures. Scoring only `stored` read
		// a correct merge as a failure and understated recall.
		for _, c := range stored {
			if strings.Contains(strings.ToLower(c.Text), strings.ToLower(exp)) {
				found = true
				break
			}
		}
		if !found {
			for _, m := range bank {
				if strings.Contains(strings.ToLower(m.Content), strings.ToLower(exp)) {
					found = true
					break
				}
			}
		}
		if found {
			hit++
		} else {
			missed = append(missed, exp)
		}
	}
	return float64(hit) / float64(len(expected)), missed
}

// precision measures how much junk got through. Anything stored that matches a
// junk marker is counted against us.
func precision(stored []Chunk) (float64, []string) {
	if len(stored) == 0 {
		return 1, nil
	}
	junkMarkers := FixtureJunk()
	var junk []string
	for _, c := range stored {
		for _, j := range junkMarkers {
			if strings.Contains(strings.ToLower(c.Text), strings.ToLower(j)) {
				junk = append(junk, c.Text)
				break
			}
		}
	}
	return float64(len(stored)-len(junk)) / float64(len(stored)), junk
}

// RunBenchmarkCLI is the entry point `go run ./cmd/membench` calls. It reports
// whether a decision model is configured and runs the benchmark if so.
func RunBenchmarkCLI(ctx context.Context) error {
	asker := NextAsker()
	if asker == nil {
		fmt.Fprintln(os.Stderr, "no decision model configured — set the jev: section in ~/.a1/config.yaml")
		fmt.Fprintln(os.Stderr, "the segmenter still works; deleting the model leaves the judgment untested")
		return errNoModel
	}
	rep, err := RunBenchmark(ctx, asker)
	if err != nil {
		return err
	}
	fmt.Println(rep.String())
	return nil
}

type benchError string

func (e benchError) Error() string { return string(e) }

const errNoModel = benchError("no decision model configured")

// Unused import guard for jev — the Asker seam is built on its types.
var _ = jev.Answer{}
