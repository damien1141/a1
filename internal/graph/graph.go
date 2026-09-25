package graph

import "sync"

// Graph is a directed dependency graph keyed by relative file path.
type Graph struct {
	mu       sync.RWMutex
	edges    map[string][]string // src -> imports
	reverse  map[string][]string // dst -> imported by
	root     string
}

// Imports returns the direct dependencies of path.
func (g *Graph) Imports(path string) []string {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return append([]string(nil), g.edges[path]...)
}

// ImportedBy returns files that directly import path.
func (g *Graph) ImportedBy(path string) []string {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return append([]string(nil), g.reverse[path]...)
}

// Len reports the number of indexed files.
func (g *Graph) Len() int {
	if g == nil {
		return 0
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.edges)
}

// Root returns the scanned workspace root.
func (g *Graph) Root() string {
	if g == nil {
		return ""
	}
	return g.root
}

// AllFiles returns every indexed source path.
func (g *Graph) AllFiles() []string {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]string, 0, len(g.edges))
	for p := range g.edges {
		out = append(out, p)
	}
	return out
}

// FormatImport returns a human-readable representation of an import edge.
func FormatImport(src, dst string) string {
	return src + " -> " + dst
}
