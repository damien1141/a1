package webserver

import (
        "strings"
        "sync"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/project"
)

// serverState mirrors the engine-level toggles the TUI's settings palette
// controls: model name, thinking effort, sub-agents, and agent memory.
// Like modeState, it is process-wide so two tabs on the same conversation
// agree about which knobs are turned which way.
//
// The Engine's own modelCfg is private, so this is the source of truth the
// UI reads. liveFor applies the values to a freshly-built engine via
// applyState; setModel / setThink re-apply to every already-running engine.
type serverState struct {
        mu sync.RWMutex

        // model is the active model name (one of project.Config().AllModels()).
        // Empty means "the project's default".
        model string

        // think is the reasoning effort level. Default means "provider-adaptive".
        think llm.ThinkMode

        // agentsEnabled registers the agent_* tools. Defaults to off — the
        // TUI ships off and the same default applies here.
        agentsEnabled bool

        // memoryEnabled registers remember/recall tools. Defaults to off.
        memoryEnabled bool

        // personaID is the active persona id. Empty means "default" — kept
        // distinct from the string default so a zero serverState needs no
        // special-casing (the persona store resolves "" to persona.DefaultID).
        personaID string
}

func newServerState() *serverState {
        return &serverState{
                think: llm.Default,
        }
}

// snapshot returns a read-only copy safe to JSON-encode.
func (s *serverState) snapshot() serverStateSnapshot {
        s.mu.RLock()
        defer s.mu.RUnlock()
        return serverStateSnapshot{
                Model:         s.model,
                Think:         string(s.think),
                AgentsEnabled: s.agentsEnabled,
                MemoryEnabled: s.memoryEnabled,
                PersonaID:     s.personaID,
        }
}

// serverStateSnapshot is the JSON shape returned to the browser. Fields use
// string for think (so the wire type is stable across ThinkMode renames)
// and bool for the toggles.
type serverStateSnapshot struct {
        Model         string `json:"model"`
        Think         string `json:"think"`
        AgentsEnabled bool   `json:"agentsEnabled"`
        MemoryEnabled bool   `json:"memoryEnabled"`
        PersonaID     string `json:"personaId"`
}

// setModel swaps the active model and returns the previous name. An empty
// name resets to the project default.
func (s *serverState) setModel(name string) string {
        s.mu.Lock()
        defer s.mu.Unlock()
        prev := s.model
        s.model = name
        return prev
}

// setThink swaps the reasoning effort level.
func (s *serverState) setThink(mode llm.ThinkMode) llm.ThinkMode {
        s.mu.Lock()
        defer s.mu.Unlock()
        prev := s.think
        s.think = mode
        return prev
}

// setAgents toggles sub-agent tool registration.
func (s *serverState) setAgents(enabled bool) bool {
        s.mu.Lock()
        defer s.mu.Unlock()
        prev := s.agentsEnabled
        s.agentsEnabled = enabled
        return prev
}

// setMemory toggles agent memory tools.
func (s *serverState) setMemory(enabled bool) bool {
        s.mu.Lock()
        defer s.mu.Unlock()
        prev := s.memoryEnabled
        s.memoryEnabled = enabled
        return prev
}

// setPersona records the active persona id.
func (s *serverState) setPersona(id string) string {
        s.mu.Lock()
        defer s.mu.Unlock()
        prev := s.personaID
        s.personaID = id
        return prev
}

// resolveModel returns the ModelConfig the server should hand to a new
// engine. Resolution order:
//
//  1. The manual knob (s.model) — an explicit pick from the settings
//     panel — with the live think level layered on top.
//  2. fallback (the ACTIVE persona's Model, wired by the Server's
//     effectiveModel* helpers): an empty knob means "follow the persona",
//     which is what makes persona settings the DEFAULT selection. Before
//     this layer existed, the empty knob resolved straight to the project
//     default — the "falls back to the startup model" report.
//  3. The project's default model.
//
// This lives here rather than in liveFor so the same lookup serves the
// settings GET endpoint (which lists models + the active one).
func (s *serverState) resolveModel(proj *project.Project, fallback string) (llm.ModelConfig, bool) {
        if proj == nil {
                return llm.ModelConfig{}, false
        }
        cfg := proj.Config()
        if cfg == nil {
                return llm.ModelConfig{}, false
        }
        all := cfg.AllModels()
        s.mu.RLock()
        name := s.model
        think := s.think
        s.mu.RUnlock()
        if name == "" {
                name = strings.TrimSpace(fallback)
        }
        if name == "" {
                def := cfg.Model()
                def.Think.Mode = think
                if think == llm.Default {
                        def.Think.Mode = cfg.Model().Think.Mode
                        if def.Think.Mode == "" {
                                def.Think.Mode = llm.Default
                        }
                }
                return def, true
        }
        for _, m := range all {
                if m.Name == name {
                        m.Think.Mode = think
                        return m, true
                }
        }
        return cfg.Model(), true
}

// modelNames returns the project's model list for the settings panel.
func modelNames(proj *project.Project) []string {
        if proj == nil || proj.Config() == nil {
                return nil
        }
        all := proj.Config().AllModels()
        out := make([]string, 0, len(all))
        for _, m := range all {
                out = append(out, m.Name)
        }
        return out
}
