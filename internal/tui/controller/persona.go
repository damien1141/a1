package controller

import (
        "fmt"
        "time"

        "github.com/damien1141/a1/internal/components/toast"
        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/project"
        "github.com/damien1141/a1/internal/weather"
)

// weatherConfigFromProject reads the weather block from the project config
// and translates it to a weather.Config. Returns the zero Config when the
// project is unset or the weather block is absent (default-off).
func weatherConfigFromProject(proj *project.Project) weather.Config {
        if proj == nil || proj.Config() == nil {
                return weather.Config{}
        }
        cfg := proj.Config()
        if cfg.Weather == nil {
                return weather.Config{}
        }
        return weather.Config{
                Enabled:   cfg.Weather.Enabled,
                Latitude:  cfg.Weather.Latitude,
                Longitude: cfg.Weather.Longitude,
                Name:      cfg.Weather.Name,
        }
}

// PersonaStore returns the controller's persona store. Used by the TUI
// command palette to enumerate personas + render avatars.
func (c *EngineController) PersonaStore() *persona.Store {
        if c == nil {
                return nil
        }
        return c.personaStore
}

// SetPersona switches the active persona on the live engine. The persona
// store's SetActive updates the persisted registry; the engine's SetPersona
// invalidates the context cache AND rebinds the LLM client — so a persona
// configured to replace the default system prompt takes over on the next
// turn. The switch also re-applies the memory surface: the new persona's
// explicit memory_enabled (when set) moves the effective memory state, so
// the shared bank's remember/recall tools and the auto inlet/outlet follow
// the persona rather than staying wherever the global toggle was left.
// Emits a toast so the user sees confirmation in the TUI.
func (c *EngineController) SetPersona(id string) error {
        if c == nil || c.personaStore == nil {
                return fmt.Errorf("controller: persona store not configured")
        }
        if err := c.personaStore.SetActive(id); err != nil {
                return err
        }
        // If the engine has a live session, propagate the switch so the cached
        // context reflects the new persona on the next turn. Sub-agents and
        // resumed sessions will pick this up from the store when they call
        // BuildContext themselves.
        if c.engine != nil {
                _ = c.engine.SetPersona(id)
                // Memory follows the persona: attach/detach the shared bank against
                // the now-active persona's effective memory state.
                c.applyMemoryToEngine()
        }
        p, err := c.personaStore.Get(id)
        if err != nil {
                return nil // persona switched successfully; lookup error is informational
        }
        c.publish(ToastMsg{
                Message:  fmt.Sprintf("Persona: %s", p.Name),
                Kind:     toast.ToastSuccess,
                Duration: 2 * time.Second,
        })
        return nil
}

// ActivePersona returns the currently active persona, or the default when
// the persona store is unset.
func (c *EngineController) ActivePersona() persona.Persona {
        if c == nil || c.personaStore == nil {
                return persona.Persona{}
        }
        p, err := c.personaStore.ActivePersona()
        if err != nil {
                return persona.Persona{}
        }
        return p
}

// RefreshWeather re-reads the weather block from the project config and
// updates the live weather service. Called after the config editor saves
// so toggling weather.enabled in the UI takes effect without a restart.
func (c *EngineController) RefreshWeather() {
        if c == nil || c.weatherSvc == nil {
                return
        }
        c.weatherSvc.UpdateConfig(weatherConfigFromProject(c.proj))
}
