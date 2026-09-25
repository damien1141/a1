package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	cli "github.com/pulseaiclub/pli"

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

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	addr := ln.Addr().(*net.TCPAddr)
	pageURL := fmt.Sprintf("http://127.0.0.1:%d/", addr.Port)
	fmt.Fprintf(os.Stderr, "a1 config: %s\n  config: %s\n  Ctrl-C to stop\n", pageURL, proj.Global().ConfigFile())
	openBrowser(ctx, pageURL)

	srv := &http.Server{
		Handler:           &configHandler{configPath: proj.Global().ConfigFile()},
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		_ = srv.Close()
	}
	return nil
}
