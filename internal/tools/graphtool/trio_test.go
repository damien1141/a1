package graphtool

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/project"
)

const fixtureModule = "example.com/tiny"

func writeFixtureFile(t *testing.T, root, rel, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644))
}

// installGraphFixture writes a tiny three-package module (app imports base
// and mid, mid imports base) under a temp dir and points the project
// singleton at it with a temp HOME, so runGraph's GetDefaultProject sees a
// hermetic workspace. Returns the fixture root.
func installGraphFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	writeFixtureFile(t, root, "go.mod", "module "+fixtureModule+"\n\ngo 1.21\n")
	writeFixtureFile(t, root, "base/base.go", "package base\n\nfunc Do() {}\n")
	writeFixtureFile(t, root, "mid/mid.go", "package mid\n\nimport \""+fixtureModule+"/base\"\n\nfunc Do() {}\n")
	writeFixtureFile(t, root, "app/app.go", "package app\n\nimport (\n\t\""+fixtureModule+"/base\"\n\t\""+fixtureModule+"/mid\"\n)\n\nfunc Do() {}\n")
	proj, err := project.Discover(root)
	require.NoError(t, err)
	project.SetDefaultProject(proj)
	t.Cleanup(func() { project.SetDefaultProject(nil) })
	return root
}

// resultPaths extracts the @file lines from rendered graph results.
func resultPaths(t *testing.T, content string) []string {
	t.Helper()
	atFile := regexp.MustCompile(`(?m)^@file (\S+)$`)
	var paths []string
	for _, m := range atFile.FindAllStringSubmatch(content, -1) {
		paths = append(paths, m[1])
	}
	return paths
}

func TestRunGraph_ReportsDependencies_Positive(t *testing.T) {
	installGraphFixture(t)

	t.Run("definition", func(t *testing.T) {
		tool := GraphTool()
		assert.Equal(t, "graph", tool.Definition.Name)
		assert.Equal(t, []string{"query"}, tool.Definition.Params.Required)
		assert.True(t, tool.Definition.Readable)
		assert.NotNil(t, tool.Run)
	})

	t.Run("imports direction resolves file paths", func(t *testing.T) {
		raw, err := json.Marshal(graphInput{Query: "deps of mid", Path: "mid/mid.go", Direction: "imports"})
		require.NoError(t, err)
		out, err := runGraph(t.Context(), raw)
		require.NoError(t, err)
		assert.Contains(t, out.Content, "Dependency graph for mid/mid.go (imports)")
		assert.Equal(t, []string{"base/base.go"}, resultPaths(t, out.Content))
		assert.Equal(t, "1 graph results for mid/mid.go (imports)", out.Detail)
	})

	t.Run("imported_by with module path", func(t *testing.T) {
		raw, err := json.Marshal(graphInput{
			Query: "who imports base", Path: fixtureModule + "/base", Direction: "imported_by",
		})
		require.NoError(t, err)
		out, err := runGraph(t.Context(), raw)
		require.NoError(t, err)
		assert.Equal(t, []string{"app/app.go", "mid/mid.go"}, resultPaths(t, out.Content))
		assert.Equal(t, "2 graph results for "+fixtureModule+"/base (imported_by)", out.Detail)
	})

	t.Run("limit truncates results", func(t *testing.T) {
		raw, err := json.Marshal(graphInput{
			Query: "who imports base", Path: fixtureModule + "/base", Direction: "imported_by", Limit: 1,
		})
		require.NoError(t, err)
		out, err := runGraph(t.Context(), raw)
		require.NoError(t, err)
		assert.Equal(t, []string{"app/app.go"}, resultPaths(t, out.Content))
		assert.Equal(t, "1 graph results for "+fixtureModule+"/base (imported_by)", out.Detail)
	})

	t.Run("empty query falls back to overview", func(t *testing.T) {
		raw, err := json.Marshal(graphInput{Query: ""})
		require.NoError(t, err)
		out, err := runGraph(t.Context(), raw)
		require.NoError(t, err)
		assert.Contains(t, out.Content, "Indexed files: 3")
		// The overview counts importers through the same reverse-key
		// expansion the direction queries use, so module-keyed reverse
		// edges surface here too: base is imported by app and mid.
		assert.Contains(t, out.Content, "Top imported files")
		assert.Contains(t, out.Content, "base/base.go (2 importers)")
		assert.Equal(t, "3 files indexed", out.Detail)
	})
}

func TestRunGraph_HostileInput_Negative(t *testing.T) {
	installGraphFixture(t)

	t.Run("malformed JSON", func(t *testing.T) {
		out, err := runGraph(t.Context(), json.RawMessage(`{"query": `))
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to parse graph arguments")
		assert.Empty(t, out.Content)
	})

	t.Run("wrong query type", func(t *testing.T) {
		out, err := runGraph(t.Context(), json.RawMessage(`{"query": 42}`))
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to parse graph arguments")
		assert.Empty(t, out.Content)
	})

	t.Run("unknown target reports no edges", func(t *testing.T) {
		raw, err := json.Marshal(graphInput{Query: "deps", Path: "no/such/file.go"})
		require.NoError(t, err)
		out, err := runGraph(t.Context(), raw)
		require.NoError(t, err, "a target with no edges is a graceful empty result, not an error")
		assert.Equal(t, "No dependency edges found for no/such/file.go", out.Content)
		assert.Equal(t, "0 results", out.Detail)
	})
}

// An unusable HOME makes project discovery fail; the tool must answer with
// its own "project config not loaded" error instead of the process dying
// inside project.GetDefaultProject's panic — the pre-TryDefaultProject
// behavior this test pins as gone.
func TestRunGraph_UnusableHome_Negative(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(home, []byte("x"), 0o644))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project.SetDefaultProject(nil)
	t.Cleanup(func() { project.SetDefaultProject(nil) })

	raw, err := json.Marshal(graphInput{Query: "who imports base"})
	require.NoError(t, err)
	var runErr error
	require.NotPanics(t, func() {
		_, runErr = runGraph(t.Context(), raw)
	})
	require.Error(t, runErr)
	assert.Contains(t, runErr.Error(), "project config not loaded")
}

// oracle: regexp scan of the fixture .go import blocks — expected import and
// importer sets derived without go/parser or internal/graph, then compared
// against the tool's rendered results as sets.
func TestRunGraph_EdgesMatchRegexpOracle_Differential(t *testing.T) {
	root := installGraphFixture(t)

	// Each quoted path on its own line is an import — both block entries
	// (tab-indented) and single-line `import "x"` declarations. The fixture
	// bodies carry no other quoted strings.
	quoted := regexp.MustCompile(`(?m)^\s*(?:import\s+)?"([^"]+)"`)
	files := []string{"app/app.go", "base/base.go", "mid/mid.go"}
	imports := make(map[string]map[string]struct{}, len(files))
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(root, rel))
		require.NoError(t, err)
		set := map[string]struct{}{}
		for _, m := range quoted.FindAllStringSubmatch(string(b), -1) {
			set[m[1]] = struct{}{}
		}
		imports[rel] = set
	}

	// Hand mapping: module path -> fixture file (module prefix strip plus the
	// dir/file naming), not graph.NewPathResolver.
	toFile := func(mod string) string {
		pkg := strings.TrimPrefix(mod, fixtureModule+"/")
		if pkg == mod {
			return ""
		}
		return pkg + "/" + pkg + ".go"
	}

	// Forward edges: the tool's "imports" per file vs the oracle.
	for _, rel := range files {
		raw, err := json.Marshal(graphInput{Query: "deps", Path: rel, Direction: "imports"})
		require.NoError(t, err)
		out, err := runGraph(t.Context(), raw)
		require.NoError(t, err)
		want := map[string]struct{}{}
		for mod := range imports[rel] {
			if f := toFile(mod); f != "" {
				want[f] = struct{}{}
			}
		}
		got := map[string]struct{}{}
		for _, p := range resultPaths(t, out.Content) {
			got[p] = struct{}{}
		}
		assert.Equal(t, want, got, "imports of %s", rel)
	}

	// Reverse edges: the tool's "imported_by" per module path vs the oracle.
	mods := map[string]struct{}{}
	for _, set := range imports {
		for mod := range set {
			mods[mod] = struct{}{}
		}
	}
	require.NotEmpty(t, mods)
	for mod := range mods {
		raw, err := json.Marshal(graphInput{Query: "importers", Path: mod, Direction: "imported_by"})
		require.NoError(t, err)
		out, err := runGraph(t.Context(), raw)
		require.NoError(t, err)
		want := map[string]struct{}{}
		for _, rel := range files {
			if _, ok := imports[rel][mod]; ok {
				want[rel] = struct{}{}
			}
		}
		got := map[string]struct{}{}
		for _, p := range resultPaths(t, out.Content) {
			got[p] = struct{}{}
		}
		assert.Equal(t, want, got, "importers of %s", mod)
	}

	// The never-imported package: its file-path and directory spellings must
	// stay empty — the candidate expansion may not invent importers.
	for _, alias := range []string{"app/app.go", "app"} {
		raw, err := json.Marshal(graphInput{Query: "importers", Path: alias, Direction: "imported_by"})
		require.NoError(t, err)
		out, err := runGraph(t.Context(), raw)
		require.NoError(t, err)
		got := map[string]struct{}{}
		for _, p := range resultPaths(t, out.Content) {
			got[p] = struct{}{}
		}
		assert.Empty(t, got, "no importers may surface for %s", alias)
	}

	// Reverse edges via file-path spellings: the tool's own parameter docs
	// steer users at file paths ("internal/auth/auth.go"), so each module's
	// file path and its bare package directory must return the same importer
	// set as the module-path query above — the reverse index is keyed by raw
	// import strings, so these spellings only resolve through the tool's
	// candidate expansion.
	for mod := range mods {
		file := toFile(mod)
		require.NotEmpty(t, file, "fixture module %s must map to a fixture file", mod)
		want := map[string]struct{}{}
		for _, rel := range files {
			if _, ok := imports[rel][mod]; ok {
				want[rel] = struct{}{}
			}
		}
		for _, alias := range []string{file, path.Dir(file)} {
			raw, err := json.Marshal(graphInput{Query: "importers", Path: alias, Direction: "imported_by"})
			require.NoError(t, err)
			out, err := runGraph(t.Context(), raw)
			require.NoError(t, err)
			got := map[string]struct{}{}
			for _, p := range resultPaths(t, out.Content) {
				got[p] = struct{}{}
			}
			assert.Equal(t, want, got, "importers of %s via file-path alias %s", mod, alias)
		}
	}
}
