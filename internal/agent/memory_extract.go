package agent

import (
	"context"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/debuglog"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/memchunk"
	"github.com/damien1141/a1/internal/memstore"
	"github.com/damien1141/a1/internal/project"
)

// The memory outlet, after the extractor landed.
//
// Two paths write memories now, and they do different jobs:
//
//   - EXTRACTION (memchunk + the decision model) is the primary path. It runs
//     every N turns, splits the turn into statements, and asks the decision
//     model per statement whether it is worth keeping. It is additive: it
//     inserts, and it folds near-duplicates into existing rows. It never
//     rewrites or removes.
//
//   - CONSOLIDATION (the LLM completion path in memory_outlet.go) is the
//     maintenance pass. It is the only thing that UPDATES a superseded memory
//     or DELETEs a revoked one, so it stays — but it runs far less often,
//     because it costs a 4K-token generation and the extractor has already
//     captured the facts.
//
// Extraction is skipped entirely when no decision model is configured; the
// consolidation path then runs on its old cadence, which is exactly the
// pre-extractor behavior.

// extractionInterval is how many completed turns pass between extraction runs.
// One: extraction is cheap enough to run every turn, and doing so keeps memory
// fresh — a fact stated in turn 3 is recallable in turn 4.
const extractionIntervalTurns = 1

// maintenanceInterval is how many extraction runs pass between consolidation
// runs. Consolidation exists to correct and prune, not to discover, so it does
// not need to see every turn.
const maintenanceIntervalTurns = 10

// consolidateTurn is the outlet's entry point, called at every terminal turn.
// It dispatches to extraction, and periodically to consolidation.
func (engine *Engine) extractTurn(userPrompt string, assistant llm.Message) {
	// Session valence (Bet 06): every terminal turn recomputes the session's
	// mood from its outcomes before any memory work fires.
	engine.updateSessionValence()
	if engine == nil {
		return
	}
	if !engine.MemoryEnabled() {
		return
	}
	// The store is resolved once and shared: both paths must write to the same
	// persona bank, and opening it twice would be pure waste.
	store := engine.memStoreForActivePersona()
	if store == nil {
		return
	}

	cfg := memoryConfig()
	if !cfg.ExtractorEnabled() {
		// Escape hatch: the previous behavior, byte for byte. The old cadence
		// (every N turns, resolved from the ops endpoint) still applies.
		if n := engine.consolidationInterval; n > 1 {
			if engine.consolidateTicks.Add(1)%int64(n) != 0 {
				return
			}
		}
		engine.consolidateSemanticMemories(userPrompt, assistant.Content, engine.recentUserMessages(maxRecentUserMessages))
		return
	}

	// Consolidation maintenance: the FIRST turn and then every Nth. It is the
	// only path that can UPDATE a superseded memory or DELETE a revoked one, so
	// it stays even when extraction does the discovery.
	//
	// The first turn is special on purpose. Waiting until turn N meant a session
	// shorter than N never consolidated at all — and when no decision model is
	// configured, extraction is a no-op, so those turns did NO memory work and a
	// short conversation remembered nothing. Firing on turn 1 makes today's
	// memories land immediately; the interval then handles the long tail.
	tick := engine.consolidateTicks.Add(1)
	maintained := maintenanceRunsOn(tick, engine.consolidationInterval)
	if maintained {
		engine.consolidateSemanticMemories(userPrompt, assistant.Content, engine.recentUserMessages(maxRecentUserMessages))
	}

	// Do not run both writers on the same turn. Consolidation has already been
	// handed this exact turn and will store its own statements; extraction
	// running alongside would ask the decision model about the same text and
	// write a second, differently-worded copy. The store's 0.90 cosine merge is
	// not a reliable guard — the two paths phrase things differently on purpose
	// (one summarizes, one quotes) — so the collision is avoided structurally
	// rather than deduplicated after the fact.
	//
	// Maintenance always wins the turn it fires on: it is the rarer, more
	// expensive path, and skipping it would defeat the cadence. Extraction
	// resumes on the next turn.
	if maintained {
		return
	}
	if tick%extractionIntervalTurns == 0 {
		engine.startExtraction(store, userPrompt, assistant)
	}
}

// startExtraction runs one extraction pass in the background.
//
// This MUST be asynchronous. runExtraction makes up to two decision-model round
// trips (extract, then dedupe) bounded by extractionTimeout; running it inline
// held the completed turn hostage for up to 20 seconds, which the consolidation
// path never did — it spawns a goroutine. Memory is background convenience and
// must never be something the user waits on.
//
// The task is registered in the same memoryOutlets map consolidation uses, so
// WaitForOutstandingOutlets (shutdown, and the tests) waits for both. A nil
// store or asker short-circuits before the spawn, so a turn with nothing to
// extract costs one goroutine at most.
func (engine *Engine) startExtraction(store *memstore.Store, userPrompt string, assistant llm.Message) {
	if engine == nil || store == nil {
		return
	}
	memoryOutletsMu.Lock()
	if len(memoryOutlets) >= maxOutstandingOutlets {
		memoryOutletsMu.Unlock()
		debuglog.Logf("memory-extract: skipping — %d outlets already in flight (max %d)",
			len(memoryOutlets), maxOutstandingOutlets)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), extractionTimeout)
	task := &outletTask{engine: engine, cancel: cancel}
	memoryOutlets[task] = true
	memoryOutletsMu.Unlock()

	go func() {
		defer func() {
			memoryOutletsMu.Lock()
			delete(memoryOutlets, task)
			memoryOutletsMu.Unlock()
			cancel()
		}()
		engine.runExtraction(ctx, store, userPrompt, assistant)
	}()
}

// runExtraction segments the turn and asks the decision model what to keep.
//
// Everything here is best-effort: a nil asker, a model error, a store error, or
// a dedupe failure all end the pass with a debuglog line and never touch the
// turn. Memory is a background convenience; it must never break a conversation.
func (engine *Engine) runExtraction(ctx context.Context, store *memstore.Store, userPrompt string, assistant llm.Message) {
	asker := memchunk.NextAsker()
	if asker == nil {
		// No decision model: nothing to judge with. Consolidation (if it ran
		// this tick) is the only writer, which is the pre-extractor behavior.
		return
	}

	// Collect the surfaces worth extracting, in the order a reader would want
	// them: what the user said, what the assistant answered, and what it thought.
	// The reasoning block is where "note that they prefer X" lives, and it was
	// discarded entirely before ReasoningContent was wired in here.
	var chunks []memchunk.Chunk
	appendChunks := func(text string, kind memchunk.Kind) {
		if strings.TrimSpace(text) == "" {
			return
		}
		for _, c := range memchunk.Segment(text, kind) {
			chunks = append(chunks, c)
		}
	}
	appendChunks(userPrompt, memchunk.KindProse)
	appendChunks(assistant.Content, memchunk.KindProse)
	appendChunks(assistant.ReasoningContent, memchunk.KindReasoning)

	chunks = memchunk.Trim(chunks)
	if len(chunks) == 0 {
		return // a code-only or empty turn costs nothing
	}
	if len(chunks) > memchunk.MaxChunks {
		chunks = chunks[:memchunk.MaxChunks]
	}
	for i := range chunks {
		chunks[i].Index = i
	}

	verdicts, _, err := memchunk.Extract(ctx, asker, chunks)
	if err != nil {
		debuglog.Logf("memory-extract: decision model failed: %v", err)
		return
	}

	keep := make([]memchunk.Verdict, 0, len(verdicts))
	for _, v := range verdicts {
		if v.Scored() {
			keep = append(keep, v)
		}
	}
	if len(keep) == 0 {
		return
	}

	// Dedupe against the nearest existing memories before writing. Without this
	// the store's own 0.90 cosine merge is the only guard, and it is a lexical
	// proxy that merges paraphrases too eagerly and distinct facts too rarely.
	if existing := engine.existingMemories(store); len(existing) > 0 {
		candidates := make([]memchunk.Chunk, 0, len(keep))
		for _, v := range keep {
			candidates = append(candidates, v.Chunk)
		}
		near := memchunk.NearestCandidates(candidates, existing, 8)
		if len(near) > 0 {
			merges, _, derr := memchunk.Dedupe(ctx, asker, candidates, near)
			if derr != nil {
				// Dedupe failing is not a reason to drop facts: store them and
				// let the store's cosine merge do what it can.
				debuglog.Logf("memory-extract: dedupe skipped: %v", derr)
			} else {
				dup := make(map[int]bool, len(merges))
				for _, m := range merges {
					if m.Duplicate() {
						dup[m.Chunk.Index] = true
					}
				}
				filtered := keep[:0]
				for _, v := range keep {
					if !dup[v.Chunk.Index] {
						filtered = append(filtered, v)
					}
				}
				keep = filtered
			}
		}
	}

	for _, v := range keep {
		if _, _, werr := store.Insert(ctx, memstore.NewTrace{
			Content:  v.Chunk.Text,
			Kind:     v.Kind,
			Scope:    v.Scope,
			Salience: v.Salience,
		}); werr != nil {
			debuglog.Logf("memory-extract: insert failed: %v", werr)
		}
	}
	debuglog.Logf("memory-extract: stored %d of %d candidate statements", len(keep), len(chunks))
}

// extractionTimeout bounds both decision calls of one extraction pass. A slow
// or wedged decision model must not leave goroutines stacked up behind a
// conversation.
const extractionTimeout = 20 * time.Second

// existingMemories projects the persona's bank for the dedupe pass, newest
// first and capped — the dedupe question count is candidates × existing, so an
// unbounded bank would be quadratic.
func (engine *Engine) existingMemories(store *memstore.Store) []memchunk.ExistingMemory {
	if store == nil {
		return nil
	}
	traces, err := store.List(dedupeBankLimit)
	if err != nil {
		return nil
	}
	out := make([]memchunk.ExistingMemory, 0, len(traces))
	for _, t := range traces {
		out = append(out, memchunk.ExistingMemory{ID: t.ID, Content: t.Content})
	}
	return out
}

// maintenanceRunsOn reports whether the consolidation maintenance pass fires on
// this turn number, given the engine's resolved cadence.
//
// The cadence is the engine's own (engine.consolidationInterval) rather than a
// constant here, because it already encodes the local-endpoint batching policy:
// a local ops endpoint resolves to localConsolidationInterval so the outlet does
// not evict a warm prompt cache every turn. Overriding that here would re-buy
// the re-prefill cost the batching exists to avoid.
//
// The one thing this adds is an ANCHOR: the count starts at turn 1 instead of
// turn N. A session shorter than the interval would otherwise never consolidate
// at all — and with no decision model configured extraction is a no-op, so
// those turns would do no memory work whatsoever and a short conversation would
// remember nothing. Anchoring at turn 1 means every session gets exactly one
// maintenance pass on its first completed turn, then settles into the interval:
// for interval 10 that is turns 1, 11, 21, … — one request per 10 turns, still.
func maintenanceRunsOn(tick int64, interval int) bool {
	if interval <= 1 {
		// A per-turn cadence (remote ops endpoint, or an explicitly pinned
		// interval of 1) means every completed turn maintains.
		return true
	}
	// Anchor: tick 1 fires, then every interval-th turn after it.
	return (tick-1)%int64(interval) == 0
}

// dedupeBankLimit caps how many stored memories the dedupe pass considers.
const dedupeBankLimit = 50

// memoryConfig reads the live memory section off the project. The type lives in
// project (it is a config section); this is only the accessor that tolerates an
// unloaded project.
func memoryConfig() project.MemoryConfig {
	proj := project.GetDefaultProject()
	if proj == nil || proj.Config() == nil {
		return project.MemoryConfig{}
	}
	return proj.Config().Memory
}
