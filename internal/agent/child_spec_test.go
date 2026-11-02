package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/agent"
	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/tools"
)

func TestSpecForRoleExplore(t *testing.T) {
	spec := agent.SpecForRole(job.RoleExplore)
	assert.Equal(t, job.RoleExplore, spec.Role)
	assert.Equal(t, permission.ModeReadonly, spec.Mode)
	assert.Contains(t, spec.Hint, "explore sub-agent")
	assert.Contains(t, spec.Hint, "cannot modify files")
	names := toolNameSet(spec.Tools)
	assert.True(t, names["read"])
	assert.True(t, names["bash"])
	assert.False(t, names["write"])
	// `edit` is no longer a separate tool — it's mode=edit of `write`.
	// `agent_spawn` is added separately by the executor when jobs are wired,
	// so it isn't part of the role's tool set.
	assert.False(t, names["agent_spawn"])
}

func TestSpecForRoleWorker(t *testing.T) {
	spec := agent.SpecForRole(job.RoleWorker)
	assert.Equal(t, job.RoleWorker, spec.Role)
	assert.Equal(t, permission.ModeHeadlessStrict, spec.Mode)
	assert.Contains(t, spec.Hint, "worker sub-agent")
	assert.Contains(t, spec.Hint, "spawn your own sub-agents")
	names := toolNameSet(spec.Tools)
	assert.True(t, names["write"])
	// `edit` was folded into `write` (mode=edit). The worker role still
	// has full write/edit capability — both behind the `write` tool.
	assert.True(t, names["read"])
	assert.False(t, names["agent_spawn"])
}

func TestSpecForRoleReview(t *testing.T) {
	spec := agent.SpecForRole(job.RoleReview)
	assert.Equal(t, job.RoleReview, spec.Role)
	assert.Equal(t, permission.ModeReadonly, spec.Mode)
	assert.Contains(t, spec.Hint, "review sub-agent")
	assert.NotContains(t, spec.Hint, "allowlisted")
	names := toolNameSet(spec.Tools)
	assert.True(t, names["bash"])
	// `grep` was folded into `search` (mode=grep). The review role
	// still has grep capability — behind the `search` tool.
	assert.True(t, names["search"])
	assert.False(t, names["write"])
}

func TestSpecForRoleEmptyDefaultsExplore(t *testing.T) {
	spec := agent.SpecForRole("")
	assert.Equal(t, job.RoleExplore, spec.Role)
}

func TestParseRole(t *testing.T) {
	r, err := job.ParseRole("")
	require.NoError(t, err)
	assert.Equal(t, job.RoleExplore, r)

	_, err = job.ParseRole("nope")
	require.Error(t, err)
}

func toolNameSet(list []tools.Tool) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, tool := range list {
		m[tool.Definition.Name] = true
	}
	return m
}
