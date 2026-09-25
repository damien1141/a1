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

// TopologicalSort returns the given files in dependency order: if A imports B,
// B appears before A. Files with no dependency information are appended in the
// original order after the sorted prefix.
func (g *Graph) TopologicalSort(files []string) []string {
	return g.TopologicalSortWithResolver(files, nil)
}

// TopologicalSortWithResolver returns the given files in dependency order,
// using the provided resolver to map import paths to file paths. If resolver
// is nil, only exact path matches are considered.
func (g *Graph) TopologicalSortWithResolver(files []string, resolver *PathResolver) []string {
	if g == nil || len(files) == 0 {
		return files
	}

	// Build a lookup for the requested subset and compute out-degrees within
	// the subset. Out-degree = number of files this file imports that are also
	// in the subset.
	subset := make(map[string]struct{}, len(files))
	for _, f := range files {
		subset[f] = struct{}{}
	}

	outDegree := make(map[string]int, len(files))
	importedBy := make(map[string][]string, len(files)) // reverse edges within subset

	for _, f := range files {
		outDegree[f] = 0
	}

	g.mu.RLock()
	for _, src := range files {
		dsts, ok := g.edges[src]
		if !ok {
			continue
		}
		for _, dst := range dsts {
			// Try to resolve the import path to a file path in the workspace.
			var dstFile string
			if resolver != nil {
				dstFile = resolver.Resolve(dst)
			}
			if dstFile == "" {
				// Fallback: if the import path already looks like a file path in
				// the subset, use it directly.
				if _, ok := subset[dst]; ok {
					dstFile = dst
				}
			}
			if _, ok := subset[dstFile]; !ok {
				continue
			}
			outDegree[src]++
			importedBy[dstFile] = append(importedBy[dstFile], src)
		}
	}
	g.mu.RUnlock()

	// Kahn's algorithm on the reversed edge direction: start with nodes that
	// have out-degree 0 within the subset (they don't import any other subset
	// member), which means they are the deepest dependencies.
	var queue []string
	for _, f := range files {
		if outDegree[f] == 0 {
			queue = append(queue, f)
		}
	}

	sorted := make([]string, 0, len(files))
	seen := make(map[string]struct{}, len(files))

	for len(queue) > 0 {
		// Pop from queue.
		var node string
		node, queue = queue[len(queue)-1], queue[:len(queue)-1]
		if _, ok := seen[node]; ok {
			continue
		}
		seen[node] = struct{}{}
		sorted = append(sorted, node)

		// Decrement out-degree for all files that import this node.
		for _, src := range importedBy[node] {
			outDegree[src]--
			if outDegree[src] == 0 {
				queue = append(queue, src)
			}
		}
	}

	// Append any files that were not reachable in the dependency order
	// (e.g., disconnected or cyclic), preserving original relative order.
	if len(sorted) < len(files) {
		order := make(map[string]int, len(files))
		for i, f := range files {
			order[f] = i
		}
		remaining := make([]string, 0, len(files)-len(sorted))
		for _, f := range files {
			if _, ok := seen[f]; !ok {
				remaining = append(remaining, f)
			}
		}
		sorted = append(sorted, remaining...)
	}

	return sorted
}
