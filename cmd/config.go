package main

import (
	"context"
	"os"
	"os/signal"

	cli "github.com/pulseaiclub/pli"

	"github.com/damien1141/a1/internal/configserver"
	"github.com/damien1141/a1/internal/project"
)

var configCommand = cli.Command{
	Name: "config",
	Desc: "open the HTML config editor (local web server)",
	Long: "Open the HTML config editor (starts a local web server on 127.0.0.1).",
	Run: func(_ []string, _ cli.Flags) error {
		return runConfigEditor()
	},
}

// runConfigEditor starts a local web server (loopback only) that edits
// config.yaml in the browser.
func runConfigEditor() error {
	proj := project.GetDefaultProject()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pageURL, closeServer, err := configserver.Start(ctx, proj.Global().ConfigFile())
	if err != nil {
		return err
	}
	defer closeServer()

	configserver.OpenBrowser(ctx, pageURL)

	<-ctx.Done()
	return nil
}
