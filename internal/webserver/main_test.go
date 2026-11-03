package webserver

import (
	"os"
	"testing"

	"github.com/damien1141/a1/internal/permission"
)

// TestMain pins the host sandbox probe to "unsupported" for the whole
// package so gate decisions are deterministic on every machine: on a
// Landlock host the real probe would otherwise flip every unpinned Ask
// expectation into a sandbox-folded Allow (containment substitutes for
// approval). The bash contracts under test here — mode folding
// (TestModeSwitchFoldsBashAsk) and the DefaultPolicy fallback
// (TestPolicyFallbackIsNotPermissive) — are about the permission algebra,
// not the host's Landlock support; the containment path itself is pinned
// in the permission package's sandbox tests, which opt in explicitly.
func TestMain(m *testing.M) {
	restore := permission.PinSandboxSupportedForTest(false)
	defer restore()
	os.Exit(m.Run())
}
