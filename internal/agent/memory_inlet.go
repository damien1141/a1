package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/memstore"
	"github.com/damien1141/a1/internal/persona"
)

// memStoreForActivePersona returns the semantic memory store for the
// currently active persona, opening it lazily if needed. Mirrors the
// JSONL bank's pattern in session.MemoryBank(): re-evaluates on every call
// so a mid-session persona switch immediately retargets reads/writes.
//
// Returns nil (no error) when:
//   - memory is effectively off — Engine.MemoryEnabled() folds the surface's
//     raw toggle together with the persona's explicit memory_enabled, so
//     either one turning off silences the auto-injection AND the
//     auto-consolidation (the remember/recall tools are separately gated on
//     the same MemoryEnabled() in buildCoreTools)
//   - no persona store is attached to the engine → fall back to engine.memStore
//     (the legacy path used when personas are disabled in config; a nil
//     memStore there means "off" and correctly yields no auto memory)
//   - no embedder is configured (can't open a vector store without one)
//
// The returned *memstore.Store is cached on the persona.Store keyed by
// persona ID, so this is cheap to call per-turn (one map lookup after the
// first open).
func (engine *Engine) memStoreForActivePersona() *memstore.Store {
	if engine == nil {
		return nil
	}
	pstore := engine.personaStore()
	if pstore == nil {
		// No persona store attached → fall back to the engine-level memStore
		// (the legacy path, e.g. when personas are disabled in config).
		return engine.memStore
	}
	p, err := pstore.ActivePersona()
	if err != nil {
		return engine.memStore
	}
	// The single effective-memory gate. A persona with an explicit
	// memory_enabled wins over the surface toggle; a persona without one
	// inherits it. Either way, "off" means no auto-injection and no
	// auto-consolidation — the explicit remember/recall tools stay
	// available (when a shared bank exists) but nothing automatic fires.
	if !engine.MemoryEnabled() {
		return nil
	}
	emb := engine.embedder
	if emb == nil {
		// No embedder configured — can't open a per-persona vector store.
		// Fall back to engine.memStore (legacy) if it's set, so memory
		// tools still work even when auto-injection is silently disabled.
		return engine.memStore
	}
	// SOUL wiring (Bet 06): a fenced ```soul block in the persona's
	// system prompt parses into memstore.Params — a nostalgic persona
	// (d 0.35) forgets slowly; an eager one (tau -1.5) recalls more.
	// Absent block = defaults, so every existing persona is unchanged.
	params := memstore.DefaultParams()
	if soul, ok := memstore.ParseSoulParams(p.SystemPrompt); ok {
		params = soul
	}
	store, err := pstore.SemanticStore(p, emb, params)
	if err != nil {
		// Open failure: fall back to engine-level store if it exists,
		// so a broken persona store doesn't take down the whole session.
		return engine.memStore
	}
	return store
}

// personaStore returns the persona.Store attached to the engine via the
// session, or nil when no session or no persona store is attached.
func (engine *Engine) personaStore() *persona.Store {
	if engine == nil || engine.session == nil {
		return nil
	}
	return engine.session.personaStore
}

// injectSemanticMemories retrieves the top-K relevant memories from the
// active persona's semantic store and returns a system message for
// injection before the latest user message. Returns nil when:
//   - memory is disabled on the active persona (memStoreForActivePersona
//     returns nil)
//   - the SkipDetector flags the message as non-personal (code, URLs, math)
//   - no relevant memories found above the activation threshold
//
// The returned message is NOT persisted to the session; it's a transient
// injection rebuilt every turn so memories written mid-conversation
// surface immediately on the next user message.
//
// Errors are silent: a broken memory subsystem never breaks the agent
// loop. The worst case is "no memories injected this turn", which is the
// same as the pre-feature baseline.
func (engine *Engine) injectSemanticMemories(ctx context.Context, userPrompt string) *llm.Message {
	if engine == nil {
		return nil
	}
	if strings.TrimSpace(userPrompt) == "" {
		return nil
	}
	store := engine.memStoreForActivePersona()
	if store == nil {
		return nil
	}
	// Skip detection: structural patterns only (v1). If the message is
	// code, command output, JSON, URL lists, etc. — skip retrieval
	// entirely. These messages have no semantic value to surface and the
	// LLM consolidation should also skip them (see outlet).
	if reason := fastPathSkip(userPrompt); reason != "" {
		return nil
	}
	// Pick the mode based on whether we're inside a git repo. ModeProject
	// filters to project-scoped + global + identity/beat/affect/bit
	// memories; ModeCompanion returns cross-project user-scoped memories
	// only. When no git root is detected, we use ModeCompanion so personal
	// facts (identity, preferences) still surface.
	mode := memstore.ModeCompanion
	project := ""
	if cwd := engine.SessionCwd(); cwd != "" {
		if ws := findGitRoot(cwd); ws != "" {
			mode = memstore.ModeProject
			project = ws
		}
	}
	now := time.Now()
	cues := memstore.Cues{
		Project: project,
		Hour:    float64(now.Hour()) + float64(now.Minute())/60.0,
		// Session valence (Bet 06): the mood computed at the last
		// turn's end now reaches selectBundle's §3.5 override.
		Valence: engine.loadSessionValence(),
	}
	traces, err := store.Retrieve(ctx, userPrompt, mode, cues, now)
	if err != nil {
		return nil
	}
	// Episodic recall (Bet 06): the project's most recent episodes
	// ("last time this happened") ride along with the semantic
	// memories through the same splice point. Empty for fresh
	// projects or when the vault is not live.
	episodeBlock := ""
	if f := engine.SessionFile(); f != "" {
		episodeBlock = recallEpisodes(engine, filepath.Dir(f))
	}
	if len(traces) == 0 && episodeBlock == "" {
		return nil
	}
	// Format as a system message block. Third-person framing ("a memory
	// about the user") on purpose: some LLMs read a generic "Relevant
	// memories" or second-person "your memories" header as "things I
	// (the AI) said/did" rather than "stored facts about this user",
	// and then attribute them to their own past turns. Naming the user
	// as the subject fixes the misattribution, and the "do not
	// mention" instruction prevents the model from saying "as you
	// mentioned earlier..." when it actually got the fact via
	// injection, not a user statement.
	var sb strings.Builder
	sb.WriteString(
		"The following are memories about the user, loaded from persistent storage. Use them for personalization but do not mention that you received this list:\n",
	)
	if len(traces) == 0 && episodeBlock != "" {
		sb.WriteString("(no semantic memories yet)\n")
	}
	for _, t := range traces {
		content := t.Content
		// Cap each memory's content at ~200 chars to keep injection
		// tight on local-LLM context budgets. The full content is
		// retrievable via the explicit `recall` tool.
		if len(content) > 200 {
			content = content[:197] + "..."
		}
		sb.WriteString(fmt.Sprintf("- %s\n", content))
	}
	if episodeBlock != "" {
		sb.WriteString("\n")
		sb.WriteString(episodeBlock)
		sb.WriteString("\n")
	}
	return &llm.Message{
		Role:    llm.RoleSystem,
		Content: sb.String(),
	}
}

// findGitRoot walks up from startDir looking for a .git directory (or
// .git file, for worktree subdirs). Returns the first ancestor
// containing .git, or "" when none is found. This mirrors
// prompt.workspaceDir() but lives in the agent package so the inlet
// doesn't need to import prompt (which would create a cycle: prompt
// imports skills, agent imports prompt, agent.memory_inlet would import
// prompt — currently fine, but this keeps the dependency graph clean).
func findGitRoot(startDir string) string {
	dir := startDir
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
