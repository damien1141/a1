package kamitool

import (
	"bufio"
	"encoding/csv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"html"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

var (
	refHeading1 = regexp.MustCompile(`^# (.*)$`)
	refHeading2 = regexp.MustCompile(`^## (.*)$`)
	refBullet   = regexp.MustCompile(`^[-*] (.*)$`)
	refBlank    = regexp.MustCompile(`^\s*$`)
)

// refParseMarkdown re-derives parseMarkdownContent with a different
// mechanism: regexp line classification instead of bytes.HasPrefix checks,
// and paragraph accumulation resolved through the section slice rather than a
// floating current pointer.
func refParseMarkdown(content string) []docSection {
	var sections []docSection
	var bodyLines []string

	ensure := func() *docSection {
		if len(sections) == 0 {
			sections = append(sections, docSection{Heading: "Overview", Level: 1})
		}
		return &sections[len(sections)-1]
	}
	flush := func() {
		if len(sections) == 0 || len(bodyLines) == 0 {
			return
		}
		text := strings.TrimSpace(strings.Join(bodyLines, " "))
		bodyLines = nil
		if text == "" {
			return
		}
		if cur := &sections[len(sections)-1]; cur.Body != "" {
			cur.Body += "\n\n" + text
		} else {
			cur.Body = text
		}
	}

	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case refHeading2.MatchString(line):
			flush()
			sections = append(sections, docSection{
				Heading: strings.TrimSpace(refHeading2.FindStringSubmatch(line)[1]), Level: 2,
			})
		case refHeading1.MatchString(line):
			flush()
			sections = append(sections, docSection{
				Heading: strings.TrimSpace(refHeading1.FindStringSubmatch(line)[1]), Level: 1,
			})
		case refBullet.MatchString(line):
			flush()
			cur := ensure()
			cur.Bullets = append(cur.Bullets, strings.TrimSpace(refBullet.FindStringSubmatch(line)[1]))
		case refBlank.MatchString(line):
			flush()
		default:
			ensure()
			bodyLines = append(bodyLines, line)
		}
	}
	flush()
	return sections
}

// oracle: naive reference implementation in this test — a regexp-based line
// classifier over normal, edge, and hostile markdown (missing space after #,
// ###, indented headings, both bullet markers, CRLF endings, leading prose)
// must produce the same sections as the prefix-checking parser.
func TestParseMarkdownContent_MatchesRegexOracle_Differential(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"heading and two paragraphs", "# A\npara one\npara two\n\nsecond para\n"},
		{"both bullet markers", "- dash bullet\n* star bullet\n"},
		{"prose before any heading", "intro line\n- bullet\n"},
		{"bullet then prose continues", "- b1\ntext after\n"},
		{"subheading levels", "# Top\nintro\n## Sub\nsub text\n"},
		{"hash without space is body", "#nope\n"},
		{"triple hash is body", "### H3\n"},
		{"indented heading", "   # Indented\n"},
		{"empty content", ""},
		{"crlf endings", "# Win\r\nbody line\r\n"},
		{"heading trailing spaces", "# Heading   \n"},
		{"blank separated bullets", "- spaced   \n\n- next\n"},
		{"lone hash", "# \n"},
		{"lone dash", "- \n"},
		{"only headings", "# One\n# Two\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, refParseMarkdown(tc.content), parseMarkdownContent(tc.content))
		})
	}
}

const csvFixtureContent = `# Rollout

Ship the widget line.

- quote: "hello, world"
- costs, computed daily

| Item | Qty |
|------|-----|
| widget, small | 2 |
| gear | 5 |
`

// oracle: hand-written golden records decoded from the written file with the
// stdlib encoding/csv reader — a genuinely distinct code path from the
// csv.Writer the renderer uses. Commas and quotes in bullets, title, and
// table cells must survive the quoting round-trip, and the pipe table must
// come back as real grid rows.
func TestRenderKamiCSV_MatchesGoldenRows_Differential(t *testing.T) {
	sections := parseMarkdownContent(csvFixtureContent)
	require.Len(t, sections, 1)

	path := filepath.Join(t.TempDir(), "rollout.csv")
	require.NoError(t, renderKamiCSV(
		docProposal, `Q3 "Launch", Plan`, sections, csvFixtureContent, path))

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	got, err := reader.ReadAll()
	require.NoError(t, err)

	// csv.Reader skips the blank separator rows the writer emits, so the
	// golden records are the non-blank rows in write order.
	want := [][]string{
		{"PROPOSAL"},
		{`Q3 "Launch", Plan`},
		{"Rollout"},
		{"Ship the widget line."},
		// The pipe table is also prose to the section parser; it rides along
		// as one space-joined paragraph before the real grid rows below.
		{"| Item | Qty | |------|-----| | widget, small | 2 | | gear | 5 |"},
		{`• quote: "hello, world"`},
		{"• costs, computed daily"},
		{"Item", "Qty"},
		{"widget, small", "2"},
		{"gear", "5"},
	}
	assert.Equal(t, want, got)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(raw), "\n\n"),
		"one blank row after the title block and one before the table")
}

// oracle: stdlib html.EscapeString / html.UnescapeString. For quote-free
// inputs the escape must match the stdlib escaper byte for byte; for every
// input the stdlib unescaper must recover the original string (both &quot;
// and &#34; decode back to ").
func TestHTMLEscape_StdlibEquivalence_Differential(t *testing.T) {
	inputs := []string{
		`plain text`,
		`a & b`,
		`5 < 6 > 4`,
		`<script>alert("x")</script>`,
		`it's a "quoted" & escaped <thing>`,
		`Tom & Jerry's "cat" <dog>`,
		`no entities here`,
		`&#39; already escaped &amp;`,
		``,
	}
	for _, s := range inputs {
		escaped := htmlEscape(s)
		assert.Equal(t, s, html.UnescapeString(escaped), "round-trip of %q", s)
		if !strings.Contains(s, `"`) {
			assert.Equal(t, html.EscapeString(s), escaped, "stdlib equivalence for %q", s)
		}
	}
}
