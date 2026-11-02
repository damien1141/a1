package bashtool

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runTool(t *testing.T, args string) (string, error) {
	t.Helper()
	res, err := runBash(context.Background(), json.RawMessage(args))
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

func TestBackgroundLifecycle(t *testing.T) {
	ResetBackground()
	defer ResetBackground()

	// Start a shell that prints, sleeps, prints, and exits 0.
	out, err := runTool(t, `{"mode":"start","command":"echo first; sleep 1; echo second"}`)
	require.NoError(t, err)
	require.Contains(t, out, "started bg1")

	// ps lists it.
	out, err = runTool(t, `{"mode":"ps"}`)
	require.NoError(t, err)
	assert.Contains(t, out, "bg1")

	// Attach with a wait budget sees the final output and clean exit.
	out, err = runTool(t, `{"mode":"attach","shell_id":"bg1","wait":5}`)
	require.NoError(t, err)
	assert.Contains(t, out, "first")
	assert.Contains(t, out, "second")
	assert.Contains(t, out, "exited 0")
}

func TestBackgroundKillStopsProcessTree(t *testing.T) {
	ResetBackground()
	defer ResetBackground()

	_, err := runTool(t, `{"mode":"start","command":"sleep 60"}`)
	require.NoError(t, err)

	out, err := runTool(t, `{"mode":"kill","shell_id":"bg1"}`)
	require.NoError(t, err)
	assert.Contains(t, out, "killed")

	// The shell is gone: attach reports the killed state, not running.
	// (Killed shells stay listed for their final output until pruned.)
	out, err = runTool(t, `{"mode":"attach","shell_id":"bg1"}`)
	require.NoError(t, err)
	assert.Contains(t, out, "exited")
	assert.NotContains(t, out, "running")
}

func TestBackgroundAttachWaitsForExit(t *testing.T) {
	ResetBackground()
	defer ResetBackground()

	_, err := runTool(t, `{"mode":"start","command":"sleep 1; echo done"}`)
	require.NoError(t, err)

	start := time.Now()
	out, err := runTool(t, `{"mode":"attach","shell_id":"bg1","wait":10}`)
	require.NoError(t, err)
	elapsed := time.Since(start)
	assert.Contains(t, out, "done")
	assert.GreaterOrEqual(t, elapsed, 900*time.Millisecond, "attach must block until exit")
}

func TestBackgroundUnknownShell(t *testing.T) {
	ResetBackground()
	defer ResetBackground()

	_, err := runTool(t, `{"mode":"attach","shell_id":"bg404"}`)
	require.Error(t, err)
	_, err = runTool(t, `{"mode":"kill","shell_id":"bg404"}`)
	require.Error(t, err)
}

func TestBackgroundOutputIsBoundedTail(t *testing.T) {
	ResetBackground()
	defer ResetBackground()

	// ~10 MB of output against the 8 MB collect cap: only the tail survives.
	_, err := runTool(t, `{"mode":"start","command":"for i in $(seq 1 220000); do echo 0123456789012345678901234567890123456789; done"}`)
	require.NoError(t, err)
	out, err := runTool(t, `{"mode":"attach","shell_id":"bg1","wait":30}`)
	require.NoError(t, err)
	assert.Contains(t, out, "[older output discarded]")
	assert.Less(t, len(out), 2*BashMaxCollectBytes, "retained output must be bounded")
}

func TestForegroundUnchangedWithoutMode(t *testing.T) {
	out, err := runTool(t, `{"command":"echo hello"}`)
	require.NoError(t, err)
	assert.Contains(t, out, "hello")
	assert.NotContains(t, out, "bg-start")

	out, err = runTool(t, `{"command":"false"}`)
	require.NoError(t, err)
	assert.Contains(t, out, "(exit error: exit status 1)")
}
