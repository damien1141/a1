package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"

	cli "github.com/pulseaiclub/pli"

	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/webserver"
)

var serveCommand = cli.Command{
	Name: "serve",
	Desc: "headless engine host: HTTP API + scheduler daemon (token auth)",
	Long: `Run a1 as a durable daemon: the full web API (chat, SSE, personas) behind
bearer-token auth, headless-strict by default (permission asks deny instead
of hanging), with the Clock wired — config.yaml ` + "`schedules:`" + ` and
schedule-tool wake-ups fire as jobs, and jobs interrupted by a restart
resume at boot.

The token comes from --token, the A1_SERVE_TOKEN env var, or is generated
(and printed) at startup. Binding a non-loopback host requires a token.
EventSource clients pass it as ?token= on the SSE endpoint.`,
	Flags: []cli.Flag{
		cli.Var("port", "", "N", "listen port (default 9002)", 0, parsePortFlag),
		cli.String("host", "", "bind address (default 127.0.0.1; non-loopback requires a token)", ""),
		cli.String("token", "", "bearer token (default: A1_SERVE_TOKEN, else generated)", ""),
		cli.Bool("no-strict", "", "start in the config's permission mode instead of headless-strict (remote-GUI use)"),
	},
	Run: func(_ []string, f cli.Flags) error { return runServe(f) },
}

func parsePortFlag(s string) (int, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	var port int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &port); err != nil {
		return 0, fmt.Errorf("invalid port %q", s)
	}
	return port, nil
}

func runServe(f cli.Flags) error {
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

	host := strings.TrimSpace(f.String("host"))
	port := f.Int("port")
	token := strings.TrimSpace(f.String("token"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("A1_SERVE_TOKEN"))
	}
	generated := false
	if token == "" {
		var b [24]byte
		if _, err := rand.Read(b[:]); err != nil {
			return fmt.Errorf("serve: generate token: %w", err)
		}
		token = hex.EncodeToString(b[:])
		generated = true
	}

	pageURL, stopServer, err := webserver.StartServe(ctx, proj, webserver.ServeOptions{
		Host:   host,
		Port:   port,
		Token:  token,
		Strict: !f.Bool("no-strict"),
	})
	if err != nil {
		return err
	}
	defer stopServer()

	fmt.Fprintf(os.Stderr, "a1 serve: %s\n", pageURL)
	if generated {
		fmt.Fprintf(os.Stderr, "token: %s (set A1_SERVE_TOKEN or --token to pin it)\n", token)
	}
	fmt.Fprintln(os.Stderr, "headless-strict: asks deny (start with --no-strict for a remote GUI) — ctrl-c to stop")
	if host != "" && !isLoopbackBind(host) {
		fmt.Fprintf(os.Stderr, "bound to %s — anyone with the token drives this agent\n", host)
	}
	<-ctx.Done()
	return nil
}

func isLoopbackBind(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
