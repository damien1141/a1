package secrettool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// wantHit is one golden detection: the 1-based corpus line and the pattern
// name the scanner must report for it.
type wantHit struct {
	line int
	name string
}

// buildSecretCorpus writes a corpus whose every line's expected detection
// was derived by hand from the documented pattern list (most-specific-first
// ordering, the Password keyword placeholder reject, and the Generic-secret
// hex-hash reject), not by running the scanner.
func buildSecretCorpus(t *testing.T) (file string, want []wantHit) {
	t.Helper()
	root := t.TempDir()
	t.Chdir(root)

	// Values are built with explicit lengths so the corpus pins the exact
	// pattern boundaries ({20,} for API key, {16,} for Generic secret,
	// 40 chars for AWS secret, 16 after ghp, 16 after AKIA).
	apiLong := "sk-" + strings.Repeat("a9", 9)  // 21 chars — API key class
	apiMed19 := strings.Repeat("aB", 9) + "c"   // 19 — below API key, above Generic
	apiShort15 := strings.Repeat("aB", 7) + "c" // 15 — below both
	ghTok := "ghp" + strings.Repeat("aB", 18)   // ghp + 36
	awsKey := "AKIA" + strings.Repeat("B2", 8)  // AKIA + 16
	awsSec := strings.Repeat("Zx", 20)          // 40 chars
	md5Hex := strings.Repeat("0123456789abcdef", 2)
	sha256Hex := strings.Repeat("0123456789abcdef", 4)
	nonHex40 := strings.Repeat("zZ", 20)  // 40 chars but not hex
	bearerTok := strings.Repeat("eY.", 7) // 21 chars
	slackTok := "xoxb-" + strings.Repeat("7a", 6)
	tok15 := strings.Repeat("tK", 7) + "z"
	tok16 := strings.Repeat("tK", 8)

	lines := []string{
		"# plain comment with no credentials",        // 1  none
		`api_key = "` + apiLong + `"`,                // 2  API key
		"aws_secret_access_key = " + awsSec,          // 3  AWS secret
		`github_token = "` + ghTok + `"`,             // 4  GitHub token
		"auth: Bearer " + bearerTok,                  // 5  Bearer token
		"slack_token = " + slackTok,                  // 6  Slack token
		"-----BEGIN RSA PRIVATE KEY-----",            // 7  Private key
		"-----BEGIN PRIVATE KEY-----",                // 8  Private key
		"password: hunter2hunter2",                   // 9  Password
		"pwd: changeme",                              // 10 placeholder → none
		"password: password",                         // 11 placeholder → none
		"key: " + md5Hex,                             // 12 md5 hash → none
		"key: " + sha256Hex,                          // 13 sha256 hash → none
		"secret: " + nonHex40,                        // 14 Generic secret
		`api_key = "` + apiMed19 + `"`,               // 15 Generic secret (19 chars)
		`api_key = "` + apiShort15 + `"`,             // 16 too short → none
		"Authorization: Bearer shorttok",             // 17 too short → none
		"// the api_key docs live elsewhere",         // 18 prose → none
		`db_pwd = "SuperSecret99"; backup ` + awsKey, // 19 AWS access key wins over Password
		"passwd: p@ssw0rd_2024",                      // 20 Password (wide char class)
		"token: " + tok15,                            // 21 too short → none
		"token: " + tok16,                            // 22 Generic secret
	}
	file = "corpus.env"
	writeFile(t, root, file, strings.Join(lines, "\n")+"\n")

	want = []wantHit{
		{2, "API key"},
		{3, "AWS secret"},
		{4, "GitHub token"},
		{5, "Bearer token"},
		{6, "Slack token"},
		{7, "Private key"},
		{8, "Private key"},
		{9, "Password"},
		{14, "Generic secret"},
		{15, "Generic secret"},
		{19, "AWS access key"},
		{20, "Password"},
		{22, "Generic secret"},
	}
	return file, want
}

// parseSecretHits extracts the (line, pattern) pairs the scanner reported
// for one file from its rendered output.
func parseSecretHits(t *testing.T, content, file string) []wantHit {
	t.Helper()
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(file) + `:>> *(\d+)\|\[([^\]]+)\]`)
	var hits []wantHit
	for _, line := range strings.Split(content, "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			n, err := strconv.Atoi(m[1])
			require.NoError(t, err)
			hits = append(hits, wantHit{line: n, name: m[2]})
		}
	}
	return hits
}

func runSecretTool(t *testing.T, in secretInput) (string, string) {
	t.Helper()
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	res, err := SecretTool().Run(t.Context(), raw)
	require.NoError(t, err)
	return res.Content, res.Detail
}

// oracle: hand-written golden literal — a 22-line corpus where each line's
// expected detection (pattern name or none) was derived by hand from the
// documented semantics: most-specific-first ordering, the {16,}/{20,} length
// boundaries, the Password placeholder reject, and the hex-hash reject.
func TestRunSecret_CorpusVsGolden_Differential(t *testing.T) {
	file, want := buildSecretCorpus(t)

	t.Run("full corpus", func(t *testing.T) {
		content, detail := runSecretTool(t, secretInput{Path: "."})
		got := parseSecretHits(t, content, file)
		assert.Equal(t, want, got, "detections must match the hand-derived golden list")
		// Pinned quirk: Detail counts every rendered line, including the
		// "@file" header, so one file with 13 matches reports 14.
		assert.Equal(t, fmt.Sprintf("%d potential secrets found", len(want)+1), detail)
	})

	t.Run("limit abort discards collected matches", func(t *testing.T) {
		// Pinned quirk: when the limit is exceeded the walk callback returns
		// an error mid-file, before the file's matches are appended to the
		// result — so the truncated scan renders only the limit-reached line
		// and none of the matches found so far.
		content, detail := runSecretTool(t, secretInput{Path: ".", Limit: 3})
		got := parseSecretHits(t, content, file)
		assert.Empty(t, got, "current behavior: matches collected before the limit abort are discarded")
		assert.Contains(t, content, "3 secrets limit reached")
		assert.Equal(t, "1 potential secrets found", detail, "the limit line itself is counted")
	})
}

// oracle: hand-written golden literal — the walk scope (ignored directories,
// glob filter) is pinned by fixtures whose only reported hits are the ones a
// reader of the documented walk rules would predict.
func TestRunSecret_WalkScopeVsGolden_Differential(t *testing.T) {
	t.Run("ignored directories are skipped", func(t *testing.T) {
		root := t.TempDir()
		t.Chdir(root)
		const leak = "password: hunter2hunter2\n"
		writeFile(t, root, "keep.txt", leak)
		for _, dir := range []string{"node_modules", "vendor", ".git", "venv"} {
			require.NoError(t, os.MkdirAll(filepath.Join(root, dir), 0o755))
			writeFile(t, root, filepath.Join(dir, "leak.js"), leak)
		}

		content, detail := runSecretTool(t, secretInput{Path: "."})
		got := parseSecretHits(t, content, "keep.txt")
		assert.Equal(t, []wantHit{{1, "Password"}}, got)
		for _, dir := range []string{"node_modules", "vendor", ".git", "venv", "leak.js"} {
			assert.NotContains(t, content, dir, "ignored directory contents must not be scanned")
		}
		// 1 match + 1 "@file" header line (pinned quirk, see the corpus test).
		assert.Equal(t, "2 potential secrets found", detail)
	})

	t.Run("glob filters by base name", func(t *testing.T) {
		root := t.TempDir()
		t.Chdir(root)
		writeFile(t, root, "a.txt", "password: hunter2hunter2\n")
		writeFile(t, root, "b.go", "password: hunter2hunter2\n")

		content, detail := runSecretTool(t, secretInput{Path: ".", Glob: "*.txt"})
		got := parseSecretHits(t, content, "a.txt")
		assert.Equal(t, []wantHit{{1, "Password"}}, got)
		assert.NotContains(t, content, "b.go", "glob must filter out non-matching files")
		// 1 match + 1 "@file" header line (pinned quirk, see the corpus test).
		assert.Equal(t, "2 potential secrets found", detail)
	})
}

// naiveSecretDetail re-derives DetailFromArgs independently: a map-based
// encoding/json decode (the tool decodes into a struct), the empty→"."
// default, and the "secret scan" formatting.
func naiveSecretDetail(input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	p, _ := m["path"].(string)
	p = strings.TrimSpace(p)
	if p == "" {
		p = "."
	}
	return "secret scan " + p
}

// oracle: naive reference implementation in this test — a map-based JSON
// decode plus hand-rolled formatting, compared over normal, edge, and
// hostile inputs the detail helper must survive.
func TestDetailFromArgs_Differential(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"path plain", `{"path":"./src"}`},
		{"path padded", `{"path":"  ./src  "}`},
		{"missing path defaults to dot", `{}`},
		{"blank path defaults to dot", `{"path":"   "}`},
		{"non-string path", `{"path": 42}`},
		{"null path", `{"path": null}`},
		{"not json", `nope`},
		{"json array", `[]`},
	}
	tool := SecretTool()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, naiveSecretDetail(json.RawMessage(tc.input)), tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}
