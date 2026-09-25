package secrettool

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

func TestSecretTool_Definition(t *testing.T) {
	tool := SecretTool()
	assert.Equal(t, "secret", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "secret")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunSecret_DetectsAPIKey(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "config.go", "package main\nconst APIKey = \"sk-1234567890abcdefghij\"\n")

	raw, _ := json.Marshal(secretInput{Path: "."})
	out, err := runSecret(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "API key")
	assert.Contains(t, out.Content, "config.go")
}

func TestRunSecret_DetectsPassword(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "db.go", "package main\nconst password = \"SuperSecret123\"\n")

	raw, _ := json.Marshal(secretInput{Path: "."})
	out, err := runSecret(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Password")
	assert.Contains(t, out.Content, "db.go")
}

func TestRunSecret_DetectsPrivateKey(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "key.pem", "-----BEGIN RSA PRIVATE KEY-----\nMIIBogIBAAJBALRiMLAHudeSA/x3hB2f+2NRkJLA1SomqGU0MpEaM6VDYzLA\n-----END RSA PRIVATE KEY-----\n")

	raw, _ := json.Marshal(secretInput{Path: "."})
	out, err := runSecret(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Private key")
	assert.Contains(t, out.Content, "key.pem")
}

func TestRunSecret_WithGlob(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "main.go", "package main\nconst key = \"sk-1234567890abcdefghij\"\n")
	writeFile(t, root, "notes.txt", "password: secret123\n")

	raw, _ := json.Marshal(secretInput{Path: ".", Glob: "*.go"})
	out, err := runSecret(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "main.go")
	assert.NotContains(t, out.Content, "notes.txt")
}

func TestRunSecret_NoSecrets(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "main.go", "package main\nfunc main() {}\n")

	raw, _ := json.Marshal(secretInput{Path: "."})
	out, err := runSecret(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No secrets detected", out.Content)
}

func TestRunSecret_Limit(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for i := 0; i < 5; i++ {
		writeFile(t, root, sprintf("file%d.go", i), "package main\nconst key = \"sk-1234567890abcdefghij\"\n")
	}

	raw, _ := json.Marshal(secretInput{Path: ".", Limit: 3})
	out, err := runSecret(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "limit reached")
}

func TestRunSecret_NotADirectory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	writeFile(t, root, "file.txt", "password: secret123\n")

	raw, _ := json.Marshal(secretInput{Path: "file.txt"})
	out, err := runSecret(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Password")
	assert.Contains(t, out.Content, "file.txt")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func sprintf(format string, args ...interface{}) string {
	return strings.TrimSpace(fmt.Sprintf(format, args...))
}
