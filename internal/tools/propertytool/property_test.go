package propertytool

import (
        "encoding/json"
        "fmt"
        "os"
        "os/exec"
        "path/filepath"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

func TestPropertyTool_Definition(t *testing.T) {
        tool := PropertyTool()
        assert.Equal(t, "property", tool.Definition.Name)
        assert.Contains(t, tool.Definition.Description, "property")
        assert.True(t, tool.Definition.Readable)
        assert.NotNil(t, tool.Run)
}

func TestRunProperty_NoFunctions(t *testing.T) {
        root := t.TempDir()
        writeFile(t, root, "main.go", `package main

func init() {}
`)

        raw, _ := json.Marshal(propertyInput{Path: root, Glob: "*.go", Limit: 10})
        out, err := runProperty(t.Context(), raw)
        require.NoError(t, err)
        assert.Equal(t, "No exported functions found for property testing", out.Content)
}

func TestRunProperty_FindsFunctions(t *testing.T) {
        root := t.TempDir()
        writeFile(t, root, "main.go", `package main

func Add(a, b int) int {
        return a + b
}
`)

        raw, _ := json.Marshal(propertyInput{Path: root, Glob: "*.go", Limit: 10})
        out, err := runProperty(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "Add")
        assert.Contains(t, out.Content, "quick.Check")
}

// TestRunProperty_SkeletonCompiles is the regression test for #5: the
// generated skeleton must compile when pasted into a _test.go file. The old
// skeleton emitted `assert := assert.New(t)` with no testify import and a
// function-level skeleton with no package or imports, so a model pasting it
// into an empty _test.go file hit "declared and not used" + "undefined:
// assert" + "undefined: quick".
//
// This test writes the generated skeleton to a file on disk, runs `go build`
// against it (via `go vet`, which type-checks without producing binaries), and
// requires the file to type-check cleanly.
func TestRunProperty_SkeletonCompiles(t *testing.T) {
        root := t.TempDir()
        writeFile(t, root, "main.go", `package calc

func Add(a, b int) int {
        return a + b
}
`)
        writeFile(t, root, "go.mod", "module example.com/calc\n\ngo 1.26\n")

        raw, _ := json.Marshal(propertyInput{Path: root, Glob: "*.go", Limit: 10})
        out, err := runProperty(t.Context(), raw)
        require.NoError(t, err)
        require.NotEmpty(t, out.Content)

        // The skeleton contains the @file header and "func:" lines; strip those
        // before writing, otherwise the .go file won't parse. The skeleton
        // itself starts after the first blank line following the signature.
        skeleton := extractSkeleton(out.Content)
        require.NotEmpty(t, skeleton, "skeleton should be non-empty")

        skelPath := filepath.Join(root, "main_property_test.go")
        require.NoError(t, os.WriteFile(skelPath, []byte(skeleton), 0o644))

        // `go vet` type-checks the package; a clean exit means the skeleton
        // compiles. We use vet instead of build because there's no main package.
        cmd := exec.CommandContext(t.Context(), "go", "vet", "./...")
        cmd.Dir = root
        cmd.Env = append(os.Environ(), "GO111MODULE=on")
        out2, err := cmd.CombinedOutput()
        if err != nil {
                t.Fatalf("skeleton did not compile: %s\n%s", err, string(out2))
        }
}

// TestRunProperty_SkeletonHasNoUnusedAssert verifies the skeleton dropped the
// unused `assert := assert.New(t)` line — that caused "declared and not used"
// when pasted into a file without testify.
func TestRunProperty_SkeletonHasNoUnusedAssert(t *testing.T) {
        root := t.TempDir()
        writeFile(t, root, "main.go", `package main

func Add(a, b int) int {
        return a + b
}
`)

        raw, _ := json.Marshal(propertyInput{Path: root, Glob: "*.go", Limit: 10})
        out, err := runProperty(t.Context(), raw)
        require.NoError(t, err)
        assert.NotContains(t, out.Content, "assert.New",
                "skeleton must not emit assert.New — testify import was missing and the value was unused")
        assert.NotContains(t, out.Content, "stretchr/testify",
                "skeleton must not import testify when it doesn't use it")
        assert.Contains(t, out.Content, "package main",
                "skeleton must include the package clause so it compiles standalone")
        assert.Contains(t, out.Content, "\"testing/quick\"",
                "skeleton must import testing/quick so quick.Check resolves")
}

// extractSkeleton peels off the @file/func/signature header lines that the
// renderer prepends and returns just the Go source of the test skeleton.
func extractSkeleton(content string) string {
        // The renderer writes `@file <path>\nfunc: <name>\nsignature: <sig>\n\n`
        // and then the skeleton. Find the first occurrence of `package ` or
        // `func Test` and slice from there.
        idx := strings.Index(content, "package ")
        if idx < 0 {
                idx = strings.Index(content, "func Test")
        }
        if idx < 0 {
                return ""
        }
        return content[idx:]
}

func TestRunProperty_Limit(t *testing.T) {
        root := t.TempDir()
        for i := 0; i < 5; i++ {
                writeFile(t, root, fmt.Sprintf("file%d.go", i), fmt.Sprintf(`package main

func Func%d(a int) int {
        return a
}
`, i))
        }

        raw, _ := json.Marshal(propertyInput{Path: root, Glob: "*.go", Limit: 3})
        out, err := runProperty(t.Context(), raw)
        require.NoError(t, err)
        // Count suggestions by @file markers.
        count := strings.Count(out.Content, "@file")
        assert.Equal(t, 3, count, "should respect limit")
}

func TestRunProperty_EmptyPath(t *testing.T) {
        // Empty path defaults to cwd; use a temp dir with no Go files instead.
        root := t.TempDir()
        writeFile(t, root, "readme.txt", "hello")

        raw, _ := json.Marshal(propertyInput{Path: root, Glob: "*.go", Limit: 10})
        out, err := runProperty(t.Context(), raw)
        require.NoError(t, err)
        assert.Equal(t, "No exported functions found for property testing", out.Content)
}

func writeFile(t *testing.T, dir, name, content string) {
        t.Helper()
        require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
