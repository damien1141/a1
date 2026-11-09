package version

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersion_Positive(t *testing.T) {
	// Release procedure: Version is always "vX.Y.Z" — the splash screen,
	// `a1 update` compare links, and the README headers all assume that
	// shape, so a missing "v" or a pre-release suffix is a bug.
	shape := regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	assert.True(t, shape.MatchString(Version), "Version = %q, want shape vX.Y.Z", Version)
}

// oracle: the release-procedure invariant documented in AGENTS.md — the
// "# A1 vX.Y.Z" header of both READMEs must match the constant in this
// package, which is the source of truth. The expected header is derived
// from the constant at runtime, never hardcoded in the test.
func TestVersionReadmeParity_Differential(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must locate this test file")
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	wantHeader := "# A1 " + Version

	for _, name := range []string{"README.md", "README.zh-CN.md"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(repoRoot, name))
			require.NoError(t, err)
			firstLine, _, _ := strings.Cut(string(data), "\n")
			assert.Equal(t, wantHeader, firstLine, "%s header must carry the version constant", name)
		})
	}
}
