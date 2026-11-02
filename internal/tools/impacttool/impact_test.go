package impacttool

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

func TestImpactTool_Definition(t *testing.T) {
	tool := ImpactTool()
	assert.Equal(t, "impact", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "blast radius")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunImpact_NoSymbol(t *testing.T) {
	raw, _ := json.Marshal(impactInput{Symbol: ""})
	out, err := runImpact(t.Context(), raw)
	require.Error(t, err)
	assert.Empty(t, out.Content)
}

func TestRunImpact_NoRefs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func PublicFunc() {}
`)

	raw, _ := json.Marshal(impactInput{Symbol: "NonExistent", Path: root, Glob: "*.go", Limit: 10})
	out, err := runImpact(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No references found for NonExistent", out.Content)
}

func TestRunImpact_FindsRefs(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", `package main

func PublicFunc() {}
`)
	writeFile(t, root, "other.go", `package main

func main() {
	PublicFunc()
}
`)

	raw, _ := json.Marshal(impactInput{Symbol: "PublicFunc", Path: root, Glob: "*.go", Limit: 10})
	out, err := runImpact(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "PublicFunc")
	assert.Contains(t, out.Content, "other.go")
}

func TestRunImpact_Limit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, root, fmt.Sprintf("file%d.go", i), fmt.Sprintf(`package main

func Func%d() {}
`, i))
	}

	raw, _ := json.Marshal(impactInput{Symbol: "Func0", Path: root, Glob: "*.go", Limit: 3})
	out, err := runImpact(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 1, "should respect limit")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
