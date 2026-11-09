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

import "./local"

func main() {}
`)

	raw, _ := json.Marshal(depsInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDeps(t.Context(), raw)
	require.NoError(t, err)
	// Relative paths (leading ".") are the non-external ones; a dot-import's
	// *path* is still external, so the fixture must use "./local" here.
	assert.Equal(t, "No external dependencies found", out.Content)
}

func TestRunDeps_GroupedAndAliasedImports(t *testing.T) {
	// The go/parser pass must see grouped `import ( ... )` blocks and aliased
	// imports — the historic line regexp matched neither, so idiomatic files
	// reported no dependencies at all.
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

import (
        "fmt"

        str "strings"
)

func main() {
        fmt.Println(str.ToUpper("x"))
}
`)
	writeFile(t, root, "blank.go", `package main

import (
        _ "embed"
)
`)

	raw, _ := json.Marshal(depsInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDeps(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "fmt")
	assert.Contains(t, out.Content, "strings")
	assert.Contains(t, out.Content, "embed")
}

func TestRunDeps_MalformedFileFallsBackToRegexp(t *testing.T) {
	// A file the parser rejects (no package clause) still gets the historic
	// single-line regexp scan instead of being dropped entirely.
	root := t.TempDir()
	writeFile(t, root, "snippet.go", "import \"fmt\"\n")

	raw, _ := json.Marshal(depsInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDeps(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "fmt")
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
