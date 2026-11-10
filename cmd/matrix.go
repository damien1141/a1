package main

import (
        "context"
        "errors"
        "fmt"
        "os"
        "os/signal"
        "path/filepath"

        cli "github.com/pulseaiclub/pli"

        "github.com/damien1141/a1/internal/matrix"
        "github.com/damien1141/a1/internal/project"
        "github.com/damien1141/a1/internal/webserver"
)

var matrixCommand = cli.Command{
        Name: "matrix",
        Desc: "bridge one Matrix room to an a1 chat session",
        Long: `Run the agent behind a Matrix room (the "text the harness" surface).

The webserver boots in-process on an ephemeral loopback port and the bridge
drives it through the same chat API + SSE stream the web GUI uses — the
agent's network posture does not change; the only new connection is the
bridge's outbound Matrix sync long-poll.

Room messages become prompts; each finished turn's reply is sent back as
chunked markdown (typing indicator while a turn runs). Permission approvals
arrive in the room — reply y or n inside the window (an unanswered approval
is denied). !stop cancels the running turn; !new starts a fresh session.

Needs a matrix: section in config.yaml:

  matrix:
    homeserver: "https://matrix.org"
    user: "a1bot"          # localpart or full @id
    password: "…"          # or access_token: syt_…
    room: "#a1:matrix.org" # or a !roomid:homeserver

Plaintext rooms only — a room with encryption enabled is refused at
startup. The room is the trust boundary: keep it invite-only.`,
        Run: func(_ []string, _ cli.Flags) error { return runMatrix() },
}

// runMatrix boots the loopback webserver, connects the Matrix account, and
// bridges until interrupted.
func runMatrix() error {
        proj := project.GetDefaultProject()
        if err := proj.LoadConfig(); err != nil {
                fmt.Fprintln(os.Stderr, "a1:", err)
                fmt.Fprintln(os.Stderr, "")
                fmt.Fprintln(os.Stderr, "Configure a model first, then restart:")
                fmt.Fprintln(os.Stderr, "  a1 config")
                return exitCode(ExitUsage)
        }
        cfg := proj.Config().Matrix
        if !cfg.Configured() {
                fmt.Fprintln(os.Stderr, "a1: the matrix: section is missing or incomplete in config.yaml")
                fmt.Fprintln(os.Stderr, "")
                fmt.Fprintln(os.Stderr, "Give the bridge an account and a room, then restart:")
                fmt.Fprintln(os.Stderr, `  matrix:
    homeserver: "https://matrix.org"
    user: "a1bot"
    password: "…"
    room: "#a1:matrix.org"`)
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

        state := matrix.LoadState(filepath.Join(proj.Global().Root(), "matrix", "state.json"))
        logf := func(format string, args ...any) {
                fmt.Fprintf(os.Stderr, "matrix: "+format+"\n", args...)
        }
        client, room, err := matrix.Connect(ctx, matrix.Config{
                Homeserver:  cfg.Homeserver,
                User:        cfg.User,
                AccessToken: cfg.AccessToken,
                Password:    cfg.Password,
                Room:        cfg.Room,
        }, state, logf)
        if err != nil {
                if ctx.Err() != nil {
                        return nil
                }
                return err
        }

        bridge, err := matrix.NewBridge(matrix.Options{
                Client:      client,
                Room:        room,
                LoopbackURL: pageURL,
                State:       state,
                Logger:      logf,
        })
        if err != nil {
                return err
        }

        errc := make(chan error, 1)
        go func() { errc <- bridge.Run(ctx) }()

        fmt.Fprintf(os.Stderr, "matrix bridge: %s as %s — ctrl-c to stop\n", cfg.Room, client.UserID())
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
