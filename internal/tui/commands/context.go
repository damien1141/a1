package commands

import (
	"time"

	"github.com/damien1141/a1/internal/components/palette"
	"github.com/damien1141/a1/internal/components/toast"
	"github.com/damien1141/a1/internal/tui/controller"
)

// Context provides TUI services to command handlers.
// Commands receive this through Run/Build; domain-specific deps arrive
// via closures captured at registration time.
type Context interface {
	Bus() *controller.Bus
	Toast(msg string, kind toast.ToastKind, d time.Duration)
	PushSubmenu(title string, cmds []palette.PaletteCommand)
}

type commandContext struct {
	bus  *controller.Bus
	push func(string, []palette.PaletteCommand)
}

func (c *commandContext) Bus() *controller.Bus { return c.bus }

func (c *commandContext) Toast(msg string, kind toast.ToastKind, d time.Duration) {
	if c.bus != nil {
		c.bus.Publish(controller.ToastMsg{Message: msg, Kind: kind, Duration: d})
	}
}

func (c *commandContext) PushSubmenu(title string, cmds []palette.PaletteCommand) {
	if c.push != nil {
		c.push(title, cmds)
	}
}

// NewContext creates a Context from a bus and optional palette-push function.
func NewContext(bus *controller.Bus, push func(string, []palette.PaletteCommand)) Context {
	return &commandContext{bus: bus, push: push}
}
