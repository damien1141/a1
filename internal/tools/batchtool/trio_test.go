package batchtool

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/project"
)

const fixtureModule = "example.com/tiny"

// chainFixture is one fixture package: its workspace-relative file and the
// module import paths it declares.
type chainFixture struct {
	rel     string
	imports []string
}

// writeChainModule materializes a valid one-file-per-package Go module under
// root. Files never need to compile — only their import declarations are read.
func writeChainModule(t *testing.T, root string, files []chainFixture) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module "+fixtureModule+"\n\ngo 1.21\n"), 0o644))
	for _, f := range files {
		pkg := filepath.Base(filepath.Dir(f.rel))
		var b strings.Builder
		b.WriteString("package " + pkg + "\n\n")
		switch len(f.imports) {
		case 0:
		case 1:
			b.WriteString("import " + strconv.Quote(f.imports[0]) + "\n\n")
		default:
			b.WriteString("import (\n")
			for _, imp := range f.imports {
				b.WriteString("\t" + strconv.Quote(imp) + "\n")
			}
			b.WriteString(")\n\n")
		}
		b.WriteString("func Do() {}\n")
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, f.rel)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, f.rel), []byte(b.String()), 0o644))
	}
}

// installFixtureProject points the project singleton at a temp-HOME fixture
// module so runBatch's GetDefaultProject sees a hermetic workspace. Cleanup
// restores lazy discovery.
func installFixtureProject(t *testing.T, files []chainFixture) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	writeChainModule(t, root, files)
	proj, err := project.Discover(root)
	require.NoError(t, err)
	project.SetDefaultProject(proj)
	t.Cleanup(func() { project.SetDefaultProject(nil) })
	return root
}

// batchOrder extracts the numbered edit order from rendered batch output.
func batchOrder(t *testing.T, content string) []string {
	t.Helper()
	entry := regexp.MustCompile(`(?m)^\d+\. @file (\S+)$`)
	var order []string
	for _, m := range entry.FindAllStringSubmatch(content, -1) {
		order = append(order, m[1])
	}
	return order
}

// importToFile maps a fixture module import path to its workspace-relative
// file path the way an agent would by hand: strip the module prefix, append
// the dir/file naming. Deliberately not graph.NewPathResolver.
func importToFile(mod string) string {
	pkg := strings.TrimPrefix(mod, fixtureModule+"/")
	if pkg == mod {
		return ""
	}
	return pkg + "/" + pkg + ".go"
}

func TestRunBatch_OrdersDependenciesFirst_Positive(t *testing.T) {
	installFixtureProject(t, []chainFixture{
		{rel: "base/base.go"},
		{rel: "mid/mid.go", imports: []string{fixtureModule + "/base"}},
		{rel: "app/app.go", imports: []string{fixtureModule + "/base", fixtureModule + "/mid"}},
	})

	raw, err := json.Marshal(batchInput{Files: []string{"app/app.go", "base/base.go", "mid/mid.go"}})
	require.NoError(t, err)
	out, err := runBatch(t.Context(), raw)
	require.NoError(t, err)

	assert.Contains(t, out.Content, "Batch edit order (dependencies first)")
	assert.Equal(t, "batch sorted 3 files by dependency order", out.Detail)
	assert.Equal(t, []string{"base/base.go", "mid/mid.go", "app/app.go"}, batchOrder(t, out.Content))
	// Entries list their raw imports; the leaf has none.
	assert.Contains(t, out.Content, "imports: "+fixtureModule+"/base, "+fixtureModule+"/mid")
	assert.Contains(t, out.Content, "1. @file base/base.go\n\n", "leaf entry carries no imports line")

	// Files the graph does not know pass through gracefully.
	raw, err = json.Marshal(batchInput{Files: []string{"ghost/nope.go", "base/base.go"}})
	require.NoError(t, err)
	out, err = runBatch(t.Context(), raw)
	require.NoError(t, err)
	assert.Equal(t, "batch sorted 2 files by dependency order", out.Detail)
	assert.ElementsMatch(t, []string{"ghost/nope.go", "base/base.go"}, batchOrder(t, out.Content))
}

func TestRunBatch_RejectsBadInput_Negative(t *testing.T) {
	// The blank-paths rejection happens after the project/graph lookup, so a
	// hermetic project must be installed for the whole table.
	installFixtureProject(t, []chainFixture{
		{rel: "base/base.go"},
		{rel: "app/app.go", imports: []string{fixtureModule + "/base"}},
	})

	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"malformed JSON", `{"files": [`, "failed to parse batch arguments"},
		{"wrong files type", `{"files": 7}`, "failed to parse batch arguments"},
		{"empty files list", `{"files": []}`, "batch tool requires a non-empty files list"},
		{"missing files field", `{}`, "batch tool requires a non-empty files list"},
		{"blank paths only", `{"files": [" ", ""]}`, "batch tool requires at least one non-empty file path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runBatch(t.Context(), json.RawMessage(tc.input))
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErr)
			assert.Empty(t, out.Content)
		})
	}
}

// An unusable HOME makes project discovery fail; the tool must answer with
// its own "project config not loaded" error instead of the process dying
// inside project.GetDefaultProject's panic — the batch tool carried the same
// unreachable nil-guard the snapshot tool's pin documented, so it switched
// to TryDefaultProject with it.
func TestRunBatch_UnusableHome_Negative(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(home, []byte("x"), 0o644))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project.SetDefaultProject(nil)
	t.Cleanup(func() { project.SetDefaultProject(nil) })

	raw, err := json.Marshal(batchInput{Files: []string{"a.go"}})
	require.NoError(t, err)
	var runErr error
	require.NotPanics(t, func() {
		_, runErr = runBatch(t.Context(), raw)
	})
	require.Error(t, runErr)
	assert.Contains(t, runErr.Error(), "project config not loaded")
}

// oracle: stdlib go/parser import extraction plus a naive depth-ranked
// topological reference computed in the test. The fixture chain forces a
// unique topological order, so runBatch's output must equal the reference
// exactly and satisfy the dependency-precedes-importer invariant.
func TestRunBatch_MatchesNaiveTopoSort_Differential(t *testing.T) {
	files := []chainFixture{
		{rel: "util/util.go"},
		{rel: "store/store.go", imports: []string{fixtureModule + "/util"}},
		{rel: "svc/svc.go", imports: []string{fixtureModule + "/store"}},
		{rel: "main/main.go", imports: []string{fixtureModule + "/svc", fixtureModule + "/store"}},
	}
	root := installFixtureProject(t, files)

	// Reference edges: parse the fixture files ourselves and map imports to
	// fixture files by hand.
	edges := map[string][]string{}
	for _, f := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(root, f.rel), nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range file.Imports {
			dst := importToFile(strings.Trim(imp.Path.Value, `"`))
			require.NotEmpty(t, dst, "fixture import must map to a fixture file: %s", imp.Path.Value)
			edges[f.rel] = append(edges[f.rel], dst)
		}
	}

	// Naive order: deepest dependency first. depth(f) = 1 + max(depth of the
	// fixture files f imports); leaves are 0. On a chain this is the unique
	// topological order.
	var depth func(string) int
	depth = func(f string) int {
		d := 0
		for _, dep := range edges[f] {
			if dd := depth(dep) + 1; dd > d {
				d = dd
			}
		}
		return d
	}
	want := make([]string, len(files))
	for i, f := range files {
		want[i] = f.rel
	}
	sort.SliceStable(want, func(i, j int) bool { return depth(want[i]) < depth(want[j]) })

	raw, err := json.Marshal(batchInput{
		Files: []string{"store/store.go", "main/main.go", "util/util.go", "svc/svc.go"}, // shuffled input
	})
	require.NoError(t, err)
	out, err := runBatch(t.Context(), raw)
	require.NoError(t, err)
	got := batchOrder(t, out.Content)
	assert.Equal(t, want, got)

	// Independent invariant: every dependency is listed before its importer.
	idx := make(map[string]int, len(got))
	for i, p := range got {
		idx[p] = i
	}
	for src, deps := range edges {
		for _, dst := range deps {
			assert.Less(t, idx[dst], idx[src], "%s must be edited before %s", dst, src)
		}
	}
}
