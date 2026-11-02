package transcript

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/status"
	"github.com/damien1141/a1/internal/session"
)

func TestTranscriptPane_ApplySessionAndSync(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	pane := NewTranscriptPane(th, spin, "A1 test")

	pane.ApplySession(session.UserAppend{Text: "hello"})
	pane.Sync()

	require.False(t, pane.IsEmpty(), "expected transcript entries after user append")
	require.Len(t, pane.Snapshot().Messages, 1)
}

func TestTranscriptPane_IsStreaming(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	pane := NewTranscriptPane(th, spin, "A1 test")

	require.False(t, pane.IsStreaming(), "empty pane should not stream")

	pane.ApplySession(session.AssistantMessageUpdate{Message: session.Message{
		ID:    "a1",
		State: session.StateStreaming,
	}})
	require.True(t, pane.IsStreaming(), "expected streaming after assistant StateStreaming")

	pane.ApplySession(session.AssistantMessageUpdate{Message: session.Message{
		ID:    "a1",
		State: session.StateComplete,
	}})
	require.False(t, pane.IsStreaming(), "expected idle after StreamEnd")
}

func TestTranscriptPane_LoadReplayClearsWidgets(t *testing.T) {
	th := components.DefaultTheme()
	spin := status.NewSpinner(th.ToolName)
	pane := NewTranscriptPane(th, spin, "A1 test")

	pane.ApplySession(session.UserAppend{Text: "x"})
	pane.Sync()
	require.False(t, pane.IsEmpty(), "setup: expected entries")

	pane.LoadReplay(session.Snapshot{})
	pane.Sync()
	require.True(t, pane.IsEmpty(), "LoadReplay should clear visible entries until snap has items")
}
