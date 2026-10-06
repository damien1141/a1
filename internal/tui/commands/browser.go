package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/tools/browsertool"
)

// BrowserCommands owns the /browser slash command.
type BrowserCommands struct {
	// Proj is the loaded project, used to seed the browser proxy from
	// config.yaml. May be nil in tests; the proxy then stays at its default.
	Proj *project.Project
}

// Register wires /browser into r.
func (b *BrowserCommands) Register(r *CommandRegistry) {
	if b == nil || r == nil {
		return
	}
	// Seed the browser proxy from config at startup so a configured proxy
	// takes effect on the first launch without a manual /browser proxy call.
	if b.Proj != nil {
		if cfg := b.Proj.Config(); cfg != nil && cfg.Browser.Proxy != "" {
			browsertool.SetProxy(cfg.Browser.Proxy)
		}
	}
	r.Register(Command{
		Name:        "browser",
		Description: "Control a live web browser — /browser open <url>",
		Slash:       true,
		NeedsArgs:   true,
		Insert:      "/browser ",
		Run: func(_ Context, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf(
					"usage: /browser open <url> | proxy <url> | close | profile list|create|switch|delete",
				)
			}
			action := args[0]
			var in struct {
				Action   string `json:"action"`
				URL      string `json:"url,omitempty"`
				Selector string `json:"selector,omitempty"`
				Text     string `json:"text,omitempty"`
			}
			in.Action = action
			if len(args) > 1 {
				in.URL = args[1]
			}
			raw, _ := json.Marshal(in)
			// Slash commands run on the UI goroutine and receive a TUI
			// Context, not a context.Context. Use a background context so
			// ensureBrowser can build its own cancel context.
			_, err := browsertool.RunBrowserCommand(context.Background(), raw)
			return err
		},
	})
}
