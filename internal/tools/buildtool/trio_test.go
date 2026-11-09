package buildtool

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// oracle: hand-written golden literals — the full rendered report and detail
// line for each manifest type, computed from the documented row format
// (name, kind, source, joined deps, description, name) rather than from any
// output of the tool under test.
func TestRunBuild_Differential(t *testing.T) {
	t.Run("makefile", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "Makefile", `# build rules for the trio fixture
APP = trio

.PHONY: all
all: build test

build:
	go build ./...

test:
	go test ./...
`)
		raw, err := json.Marshal(buildInput{Path: root, Limit: 10})
		require.NoError(t, err)
		out, err := runBuild(t.Context(), raw)
		require.NoError(t, err)

		assert.Equal(t, ""+
			".PHONY\tmake\tMakefile\tall\tphony\t.PHONY\n"+
			"all\tmake\tMakefile\tbuild, test\t\tall\n"+
			"build\tmake\tMakefile\t\t\tbuild\n"+
			"test\tmake\tMakefile\t\t\ttest\n", out.Content)
		assert.Equal(t, "4 build targets", out.Detail)
		assert.Equal(t, out.Content, out.Output)
	})

	t.Run("go module", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "go.mod", "module example.com/trio\n\ngo 1.26\n")
		raw, err := json.Marshal(buildInput{Path: root, Limit: 10})
		require.NoError(t, err)
		out, err := runBuild(t.Context(), raw)
		require.NoError(t, err)

		assert.Equal(t, "go build\tgo\tgo.mod\t\tmodule build\tgo build\n", out.Content)
		assert.Equal(t, "1 build targets", out.Detail)
	})

	t.Run("cmake", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "CMakeLists.txt", `cmake_minimum_required(VERSION 3.20)
project(trio CXX)
add_executable(trioapp main.cpp)
add_library(util util.cpp)
`)
		raw, err := json.Marshal(buildInput{Path: root, Limit: 10})
		require.NoError(t, err)
		out, err := runBuild(t.Context(), raw)
		require.NoError(t, err)

		assert.Equal(t, ""+
			"trioapp\tcmake\tCMakeLists.txt\tmain.cpp\texecutable\ttrioapp\n"+
			"util\tcmake\tCMakeLists.txt\tutil.cpp\tlibrary\tutil\n", out.Content)
		assert.Equal(t, "2 build targets", out.Detail)
	})

	t.Run("cargo", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "Cargo.toml", `[package]
name = "myapp"
version = "0.1.0"

[[bin]]
name = "cli"
path = "src/main.rs"
`)
		raw, err := json.Marshal(buildInput{Path: root, Limit: 10})
		require.NoError(t, err)
		out, err := runBuild(t.Context(), raw)
		require.NoError(t, err)

		assert.Equal(t, "cli\tcargo\tCargo.toml\t\tcargo target\tcli\n", out.Content)
		assert.Equal(t, "1 build targets", out.Detail)
	})
}
