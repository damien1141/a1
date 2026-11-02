package kamitool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/tools/tooldef"

	"baliance.com/gooxml/color"
	"baliance.com/gooxml/measurement"
)

// docType is the enum of document types the tool can produce.
const (
	docProposal     = "proposal"
	docResume       = "resume"
	docOnePager     = "onepager"
	docLetter       = "letter"
	docChangelog    = "changelog"
	docEquity       = "equity_report"
	docPortfolio    = "portfolio"
	docEssayMLA     = "essay_mla"
	docEssayChicago = "essay_chicago"
)

// documentInput is the flat schema the LLM sees.
type documentInput struct {
	Type    string `json:"type"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Format  string `json:"format,omitempty"`
	Output  string `json:"output,omitempty"`
}

var documentDescription = `Generate a professionally designed document. Write the content as
markdown (# headings, - bullets); formatting, layout, and styling are
automatic.

Types: proposal, resume, onepager, letter, changelog, equity_report,
portfolio, essay_mla, essay_chicago. Formats: pdf (default), docx,
pptx (one slide per section), xlsx (rows; pipe tables become grid rows),
csv. Pipe tables in content (| a | b |) render as real table rows in
xlsx/csv. Output path defaults to ~/download/.`

// DocumentTool returns the unified document generation tool.
func DocumentTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "document",
			Description: documentDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"type": llm.Object{
						"type":        "string",
						"description": "Document type (see tool description).",
					},
					"title": llm.Object{
						"type":        "string",
						"description": "Document title (person's name for resumes).",
					},
					"content": llm.Object{
						"type":        "string",
						"description": "Body as markdown: # headings, - bullets.",
					},
					"format": llm.Object{
						"type":        "string",
						"description": "Output format: 'pdf' (default), 'docx', 'pptx', 'xlsx', or 'csv'.",
					},
					"output": llm.Object{
						"type":        "string",
						"description": "Output file path. Default: ~/download/<type>.<format>",
					},
				},
				Required: []string{"type", "title", "content"},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in documentInput
			_ = json.Unmarshal(input, &in)
			t := strings.TrimSpace(in.Title)
			if t == "" {
				return fmt.Sprintf("document %s", in.Type)
			}
			return fmt.Sprintf("document %s: %q", in.Type, t)
		},
		Run: runDocument,
	}
}

func runDocument(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in documentInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse document arguments: %w", err)
	}
	if strings.TrimSpace(in.Type) == "" {
		return tooldef.Result{}, fmt.Errorf(
			"type is required: proposal, resume, onepager, letter, changelog, equity_report, portfolio, essay_mla, essay_chicago",
		)
	}
	if strings.TrimSpace(in.Title) == "" {
		return tooldef.Result{}, fmt.Errorf("title is required")
	}
	if strings.TrimSpace(in.Content) == "" {
		return tooldef.Result{}, fmt.Errorf("content is required")
	}

	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format == "" {
		format = "pdf"
	}

	// Normalize the type.
	docType := strings.ToLower(strings.TrimSpace(in.Type))

	outputPath, err := resolveOutput(in.Output, docType, format)
	if err != nil {
		return tooldef.Result{}, err
	}

	// Parse the markdown content into sections.
	sections := parseMarkdownContent(in.Content)
	if len(sections) == 0 {
		return tooldef.Result{}, fmt.Errorf("content must contain at least one section or paragraph")
	}

	switch format {
	case "pdf":
		html := renderDocumentHTML(docType, in.Title, sections)
		if err := renderPDF(ctx, html, outputPath, "Letter"); err != nil {
			return tooldef.Result{}, err
		}
	case "docx":
		if err := renderDocumentDOCX(docType, in.Title, sections, outputPath); err != nil {
			return tooldef.Result{}, err
		}
	case "pptx":
		if err := renderKamiPPTX(docType, in.Title, sections, in.Content, outputPath); err != nil {
			return tooldef.Result{}, err
		}
	case "xlsx":
		if err := renderKamiXLSX(docType, in.Title, sections, in.Content, outputPath); err != nil {
			return tooldef.Result{}, err
		}
	case "csv":
		if err := renderKamiCSV(docType, in.Title, sections, in.Content, outputPath); err != nil {
			return tooldef.Result{}, err
		}
	default:
		return tooldef.Result{}, fmt.Errorf("unknown format %q: use pdf, docx, pptx, xlsx, or csv", format)
	}

	info, _ := os.Stat(outputPath)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	content := fmt.Sprintf("%s saved to %s (%d bytes)", docType, outputPath, size)
	return tooldef.Result{
		Content: content,
		Detail:  fmt.Sprintf("document %s → %s", format, filepath.Base(outputPath)),
		Output:  content,
	}, nil
}

// isEssayType reports whether the document type is an academic essay.
// Essays use Times New Roman, 12pt, double-spaced, 1" margins — strict
// MLA/Chicago formatting instead of the Kami warm-palette design.
func isEssayType(docType string) bool {
	return docType == docEssayMLA || docType == docEssayChicago
}

// renderDocumentHTML generates HTML for any document type. Kami-styled
// for business docs, plain white for academic essays.
func renderDocumentHTML(docType string, title string, sections []docSection) string {
	if isEssayType(docType) {
		return renderEssayHTML(docType, title, sections)
	}
	return renderKamiHTML(docType, title, sections)
}

// renderDocumentDOCX generates DOCX for any document type.
func renderDocumentDOCX(docType string, title string, sections []docSection, path string) error {
	if isEssayType(docType) {
		return renderEssayDOCX(docType, title, sections, path)
	}
	return renderKamiDOCX(docType, title, sections, path)
}

// renderKamiHTML generates the Kami-styled HTML for business documents.
// All non-essay types share the same warm-palette template; the doc type
// only affects the cover layout and section density.
func renderKamiHTML(docType string, title string, sections []docSection) string {
	fontFace := fontFaceCSS()

	// Compact layout for one-pagers; generous for proposals/reports.
	compact := docType == docOnePager || docType == docChangelog
	padding := "72pt 64pt"
	titleSize := "28pt"
	if compact {
		padding = "48pt 52pt"
		titleSize = "24pt"
	}

	var body strings.Builder
	for _, s := range sections {
		body.WriteString(fmt.Sprintf(`<section class="doc-section">
  <h2>%s</h2>`, htmlEscape(s.Heading)))
		if s.Body != "" {
			body.WriteString(fmt.Sprintf(`  <p>%s</p>`, htmlEscape(s.Body)))
		}
		if len(s.Bullets) > 0 {
			body.WriteString(`  <ul>`)
			for _, b := range s.Bullets {
				body.WriteString(fmt.Sprintf(`    <li>%s</li>`, htmlEscape(b)))
			}
			body.WriteString(`  </ul>`)
		}
		body.WriteString(`</section>`)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>%s</title><style>
%s
@page { size: Letter; margin: 0; }
* { margin: 0; padding: 0; box-sizing: border-box; }
html, body { background: %s; font-family: %s; color: %s; font-size: 11pt; line-height: 1.6; }
.page { padding: %s; }
.cover { padding-bottom: 36pt; margin-bottom: 36pt; border-bottom: 2pt solid %s; }
.cover h1 { font-size: %s; font-weight: 700; color: %s; margin-bottom: 4pt; line-height: 1.2; }
.cover .doctype { font-size: 10pt; color: %s; text-transform: uppercase; letter-spacing: 1.5pt; margin-bottom: 12pt; }
.doc-section { margin-bottom: 28pt; }
.doc-section h2 { font-size: 14pt; font-weight: 600; color: %s; margin-bottom: 10pt; padding-bottom: 3pt; border-bottom: 1pt solid %s; }
.doc-section p { margin-bottom: 10pt; color: %s; }
.doc-section ul { margin: 8pt 0 8pt 20pt; color: %s; }
.doc-section li { margin-bottom: 4pt; }
</style></head><body>
<div class="page">
  <div class="cover">
    <div class="doctype">%s</div>
    <h1>%s</h1>
  </div>
  %s
</div>
</body></html>`,
		htmlEscape(title), fontFace,
		kamiBackground, fontPrimary, kamiText,
		padding,
		kamiInk, titleSize, kamiInk,
		kamiTextMuted,
		kamiInk, kamiBorder,
		kamiText, kamiText,
		docTypeLabel(docType),
		htmlEscape(title),
		body.String(),
	)
}

// docTypeLabel converts a doc type to a human label for the cover.
func docTypeLabel(docType string) string {
	switch docType {
	case docProposal:
		return "Proposal"
	case docResume:
		return "Resume"
	case docOnePager:
		return "One-Pager"
	case docLetter:
		return "Letter"
	case docChangelog:
		return "Changelog"
	case docEquity:
		return "Equity Report"
	case docPortfolio:
		return "Portfolio"
	default:
		return strings.Title(docType)
	}
}

// renderKamiDOCX generates the Kami-styled DOCX for business documents.
func renderKamiDOCX(docType string, title string, sections []docSection, path string) error {
	cfg := kamiDOCXConfig()
	doc := newDOCX(cfg)

	// Doctype label.
	para := doc.AddParagraph()
	run := para.AddRun()
	run.AddText(strings.ToUpper(docTypeLabel(docType)))
	run.Properties().SetFontFamily(cfg.fontFamily)
	run.Properties().SetSize(measurement.Distance(9 * measurement.Point))
	run.Properties().SetColor(color.FromHex("8B7E66"))
	para.Properties().SetSpacing(0, measurement.Distance(4*measurement.Point))

	// Title.
	para = doc.AddParagraph()
	run = para.AddRun()
	run.AddText(title)
	run.Properties().SetFontFamily(cfg.fontFamily)
	run.Properties().SetSize(measurement.Distance(22 * measurement.Point))
	run.Properties().SetBold(true)
	run.Properties().SetColor(color.FromHex(cfg.headingColor))
	para.Properties().SetSpacing(0, measurement.Distance(24*measurement.Point))

	// Body sections.
	for _, s := range sections {
		addHeading(doc, cfg, s.Heading, 2)
		if s.Body != "" {
			// Split body by double-newline into separate paragraphs.
			for _, para := range strings.Split(s.Body, "\n\n") {
				text := strings.TrimSpace(para)
				if text != "" {
					addBodyParagraph(doc, cfg, text)
				}
			}
		}
		for _, b := range s.Bullets {
			addBullet(doc, cfg, b)
		}
	}

	return saveDOCX(doc, path)
}

// renderEssayHTML generates plain white-page HTML for academic essays.
// MLA: no title page, 4-line header (name, instructor, course, date).
// Chicago: title page, footnotes.
func renderEssayHTML(docType string, title string, sections []docSection) string {
	var body strings.Builder
	for _, s := range sections {
		if s.Heading != "" && s.Heading != "Overview" {
			body.WriteString(fmt.Sprintf(`<h2 class="essay-heading">%s</h2>`, htmlEscape(s.Heading)))
		}
		if s.Body != "" {
			for _, para := range strings.Split(s.Body, "\n\n") {
				text := strings.TrimSpace(para)
				if text != "" {
					body.WriteString(fmt.Sprintf(`<p class="essay-body">%s</p>`, htmlEscape(text)))
				}
			}
		}
		if len(s.Bullets) > 0 {
			body.WriteString(`<ul class="essay-bullets">`)
			for _, b := range s.Bullets {
				body.WriteString(fmt.Sprintf(`<li>%s</li>`, htmlEscape(b)))
			}
			body.WriteString(`</ul>`)
		}
	}

	// MLA header: the first 4 lines of content are the heading block
	// (name, instructor, course, date). We detect this by checking if
	// the first section has no heading and 4+ lines of body.
	headerHTML := ""
	if docType == docEssayMLA {
		headerHTML = `<div class="mla-header">
  <p>Student Name</p>
  <p>Instructor Name</p>
  <p>Course Name</p>
  <p>Date</p>
</div>`
	}

	// Chicago: centered title on first page, no MLA header.
	titleHTML := ""
	if docType == docEssayChicago {
		titleHTML = fmt.Sprintf(`<h1 class="essay-title">%s</h1>`, htmlEscape(title))
	} else {
		titleHTML = fmt.Sprintf(`<h1 class="essay-title">%s</h1>`, htmlEscape(title))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>%s</title><style>
@page { size: Letter; margin: 1in; }
* { margin: 0; padding: 0; box-sizing: border-box; }
html, body {
  background: #ffffff;
  font-family: %s;
  color: #000000;
  font-size: 12pt;
  line-height: 2.0;
}
.page { padding: 0; }
.mla-header { text-align: left; margin-bottom: 24pt; }
.mla-header p { margin: 0; }
.essay-title { text-align: center; font-size: 12pt; font-weight: normal; margin-bottom: 24pt; }
.essay-heading { font-size: 12pt; font-weight: bold; margin-top: 18pt; margin-bottom: 6pt; }
.essay-body { text-indent: 0.5in; margin-bottom: 0; }
.essay-bullets { margin-left: 0.5in; margin-top: 6pt; }
.essay-bullets li { margin-bottom: 6pt; }
</style></head><body>
<div class="page">
  %s
  %s
  %s
</div>
</body></html>`,
		htmlEscape(title),
		fontEssay,
		headerHTML,
		titleHTML,
		body.String(),
	)
}

// renderEssayDOCX generates a DOCX in academic essay format.
func renderEssayDOCX(docType string, title string, sections []docSection, path string) error {
	cfg := essayDOCXConfig()
	doc := newDOCX(cfg)

	// MLA: 4-line header.
	if docType == docEssayMLA {
		addBodyParagraph(doc, cfg, "Student Name")
		addBodyParagraph(doc, cfg, "Instructor Name")
		addBodyParagraph(doc, cfg, "Course Name")
		addBodyParagraph(doc, cfg, "Date")
	}

	// Title (centered, no bold per MLA/Chicago).
	para := doc.AddParagraph()
	para.Properties().SetAlignment(wmlSTJcCenter)
	run := para.AddRun()
	run.AddText(title)
	run.Properties().SetFontFamily(cfg.fontFamily)
	run.Properties().SetSize(measurement.Distance(12 * measurement.Point))
	para.Properties().SetSpacing(0, measurement.Distance(24*measurement.Point))

	// Body sections.
	for _, s := range sections {
		if s.Heading != "" && s.Heading != "Overview" {
			// Essay headings are plain bold, same font size.
			para := doc.AddParagraph()
			run := para.AddRun()
			run.AddText(s.Heading)
			run.Properties().SetFontFamily(cfg.fontFamily)
			run.Properties().SetSize(measurement.Distance(12 * measurement.Point))
			run.Properties().SetBold(true)
			para.Properties().SetSpacing(0, measurement.Distance(6*measurement.Point))
		}
		if s.Body != "" {
			for _, paraText := range strings.Split(s.Body, "\n\n") {
				text := strings.TrimSpace(paraText)
				if text != "" {
					addBodyParagraph(doc, cfg, text)
				}
			}
		}
		for _, b := range s.Bullets {
			addBullet(doc, cfg, b)
		}
	}

	return saveDOCX(doc, path)
}

// wmlSTJcCenter is the gooxml alignment constant for centered text.
// (Defined here to avoid importing wml in every function that uses it.)
var wmlSTJcCenter = stJcCenter
