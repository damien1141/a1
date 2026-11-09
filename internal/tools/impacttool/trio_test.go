package impacttool

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// requireGrep skips when the grep binary the tool shells out to is missing.
func requireGrep(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not on PATH:", err)
	}
}

// impactFixtureTree covers the scan's real variables: hidden directories the
// reference walk must enter, subdirectories, a CRLF file (grep keeps the \r
// in the printed line), CJK text, a longer identifier that contains the
// symbol (substring semantics), and files the *.go include must reject.
var impactFixtureTree = map[string]string{
	"a.go":         "package a\n\nfunc Target() {}\n\tTarget()\n\tTargetX()\n",
	"sub/b.go":     "package b\n\nfunc main() {\n\tTarget()\n}\n",
	".hidden/c.go": "package c\n\nTarget()\n",
	"notes.txt":    "Target()\n",
	"deep.go.md":   "Target()\n",
	"crlf.go":      "Target()\r\nplain\r\n",
	"cjk.go":       "Target(日本語)\n",
}

func writeImpactTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		fp := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(fp), 0o755), "fixture dir for %s", rel)
		require.NoError(t, os.WriteFile(fp, []byte(content), 0o644), "fixture file %s", rel)
	}
	return root
}

// refImpactScan re-derives the reference set with the standard library alone:
// filepath.WalkDir enumerates files (hidden ones included — GNU grep -r has
// no hidden-file semantics), path.Match applies the include glob to the base
// name the way --include does, and plain strings.Contains finds symbol
// occurrences. Symbols in these fixtures are regex-metachar-free, so grep's
// basic-regex reading of the pattern is the literal one. Output is sorted:
// grep's directory traversal order is not a contract.
func refImpactScan(t *testing.T, root, symbol, glob string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if ok, _ := path.Match(glob, path.Base(rel)); !ok {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, symbol) {
				out = append(out, rel+":"+strconv.Itoa(i+1)+"\t"+line)
			}
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(out)
	return out
}

func runImpactJSON(t *testing.T, args map[string]any) (string, string) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	res, err := runImpact(t.Context(), raw)
	require.NoError(t, err)
	return res.Content, res.Detail
}

// oracle: stdlib equivalence — a filepath.WalkDir + strings.Contains scan of
// the same fixture tree re-derives the full reference set (file, line,
// context) for the blast-radius report: hidden dirs, subdirs, CRLF and CJK
// lines, substring matches, and the include glob's basename filtering.
func TestRunImpact_RefSetVsWalkDirScan_Differential(t *testing.T) {
	requireGrep(t)
	root := writeImpactTree(t, impactFixtureTree)
	t.Chdir(root)

	t.Run("default go glob", func(t *testing.T) {
		want := refImpactScan(t, root, "Target", "*.go")
		require.Len(t, want, 7, "reference sanity: seven refs across five .go files")
		require.Contains(t, want, "a.go:5\t\tTargetX()", "substring matches are part of the contract")
		require.NotContains(t, want, "notes.txt", "non-go files are filtered")
		require.NotContains(t, want, "deep.go.md", "basename glob must not match deep.go.md")

		content, detail := runImpactJSON(t, map[string]any{"symbol": "Target", "path": root})
		// Only the trailing newline is stripped: the CRLF fixture's last
		// grep-printed line legitimately ends in \r, which a TrimSpace would
		// silently eat.
		got := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
		sort.Strings(got)
		assert.Equal(t, want, got, "rendered refs must equal the WalkDir scan reference")
		assert.Equal(t, "7 references to Target", detail)
	})

	t.Run("custom glob picks other extensions", func(t *testing.T) {
		want := refImpactScan(t, root, "Target", "*.md")
		require.Equal(t, []string{"deep.go.md:1\tTarget()"}, want, "reference sanity")

		content, detail := runImpactJSON(t, map[string]any{"symbol": "Target", "path": root, "glob": "*.md"})
		got := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
		assert.Equal(t, want, got)
		assert.Equal(t, "1 references to Target", detail)
	})

	t.Run("limit keeps only reference-set members", func(t *testing.T) {
		want := refImpactScan(t, root, "Target", "*.go")
		require.Len(t, want, 7)

		// grep's traversal order decides which three survive, so only
		// membership and count are deterministic.
		content, detail := runImpactJSON(t, map[string]any{"symbol": "Target", "path": root, "limit": 3})
		got := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
		require.Len(t, got, 3)
		for _, l := range got {
			assert.Contains(t, want, l, "limited picks must come from the reference set")
		}
		assert.Equal(t, "3 references to Target", detail)
	})
}
