package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenGraphGoImports(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nimport \"fmt\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\nimport \"os\"\n"), 0o644))

	g, err := OpenGraph(context.Background(), dir)
	require.NoError(t, err)
	require.NotNil(t, g)
	assert.Equal(t, 2, g.Len())
	assert.Equal(t, []string{"fmt"}, g.Imports("a.go"))
	assert.Equal(t, []string{"os"}, g.Imports("b.go"))
}

func TestOpenGraphSkipsVendor(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "vendor"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vendor", "v.go"), []byte("package v\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))

	g, err := OpenGraph(context.Background(), dir)
	require.NoError(t, err)
	assert.Equal(t, 1, g.Len())
}

func TestImportedBy(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nimport \"a\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644))

	g, err := OpenGraph(context.Background(), dir)
	require.NoError(t, err)
	// Reverse lookup uses the raw import path as stored in the graph.
	assert.Equal(t, []string{"main.go"}, g.ImportedBy("a"))
}

func TestPathResolver(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "auth"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "auth", "auth.go"), []byte("package auth\n"), 0o644))

	r := NewPathResolver(dir)
	assert.Equal(t, filepath.Join("internal", "auth", "auth.go"), r.Resolve("internal/auth"))
	assert.Equal(t, filepath.Join("internal", "auth", "auth.go"), r.Resolve("./internal/auth"))
	assert.Equal(t, "", r.Resolve("does-not-exist"))
}

func TestGraphNilSafe(t *testing.T) {
	var g *Graph
	assert.Empty(t, g.Imports("x.go"))
	assert.Empty(t, g.ImportedBy("x.go"))
	assert.Empty(t, g.AllFiles())
	assert.Equal(t, 0, g.Len())
	assert.Equal(t, "", g.Root())
}

func TestTopologicalSortBasic(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nimport \"b\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "c.go"), []byte("package c\nimport \"a\"\n"), 0o644))

	g, err := OpenGraph(context.Background(), dir)
	require.NoError(t, err)

	resolver := NewPathResolver(dir)
	order := g.TopologicalSortWithResolver([]string{"a.go", "b.go", "c.go"}, resolver)
	require.Len(t, order, 3)
	// b.go should come before a.go because a.go imports b.go
	bIdx := indexOf(order, "b.go")
	aIdx := indexOf(order, "a.go")
	assert.True(t, bIdx < aIdx, "b.go should come before a.go, got %v", order)
	// a.go should come before c.go because c.go imports a.go
	assert.True(t, aIdx < indexOf(order, "c.go"), "a.go should come before c.go, got %v", order)
}

func TestTopologicalSortDisconnected(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n"), 0o644))

	g, err := OpenGraph(context.Background(), dir)
	require.NoError(t, err)

	order := g.TopologicalSort([]string{"b.go", "a.go"})
	// Disconnected files should preserve original relative order after sorted prefix
	assert.Len(t, order, 2)
}

func TestTopologicalSortEmpty(t *testing.T) {
	var g *Graph
	assert.Empty(t, g.TopologicalSort(nil))
	assert.Empty(t, g.TopologicalSort([]string{}))
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
