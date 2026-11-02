// Package webserver — statemem.go
//
// The persona + memory plumbing behind the live state surface (/api/state,
// the SSE state event, and the settings panel's agents/memory toggles).
//
// This backend's model differs from the original parity branch in one way:
// persona identity is store-backed — engines receive the ACTIVE persona
// through their session (agent.WithPersonaStore + Session.SetPersona), not
// through a pre-rendered system-prompt section. Memory, likewise, is a
// single shared semantic bank at ~/.a1/agent/memories.db — the exact file
// the TUI's Ctrl+K memory toggle opens — so memories recorded from the web
// surface are the ones the TUI recalls, and vice versa.
package webserver

import (
        "os"
        "path/filepath"

        "github.com/damien1141/a1/internal/agent"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/memstore"
        "github.com/damien1141/a1/internal/persona"

        "github.com/damien1141/a1/internal/vault"
)

// personaView is the lightweight persona embedded in /api/state so every
// surface (sidebar chip, welcome screen, footer) can render the active
// identity without a second request. Memory count is omitted on purpose —
// the manager's own list endpoint carries the per-bank counts.
//
// A server without a persona store (tests, embedded builds) reports the
// default persona rather than 500ing every state push.
func (s *Server) personaView() personaDTO {
        if s.personaStore == nil {
                return toDTO(persona.Persona{ID: persona.DefaultPersonaID, Name: "a1", Emoji: "🧭", Hue: 200}, persona.DefaultPersonaID, 0)
        }
        p, err := s.personaStore.ActivePersona()
        if err != nil || p.ID == "" {
                return toDTO(persona.Persona{ID: persona.DefaultPersonaID, Name: "a1", Emoji: "🧭", Hue: 200}, persona.DefaultPersonaID, 0)
        }
        return toDTO(p, p.ID, 0)
}

// memStoreFor returns the (cached) shared memory bank, opening it on first
// use. The bank lives at <global>/agent/memories.db — the same file the
// TUI's memory toggle opens — so both surfaces share one memory. A bank
// that cannot be opened returns nil (memory tools simply stay off): a
// broken bank must not take the chat API down.
func (s *Server) memStoreFor() *memstore.Store {
        if s.proj == nil {
                return nil
        }
        s.mu.Lock()
        defer s.mu.Unlock()
        if s.memStore != nil {
                return s.memStore
        }
        // Embedder selection mirrors the TUI controller: Ollama when the
        // config's semantic_search names a model, FakeEmbedder otherwise.
        var emb memstore.Embedder = memstore.FakeEmbedder{DimCount: 64}
        if cfg := s.proj.Config(); cfg != nil {
                if o := memstore.TryOllamaFromConfig(cfg.SemanticSearch.OllamaBaseURL, cfg.SemanticSearch.EmbeddingModel); o != nil {
                        emb = o
                }
        }
        // Bet 08: the shared bank is a namespaced family inside the vault
        // when it booted; the legacy file store remains the fallback.
        if s.vault != nil {
                st, err := memstore.OpenIn(s.vault.DB(), vault.SharedNS(), emb, memstore.DefaultParams())
                if err != nil {
                        return nil
                }
                s.memStore = st
                return s.memStore
        }
        agentDir := filepath.Join(s.proj.Global().Root(), "agent")
        if err := os.MkdirAll(agentDir, 0o755); err != nil {
                return nil
        }
        st, err := memstore.Open(filepath.Join(agentDir, "memories.db"), emb, memstore.DefaultParams())
        if err != nil {
                return nil
        }
        s.memStore = st
        return s.memStore
}

// applyPersonaToEngines pushes the active persona to every running engine
// and rebinds the memory tools to the shared bank (or away from it when
// memory is effectively off). Engines created later pick the values up in
// liveFor. Called after a persona switch, a persona edit that touches the
// active profile, or the memory on/off toggle.
func (s *Server) applyPersonaToEngines() {
        var (
                engines []*agent.Engine
                id      string
        )
        if s.personaStore != nil {
                if p, err := s.personaStore.ActivePersona(); err == nil {
                        id = p.ID
                }
        }
        s.mu.Lock()
        for _, le := range s.engines {
                if le != nil && le.engine != nil {
                        engines = append(engines, le.engine)
                }
        }
        s.mu.Unlock()
        for _, e := range engines {
                // Engine-level switch: session.SetPersona re-reads the persona
                // from the injected store on the next context build ("" reverts
                // to the default persona), and the rebind rebuilds the LLM
                // client's system prompt — so a persona marked "replace the
                // default system prompt" takes over (or hands control back)
                // on the very next turn.
                _ = e.SetPersona(id)
                // Memory follows the persona. SetPersona just made the new
                // persona active, so e.MemoryEnabled() now resolves the persona's
                // explicit memory_enabled (when set) against the surface's raw
                // toggle. Attach the shared bank when memory is effectively on,
                // detach when off — the remember/recall tools rebind with it and
                // the auto inlet/outlet gate on the same value, so switching
                // personas moves the whole memory surface, both directions.
                if e.MemoryEnabled() {
                        e.SetMemoryStore(s.memStoreFor())
                } else {
                        e.SetMemoryStore(nil)
                }
        }
        // Mirror the active id into serverState so GET /api/state and the SSE
        // state events agree with the persona store (the store is the source
        // of truth; the mirror is what the browser reads).
        s.stateStore().setPersona(id)
}

// effectiveMemoryEnabled resolves the memory state the UI displays and the
// engines run with: the ACTIVE persona's explicit memory_enabled when it
// carries one, otherwise the global settings toggle. Mirrors
// Engine.MemoryEnabled() exactly, so the footer's memory chip can never
// disagree with what the engine is actually doing.
func (s *Server) effectiveMemoryEnabled(global bool) bool {
        if s.personaStore != nil {
                if p, err := s.personaStore.ActivePersona(); err == nil && p.MemoryEnabled != nil {
                        return *p.MemoryEnabled
                }
        }
        return global
}

// effectiveModelName resolves the model name the UI should show and new
// engines should boot: the manual settings-panel knob when set, otherwise
// the ACTIVE persona's Model, otherwise "" (the project default sentinel
// the frontend renders as "default"). This is the display-side counterpart
// of Engine.applyPersonaSettings' model layer — before it, an untouched
// knob resolved straight to the startup model and the persona's model was
// invisible to the footer chip and the settings panel until some engine
// happened to apply it.
func (s *Server) effectiveModelName() string {
        if name := s.stateStore().snapshot().Model; name != "" {
                return name
        }
        if s.personaStore != nil {
                if p, err := s.personaStore.ActivePersona(); err == nil && p.Model != "" {
                        return p.Model
                }
        }
        return ""
}

// effectiveModelConfig resolves the model config for a new engine: knob →
// active persona's Model → project default (same order as
// effectiveModelName, returning the full config + think level).
func (s *Server) effectiveModelConfig() (llm.ModelConfig, bool) {
        return s.stateStore().resolveModel(s.proj, s.personaModelName())
}

// personaModelName returns the active persona's Model override ("" when the
// persona declares none, no store is attached, or the store fails to read).
func (s *Server) personaModelName() string {
        if s.personaStore != nil {
                if p, err := s.personaStore.ActivePersona(); err == nil {
                        return p.Model
                }
        }
        return ""
}

// personaPermissionMode returns the active persona's PermissionMode override
// ("" when the persona declares none). Used by the mode surfaces so the
// switcher shows — and writes — what the persona owns, mirroring the memory
// toggle's persona-ownership pattern.
func (s *Server) personaPermissionMode() string {
        if s.personaStore != nil {
                if p, err := s.personaStore.ActivePersona(); err == nil {
                        return p.PermissionMode
                }
        }
        return ""
}

// effectivePermissionMode resolves the permission mode the UI displays and
// new sessions run with: the ACTIVE persona's explicit PermissionMode when
// set, otherwise the surface's raw mode (modeStore). The engine folds the
// same layer in applyPersonaSettings, so the switcher pill and the gate can
// never disagree about which mode is live.
func (s *Server) effectivePermissionMode() string {
        if m := s.personaPermissionMode(); m != "" {
                return m
        }
        return string(s.modeStore().mode())
}
