package persona

import (
        "errors"
        "fmt"
        "os"
        "path/filepath"

        "github.com/damien1141/a1/internal/memstore"
        "github.com/damien1141/a1/internal/session/memory"
)

// MemoryBank returns the persona's memory bank, opening it if needed. The
// persona ID is namespaced under "persona:" so a persona's notes never
// collide with a session's notes (which are keyed by raw session ID) inside
// the bank dir.
//
// The bank lives in the store's own memory dir (<personas root>/memory), so
// every reader and writer in the process — agent sessions, the executor's
// remember mirror, the web manager's counts and memory lists — resolves the
// exact same file by going through this one method. Earlier builds derived
// the dir differently per caller (the agent wrote to <personas root>/ while
// the web UI read <global>/sessions/memory/), which split each persona's
// memories into two files and made the manager show an eternally empty bank;
// banks stranded at the legacy path are adopted below.
func (s *Store) MemoryBank(p Persona) (*memory.Bank, error) {
        if p.ID == "" {
                return nil, errors.New("persona: empty ID")
        }
        if s.vault != nil {
                // Rows in the one durable store — no adoption shim needed:
                // the one-shot migration already imported the file banks.
                return s.vaultBank(p)
        }
        s.adoptLegacyBank(p.ID)
        return memory.OpenBank("persona:"+p.ID, s.memoryDir)
}

// SemanticStore opens (and caches) the persona's SQLite semantic memory
// store. Each persona gets its own file at
// <memoryDir>/semantic/<id>.sqlite, mirroring the JSONL bank's per-persona
// isolation: there is no global semantic memory, and one persona's vector
// store never leaks into another's.
//
// The returned *memstore.Store is cached on the persona.Store keyed by
// persona ID, so subsequent calls return the same handle (and the same
// SQLite connection pool). Callers should NOT Close() the returned store —
// the persona.Store owns the lifecycle and closes all cached stores when
// the persona.Store itself is closed (or replaced).
//
// The embedder and params are required on first open for a given persona
// ID and ignored on subsequent calls (the cached store's embedder and
// params from the first open are kept). This matches the JSONL bank's
// lazy-open pattern.
func (s *Store) SemanticStore(p Persona, emb memstore.Embedder, params memstore.Params) (*memstore.Store, error) {
        if p.ID == "" {
                return nil, errors.New("persona: empty ID")
        }
        if s.semanticStores == nil {
                s.semanticStores = make(map[string]*memstore.Store)
        }
        s.semanticMu.Lock()
        defer s.semanticMu.Unlock()
        if existing, ok := s.semanticStores[p.ID]; ok {
                return existing, nil
        }
        var store *memstore.Store
        if s.vault != nil {
                // One table family per persona inside the one durable store —
                // no per-persona SQLite files anymore (Bet 08).
                var err error
                store, err = s.vaultSemantic(p, emb, params)
                if err != nil {
                        return nil, fmt.Errorf("persona: open semantic store for %s: %w", p.ID, err)
                }
        } else {
                dir := filepath.Join(s.memoryDir, "semantic")
                if err := os.MkdirAll(dir, 0o755); err != nil {
                        return nil, fmt.Errorf("persona: create semantic dir: %w", err)
                }
                path := filepath.Join(dir, p.ID+".sqlite")
                var err error
                store, err = memstore.Open(path, emb, params)
                if err != nil {
                        return nil, fmt.Errorf("persona: open semantic store for %s: %w", p.ID, err)
                }
        }
        s.semanticStores[p.ID] = store
        return store, nil
}

// CloseSemanticStores closes every cached semantic store. Called by the
// harness on shutdown so SQLite connections flush cleanly. Safe to call
// multiple times; cached stores are cleared after the first close.
func (s *Store) CloseSemanticStores() error {
        s.semanticMu.Lock()
        defer s.semanticMu.Unlock()
        var firstErr error
        for id, store := range s.semanticStores {
                if err := store.Close(); err != nil && firstErr == nil {
                        firstErr = fmt.Errorf("persona: close semantic store %s: %w", id, err)
                }
                delete(s.semanticStores, id)
        }
        return firstErr
}

// adoptLegacyBank moves a bank file written by an earlier build (at
// <store root>/persona:<id>.jsonl) into the canonical memory dir when the
// canonical file does not exist yet. It is a no-op when there is nothing to
// adopt; failures are swallowed because the worst case is the same as before
// the fix — the old file simply stays where it is.
func (s *Store) adoptLegacyBank(id string) {
        legacy := filepath.Join(s.root, "persona:"+id+".jsonl")
        canonical := filepath.Join(s.memoryDir, "persona:"+id+".jsonl")
        if _, err := os.Stat(canonical); err == nil {
                return // canonical bank already exists — nothing to adopt
        }
        if _, err := os.Stat(legacy); err != nil {
                return // no legacy bank either
        }
        if err := os.MkdirAll(s.memoryDir, 0o755); err != nil {
                return
        }
        _ = os.Rename(legacy, canonical) // same tree, so rename suffices
}

// SystemPromptOverride returns the persona's explicit system-prompt override,
// or "" when the persona relies on the default system prompt. Callers should
// prepend this to (or inject it alongside) the base system prompt.
func (s *Store) SystemPromptOverride(p Persona) string {
        return p.SystemPrompt
}

// PersonaSummary renders a one-line persona description for the agent's
// system message ("You are speaking as <name>: <tagline>"). The tagline is
// preferred; the legacy Description field is the fallback.
func (s *Store) PersonaSummary(p Persona) string {
        if p.ID == DefaultPersonaID {
                // The default persona is the bare a1 assistant — no extra framing.
                return ""
        }
        out := fmt.Sprintf("Active persona: %s.", p.Name)
        tagline := p.Tagline
        if tagline == "" {
                tagline = p.Description
        }
        if tagline != "" {
                out += " " + tagline
        }
        return out
}
