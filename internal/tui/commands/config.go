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
        path := configPath(c.Ctrl)
        // The config editor server lives until the process exits. The previous
        // implementation passed a cancellable context with `defer stop()`,
        // which cancelled the context the moment openConfig returned — and
        // configserver.Start wires srv.Shutdown to fire on ctx.Done(). That
        // tore the server down before the browser could fetch the page, so
        // /config opened a dead URL.
        //
        // Use context.Background() so the server's lifetime is the process's
        // lifetime. The returned stop function is discarded (the server
        // outlives this call). On process exit the OS reaps the listener.
        ctx := context.Background()
        pageURL, _, err := configserver.Start(ctx, path)
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
        return nil
}

// configPath resolves the config file the editor should read and write.
// The global `~/.a1/config.yaml` is the answer for every workspace, but it is
// resolved through the controller so a project that overrides the config file
// location is honored instead of silently editing a file that is never read.
func configPath(ctrl *controller.EngineController) string {
        if ctrl != nil {
                if path := ctrl.ConfigPath(); path != "" {
                        return path
                }
        }
        return project.GetDefaultProject().Global().ConfigFile()
}
