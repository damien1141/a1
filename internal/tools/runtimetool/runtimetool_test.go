package runtimetool

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeTool_Definition(t *testing.T) {
	tool := RuntimeTool()
	assert.Equal(t, "runtime", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "test")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunRuntime_ParseEvents(t *testing.T) {
	raw := `{"Action":"run","Package":"pkg","Name":"TestA"}
{"Action":"pass","Package":"pkg","Name":"TestA"}
{"Action":"run","Package":"pkg","Name":"TestB"}
{"Action":"fail","Package":"pkg","Name":"TestB"}
{"Action":"output","Package":"pkg","Name":"TestB","Output":"--- FAIL: TestB (0.00s)\n\tfoo_test.go:42: expected 1, got 2\n"}
`
	events, err := parseTestEvents(raw)
	require.NoError(t, err)
	require.Len(t, events, 5)
}

func TestCollectFailures(t *testing.T) {
	events := []testEvent{
		{Action: "run", Package: "pkg", Name: "TestA"},
		{Action: "pass", Package: "pkg", Name: "TestA"},
		{Action: "run", Package: "pkg", Name: "TestB"},
		{Action: "fail", Package: "pkg", Name: "TestB"},
		{Action: "output", Package: "pkg", Name: "TestB", Output: "FAIL\n"},
	}
	failures := collectFailures(events, 10)
	require.Len(t, failures, 1)
	assert.Equal(t, "TestB", failures[0].Name)
}

func TestRenderRuntimeResults(t *testing.T) {
	failures := []failure{
		{Package: "pkg", Name: "TestB", Output: []string{"FAIL\n"}},
	}
	out := renderRuntimeResults(failures)
	assert.Contains(t, out, "TestB")
	assert.Contains(t, out, "FAIL")
}

func TestRuntimeTool_DetailFromArgs(t *testing.T) {
	tool := RuntimeTool()
	raw, _ := json.Marshal(runtimeInput{Path: "./pkg"})
	detail := tool.DetailFromArgs(raw)
	assert.Equal(t, "runtime ./pkg", detail)
}
