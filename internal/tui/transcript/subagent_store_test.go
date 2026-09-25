package transcript_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components/status"
	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/tools"
	"github.com/damien1141/a1/internal/tui/transcript"
)

func TestSubagentStoreProgressAndResult(t *testing.T) {
	s := transcript.NewSubagentStore()
	s.Bind("job1", "parent1")
	s.ApplyProgress(job.Progress{
		JobID:           "job1",
		ParentToolUseID: "parent1",
		ToolUseID:       "c1",
		Name:            "read",
		Status:          "in-progress",
		Detail:          "a.go",
	})
	s.ApplyProgress(job.Progress{
		JobID:           "job1",
		ParentToolUseID: "parent1",
		ToolUseID:       "c1",
		Name:            "read",
		Status:          "done",
		Detail:          "a.go",
	})
	s.ApplyProgress(job.Progress{
		JobID:           "job1",
		ParentToolUseID: "parent1",
		ToolUseID:       "c2",
		Name:            "bash",
		Status:          "done",
		Detail:          "test",
	})

	kids := s.Children("parent1")
	require.Len(t, kids, 2)
	require.Equal(t, status.ToolDone, kids[0].Status)
	require.Equal(t, "read", kids[0].Name)
	byJob := s.ChildrenByJob("job1")
	require.Len(t, byJob, 2)

	s.ApplyResult("parent1", tools.ParseAgentResult(`{
		"job_id":"job1","status":"completed","summary":"## Ok"
	}`))
	// Summary is stored; Children unchanged.
	require.Len(t, s.Children("parent1"), 2, "children should not be cleared")
}
