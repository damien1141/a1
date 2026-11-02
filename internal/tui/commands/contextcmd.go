package commands

import (
	"fmt"
	"time"

	"github.com/damien1141/a1/internal/contextbreakdown"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/controller"
)

// ContextCommands owns /context: a breakdown of where the context window goes.
//
// Claude Code renders this as a grid of squares sized per category. a1 prints a
// table instead, because the terminal here is line-oriented and the numbers are
// estimates anyway — a grid would imply precision the estimate does not have.
type ContextCommands struct {
	Bus    *controller.Bus
	Snap   func() session.Snapshot
	Window func() int
	// Tools returns the registered tool schema bytes and their count.
	Tools func() (bytes, count int)
}

// Register wires /context into r.
func (c *ContextCommands) Register(r *CommandRegistry) {
	if c == nil || r == nil {
		return
	}
	r.Register(Command{
		Name:        "context",
		Description: "Show where the context window is spent",
		Slash:       true,
		Insert:      "/context",
		Run: func(_ Context, _ []string) error {
			c.Show()
			return nil
		},
	})
}

// Show writes the breakdown into the transcript.
//
// It goes to the transcript rather than a toast: a toast is one line and this is
// a table. Anyone running /context is asking a question a table answers and a
// toast cannot.
func (c *ContextCommands) Show() {
	if c == nil || c.Bus == nil {
		return
	}
	var snap session.Snapshot
	if c.Snap != nil {
		snap = c.Snap()
	}
	window := 0
	if c.Window != nil {
		window = c.Window()
	}
	toolBytes, toolCount := 0, 0
	if c.Tools != nil {
		toolBytes, toolCount = c.Tools()
	}

	body := contextbreakdown.Build(snap, window, toolBytes, toolCount).Estimate()
	c.Bus.Publish(controller.SessionEventMsg{Event: session.AssistantMessageUpdate{
		Message: session.Message{
			ID:      fmt.Sprintf("context-%d", time.Now().UnixNano()),
			State:   session.StateComplete,
			Text:    body,
			Content: []session.ContentBlock{{Type: session.BlockText, Text: body}},
		},
	}})
}
