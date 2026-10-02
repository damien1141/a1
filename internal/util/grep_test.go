package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
