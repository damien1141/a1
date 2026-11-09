package gitx

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// refParseTrack reads git's track summary by slicing instead of regex:
// strip the brackets, split on ", ", and read each "ahead N"/"behind N"
// field pair.
func refParseTrack(s string) (ahead, behind int, gone bool) {
	if s == "" {
		return 0, 0, false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	for _, part := range strings.Split(inner, ", ") {
		fields := strings.Fields(part)
		if len(fields) != 2 {
			continue
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		switch fields[0] {
		case "ahead":
			ahead = n
		case "behind":
			behind = n
		}
	}
	return ahead, behind, strings.Contains(s, "gone")
}

// refParseBranches re-derives parseBranches with plain strings.Split: full
// split (subject tabs re-joined), the same skips, and the slicing reference
// for the track summary.
func refParseBranches(out string) []Branch {
	var branches []Branch
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 7 {
			continue
		}
		ref, short := f[1], f[2]
		if short == "" || strings.HasSuffix(ref, "/HEAD") {
			continue
		}
		var subject string
		if len(f) > 7 {
			subject = strings.Join(f[6:], "\t")
		} else {
			subject = f[6]
		}
		b := Branch{
			Name:      short,
			Current:   f[0] == "*",
			Remote:    strings.HasPrefix(ref, "refs/remotes/"),
			Upstream:  f[3],
			Committed: f[5],
			Subject:   subject,
		}
		b.Ahead, b.Behind, b.Gone = refParseTrack(f[4])
		branches = append(branches, b)
	}
	return branches
}

// oracle: naive strings.Split-based reference over golden for-each-ref
// output lines (real tab-separated branchFormat records, including a tab
// inside a subject, a symbolic origin/HEAD row, and malformed rows).
func TestParseBranches_MatchesNaiveSplitReference_Differential(t *testing.T) {
	fixture := strings.Join([]string{
		"*\trefs/heads/main\tmain\torigin/main\t[ahead 1]\t2 hours ago\tinit",
		"\trefs/heads/fix/one\tfix/one\t\t\t3 days ago\tfix\tthe\tthing",
		"\trefs/remotes/origin/main\torigin/main\torigin/main\t\t2 hours ago\tinit",
		"\trefs/remotes/origin/HEAD\torigin/HEAD\t\t\t2 hours ago\tinit",
		"*\trefs/heads/tracked\ttracked\torigin/tracked\t[ahead 2, behind 3]\t5 minutes ago\twork",
		"\trefs/heads/gone\tgone\torigin/gone\t[gone]\t1 week ago\tstale",
		"\trefs/heads/clean\tclean\torigin/clean\t\t1 hour ago\tno track info",
		"",
		"\tshortline",
	}, "\n")

	want := refParseBranches(fixture)
	require.Len(t, want, 6, "reference must keep six branches, dropping /HEAD, blank, and short rows")
	assert.Equal(t, want, parseBranches(fixture))

	// Spot-check the semantics the reference encodes.
	byName := map[string]Branch{}
	for _, b := range want {
		byName[b.Name] = b
	}
	assert.True(t, byName["main"].Current)
	assert.True(t, byName["origin/main"].Remote)
	assert.False(t, byName["fix/one"].Remote)
	assert.Equal(t, 2, byName["tracked"].Ahead)
	assert.Equal(t, 3, byName["tracked"].Behind)
	assert.True(t, byName["gone"].Gone)
	assert.Equal(t, "fix\tthe\tthing", byName["fix/one"].Subject)
}

// refIsObjectID re-derives the SHA detector with strconv.ParseUint on
// 10-hex-digit quarters instead of a rune scan.
func refIsObjectID(name string) bool {
	if len(name) != 40 {
		return false
	}
	for i := 0; i < 40; i += 10 {
		if _, err := strconv.ParseUint(name[i:i+10], 16, 64); err != nil {
			return false
		}
	}
	return true
}

// refParseRecent re-derives parseRecent with strings.Split: cut the reflog
// prefix, split on " to " and keep the last field, drop SHAs and duplicates
// with a slice scan instead of a map.
func refParseRecent(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "checkout: moving from ")
		if !ok {
			continue
		}
		parts := strings.Split(rest, " to ")
		if len(parts) < 2 {
			continue // no " to <target>" on this line
		}
		name := strings.TrimSpace(parts[len(parts)-1])
		if name == "" || refIsObjectID(name) {
			continue
		}
		dup := false
		for _, seen := range names {
			if seen == name {
				dup = true
				break
			}
		}
		if !dup {
			names = append(names, name)
		}
	}
	return names
}

// oracle: naive strings.Split-based reference over golden reflog lines
// (branch moves, a detached SHA target, a non-hex 40-char name, unrelated
// reflog entries, duplicates, and padding).
func TestParseRecent_MatchesNaiveSplitReference_Differential(t *testing.T) {
	fixture := strings.Join([]string{
		"checkout: moving from main to fix/one",
		"checkout: moving from fix/one to main",
		"checkout: moving from main to 0123456789abcdef0123456789abcdef01234567",
		"commit (initial): init",
		"checkout: moving from main to fix/one",
		"checkout: moving from main to ",
		"checkout: moving from feature/x to feature/y",
		"",
		"   checkout: moving from a to b   ",
		"checkout: moving from main to " + strings.Repeat("z", 40),
		"pull: fast-forward",
	}, "\n")

	want := refParseRecent(fixture)
	assert.Equal(t, want, parseRecent(fixture))

	// The reference encodes: newest first, SHAs and blanks dropped,
	// duplicates collapsed, non-hex long names kept.
	assert.Equal(t, []string{"fix/one", "main", "feature/y", "b", strings.Repeat("z", 40)}, want)
}

// validRefPattern allows exactly the runes production accepts: printable
// ASCII (0x21–0x7e) and anything at or above U+0080; space, tab, newline,
// and the other C0 controls plus DEL are out.
var validRefPattern = regexp.MustCompile(`^[\x21-\x7e\x80-\x{10FFFF}]+$`)

func refValidRef(ref string) bool {
	return ref != "" && !strings.HasPrefix(ref, "-") && validRefPattern.MatchString(ref)
}

// oracle: regexp character class — ValidRef's rune-loop rules re-derived as
// a single anchored pattern plus the option-prefix and empty rules.
func TestValidRef_MatchesRegexpReference_Differential(t *testing.T) {
	corpus := map[string]bool{
		"fix/one":             true,
		"origin/main":         true,
		"main~1":              true,
		"HEAD@{2}":            true,
		"ü-branch":            true,
		"日本語":                 true,
		"":                    false,
		"-f":                  false,
		"--detach":            false,
		"main; rm -rf /":      false, // space
		"main\t--force":       false, // tab
		"a b":                 false,
		"main\n":              false,
		"carriage\rreturn":    false,
		"del\x7fchar":         false,
		"ctrl\x01char":        false,
		"vertical\x0btab":     false,
		"-":                   false,
		"trailing-dash-":      true,
		"sha-like 0123456789": false, // space inside
		"feature/x~y^z{a}b=c": true,
	}
	for ref, want := range corpus {
		assert.Equal(t, want, refValidRef(ref), "reference disagrees on corpus entry %q", ref)
		assert.Equal(t, want, ValidRef(ref), "ValidRef(%q)", ref)
	}
}

// oracle: regexp character class — OneLine's whitespace flattening must
// equal TrimSpace plus collapsing runs with the regexp \s class, a
// different stdlib mechanism than strings.Fields.
func TestOneLine_MatchesRegexpReference_Differential(t *testing.T) {
	whitespaceRun := regexp.MustCompile(`\s+`)
	corpus := []string{
		"single",
		"",
		" \t\n ",
		"  fatal: not a git repository  ",
		"error: branch\r\ndetails\n   here\n",
		"a\r\n\r\nb",
	}
	for _, s := range corpus {
		assert.Equal(t, strings.TrimSpace(whitespaceRun.ReplaceAllString(s, " ")), OneLine(s), "input %q", s)
	}
}

// refTruncate re-derives Truncate with manual rune decode loops instead of
// a rune slice: collect every rune, and when the budget is exceeded, copy
// the first n runes one by one before trimming.
func refTruncate(s string, n int) string {
	var all []rune
	for _, r := range s {
		all = append(all, r)
	}
	if len(all) <= n {
		return s
	}
	var b strings.Builder
	for _, r := range all[:n] {
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String()) + "…"
}

// oracle: naive reference implementation — rune-by-rune prefix copy versus
// Truncate's rune-slice cut, over ASCII, CJK, and padding edge cases.
func TestTruncate_MatchesRuneLoopReference_Differential(t *testing.T) {
	cases := []struct {
		s string
		n int
	}{
		{"hello", 10},
		{"hello", 5},
		{"hello", 4},
		{"  padded  ", 7},
		{"你好世界", 3},
		{"你好世界", 10},
		{"abc", 0},
		{"mixed ascii内容", 7},
	}
	for _, tc := range cases {
		assert.Equal(t, refTruncate(tc.s, tc.n), Truncate(tc.s, tc.n), "Truncate(%q, %d)", tc.s, tc.n)
	}
}
