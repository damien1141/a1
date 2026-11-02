package kamitool

import (
        "encoding/json"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// TestDocumentTool_Definition verifies the unified tool schema is simple:
// 3 required fields (type, title, content), 2 optional (format, output).
func TestDocumentTool_Definition(t *testing.T) {
        tool := DocumentTool()
        assert.Equal(t, "document", tool.Definition.Name)
        assert.Contains(t, tool.Definition.Description, "markdown")
        assert.Contains(t, tool.Definition.Params.Required, "type")
        assert.Contains(t, tool.Definition.Params.Required, "title")
        assert.Contains(t, tool.Definition.Params.Required, "content")
        assert.NotNil(t, tool.Run)

        // The description must list all doc types.
        desc := tool.Definition.Description
        for _, dt := range []string{"proposal", "resume", "onepager", "essay_mla", "essay_chicago"} {
                assert.Contains(t, desc, dt, "description must list type %q", dt)
        }
}

// TestDocumentTool_MissingType verifies the required-field guard.
func TestDocumentTool_MissingType(t *testing.T) {
        raw, _ := json.Marshal(documentInput{Title: "x", Content: "# H\nbody"})
        _, err := runDocument(nil, raw)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "type is required")
}

// TestDocumentTool_MissingTitle verifies the required-field guard.
func TestDocumentTool_MissingTitle(t *testing.T) {
        raw, _ := json.Marshal(documentInput{Type: "proposal", Content: "# H\nbody"})
        _, err := runDocument(nil, raw)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "title is required")
}

// TestDocumentTool_MissingContent verifies the required-field guard.
func TestDocumentTool_MissingContent(t *testing.T) {
        raw, _ := json.Marshal(documentInput{Type: "proposal", Title: "x"})
        _, err := runDocument(nil, raw)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "content is required")
}

// TestParseMarkdownContent_BasicHeading verifies the markdown parser
// extracts headings, body, and bullets from a simple markdown string.
func TestParseMarkdownContent_BasicHeading(t *testing.T) {
        content := `# Executive Summary
This is the summary paragraph.

# Timeline
- Phase 1: Q1
- Phase 2: Q2`

        sections := parseMarkdownContent(content)
        require.Len(t, sections, 2)

        assert.Equal(t, "Executive Summary", sections[0].Heading)
        assert.Equal(t, "This is the summary paragraph.", sections[0].Body)
        assert.Empty(t, sections[0].Bullets)

        assert.Equal(t, "Timeline", sections[1].Heading)
        assert.Empty(t, sections[1].Body)
        assert.Equal(t, []string{"Phase 1: Q1", "Phase 2: Q2"}, sections[1].Bullets)
}

// TestParseMarkdownContent_Subheading verifies ## subheadings.
func TestParseMarkdownContent_Subheading(t *testing.T) {
        content := `# Main Section
Intro text.

## Subsection
Sub text.`

        sections := parseMarkdownContent(content)
        require.Len(t, sections, 2)

        assert.Equal(t, "Main Section", sections[0].Heading)
        assert.Equal(t, 1, sections[0].Level)
        assert.Equal(t, "Intro text.", sections[0].Body)

        assert.Equal(t, "Subsection", sections[1].Heading)
        assert.Equal(t, 2, sections[1].Level)
        assert.Equal(t, "Sub text.", sections[1].Body)
}

// TestParseMarkdownContent_NoHeadings verifies content without headings
// gets wrapped in an implicit "Overview" section.
func TestParseMarkdownContent_NoHeadings(t *testing.T) {
        content := "Just some plain text\nwith two lines."

        sections := parseMarkdownContent(content)
        require.Len(t, sections, 1)
        assert.Equal(t, "Overview", sections[0].Heading)
        assert.Contains(t, sections[0].Body, "Just some plain text")
        assert.Contains(t, sections[0].Body, "with two lines")
}

// TestParseMarkdownContent_MultiParagraph verifies multi-paragraph body
// text is joined with double newlines.
func TestParseMarkdownContent_MultiParagraph(t *testing.T) {
        content := `# Section

First paragraph.

Second paragraph.

Third paragraph.`

        sections := parseMarkdownContent(content)
        require.Len(t, sections, 1)
        assert.Contains(t, sections[0].Body, "First paragraph.")
        assert.Contains(t, sections[0].Body, "Second paragraph.")
}

// TestRenderKamiHTML verifies the Kami HTML template includes palette
// colors and all content.
func TestRenderKamiHTML(t *testing.T) {
        sections := []docSection{
                {Heading: "Summary", Body: "The plan is good.", Bullets: []string{"Fast", "Cheap"}},
        }
        html := renderKamiHTML("proposal", "Project Apollo", sections)

        assert.Contains(t, html, kamiBackground)
        assert.Contains(t, html, kamiInk)
        assert.Contains(t, html, fontPrimary)
        assert.Contains(t, html, "Project Apollo")
        assert.Contains(t, html, "Proposal")
        assert.Contains(t, html, "Summary")
        assert.Contains(t, html, "The plan is good.")
        assert.Contains(t, html, "Fast")
}

// TestRenderEssayHTML verifies the essay HTML uses white background,
// Times New Roman, and 12pt double-spaced.
func TestRenderEssayHTML(t *testing.T) {
        sections := []docSection{
                {Heading: "Introduction", Body: "This paper examines..."},
        }
        html := renderEssayHTML("essay_mla", "The Impact of AI", sections)

        // Essays must NOT use Kami palette.
        assert.NotContains(t, html, kamiBackground)
        assert.NotContains(t, html, kamiInk)

        // Essays must use white background and serif font.
        assert.Contains(t, html, "#ffffff")
        assert.Contains(t, html, "Times New Roman")
        assert.Contains(t, html, "12pt")
        assert.Contains(t, html, "2.0")

        // MLA must have the 4-line header.
        assert.Contains(t, html, "Student Name")
        assert.Contains(t, html, "Instructor Name")

        // Content must be present.
        assert.Contains(t, html, "The Impact of AI")
        assert.Contains(t, html, "This paper examines...")
}

// TestRenderKamiDOCX verifies the DOCX file is created and non-empty.
func TestRenderKamiDOCX(t *testing.T) {
        sections := []docSection{
                {Heading: "Section 1", Body: "Body text here."},
        }
        path := filepath.Join(t.TempDir(), "test.docx")
        err := renderKamiDOCX("proposal", "Test Title", sections, path)
        require.NoError(t, err)
        info, err := os.Stat(path)
        require.NoError(t, err)
        assert.Greater(t, info.Size(), int64(0))
}

// TestRenderEssayDOCX verifies the essay DOCX file is created.
func TestRenderEssayDOCX(t *testing.T) {
        sections := []docSection{
                {Heading: "Introduction", Body: "This paper examines the topic."},
        }
        path := filepath.Join(t.TempDir(), "essay.docx")
        err := renderEssayDOCX("essay_mla", "Test Essay", sections, path)
        require.NoError(t, err)
        info, err := os.Stat(path)
        require.NoError(t, err)
        assert.Greater(t, info.Size(), int64(0))
}

// TestResolveOutput verifies the output path resolution.
func TestResolveOutput(t *testing.T) {
        p, err := resolveOutput("/tmp/test-doc.pdf", "proposal", "pdf")
        require.NoError(t, err)
        assert.Equal(t, "/tmp/test-doc.pdf", p)

        p, err = resolveOutput("", "proposal", "pdf")
        require.NoError(t, err)
        assert.True(t, strings.HasSuffix(p, "proposal.pdf"))
}

// TestHTMLEscape verifies the HTML escaping helper.
func TestHTMLEscape(t *testing.T) {
        assert.Equal(t, "&amp;", htmlEscape("&"))
        assert.Equal(t, "&lt;", htmlEscape("<"))
        assert.Equal(t, "&gt;", htmlEscape(">"))
        assert.Equal(t, "&quot;", htmlEscape(`"`))
        assert.Equal(t, "&#39;", htmlEscape("'"))
}

// TestKamiPalette verifies the palette constants.
func TestKamiPalette(t *testing.T) {
        colors := []string{kamiBackground, kamiInk, kamiText, kamiTextMuted, kamiBorder, kamiWhite}
        for _, c := range colors {
                assert.True(t, strings.HasPrefix(c, "#"))
                assert.Equal(t, 7, len(c))
        }
}

// TestDocumentDetailFromArgs verifies the detail line.
func TestDocumentDetailFromArgs(t *testing.T) {
        tool := DocumentTool()
        raw, _ := json.Marshal(documentInput{Type: "proposal", Title: "Apollo"})
        d := tool.DetailFromArgs(raw)
        assert.Equal(t, `document proposal: "Apollo"`, d)

        raw, _ = json.Marshal(documentInput{Type: "resume"})
        d = tool.DetailFromArgs(raw)
        assert.Equal(t, "document resume", d)
}

// TestIsEssayType verifies the essay type detector.
func TestIsEssayType(t *testing.T) {
        assert.True(t, isEssayType("essay_mla"))
        assert.True(t, isEssayType("essay_chicago"))
        assert.False(t, isEssayType("proposal"))
        assert.False(t, isEssayType("resume"))
}

// TestDocTypeLabel verifies the human-readable labels.
func TestDocTypeLabel(t *testing.T) {
        assert.Equal(t, "Proposal", docTypeLabel("proposal"))
        assert.Equal(t, "Resume", docTypeLabel("resume"))
        assert.Equal(t, "One-Pager", docTypeLabel("onepager"))
        assert.Equal(t, "Equity Report", docTypeLabel("equity_report"))
}
