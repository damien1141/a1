package controller

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/job"
)

func TestShouldPublishJobProgressDedup(t *testing.T) {
	c := &EngineController{}
	p := job.Progress{
		JobID:     "j",
		ToolUseID: "t1",
		Name:      "read",
		Status:    "in-progress",
		Detail:    "a.go",
	}
	require.True(t, c.shouldPublishJobProgress(p), "first should publish")
	require.False(t, c.shouldPublishJobProgress(p), "duplicate should drop")
	p.Status = "done"
	require.True(t, c.shouldPublishJobProgress(p), "status change should publish")
	p2 := job.Progress{
		JobID:     "j",
		ToolUseID: "t2",
		Name:      "bash",
		Status:    "done",
		Detail:    "ls",
	}
	require.True(t, c.shouldPublishJobProgress(p2), "new child should publish")
}
