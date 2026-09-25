package graph

import (
	"os"
	"path/filepath"
	"strings"
)

// PathResolver converts module-style import paths to workspace-relative paths
// for matching against the graph. It handles common conventions but is not
// exhaustive.
type PathResolver struct {
	root string
}

// NewPathResolver creates a resolver for the given workspace root.
func NewPathResolver(root string) *PathResolver {
	return &PathResolver{root: filepath.Clean(root)}
}

// Resolve attempts to map a module path to a relative file path.
func (r *PathResolver) Resolve(mod string) string {
	if r == nil || mod == "" {
		return ""
	}
	mod = strings.TrimPrefix(mod, "./")
	mod = strings.TrimPrefix(mod, "../")
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
