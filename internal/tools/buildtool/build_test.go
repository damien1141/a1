package buildtool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTool_Definition(t *testing.T) {
	tool := BuildTool()
	assert.Equal(t, "build", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "build")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunBuild_Makefile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Makefile", `all: clean build test

build:
	cargo build

test:
	cargo test

clean:
	cargo clean
`)

	raw, _ := json.Marshal(buildInput{Path: root, Limit: 10})
	out, err := runBuild(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "all")
	assert.Contains(t, out.Content, "build")
	assert.Contains(t, out.Content, "test")
}

func TestRunBuild_CMake(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "CMakeLists.txt", `cmake_minimum_required(VERSION 3.20)
project(myapp CXX)
add_executable(myapp main.cpp)
add_library(util util.cpp)
`)

	raw, _ := json.Marshal(buildInput{Path: root, Limit: 10})
	out, err := runBuild(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "myapp")
	assert.Contains(t, out.Content, "cmake")
}

func TestRunBuild_Cargo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Cargo.toml", `[package]
name = "myapp"
version = "0.1.0"
edition = "2021"

[[bin]]
name = "myapp"
path = "src/main.rs"
`)

	raw, _ := json.Marshal(buildInput{Path: root, Limit: 10})
	out, err := runBuild(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "cargo")
}

func TestRunBuild_Npm(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "package.json", `{
	"name": "myapp",
	"scripts": {
		"build": "tsc",
		"test": "vitest"
	}
}`)

	raw, _ := json.Marshal(buildInput{Path: root, Limit: 10})
	out, err := runBuild(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "build")
	assert.Contains(t, out.Content, "test")
}

func TestRunBuild_NoManifest(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "readme.txt", "hello")

	raw, _ := json.Marshal(buildInput{Path: root, Limit: 10})
	out, err := runBuild(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "No build targets found", out.Content)
}

func TestRunBuild_Limit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "Makefile", `all: clean build test

clean:
	cargo clean

build:
	cargo build

test:
	cargo test
`)

	raw, _ := json.Marshal(buildInput{Path: root, Limit: 2})
	out, err := runBuild(t.Context(), raw)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(out.Content), "\n")
	assert.Len(t, lines, 2, "should respect limit")
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}
