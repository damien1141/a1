package doctool

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

func TestDocTool_Definition(t *testing.T) {
	tool := DocTool()
	assert.Equal(t, "docsync", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "documentation")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunDocSync_NoDrift(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

// PublicFunc does something.
func PublicFunc() {}
`)
	writeFile(t, root, "README.md", `# Test

See `+"`"+`PublicFunc`+"`"+` for details.
`)

	raw, _ := json.Marshal(docInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDocSync(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No documentation drift detected", out.Content)
}

func TestRunDocSync_UndocumentedExport(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func UndocumentedFunc() {}
`)
	writeFile(t, root, "README.md", "# Test\n")

	raw, _ := json.Marshal(docInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDocSync(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "UndocumentedFunc")
	assert.Contains(t, out.Content, "not documented")
}

func TestRunDocSync_PhantomDoc(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func RealFunc() {}
`)
	writeFile(t, root, "README.md", `# Test

See `+"`"+`PhantomFunc`+"`"+` for details.
`)

	raw, _ := json.Marshal(docInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDocSync(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "PhantomFunc")
	assert.Contains(t, out.Content, "does not exist")
}

func TestRunDocSync_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		funcName := fmt.Sprintf("Func%d", i)
		writeFile(t, root, fmt.Sprintf("file%d.go", i), fmt.Sprintf(`package main

func %s() {}
`, funcName))
	}
	writeFile(t, root, "README.md", "# Test\n")

	raw, _ := json.Marshal(docInput{Path: root, Glob: "*.go", Limit: 3})
	out, err := runDocSync(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 3, "should respect limit")
}

func TestRunDocSync_EmptyPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(docInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runDocSync(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No documentation drift detected", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
