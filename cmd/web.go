package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	cli "github.com/pulseaiclub/pli"

	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/webserver"
)

var webCommand = cli.Command{
	Name: "web",
	Desc: "start the web chat GUI (htmx)",
	Long: "Start the htmx web chat GUI on http://127.0.0.1:9001 (loopback only).",
	Run:  func(_ []string, _ cli.Flags) error { return runWeb() },
}

// runWeb starts the web chat server and blocks until interrupted.
func runWeb() error {
	proj := project.GetDefaultProject()
	if err := proj.LoadConfig(); err != nil {
		fmt.Fprintln(os.Stderr, "a1:", err)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Configure a model first, then restart:")
		fmt.Fprintln(os.Stderr, "  a1 config")
		return exitCode(ExitUsage)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pageURL, closeServer, err := webserver.Start(ctx, proj, webserver.DefaultPort)
	if err != nil {
		return err
	}
	defer closeServer()

	fmt.Fprintf(os.Stderr, "web chat: %s\n", pageURL)
	fmt.Fprintln(os.Stderr, "serving this directory — ctrl-c to stop")
	<-ctx.Done()
	return nil
}
