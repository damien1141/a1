package llm

import (
	"math"
	"strings"
	"unicode/utf8"
)

// Repetition-loop detection for model output, completed or still streaming.
// Ported from hermes-agent's agent/repetition_guard.py (issues #86581,
// #100716, #112764, #127234): a model in a degenerate repetition loop can
// spend its ENTIRE output budget echoing one fragment, and a provider may
// even end such a loop with a clean finish — so the guard runs both while
// streaming (cut the stream) and on completed text (refuse to persist the
// looped bytes). Deliberately conservative: only LONG verbatim repeats
// covering a majority of the fragment trip it.

const (
	// repetitionMinFragment is the length below which the check doesn't
	// run: short truncations trivially contain repeated tokens and are
	// legitimately continued.
	repetitionMinFragment = 400
	// repetitionWindow is the exact-repeat window size; far beyond
	// ordinary phrasing reuse (citations, headings, similar code).
	repetitionWindow = 60
	// repetitionMinCount is how often a window must repeat to be a signal
	// even for short fragments.
	repetitionMinCount = 5
	// repetitionDominanceRatio is the fraction of the text repeated
	// windows must cover for "repetition-dominated".
	repetitionDominanceRatio = 0.5
)

// RepetitionStopPathMinChars is the scale at which the completed-response
// (stop-path) check fires. The stop path DISCARDS a finished answer, so it
// only aborts at runaway scale: real stop-path loops run 80k-350k chars,
// while asked-for repeats ("say X 50 times", identical table rows,
// templated YAML) stay in the low KB and must be delivered.
const RepetitionStopPathMinChars = 16_000

// repetitionStreamTailChars bounds how much of a live stream's latest text
// one check reads, so a single check stays bounded however long the reply
// grows.
const repetitionStreamTailChars = 4 * RepetitionStopPathMinChars

// Sampling bounds keep the periodic-scan path linear in output size with a
// small, fixed multiplier. A dominant contiguous run necessarily crosses
// many of these evenly spaced anchors.
const (
	repetitionMaxAnchorSamples = 32
	repetitionMaxAnchorMatches = 8
)

// repetitionRunawayDistinctLineRatio: a runaway (as opposed to merely
// repetitive) multi-line partial must be mostly copies of a few lines.
// Batch-style output (distinct INSERT rows, similar table rows) shares long
// prefixes and trips the window scan, but every line is distinct; a loop
// re-emits the same line(s).
const repetitionRunawayDistinctLineRatio = 0.5

// RepetitionLoopPlaceholder is what gets persisted INSTEAD of a
// repetition-dominated reply. Replaying the looped bytes — as the assistant
// row's content — re-seeds the loop on the next request and the corruption
// survives restarts (hermes #112764); the model (and the user) only need
// to know the reply degenerated and was cut off.
const RepetitionLoopPlaceholder = "[the reply degenerated into a repetition loop and was interrupted]"

// IsRepetitionDominated reports whether a contiguous run of at least five
// exact repetitions covers at least half of text — the signature of a model
// repetition loop. Coverage is measured with the true period, so long
// multi-line loop units count fully. Fail-open for short input.
func IsRepetitionDominated(text string) bool {
	runes := []rune(text)
	n := len(runes)
	if n < repetitionMinFragment {
		return false
	}
	// Fast path: one normalized line duplicated enough to cover half the
	// fragment (the common echo shape).
	if lineRepetitionDominated(text, n) {
		return true
	}
	// The window-count scan catches loops whose repeats differ by a
	// counter or noise token; the periodic scan catches long exact units
	// whose 60-rune windows each recur too rarely.
	return windowCountDominated(runes, n) || periodicRunDominated(text, runes, n)
}

// windowCountDominated reports whether one 60-rune window recurs often
// enough to cover half of text.
func windowCountDominated(runes []rune, n int) bool {
	needed := max(repetitionMinCount,
		int(math.Ceil(float64(n)*repetitionDominanceRatio/float64(repetitionWindow))))
	counts := make(map[string]int, n/repetitionWindow*2+1)
	for i := 0; i+repetitionWindow <= n; i++ {
		key := string(runes[i : i+repetitionWindow])
		c := counts[key] + 1
		if c >= needed {
			return true
		}
		counts[key] = c
	}
	return false
}

// periodicRunDominated detects a dominant exact periodic run from evenly
// spaced anchors. Matching a 60-rune anchor at a later position supplies a
// candidate period; expanding the equality runes[i] == runes[i+period] in
// both directions recovers the full run, so coverage is measured using the
// true repeating unit rather than crediting every occurrence with only 60
// runes.
func periodicRunDominated(text string, runes []rune, n int) bool {
	maxStart := n - repetitionWindow
	if maxStart < 1 {
		return false
	}
	// runeOff[i] is the byte offset of runes[i] in text (runeOff[n] =
	// len(text)), so the anchor search can run in byte space with
	// strings.Index while every period/bound stays in rune units — the
	// units hermes's constants are tuned for.
	runeOff := make([]int, n+1)
	off := 0
	for i, r := range runes {
		runeOff[i] = off
		off += utf8.RuneLen(r)
	}
	runeOff[n] = off
	anchorAt := func(i int) string {
		return text[runeOff[i]:runeOff[i+repetitionWindow]]
	}
	// indexFrom finds the first rune index >= from where needle occurs,
	// or -1. A UTF-8 substring match always starts on a rune boundary, so
	// the binary search over runeOff lands exactly.
	indexFrom := func(needle string, from int) int {
		if from >= n {
			return -1
		}
		b := strings.Index(text[runeOff[from]:], needle)
		if b < 0 {
			return -1
		}
		byteIdx := runeOff[from] + b
		lo, hi := from, n
		for lo < hi {
			mid := int(uint(lo+hi) >> 1)
			if runeOff[mid] < byteIdx {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		return lo
	}

	step := max(1, (maxStart+repetitionMaxAnchorSamples-2)/(repetitionMaxAnchorSamples-1))
	starts := make([]int, 0, repetitionMaxAnchorSamples+1)
	for s := 0; s <= maxStart; s += step {
		starts = append(starts, s)
	}
	if starts[len(starts)-1] != maxStart {
		starts = append(starts, maxStart)
	}

	// Runs already expanded and rejected, as (left, right, period). A
	// later anchor inside one of them whose period is a multiple of that
	// run's period would re-walk the same run.
	type rejectedRun struct{ left, right, period int }
	var rejected []rejectedRun
	for _, start := range starts {
		anchor := anchorAt(start)
		searchFrom := start + 1
		for match := 0; match < repetitionMaxAnchorMatches; match++ {
			m := indexFrom(anchor, searchFrom)
			if m < 0 {
				break
			}
			period := m - start
			searchFrom = m + 1
			skip := false
			for _, r := range rejected {
				if r.left <= start && start < r.right && period%r.period == 0 {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			left, right := expandRun(runes, n, start, period, repetitionWindow)
			if right-left >= repetitionMinCount*period &&
				float64(right-left) >= float64(n)*repetitionDominanceRatio {
				return true
			}
			rejected = append(rejected, rejectedRun{left, right, period})
		}
	}
	return false
}

// expandRun expands one known-equal window to the [left, right) bounds of
// its exact periodic run.
func expandRun(runes []rune, n, start, period, matched int) (int, int) {
	left := start
	for left > 0 && runes[left-1] == runes[left-1+period] {
		left--
	}
	right := start + matched
	for right+period < n && runes[right] == runes[right+period] {
		right++
	}
	return left, right + period
}

// lineRepetitionDominated reports whether a single normalized line covers
// half the fragment via repeats. n is the fragment's rune count.
func lineRepetitionDominated(text string, n int) bool {
	counts := make(map[string]int)
	for _, line := range strings.Split(text, "\n") {
		if norm := strings.TrimSpace(line); norm != "" {
			counts[norm]++
		}
	}
	for line, c := range counts {
		if c >= repetitionMinCount && c*len([]rune(line)) >= int(float64(n)*repetitionDominanceRatio) {
			return true
		}
	}
	return false
}

// IsRunawayRepetition is stricter than IsRepetitionDominated: it also
// requires the runaway shape. An interrupt DROPS the partial when this
// fires, so a legitimately repetitive but correct reply (distinct batch
// rows) must not qualify: repeated windows have to dominate AND, when the
// text has line structure, at most half of its non-empty lines may be
// distinct.
func IsRunawayRepetition(text string) bool {
	if !IsRepetitionDominated(text) {
		return false
	}
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if norm := strings.TrimSpace(line); norm != "" {
			lines = append(lines, norm)
		}
	}
	if len(lines) < repetitionMinCount {
		// No line structure to judge by: a dominated single-line loop.
		return true
	}
	distinct := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		distinct[line] = struct{}{}
	}
	return len(distinct) <= int(float64(len(lines))*repetitionRunawayDistinctLineRatio)
}

// RunawayWatch applies the stop-path criterion WHILE one channel (visible
// text or reasoning) streams. A stream that keeps producing never reaches a
// completion check, and endpoints without an output cap keep a looping
// model going for as long as the turn lives (hermes #127234). The first
// check runs at RepetitionStopPathMinChars (asked-for repeats stay below
// it, as on the stop path) and the gap doubles until it reaches one tail
// window, then stays there. Each check reads only the tail, so the total
// work stays linear in the output; the watch also trims its held text to
// the tail on every check, so it never holds more than two tail windows
// plus the latest delta.
type RunawayWatch struct {
	parts     []string
	chars     int
	nextCheck int
}

// NewRunawayWatch builds a watch for one streaming channel.
func NewRunawayWatch() *RunawayWatch {
	return &RunawayWatch{nextCheck: RepetitionStopPathMinChars}
}

// Feed adds one delta and reports whether the channel has become a
// runaway loop.
func (w *RunawayWatch) Feed(text string) bool {
	if text == "" {
		return false
	}
	w.parts = append(w.parts, text)
	w.chars += len([]rune(text))
	if w.chars < w.nextCheck {
		return false
	}
	w.nextCheck = w.chars + min(w.chars, repetitionStreamTailChars)
	tail := tailRunes(strings.Join(w.parts, ""), repetitionStreamTailChars)
	w.parts = []string{string(tail)}
	return IsRunawayRepetition(string(tail))
}

// tailRunes keeps the last n runes of s.
func tailRunes(s string, n int) []rune {
	runes := []rune(s)
	if len(runes) <= n {
		return runes
	}
	return runes[len(runes)-n:]
}
