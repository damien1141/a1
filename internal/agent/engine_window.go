package agent

import (
        "context"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/util"
)

// maxConsecutiveOverflowRecoveries bounds back-to-back overflow recoveries
// within one turn. The counter resets after any successful stream round — a
// success proves the context fits — so three CONSECUTIVE failures means
// compaction is no longer shrinking the context (or the real window sits
// below the halving floor) and the original error must surface instead of
// looping. See doc/context-folding-math.md §5.
const maxConsecutiveOverflowRecoveries = 3

// WindowProber discovers a context window for a model from its endpoint.
// Production uses the llama.cpp / ollama local probe (llm.ProbeContextWindow);
// tests inject a deterministic stub via WithWindowProber.
type WindowProber func(ctx context.Context, baseURL, model string) int

func defaultWindowProber(ctx context.Context, baseURL, model string) int {
        return llm.ProbeContextWindow(ctx, util.DefaultHTTPClient(), baseURL, model)
}

// ensureWindow resolves the engine's effective runtime context window, once.
// Resolution order (doc/context-folding-math.md §4): explicit config/preset
// value (already in ctxWindow — nothing to do), local endpoint probe, then
// the assumed fallback. Folding must run even when the window is unknown;
// before this existed, window 0 disabled both compaction triggers entirely.
func (engine *Engine) ensureWindow(ctx context.Context) {
        if engine == nil {
                return
        }
        if engine.windowResolved.Load() {
                return
        }
        if w := engine.ctxWindow.Load(); w > 0 {
                engine.windowResolved.Store(true)
                return
        }
        prober := engine.windowProber
        if prober == nil {
                prober = defaultWindowProber
        }
        if w := prober(ctx, engine.modelCfg.BaseURL, engine.modelCfg.Name); w > 0 {
                engine.ctxWindow.Store(int64(w))
                engine.windowResolved.Store(true)
                return
        }
        engine.ctxWindow.Store(llm.AssumedContextWindow)
        engine.windowResolved.Store(true)
}

// ContextWindow returns the effective runtime context window (0 before the
// first resolution). The value reflects learning: config → probe → assumed →
// provider-stated limit from an overflow error. Surfaces (TUI footer, web
// status) read this so the displayed denominator tracks reality.
func (engine *Engine) ContextWindow() int {
        if engine == nil {
                return 0
        }
        return int(engine.ctxWindow.Load())
}

// ModelName returns the engine's currently active model name — the base
// config model, or the active persona's override after a persona switch.
// The web state view reads this so the footer's model chip follows persona
// switches instead of staying pinned to the config default.
func (engine *Engine) ModelName() string {
        if engine == nil {
                return ""
        }
        return engine.modelCfg.Name
}

// setContextWindow records a runtime-learned window from a provider-stated
// limit. The provider measured it, so it is ground truth and overrides
// config/preset/assumption for the rest of the engine's life; a model switch
// re-seeds everything via resetWindowForModel.
func (engine *Engine) setContextWindow(n int) {
        if engine == nil || n <= 0 {
                return
        }
        engine.ctxWindow.Store(int64(n))
        engine.windowResolved.Store(true)
}

// resetWindowForModel re-seeds the runtime window after a model switch: an
// explicit config/preset window resolves immediately; an unknown window
// re-arms the probe + assumed fallback on the next turn.
func (engine *Engine) resetWindowForModel(cfg llm.ModelConfig) {
        if engine == nil {
                return
        }
        w := cfg.ContextWindow
        if w < 0 {
                w = 0
        }
        engine.ctxWindow.Store(int64(w))
        engine.windowResolved.Store(w > 0)
}
