package contexttool

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// naiveRule is one fold-analysis trigger expressed as a regexp predicate —
// a different matching engine than the production strings.Contains chain.
type naiveRule struct {
	match      func(line string) bool
	category   string
	tier       string
	message    string
	suggestion string
}

func has(pattern string) func(string) bool {
	re := regexp.MustCompile(regexp.QuoteMeta(pattern))
	return func(line string) bool { return re.MatchString(line) }
}

// naiveRules mirrors the documented trigger table in evaluation order.
func naiveRules() []naiveRule {
	return []naiveRule{
		{has("context.WithValue"), "protected-zone", "tier-1",
			"context.WithValue usage detected", "consider using typed context keys for better type safety"},
		{has("context.Background()"), "recent-zone", "tier-1",
			"context.Background() usage detected", "consider using context.WithTimeout for bounded operations"},
		{func(l string) bool { return has("http.NewRequest")(l) && !has("WithContext")(l) },
			"growth-gate", "tier-2", "HTTP request without context", "use http.NewRequestWithContext with a timeout"},
		{func(l string) bool { return has("context.WithTimeout")(l) || has("context.WithDeadline")(l) },
			"protected-zone", "tier-1", "bounded context detected", "protected zone: timeout discipline confirmed"},
		{func(l string) bool { return has("cancel")(l) && has("defer")(l) },
			"protected-zone", "tier-1", "cancellation cleanup detected", "protected zone: cancel discipline confirmed"},
		{func(l string) bool { return has("io.ReadAll")(l) || has("ioutil.ReadAll")(l) },
			"growth-gate", "tier-2", "unbounded read detected", "limit body reads or compress the result before adding to context"},
		{func(l string) bool { return has("json.Marshal")(l) || has("json.MarshalIndent")(l) },
			"growth-gate", "tier-2", "JSON serialization detected", "compress large payloads before adding to context"},
	}
}

var naiveSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, ".tox": true, "dist": true, "build": true,
}

// naiveFoldScan is the reference implementation: its own walk (WalkDir, not
// Walk), its own .go filter, and the regexp trigger table above.
func naiveFoldScan(t *testing.T, root string) (content string, detail string) {
	t.Helper()
	rules := naiveRules()
	type entry struct {
		file, category, tier, message, suggestion string
		line                                      int
	}
	var entries []entry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		if d.IsDir() {
			if naiveSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		for i, line := range strings.Split(string(b), "\n") {
			for _, r := range rules {
				if r.match(line) {
					entries = append(entries, entry{path, r.category, r.tier, r.message, r.suggestion, i + 1})
				}
			}
		}
		return nil
	})
	require.NoError(t, err)

	var sb strings.Builder
	protected, growth, recent, tier1, tier2 := 0, 0, 0, 0, 0
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("%s:%d\t%s\t%s\t%s\t%s\n", e.file, e.line, e.category, e.tier, e.message, e.suggestion))
		switch e.category {
		case "protected-zone":
			protected++
		case "growth-gate":
			growth++
		case "recent-zone":
			recent++
		}
		switch e.tier {
		case "tier-1":
			tier1++
		case "tier-2":
			tier2++
		}
	}
	summary := fmt.Sprintf("protected-zone=%d growth-gate=%d recent-zone=%d tier-1=%d tier-2=%d total=%d",
		protected, growth, recent, tier1, tier2, len(entries))
	if sb.Len() == 0 {
		return "No context issues detected", summary
	}
	return sb.String(), summary
}

// oracle: naive reference implementation in this test — an independent
// fold scanner (filepath.WalkDir + regexp trigger table) re-derives every
// issue line, its order, and the category/tier summary for the same fixture
// tree, including skipped directories, non-Go files, same-line multi-triggers,
// and the WithContext negation.
func TestRunContext_Differential(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", `package a

func f() {
        ctx := context.WithValue(context.Background(), "k", "v")
        _ = ctx
}
`)
	writeFile(t, root, "b.go", `package b

func g() {
        req, _ := http.NewRequest("GET", "http://example.com", nil)
        safe, _ := http.NewRequestWithContext(ctx, "GET", "http://example.com", nil)
        _, _ = io.ReadAll(r)
        _, _ = json.Marshal(v)
        _, _ = req, safe
}
`)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	writeFile(t, filepath.Join(root, "sub"), "c.go", `package c

func h() {
        ctx, cancel := context.WithTimeout(context.Background(), deadline)
        defer cancel()
        ch := cancelChan
        defer close(ch)
}
`)
	// .git and non-Go files must contribute nothing even when they trigger.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	writeFile(t, filepath.Join(root, ".git"), "skip.go", "package git\n\nfunc dead() { ctx := context.Background() }\n")
	writeFile(t, root, "notes.txt", "ctx := context.Background()\nhttp.NewRequest\n")

	raw, err := json.Marshal(contextInput{Path: root, Limit: 100})
	require.NoError(t, err)
	out, err := runContext(t.Context(), raw)
	require.NoError(t, err)

	wantContent, wantDetail := naiveFoldScan(t, root)
	// Guard against vacuous agreement: the fixture must produce real hits on
	// both sides of the comparison.
	require.NotEqual(t, "No context issues detected", wantContent)
	require.Contains(t, wantDetail, "total=8")
	assert.Equal(t, wantContent, out.Content)
	assert.Equal(t, wantDetail, out.Detail)
	assert.Equal(t, wantContent, out.Output)
}
