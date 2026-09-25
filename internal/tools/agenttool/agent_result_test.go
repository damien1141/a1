package agenttool_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/tools"
)

func TestParseAgentResultSummary(t *testing.T) {
	out := `{
  "job_id": "job_1",
  "status": "completed",
  "error": "",
  "summary": "## Findings\n\n- ok\n"
}`
	r := tools.ParseAgentResult(out)
	require.True(t, r.OK)
	require.Equal(t, "job_1", r.JobID)
	require.Equal(t, "completed", r.Status)
	require.Equal(t, "## Findings\n\n- ok", r.RenderableSummary())
}

func TestParseAgentResultRunningNoSummary(t *testing.T) {
	r := tools.ParseAgentResult(`{"job_id":"j","status":"running"}`)
	require.True(t, r.OK)
	require.Empty(t, r.RenderableSummary())
}

func TestParseAgentResultRejectsPlain(t *testing.T) {
	assert.False(t, tools.ParseAgentResult("hello").OK, "expected reject")
}
