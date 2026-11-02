package toolmanager

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallBunToolUnknown(t *testing.T) {
	_, err := InstallBunTool(t.Context(), "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown bun-managed tool")
}

func TestInstallBunToolPinsVersion(t *testing.T) {
	var gotArgs []string
	orig := runInstall
	runInstall = func(_ context.Context, bun string, args []string) ([]byte, error) {
		assert.True(t, strings.HasSuffix(bun, "/bun"), "the resolved bun is the one on PATH, got %s", bun)
		gotArgs = args
		return []byte("installed"), nil
	}
	t.Cleanup(func() { runInstall = orig })

	// The CLI lands on PATH for the post-install resolution; a fake bun
	// rides along so LookPath finds the package manager too.
	dir := t.TempDir()
	bin := dir + "/betterwright"
	require.NoError(t, writeFileExecutable(bin))
	require.NoError(t, writeFileExecutable(dir+"/bun"))
	t.Setenv("PATH", dir)

	path, err := InstallBunTool(t.Context(), "betterwright")
	require.NoError(t, err)
	assert.Equal(t, bin, path)
	require.Len(t, gotArgs, 3)
	assert.Equal(t, "install", gotArgs[0])
	assert.Equal(t, "-g", gotArgs[1])
	assert.Equal(t, "betterwright@"+BunTools["betterwright"].Version, gotArgs[2], "the install is version-pinned")
}

func TestInstallBunToolInstallFailureSurfaces(t *testing.T) {
	orig := runInstall
	runInstall = func(_ context.Context, _ string, _ []string) ([]byte, error) {
		return []byte("registry 500"), fmt.Errorf("exit status 1")
	}
	t.Cleanup(func() { runInstall = orig })
	dir := t.TempDir()
	require.NoError(t, writeFileExecutable(dir+"/bun"))
	t.Setenv("PATH", dir)

	_, err := InstallBunTool(t.Context(), "betterwright")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "betterwright@")
	assert.Contains(t, err.Error(), "registry 500")
}

func writeFileExecutable(path string) error {
	return os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755)
}
