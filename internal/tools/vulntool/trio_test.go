package vulntool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// naiveSkip models the scanner's documented directory skip list so the
// oracle walks the fixture under the same contract as the implementation.
var naiveSkip = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true,
	"venv": true, "__pycache__": true, ".tox": true, "dist": true, "build": true,
}

// naiveMarkers detects the planted findings with plain substring tests — an
// independent mechanism from the scanner's regexp table. Messages and
// severities are hand-written literals for the documented finding
// categories, not values produced by the code under test.
var naiveMarkers = []struct {
	marker   string
	severity string
	message  string
}{
	{"md5.New()", "medium", "use of weak hash function MD5"},
	{"sha1.New()", "medium", "use of weak hash function SHA1"},
	{"http.Get(", "medium", "HTTP request without timeout; consider context.WithTimeout"},
	{`fmt.Sprintf("SELECT * FROM users WHERE id = %s"`, "high", "potential SQL injection via fmt.Sprintf with user input"},
	{`exec.Command("sh"`, "high", "potential command injection via exec.Command with user input"},
	{`template.HTML("`, "high", "potential XSS via template.HTML with unescaped content"},
	{`password = "`, "high", "hardcoded password in source code"},
	{"InsecureSkipVerify: true", "high", "InsecureSkipVerify=true disables TLS certificate validation"},
}

// naiveFinding is the oracle's tuple for one reported hit.
type naiveFinding struct {
	path     string
	line     int
	severity string
	message  string
}

// naiveScan re-derives the findings with an os.ReadDir recursion (not the
// tool's filepath.Walk) and substring detection (not its regexp table).
func naiveScan(t *testing.T, root, rel, glob string) []naiveFinding {
	t.Helper()
	var findings []naiveFinding
	entries, err := os.ReadDir(filepath.Join(root, rel))
	require.NoError(t, err)
	for _, e := range entries {
		childRel := filepath.ToSlash(filepath.Join(rel, e.Name()))
		if e.IsDir() {
			if naiveSkip[e.Name()] {
				continue
			}
			findings = append(findings, naiveScan(t, root, childRel, glob)...)
			continue
		}
		matched, err := filepath.Match(glob, e.Name())
		require.NoError(t, err)
		if !matched || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, childRel))
		require.NoError(t, err)
		for i, line := range strings.Split(string(b), "\n") {
			for _, m := range naiveMarkers {
				if strings.Contains(line, m.marker) {
					findings = append(findings, naiveFinding{
						path: childRel, line: i + 1, severity: m.severity, message: m.message,
					})
				}
			}
		}
	}
	return findings
}

// vulnFixture plants one finding per scanner category plus near-misses that
// must stay silent, spread across a nested dir, a vendor dir (skipped), a
// non-.go file (glob-excluded), and a clean file. The files are never
// compiled — only scanned as text.
func vulnFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "a.go", `package main

import (
	"crypto/md5"
	"net/http"
)

func fetch(url string) {
	h := md5.New()
	_ = h
	resp, err := http.Get(url)
	_ = resp
	_ = err
}
`)
	writeFile(t, root, "c.go", `package config

import (
	"crypto/sha1"
	"crypto/tls"
)

const password = "hunter2hunter2"

func tlsConf() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true}
}

func weak() {
	s := sha1.New()
	_ = s
}
`)
	// Near-misses: a comment naming the weak hashes without call parens, and
	// a %-format Sprintf with a non-%s verb — neither is a finding.
	writeFile(t, root, "d.go", `package clean

// md5.New and sha1.New are weak, but this comment has no call parens.
func safe(userID int) string {
	return fmt.Sprintf("SELECT * FROM users WHERE id = %d", userID)
}
`)
	writeFile(t, root, "gen.gen.go", `package gen

import "crypto/md5"

func gen() {
	h := md5.New()
	_ = h
}
`)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	writeFile(t, root, filepath.Join("sub", "b.go"), `package main

import (
	"fmt"
	"os/exec"
	"text/template"
)

func risky() {
	q := fmt.Sprintf("SELECT * FROM users WHERE id = %s", userID)
	_ = q
	cmd := exec.Command("sh", "-c", userInput)
	_ = cmd
	_ = template.HTML("<b>bold</b>")
}
`)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "vendor"), 0o755))
	writeFile(t, root, filepath.Join("vendor", "skipme.go"), `package vendor

import "crypto/md5"

func init() {
	h := md5.New()
	_ = h
}
`)
	// Would match several patterns if it were ever scanned; the *.go glob
	// and the .go suffix check must both exclude it.
	writeFile(t, root, "notes.txt", "md5.New() http.Get( password = \"aaaaaaaa\"\n")
	return root
}

// parseVulnLines splits the tool's "path:line\t[severity]\tmessage" output
// back into comparable tuples.
func parseVulnLines(t *testing.T, content string) []naiveFinding {
	t.Helper()
	var got []naiveFinding
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		if line == "" {
			continue
		}
		tab := strings.Index(line, "\t")
		require.NotEqual(t, -1, tab, "finding must be path:line\\t[severity]\\tmessage, got %q", line)
		head, rest := line[:tab], line[tab+1:]
		colon := strings.LastIndex(head, ":")
		require.NotEqual(t, -1, colon, "finding head must be path:line, got %q", head)
		lineNo, err := strconv.Atoi(head[colon+1:])
		require.NoError(t, err)
		nextTab := strings.Index(rest, "\t")
		require.NotEqual(t, -1, nextTab, "finding tail must be [severity]\\tmessage, got %q", rest)
		got = append(got, naiveFinding{
			path:     head[:colon],
			line:     lineNo,
			severity: strings.Trim(rest[:nextTab], "[]"),
			message:  rest[nextTab+1:],
		})
	}
	return got
}

// oracle: naive reference implementation in this test — an os.ReadDir
// recursion with substring detection over the fixture corpus, independent of
// the scanner's filepath.Walk traversal and regexp pattern table; expected
// messages and severities are hand-written literals.
func TestRunVuln_Findings_Differential(t *testing.T) {
	root := vulnFixture(t)
	t.Chdir(root)

	for _, tc := range []struct {
		name string
		glob string
	}{
		{"default go glob", "*.go"},
		{"narrow gen glob", "*.gen.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(vulnInput{Path: ".", Glob: tc.glob})
			require.NoError(t, err)

			res, err := VulnTool().Run(t.Context(), raw)
			require.NoError(t, err)

			want := naiveScan(t, root, "", tc.glob)
			require.NotEmpty(t, want, "oracle must expect at least one finding")
			assert.ElementsMatch(t, want, parseVulnLines(t, res.Content))
			assert.Equal(t, fmt.Sprintf("%d potential vulnerabilities", len(want)), res.Detail)
			assert.Equal(t, res.Content, res.Output)
		})
	}
}

// oracle: naive reference implementation in this test — a map-based
// encoding/json decode (the tool decodes into a struct) with independent
// trim and default handling over normal, edge, and hostile inputs.
func TestVulnDetailFromArgs_Differential(t *testing.T) {
	cases := []struct{ name, input string }{
		{"path", `{"path": "internal/tools"}`},
		{"padded path", `{"path": "  ./cmd  "}`},
		{"empty object", `{}`},
		{"empty path", `{"path": ""}`},
		{"whitespace path", `{"path": "   "}`},
		{"null path", `{"path": null}`},
		{"non-string path", `{"path": 42}`},
		{"glob and limit only", `{"glob": "*.go", "limit": 5}`},
		{"not json", `not json at all`},
		{"array", `[]`},
	}
	tool := VulnTool()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var m map[string]any
			_ = json.Unmarshal([]byte(tc.input), &m)
			p, _ := m["path"].(string)
			p = strings.TrimSpace(p)
			if p == "" {
				p = "."
			}
			assert.Equal(t, "vuln "+p, tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}
