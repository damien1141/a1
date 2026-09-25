package contexttool

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

func TestContextTool_Definition(t *testing.T) {
	tool := ContextTool()
	assert.Equal(t, "context", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "context")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunContext_NoIssues(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func main() {
	println("hello")
}
`)

	raw, _ := json.Marshal(contextInput{Path: root, Limit: 10})
	out, err := runContext(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No context issues detected", out.Content)
}

func TestRunContext_FindsContextIssues(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

import (
	"context"
	"net/http"
)

func main() {
	ctx := context.Background()
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	_ = req
}
`)

	raw, _ := json.Marshal(contextInput{Path: root, Limit: 10})
	out, err := runContext(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "context.Background()")
	assert.Contains(t, out.Content, "HTTP request")
}

func TestRunContext_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, root, fmt.Sprintf("file%d.go", i), `package main

import "context"

func main() {
	ctx := context.WithValue(context.Background(), "key", "value")
	_ = ctx
}
`)
	}

	raw, _ := json.Marshal(contextInput{Path: root, Limit: 3})
	out, err := runContext(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 3, "should respect limit")
}

func TestRunContext_EmptyPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(contextInput{Path: root, Limit: 10})
	out, err := runContext(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No context issues detected", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
