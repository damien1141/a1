package kamitool

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"baliance.com/gooxml/color"
	"baliance.com/gooxml/measurement"
	"baliance.com/gooxml/presentation"
	"baliance.com/gooxml/schema/soo/dml"
	"baliance.com/gooxml/spreadsheet"
)

// Spreadsheet and presentation output for the document tool: the same
// markdown content (sections, bullets, pipe tables) that feeds the PDF and
// DOCX renderers, laid out as rows or slides. gooxml is already the DOCX
// dependency, so no new module rides in.

// markdownTable is one pipe-table block parsed from the content.
type markdownTable struct {
	Header []string
	Rows   [][]string
}

// parseMarkdownTables extracts pipe tables from raw markdown content:
// consecutive lines that start with '|', with separator rows (---) dropped.
func parseMarkdownTables(content string) []markdownTable {
	var tables []markdownTable
	var current *markdownTable
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			current = nil
			continue
		}
		cells := splitPipeRow(trimmed)
		if isSeparatorRow(cells) {
			continue
		}
		if current == nil {
			tables = append(tables, markdownTable{Header: cells})
			current = &tables[len(tables)-1]
			continue
		}
		current.Rows = append(current.Rows, cells)
	}
	return tables
}

func splitPipeRow(line string) []string {
	line = strings.TrimPrefix(strings.TrimSuffix(trimmedPipe(line), "|"), "|")
	parts := strings.Split(line, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

func trimmedPipe(line string) string {
	return strings.TrimRight(line, " \t")
}

func isSeparatorRow(cells []string) bool {
	for _, c := range cells {
		if c == "" {
			continue
		}
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return len(cells) > 0
}

// renderKamiXLSX writes the document as a spreadsheet: title, then one row
// per heading / paragraph / bullet, with pipe tables as real grid rows.
func renderKamiXLSX(docType, title string, sections []docSection, content, path string) error {
	wb := spreadsheet.New()
	sheet := wb.AddSheet()
	sheet.SetName(docTypeLabel(docType))

	rowNum := uint32(1)
	setCell := func(text string, bold bool, size measurement.Distance) {
		row := sheet.Row(rowNum)
		cell := row.AddCell()
		if bold || size != 0 {
			rt := cell.SetRichTextString()
			run := rt.AddRun()
			run.SetText(text)
			if bold {
				run.SetBold(true)
			}
			if size != 0 {
				run.SetSize(size)
			}
		} else {
			cell.SetString(text)
		}
		rowNum++
	}
	setRow := func(cells []string) {
		row := sheet.Row(rowNum)
		for _, c := range cells {
			row.AddCell().SetString(c)
		}
		rowNum++
	}

	setCell(strings.ToUpper(docTypeLabel(docType)), false, 9*measurement.Point)
	setCell(title, true, 18*measurement.Point)
	rowNum++ // blank spacer

	for _, s := range sections {
		if s.Heading != "" {
			setCell(s.Heading, true, 12*measurement.Point)
		}
		for _, para := range strings.Split(s.Body, "\n\n") {
			if text := strings.TrimSpace(para); text != "" {
				setCell(text, false, 0)
			}
		}
		for _, b := range s.Bullets {
			setCell("• "+b, false, 0)
		}
	}
	// Tables follow the prose so each table starts on its own rows.
	for _, tbl := range parseMarkdownTables(content) {
		setRow(tbl.Header)
		for _, r := range tbl.Rows {
			setRow(r)
		}
		rowNum++
	}
	return wb.SaveToFile(path)
}

// renderKamiPPTX writes the document as a slide deck: title slide, then
// one slide per section with its bullets (or paragraphs) as body lines.
// Table rows render as one line per row.
func renderKamiPPTX(docType, title string, sections []docSection, content, path string) error {
	ppt := presentation.New()

	addSlide := func(heading, subtitle string, lines []string) {
		slide := ppt.AddSlide()

		head := slide.AddTextBox()
		head.Properties().SetPosition(0.5*measurement.Inch, 0.5*measurement.Inch)
		head.Properties().SetWidth(9 * measurement.Inch)
		head.Properties().SetHeight(1.1 * measurement.Inch)
		p := head.AddParagraph()
		r := p.AddRun()
		r.SetText(heading)
		r.Properties().SetSize(28 * measurement.Point)
		r.Properties().SetBold(true)
		r.Properties().SetSolidFill(color.FromHex("3B3226"))

		if subtitle != "" {
			sp := head.AddParagraph()
			sr := sp.AddRun()
			sr.SetText(subtitle)
			sr.Properties().SetSize(12 * measurement.Point)
			sr.Properties().SetSolidFill(color.FromHex("8B7E66"))
		}

		if len(lines) == 0 {
			return
		}
		body := slide.AddTextBox()
		body.Properties().SetPosition(0.7*measurement.Inch, 1.9*measurement.Inch)
		body.Properties().SetWidth(8.6 * measurement.Inch)
		body.Properties().SetHeight(5 * measurement.Inch)
		for _, line := range lines {
			bp := body.AddParagraph()
			br := bp.AddRun()
			br.SetText("• " + line)
			br.Properties().SetSize(14 * measurement.Point)
			bp.Properties().SetAlign(dml.ST_TextAlignTypeL)
		}
	}

	addSlide(title, strings.ToUpper(docTypeLabel(docType)), nil)
	for _, s := range sections {
		var lines []string
		for _, para := range strings.Split(s.Body, "\n\n") {
			if text := strings.TrimSpace(para); text != "" {
				lines = append(lines, text)
			}
		}
		lines = append(lines, s.Bullets...)
		addSlide(s.Heading, "", lines)
	}
	for _, tbl := range parseMarkdownTables(content) {
		var lines []string
		lines = append(lines, strings.Join(tbl.Header, " — "))
		for _, r := range tbl.Rows {
			lines = append(lines, strings.Join(r, " — "))
		}
		addSlide("Table", "", lines)
	}
	return ppt.SaveToFile(path)
}

// renderKamiCSV writes the document as CSV: pipe tables become real rows;
// prose content falls back to one row per line so the output is never
// empty.
func renderKamiCSV(docType, title string, sections []docSection, content, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create csv: %w", err)
	}
	defer f.Close()
	w := csv.NewWriter(f)

	writeRow := func(cells ...string) error {
		return w.Write(cells)
	}

	tables := parseMarkdownTables(content)
	if err := writeRow(strings.ToUpper(docTypeLabel(docType))); err != nil {
		return err
	}
	if err := writeRow(title); err != nil {
		return err
	}
	if err := writeRow(); err != nil {
		return err
	}
	for _, s := range sections {
		if s.Heading != "" {
			if err := writeRow(s.Heading); err != nil {
				return err
			}
		}
		for _, para := range strings.Split(s.Body, "\n\n") {
			if text := strings.TrimSpace(para); text != "" {
				if err := writeRow(text); err != nil {
					return err
				}
			}
		}
		for _, b := range s.Bullets {
			if err := writeRow("• " + b); err != nil {
				return err
			}
		}
	}
	for _, tbl := range tables {
		if err := writeRow(); err != nil {
			return err
		}
		if err := writeRow(tbl.Header...); err != nil {
			return err
		}
		for _, r := range tbl.Rows {
			if err := writeRow(r...); err != nil {
				return err
			}
		}
	}
	w.Flush()
	return w.Error()
}
