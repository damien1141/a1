package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	cli "github.com/pulseaiclub/pli"

	"github.com/damien1141/a1/internal/discord"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/webserver"
)

var discordCommand = cli.Command{
	Name: "discord",
	Desc: "bridge one Discord channel to an a1 chat session",
	Long: `Run the agent behind a Discord channel.

The webserver boots in-process on an ephemeral loopback port and the bridge
drives it through the same chat API + SSE stream the web GUI uses — the
agent's network posture does not change; the only new connections are the
bridge's outbound gateway websocket and REST calls.

Channel messages become prompts; each finished turn's reply is sent back as
chunked markdown (typing indicator while a turn runs). Permission approvals
arrive in the channel — reply y or n inside the window (an unanswered
approval is denied). !stop cancels the running turn; !new starts a fresh
session. Other bots do not get a turn; the channel is the trust boundary.

Needs a discord: section in config.yaml:

  discord:
    token: "…"            # bot token (Developer Portal → Bot)
    channel: "…"          # the channel id to bridge

Prerequisites no code can supply: invite the bot to the server first, and
enable the MESSAGE CONTENT intent in the Developer Portal (Bot → Privileged
Gateway Intents) — without it, messages arrive with empty content and the
bridge sees nothing.`,
	Run: func(_ []string, _ cli.Flags) error { return runDiscord() },
}

// runDiscord boots the loopback webserver, validates the discord config
// against the live API, and bridges until interrupted.
func runDiscord() error {
	proj := project.GetDefaultProject()
	if err := proj.LoadConfig(); err != nil {
		fmt.Fprintln(os.Stderr, "a1:", err)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Configure a model first, then restart:")
		fmt.Fprintln(os.Stderr, "  a1 config")
		return exitCode(ExitUsage)
	}
	cfg := proj.Config().Discord
	if !cfg.Configured() {
		fmt.Fprintln(os.Stderr, "a1: the discord: section is missing or incomplete in config.yaml")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Give the bridge a bot token and a channel, then restart:")
		fmt.Fprintln(os.Stderr, `  discord:
    token: "…"
    channel: "…"`)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "The bot needs to be in the server and the MESSAGE CONTENT intent")
		fmt.Fprintln(os.Stderr, "enabled in the Developer Portal — see doc/discord.md.")
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

	state := discord.LoadState(filepath.Join(proj.Global().Root(), "discord", "state.json"))
	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "discord: "+format+"\n", args...)
	}
	client, gateway, err := discord.Connect(ctx, discord.Config{
		Token:   cfg.Token,
		Channel: cfg.Channel,
		API:     cfg.API,
		Gateway: cfg.Gateway,
	}, logf)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	bridge, err := discord.NewBridge(discord.Options{
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

	fmt.Fprintf(os.Stderr, "discord bridge: channel %s — ctrl-c to stop\n", cfg.Channel)
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
