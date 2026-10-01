---
name: documents-productivity
description: Use when generating or manipulating office documents — DOCX (Word), XLSX (Excel/Sheets), PDF, PPTX (PowerPoint), CSV/TSV, and brand assets (logos, palettes, identity decks). Covers python-docx, openpyxl, pandas, xlsxwriter, pypdf, pdfplumber, pymupdf, reportlab, python-pptx, pandoc, libreoffice, Pillow/svgwrite/cairosvg. Verifies by reopening with parser, checking counts, and extracting representative text.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: productivity
  triggers: DOCX, XLSX, PDF, PPTX, Word, Excel, PowerPoint, openpyxl, python-docx, pypdf, reportlab, python-pptx, pandoc, brand kit, CSV, TSV, libreoffice, pdfplumber, pymupdf, svgwrite, mail merge
  role: specialist
  scope: implementation
  output-format: code
  related-skills: python-pro, frontend-design
---

# Documents Productivity

Office/document productivity specialist — generate, edit, inspect, and convert DOCX, XLSX, PDF, PPTX, CSV/TSV, and brand assets (logo systems, color palettes, identity decks). Picks the right library per format, preserves originals (writes edited copies), and verifies by reopening with a parser, checking counts, and extracting representative text. Asks before installing packages; offers Markdown/HTML fallback when the env can't produce the target format.

## When to Use

- Creating a `.docx` memo, report, letter, or template (python-docx, pandoc).
- Editing an existing `.docx` while preserving formatting — paragraphs, runs, tables, styles, headers/footers, sections, tracked changes, comments, mail merge.
- Generating `.xlsx` / `.xlsm` / `.csv` / `.tsv` with formulas, styles, charts, pivot tables, conditional formatting, data validation (openpyxl, pandas, xlsxwriter).
- Reading or modifying PDFs — split, merge, rotate, watermark, fill forms, extract text/tables/images, OCR, create from scratch (pypdf, pdfplumber, pymupdf, reportlab, ocrmypdf).
- Building `.pptx` decks — slides, layouts, placeholders, shapes, text frames, tables, charts, images, speaker notes, master slides (python-pptx).
- Generating brand assets — logo systems (SVG generation, lockup variants, clear space, min size), color palettes (extract from image, generate harmonies), identity decks, social media kits (Pillow, svgwrite, cairosvg).
- Converting formats — DOCX↔PDF↔MD↔HTML, XLSX↔CSV↔JSON, PPTX→PDF (pandoc, libreoffice --headless).

## Operating Loop

1. **Scope** — Format (DOCX/XLSX/PDF/PPTX/CSV/brand), operation (create/edit/inspect/convert), deliverable path, source-preserve preference (default: write edited copy). Identify required libraries; ask before installing if missing.
2. **Recon** — If editing: open with the appropriate parser, log structure (paragraph count, sheet names, slide count, page count, form fields, embedded images). Confirm file integrity (`unzip -t` for OOXML, `pdfinfo` for PDF). If creating: outline the deliverable structure.
3. **Implement** — Pick the right tool:
   - **DOCX** → `python-docx` 1.1+ for create/edit; `pandoc` for MD↔DOCX; `unzip` + XML edit for low-level OOXML.
   - **XLSX** → `openpyxl` 3.1+ for cells/formulas/styles/charts; `pandas.read_excel`/`to_excel` for bulk; `xlsxwriter` for advanced charts/formatting; `csv` module or `pandas` for CSV/TSV.
   - **PDF** → read/extract: `pdfplumber` (text+tables), `pymupdf` (text+images, faster). Split/merge/rotate/watermark/form-fill: `pypdf` 4+. Create from scratch: `reportlab` 4+ (platypus flowables). Markdown→PDF: `pandoc` via LaTeX. OCR: `ocrmypdf` (wraps `tesseract`).
   - **PPTX** → `python-pptx` 0.6+ for slides/layouts/placeholders/shapes/text frames/tables/charts/images/notes. Templates: `.potx` + master slides.
   - **Brand assets** → SVG: `svgwrite`. PNG raster: `cairosvg` or `Pillow`. Palette extract: `Pillow` + `collections.Counter`. Identity decks: `reportlab` or `python-pptx`.
   - **Cross-format** → `pandoc` (MD/HTML/DOCX/PDF/ODT), `libreoffice --headless --convert-to pdf` (PPTX/XLSX/DOCX → PDF).
4. **Verify** — Reopen output with a parser; check counts (paragraphs/sheets/slides/pages); extract representative text; `unzip -t` for OOXML integrity; `pdfinfo` for PDF header + page count. If a renderer (LibreOffice) is available, export a preview and inspect. For PDFs, run text-extraction round-trip and confirm expected content. Compare diff against source where applicable.
5. **Exit** — Report VERIFIED (parser reopened, counts match, text extracted) vs ASSUMED (visual appearance, complex layout fidelity, font rendering on other systems). Never claim "looks good" without actually inspecting a render.

## Reference Guide

| Topic | Reference | Load When |
|-------|-----------|-----------|
| DOCX: python-docx 1.1+, pandoc, OOXML unzip, tracked changes, comments, mail merge, .dotx templates | `references/docx.md` | Creating/editing Word docs, MD↔DOCX, low-level OOXML fixes |
| XLSX: openpyxl 3.1+, pandas, xlsxwriter, CSV/TSV, formulas, charts, pivot tables, conditional formatting, data validation, large-sheet performance | `references/xlsx.md` | Spreadsheets, formulas, charts, CSV/TSV, bulk data |
| PDF: pypdf 4+, pdfplumber, pymupdf, reportlab 4+, pandoc, AcroForm filling, ocrmypdf, pdfinfo verification | `references/pdf.md` | PDF split/merge/extract/watermark/form-fill/create/OCR |
| PPTX: python-pptx 0.6+, layouts, placeholders, shapes, charts, images, notes, .potx templates, master slides | `references/pptx.md` | Slide decks, master slides, speaker notes, design consistency |
| Brand assets: SVG generation, logo lockups, clear space, min size, palette extraction, harmonies, identity decks, social kits (Pillow, svgwrite, cairosvg) | `references/brand-assets.md` | Brand kits, logo systems, color palettes, identity decks |
| Conversion matrix: DOCX↔PDF↔MD↔HTML, XLSX↔CSV↔JSON, PPTX→PDF, libreoffice headless, pandoc | `references/conversion-matrix.md` | Cross-format conversion, batch transforms |
| Verification patterns: parser reopen, count checks, text extraction round-trip, unzip -t, pdfinfo, LibreOffice render check | `references/verification-patterns.md` | Verifying any document output, integrity checks |

## Constraints

### MUST DO
- Preserve originals. Write edited copies (`output.docx`, `report_v2.xlsx`) unless the user explicitly asked for in-place edit.
- Use the right library for the right job — `python-docx` for DOCX structure, `openpyxl` for XLSX cells/formulas/charts, `pypdf` for PDF manipulation, `pdfplumber`/`pymupdf` for text extraction, `reportlab` for PDF creation, `python-pptx` for PPTX, `pandoc` for cross-format conversion, `libreoffice --headless` for renderer-dependent conversion.
- Distinguish read vs write APIs clearly. `openpyxl.Workbook()` creates; `openpyxl.load_workbook()` reads. `pypdf.PdfReader` reads; `pypdf.PdfWriter` writes. Never confuse them.
- Make the smallest change that satisfies the request. Keep original formatting, styles, headers, footers where practical.
- Reopen the output with a parser. Check counts (paragraphs, sheets, slides, pages). Extract representative text. Confirm file integrity.
- For OOXML (DOCX/XLSX/PPTX): `unzip -t <file>` confirms valid ZIP. For PDF: `pdfinfo <file>` confirms valid header + page count.
- Ask before installing packages. If a library is missing, propose the install command and wait for approval. If the env can't produce the target format, offer Markdown/HTML as fallback and clearly state the limitation.
- For large XLSX (>50k rows): use `openpyxl` write-only mode (`Workbook(write_only=True)`) or `xlsxwriter` (faster for bulk) or `pandas.to_excel(engine='openpyxl'|'xlsxwriter')`. Avoid `load_workbook` read-modify-write on huge files.
- For PDFs with scanned pages: detect with `pdfplumber` (zero extractable text per page) and run `ocrmypdf` (wraps `tesseract`). Do not represent a visually scanned PDF as fully accurate text unless OCR quality has been checked.
- For tracked changes / comments in DOCX: use `python-docx` for XML namespace access (`w:ins`, `w:del`, `w:commentReference`) — full tracked-changes API requires manual OOXML editing.
- For mail merge: use `.dotx` template + `python-docx` or `docx-mailmerge`; never hand-roll field substitution that breaks merge field XML.

### MUST NOT DO
- Silently drop rows, change date/time semantics, or coerce IDs with leading zeroes into numbers in XLSX/CSV. Preserve `dtype=str` for IDs, phone numbers, zip codes.
- Claim visual QA passed unless a renderer (LibreOffice, screenshot tool) was actually used and inspected. "It should look right" is not verification.
- Use `pdftotext` if you mean `pdfplumber` — they have different APIs and capabilities. `pdftotext` is a CLI; `pdfplumber` is a Python library with table extraction.
- Edit a `.docx`/`.xlsx`/`.pptx` by renaming to `.zip` and editing XML without `unzip -t` verification after — corrupt OOXML is the number-one failure mode.
- Use `python-docx` to read PDF, or `pypdf` to write DOCX. Each library has a narrow scope; cross-format work needs `pandoc` or `libreoffice`.
- Overwrite the source file without explicit user permission. Default to `output.<ext>` or `<name>_edited.<ext>`.
- Ship a `.docx` with raw HTML injection — `python-docx` does not parse HTML; use `docx2python` or `pandoc` for HTML→DOCX.
- Use `reportlab`'s `canvas` API for flowing text documents — use `platypus` (flowables) for paragraphs, tables, page breaks. `canvas` is for fixed-coordinate drawing only.
- Generate a PPTX with paragraphs of text per slide — slides are for claims, not paragraphs. One clear job per slide.
- Fabricate stats, names, or testimonials in brand assets. Brand copy must be sourced from the brief; missing facts go to a gap list.

## Code Examples

### DOCX — structured report with `python-docx` 1.1+

```python
from docx import Document
from docx.shared import Pt, Inches, RGBColor
from docx.enum.text import WD_ALIGN_PARAGRAPH

doc = Document()
title = doc.add_heading('Q3 Performance Review', level=0)
title.alignment = WD_ALIGN_PARAGRAPH.CENTER
doc.add_paragraph('Prepared by: Operations Team').italic = True

doc.add_heading('Executive Summary', level=1)
doc.add_paragraph('Revenue grew 14% quarter-over-quarter, driven by enterprise renewals.')

# Styled runs
p = doc.add_paragraph()
p.add_run('Key metric: ').bold = True
p.add_run('$4.2M ARR').font.color.rgb = RGBColor(0x0D, 0x94, 0x88)

# Table with style
table = doc.add_table(rows=1, cols=3, style='Light Grid Accent 1')
hdr = table.rows[0].cells
hdr[0].text, hdr[1].text, hdr[2].text = 'Region', 'ARR', 'Growth'
for region, arr, growth in [('NA', '$2.1M', '+8%'), ('EMEA', '$1.4M', '+22%'), ('APAC', '$0.7M', '+12%')]:
    row = table.add_row().cells
    row[0].text, row[1].text, row[2].text = region, arr, growth

doc.add_picture('chart.png', width=Inches(5.5))

# Header + footer (section-level)
section = doc.sections[0]
section.footer.paragraphs[0].text = 'Confidential — Internal Use Only'
section.header.paragraphs[0].text = 'Q3 Performance Review'

doc.save('output/q3_review.docx')
```

### XLSX — formulas, styling, chart with `openpyxl` 3.1+

```python
from openpyxl import Workbook
from openpyxl.styles import Font, PatternFill, Border, Side, Alignment
from openpyxl.chart import BarChart, Reference
from openpyxl.utils import get_column_letter

wb = Workbook()
ws = wb.active
ws.title = 'Sales'

header_fill = PatternFill('solid', fgColor='0D9488')
header_font = Font(bold=True, color='FFFFFF')
thin = Side(style='thin', color='CCCCCC')
border = Border(left=thin, right=thin, top=thin, bottom=thin)

headers = ['Region', 'Q2 ARR', 'Q3 ARR', 'Growth', 'Growth %']
for col, h in enumerate(headers, start=1):
    c = ws.cell(row=1, column=col, value=h)
    c.fill, c.font, c.border, c.alignment = header_fill, header_font, border, Alignment(horizontal='center')

data = [('NA', 1_940_000, 2_100_000), ('EMEA', 1_150_000, 1_400_000), ('APAC', 620_000, 700_000)]
for i, (region, q2, q3) in enumerate(data, start=2):
    ws.cell(row=i, column=1, value=region).border = border
    ws.cell(row=i, column=2, value=q2).number_format = '$#,##0'
    ws.cell(row=i, column=3, value=q3).number_format = '$#,##0'
    ws.cell(row=i, column=4, value=f'=C{i}-B{i}').number_format = '$#,##0'
    ws.cell(row=i, column=5, value=f'=(C{i}-B{i})/B{i}').number_format = '0.0%'

total_row = len(data) + 2
ws.cell(row=total_row, column=1, value='Total').font = Font(bold=True)
ws.cell(row=total_row, column=2, value=f'=SUM(B2:B{total_row-1})').number_format = '$#,##0'
ws.cell(row=total_row, column=5, value=f'=(C{total_row}-B{total_row})/B{total_row}').number_format = '0.0%'

ws.freeze_panes = 'A2'
for col, width in enumerate([12, 14, 14, 14, 12], start=1):
    ws.column_dimensions[get_column_letter(col)].width = width

chart = BarChart()
chart.title = 'Q2 vs Q3 ARR by Region'
values = Reference(ws, min_col=2, max_col=3, min_row=1, max_row=total_row - 1)
cats = Reference(ws, min_col=1, min_row=2, max_row=total_row - 1)
chart.add_data(values, titles_from_data=True)
chart.set_categories(cats)
ws.add_chart(chart, 'G2')

wb.save('output/sales_report.xlsx')
```

### PDF — merge + watermark with `pypdf` 4+ + `reportlab`

```python
from pypdf import PdfReader, PdfWriter
from reportlab.pdfgen import canvas
from reportlab.lib.pagesizes import letter
from io import BytesIO

# 1. Build watermark page with reportlab
wm_buf = BytesIO()
c = canvas.Canvas(wm_buf, pagesize=letter)
c.setFont('Helvetica', 40)
c.setFillColorRGB(0.5, 0.5, 0.5, alpha=0.3)
c.translate(300, 400); c.rotate(45)
c.drawCentredString(0, 0, 'CONFIDENTIAL')
c.save()
wm_buf.seek(0)
wm_page = PdfReader(wm_buf).pages[0]

# 2. Merge watermark onto every page of source
reader = PdfReader('input/contract.pdf')
writer = PdfWriter()
for page in reader.pages:
    page.merge_page(wm_page)
    writer.add_page(page)

writer.add_metadata({'/Title': 'Contract — Confidential', '/Author': 'Legal Team'})
# writer.encrypt(user_password='', owner_password='owner-pass-2024')  # optional

with open('output/contract_watermarked.pdf', 'wb') as f:
    writer.write(f)

# 3. Verify
v = PdfReader('output/contract_watermarked.pdf')
print(f'Pages: {len(v.pages)}, Title: {v.metadata.get("/Title")}')
print(f'Sample: {v.pages[0].extract_text()[:200]}')
```

### PPTX — deck from claims with `python-pptx` 0.6+

```python
from pptx import Presentation
from pptx.util import Inches, Pt
from pptx.dml.color import RGBColor

prs = Presentation()
prs.slide_width = Inches(13.333)   # 16:9
prs.slide_height = Inches(7.5)
blank = prs.slide_layouts[6]

slides = [
    ('Q3 Performance Review', 'Operations Team — October 2024'),
    ('Revenue grew 14%', 'Driven by enterprise renewals and two new EMEA logos.'),
    ('NA: $2.1M ARR (+8%)', 'Stable enterprise base; one churn offset by expansion.'),
    ('EMEA: $1.4M ARR (+22%)', 'New logos in Germany and France; pipeline healthy.'),
    ('Next quarter', 'Focus on APAC pipeline conversion and EMEA expansion.'),
]
for title_text, body_text in slides:
    slide = prs.slides.add_slide(blank)
    tb = slide.shapes.add_textbox(Inches(1), Inches(0.5), Inches(11.3), Inches(1.5))
    p = tb.text_frame.paragraphs[0]
    p.text = title_text; p.font.size = Pt(40); p.font.bold = True
    p.font.color.rgb = RGBColor(0x0F, 0x17, 0x2A)
    bb = slide.shapes.add_textbox(Inches(1), Inches(2.5), Inches(11.3), Inches(4))
    bp = bb.text_frame.paragraphs[0]
    bp.text = body_text; bp.font.size = Pt(24); bp.font.color.rgb = RGBColor(0x47, 0x55, 0x69)
    slide.notes_slide.notes_text_frame.text = f'Talking points for: {title_text}'

prs.save('output/q3_review.pptx')
```

### Brand asset — SVG logo with `svgwrite` + rasterize with `cairosvg`

```python
import svgwrite
import cairosvg

# Monogram + meaning: 'A' (ascent) inside circle (system)
dwg = svgwrite.Drawing('output/logo.svg', profile='full', size=('240', '280'))
dwg.add(dwg.circle(center=(120, 120), r=110, fill='none', stroke='#0F172A', stroke_width=8))
dwg.add(dwg.polygon(points=[(120, 40), (200, 200), (40, 200)], fill='#0F172A'))
dwg.add(dwg.line(start=(80, 150), end=(160, 150), stroke='#FFFFFF', stroke_width=12))  # 'A' crossbar
dwg.add(dwg.text('ASCENT', insert=(120, 250), font_family='Inter, system-ui, sans-serif',
                 font_size=28, font_weight=700, text_anchor='middle', fill='#0F172A'))
dwg.save()

# Rasterize to PNG (social, favicon)
cairosvg.svg2png(url='output/logo.svg', write_to='output/logo.png', output_width=480, output_height=560)

# Horizontal lockup variant
lockup = svgwrite.Drawing('output/lockup-horizontal.svg', profile='full', size=('600', '240'))
lockup.add(lockup.circle(center=(120, 120), r=80, fill='none', stroke='#0F172A', stroke_width=6))
lockup.add(lockup.polygon(points=[(120, 60), (180, 180), (60, 180)], fill='#0F172A'))
lockup.add(lockup.line(start=(90, 150), end=(150, 150), stroke='#FFFFFF', stroke_width=8))
lockup.add(lockup.text('ASCENT', insert=(240, 130), font_family='Inter, system-ui, sans-serif',
                       font_size=56, font_weight=700, fill='#0F172A'))
lockup.add(lockup.text('Build with momentum.', insert=(240, 175),
                       font_family='Inter, system-ui, sans-serif', font_size=20, fill='#475569'))
lockup.save()
```

### Cross-format conversion — see `references/conversion-matrix.md` for the full DOCX↔PDF↔MD↔HTML, XLSX↔CSV↔JSON, PPTX→PDF matrix using `pandoc` and `libreoffice --headless --convert-to`.

## Output Template

When implementing a document task, deliver:

1. **Source-preserved output** — `output/<name>.<ext>` (or `<name>_edited.<ext>`); original untouched unless in-place edit was explicitly requested.
2. **Parser verification** — reopen with the appropriate parser (`python-docx`, `openpyxl`, `pypdf`, `python-pptx`); print paragraph/sheet/slide/page counts; extract representative text sample.
3. **Integrity check** — `unzip -t` for OOXML, `pdfinfo` for PDF, file size sanity.
4. **Render check (if renderer available)** — `libreoffice --headless --convert-to png|pdf` and inspect; if no renderer, mark visual QA as ASSUMED.
5. **Diff summary** — for edits, list what changed (paragraphs added/modified, cells updated, slides added, pages merged).
6. **Gap list** — missing facts the document needs from the owner (cut, not fabricated). Never invent stats, names, quotes.
7. **Limitations noted** — fonts not embedded, OCR quality unchecked, complex layout fidelity unverified.

## Knowledge Reference

**DOCX:** `python-docx` 1.1+ (`Document`, `add_paragraph`, `add_run`, `add_table`, `add_heading`, `sections`, `core_properties`); `pandoc` (`-f markdown -t docx`, `--reference-doc=template.docx`); OOXML namespaces (`w:`, `r:`, `wp:`); `unzip -t`; `.dotx` templates; mail merge (`docx-mailmerge`); tracked changes (`w:ins`/`w:del`); comments (`w:commentReference`). **XLSX:** `openpyxl` 3.1+ (`Workbook`, `load_workbook`, `Worksheet.cell`, `Font`/`PatternFill`/`Border`/`Alignment`, `Chart`/`Reference`, `conditional_formatting`, `DataValidation`, `freeze_panes`, `defined_names`); `pandas.read_excel`/`to_excel` (engines `openpyxl`/`xlsxwriter`); `xlsxwriter` (advanced charts, `add_format`, `write_url`); `csv` (`DictReader`/`DictWriter`); `tablib`. **PDF:** `pypdf` 4+ (`PdfReader`/`PdfWriter`, `merge_page`, `add_metadata`, `encrypt`, AcroForm via `/AcroForm`); `pdfplumber` (`extract_text`, `extract_tables`, `extract_words`); `pymupdf` (`fitz` — `open`, `page.get_text`, `page.get_images`); `reportlab` 4+ (`platypus.SimpleDocTemplate`, `Paragraph`, `Table`, `Spacer`, `PageBreak`, `canvas.Canvas`); `pandoc` (`-f markdown -t pdf --pdf-engine=xelatex`); `ocrmypdf` (`-l eng --rotate-pages --deskew`); `pdfinfo`/`pdftotext` (CLI). **PPTX:** `python-pptx` 0.6+ (`Presentation`, `slides.add_slide`, `slide_layouts`, `placeholders`, `shapes.add_textbox`, `add_table`, `add_chart`, `add_picture`, `notes_slide.notes_text_frame`); `.potx` templates; master slides via `slide_master`. **Brand:** `svgwrite` (`Drawing`, `add`, `circle`/`polygon`/`line`/`text`); `cairosvg` (`svg2png`, `svg2pdf`); `Pillow` (`Image.open`, `Image.quantize`, `Image.getcolors`); `collections.Counter` for color frequency. **Conversion:** `pandoc` (MD/HTML/DOCX/PDF/ODT/EPUB); `libreoffice --headless --convert-to pdf|docx|xlsx|png`; `csv`/`json`. **Verification:** `unzip -t`, `pdfinfo`, parser reopen, count assertions, text extraction round-trip.

## VERIFIED vs ASSUMED (honest exit)

Before reporting done, separate:

- **VERIFIED** — gates you actually ran with real output: parser reopened the file successfully, counts (paragraphs/sheets/slides/pages) match expectations, representative text extracted matches the source/intent, `unzip -t` passed (OOXML), `pdfinfo` confirmed valid PDF + page count, LibreOffice render export succeeded and was inspected (if a renderer was available).
- **ASSUMED** — anything you did not directly verify: visual appearance on other systems, font rendering fidelity (embedded fonts vs system fallbacks), complex layout preservation through conversion (DOCX→PDF can shift pagination), OCR accuracy on scanned PDFs (unless spot-checked), cross-version compatibility (Office 2019 vs 365 vs older), real print rendering.

Report both lists. "It opens fine" is not verification — open it with a parser and confirm. If a check was skipped (no renderer, no OCR tool, no time), say so. Never claim visual QA passed without inspecting a render.
