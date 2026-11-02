package controller_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tui/controller"
)

func TestBusOnWakeOnceUntilDrain(t *testing.T) {
	var wakes int
	b := controller.NewBus(func() { wakes++ })
	b.Publish(controller.JobProgressMsg{Progress: job.Progress{JobID: "j", ToolUseID: "t1", Status: "in-progress"}})
	b.Publish(controller.JobProgressMsg{Progress: job.Progress{JobID: "j", ToolUseID: "t1", Status: "done"}})
	b.Publish(controller.JobProgressMsg{Progress: job.Progress{JobID: "j", ToolUseID: "t2", Status: "done"}})
	require.Equal(t, 1, wakes, "wakes before Drain")
	batch := b.Drain()
	require.Len(t, batch, 2)
	b.Publish(controller.JobProgressMsg{Progress: job.Progress{JobID: "j", ToolUseID: "t3", Status: "done"}})
	require.Equal(t, 2, wakes, "wakes after re-arm")
}

func TestBusCoalesceNonAdjacentAssistant(t *testing.T) {
	var wakes int
	b := controller.NewBus(func() { wakes++ })
	b.Publish(controller.SessionEventMsg{Event: session.AssistantMessageUpdate{Message: session.Message{
		ID: "a1", Text: "one", State: session.StateStreaming,
	}}})
	b.Publish(controller.JobProgressMsg{Progress: job.Progress{JobID: "j", ToolUseID: "t1", Status: "in-progress"}})
	b.Publish(controller.SessionEventMsg{Event: session.AssistantMessageUpdate{Message: session.Message{
		ID: "a1", Text: "two", State: session.StateStreaming,
	}}})
	require.Equal(t, 1, wakes)
	batch := b.Drain()
	require.Len(t, batch, 2, "assistant coalesced across progress")
	te := batch[0].(controller.SessionEventMsg)
	upd := te.Event.(session.AssistantMessageUpdate)
	require.Equal(t, "two", upd.Message.Text)
}

func TestBusCoalesceJobProgressAcrossSession(t *testing.T) {
	b := controller.NewBus(nil)
	b.Publish(controller.JobProgressMsg{Progress: job.Progress{JobID: "j", ToolUseID: "t1", Status: "in-progress"}})
	b.Publish(controller.SessionEventMsg{Event: session.AssistantMessageUpdate{Message: session.Message{
		ID: "a1", Text: "x", State: session.StateStreaming,
	}}})
	b.Publish(controller.JobProgressMsg{Progress: job.Progress{JobID: "j", ToolUseID: "t1", Status: "done"}})
	batch := b.Drain()
	require.Len(t, batch, 2)
	jp := batch[0].(controller.JobProgressMsg)
	require.Equal(t, "done", jp.Progress.Status)
}
