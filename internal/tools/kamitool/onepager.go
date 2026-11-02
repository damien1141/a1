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

// onepagerInput is the typed schema for the one-pager tool.
type onepagerInput struct {
        Title      string            `json:"title"`
        Subtitle   string            `json:"subtitle,omitempty"`
        Tagline    string            `json:"tagline,omitempty"`
        Sections   []ProposalSection  `json:"sections"` // reuse ProposalSection
        Footer     string            `json:"footer,omitempty"`
        Format     string            `json:"format,omitempty"`
        Output     string            `json:"output,omitempty"`
}

var onepagerDescription = `Generate a Kami-styled one-pager.

A single-page document with a title, subtitle, and 2-4 sections. Designed to
fit on one Letter page with compact spacing. Warm parchment background, ink-blue
accents, HarmonyOS Sans typography.

Use this for executive summaries, product overviews, project briefs, or any
document that needs to convey a complete picture on one page.`

func OnePagerTool() tooldef.Tool {
        return tooldef.Tool{
                Definition: llm.ToolDefinition{
                        Name:        "onepager",
                        Description: onepagerDescription,
                        Params: &llm.FunctionParameters{
                                Type: "object",
                                Properties: llm.Object{
                                        "title":    llm.Object{"type": "string", "description": "Document title."},
                                        "subtitle": llm.Object{"type": "string", "description": "Subtitle below the title."},
                                        "tagline":  llm.Object{"type": "string", "description": "Short one-line tagline at the top."},
                                        "sections": llm.Object{
                                                "type":        "array",
                                                "description": "2-4 sections. Each has a heading, body, and optional bullets.",
                                                "items": llm.Object{
                                                        "type": "object",
                                                        "properties": llm.Object{
                                                                "heading": llm.Object{"type": "string"},
                                                                "body":    llm.Object{"type": "string"},
                                                                "bullets": llm.Object{"type": "array", "items": llm.Object{"type": "string"}},
                                                        },
                                                },
                                        },
                                        "footer": llm.Object{"type": "string", "description": "Footer line (e.g. contact info, date)."},
                                        "format": llm.Object{"type": "string", "description": "Output format: 'pdf' (default) or 'docx'."},
                                        "output": llm.Object{"type": "string", "description": "Output file path."},
                                },
                                Required: []string{"title", "sections"},
                        },
                },
                DetailFromArgs: func(input json.RawMessage) string {
                        var in onepagerInput
                        _ = json.Unmarshal(input, &in)
                        t := strings.TrimSpace(in.Title)
                        if t == "" {
                                return "onepager"
                        }
                        return fmt.Sprintf("onepager %q", t)
                },
                Run: runOnePager,
        }
}

func runOnePager(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
        var in onepagerInput
        if err := json.Unmarshal(input, &in); err != nil {
                return tooldef.Result{}, fmt.Errorf("failed to parse onepager arguments: %w", err)
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
        outputPath, err := resolveOutput(in.Output, "onepager", format)
        if err != nil {
                return tooldef.Result{}, err
        }

        switch format {
        case "pdf":
                html := renderOnePagerHTML(in)
                if err := renderPDF(ctx, html, outputPath, "Letter"); err != nil {
                        return tooldef.Result{}, err
                }
        case "docx":
                if err := renderOnePagerDOCX(in, outputPath); err != nil {
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
        content := fmt.Sprintf("One-pager saved to %s (%d bytes)", outputPath, size)
        return tooldef.Result{
                Content: content,
                Detail:  fmt.Sprintf("onepager %s → %s", format, filepath.Base(outputPath)),
                Output:  content,
        }, nil
}

func renderOnePagerHTML(in onepagerInput) string {
        fontFace := fontFaceCSS()

        var body strings.Builder
        for _, s := range in.Sections {
                body.WriteString(fmt.Sprintf(`<div class="op-section">
  <h3>%s</h3>
  <p>%s</p>`, htmlEscape(s.Heading), htmlEscape(s.Body)))
                if len(s.Bullets) > 0 {
                        body.WriteString(`<ul>`)
                        for _, b := range s.Bullets {
                                body.WriteString(fmt.Sprintf(`<li>%s</li>`, htmlEscape(b)))
                        }
                        body.WriteString(`</ul>`)
                }
                body.WriteString(`</div>`)
        }

        footerHTML := ""
        if in.Footer != "" {
                footerHTML = fmt.Sprintf(`<div class="op-footer">%s</div>`, htmlEscape(in.Footer))
        }

        taglineHTML := ""
        if in.Tagline != "" {
                taglineHTML = fmt.Sprintf(`<div class="op-tagline">%s</div>`, htmlEscape(in.Tagline))
        }

        subtitleHTML := ""
        if in.Subtitle != "" {
                subtitleHTML = fmt.Sprintf(`<div class="op-subtitle">%s</div>`, htmlEscape(in.Subtitle))
        }

        return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>%s</title><style>
%s
@page { size: Letter; margin: 0; }
* { margin: 0; padding: 0; box-sizing: border-box; }
html, body { background: %s; font-family: %s; color: %s; font-size: 10.5pt; line-height: 1.5; }
.page { padding: 48pt 52pt; max-width: 612pt; }
.op-tagline { font-size: 9pt; color: %s; text-transform: uppercase; letter-spacing: 2pt; margin-bottom: 8pt; font-weight: 500; }
.op-title { font-size: 24pt; font-weight: 700; color: %s; margin-bottom: 4pt; line-height: 1.2; }
.op-subtitle { font-size: 12pt; color: %s; margin-bottom: 24pt; font-weight: 300; padding-bottom: 12pt; border-bottom: 2pt solid %s; }
.op-section { margin-bottom: 18pt; }
.op-section h3 { font-size: 11pt; font-weight: 600; color: %s; margin-bottom: 6pt; }
.op-section p { color: %s; margin-bottom: 6pt; }
.op-section ul { margin: 4pt 0 4pt 16pt; color: %s; }
.op-section li { margin-bottom: 2pt; }
.op-footer { margin-top: 24pt; padding-top: 10pt; border-top: 1pt solid %s; font-size: 9pt; color: %s; }
</style></head><body>
<div class="page">
  %s
  <div class="op-title">%s</div>
  %s
  %s
  %s
</div>
</body></html>`,
                htmlEscape(in.Title), fontFace,
                kamiBackground, fontPrimary, kamiText,
                kamiInkLight, kamiInk, kamiInkLight, kamiInk,
                kamiInk, kamiText, kamiText,
                kamiBorder, kamiTextMuted,
                taglineHTML,
                htmlEscape(in.Title),
                subtitleHTML,
                body.String(),
                footerHTML,
        )
}

func renderOnePagerDOCX(in onepagerInput, path string) error {
        cfg := kamiDOCXConfig()
        doc := newDOCX(cfg)

        // Tagline.
        if in.Tagline != "" {
                para := doc.AddParagraph()
                run := para.AddRun()
                run.AddText(strings.ToUpper(in.Tagline))
                run.Properties().SetFontFamily(cfg.fontFamily)
                run.Properties().SetSize(measurement.Distance(9 * measurement.Point))
                run.Properties().SetColor(color.FromHex("8B7E66"))
                para.Properties().SetSpacing(0, measurement.Distance(0))
        }

        // Title.
        para := doc.AddParagraph()
        run := para.AddRun()
        run.AddText(in.Title)
        run.Properties().SetFontFamily(cfg.fontFamily)
        run.Properties().SetSize(measurement.Distance(22 * measurement.Point))
        run.Properties().SetBold(true)
        run.Properties().SetColor(color.FromHex(cfg.headingColor))
        para.Properties().SetSpacing(0, measurement.Distance(2 * measurement.Point))

        // Subtitle.
        if in.Subtitle != "" {
                para = doc.AddParagraph()
                run = para.AddRun()
                run.AddText(in.Subtitle)
                run.Properties().SetFontFamily(cfg.fontFamily)
                run.Properties().SetSize(measurement.Distance(12 * measurement.Point))
                run.Properties().SetColor(color.FromHex("8B7E66"))
                para.Properties().SetSpacing(0, measurement.Distance(24 * measurement.Point))
        }

        // Body sections.
        for _, s := range in.Sections {
                addHeading(doc, cfg, s.Heading, 3)
                if s.Body != "" {
                        addBodyParagraph(doc, cfg, s.Body)
                }
                for _, b := range s.Bullets {
                        addBullet(doc, cfg, b)
                }
        }

        // Footer.
        if in.Footer != "" {
                para = doc.AddParagraph()
                run = para.AddRun()
                run.AddText(in.Footer)
                run.Properties().SetFontFamily(cfg.fontFamily)
                run.Properties().SetSize(measurement.Distance(9 * measurement.Point))
                run.Properties().SetColor(color.FromHex("8B7E66"))
        }

        return saveDOCX(doc, path)
}
