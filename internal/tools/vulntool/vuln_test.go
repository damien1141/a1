package vulntool

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

func TestVulnTool_Definition(t *testing.T) {
	tool := VulnTool()
	assert.Equal(t, "vuln", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "vulnerability")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunVuln_NoPatterns(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func main() {
	println("hello")
}
`)

	raw, _ := json.Marshal(vulnInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runVuln(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No vulnerability patterns detected", out.Content)
}

func TestRunVuln_FindsPatterns(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

import (
	"crypto/md5"
	"fmt"
	"os/exec"
)

func main() {
	h := md5.New()
	_ = h
	cmd := exec.Command("sh", "-c", userInput)
	_ = cmd
	fmt.Sprintf("SELECT * FROM users WHERE id = %s", userInput)
}
`)

	raw, _ := json.Marshal(vulnInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runVuln(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "MD5")
	assert.Contains(t, out.Content, "command injection")
	assert.Contains(t, out.Content, "SQL injection")
}

func TestRunVuln_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, root, fmt.Sprintf("file%d.go", i), `package main

import "crypto/md5"

func main() {
	h := md5.New()
	_ = h
}
`)
	}

	raw, _ := json.Marshal(vulnInput{Path: root, Glob: "*.go", Limit: 3})
	out, err := runVuln(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 3, "should respect limit")
}

func TestRunVuln_EmptyPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(vulnInput{Path: root, Glob: "*.go", Limit: 10})
	out, err := runVuln(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No vulnerability patterns detected", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
