package commands

import (
	"context"
	"errors"
	"time"

	"github.com/damien1141/a1/internal/components/toast"
	"github.com/damien1141/a1/internal/tui/controller"
)

// CompactCommands owns /compact: a user-initiated context compaction.
//
// The engine compacts automatically when the context crosses the growth-gate
// threshold or the safety valve. /compact lets the user trigger it on demand
// — useful when the user is about to paste a large reference and wants to
// free up the window first, or when a long tool run filled the context
// faster than the gate expected.
type CompactCommands struct {
	Bus     *controller.Bus
	Ctrl    *controller.EngineController
	// StreamActive reports whether a reply or command is running. /compact
	// is refused while a stream is active because compaction mutates the
	// session tree the engine is mid-turn building, which would race.
	StreamActive func() bool
}

// Register wires /compact into r.
func (c *CompactCommands) Register(r *CommandRegistry) {
	if c == nil || r == nil {
		return
	}
	r.Register(Command{
		Name:        "compact",
		Description: "Compact the context window now",
		Slash:       true,
		Insert:      "/compact",
		Run: func(_ Context, _ []string) error {
			c.Run(context.Background())
			return nil
		},
	})
}

// Run triggers a forced compaction and toasts the outcome.
//
// Three outcomes, each with a distinct toast:
//   - compaction ran: green "Context compacted"
//   - nothing to summarize: warning "Nothing to compact"
//   - error: red "Compaction failed: <err>"
//
// Refused entirely (warning toast, no compaction attempt) when a stream is
// active. Compaction while the engine is mid-turn would race the engine's
// own session-tree mutation, so the guard is conservative.
func (c *CompactCommands) Run(ctx context.Context) {
	if c == nil {
		return
	}
	if c.StreamActive != nil && c.StreamActive() {
		publishToast(c.Bus, "Cannot compact while a reply or command is running", toast.ToastWarning, 3*time.Second)
		return
	}
	if c.Ctrl == nil {
		publishToast(c.Bus, "Compaction unavailable (engine not configured)", toast.ToastError, 3*time.Second)
		return
	}
	err := c.Ctrl.ForceCompact(ctx)
	switch {
	case err == nil:
		publishToast(c.Bus, "Context compacted", toast.ToastSuccess, 3*time.Second)
	case errors.Is(err, controller.ErrNothingToCompact):
		publishToast(c.Bus, "Nothing to compact", toast.ToastWarning, 3*time.Second)
	default:
		publishToast(c.Bus, "Compaction failed: "+err.Error(), toast.ToastError, 4*time.Second)
	}
}
