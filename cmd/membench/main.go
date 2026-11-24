package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/damien1141/a1/internal/memchunk"
	"github.com/damien1141/a1/internal/project"
)

// membench runs the memory-extraction benchmark against the configured decision
// model.
//
// The judgement lives in internal/memchunk (RunBenchmark); this command only
// wires a project config so the jev settings resolve, and reports.
//
//	go run ./cmd/membench
//
// Exit codes: 0 ran, 1 errored, 2 no decision model configured.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	proj := project.GetDefaultProject()
	if proj != nil {
		_ = proj.LoadConfig()
	}

	if err := memchunk.RunBenchmarkCLI(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "membench:", err)
		os.Exit(1)
	}
}
