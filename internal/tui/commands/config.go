package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/damien1141/a1/internal/components/toast"
	"github.com/damien1141/a1/internal/configserver"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/tui/controller"
)

// ConfigCommands owns the /config slash command.
type ConfigCommands struct {
	Bus  *controller.Bus
	Ctrl *controller.EngineController
}

// Register wires /config into r.
func (c *ConfigCommands) Register(r *CommandRegistry) {
	if c == nil || r == nil {
		return
	}
	r.Register(Command{
		Name:        "config",
		Description: "Open the local config editor",
		Slash:       true,
		Run: func(_ Context, _ []string) error {
			return c.openConfig()
		},
	})
}

func (c *ConfigCommands) openConfig() error {
	if c == nil || c.Bus == nil {
		return nil
	}
	proj := project.GetDefaultProject()
	ctx := context.Background()
	pageURL, closeServer, err := configserver.Start(ctx, proj.Global().ConfigFile())
	if err != nil {
		c.Bus.Publish(controller.ToastMsg{
			Message:  fmt.Sprintf("failed to start config editor: %v", err),
			Kind:     toast.ToastError,
			Duration: 5 * time.Second,
		})
		return err
	}
	configserver.OpenBrowser(ctx, pageURL)
	c.Bus.Publish(controller.ToastMsg{
		Message:  "opened config editor: " + pageURL,
		Kind:     toast.ToastInfo,
		Duration: 4 * time.Second,
	})
	go func() {
		<-ctx.Done()
		closeServer()
	}()
	return nil
}
