package webserver

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/damien1141/a1/internal/agent"
	"github.com/damien1141/a1/internal/permission"
)

// webPermissionModes are the modes the switcher offers, in cycle order. They
// mirror controller.CyclePermissionMode so the two surfaces cannot drift: a user
// who switches to read-only in the browser gets what the terminal would give.
var webPermissionModes = []permission.Mode{
	permission.ModeInteractive,
	permission.ModeReadonly,
	permission.ModeAutopilot,
	permission.ModeHeadlessStrict,
}

// modeLabel is the switcher's short name for each mode.
var modeLabel = map[permission.Mode]string{
	permission.ModeInteractive:    "ask",
	permission.ModeReadonly:       "read-only",
	permission.ModeAutopilot:      "autopilot",
	permission.ModeHeadlessStrict: "strict",
}

// modeSummary explains what a mode does, so the name never has to be decoded
// from the policy tables.
var modeSummary = map[permission.Mode]string{
	permission.ModeInteractive:    "prompts before anything risky",
	permission.ModeReadonly:       "reads and searches only, no writes or bash",
	permission.ModeAutopilot:      "allowlisted tools run without asking",
	permission.ModeHeadlessStrict: "anything needing approval is denied",
}

// modeState is the live permission mode for the process. It is process-wide
// rather than per-session (like the TUI's controller): two tabs open on the same
// conversation must agree about whether a command will ship.
type modeState struct {
	// current holds a permission.Mode.
	current atomic.Value

	mu    sync.Mutex
	gates map[permission.Mode]permission.Gate
}

// loadGate returns the cached gate for mode, if one has been built.
func (m *modeState) loadGate(mode permission.Mode) (permission.Gate, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.gates[mode]
	return g, ok && g != nil
}

// storeGate caches a gate for mode.
func (m *modeState) storeGate(mode permission.Mode, g permission.Gate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gates == nil {
		m.gates = map[permission.Mode]permission.Gate{}
	}
	m.gates[mode] = g
}

// newModeState returns a modeState in interactive.
func newModeState() *modeState {
	m := &modeState{}
	m.current.Store(permission.ModeInteractive)
	return m
}

// mode returns the active mode, defaulting to interactive for a fresh server.
func (m *modeState) mode() permission.Mode {
	if m == nil {
		return permission.ModeInteractive
	}
	if v, ok := m.current.Load().(permission.Mode); ok && v != "" {
		return v
	}
	return permission.ModeInteractive
}

// setMode validates and installs mode, reporting the previous one. An unknown
// mode is rejected rather than silently treated as interactive, which would
// quietly green-light tools the user believes are restricted.
func (m *modeState) setMode(mode permission.Mode) (permission.Mode, error) {
	if _, ok := modeSummary[mode]; !ok {
		return m.mode(), fmt.Errorf("web: unknown permission mode %q", mode)
	}
	prev := m.mode()
	m.current.Store(mode)
	return prev, nil
}

// cycle advances to the next mode and returns it.
func (m *modeState) cycle() permission.Mode {
	modes := webPermissionModes
	idx := 0
	for i, md := range modes {
		if md == m.mode() {
			idx = (i + 1) % len(modes)
			break
		}
	}
	m.current.Store(modes[idx])
	return modes[idx]
}

// policy returns a copy of the project policy with the current mode applied, so
// the gate the server hands to engines reflects the switcher rather than
// whatever the config file said.
func (m *modeState) policy(base permission.Policy) permission.Policy {
	p := base
	p.Mode = m.mode()
	if p.Mode == "" {
		p.Mode = permission.ModeInteractive
	}
	return p
}

// rebuildGate constructs a StaticGate for the current mode over the project
// policy, preserving APPA trajectory state from the previous gate so the label
// L=(R,t) survives a mode switch instead of resetting to Trusted.
func (m *modeState) rebuildGate(
	base permission.Policy,
	workspace string,
	prev permission.Gate,
) (permission.Gate, error) {
	var trajectory *permission.TrajectoryState
	if sg, ok := prev.(*permission.StaticGate); ok {
		trajectory = sg.Trajectory()
	}
	g, err := permission.NewGate(m.policy(base), workspace)
	if err != nil {
		return nil, err
	}
	if trajectory != nil {
		g.SetTrajectory(trajectory)
	}
	return g, nil
}

// applyToEngines installs gate on every live engine. Engines created later pick
// it up in engineOptions, so this only has to touch the ones already running.
func (s *Server) applyToEngines(gate permission.Gate) {
	s.mu.Lock()
	engines := make([]*agent.Engine, 0, len(s.engines))
	for _, le := range s.engines {
		if le.engine != nil {
			engines = append(engines, le.engine)
		}
	}
	s.mu.Unlock()
	for _, e := range engines {
		e.SetPermission(gate, s.ask)
	}
}
