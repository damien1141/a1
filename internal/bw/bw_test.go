package bw

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDaemon swaps RunCommand for an in-test responder and restores it after.
func fakeDaemon(t *testing.T, fn func(bin string, args []string) ([]byte, error)) {
	t.Helper()
	orig := RunCommand
	RunCommand = func(_ context.Context, bin string, args []string, _ []string) ([]byte, error) {
		return fn(bin, args)
	}
	t.Cleanup(func() { RunCommand = orig })
}

func TestRunParsesEnvelopeAndArgs(t *testing.T) {
	fakeDaemon(t, func(bin string, args []string) ([]byte, error) {
		assert.Equal(t, "/usr/bin/fake-bw", bin)
		require.Contains(t, args, "run")
		require.Contains(t, args, "--session")
		require.Equal(t, Session, args[1+indexOf(args, "--session")])
		require.NotContains(t, args, "--profile", "default profile passes no --profile flag")
		require.Contains(t, args, "-c")
		require.Contains(t, args, "return page.title()")
		return []byte(`{"ok":true,"result":"Example Domain"}`), nil
	})
	orig := Resolve
	Resolve = func() (string, error) { return "/usr/bin/fake-bw", nil }
	t.Cleanup(func() { Resolve = orig })

	env, err := Run(t.Context(), "", false, "return page.title()")
	require.NoError(t, err)
	assert.True(t, env.Ok)
	assert.Equal(t, `"Example Domain"`, string(env.Result))
}

func TestRunNamedProfilePassesFlag(t *testing.T) {
	fakeDaemon(t, func(_ string, args []string) ([]byte, error) {
		i := indexOf(args, "--profile")
		require.GreaterOrEqual(t, i, 0)
		require.Less(t, i+1, len(args))
		assert.Equal(t, "work", args[i+1])
		return []byte(`{"ok":true,"result":true}`), nil
	})
	orig := Resolve
	Resolve = func() (string, error) { return "/usr/bin/fake-bw", nil }
	t.Cleanup(func() { Resolve = orig })

	_, err := Run(t.Context(), "work", true, "return true")
	require.NoError(t, err)
}

func TestRunFailureEnvelopeIsError(t *testing.T) {
	fakeDaemon(t, func(_ string, _ []string) ([]byte, error) {
		return []byte(`{"ok":false,"error":"navigation timeout"}`), nil
	})
	orig := Resolve
	Resolve = func() (string, error) { return "/usr/bin/fake-bw", nil }
	t.Cleanup(func() { Resolve = orig })

	_, err := Run(t.Context(), "", false, "await page.goto('x')")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "navigation timeout")
}

func TestRunUnavailableIsActionable(t *testing.T) {
	ResetForTest()
	t.Setenv("PATH", t.TempDir()) // nothing resolvable
	t.Cleanup(func() { ResetForTest() })
	_, err := Run(t.Context(), "", false, "return true")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bun install -g betterwright")
}

func TestEnvelopeHandoffReason(t *testing.T) {
	env := Envelope{Challenges: []Challenge{{
		Provider: "cloudflare",
		URL:      "https://shop.example.com/checkout",
		Advice:   "solve before retrying",
	}}}
	reason := env.HandoffReason()
	assert.Contains(t, reason, "cloudflare")
	assert.Contains(t, reason, "shop.example.com")
	assert.Contains(t, reason, "solve before retrying")
	assert.Empty(t, Envelope{}.HandoffReason())
}

func TestCloseAllStopsEveryDaemon(t *testing.T) {
	var got []string
	fakeDaemon(t, func(_ string, args []string) ([]byte, error) {
		got = args
		return []byte(`{}`), nil
	})
	orig := Resolve
	Resolve = func() (string, error) { return "/usr/bin/fake-bw", nil }
	t.Cleanup(func() { Resolve = orig })

	require.NoError(t, Close(t.Context(), true, ""))
	assert.Contains(t, got, "--all")
}

func TestJSStringEscapes(t *testing.T) {
	assert.Equal(t, `'it\'s \\ fine'`, JSString(`it's \ fine`))
	assert.Equal(t, `'a\nb'`, JSString("a\nb"))
}

func TestProfilesDirHonorsHome(t *testing.T) {
	t.Setenv("BETTERWRIGHT_HOME", "/tmp/bwhome")
	assert.Equal(t, filepath.Join("/tmp/bwhome", "browser", "profiles"), ProfilesDir())
}

func TestSpilledResultDetected(t *testing.T) {
	// Large results spill to a file; the envelope then carries
	// {truncated, preview, fullOutputPath}. The shape is documented in
	// BetterWright's browser API; this test pins our awareness of it.
	var spilled struct {
		Truncated      bool   `json:"truncated"`
		Preview        string `json:"preview"`
		FullOutputPath string `json:"fullOutputPath"`
	}
	require.NoError(
		t,
		json.Unmarshal([]byte(`{"truncated":true,"preview":"…","fullOutputPath":"/tmp/x.json"}`), &spilled),
	)
	assert.True(t, spilled.Truncated)
	assert.Equal(t, "/tmp/x.json", spilled.FullOutputPath)
}

func indexOf(xs []string, want string) int {
	for i, x := range xs {
		if x == want {
			return i
		}
	}
	return -1
}

var (
	_ = fmt.Sprintf
	_ = os.Getenv
)
