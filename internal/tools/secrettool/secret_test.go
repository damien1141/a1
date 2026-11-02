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
        writeFile(
                t,
                root,
                "key.pem",
                "-----BEGIN RSA PRIVATE KEY-----\nMIIBogIBAAJBALRiMLAHudeSA/x3hB2f+2NRkJLA1SomqGU0MpEaM6VDYzLA\n-----END RSA PRIVATE KEY-----\n",
        )

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

// TestRunSecret_NoDoubleCount is the regression test for #6: a single line
// that matches both an API key pattern and Generic secret used to produce
// two separate entries — the headline number was inflated.
func TestRunSecret_NoDoubleCount(t *testing.T) {
        root := t.TempDir()
        t.Chdir(root)
        // The line below matches both the API key pattern (api_key = ...)
// and Generic secret (key = ...) at the same position.
        writeFile(t, root, "config.go", "package main\nconst apiKey = \"sk-1234567890abcdefghij\"\n")

        raw, _ := json.Marshal(secretInput{Path: "."})
        out, err := runSecret(t.Context(), raw)
        require.NoError(t, err)

        // The line is matched once. Count the number of `>>NNNN|` match
        // lines — each match has exactly one of these.
        matchCount := strings.Count(out.Content, ">>")
        assert.Equal(t, 1, matchCount,
                "a line matching multiple patterns must be reported once, got: %s", out.Content)

        // And the specific pattern (API key) must win over Generic secret
        // because the pattern list is ordered most-specific-first.
        assert.Contains(t, out.Content, "[API key]")
        assert.NotContains(t, out.Content, "[Generic secret]")
}

// TestRunSecret_HexHashNotGenericSecret verifies the Generic secret regex no
// longer fires on pure-hex values of common hash lengths (MD5/SHA-1/
// SHA-256/SHA-512). Those are almost always checksums, not secrets.
func TestRunSecret_HexHashNotGenericSecret(t *testing.T) {
        cases := []struct {
                name   string
                hash   string
                length int
        }{
                {"md5", "0123456789abcdef0123456789abcdef", 32},
                {"sha1", "0123456789abcdef0123456789abcdef01234567", 40},
                {"sha256", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", 64},
        }
        for _, tc := range cases {
                t.Run(tc.name, func(t *testing.T) {
                        root := t.TempDir()
                        t.Chdir(root)
                        // A line like `key: <hex>` used to match Generic secret.
                        writeFile(t, root, "lockfile.txt", fmt.Sprintf("key: %s\n", tc.hash))

                        raw, _ := json.Marshal(secretInput{Path: "."})
                        out, err := runSecret(t.Context(), raw)
                        require.NoError(t, err)
                        assert.Equal(t, "No secrets detected", out.Content,
                                "pure-hex hash of length %d must not match Generic secret", tc.length)
                })
        }
}

// TestRunSecret_PasswordKeywordRejected verifies the Password pattern drops
// matches when the captured value is just the bare keyword — `password =
// "password"` is not a leaked secret, it's a placeholder.
func TestRunSecret_PasswordKeywordRejected(t *testing.T) {
        root := t.TempDir()
        t.Chdir(root)
        writeFile(t, root, "config.txt", "password: password\n")

        raw, _ := json.Marshal(secretInput{Path: "."})
        out, err := runSecret(t.Context(), raw)
        require.NoError(t, err)
        assert.Equal(t, "No secrets detected", out.Content,
                "placeholder value `password` must not match the Password pattern: %s", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
        t.Helper()
        require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func sprintf(format string, args ...interface{}) string {
        return strings.TrimSpace(fmt.Sprintf(format, args...))
}
