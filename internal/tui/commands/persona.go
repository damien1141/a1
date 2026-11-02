package commands

import (
	"fmt"
	"time"

	"github.com/damien1141/a1/internal/components/toast"
	"github.com/damien1141/a1/internal/components/palette"
	"github.com/damien1141/a1/internal/persona"
	"github.com/damien1141/a1/internal/tui/controller"
)

// PersonaCommands owns the persona palette command: switch.
//
// The TUI command palette is the primary surface for swapping personas. The
// command renders a submenu following the settings.go pattern: a top-level
// entry whose Verb is "switch", whose Submenu carries the actual pickers.
//
// Creation lives in the web GUI's persona manager (/personas from the chat
// surface, or the personas section of the config editor at /settings): the
// TUI's old "create default personality" stub could only mint a placeholder
// ("New Persona", empty prompt) that still needed the web editor to become
// useful — one flow fewer to keep coherent.
type PersonaCommands struct {
	Ctrl *controller.EngineController
	Bus  *controller.Bus
}

// Register wires the persona palette entries into r. Each entry builds its
// submenu lazily on open (so personas created in the web GUI appear
// immediately).
func (p *PersonaCommands) Register(r *CommandRegistry) {
	if p == nil || r == nil {
		return
	}
	r.Register(Command{
		Name: "persona-switch",
		Build: func(_ Context) palette.PaletteCommand {
			return p.buildSwitchPalette()
		},
	})
}

// buildSwitchPalette enumerates personas from the store and renders a
// submenu: one row per persona, marked with ✓ next to the active one. A
// trailing hint points at the web surface where personas are created.
func (p *PersonaCommands) buildSwitchPalette() palette.PaletteCommand {
	store := p.store()
	title := "Switch Persona"
	if store == nil {
		return palette.PaletteCommand{
			ID:           "persona-switch",
			Noun:         "persona",
			Verb:         "switch",
			Keywords:     []string{"persona", "switch", "personality"},
			SubmenuTitle: title,
			Submenu: []palette.PaletteCommand{{
				ID:       "persona-disabled",
				Verb:     "personas not configured",
				Disabled: true,
				Run:      func() {},
			}},
		}
	}
	personas, err := store.List()
	if err != nil {
		return palette.PaletteCommand{
			ID:           "persona-switch",
			Noun:         "persona",
			Verb:         "switch",
			Keywords:     []string{"persona", "switch", "personality"},
			SubmenuTitle: title,
			Submenu: []palette.PaletteCommand{{
				ID:       "persona-error",
				Verb:     fmt.Sprintf("error: %v", err),
				Disabled: true,
				Run:      func() {},
			}},
		}
	}
	active, _ := store.Active()
	cmds := make([]palette.PaletteCommand, 0, len(personas)+1)
	for _, per := range personas {
		mark := "  "
		if per.ID == active {
			mark = "✓ "
		}
		name := per.Name
		if per.ID == persona.DefaultPersonaID {
			name = "a1 (default)"
		}
		per := per // capture for closure
		cmds = append(cmds, palette.PaletteCommand{
			ID:       "persona-" + per.ID,
			Verb:     mark + name,
			Keywords: []string{per.Name, per.Description, "persona"},
			Run: func() {
				p.activate(per.ID)
			},
		})
	}
	cmds = append(cmds, palette.PaletteCommand{
		ID:       "persona-create-hint",
		Verb:     "+ create personas in the web GUI (personas, or /settings)",
		Keywords: []string{"persona", "create", "new", "add", "web"},
		Run:      func() {},
	})
	return palette.PaletteCommand{
		ID:           "persona-switch",
		Noun:         "persona",
		Verb:         "switch",
		Keywords:     []string{"persona", "switch", "personality", "memory", "profile"},
		SubmenuTitle: title,
		Submenu:      cmds,
	}
}

// activate switches the active persona via the controller and toasts the
// confirmation. Errors are surfaced as error toasts rather than returned so
// the palette can close cleanly.
func (p *PersonaCommands) activate(id string) {
	if p == nil || p.Ctrl == nil {
		return
	}
	if err := p.Ctrl.SetPersona(id); err != nil {
		publishToast(p.Bus, fmt.Sprintf("failed to switch persona: %v", err), toast.ToastError, 3*time.Second)
		return
	}
}

// store returns the controller's persona store, or nil if the controller is
// not wired (defensive: palette commands can run before the controller is
// bound in tests).
func (p *PersonaCommands) store() *persona.Store {
	if p == nil || p.Ctrl == nil {
		return nil
	}
	return p.Ctrl.PersonaStore()
}
