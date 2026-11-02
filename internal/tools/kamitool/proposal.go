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

// ProposalSection is one section of a proposal (heading + body paragraphs).
type ProposalSection struct {
	Heading string   `json:"heading"`
	Body    string   `json:"body"`
	Bullets []string `json:"bullets,omitempty"`
}

// proposalInput is the typed schema for the proposal tool.
type proposalInput struct {
	Title        string            `json:"title"`
	Subtitle     string            `json:"subtitle,omitempty"`
	Author       string            `json:"author,omitempty"`
	Organization string            `json:"organization,omitempty"`
	Date         string            `json:"date,omitempty"`
	Sections     []ProposalSection `json:"sections"`
	Format       string            `json:"format,omitempty"` // "pdf" (default) or "docx"
	Output       string            `json:"output,omitempty"`  // output path; default ~/download/
}

var proposalDescription = `Generate a Kami-styled proposal document.

Produces a warm, professionally designed proposal with parchment background,
ink-blue accents, and clean typography. The design follows the Kami design
system (tw93/Kami) with HarmonyOS Sans as the primary font.

The proposal includes a cover section (title, subtitle, author, date) followed
by body sections (heading + paragraphs + optional bullets). Export as PDF
(default) or DOCX.

Example: a project proposal with executive summary, problem statement, solution,
timeline, and next steps.`

// ProposalTool returns the Kami-styled proposal generator.
func ProposalTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "proposal",
			Description: proposalDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"title": llm.Object{
						"type":        "string",
						"description": "Proposal title. Example: Project Apollo",
					},
					"subtitle": llm.Object{
						"type":        "string",
						"description": "One-line subtitle/tagline below the title.",
					},
					"author": llm.Object{
						"type":        "string",
						"description": "Author name. Example: Jane Doe",
					},
					"organization": llm.Object{
						"type":        "string",
						"description": "Organization name. Example: Acme Corp",
					},
					"date": llm.Object{
						"type":        "string",
						"description": "Date string. Example: January 2026",
					},
					"sections": llm.Object{
						"type":        "array",
						"description": "Body sections. Each has a heading, body text, and optional bullet points.",
						"items": llm.Object{
							"type": "object",
							"properties": llm.Object{
								"heading": llm.Object{"type": "string"},
								"body":    llm.Object{"type": "string"},
								"bullets": llm.Object{"type": "array", "items": llm.Object{"type": "string"}},
							},
						},
					},
					"format": llm.Object{
						"type":        "string",
						"description": "Output format: 'pdf' (default) or 'docx'.",
					},
					"output": llm.Object{
						"type":        "string",
						"description": "Output file path. Default: ~/download/proposal-<timestamp>.pdf",
					},
				},
				Required: []string{"title", "sections"},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in proposalInput
			_ = json.Unmarshal(input, &in)
			t := strings.TrimSpace(in.Title)
			if t == "" {
				return "proposal"
			}
			return fmt.Sprintf("proposal %q", t)
		},
		Run: runProposal,
	}
}

func runProposal(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in proposalInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse proposal arguments: %w", err)
	}
	if strings.TrimSpace(in.Title) == "" {
		return tooldef.Result{}, fmt.Errorf("title is required")
	}
	if len(in.Sections) == 0 {
		return tooldef.Result{}, fmt.Errorf("at least one section is required")
	}

	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format == "" {
		format = "pdf"
	}

	outputPath, err := resolveOutput(in.Output, "proposal", format)
	if err != nil {
		return tooldef.Result{}, err
	}

	switch format {
	case "pdf":
		html := renderProposalHTML(in)
		if err := renderPDF(ctx, html, outputPath, "Letter"); err != nil {
			return tooldef.Result{}, err
		}
	case "docx":
		if err := renderProposalDOCX(in, outputPath); err != nil {
			return tooldef.Result{}, err
		}
	default:
		return tooldef.Result{}, fmt.Errorf("unknown format %q: use pdf or docx", format)
	}

	info, _ := os.Stat(outputPath)
	size := int64(0)
	if info != nil {
		size = info.Size()
	}
	content := fmt.Sprintf("Proposal saved to %s (%d bytes)", outputPath, size)
	return tooldef.Result{
		Content: content,
		Detail:  fmt.Sprintf("proposal %s → %s", format, filepath.Base(outputPath)),
		Output:  content,
	}, nil
}

// renderProposalHTML generates the Kami-styled HTML for the proposal.
func renderProposalHTML(in proposalInput) string {
	fontFace := fontFaceCSS()
	var body strings.Builder

	for _, s := range in.Sections {
		body.WriteString(fmt.Sprintf(`<section class="doc-section">
  <h2>%s</h2>
  <p>%s</p>`, htmlEscape(s.Heading), htmlEscape(s.Body)))
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
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>%s</title>
  <style>
%s
    @page { size: Letter; margin: 0; }
    * { margin: 0; padding: 0; box-sizing: border-box; }
    html, body {
      background: %s;
      font-family: %s;
      color: %s;
      font-size: 11pt;
      line-height: 1.6;
    }
    .page {
      padding: 72pt 64pt;
      max-width: 612pt; /* Letter width */
    }
    .cover {
      padding-top: 120pt;
      padding-bottom: 60pt;
      border-bottom: 2pt solid %s;
      margin-bottom: 48pt;
    }
    .cover h1 {
      font-size: 28pt;
      font-weight: 700;
      color: %s;
      margin-bottom: 8pt;
      line-height: 1.2;
    }
    .cover .subtitle {
      font-size: 14pt;
      color: %s;
      margin-bottom: 24pt;
      font-weight: 300;
    }
    .cover .meta {
      font-size: 10pt;
      color: %s;
      line-height: 1.8;
    }
    .doc-section {
      margin-bottom: 36pt;
    }
    .doc-section h2 {
      font-size: 14pt;
      font-weight: 600;
      color: %s;
      margin-bottom: 12pt;
      padding-bottom: 4pt;
      border-bottom: 1pt solid %s;
    }
    .doc-section p {
      margin-bottom: 12pt;
      color: %s;
    }
    .doc-section ul {
      margin: 12pt 0 12pt 24pt;
      color: %s;
    }
    .doc-section li {
      margin-bottom: 6pt;
    }
  </style>
</head>
<body>
  <div class="page">
    <div class="cover">
      <h1>%s</h1>
      %s
      <div class="meta">%s</div>
    </div>
    %s
  </div>
</body>
</html>`,
		htmlEscape(in.Title),
		fontFace,
		kamiBackground,
		fontPrimary,
		kamiText,
		kamiInk,
		kamiInk,
		kamiInkLight,
		kamiTextMuted,
		kamiInk,
		kamiBorder,
		kamiText,
		kamiText,
		htmlEscape(in.Title),
		subtitleHTML(in.Subtitle),
		metaHTML(in.Author, in.Organization, in.Date),
		body.String(),
	)
}

func subtitleHTML(s string) string {
	if s == "" {
		return ""
	}
	return fmt.Sprintf(`<div class="subtitle">%s</div>`, htmlEscape(s))
}

func metaHTML(author, org, date string) string {
	var parts []string
	if author != "" {
		parts = append(parts, htmlEscape(author))
	}
	if org != "" {
		parts = append(parts, htmlEscape(org))
	}
	if date != "" {
		parts = append(parts, htmlEscape(date))
	}
	return strings.Join(parts, "<br>")
}

// renderProposalDOCX generates a Kami-styled DOCX proposal.
func renderProposalDOCX(in proposalInput, path string) error {
	cfg := kamiDOCXConfig()
	doc := newDOCX(cfg)

	// Cover: title (large, ink-blue), subtitle, author/org/date.
	if in.Title != "" {
		para := doc.AddParagraph()
		run := para.AddRun()
		run.AddText(in.Title)
		run.Properties().SetFontFamily(cfg.fontFamily)
		run.Properties().SetSize(measurement.Distance(24 * measurement.Point))
		run.Properties().SetBold(true)
		run.Properties().SetColor(color.FromHex(cfg.headingColor))
		para.Properties().SetSpacing(0, measurement.Distance(6 * measurement.Point))
	}
	if in.Subtitle != "" {
		para := doc.AddParagraph()
		run := para.AddRun()
		run.AddText(in.Subtitle)
		run.Properties().SetFontFamily(cfg.fontFamily)
		run.Properties().SetSize(measurement.Distance(13 * measurement.Point))
		run.Properties().SetColor(color.FromHex("8B7E66"))
		para.Properties().SetSpacing(0, measurement.Distance(18 * measurement.Point))
	}
	if in.Author != "" || in.Organization != "" || in.Date != "" {
		para := doc.AddParagraph()
		var metaParts []string
		if in.Author != "" {
			metaParts = append(metaParts, in.Author)
		}
		if in.Organization != "" {
			metaParts = append(metaParts, in.Organization)
		}
		if in.Date != "" {
			metaParts = append(metaParts, in.Date)
		}
		run := para.AddRun()
		run.AddText(strings.Join(metaParts, " · "))
		run.Properties().SetFontFamily(cfg.fontFamily)
		run.Properties().SetSize(measurement.Distance(10 * measurement.Point))
		run.Properties().SetColor(color.FromHex("8B7E66"))
		para.Properties().SetSpacing(0, measurement.Distance(36 * measurement.Point))
	}

	// Body sections.
	for _, s := range in.Sections {
		addHeading(doc, cfg, s.Heading, 2)
		if s.Body != "" {
			addBodyParagraph(doc, cfg, s.Body)
		}
		for _, b := range s.Bullets {
			addBullet(doc, cfg, b)
		}
	}

	return saveDOCX(doc, path)
}

// resolveOutput determines the output file path. If output is empty,
// generates a default path in ~/download/.
func resolveOutput(output, docType, format string) (string, error) {
	if output != "" {
		// Ensure parent dir exists.
		dir := filepath.Dir(output)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("create output dir: %w", err)
			}
		}
		return output, nil
	}
	// Default: ~/download/<docType>-<timestamp>.<ext>
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	downloadDir := filepath.Join(home, "download")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return "", fmt.Errorf("create download dir: %w", err)
	}
	ext := format
	if format == "pptx" {
		ext = "pptx"
	}
	return filepath.Join(downloadDir, fmt.Sprintf("%s.%s", docType, ext)), nil
}

// htmlEscape replaces &, <, >, ", ' with their HTML entities.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, `'`, "&#39;")
	return s
}
