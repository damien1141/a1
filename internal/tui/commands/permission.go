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

// PermissionCommands owns the /permission slash command.
type PermissionCommands struct {
	Bus  *controller.Bus
	Ctrl *controller.EngineController
}

// Register wires /permission into r.
func (p *PermissionCommands) Register(r *CommandRegistry) {
	if p == nil || r == nil {
		return
	}
	r.Register(Command{
		Name:        "permission",
		Description: "Inspect APPA permission state — /permission trajectory|recovery|authority|config",
		Slash:       true,
		Run: func(_ Context, args []string) error {
			return p.handle(args)
		},
		ArgCompleter: func(args []string) []ArgItem {
			if len(args) == 0 {
				return []ArgItem{
					{Insert: "trajectory", Description: "current trajectory state"},
					{Insert: "recovery", Description: "recovery graph summary"},
					{Insert: "authority", Description: "authority resolver backend"},
					{Insert: "config", Description: "open config editor"},
				}
			}
			return nil
		},
	})
}

func (p *PermissionCommands) handle(args []string) error {
	if p == nil || p.Ctrl == nil || p.Bus == nil {
		return nil
	}
	g := p.Ctrl.PermissionGate()
	if g == nil {
		p.Bus.Publish(controller.ToastMsg{
			Message:  "permission gate unavailable",
			Kind:     toast.ToastError,
			Duration: 3 * time.Second,
		})
		return nil
	}
	action := "trajectory"
	if len(args) > 0 {
		action = args[0]
	}
	switch action {
	case "trajectory":
		if t := g.Trajectory(); t != nil {
			p.Bus.Publish(controller.ToastMsg{
				Message:  fmt.Sprintf("trajectory: %s", t.String()),
				Kind:     toast.ToastInfo,
				Duration: 0,
			})
		}
	case "recovery":
		if r := g.RecoveryGraph(); r != nil {
			p.Bus.Publish(controller.ToastMsg{
				Message:  fmt.Sprintf("recovery: %s", r.String()),
				Kind:     toast.ToastInfo,
				Duration: 0,
			})
		}
	case "authority":
		res := g.Resolver()
		p.Bus.Publish(controller.ToastMsg{
			Message:  fmt.Sprintf("authority: requires=%v resolver=%T", g.Policy.RequiresAuthority, res),
			Kind:     toast.ToastInfo,
			Duration: 0,
		})
	case "config":
		p.openConfig()
	default:
		p.Bus.Publish(controller.ToastMsg{
			Message:  "unknown action: " + action,
			Kind:     toast.ToastError,
			Duration: 3 * time.Second,
		})
	}
	return nil
}

func (p *PermissionCommands) openConfig() {
	proj := project.GetDefaultProject()
	ctx := context.Background()
	pageURL, closeServer, err := configserver.Start(ctx, proj.Global().ConfigFile())
	if err != nil {
		p.Bus.Publish(controller.ToastMsg{
			Message:  fmt.Sprintf("failed to start config editor: %v", err),
			Kind:     toast.ToastError,
			Duration: 5 * time.Second,
		})
		return
	}
	configserver.OpenBrowser(ctx, pageURL)
	p.Bus.Publish(controller.ToastMsg{
		Message:  "opened config editor: " + pageURL,
		Kind:     toast.ToastInfo,
		Duration: 4 * time.Second,
	})
	go func() {
		<-ctx.Done()
		closeServer()
	}()
}
