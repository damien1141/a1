// Package kamitool generates Kami-styled documents (proposals, resumes,
// one-pagers, equity reports, letters, changelogs, portfolios, slides)
// and academic essays (MLA + Chicago). It renders to PDF via Playwright
// (HTML → page.PDF()) and to DOCX via unioffice.
//
// The Kami design system (warm parchment palette, ink-blue accents,
// HarmonyOS Sans typography) is shared across all business document
// types. Academic essays use strict MLA/Chicago formatting instead —
// boring white page, Times New Roman, 12pt, 1" margins, double-spaced.
package kamitool

// Kami design system. Palette from tw93/Kami, font swapped to
// HarmonyOS Sans (free for commercial use per Huawei's font license —
// works created with the font may be freely distributed).
const (
	// Kami palette — warm parchment + ink-blue accents.
	kamiBackground = "#f5f4ed" // warm parchment
	kamiSurface    = "#ede9dc" // slightly darker parchment for cards
	kamiInk        = "#1B365D" // ink-blue accent (headings, rules, links)
	kamiInkLight   = "#3A5A85" // lighter ink for subheadings
	kamiText       = "#2c2c2c" // body text — warm near-black
	kamiTextMuted  = "#8B7E66" // warm gray with yellow-brown undertone
	kamiBorder     = "#d4cebd" // warm border/rule color
	kamiWhite      = "#ffffff"

	// Typography — HarmonyOS Sans replaces Kami's Charter/TsangerJinKai.
	// Falls back to system sans-serif when HarmonyOS Sans isn't installed.
	fontPrimary = "HarmonyOS Sans, 'Noto Sans SC', 'Noto Sans', 'Segoe UI', sans-serif"
	fontMono    = "'JetBrains Mono', 'Cascadia Code', monospace"

	// Font name for DOCX (unioffice references by name, not CSS stack).
	fontDocxPrimary = "HarmonyOS Sans"

	// Academic essay fonts — strict MLA/Chicago convention.
	fontEssay     = "'Times New Roman', 'Liberation Serif', serif"
	fontEssayDocx = "Times New Roman"
)

// DocumentFormat selects the output format.
type DocumentFormat string

const (
	FormatPDF  DocumentFormat = "pdf"
	FormatDOCX DocumentFormat = "docx"
	FormatPPTX DocumentFormat = "pptx"
	FormatXLSX DocumentFormat = "xlsx"
	FormatCSV  DocumentFormat = "csv"
)
