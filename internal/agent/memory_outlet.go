package agent

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/debuglog"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/memstore"
)

// memoryOutlets is the set of in-flight consolidation goroutines started by
// consolidateSemanticMemories. The engine keeps them registered so a
// shutdown can wait for them (currently best-effort: errors are logged
// via the engine's debug log).
//
// Mutex: the map is mutated by consolidateSemanticMemories (writer) and
// by the per-task done-callback (deleter). Both acquire
// memoryOutletsMu.
var (
	memoryOutlets   = map[*outletTask]bool{}
	memoryOutletsMu sync.Mutex
)

type outletTask struct {
	engine *Engine
	cancel context.CancelFunc
}

// consolidationTimeout caps how long an outlet goroutine can run before
// being cancelled. 30s matches Open WebUI's
// LLM_CONSOLIDATION_TIMEOUT_SEC; long enough for a slow local model,
// short enough that a stuck consolidation doesn't pile up forever.
const consolidationTimeout = 30 * time.Second

// maxRecentUserMessages is how many of the most recent user messages get
// passed to the consolidation LLM for pronoun/context resolution. The
// Open WebUI default is 7; we use 5 for tighter local-LLM context
// budgets.
const maxRecentUserMessages = 5

// maxOutstandingOutlets caps how many background consolidation goroutines
// can run concurrently. If a new consolidation would exceed this, we skip
// it rather than queue — a backlogged outlet pipeline is worse than a
// missed consolidation turn (the next user message will retry).
const maxOutstandingOutlets = 2

// maxConsolidationOps is a safety cap on the number of operations we'll
// execute from a single LLM response. If the LLM hallucinates a huge ops
// list, we don't want to thrash the SQLite store. Matches Open WebUI's
// 50-op cap.
const maxConsolidationOps = 50

// maxDeleteRatio is the maximum fraction of ops that may be DELETE before
// we reject the whole batch. Matches Open WebUI's 0.6 — a plan that
// tries to delete >60% of memories is suspicious (probably a confused
// LLM or a misread prompt).
const maxDeleteRatio = 0.6

// consolidateTurn fires the memory outlet for a COMPLETED turn: the user
// prompt plus the round's assistant text (and recent user messages for
// context) go to the background consolidation pipeline. Called at every
// terminal path where the model finished its turn — the normal no-tool
// wrap-up, the verdict gate's stall stop, an executor stop, the tool-call
// budget trip, and the declared-budget terminal stop. Turns aborted by
// errors, cancellation, or a declined continue-ask do NOT consolidate:
// the model never got to answer, so there is no exchange worth keeping.
//
// An early-stopped turn's assistant text can be thin (a bare tool-call
// round carries no prose), but the user message alone is still personal
// content the consolidation LLM can extract from — and the outlet's own
// skip detector plus the store's dedup keep noise out of the bank.
func (engine *Engine) consolidateTurn(userPrompt string, assistant llm.Message) {
	// Session valence (Bet 06): every terminal turn recomputes the
	// session's mood from its outcomes before consolidation fires.
	engine.updateSessionValence()
	if engine == nil {
		return
	}
	engine.consolidateSemanticMemories(userPrompt, assistant.Content, engine.recentUserMessages(maxRecentUserMessages))
}

// consolidateSemanticMemories spawns a background goroutine that runs
// the memory consolidation pipeline: take the latest user message + the
// assistant response + the recent conversation context, ask the LLM
// what CREATE/UPDATE/DELETE operations to perform against the persona's
// semantic memory store, dedup against existing memories, and execute.
//
// The goroutine is registered on the engine's memoryOutlets map so a
// future shutdown can wait for in-flight consolidations. The task is
// auto-removed when done (success or failure). Errors are silently
// logged — the user must not be interrupted by a failed memory write.
//
// The function returns immediately (does not block). When the active
// persona has memory disabled, or when the conversation is non-personal
// (skip detector flags code/URLs/math), the function is a no-op.
func (engine *Engine) consolidateSemanticMemories(userMsg, assistantMsg string, recentUserMsgs []string) {
	if engine == nil {
		return
	}
	store := engine.memStoreForActivePersona()
	if store == nil {
		debuglog.Logf("memory-outlet: skipping consolidation — no persona memory store (embedder=%v, personaStore=%v)",
			engine.embedder != nil, engine.personaStore() != nil)
		return
	}
	// Skip detection: if the user message AND assistant message both look
	// non-personal (code output, URLs, etc.), skip consolidation entirely.
	// We check both because the user might paste code as a "personal"
	// message ("here's my code, fix it") — we want to consolidate that the
	// user is working on this codebase, just not the code itself.
	if fastPathSkip(userMsg+assistantMsg) != "" && fastPathSkip(assistantMsg) != "" {
		debuglog.Logf("memory-outlet: skipping consolidation — skip detector flagged both messages as non-personal")
		return
	}
	// Backpressure: if too many outlet goroutines are in flight, skip this
	// turn. The next user message will retry.
	memoryOutletsMu.Lock()
	if len(memoryOutlets) >= maxOutstandingOutlets {
		memoryOutletsMu.Unlock()
		debuglog.Logf("memory-outlet: skipping consolidation — %d outlets already in flight (max %d)",
			len(memoryOutlets), maxOutstandingOutlets)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), consolidationTimeout)
	task := &outletTask{engine: engine, cancel: cancel}
	memoryOutlets[task] = true
	memoryOutletsMu.Unlock()

	debuglog.Logf("memory-outlet: spawning consolidation for %d-char user msg + %d-char assistant msg",
		len(userMsg), len(assistantMsg))

	go func() {
		defer func() {
			memoryOutletsMu.Lock()
			delete(memoryOutlets, task)
			memoryOutletsMu.Unlock()
			cancel()
		}()
		engine.runConsolidationPipeline(ctx, store, userMsg, assistantMsg, recentUserMsgs)
	}()
}

// runConsolidationPipeline does the actual work: build the prompt,
// call the LLM, parse the ops, execute them against the store.
//
// Errors at every step are silent — they're logged via the engine's
// debug log but never returned to the caller (this runs in a background
// goroutine with no caller to surface errors to).
func (engine *Engine) runConsolidationPipeline(
	ctx context.Context,
	store *memstore.Store,
	userMsg, assistantMsg string,
	recentUserMsgs []string,
) {
	// 1. Retrieve existing candidate memories that might be UPDATEd or
	//    DELETEd. We use ModeCompanion (cross-project within this
	//    persona) so the LLM sees all of the persona's relevant context.
	now := time.Now()
	cues := memstore.Cues{
		Project: "",
		Hour:    float64(now.Hour()) + float64(now.Minute())/60.0,
		// Session valence (Bet 06): mood-congruent candidate
		// selection for consolidation too.
		Valence: engine.loadSessionValence(),
	}
	candidates, _ := store.Retrieve(ctx, userMsg, memstore.ModeCompanion, cues, now)
	candidateIDs := make(map[string]int64, len(candidates))
	var candidateBlock strings.Builder
	candidateBlock.WriteString("Existing memories for consolidation:\n")
	if len(candidates) == 0 {
		candidateBlock.WriteString("[]\n\nNote: No existing memories found - focus on extracting new memories.\n\n")
	} else {
		for _, t := range candidates {
			idStr := fmt.Sprintf("mem-%d", t.ID)
			candidateIDs[idStr] = t.ID
			content := t.Content
			if len(content) > 100 {
				content = content[:97] + "..."
			}
			candidateBlock.WriteString(fmt.Sprintf("[id:%s] %s\n", idStr, content))
		}
		candidateBlock.WriteString("\n")
	}

	// 2. Build the conversation context block. We pass the recent N user
	//    messages (newest last) so the LLM can resolve pronouns and
	//    understand the conversational arc — but we cap each at 1000
	//    chars to keep the prompt within local-LLM budgets.
	var convoBlock strings.Builder
	convoBlock.WriteString("Conversation (recent user messages for context, newest last):\n")
	if len(recentUserMsgs) == 0 {
		convoBlock.WriteString("(none)\n")
	}
	for i, msg := range recentUserMsgs {
		if len(msg) > 1000 {
			msg = msg[:997] + "..."
		}
		convoBlock.WriteString(fmt.Sprintf("[%d] %s\n", i+1, msg))
	}
	convoBlock.WriteString("\nLatest user message:\n")
	um := userMsg
	if len(um) > 4000 {
		um = um[:3997] + "..."
	}
	convoBlock.WriteString(um)
	convoBlock.WriteString("\n\nLatest assistant response:\n")
	am := assistantMsg
	if len(am) > 4000 {
		am = am[:3997] + "..."
	}
	convoBlock.WriteString(am)

	// 3. Build the prompt and call the LLM. We use client.Compact
	//    (not Stream) because Compact does a clean one-shot call
	//    with no system prompt, no tools, and a single user message —
	//    the agent's main system prompt + tool list don't leak in.
	//    We prepend the consolidation system prompt to the user
	//    message as a single text block.
	//
	//    For v1 we DO reuse engine.client (the agent's main LLM). v2:
	//    spin up a dedicated client per persona.Model config so a
	//    "work" persona can route consolidation to a fast 8B model
	//    while the user-facing assistant is on a 70B.
	promptText := memoryConsolidationPrompt() + "\n\n" +
		candidateBlock.String() + convoBlock.String()
	// Memory-ops model (Bet 06): the persona's dedicated MemoryModel
	// (when configured) does the consolidation, freeing the main LLM.
	resp, err := engine.memoryConsolidationClient().Compact(ctx, llm.CompactRequest{
		Prompt:    promptText,
		MaxTokens: 4096,
	})
	if err != nil {
		debuglog.Logf("memory-outlet: LLM Compact failed: %v", err)
		return // silent failure
	}
	finalContent := resp.Text
	if strings.TrimSpace(finalContent) == "" {
		debuglog.Logf("memory-outlet: LLM Compact returned empty response")
		return
	}

	// 5. Parse the JSON ops. The prompt instructs the LLM to return
	//    {"ops":[{"operation":"CREATE|UPDATE|DELETE","id":"","content":"","kind":"","scope":""}]}
	//    If the LLM produces invalid JSON or a non-ops structure, we
	//    skip — better to miss a consolidation than to miswrite memory.
	ops, err := parseConsolidationOps(finalContent)
	if err != nil {
		return
	}
	if len(ops) == 0 {
		debuglog.Logf("memory-outlet: LLM returned 0 ops — nothing to consolidate")
		return
	}
	if len(ops) > maxConsolidationOps {
		ops = ops[:maxConsolidationOps]
	}
	// Safety: reject the batch if delete ratio is suspiciously high.
	deleteCount := 0
	for _, op := range ops {
		if op.Operation == "DELETE" {
			deleteCount++
		}
	}
	if len(ops) >= 6 && float64(deleteCount)/float64(len(ops)) > maxDeleteRatio {
		debuglog.Logf("memory-outlet: rejecting batch — %d/%d ops are DELETE (%.0f%% > %.0f%% cap)",
			deleteCount, len(ops), float64(deleteCount)/float64(len(ops))*100, maxDeleteRatio*100)
		return
	}

	// 6. Execute the ops against the store. memstore.Insert already has
	//    built-in dedup at 0.90 similarity (auto-merge), so CREATE ops
	//    that match an existing memory will merge rather than duplicate.
	//    UPDATE/DELETE use the candidate IDs we resolved earlier.
	debuglog.Logf("memory-outlet: executing %d ops (%d CREATE, %d UPDATE, %d DELETE)",
		len(ops), len(ops)-deleteCount, deleteCount, deleteCount)
	for _, op := range ops {
		execConsolidationOp(ctx, store, op, candidateIDs)
	}
	debuglog.Logf("memory-outlet: consolidation complete")
}

// memoryConsolidationOp is one operation parsed from the LLM's JSON
// response. Tags are loose (we tolerate missing fields) because LLMs
// don't always produce strict JSON.
type memoryConsolidationOp struct {
	Operation string `json:"operation"`
	ID        string `json:"id"`
	Content   string `json:"content"`
	Kind      string `json:"kind"`
	Scope     string `json:"scope"`
}

// parseConsolidationOps extracts the ops array from the LLM's JSON
// response. Tolerates leading/trailing whitespace and markdown fences
// (```json...```) that some models wrap around JSON. Returns nil + a
// non-nil error when the response isn't valid JSON or doesn't contain
// an ops array.
func parseConsolidationOps(raw string) ([]memoryConsolidationOp, error) {
	// Strip markdown code fences if present.
	body := strings.TrimSpace(raw)
	if strings.HasPrefix(body, "```") {
		// Find the first newline (after the opening fence) and the
		// closing fence.
		if idx := strings.Index(body, "\n"); idx > 0 {
			body = body[idx+1:]
		}
		if idx := strings.LastIndex(body, "```"); idx >= 0 {
			body = body[:idx]
		}
		body = strings.TrimSpace(body)
	}
	// Extract the first { and last } — the LLM may include preface text
	// like "Here are the operations:" before the JSON.
	start := strings.Index(body, "{")
	end := strings.LastIndex(body, "}")
	if start < 0 || end < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found")
	}
	body = body[start : end+1]

	var parsed struct {
		Ops []memoryConsolidationOp `json:"ops"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return nil, err
	}
	return parsed.Ops, nil
}

// execConsolidationOp executes a single CREATE/UPDATE/DELETE op against
// the store. Errors are silently ignored — a single failed op doesn't
// abort the batch.
func execConsolidationOp(
	ctx context.Context,
	store *memstore.Store,
	op memoryConsolidationOp,
	candidateIDs map[string]int64,
) {
	content := strings.TrimSpace(op.Content)
	kind := strings.TrimSpace(op.Kind)
	scope := strings.TrimSpace(op.Scope)
	if kind == "" {
		kind = "fact"
	}
	if scope == "" {
		scope = "user"
	}
	switch strings.ToUpper(op.Operation) {
	case "CREATE":
		if content == "" {
			return
		}
		_, _, _ = store.Insert(ctx, memstore.NewTrace{
			Content:  content,
			Kind:     kind,
			Scope:    scope,
			Salience: 0.3, // default; the LLM doesn't produce salience yet
		})
	case "UPDATE":
		id, ok := candidateIDs[op.ID]
		if !ok || content == "" {
			return
		}
		_ = store.Revise(ctx, id, content, "auto-consolidation")
	case "DELETE":
		id, ok := candidateIDs[op.ID]
		if !ok {
			return
		}
		_ = store.Forget(id)
	}
}

// memoryConsolidationPrompt returns the system prompt for the
// consolidation LLM call. Loaded from
// internal/agent/prompt/memory-consolidation.tmpl at build time via
// go:embed.
//
//go:embed prompt/memory-consolidation.tmpl
var memoryConsolidationTmpl string

func memoryConsolidationPrompt() string {
	return memoryConsolidationTmpl
}

// WaitForOutstandingOutlets blocks until all in-flight consolidation
// goroutines complete (or until ctx is cancelled). Called by the CLI
// on shutdown so SQLite connections flush cleanly. Safe to call
// multiple times; safe to call when no outlets are in flight (returns
// immediately).
func (engine *Engine) WaitForOutstandingOutlets(ctx context.Context) error {
	for {
		memoryOutletsMu.Lock()
		count := len(memoryOutlets)
		memoryOutletsMu.Unlock()
		if count == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
