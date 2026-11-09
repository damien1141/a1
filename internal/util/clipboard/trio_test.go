package clipboard

import (
	"bufio"
	"mime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// normLines treats nil and empty as the same value: splitLines returns nil
// for empty input but an empty non-nil slice when every line is blank, a
// distinction no caller observes.
func normLines(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	return ss
}

// oracle: stdlib mime.ParseMediaType — the hand-rolled base-type extraction
// (cut at ";", trim, lower) must agree with the stdlib parser's media type
// for every realistic clipboard MIME token.
func TestBaseMimeType_MatchesMimeParseMediaType_Differential(t *testing.T) {
	corpus := []string{
		"image/png",
		"image/png; charset=binary",
		"IMAGE/JPEG",
		" image/webp ",
		"text/plain;charset=utf-8",
		"text/plain; charset=UTF-8",
		"image/gif",
		"image/PNG",
		"image/bmp",
		"application/octet-stream",
		"image/webp; charset=binary",
	}
	for _, s := range corpus {
		want, _, err := mime.ParseMediaType(s)
		require.NoError(t, err, "corpus entry %q must be parseable", s)
		assert.Equal(t, want, baseMimeType(s), "base type of %q", s)
	}
}

// refSplitLines re-derives splitLines with bufio.Scanner instead of
// ReplaceAll+Split: ScanLines already drops the CR of a CRLF pair, then each
// token is trimmed and blank results are dropped.
func refSplitLines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// oracle: naive reference implementation — line splitting re-derived with
// bufio.Scanner (a different stdlib mechanism for CRLF-normalized lines)
// instead of the production ReplaceAll+Split.
func TestSplitLines_MatchesScannerReference_Differential(t *testing.T) {
	corpus := []string{
		"image/png\r\ntext/plain\n",
		"image/png\r\n\r\ntext/plain\n",
		"",
		"\n\n\n",
		"  \n\t\n",
		"single",
		"\r",
		"a\r\rb",
		"  spaced  \n\n trimmed \n",
		"image/png\ntext/plain;charset=utf-8\n",
		"trailing\r\n",
	}
	for _, s := range corpus {
		assert.Equal(t, normLines(refSplitLines(s)), normLines(splitLines(s)), "input %q", s)
	}
}

// refPreferredImageMime ranks candidates instead of scanning: a preferred
// base type scores by the golden rank below, any other image/* type scores
// behind all preferred ones (earlier candidate wins ties), everything else
// is out. Lowest score wins.
func refPreferredImageMime(types []string) string {
	prefRank := map[string]int{"image/png": 0, "image/jpeg": 1, "image/webp": 2, "image/gif": 3}
	const imageFallback = 100
	const out = 200
	best, bestScore := "", out
	for i, raw := range types {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		base, _, err := mime.ParseMediaType(t)
		if err != nil {
			continue // X11 targets like UTF8_STRING are never image types
		}
		score := out
		if rank, ok := prefRank[base]; ok {
			score = rank
		} else if strings.HasPrefix(base, "image/") {
			score = imageFallback + i
		}
		if score < bestScore {
			best, bestScore = t, score
		}
	}
	return best
}

// oracle: naive rank-based reference plus golden preference order — the
// winner must match selectPreferredImageMimeType for X11/Wayland target
// lists, including case, parameters, and non-image noise.
func TestSelectPreferredImageMimeType_MatchesRankReference_Differential(t *testing.T) {
	cases := [][]string{
		{"text/plain", "image/webp", "image/png"},
		{"image/jpeg", "image/png"},
		{"image/svg+xml", "application/pdf"},
		{"UTF8_STRING", "STRING", "TARGETS"},
		{" image/gif ", "IMAGE/PNG"},
		{"text/plain;charset=utf-8"},
		{"image/bmp", "image/tiff"},
		{"", "  ", "image/webp"},
		{"image/png", "image/png; charset=binary"},
	}
	for _, types := range cases {
		assert.Equal(t, refPreferredImageMime(types), selectPreferredImageMimeType(types),
			"targets %v", types)
	}
}

// oracle: hand-written golden set — exactly the four raster types vision
// APIs accept are supported, however they are spelled (parameters, case,
// whitespace).
func TestIsSupportedImageMimeType_GoldenSet_Differential(t *testing.T) {
	supported := []string{"image/png", "image/jpeg", "image/webp", "image/gif"}
	unsupported := []string{"image/bmp", "image/svg+xml", "image/tiff", "text/plain", "application/pdf", ""}
	for _, s := range supported {
		assert.True(t, isSupportedImageMimeType(s), "%s must be supported", s)
		assert.True(t, isSupportedImageMimeType(strings.ToUpper(s+"; charset=binary")),
			"spelling must not matter: %s", s)
	}
	for _, s := range unsupported {
		assert.False(t, isSupportedImageMimeType(s), "%s must not be supported", s)
	}
}
