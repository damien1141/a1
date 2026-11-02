package compaction

import (
        "fmt"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// TestSettingsFor_Invariants is the regression test for the math fix:
// SettingsFor must produce K, T, G that satisfy all four invariants across
// the realistic window range. The fixed DefaultSettings break INV1 for
// windows below ~96K (post-compaction estimate above the floor) and break
// INV3 for windows below 20K (kept zone exceeds the window).
//
// The invariants (see SettingsFor doc comment):
//
//      INV1: post < floor — K + T*r < W*f
//      INV2: floor + G <= W - T — growth gate fires at or below safety valve
//      INV3: K + T*r < W — compaction output fits in the window
//      INV4: K, T, G > 0
//
// We check each invariant for windows from 16K to 1M. Below 16K the mins
// dominate and the math degrades gracefully (the test acknowledges this by
// not asserting INV1/INV2 below 16K — INV3 and INV4 still hold).
func TestSettingsFor_Invariants(t *testing.T) {
        windows := []int{
                8_000,    // sub-minimum: mins dominate, degrades gracefully
                16_000,   // smallest realistic window (GPT-3.5-tier)
                32_000,
                64_000,   // the window the user reported broken
                96_000,
                128_000,  // Anthropic / OpenAI default tier
                200_000,
                256_000,
                512_000,
                1_000_000,
        }

        for _, w := range windows {
                t.Run(fmt.Sprintf("W=%dK", w/1000), func(t *testing.T) {
                        s := SettingsFor(w)
                        post := PostCompactionEstimate(s)
                        floor := int(float64(w) * s.FloorFraction)
                        safety := w - s.reverseTokens
                        growthTrigger := floor + s.GrowthThreshold

                        // INV4: all settings positive.
                        assert.Greater(t, s.keepRecentTokens, 0, "K > 0")
                        assert.Greater(t, s.reverseTokens, 0, "T > 0")
                        assert.Greater(t, s.GrowthThreshold, 0, "G > 0")

                        // INV3: compaction output fits in the window. This must hold
                        // even when the mins dominate, otherwise compaction itself
                        // would overflow the window.
                        assert.Less(t, post, w,
                                "INV3: post (%d) must fit in window (%d)", post, w)

                        // INV1 and INV2: the growth-gate headroom invariants. These
                        // hold for W >= ~12.8K (the threshold where the mins stop
                        // dominating). For smaller windows, the mins dominate and we
                        // accept graceful degradation — compaction still runs, the
                        // safety valve just fires more often than the growth gate.
                        if w < 16000 {
                                t.Logf("W=%d: below realistic minimum, INV1/INV2 may not hold (mins dominate)", w)
                                return
                        }

                        // INV1: post-compaction below floor.
                        assert.Less(t, post, floor,
                                "INV1: post (%d) must be below floor (%d) so the growth gate has headroom",
                                post, floor)

                        // INV2: growth-gate trigger at or below safety valve.
                        assert.LessOrEqual(t, growthTrigger, safety,
                                "INV2: growth trigger (%d) must be ≤ safety valve (%d) so the growth gate fires first",
                                growthTrigger, safety)
                })
        }
}

// TestSettingsFor_DefaultsMatchAt128K verifies the window-aware settings
// reproduce the original fixed defaults (within rounding) at the 128K
// reference window. This proves SettingsFor doesn't regress behavior for
// the most common window size — the original defaults were calibrated for
// 128K, so SettingsFor(128000) should produce the same operating point.
func TestSettingsFor_DefaultsMatchAt128K(t *testing.T) {
        s := SettingsFor(128_000)
        def := DefaultSettings()

        // K: 128000 * 0.15 = 19200 ≈ 20000 (original)
        assert.InDelta(t, def.keepRecentTokens, s.keepRecentTokens, 1000,
                "K at 128K should match original default within 1K")

        // T: 128000 * 0.125 = 16000 ≈ 16384 (original)
        assert.InDelta(t, def.reverseTokens, s.reverseTokens, 1000,
                "T at 128K should match original default within 1K")

        // G: 128000 * 0.30 = 38400 (original was 50000 = 39% — we relaxed to
        // 30% so INV2 holds for smaller windows, see SettingsFor doc)
        assert.InDelta(t, def.GrowthThreshold, s.GrowthThreshold, 15000,
                "G at 128K should be in the same order as original default")
}

// TestSettingsFor_ScalesProportionally verifies that doubling the window
// roughly doubles each setting (when above the mins). This catches
// off-by-constant bugs where a setting doesn't scale with W.
func TestSettingsFor_ScalesProportionally(t *testing.T) {
        small := SettingsFor(64_000)
        large := SettingsFor(128_000)

        // 128K is 2× 64K, so each setting should roughly 2× (the mins no
        // longer dominate at 64K — 64K*0.15=9600 > 2000, etc.).
        ratio := func(a, b int) float64 {
                if b == 0 {
                        return 0
                }
                return float64(a) / float64(b)
        }

        assert.InDelta(t, 2.0, ratio(large.keepRecentTokens, small.keepRecentTokens), 0.05,
                "K should scale ~2× when window doubles (64K → 128K)")
        assert.InDelta(t, 2.0, ratio(large.reverseTokens, small.reverseTokens), 0.05,
                "T should scale ~2× when window doubles (64K → 128K)")
        assert.InDelta(t, 2.0, ratio(large.GrowthThreshold, small.GrowthThreshold), 0.05,
                "G should scale ~2× when window doubles (64K → 128K)")
}

// TestSettingsFor_64K_FixesOriginalBug is the focused regression test for
// the user-reported issue: on a 64K window, the fixed defaults produced
// post (33107) > floor (28800), so the growth gate couldn't fire usefully.
// SettingsFor(64000) must produce post < floor.
func TestSettingsFor_64K_FixesOriginalBug(t *testing.T) {
        s := SettingsFor(64_000)
        post := PostCompactionEstimate(s)
        floor := int(float64(64_000) * s.FloorFraction)

        require.Less(t, post, floor,
                "on a 64K window, post-compaction estimate (%d) must be below the floor (%d) — "+
                        "the original fixed defaults produced post=33107 > floor=28800, breaking the growth gate",
                post, floor)

        // And the growth gate must be able to fire before the safety valve.
        growthTrigger := floor + s.GrowthThreshold
        safety := 64_000 - s.reverseTokens
        require.LessOrEqual(t, growthTrigger, safety,
                "growth-gate trigger (%d) must be ≤ safety valve (%d) on 64K",
                growthTrigger, safety)
}

// TestSettingsFor_ZeroWindowFallsBackToDefaults verifies the defensive
// fallback: a zero or negative window (which can happen if a model config
// is missing ContextWindow) returns the fixed defaults rather than
// producing nonsense settings.
func TestSettingsFor_ZeroWindowFallsBackToDefaults(t *testing.T) {
        s := SettingsFor(0)
        def := DefaultSettings()
        assert.Equal(t, def, s, "zero window falls back to fixed defaults")

        s = SettingsFor(-1)
        assert.Equal(t, def, s, "negative window falls back to fixed defaults")
}

// TestSettingsFor_Table pins the exact operating points from
// doc/context-folding-math.md §2. The doc's reference table and this test
// are the same numbers — if either changes, the other must change with it.
// The values are the int-truncated arithmetic the production code performs:
// K = max(0.15W, 2000), T = max(0.125W, 2048), G = max(0.30W, 5000),
// floor = int(0.45W), post = K + int(0.8·T).
func TestSettingsFor_Table(t *testing.T) {
        cases := []struct {
                w      int
                k, tld, g, floor, safety, post int
        }{
                {w: 16384, k: 2457, tld: 2048, g: 5000, floor: 7372, safety: 14336, post: 4095},
                {w: 32768, k: 4915, tld: 4096, g: 9830, floor: 14745, safety: 28672, post: 8191},
                {w: 65536, k: 9830, tld: 8192, g: 19660, floor: 29491, safety: 57344, post: 16383},
                {w: 131072, k: 19660, tld: 16384, g: 39321, floor: 58982, safety: 114688, post: 32767},
                {w: 262144, k: 39321, tld: 32768, g: 78643, floor: 117964, safety: 229376, post: 65535},
                {w: 1048576, k: 157286, tld: 131072, g: 314572, floor: 471859, safety: 917504, post: 262143},
        }
        for _, c := range cases {
                t.Run(fmt.Sprintf("W=%d", c.w), func(t *testing.T) {
                        s := SettingsFor(c.w)
                        assert.Equal(t, c.k, s.keepRecentTokens, "K")
                        assert.Equal(t, c.tld, s.reverseTokens, "T")
                        assert.Equal(t, c.g, s.GrowthThreshold, "G")
                        assert.Equal(t, c.floor, int(float64(c.w)*s.FloorFraction), "floor")
                        assert.Equal(t, c.safety, c.w-s.reverseTokens, "safety = W-T")
                        assert.Equal(t, c.post, PostCompactionEstimate(s), "post = K + r·T")
                })
        }
}

// TestSettingsFits asserts INV5 from doc/context-folding-math.md §2: the
// summarization call itself must fit the window on both trigger paths —
// prompt bounded by the trigger size, output bounded by the summary cap.
//   - safety-valve path: (W-T) + 0.8·T = W - 0.2·T < W
//   - growth-gate path: (floor+G) + 0.8·T ≤ W   [fraction regime]
func TestSettingsFits(t *testing.T) {
        for _, w := range []int{16384, 20000, 32768, 40960, 65536, 131072, 262144, 1048576} {
                t.Run(fmt.Sprintf("W=%d", w), func(t *testing.T) {
                        s := SettingsFor(w)
                        summaryCap := int(float64(s.reverseTokens) * historySummaryRatio)

                        valvePrompt := w - s.reverseTokens // safety threshold
                        assert.Less(t, valvePrompt+summaryCap, w,
                                "INV5 valve: prompt (%d) + summary cap (%d) must fit W (%d)",
                                valvePrompt, summaryCap, w)

                        // Growth-gate path only in the fraction regime (W ≥ ~13.3K,
                        // where the growth-gate invariants hold).
                        if w >= 16000 {
                                gatePrompt := int(float64(w)*s.FloorFraction) + s.GrowthThreshold
                                assert.LessOrEqual(t, gatePrompt+summaryCap, w,
                                        "INV5 gate: prompt (%d) + summary cap (%d) must fit W (%d)",
                                        gatePrompt, summaryCap, w)
                        }
                })
        }
}

// TestINV2BoundaryMeasures the honest boundary claimed in
// doc/context-folding-math.md §2: INV2 (floor + G ≤ W − T) holds for
// W ≥ 13334; just below that, in a narrow band, the growth-gate trigger
// lands slightly above the safety threshold — harmless (the valve is a
// superset trigger) but documented rather than hidden.
func TestINV2Boundary(t *testing.T) {
        holdsAt := []int{13334, 14000, 16000}
        for _, w := range holdsAt {
                s := SettingsFor(w)
                floor := int(float64(w) * s.FloorFraction)
                assert.LessOrEqual(t, floor+s.GrowthThreshold, w-s.reverseTokens,
                        "INV2 must hold at W=%d", w)
        }

        // The band below: find the largest W < 13334 where INV2 breaks and
        // confirm the violation is small (a few hundred tokens), not structural.
        worst := 0
        for w := 12800; w < 13334; w += 8 {
                s := SettingsFor(w)
                floor := int(float64(w) * s.FloorFraction)
                if over := floor + s.GrowthThreshold - (w - s.reverseTokens); over > worst {
                        worst = over
                }
        }
        assert.LessOrEqual(t, worst, 600,
                "INV2 violation in the sub-13.3K band must stay small (measured worst: %d)", worst)
}
