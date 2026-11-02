package propertytool

import (
        "context"
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "regexp"
        "strings"

        "github.com/damien1141/a1/internal/tools/tooldef"

        "github.com/damien1141/a1/internal/llm"
)

const (
        propertyDefaultLimit = 50
)

var propertyDescription = `Generate property-based test skeletons for Go functions.

Parses exported Go functions and returns testing/quick-based test skeletons
that validate round-trip, commutativity, and idempotency properties.`

// PropertyTool returns the property-based test generator tool definition + handler.
func PropertyTool() tooldef.Tool {
        return tooldef.Tool{
                Definition: llm.ToolDefinition{
                        Name:        "property",
                        Description: propertyDescription,
                        Params: &llm.FunctionParameters{
                                Type: "object",
                                Properties: llm.Object{
                                        "path": llm.Object{
                                                "type":        "string",
                                                "description": "Directory or package path. Example: ./internal/tools",
                                        },
                                        "glob": llm.Object{
                                                "type":        "string",
                                                "description": "File pattern filter. Example: *.go",
                                        },
                                        "limit": llm.Object{
                                                "type": "integer",
                                                "description": fmt.Sprintf(
                                                        "Maximum results to return. Example: 20 (default: %d)",
                                                        propertyDefaultLimit,
                                                ),
                                        },
                                },
                                Required: []string{},
                        },
                        Readable: true,
                },
                DetailFromArgs: func(input json.RawMessage) string {
                        var in propertyInput
                        _ = json.Unmarshal(input, &in)
                        p := strings.TrimSpace(in.Path)
                        if p == "" {
                                p = "."
                        }
                        return fmt.Sprintf("property %s", p)
                },
                Run: runProperty,
        }
}

type propertyInput struct {
        Path  string `json:"path,omitempty"`
        Glob  string `json:"glob,omitempty"`
        Limit int    `json:"limit,omitempty"`
}

type propertySuggestion struct {
        file      string
        funcName  string
        signature string
        testCode  string
}

func runProperty(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
        var in propertyInput
        if err := json.Unmarshal(input, &in); err != nil {
                return tooldef.Result{}, fmt.Errorf("failed to parse property arguments: %w", err)
        }

        searchRel := strings.TrimSpace(in.Path)
        if searchRel == "" {
                searchRel = "."
        }
        searchPath, err := tooldef.ResolveToCwd(ctx, searchRel)
        if err != nil {
                return tooldef.Result{}, err
        }

        if _, err := os.Stat(searchPath); err != nil {
                return tooldef.Result{}, fmt.Errorf("path not found: %s", searchPath)
        }

        glob := strings.TrimSpace(in.Glob)
        if glob == "" {
                glob = "*.go"
        }

        limit := in.Limit
        if limit <= 0 {
                limit = propertyDefaultLimit
        }

        suggestions, err := generatePropertyTests(searchPath, glob, limit)
        if err != nil {
                return tooldef.Result{}, err
        }

        if len(suggestions) == 0 {
                return tooldef.Result{
                        Content: "No exported functions found for property testing",
                        Detail:  "0 functions",
                        Output:  "No exported functions found for property testing",
                }, nil
        }

        content := renderPropertyResults(ctx, suggestions)
        detail := fmt.Sprintf("%d property test skeletons", len(suggestions))
        return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func generatePropertyTests(root, glob string, limit int) ([]propertySuggestion, error) {
        goFiles, err := collectGoFiles(root, glob)
        if err != nil {
                return nil, err
        }

        var suggestions []propertySuggestion
        for _, file := range goFiles {
                fileSuggestions, err := parseFunctionsForProperties(file)
                if err != nil {
                        continue
                }
                suggestions = append(suggestions, fileSuggestions...)
                if len(suggestions) >= limit {
                        break
                }
        }

        if len(suggestions) > limit {
                suggestions = suggestions[:limit]
        }

        return suggestions, nil
}

func collectGoFiles(root, glob string) ([]string, error) {
        var files []string
        err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
                if walkErr != nil {
                        return nil
                }
                if info == nil || info.IsDir() {
                        if info != nil && info.IsDir() {
                                if shouldSkipDir(filepath.Base(path)) {
                                        return filepath.SkipDir
                                }
                        }
                        return nil
                }

                matched, err := filepath.Match(glob, filepath.Base(path))
                if err != nil || !matched {
                        return nil
                }

                if strings.HasSuffix(path, ".go") {
                        files = append(files, path)
                }
                return nil
        })
        return files, err
}

var (
        funcRe     = regexp.MustCompile(`(?m)^func\s+([A-Z][a-zA-Z0-9_]*)\s*\(([^)]*)\)\s*([^\({]*)`)
        packageRe  = regexp.MustCompile(`(?m)^package\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

func parseFunctionsForProperties(path string) ([]propertySuggestion, error) {
        b, err := os.ReadFile(path)
        if err != nil {
                return nil, err
        }
        text := string(b)

        pkg := detectPackageName(text) // "" if not found; caller falls back to "propertytest"

        matches := funcRe.FindAllStringSubmatch(text, -1)
        suggestions := make([]propertySuggestion, 0, len(matches))
        for _, m := range matches {
                funcName := m[1]
                params := m[2]
                returnType := strings.TrimSpace(m[3])

                if strings.Contains(params, "...") {
                        continue
                }

                testCode := generatePropertyTest(pkg, funcName, params, returnType)
                suggestions = append(suggestions, propertySuggestion{
                        file:      path,
                        funcName:  funcName,
                        signature: fmt.Sprintf("func %s(%s) %s", funcName, params, returnType),
                        testCode:  testCode,
                })
        }

        return suggestions, nil
}

// detectPackageName pulls the `package X` clause out of a Go file. Returns ""
// if the file has no package clause. Used to make generated test skeletons
// compile as part of a complete file in the right package.
func detectPackageName(src string) string {
        m := packageRe.FindStringSubmatch(src)
        if m == nil {
                return ""
        }
        return m[1]
}

// generatePropertyTest emits a complete, compiling _test.go skeleton: package
// clause, required imports, and the test function. Pasting the previous
// skeleton (function alone with `assert.New(t)` but no imports) into an empty
// _test.go file failed to build — missing imports and "declared and not used".
// The skeleton now:
//   - omits the unused `assert := assert.New(t)` line (no testify dependency),
//   - imports `testing` and `testing/quick` so the file builds standalone,
//   - includes the package clause so it lives next to the source file.
func generatePropertyTest(pkg, funcName, params, returnType string) string {
        if pkg == "" {
                pkg = "propertytest"
        }
        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("package %s\n\n", pkg))
        sb.WriteString("import (\n")
        sb.WriteString("\t\"testing\"\n")
        sb.WriteString("\t\"testing/quick\"\n")
        sb.WriteString(")\n\n")
        sb.WriteString(fmt.Sprintf("func Test%s_Property(t *testing.T) {\n", funcName))
        // quick.Check requires the function it samples to be of the form
        // func(<inputs>) bool. Inputs are restricted to types quick can
        // generate: bool, int*, uint*, float*, complex*, string, and pointers
        // to those. For unsupported input types quick.Check returns an error at
        // runtime; the test still compiles, and the model can swap in
        // quick.Value-based inputs by hand.
        sb.WriteString(fmt.Sprintf("\tf := func(%s) bool {\n", params))
        sb.WriteString(fmt.Sprintf("\t\t// TODO: implement a property check for %s.\n", funcName))
        sb.WriteString("\t\t// A good starter: round-trip (f(x) inverse-f applied) or idempotency (f(f(x)) == f(x)).\n")
        sb.WriteString("\t\treturn true\n")
        sb.WriteString("\t}\n")
        sb.WriteString("\tif err := quick.Check(f, nil); err != nil {\n")
        sb.WriteString(fmt.Sprintf("\t\tt.Errorf(\"property %s failed: %%v\", err)\n", funcName))
        sb.WriteString("\t}\n")
        sb.WriteString("}\n")

        return sb.String()
}

func renderPropertyResults(ctx context.Context, suggestions []propertySuggestion) string {
        var sb strings.Builder
        for _, s := range suggestions {
                rel := tooldef.RelToCwd(ctx, s.file)
                sb.WriteString(fmt.Sprintf("@file %s\n", rel))
                sb.WriteString(fmt.Sprintf("func: %s\n", s.funcName))
                sb.WriteString(fmt.Sprintf("signature: %s\n\n", s.signature))
                sb.WriteString(s.testCode)
                sb.WriteString("\n")
        }
        return sb.String()
}

func shouldSkipDir(name string) bool {
        switch name {
        case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".tox", "dist", "build":
                return true
        }
        return false
}
