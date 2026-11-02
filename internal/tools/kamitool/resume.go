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
	"baliance.com/gooxml/schema/soo/wml"
	"baliance.com/gooxml/measurement"
)

type resumeContact struct {
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Location string `json:"location,omitempty"`
	Website   string `json:"website,omitempty"`
}

type resumeExperience struct {
	Company string   `json:"company"`
	Role    string   `json:"role"`
	Period  string   `json:"period,omitempty"`
	Bullets []string `json:"bullets,omitempty"`
}

type resumeEducation struct {
	School  string `json:"school"`
	Degree  string `json:"degree"`
	Period  string `json:"period,omitempty"`
}

type resumeInput struct {
	Name        string            `json:"name"`
	Title       string            `json:"title,omitempty"`
	Contact     resumeContact     `json:"contact,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Experience  []resumeExperience `json:"experience,omitempty"`
	Education   []resumeEducation  `json:"education,omitempty"`
	Skills      []string          `json:"skills,omitempty"`
	Format      string            `json:"format,omitempty"`
	Output      string            `json:"output,omitempty"`
}

var resumeDescription = `Generate a Kami-styled resume.

Produces a clean, warm-toned resume with parchment background, ink-blue section
headers, and HarmonyOS Sans typography. Sections: header (name + title + contact),
summary, experience (company/role/period/bullets), education, skills.

Export as PDF (default) or DOCX.`

func ResumeTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "resume",
			Description: resumeDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"name":     llm.Object{"type": "string", "description": "Full name. Example: Jane Doe"},
					"title":    llm.Object{"type": "string", "description": "Professional title. Example: Senior Software Engineer"},
					"contact": llm.Object{
						"type": "object",
						"properties": llm.Object{
							"email":    llm.Object{"type": "string"},
							"phone":    llm.Object{"type": "string"},
							"location":  llm.Object{"type": "string"},
							"website":   llm.Object{"type": "string"},
						},
					},
					"summary": llm.Object{"type": "string", "description": "1-3 sentence professional summary."},
					"experience": llm.Object{
						"type":        "array",
						"description": "Work experience entries.",
						"items": llm.Object{
							"type": "object",
							"properties": llm.Object{
								"company": llm.Object{"type": "string"},
								"role":    llm.Object{"type": "string"},
								"period":  llm.Object{"type": "string"},
								"bullets": llm.Object{"type": "array", "items": llm.Object{"type": "string"}},
							},
						},
					},
					"education": llm.Object{
						"type":        "array",
						"description": "Education entries.",
						"items": llm.Object{
							"type": "object",
							"properties": llm.Object{
								"school": llm.Object{"type": "string"},
								"degree": llm.Object{"type": "string"},
								"period": llm.Object{"type": "string"},
							},
						},
					},
					"skills":  llm.Object{"type": "array", "items": llm.Object{"type": "string"}, "description": "Skill list."},
					"format":  llm.Object{"type": "string", "description": "Output format: 'pdf' (default) or 'docx'."},
					"output":  llm.Object{"type": "string", "description": "Output file path."},
				},
				Required: []string{"name"},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in resumeInput
			_ = json.Unmarshal(input, &in)
			n := strings.TrimSpace(in.Name)
			if n == "" {
				return "resume"
			}
			return fmt.Sprintf("resume %q", n)
		},
		Run: runResume,
	}
}

func runResume(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in resumeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse resume arguments: %w", err)
	}
	if strings.TrimSpace(in.Name) == "" {
		return tooldef.Result{}, fmt.Errorf("name is required")
	}

	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format == "" {
		format = "pdf"
	}
	outputPath, err := resolveOutput(in.Output, "resume", format)
	if err != nil {
		return tooldef.Result{}, err
	}

	switch format {
	case "pdf":
		html := renderResumeHTML(in)
		if err := renderPDF(ctx, html, outputPath, "Letter"); err != nil {
			return tooldef.Result{}, err
		}
	case "docx":
		if err := renderResumeDOCX(in, outputPath); err != nil {
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
	content := fmt.Sprintf("Resume saved to %s (%d bytes)", outputPath, size)
	return tooldef.Result{
		Content: content,
		Detail:  fmt.Sprintf("resume %s → %s", format, filepath.Base(outputPath)),
		Output:  content,
	}, nil
}

func renderResumeHTML(in resumeInput) string {
	fontFace := fontFaceCSS()

	// Contact line.
	var contacts []string
	if in.Contact.Email != "" {
		contacts = append(contacts, htmlEscape(in.Contact.Email))
	}
	if in.Contact.Phone != "" {
		contacts = append(contacts, htmlEscape(in.Contact.Phone))
	}
	if in.Contact.Location != "" {
		contacts = append(contacts, htmlEscape(in.Contact.Location))
	}
	if in.Contact.Website != "" {
		contacts = append(contacts, htmlEscape(in.Contact.Website))
	}
	contactLine := strings.Join(contacts, " · ")

	// Experience sections.
	var expHTML strings.Builder
	for _, e := range in.Experience {
		expHTML.WriteString(fmt.Sprintf(`<div class="exp-entry">
  <div class="exp-header"><span class="exp-role">%s</span><span class="exp-company">%s</span><span class="exp-period">%s</span></div>`,
			htmlEscape(e.Role), htmlEscape(e.Company), htmlEscape(e.Period)))
		if len(e.Bullets) > 0 {
			expHTML.WriteString(`<ul>`)
			for _, b := range e.Bullets {
				expHTML.WriteString(fmt.Sprintf(`<li>%s</li>`, htmlEscape(b)))
			}
			expHTML.WriteString(`</ul>`)
		}
		expHTML.WriteString(`</div>`)
	}

	// Education sections.
	var eduHTML strings.Builder
	for _, ed := range in.Education {
		eduHTML.WriteString(fmt.Sprintf(`<div class="edu-entry"><span class="edu-degree">%s</span><span class="edu-school">%s</span><span class="edu-period">%s</span></div>`,
			htmlEscape(ed.Degree), htmlEscape(ed.School), htmlEscape(ed.Period)))
	}

	// Skills.
	skillsHTML := ""
	if len(in.Skills) > 0 {
		skillsHTML = fmt.Sprintf(`<p class="skills-list">%s</p>`, htmlEscape(strings.Join(in.Skills, " · ")))
	}

	// Summary.
	summaryHTML := ""
	if in.Summary != "" {
		summaryHTML = fmt.Sprintf(`<p class="summary">%s</p>`, htmlEscape(in.Summary))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>%s</title><style>
%s
@page { size: Letter; margin: 0; }
* { margin: 0; padding: 0; box-sizing: border-box; }
html, body { background: %s; font-family: %s; color: %s; font-size: 10.5pt; line-height: 1.5; }
.page { padding: 56pt 56pt; }
.header { text-align: center; margin-bottom: 28pt; padding-bottom: 16pt; border-bottom: 2pt solid %s; }
.header h1 { font-size: 22pt; font-weight: 700; color: %s; margin-bottom: 4pt; }
.header .title { font-size: 11pt; color: %s; margin-bottom: 8pt; font-weight: 500; }
.header .contact { font-size: 9.5pt; color: %s; }
.section { margin-bottom: 24pt; }
.section h2 { font-size: 11pt; font-weight: 600; color: %s; text-transform: uppercase; letter-spacing: 1pt; margin-bottom: 10pt; padding-bottom: 3pt; border-bottom: 1pt solid %s; }
.summary { color: %s; margin-bottom: 8pt; }
.exp-entry { margin-bottom: 16pt; }
.exp-header { display: flex; justify-content: space-between; margin-bottom: 4pt; }
.exp-role { font-weight: 600; color: %s; }
.exp-company { color: %s; flex: 1; margin-left: 8pt; }
.exp-period { color: %s; font-size: 9.5pt; }
.exp-entry ul { margin: 4pt 0 0 18pt; color: %s; }
.exp-entry li { margin-bottom: 3pt; }
.edu-entry { display: flex; justify-content: space-between; margin-bottom: 6pt; }
.edu-degree { font-weight: 600; color: %s; }
.edu-school { color: %s; flex: 1; margin-left: 8pt; }
.edu-period { color: %s; font-size: 9.5pt; }
.skills-list { color: %s; }
</style></head><body>
<div class="page">
  <div class="header"><h1>%s</h1><div class="title">%s</div><div class="contact">%s</div></div>
  %s
  %s
  %s
  %s
</div>
</body></html>`,
		htmlEscape(in.Name), fontFace,
		kamiBackground, fontPrimary, kamiText,
		kamiInk, kamiInk, kamiInkLight, kamiTextMuted,
		kamiInk, kamiBorder,
		kamiText,
		kamiInk, kamiText, kamiTextMuted, kamiText,
		kamiInk, kamiText, kamiTextMuted,
		kamiText,
		htmlEscape(in.Name), htmlEscape(in.Title), contactLine,
		sectionHTML("Summary", summaryHTML),
		sectionHTML("Experience", expHTML.String()),
		sectionHTML("Education", eduHTML.String()),
		sectionHTML("Skills", skillsHTML),
	)
}

func sectionHTML(heading, body string) string {
	if body == "" {
		return ""
	}
	return fmt.Sprintf(`<div class="section"><h2>%s</h2>%s</div>`, heading, body)
}

func renderResumeDOCX(in resumeInput, path string) error {
	cfg := kamiDOCXConfig()
	doc := newDOCX(cfg)

	// Header: name (large, ink-blue), title, contact line.
	para := doc.AddParagraph()
	para.Properties().SetAlignment(wml.ST_JcCenter)
	run := para.AddRun()
	run.AddText(in.Name)
	run.Properties().SetFontFamily(cfg.fontFamily)
	run.Properties().SetSize(measurement.Distance(20 * measurement.Point))
	run.Properties().SetBold(true)
	run.Properties().SetColor(color.FromHex(cfg.headingColor))
	para.Properties().SetSpacing(0, measurement.Distance(2 * measurement.Point))

	if in.Title != "" {
		para = doc.AddParagraph()
		para.Properties().SetAlignment(wml.ST_JcCenter)
		run = para.AddRun()
		run.AddText(in.Title)
		run.Properties().SetFontFamily(cfg.fontFamily)
		run.Properties().SetSize(measurement.Distance(11 * measurement.Point))
		run.Properties().SetColor(color.FromHex("8B7E66"))
		para.Properties().SetSpacing(0, measurement.Distance(6 * measurement.Point))
	}

	// Contact line.
	var contacts []string
	if in.Contact.Email != "" {
		contacts = append(contacts, in.Contact.Email)
	}
	if in.Contact.Phone != "" {
		contacts = append(contacts, in.Contact.Phone)
	}
	if in.Contact.Location != "" {
		contacts = append(contacts, in.Contact.Location)
	}
	if in.Contact.Website != "" {
		contacts = append(contacts, in.Contact.Website)
	}
	if len(contacts) > 0 {
		para = doc.AddParagraph()
		para.Properties().SetAlignment(wml.ST_JcCenter)
		run = para.AddRun()
		run.AddText(strings.Join(contacts, " · "))
		run.Properties().SetFontFamily(cfg.fontFamily)
		run.Properties().SetSize(measurement.Distance(9.5 * measurement.Point))
		run.Properties().SetColor(color.FromHex("8B7E66"))
		para.Properties().SetSpacing(0, measurement.Distance(18 * measurement.Point))
	}

	// Summary.
	if in.Summary != "" {
		addHeading(doc, cfg, "Summary", 3)
		addBodyParagraph(doc, cfg, in.Summary)
	}

	// Experience.
	if len(in.Experience) > 0 {
		addHeading(doc, cfg, "Experience", 3)
		for _, e := range in.Experience {
			para = doc.AddParagraph()
			run = para.AddRun()
			run.AddText(fmt.Sprintf("%s — %s", e.Role, e.Company))
			run.Properties().SetFontFamily(cfg.fontFamily)
			run.Properties().SetSize(measurement.Distance(11 * measurement.Point))
			run.Properties().SetBold(true)
			run.Properties().SetColor(color.FromHex(cfg.headingColor))
			if e.Period != "" {
				run2 := para.AddRun()
				run2.AddText("  " + e.Period)
				run2.Properties().SetFontFamily(cfg.fontFamily)
				run2.Properties().SetSize(measurement.Distance(9.5 * measurement.Point))
				run2.Properties().SetColor(color.FromHex("8B7E66"))
			}
			para.Properties().SetSpacing(0, measurement.Distance(2 * measurement.Point))
			for _, b := range e.Bullets {
				addBullet(doc, cfg, b)
			}
		}
	}

	// Education.
	if len(in.Education) > 0 {
		addHeading(doc, cfg, "Education", 3)
		for _, ed := range in.Education {
			para = doc.AddParagraph()
			run = para.AddRun()
			run.AddText(fmt.Sprintf("%s — %s", ed.Degree, ed.School))
			run.Properties().SetFontFamily(cfg.fontFamily)
			run.Properties().SetSize(measurement.Distance(11 * measurement.Point))
			run.Properties().SetBold(true)
			run.Properties().SetColor(color.FromHex(cfg.headingColor))
			if ed.Period != "" {
				run2 := para.AddRun()
				run2.AddText("  " + ed.Period)
				run2.Properties().SetFontFamily(cfg.fontFamily)
				run2.Properties().SetSize(measurement.Distance(9.5 * measurement.Point))
				run2.Properties().SetColor(color.FromHex("8B7E66"))
			}
		}
	}

	// Skills.
	if len(in.Skills) > 0 {
		addHeading(doc, cfg, "Skills", 3)
		addBodyParagraph(doc, cfg, strings.Join(in.Skills, " · "))
	}

	return saveDOCX(doc, path)
}
