package coveragetool

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// differentialProfile is a hand-written cover.out fixture: interleaved
// per-file blocks, varied numStmt weights, a zero-statement file, blank and
// malformed lines that must be skipped silently.
const differentialProfile = `mode: count

github.com/example/alpha.go:10.2,15.1 1 5
github.com/example/beta.go:3.2,6.1 3 1

github.com/example/alpha.go:20.2,25.1 2 0
garbage
github.com/example/alpha.go:30.2,38.1 7 3
github.com/example/gamma.go:5.2,9.1 5 2
github.com/example/beta.go:10.2,13.1 3 1
github.com/example/delta.go:1.1,2.1 0 4
github.com/example/gamma.go:20.2,21.1 1 0
github.com/example/alpha.go:40.2,41.1 1
github.com/example/alpha.go:50.a,51.1 1 1
`

// naiveCoverParse re-derives the per-file percentages with strings.Fields
// splitting and a LastIndex colon cut — no regexp, no shared code with the
// parser under test. Percentages are plain covered/total*100 arithmetic.
func naiveCoverParse(t *testing.T, profile string) []fileCoverage {
	t.Helper()
	type coverTally struct{ total, covered float64 }
	perFile := map[string]*coverTally{}
	var order []string
	for _, line := range strings.Split(profile, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		loc := fields[0]
		colon := strings.LastIndex(loc, ":")
		if colon <= 0 {
			continue
		}
		if !looksLikePositions(loc[colon+1:]) {
			continue
		}
		numStmt, err1 := strconv.ParseFloat(fields[1], 64)
		count, err2 := strconv.ParseFloat(fields[2], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		file := loc[:colon]
		t, ok := perFile[file]
		if !ok {
			t = &coverTally{}
			perFile[file] = t
			order = append(order, file)
		}
		t.total += numStmt
		if count > 0 {
			t.covered += numStmt
		}
	}

	var out []fileCoverage
	for _, f := range order {
		pct := 0.0
		if perFile[f].total > 0 {
			pct = (perFile[f].covered / perFile[f].total) * 100
		}
		out = append(out, fileCoverage{path: f, percent: pct})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].percent < out[j].percent })
	return out
}

// looksLikePositions validates the "12.34,56.78" span after the last colon.
func looksLikePositions(s string) bool {
	startEnd := strings.Split(s, ",")
	if len(startEnd) != 2 {
		return false
	}
	for _, part := range startEnd {
		lineCol := strings.Split(part, ".")
		if len(lineCol) != 2 {
			return false
		}
		for _, digits := range lineCol {
			if _, err := strconv.Atoi(digits); err != nil {
				return false
			}
		}
	}
	return true
}

func writeProfile(t *testing.T, profile string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cover.out")
	require.NoError(t, os.WriteFile(path, []byte(profile), 0o644))
	return path
}

// oracle: naive reference implementation in this test — an independent
// coverprofile parser (strings.Fields + LastIndex colon cut instead of the
// production regexp) computing per-file percentages with plain arithmetic,
// compared on a hand-written fixture including malformed lines, interleaved
// blocks, and a zero-statement file.
func TestParseCoverageProfile_Differential(t *testing.T) {
	profilePath := writeProfile(t, differentialProfile)

	results, err := parseCoverageProfile(profilePath, "/tmp", 100)
	require.NoError(t, err)

	want := naiveCoverParse(t, differentialProfile)
	require.Len(t, want, 4, "fixture must yield four files")
	require.NotEmpty(t, results)

	assert.Len(t, results, len(want))
	for i := range want {
		assert.Equal(t, want[i].path, results[i].path, "file #%d (ascending percent order)", i)
		assert.Equal(t, fmt.Sprintf("%.1f%%", want[i].percent), fmt.Sprintf("%.1f%%", results[i].percent),
			"percent for %s", want[i].path)
	}

	// Rendered lines agree with the naive rendering of the same results.
	var naiveRender strings.Builder
	for _, c := range results {
		naiveRender.WriteString(fmt.Sprintf("%.1f%%\t%s\n", c.percent, c.path))
	}
	assert.Equal(t, naiveRender.String(), renderCoverageResults(t.Context(), results))

	// A tight limit keeps the lowest-percentage files only.
	capped, err := parseCoverageProfile(profilePath, "/tmp", 2)
	require.NoError(t, err)
	require.Len(t, capped, 2)
	assert.Equal(t, want[0].path, capped[0].path)
	assert.Equal(t, want[1].path, capped[1].path)
}
