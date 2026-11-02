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

// === mode dispatch (Tier A.4) ===

func TestDocTool_SchemaHasModeEnum(t *testing.T) {
        tool := DocTool()
        modeParam, ok := tool.Definition.Params.Properties["mode"].(map[string]any)
        require.True(t, ok, "mode param should be present")
        enum, _ := modeParam["enum"].([]string)
        assert.Equal(t, []string{"sync", "api"}, enum)
}

func TestRunDoc_DefaultModeIsSync(t *testing.T) {
        // No mode field — defaults to "sync" (backward compat). The sync
        // mode returns "No documentation drift detected" for a fresh dir.
        dir := t.TempDir()
        writeFile(t, dir, "x.go", "package x\n")

        tool := DocTool()
        b, _ := json.Marshal(map[string]any{"path": dir, "glob": "*.go"})
        out, err := tool.Run(t.Context(), b)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "No documentation drift")
}

func TestRunDoc_ModeSyncExplicit(t *testing.T) {
        dir := t.TempDir()
        writeFile(t, dir, "x.go", "package x\n")

        tool := DocTool()
        b, _ := json.Marshal(map[string]any{"mode": "sync", "path": dir, "glob": "*.go"})
        out, err := tool.Run(t.Context(), b)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "No documentation drift")
}

func TestRunDoc_ModeApiDispatchesToApidoc(t *testing.T) {
        // mode=api should route through apidoc.ApidocTool().Run — the
        // output should look like API doc stubs, not the "No documentation
        // drift detected" sync message.
        dir := t.TempDir()
        writeFile(t, dir, "x.go", "package x\n// Foo is a thing.\nfunc Foo() {}\n")

        tool := DocTool()
        b, _ := json.Marshal(map[string]any{"mode": "api", "path": dir, "glob": "*.go"})
        out, err := tool.Run(t.Context(), b)
        require.NoError(t, err)
        // apidoc output should mention Foo — sync would say "No drift".
        assert.Contains(t, out.Content, "Foo")
}

func TestRunDoc_UnknownModeErrors(t *testing.T) {
        tool := DocTool()
        b, _ := json.Marshal(map[string]any{"mode": "readme", "path": "."})
        _, err := tool.Run(t.Context(), b)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "unknown doc mode")
        assert.Contains(t, err.Error(), "readme")
}
