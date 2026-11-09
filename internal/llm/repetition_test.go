package llm

import (
        "strings"
        "testing"
)

func TestIsRepetitionDominatedShortInputFailsOpen(t *testing.T) {
        if IsRepetitionDominated(strings.Repeat("ab", 100)) {
                // 200 runes < 400: never judged.
                t.Fatalf("short input must fail open")
        }
        if IsRepetitionDominated("") {
                t.Fatalf("empty input must fail open")
        }
}

func TestIsRepetitionDominatedSingleLineEcho(t *testing.T) {
        // The common echo shape: one line, repeated far past half the fragment.
        text := strings.Repeat("The build failed again. ", 40)
        if !IsRepetitionDominated(text) {
                t.Fatalf("a single-line echo loop must be dominated")
        }
        if !IsRunawayRepetition(text) {
                t.Fatalf("a single-line echo loop is runaway")
        }
}

func TestIsRepetitionDominatedPeriodicUnitLongerThanWindow(t *testing.T) {
        // A loop unit longer than the 60-rune window: each 60-rune window
        // recurs too rarely for the count scan, so only the periodic scan's
        // true-period coverage can catch it.
        unit := "Step 1: read the config. Step 2: validate the schema. Step 3: retry. "
        text := strings.Repeat(unit, 20)
        if !IsRepetitionDominated(text) {
                t.Fatalf("a long-unit periodic loop must be dominated")
        }
        if !IsRunawayRepetition(text) {
                t.Fatalf("a long-unit periodic loop is runaway")
        }
}

func TestIsRepetitionDominatedCounterVariants(t *testing.T) {
        // Window-count scan: repeats that differ by a counter token still
        // share their 60-rune windows.
        var b strings.Builder
        for i := 0; i < 40; i++ {
                b.WriteString("Attempting to rebuild the index because the previous attempt failed; ")
        }
        if !IsRepetitionDominated(b.String()) {
                t.Fatalf("near-identical repeats must trip the window scan")
        }
}

func TestIsRepetitionDominatedProseIsNotDominated(t *testing.T) {
        text := `Regular code review passes catch most issues before they land.
The reviewer reads the diff, asks about invariants, and checks tests.
Sometimes a second pass finds what the first one missed entirely.
Documentation updates ride along with the change they describe.
A well-scoped PR merges faster than a sprawling one ever will.
Release notes mention only what users can observe from outside.
Internal refactors stay invisible unless they change behavior.
The changelog is the contract between today and last quarter.`
        if IsRepetitionDominated(text) {
                t.Fatalf("ordinary prose must not be dominated")
        }
}

func TestIsRunawayRepetitionDistinctBatchRowsAreDelivered(t *testing.T) {
        // Batch-style output shares long prefixes but every line is distinct:
        // dominated-or-not, it must not qualify as runaway, because an
        // interrupt would DROP a correct reply.
        var b strings.Builder
        for i := 0; i < 60; i++ {
                b.WriteString("INSERT INTO events (id, kind, payload) VALUES (")
                b.WriteString(strings.Repeat("x", 20))
                b.WriteString(", '")
                // Every row carries a distinct key: batch output whose lines all
                // differ, however similar their shape.
                b.WriteString(strings.Repeat(string(rune('a'+i%26)), 30))
                b.WriteString("', '")
                b.WriteString(strings.Repeat("y", 40))
                b.WriteString("');\n")
        }
        text := b.String()
        if IsRunawayRepetition(text) {
                t.Fatalf("distinct batch rows must be delivered, not interrupted")
        }
}

func TestIsRunawayRepetitionAskedForRepeatsAtStopScaleShape(t *testing.T) {
        // Same-line repetition IS runaway regardless of the batch shape.
        text := strings.Repeat("I will not repeat myself. ", 1000)
        if !IsRunawayRepetition(text) {
                t.Fatalf("a true same-line loop at scale is runaway")
        }
}

func TestStopPathScaleGate(t *testing.T) {
        // Below the stop-path minimum the completed-reply check never fires,
        // even for a runaway shape: asked-for repeats stay in the low KB.
        small := strings.Repeat("ha ", 500) // 1500 runes, runaway-shaped
        if len([]rune(small)) >= RepetitionStopPathMinChars {
                t.Fatalf("fixture drifted above the stop-path minimum")
        }
        if !IsRunawayRepetition(small) {
                t.Fatalf("fixture should be runaway-shaped (sanity)")
        }
}

func TestRunawayWatchFeedsUntilLoop(t *testing.T) {
        w := NewRunawayWatch()
        unit := strings.Repeat("same sentence over and over. ", 40) // 1120 runes
        // Below the first check threshold: nothing fires.
        for i := 0; i < 5; i++ {
                if w.Feed(unit) {
                        t.Fatalf("fired before the first check threshold")
                }
        }
        // Cross RepetitionStopPathMinChars with pure loop content: fires.
        if !w.Feed(strings.Repeat(unit, 20)) {
                t.Fatalf("a stream of pure loop units must trip the watch")
        }
}

func TestRunawayWatchHoldsOnlyTheTail(t *testing.T) {
        w := NewRunawayWatch()
        // Feed well past two tail windows of healthy prose: no fire, and the
        // retained parts must stay bounded (one trimmed tail).
        para := strings.Repeat("distinct healthy prose with varied wording and no loops at all. ", 400)
        for i := 0; i < 4; i++ {
                w.Feed(para)
        }
        if w.Feed("") {
                t.Fatalf("empty delta never fires")
        }
        if len(w.parts) != 1 {
                t.Fatalf("watch should hold exactly the trimmed tail, held %d parts", len(w.parts))
        }
        if len([]rune(w.parts[0])) > repetitionStreamTailChars {
                t.Fatalf("held tail exceeds one window: %d", len([]rune(w.parts[0])))
        }
        // A loop that starts LATE in a long reply is still caught: the tail is
        // all loop by now. The check gap after the previous feed is one tail
        // window, so feed past it.
        if !w.Feed(strings.Repeat("loop loop loop loop loop loop loop loop ", 2500)) {
                t.Fatalf("a late-starting loop must trip the tail check")
        }
}

func TestRunawayWatchMultibyteLoop(t *testing.T) {
        w := NewRunawayWatch()
        unit := strings.Repeat("构建失败了，正在重试。", 60) // 660 runes, multibyte
        for i := 0; i < 40; i++ {
                if w.Feed(unit) {
                        return // fired — acceptable at any point past the threshold
                }
        }
        t.Fatalf("a multibyte echo loop must trip the watch")
}
