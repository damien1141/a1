package errpattern

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- naive reference implementation (differential oracle) ----------
//
// Every anti-pattern predicate is re-derived with plain byte scanning — no
// regexp — mirroring RE2 semantics for the constructs the scanner uses
// (case-insensitive literals, \s*/\s+ runs, \w+, [^)]*). The directory walk is
// a separate os.ReadDir recursion (the scanner uses filepath.Walk) over the
// same fixture tree, skipping the same directory names.

func npLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func npHasLit(line string, i int, lit string) bool {
	if i < 0 || i+len(lit) > len(line) {
		return false
	}
	for j := 0; j < len(lit); j++ {
		if npLower(line[i+j]) != lit[j] {
			return false
		}
	}
	return true
}

func npFindLit(line, lit string, from int) int {
	for i := from; i+len(lit) <= len(line); i++ {
		if npHasLit(line, i, lit) {
			return i
		}
	}
	return -1
}

// \s in Go regexp is exactly [\t\n\f\r ]; lines carry no \n but may carry \r.
func npIsSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\f' || b == '\r'
}

func npSkipSpace(line string, i int) int {
	for i < len(line) && npIsSpace(line[i]) {
		i++
	}
	return i
}

// npSkipSpacePlus matches \s+ — at least one whitespace byte at i.
func npSkipSpacePlus(line string, i int) (int, bool) {
	if i >= len(line) || !npIsSpace(line[i]) {
		return i, false
	}
	return npSkipSpace(line, i), true
}

func npIsWord(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// npIgnoredErr matches `(?i)_\s*=\s*err` — an underscore, an equals, then err.
func npIgnoredErr(line string) bool {
	for i := npFindLit(line, "_", 0); i >= 0; i = npFindLit(line, "_", i+1) {
		j := npSkipSpace(line, i+1)
		if j >= len(line) || line[j] != '=' {
			continue
		}
		if k := npSkipSpace(line, j+1); npHasLit(line, k, "err") {
			return true
		}
	}
	return false
}

// npSemicolonErr matches `(?i)err\s*:=\s*\w+\([^)]*\)\s*;`.
func npSemicolonErr(line string) bool {
	for i := npFindLit(line, "err", 0); i >= 0; i = npFindLit(line, "err", i+1) {
		p := npSkipSpace(line, i+3)
		if !npHasLit(line, p, ":=") {
			continue
		}
		p = npSkipSpace(line, p+2)
		d := p
		for d < len(line) && npIsWord(line[d]) {
			d++
		}
		if d == p || d >= len(line) || line[d] != '(' {
			continue
		}
		e := d + 1
		for e < len(line) && line[e] != ')' {
			e++
		}
		if e >= len(line) {
			continue
		}
		if f := npSkipSpace(line, e+1); f < len(line) && line[f] == ';' {
			return true
		}
	}
	return false
}

// npBareReturn matches `(?i)if\s+err\s*:=\s*\w+\([^)]*\);\s*err\s*!=\s*nil\s*\{\s*return\s+err\s*\}`.
func npBareReturn(line string) bool {
	for i := npFindLit(line, "if", 0); i >= 0; i = npFindLit(line, "if", i+1) {
		p, ok := npSkipSpacePlus(line, i+2)
		if !ok || !npHasLit(line, p, "err") {
			continue
		}
		p = npSkipSpace(line, p+3)
		if !npHasLit(line, p, ":=") {
			continue
		}
		p = npSkipSpace(line, p+2)
		d := p
		for d < len(line) && npIsWord(line[d]) {
			d++
		}
		if d == p || d >= len(line) || line[d] != '(' {
			continue
		}
		e := d + 1
		for e < len(line) && line[e] != ')' {
			e++
		}
		if e+1 >= len(line) || line[e+1] != ';' {
			continue
		}
		p = npSkipSpace(line, e+2)
		if !npHasLit(line, p, "err") {
			continue
		}
		p = npSkipSpace(line, p+3)
		if !npHasLit(line, p, "!=") {
			continue
		}
		p = npSkipSpace(line, p+2)
		if !npHasLit(line, p, "nil") {
			continue
		}
		p = npSkipSpace(line, p+3)
		if p >= len(line) || line[p] != '{' {
			continue
		}
		p = npSkipSpace(line, p+1)
		if !npHasLit(line, p, "return") {
			continue
		}
		q, ok2 := npSkipSpacePlus(line, p+6)
		if !ok2 || !npHasLit(line, q, "err") {
			continue
		}
		if r := npSkipSpace(line, q+3); r < len(line) && line[r] == '}' {
			return true
		}
	}
	return false
}

func npContainsLit(line, lit string) bool {
	return npFindLit(line, lit, 0) >= 0
}

var naiveErrPatternPreds = []struct {
	msg  string
	pred func(string) bool
}{
	{"error is ignored; handle or explicitly discard with blank identifier", npIgnoredErr},
	{"error is ignored; handle or explicitly discard with blank identifier", npSemicolonErr},
	{"error is returned without context; consider wrapping with additional context", npBareReturn},
	{"panic usage; consider returning error instead", func(l string) bool { return npContainsLit(l, "panic(") }},
	{"log.Fatal in library code; consider returning error instead", func(l string) bool { return npContainsLit(l, "log.fatal") }},
}

func npSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".tox", "dist", "build":
		return true
	}
	return false
}

type npHit struct {
	rel  string
	line int
	msg  string
}

// naiveScan walks root with os.ReadDir recursion in sorted order and applies
// the naive predicates per file, pattern order outermost, then line order —
// the emission order the scanner documents.
func naiveScan(t *testing.T, root, glob string, limit int) []npHit {
	t.Helper()
	var hits []npHit
	var walk func(dir, rel string)
	walk = func(dir, rel string) {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			childRel := entry.Name()
			if rel != "" {
				childRel = rel + "/" + entry.Name()
			}
			if entry.IsDir() {
				if !npSkipDir(entry.Name()) {
					walk(path, childRel)
				}
				continue
			}
			if !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			matched, err := filepath.Match(glob, entry.Name())
			require.NoError(t, err)
			if !matched {
				continue
			}
			b, err := os.ReadFile(path)
			require.NoError(t, err)
			lines := strings.Split(string(b), "\n")
			for _, p := range naiveErrPatternPreds {
				for i, line := range lines {
					if p.pred(line) {
						hits = append(hits, npHit{rel: childRel, line: i + 1, msg: p.msg})
					}
				}
			}
		}
	}
	walk(root, "")
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// parseErrPatternLines decodes the rendered "rel:line\tmessage" rows.
func parseErrPatternLines(t *testing.T, content string) []npHit {
	t.Helper()
	var out []npHit
	for _, row := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		if row == "" {
			continue
		}
		parts := strings.SplitN(row, "\t", 2)
		require.Len(t, parts, 2, "each row is rel:line\\tmessage: %q", row)
		i := strings.LastIndex(parts[0], ":")
		require.GreaterOrEqual(t, i, 0, "row carries the rel:line prefix: %q", row)
		out = append(out, npHit{rel: parts[0][:i], line: atoi(t, parts[0][i+1:]), msg: parts[1]})
	}
	return out
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	require.NoError(t, err)
	return n
}

// writeErrPatternFixture materializes the scan corpus: clean files, files
// with every anti-pattern (including case games and mid-word matches), a
// non-Go decoy, a vendor/ tree that must be skipped, and a *_test.go file.
func writeErrPatternFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	write("main.go", "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n")
	write("a.go", "package a\n\nfunc f() {\n\terr := do()\n\t_ = err\n\tif err := wrap(); err != nil { return err }\n\tpanic(\"boom\")\n\tlog.Fatal(\"die\")\n}\n")
	write("CASE.go", "package a\n\nfunc g() {\n\t_=Err\n\tIF ERR:=DO();ERR != NIL {RETURN ERR}\n}\n")
	write("sub/b.go", "package b\n\nfunc h() {\n\tx := do()\n\tmyerr := f(x);\n}\n")
	write("sub/deep/c.go", "package c\n\n// _ = err in a comment\nvar _ = err\n")
	write("vendor/skipme.go", "package v\n\npanic(\"vendor must be skipped\")\n")
	write("notes.txt", "panic(\"not a go file\")\n")
	write("x_test.go", "package x\n\npanic(\"in a test file\")\n")
	return root
}

// oracle: naive reference implementation in this test — the five anti-pattern
// predicates re-derived with plain string scanning (no regexp) plus an
// independent os.ReadDir walk, compared row for row (order included) against
// the scanner's output over the same fixture tree.
func TestRunErrPattern_Differential(t *testing.T) {
	root := writeErrPatternFixture(t)
	t.Chdir(root)

	run := func(glob string, limit int) []npHit {
		t.Helper()
		raw, err := json.Marshal(errPatternInput{Path: ".", Glob: glob, Limit: limit})
		require.NoError(t, err)
		res, err := runErrPattern(t.Context(), raw)
		require.NoError(t, err)
		return parseErrPatternLines(t, res.Content)
	}

	t.Run("default glob full tree", func(t *testing.T) {
		got := run("", 100)
		want := naiveScan(t, root, "*.go", 100)
		assert.Equal(t, want, got)
		// The corpus guarantees at least one hit per predicate plus a vendor
		// file that must not appear.
		assert.NotEmpty(t, want)
		for _, h := range got {
			assert.NotContains(t, h.rel, "vendor", "vendor/ must be skipped")
		}
	})

	t.Run("test-file glob only", func(t *testing.T) {
		got := run("*_test.go", 100)
		want := naiveScan(t, root, "*_test.go", 100)
		assert.Equal(t, want, got)
		assert.Equal(t, []npHit{{rel: "x_test.go", line: 3, msg: "panic usage; consider returning error instead"}}, got)
	})

	t.Run("limit cuts the prefix", func(t *testing.T) {
		got := run("", 3)
		want := naiveScan(t, root, "*.go", 100)
		require.Greater(t, len(want), 3)
		assert.Equal(t, want[:3], got)
	})

	t.Run("glob matching nothing renders the empty scan", func(t *testing.T) {
		assert.Empty(t, naiveScan(t, root, "*.nomatch", 100))
		raw, err := json.Marshal(errPatternInput{Path: ".", Glob: "*.nomatch", Limit: 100})
		require.NoError(t, err)
		res, err := runErrPattern(t.Context(), raw)
		require.NoError(t, err)
		assert.Equal(t, "No error handling issues detected", res.Content)
		assert.Equal(t, "0 hits", res.Detail)
	})
}
