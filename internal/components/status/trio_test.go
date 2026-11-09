package status

import (
	"math"
	"math/rand"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
)

func TestSpinner_NilReceiverAndEmptyFrames_Negative(t *testing.T) {
	var sp *Spinner
	assert.NotPanics(t, func() { sp.Tick() }, "nil receiver Tick is a guarded no-op")
	assert.Equal(t, "⋯", sp.Glyph(), "nil spinner falls back to the ellipsis glyph")

	// A zero-value Spinner carries no frame table: it renders the fallback
	// glyph as a single cell instead of panicking on the empty slice.
	bare := &Spinner{}
	assert.Equal(t, "⋯", bare.Glyph())
	s := bare.Draw(components.DrawContext{Method: xui.WidthUnicode})
	assert.Equal(t, 1, s.Size.Width)
	assert.Equal(t, 1, s.Size.Height)
	assert.Equal(t, "⋯", s.Buffer[0].Char)

	// Huge positive frame indices stay in range: int64 modulo has room to
	// spare. math.MaxInt % 10 == 7 → the 8th braille frame.
	sp2 := NewSpinner(xui.Style{})
	sp2.Frame = math.MaxInt
	assert.Equal(t, "⠧", sp2.Glyph(), "MaxInt frame maps to frames[7]")

	// Negative frames wrap from the top instead of indexing out of range:
	// Go's % keeps the dividend's sign, so Frame -1 must land on frames[9]
	// (the last braille glyph), not index -1. Draw shares the table lookup.
	sp2.Frame = -1
	assert.Equal(t, "⠏", sp2.Glyph(), "-1 wraps to frames[9]")
	sp2.Frame = -11
	assert.Equal(t, "⠏", sp2.Glyph(), "-11 wraps to frames[9] as well")
	sp2.Frame = -1
	neg := sp2.Draw(components.DrawContext{Method: xui.WidthUnicode})
	assert.Equal(t, "⠏", neg.Buffer[0].Char, "Draw renders the wrapped frame, not a panic")

	// Flow text under a negative frame still walks every cluster in order.
	sp2.Frame = -7
	var got []string
	sp2.ForEachFlowCell("ab", func(ch string, _ bool) { got = append(got, ch) })
	assert.Equal(t, []string{"a", "b"}, got, "negative frame keeps cluster order")
}

func TestForEachFlowCell_DegenerateInput_Negative(t *testing.T) {
	sp := NewSpinner(xui.Style{})
	called := false
	sp.ForEachFlowCell("", func(string, bool) { called = true })
	assert.False(t, called, "empty text lights nothing")

	// A nil callback is dropped, not dereferenced.
	sp.ForEachFlowCell("text", nil)

	// A nil spinner still walks the text with the highlight parked at 0.
	var nilSp *Spinner
	var got []string
	nilSp.ForEachFlowCell("ab", func(ch string, _ bool) { got = append(got, ch) })
	assert.Equal(t, []string{"a", "b"}, got, "nil spinner keeps cluster order")
}

// naiveClusters is a rune-level reference: every non-mark rune starts a
// cluster and following combining marks (Mn/Me/Mc) attach to it — the
// segmentation rules the status corpus actually exercises.
func naiveClusters(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		start := i
		i += size
		for i < len(s) {
			r2, sz := utf8.DecodeRuneInString(s[i:])
			if !unicode.Is(unicode.Mn, r2) && !unicode.Is(unicode.Me, r2) && !unicode.Is(unicode.Mc, r2) {
				break
			}
			i += sz
		}
		out = append(out, s[start:i])
	}
	return out
}

// oracle: naive rune-cluster reference (above) on edge cases and randomized
// inputs from a fixed alphabet; graphemeClusters must split exactly where a
// non-mark rune starts, and re-joining the clusters must rebuild the input.
func TestGraphemeClusters_MatchesNaiveReference_Differential(t *testing.T) {
	edges := []string{
		"", "a", "e\u0301", "\u0301", "\u0301\u0301", "\u0301e",
		"日本語", "😀", "e\u0301e\u0301", "a日😀e\u0301", "😀\u0301日",
	}
	alphabet := []string{"a", "Z", "7", " ", "日", "本", "😀", "e", "\u0301"}
	rng := rand.New(rand.NewSource(11))
	corpus := append([]string{}, edges...)
	for range 250 {
		var sb strings.Builder
		for j := rng.Intn(14); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		corpus = append(corpus, sb.String())
	}
	for _, s := range corpus {
		got := graphemeClusters(s)
		assert.Equal(t, naiveClusters(s), got, "clusters(%q)", s)
		// Round-trip through the join: no cluster may be dropped or altered.
		assert.Equal(t, s, strings.Join(got, ""), "join(clusters(%q))", s)
	}
}

// oracle: naive lit-set — for head h over n clusters the lit positions are
// exactly {(h-d) mod n : d ∈ [0,flowTrail)}, derived as a set of offsets from
// the head rather than the implementation's per-index wrapped distance. The
// formula normalizes negative heads, so negative frames are fair game too.
func TestForEachFlowCell_MatchesNaiveLitSet_Differential(t *testing.T) {
	texts := []string{"Generating…", "run", "日本語😀", "e\u0301e\u0301e\u0301"}
	rng := rand.New(rand.NewSource(5))
	sp := NewSpinner(xui.Style{})
	for _, text := range texts {
		clusters := naiveClusters(text)
		n := len(clusters)
		require.NotZero(t, n, "text %q", text)
		for _, frame := range []int{0, 1, 2, 3, n - 1, n, 2*n + 1, rng.Intn(1000), -1, -11, -(1 + rng.Intn(1000))} {
			sp.Frame = frame
			wantLit := make([]bool, n)
			head := frame % n
			for d := range flowTrail {
				wantLit[((head-d)%n+n)%n] = true
			}
			var gotCh []string
			var gotLit []bool
			sp.ForEachFlowCell(text, func(ch string, lit bool) {
				gotCh = append(gotCh, ch)
				gotLit = append(gotLit, lit)
			})
			assert.Equal(t, clusters, gotCh, "frame %d text %q: cells must be the clusters in order", frame, text)
			assert.Equal(t, wantLit, gotLit, "frame %d text %q: lit set", frame, text)
			if n >= flowTrail {
				lit := 0
				for _, l := range gotLit {
					if l {
						lit++
					}
				}
				assert.Equal(t, flowTrail, lit, "frame %d text %q: trail length is constant", frame, text)
			}
		}
	}
}
