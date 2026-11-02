package agent_test

import (
        "context"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/agent"
        "github.com/damien1141/a1/internal/job"
        "github.com/damien1141/a1/internal/llm"
)

func TestNewEngineRegistersJobs(t *testing.T) {
        mgr, err := job.New(job.Options{
                Root: t.TempDir(),
                Runner: job.RunnerFunc(func(_ context.Context, _ job.RunEnv) (string, error) {
                        return "ok", nil
                }),
        })
        require.NoError(t, err)
        t.Cleanup(func() { _ = mgr.Close(t.Context()) })

        sess, err := agent.NewSession(agent.WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := agent.NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:9", APIKey: "x"},
                sess,
                agent.WithJobs(mgr),
        )
        require.NoError(t, err)
        assert.True(t, eng.HasTool("agent_spawn"))

        eng.SetModel(llm.ModelConfig{Name: "fake2", BaseURL: "http://127.0.0.1:9", APIKey: "x"})
        assert.True(t, eng.HasTool("agent_spawn"))
}

func TestSetJobsTogglesAgentTools(t *testing.T) {
        mgr, err := job.New(job.Options{
                Root: t.TempDir(),
                Runner: job.RunnerFunc(func(_ context.Context, _ job.RunEnv) (string, error) {
                        return "ok", nil
                }),
        })
        require.NoError(t, err)
        t.Cleanup(func() { _ = mgr.Close(t.Context()) })

        sess, err := agent.NewSession(agent.WithCwd(t.TempDir()))
        require.NoError(t, err)
        eng, err := agent.NewEngine(
                llm.ModelConfig{Name: "fake", BaseURL: "http://127.0.0.1:9", APIKey: "x"},
                sess,
        )
        require.NoError(t, err)
        assert.False(t, eng.HasTool("agent_spawn"))

        eng.SetJobs(mgr)
        assert.True(t, eng.HasTool("agent_spawn"))

        eng.SetJobs(nil)
        assert.False(t, eng.HasTool("agent_spawn"))
        assert.True(t, eng.HasTool("bash")) // default tools still present
}

func TestChildToolsHaveNoAgent(t *testing.T) {
        for _, tool := range agent.ChildTools() {
                assert.NotContains(t, tool.Definition.Name, "agent_")
        }
}

func TestChildToolsAreReadonly(t *testing.T) {
        names := map[string]bool{}
        for _, tool := range agent.ChildTools() {
                names[tool.Definition.Name] = true
        }
        assert.True(t, names["read"])
        // `grep` was folded into `search` (mode=grep). The child tool set
        // still has grep capability — behind the `search` tool.
        assert.True(t, names["search"])
        assert.True(t, names["bash"])
        assert.False(t, names["write"])
        // `edit` was folded into `write` (mode=edit). Child tool set is
        // read-only, so neither `write` nor `edit` should be present.
        assert.False(t, names["edit"])
}
