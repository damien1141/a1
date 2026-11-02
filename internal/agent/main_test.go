package agent

import (
	"os"
	"testing"
)

// TestMain pins HOME (and USERPROFILE, its Windows twin) to a throwaway dir
// for the whole test binary. Every Engine binds a memory bank at construction
// (bindExecutor -> Session.MemoryBank), and a session without an attached
// persona store falls back to project.GetDefaultProject() — a process-global
// singleton that mkdirs ~/.a1 and opens per-session banks under
// ~/.a1/sessions/memory. Without this pin, every engine-having test leaked a
// bank file into the developer's real ~/.a1 (40+ files per run) and parallel
// package test binaries fought over the same real home. The env is set before
// any test runs, so the singleton resolves inside the sandbox on its one and
// only initialization; per-test t.Setenv cannot fix this, because the first
// test to trigger the singleton would pin it to a dir that t.TempDir cleanup
// deletes underneath the later tests.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "a1-agent-test-home-")
	if err != nil {
		panic(err)
	}
	// os.UserHomeDir reads HOME on Unix, USERPROFILE on Windows.
	_ = os.Setenv("HOME", home)
	_ = os.Setenv("USERPROFILE", home)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
