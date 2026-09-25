//go:build !treesitter

package graph

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
)

// OpenGraph scans root and indexes imports for supported file types.
func OpenGraph(ctx context.Context, root string) (*Graph, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	root = filepath.Clean(root)
	g := &Graph{
		edges:   make(map[string][]string),
		reverse: make(map[string][]string),
		root:    root,
	}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == "vendor" || name == ".git" || name == "node_modules" || name == "target" || name == "dist" || name == "build" {
				return fs.SkipDir
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rel := strings.TrimPrefix(path, root)
		rel = strings.TrimPrefix(rel, string(filepath.Separator))
		if rel == "" {
			return nil
		}
		imports, ok := parseImports(path, rel)
		if !ok {
			return nil
		}
		g.mu.Lock()
		g.edges[rel] = imports
		for _, dst := range imports {
			g.reverse[dst] = append(g.reverse[dst], rel)
		}
		g.mu.Unlock()
		return nil
	}); err != nil {
		return nil, err
	}
	return g, nil
}
