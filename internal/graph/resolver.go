package graph

import (
        "bufio"
        "os"
        "path/filepath"
        "strings"
)

// PathResolver converts module-style import paths to workspace-relative paths
// for matching against the graph. It handles common conventions but is not
// exhaustive.
type PathResolver struct {
        root       string
        modulePath string // best-effort go.mod module path; "" if absent
}

// NewPathResolver creates a resolver for the given workspace root. If a
// go.mod lives at root, its module path is read so Go imports of the form
// "<module>/internal/foo" can be resolved to "internal/foo" before the file
// lookup. Absence of go.mod is fine — the resolver just behaves like the
// pre-module-path implementation for non-Go imports.
func NewPathResolver(root string) *PathResolver {
        r := &PathResolver{root: filepath.Clean(root)}
        r.modulePath = readGoModModule(r.root)
        return r
}

// ModulePath returns the go.mod module path, if any. Exposed for tests.
func (r *PathResolver) ModulePath() string {
        if r == nil {
                return ""
        }
        return r.modulePath
}

// readGoModModule reads the `module <path>` line from root/go.mod. Returns ""
// if the file is missing or the module line cannot be found. This is
// deliberately minimal — go.mod can have replace/exclude directives we don't
// need here, and pulling in golang.org/x/mod would be a heavier dependency
// than the bug deserves.
func readGoModModule(root string) string {
        f, err := os.Open(filepath.Join(root, "go.mod"))
        if err != nil {
                return ""
        }
        defer f.Close()
        s := bufio.NewScanner(f)
        for s.Scan() {
                line := strings.TrimSpace(s.Text())
                if strings.HasPrefix(line, "module ") {
                        mod := strings.TrimSpace(strings.TrimPrefix(line, "module "))
                        // Strip inline comments like `module foo // v2`.
                        if i := strings.Index(mod, "//"); i >= 0 {
                                mod = strings.TrimSpace(mod[:i])
                        }
                        return mod
                }
        }
        return ""
}

// Resolve attempts to map a module path to a relative file path.
func (r *PathResolver) Resolve(mod string) string {
        if r == nil || mod == "" {
                return ""
        }
        mod = strings.TrimPrefix(mod, "./")
        mod = strings.TrimPrefix(mod, "../")
        // Strip the go.mod module prefix so e.g. "github.com/damien1141/a1/internal/graph"
        // resolves to "internal/graph". Without this, every Go import that uses the
        // module path fails to resolve and TopologicalSort degenerates to
        // reverse-input order.
        if r.modulePath != "" {
                if mod == r.modulePath {
                        mod = "."
                } else if strings.HasPrefix(mod, r.modulePath+"/") {
                        mod = strings.TrimPrefix(mod, r.modulePath+"/")
                }
        }
        candidate := filepath.Join(r.root, filepath.FromSlash(mod))
        // Direct file match first.
        exts := []string{"", ".go", ".py", ".rs", ".ts", ".js", ".mjs", ".cjs"}
        for _, ext := range exts {
                p := candidate + ext
                if info, err := os.Stat(p); err == nil && !info.IsDir() {
                        rel := strings.TrimPrefix(p, r.root)
                        rel = strings.TrimPrefix(rel, string(filepath.Separator))
                        return rel
                }
        }
        // Directory match: probe common package entry files, then any file
        // matching the directory base name.
        if info, err := os.Stat(candidate); err == nil && info.IsDir() {
                entries := []string{"main.go", "lib.rs", "index.ts", "index.js", "__init__.py"}
                base := filepath.Base(candidate)
                entries = append(entries, base+".go", base+".rs", base+".py", base+".ts", base+".js")
                for _, entry := range entries {
                        p := filepath.Join(candidate, entry)
                        if info, err := os.Stat(p); err == nil && !info.IsDir() {
                                rel := strings.TrimPrefix(p, r.root)
                                rel = strings.TrimPrefix(rel, string(filepath.Separator))
                                return rel
                        }
                }
        }
        return ""
}
