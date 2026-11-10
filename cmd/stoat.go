package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	cli "github.com/pulseaiclub/pli"

	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/stoat"
	"github.com/damien1141/a1/internal/webserver"
)

var stoatCommand = cli.Command{
	Name: "stoat",
	Desc: "bridge one stoat.chat channel to an a1 chat session",
	Long: `Run the agent behind a stoat.chat channel (the Revolt-lineage platform).

The webserver boots in-process on an ephemeral loopback port and the bridge
drives it through the same chat API + SSE stream the web GUI uses — the
agent's network posture does not change; the only new connections are the
bridge's outbound event websocket and REST calls.

Channel messages become prompts; each finished turn's reply is sent back as
chunked markdown (typing indicator while a turn runs). Permission approvals
arrive in the channel — reply y or n inside the window (an unanswered
approval is denied). !stop cancels the running turn; !new starts a fresh
session. Other bots do not get a turn; the channel is the trust boundary.

Needs a stoat: section in config.yaml:

  stoat:
    token: "…"            # bot token (create one in stoat's bot settings)
    channel: "…"          # the channel id to bridge

Invite the bot to the server first — membership is granted by invite, and
the bridge validates channel visibility at startup.`,
	Run: func(_ []string, _ cli.Flags) error { return runStoat() },
}

// runStoat boots the loopback webserver, validates the stoat config
// against the live API, and bridges until interrupted.
func runStoat() error {
	proj := project.GetDefaultProject()
	if err := proj.LoadConfig(); err != nil {
		fmt.Fprintln(os.Stderr, "a1:", err)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Configure a model first, then restart:")
		fmt.Fprintln(os.Stderr, "  a1 config")
		return exitCode(ExitUsage)
	}
	cfg := proj.Config().Stoat
	if !cfg.Configured() {
		fmt.Fprintln(os.Stderr, "a1: the stoat: section is missing or incomplete in config.yaml")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Give the bridge a bot token and a channel, then restart:")
		fmt.Fprintln(os.Stderr, `  stoat:
    token: "…"
    channel: "…"`)
		return exitCode(ExitUsage)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// The chat surface stays loopback-only: an ephemeral port cannot
	// collide with a running `a1 web`, and nothing but the bridge talks
	// to it.
	pageURL, closeServer, err := webserver.Start(ctx, proj, 0)
	if err != nil {
		return err
	}
	defer closeServer()

	state := stoat.LoadState(filepath.Join(proj.Global().Root(), "stoat", "state.json"))
	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "stoat: "+format+"\n", args...)
	}
	client, gateway, err := stoat.Connect(ctx, stoat.Config{
		Token:   cfg.Token,
		Channel: cfg.Channel,
		API:     cfg.API,
		WS:      cfg.WS,
	}, logf)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	bridge, err := stoat.NewBridge(stoat.Options{
		Client:      client,
		Gateway:     gateway,
		Channel:     cfg.Channel,
		LoopbackURL: pageURL,
		State:       state,
		Logger:      logf,
	})
	if err != nil {
		return err
	}

	errc := make(chan error, 1)
	go func() { errc <- bridge.Run(ctx) }()

	fmt.Fprintf(os.Stderr, "stoat bridge: channel %s — ctrl-c to stop\n", cfg.Channel)
	fmt.Fprintf(os.Stderr, "chat surface (loopback, bridge-only): %s\n", pageURL)
	select {
	case <-ctx.Done():
		return nil
	case err := <-errc:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}
