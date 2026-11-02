package util

import (
        "os"
        "path/filepath"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// TestSplitGrepLine_WindowsDriveLetter is the regression guard for the
// windows-latest CI failures in api/deadcode/impact/stack tools. A naive
// SplitN(line, ":", 3) takes "C" as the path for
// "C:\Users\runneradmin\project\main.go:5:content", so every grep-derived
// reference count silently came back wrong on Windows.
func TestSplitGrepLine_WindowsDriveLetter(t *testing.T) {
        file, line, content, ok := SplitGrepLine(`C:\Users\runneradmin\project\main.go:5:func main() {}`)
        assert.True(t, ok)
        assert.Equal(t, `C:\Users\runneradmin\project\main.go`, file)
        assert.Equal(t, 5, line)
        assert.Equal(t, "func main() {}", content)
}

func TestSplitGrepLine_UnixPath(t *testing.T) {
        file, line, content, ok := SplitGrepLine("/home/solis/project/main.go:5:func main() {}")
        assert.True(t, ok)
        assert.Equal(t, "/home/solis/project/main.go", file)
        assert.Equal(t, 5, line)
        assert.Equal(t, "func main() {}", content)
}

func TestSplitGrepLine_NoMatch(t *testing.T) {
        _, _, _, ok := SplitGrepLine("")
        assert.False(t, ok)
}

func TestSplitStackFrame_WindowsDriveLetter(t *testing.T) {
        path, line, ok := SplitStackFrame(`C:\Users\runneradmin\project\main.go:42`)
        assert.True(t, ok)
        assert.Equal(t, `C:\Users\runneradmin\project\main.go`, path)
        assert.Equal(t, 42, line)
}

func TestSplitStackFrame_UnixPath(t *testing.T) {
        path, line, ok := SplitStackFrame("/home/solis/project/main.go:42")
        assert.True(t, ok)
        assert.Equal(t, "/home/solis/project/main.go", path)
        assert.Equal(t, 42, line)
}

// TestCountGoReferences_* is the regression suite for #7: the previous
// countReferences implementations in apitool and deadcode shelled out to
// `grep -r`, which is fragile (depends on a `grep` binary in PATH) and
// POSIX-only (Windows has no `grep` by default). The Go-native replacement
// walks the tree itself and uses Go identifier boundaries so `Foo` doesn't
// match `FooBar` (substring grep did, and inflated counts).

func TestCountGoReferences_BasicCount(t *testing.T) {
        root := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(root, "def.go"),
                []byte("package main\n\nfunc Target() {}\n"), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "caller.go"),
                []byte("package main\n\nfunc use() { Target() }\n"), 0o644))

        n, err := CountGoReferences(root, filepath.Join(root, "def.go"), "Target")
        require.NoError(t, err)
        assert.Equal(t, 1, n, "one reference in caller.go should be counted")
}

func TestCountGoReferences_SkipsDefiningFile(t *testing.T) {
        root := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(root, "def.go"),
                []byte("package main\n\nfunc Target() {}\nfunc use() { Target() }\n"), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "caller.go"),
                []byte("package main\n\nfunc use() { Target() }\n"), 0o644))

        n, err := CountGoReferences(root, filepath.Join(root, "def.go"), "Target")
        require.NoError(t, err)
        assert.Equal(t, 1, n, "self-reference in def.go must be excluded")
}

func TestCountGoReferences_SkipsTestFiles(t *testing.T) {
        root := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(root, "def.go"),
                []byte("package main\n\nfunc Target() {}\n"), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "def_test.go"),
                []byte("package main\n\nimport \"testing\"\nfunc TestTarget(t *testing.T) { Target() }\n"), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "caller.go"),
                []byte("package main\n\nfunc use() { Target() }\n"), 0o644))

        n, err := CountGoReferences(root, filepath.Join(root, "def.go"), "Target")
        require.NoError(t, err)
        assert.Equal(t, 1, n, "test files must not count toward usage")
}

func TestCountGoReferences_IdentifierBoundary(t *testing.T) {
        root := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(root, "def.go"),
                []byte("package main\n\nfunc Target() {}\n"), 0o644))
        // `TargetBar` and `myTarget` should NOT match `Target` under \b anchoring.
        require.NoError(t, os.WriteFile(filepath.Join(root, "false_positive.go"),
                []byte("package main\n\nfunc TargetBar() {}\nfunc myTarget() {}\n"), 0o644))
        // A true `Target()` call should match.
        require.NoError(t, os.WriteFile(filepath.Join(root, "real.go"),
                []byte("package main\n\nfunc use() { Target() }\n"), 0o644))

        n, err := CountGoReferences(root, filepath.Join(root, "def.go"), "Target")
        require.NoError(t, err)
        assert.Equal(t, 1, n, "identifier-boundary anchoring must reject FooBar / myFoo")
}

func TestCountGoReferences_SkipsVendorAndBuildDirs(t *testing.T) {
        root := t.TempDir()
        require.NoError(t, os.MkdirAll(filepath.Join(root, "vendor"), 0o755))
        require.NoError(t, os.WriteFile(filepath.Join(root, "vendor", "v.go"),
                []byte("package v\n\nfunc use() { Target() }\n"), 0o644))
        require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
        require.NoError(t, os.WriteFile(filepath.Join(root, ".git", "config"),
                []byte("Target\n"), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "def.go"),
                []byte("package main\n\nfunc Target() {}\n"), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "real.go"),
                []byte("package main\n\nfunc use() { Target() }\n"), 0o644))

        n, err := CountGoReferences(root, filepath.Join(root, "def.go"), "Target")
        require.NoError(t, err)
        assert.Equal(t, 1, n, "vendor/, .git/, and similar dirs must be skipped")
}

func TestCountGoReferences_EmptySymbol(t *testing.T) {
        root := t.TempDir()
        n, err := CountGoReferences(root, "", "")
        require.NoError(t, err)
        assert.Equal(t, 0, n)
}

func TestCountGoReferences_NoMatches(t *testing.T) {
        root := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(root, "def.go"),
                []byte("package main\n\nfunc Target() {}\n"), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "caller.go"),
                []byte("package main\n\nfunc use() {}\n"), 0o644))

        n, err := CountGoReferences(root, filepath.Join(root, "def.go"), "Target")
        require.NoError(t, err)
        assert.Equal(t, 0, n, "no references = zero count, no error")
}
