package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/session"
)

func TestActivityLabelNamesRunningTool(t *testing.T) {
	h := NewActivityHandler(nil)
	h.Apply(ActivityTools)

	snap := session.Snapshot{Tools: map[string]session.ToolRun{
		"e": {ToolUseID: "e", Name: "edit", Status: session.ToolInProgress, Detail: "internal/permission/gate.go"},
	}}
	assert.Equal(t, "Running edit · internal/permission/gate.go…", h.Label(snap),
		"the slot must say which tool and on what file")
}

func TestActivityLabelOmitsDetailWhenAbsent(t *testing.T) {
	h := NewActivityHandler(nil)
	h.Apply(ActivityTools)

	snap := session.Snapshot{Tools: map[string]session.ToolRun{
		"a": {ToolUseID: "a", Name: "agent_spawn", Status: session.ToolInProgress},
	}}
	assert.Equal(t, "Running agent_spawn…", h.Label(snap))
}

func TestActivityLabelCountsConcurrentTools(t *testing.T) {
	h := NewActivityHandler(nil)
	h.Apply(ActivityTools)

	snap := session.Snapshot{Tools: map[string]session.ToolRun{
		"a": {ToolUseID: "a", Name: "grep", Status: session.ToolInProgress},
		"b": {ToolUseID: "b", Name: "read", Status: session.ToolInProgress},
	}}
	assert.Equal(t, "Calling 2 tools…", h.Label(snap), "with several in flight a count beats a name")
}

func TestActivityLabelIgnoresLocalBash(t *testing.T) {
	h := NewActivityHandler(nil)
	h.Apply(ActivityTools)

	snap := session.Snapshot{Tools: map[string]session.ToolRun{
		"l": {ToolUseID: "l", Name: "bash", Status: session.ToolInProgress, Local: true},
	}}
	// No agent tool runs, so the activity message falls back to the generic label.
	assert.Equal(t, "Calling tools…", h.Label(snap),
		"a user '!cmd' run must not be reported as an agent tool call")
}

func TestActivityLabelOtherActivitiesUnchanged(t *testing.T) {
	// A slice, not a map: map iteration order would make the final Apply
	// (and the idle assertion after it) nondeterministic.
	cases := []struct {
		act  Activity
		want string
	}{
		{ActivitySubmitting, "Sending…"},
		{ActivityWaiting, "Awaiting reply…"},
		{ActivityStreaming, "Generating…"},
		{ActivityCompacting, "Auto-compacting…"},
		{ActivityAwaitingApproval, "Waiting for approval…"},
	}
	h := NewActivityHandler(nil)
	for _, tc := range cases {
		h.Apply(tc.act)
		assert.Equal(t, tc.want, h.Label(session.Snapshot{}), "activity %d", tc.act)
	}

	h.Apply(ActivityIdle)
	assert.Empty(t, h.Label(session.Snapshot{}), "idle has no slot text")
}

// The interrupt line invites the redirect, so the slot says what to do next
// rather than just reporting the stop.
func TestActivityLabelCancelledInvitesRedirect(t *testing.T) {
	h := NewActivityHandler(nil)
	h.Apply(ActivityCancelled)
	assert.Equal(t, "Stopped · What should I do instead?", h.Label(session.Snapshot{}))
}
