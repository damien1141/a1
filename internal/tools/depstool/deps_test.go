package depstool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDepsTool_Definition(t *testing.T) {
	tool := DepsTool()
	assert.Equal(t, "deps", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "dependency")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunDeps_NoExternalDeps(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func main() {}
`)

	raw, _ := json.Marshal(depsInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDeps(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No external dependencies found", out.Content)
}

func TestRunDeps_FindsExternalDeps(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)

	raw, _ := json.Marshal(depsInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDeps(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "fmt")
}

func TestRunDeps_SkipsInternalImports(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

import . "fmt"

func main() {}
`)

	raw, _ := json.Marshal(depsInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDeps(t.Context(), raw)
	require.NoError(t, err)
	assert.NotContains(t, out.Content, "fmt")
}

func TestRunDeps_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		pkg := fmt.Sprintf("pkg%d", i)
		writeFile(t, root, fmt.Sprintf("file%d.go", i), fmt.Sprintf(`package main

import "%s"

func main() {}
`, pkg))
	}

	raw, _ := json.Marshal(depsInput{Path: root, Glob: "*.go", Limit: 3})
	out, err := runDeps(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 3, "should respect limit")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
