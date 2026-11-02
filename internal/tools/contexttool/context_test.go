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

// === mode dispatch (Tier A.5) ===

func TestContextTool_SchemaHasModeEnum(t *testing.T) {
        tool := ContextTool()
        modeParam, ok := tool.Definition.Params.Properties["mode"].(map[string]any)
        require.True(t, ok, "mode param should be present")
        enum, _ := modeParam["enum"].([]string)
        assert.Equal(t, []string{"fold", "journal", "batch", "snapshot", "config"}, enum)
}

func TestRunContext_DefaultModeIsFold(t *testing.T) {
        // No mode field — defaults to "fold" (backward compat). The fold
        // mode returns "No context issues detected" for a fresh dir.
        dir := t.TempDir()
        writeFile(t, dir, "x.go", "package x\n")

        tool := ContextTool()
        b, _ := json.Marshal(map[string]any{"path": dir, "limit": 10})
        out, err := tool.Run(t.Context(), b)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "No context issues")
}

func TestRunContext_ModeFoldExplicit(t *testing.T) {
        dir := t.TempDir()
        writeFile(t, dir, "x.go", "package x\n")

        tool := ContextTool()
        b, _ := json.Marshal(map[string]any{"mode": "fold", "path": dir, "limit": 10})
        out, err := tool.Run(t.Context(), b)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "No context issues")
}

func TestRunContext_ModeJournalDispatchesToJournal(t *testing.T) {
        // mode=journal should route through journaltool.JournalTool().Run.
        // Read mode (no action) returns "No journal entries found." for a
        // fresh dir.
        dir := t.TempDir()

        tool := ContextTool()
        b, _ := json.Marshal(map[string]any{"mode": "journal", "path": dir})
        out, err := tool.Run(t.Context(), b)
        require.NoError(t, err)
        // journal read returns "No journal entries found." — fold would say
        // "No context issues".
        assert.Contains(t, out.Content, "journal")
}

func TestRunContext_ModeConfigDispatches(t *testing.T) {
        // mode=config should route through configvalidatortool. It may
        // return errors about the config (which is fine — we just need to
        // confirm the dispatch happened, not that the config is valid).
        tool := ContextTool()
        b, _ := json.Marshal(map[string]any{"mode": "config"})
        _, err := tool.Run(t.Context(), b)
        // config validator may succeed or error depending on env, but it
        // shouldn't error with "unknown context mode".
        if err != nil {
                assert.NotContains(t, err.Error(), "unknown context mode")
        }
}

func TestRunContext_UnknownModeErrors(t *testing.T) {
        tool := ContextTool()
        b, _ := json.Marshal(map[string]any{"mode": "audit", "path": "."})
        _, err := tool.Run(t.Context(), b)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "unknown context mode")
        assert.Contains(t, err.Error(), "audit")
}
