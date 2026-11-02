package commands

import (
	"time"

	"github.com/damien1141/a1/internal/components/toast"
	"github.com/damien1141/a1/internal/tui/controller"
)

// publishToast sends a toast message via the bus. Safe to call with a nil bus.
func publishToast(bus *controller.Bus, msg string, kind toast.ToastKind, d time.Duration) {
	if bus != nil {
		bus.Publish(controller.ToastMsg{Message: msg, Kind: kind, Duration: d})
	}
}
