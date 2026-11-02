# Context Folding Math

Verifiable math behind a1's context folding (compaction). Every table in this
document is asserted by machine-checked tests:

- `internal/session/compaction/setting_test.go` — `TestSettingsFor_Invariants`,
  `TestSettingsFor_Table` (the exact numbers below), `TestSettingsFits`
  (INV5).
- `internal/agent/engine_test.go` — window learning, halving, and multi-shot
  overflow recovery against fake servers.

If you change a constant here, change it in `setting.go` and the tests fail
until the doc, code, and tests agree again.

## 1. Operating point

Compaction ("folding") is governed by four parameters derived from the
model's context window `W` (tokens):

| Symbol | Meaning | Formula | Min |
|--------|---------|---------|-----|
| `K` | keepRecentTokens — newest context kept verbatim | `0.15·W` | 2000 |
| `T` | reverseTokens — headroom reserved from the window edge (safety-valve margin) | `0.125·W` | 2048 |
| `G` | GrowthThreshold — minimum growth since last compaction | `0.30·W` | 5000 |
| `f` | FloorFraction — growth-gate floor | `0.45` | — |

Derived values:

- `floor = f·W` — above this, the growth gate is *eligible*.
- `safety = W − T` — above this (provider-reported real tokens), the safety
  valve fires unconditionally.
- `post = K + r·T` where `r = 0.8` (historySummaryRatio) — expected context
  size right after a compaction.
- summary output cap: `0.8·T` (history), `0.5·T` (turn-prefix).

Two triggers, OR'd at every turn end:

1. **Growth gate** (primary, cheap, predictable): estimated context
   (bytes/4 ruler, see §3) > `floor` **and** growth since last compaction
   ≥ `G`.
2. **Safety valve** (backstop): provider-reported real context tokens >
   `safety`.

## 2. Invariants

In the fraction regime (W ≥ ~13.3K, where the minimums stop dominating):

- **INV1** `post < floor`: `0.15W + 0.8·0.125W = 0.25W < 0.45W` ✓ — after a
  compaction the context is below the floor, so the gate has headroom before
  it fires again.
- **INV2** `floor + G ≤ W − T`: `0.45W + 0.30W = 0.75W ≤ 0.875W` ✓ — the
  growth gate fires before the safety valve, so folding is led by the
  predictable gate, not the reactive valve.
- **INV3** `post < W`: `0.25W < W` ✓ — the compaction output fits the window.
- **INV5 Summarizer fit** — the summarization call itself must fit:
  - via safety valve: prompt ≤ `W−T`, output ≤ `0.8·T` ⇒ total ≤ `W−0.2T < W` ✓
  - via growth gate: prompt ≤ `floor+G = 0.75W`, output ≤ `0.1W` ⇒ `0.85W < W` ✓

Measured boundary (machine-checked, not hand-waved): INV1/INV3 hold at every
W ≥ 4096. INV2 holds for W ≥ 13334; in the band W ∈ [12800, 13334] the
floor+G point lands up to ~300 tokens above the safety threshold, so the
valve leads there instead of the gate — harmless (the valve is a superset
trigger), documented rather than hidden.

### Reference table (exact values, int-truncated as the code does)

| W | K | T | G | floor | safety (`W−T`) | post (`K+0.8T`) | INV1 | INV2 |
|---------|-------|-------|-------|--------|--------|-------|------|------|
| 16384 | 2457 | 2048 | 5000 | 7372 | 14336 | 4095 | ✓ | ✓ |
| 32768 | 4915 | 4096 | 9830 | 14745 | 28672 | 8191 | ✓ | ✓ |
| 65536 | 9830 | 8192 | 19660 | 29491 | 57344 | 16383 | ✓ | ✓ |
| 131072 | 19660 | 16384 | 39321 | 58982 | 114688 | 32767 | ✓ | ✓ |
| 262144 | 39321 | 32768 | 78643 | 117964 | 229376 | 65535 | ✓ | ✓ |
| 1048576 | 157286 | 131072 | 314572 | 471859 | 917504 | 262143 | ✓ | ✓ |

## 3. Rulers (never mix)

Two token rulers coexist by design:

- **Real tokens** — provider-reported usage. Used *only* by the safety valve,
  which compares against `W` (also real tokens). Like-for-like.
- **Bytes/4 estimate** — `chars+3 / 4` per message (`estimateMessageTokens`).
  Used by the growth gate, the cut logic, and `PostCompactionEstimate`
  (`lastCompactTokens` baseline). All three like-for-like.

Mixing them broke the growth calculation historically (issues.md #12):
subtracting a real-token baseline from a bytes/4 measurement produced
nonsense. For code-heavy content the bytes/4 ruler *overestimates* real
tokens by ~1.1–1.4×, which biases the gate to fire early — the safe
direction.

## 4. Window resolution (what W actually is)

Priority order, resolved once per engine, re-resolved on model switch:

1. **Explicit config** — `context_window:` in the model entry in
   `~/.a1/config.yaml`.
2. **Built-in preset** — the model catalog (`internal/project/model`).
3. **Probe** (only when 1–2 gave nothing and the host is loopback/private):
   - llama.cpp: `GET {origin}/props` → `default_generation_settings.n_ctx`
     (any `n_ctx` found in the document).
   - ollama: `POST {origin}/api/show` `{"model": name}` → any
     `*.context_length` / `context_length` key.
   - 1.5 s budget each; failure is silent and non-fatal.
4. **Assumed default** — `32768` when the window is still unknown after 1–3.
   Folding *must* run on unknown-window models; before this change W=0
   disabled both triggers entirely and the only folding left was the
   overflow-recovery path.
5. **Learned** — on a context-overflow API error that states the limit
   (`maximum context length is 16384 tokens`, `exceeds the limit of N`, …),
   the parsed limit becomes the runtime W (ground truth beats config).

Cost of the 32K assumption (state it, don't hide it): for a model whose real
window is `R`, folding runs ~`R/32K`× more often than optimal until the first
overflow teaches the real value (remote endpoints) or the probe returns it
(local ones). Set `context_window:` to skip the assumption entirely.

## 5. Overflow recovery (mid-turn)

When the stream errors with a context overflow mid-turn:

1. Parse the error. If it carries a limit N → learn `W = N` (persistent).
2. If it carries no number (llama.cpp: "request exceeds the available context
   size") → **turn-local** window for the retry cycle, halved from the
   current value (`W/2`, floor 4096). Turn-local because a numberless
   overflow is usually "one paste too big", not "the window is smaller than
   configured" — poisoning the persistent window from a transient cause
   would under-fold every later turn.
   - Halving shrinks `K` and `T` (they derive from W), which shrinks the
     post-compaction context, until the retry fits: at the 4096 floor,
     `post = 2000 + 0.8·2048 = 3638 < 4096` ✓.
3. Force-compact (skip thresholds, but no-op when nothing can be summarized)
   and retry the stream.
4. Up to **3 consecutive** recoveries per turn (was 1 — "a second overflow
   fails closed"). The counter resets after any successful stream round,
   because a success proves the context now fits. A recovery that cannot
   shrink anything surfaces the original error.

## 6. Worked example — 64K llama.cpp, continuous work

`llama-server -c 65536` → probe returns 65536 → `K=9830, T=8192, G=19660,
floor=29491, safety=57344, post=16383` (est-tokens):

- Session grows. At est ≈ 29.5K (real ≈ 21–27K, ruler overestimates) the
  growth gate fires: history summarized (output ≤ 0.8·8192 = 6553 tokens),
  context drops to ≈ 16.4K est.
- Next gate at est ≥ max(16383 + 19660, 29491) = 36043: a ~19.7K-est cycle
  of useful work between compactions, one summarizer call each.
- If a single round (big read, big paste) jumps past `safety = 57344` real
  tokens mid-turn, the stream overflows → force-compact → retry, up to 3×
  per turn, halving (65536 → 32768 → 16384) only when the error is
  numberless and the retry still does not fit.
- Steady state: the turn never dies of context exhaustion; the model loses
  only pre-compaction history, never the kept recent zone.

## 7. What is NOT covered

- The post-turn check granularity: growth is measured at turn end; a single
  mid-turn round can overshoot `safety` before any check runs — that is what
  overflow recovery absorbs.
- Mid-stream drops (network) are not retried beyond transport-level
  `DoWithRetry` (429/5xx/stale-conn); a drop after bytes have streamed
  yields a partial-message error by design, since replaying would duplicate
  already-rendered output.
- Token-exact accounting: the growth gate is deliberately estimate-based;
  exact counting would require a tokenizer per model family.
