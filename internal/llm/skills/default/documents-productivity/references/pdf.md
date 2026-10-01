# PDF — pypdf, pdfplumber, pymupdf, reportlab, pandoc, ocrmypdf

Read, extract, split, merge, rotate, watermark, fill forms, OCR, and create PDFs. `pypdf` 4+ for manipulation; `pdfplumber` for text/tables; `pymupdf` for fast text+images; `reportlab` 4+ for creation; `pandoc` for MD→PDF; `ocrmypdf` for OCR. Verify with `pdfinfo` + text extraction round-trip.

## Library Selection

| Task | Library | Notes |
|---|---|---|
| Split, merge, rotate, watermark, encrypt | `pypdf` 4+ | Pure Python, no system deps |
| Form fill (AcroForm) | `pypdf` | Access `/AcroForm` field dictionary |
| Extract text (simple layout) | `pdfplumber` | Slow but accurate |
| Extract text + tables | `pdfplumber` | `extract_tables()` |
| Extract text + images (fast) | `pymupdf` (`fitz`) | C extension; 10x faster than pdfplumber |
| Create from scratch | `reportlab` 4+ | `platypus` for flowables, `canvas` for fixed-coordinate |
| Markdown → PDF | `pandoc` + LaTeX | `--pdf-engine=xelatex` (or `tectonic`) |
| OCR scanned pages | `ocrmypdf` | Wraps `tesseract`; adds text layer |
| Page render to PNG | `pymupdf` or `pdftoppm` | For visual QA |
| Inspect metadata | `pdfinfo` (CLI) | Page count, title, author, encrypted flag |

## Read / Extract

### pdfplumber — text + tables

```python
import pdfplumber

with pdfplumber.open('input/report.pdf') as pdf:
    print(f'Pages: {len(pdf.pages)}')
    print(f'Metadata: {pdf.metadata}')

    # Per-page text
    for i, page in enumerate(pdf.pages):
        text = page.extract_text() or ''
        print(f'\n--- Page {i+1} (chars: {len(text)}) ---')
        print(text[:200])

    # Extract a table from a specific page
    page = pdf.pages[0]
    tables = page.extract_tables()
    for table in tables:
        for row in table:
            print(row)

    # Word-level with coordinates (for layout analysis)
    words = page.extract_words()
    print(f'First 5 words: {words[:5]}')
```

### pymupdf (fitz) — fast text + images

```python
import fitz  # pymupdf

doc = fitz.open('input/report.pdf')
print(f'Pages: {len(doc)}')
print(f'Metadata: {doc.metadata}')

for i, page in enumerate(doc):
    text = page.get_text()
    print(f'Page {i+1}: {len(text)} chars')

    # Images on the page
    images = page.get_images(full=True)
    for img_index, img in enumerate(images):
        xref = img[0]
        pix = fitz.Pixmap(doc, xref)
        if pix.n > 4:  # CMYK → RGB
            pix = fitz.Pixmap(fitz.csRGB, pix)
        pix.save(f'output/page{i+1}_img{img_index}.png')

    # Render page to PNG (for visual QA)
    pix = page.get_pixmap(dpi=150)
    pix.save(f'output/page{i+1}.png')

doc.close()
```

## Split, Merge, Rotate

```python
from pypdf import PdfReader, PdfWriter

# Split — extract pages 5-10 into a new PDF
reader = PdfReader('input/big.pdf')
writer = PdfWriter()
for i in range(4, 10):  # 0-indexed
    writer.add_page(reader.pages[i])
writer.add_metadata({'/Title': 'Extracted pages 5-10'})
with open('output/extract.pdf', 'wb') as f:
    writer.write(f)

# Merge — concatenate multiple PDFs
writer = PdfWriter()
for path in ['input/part1.pdf', 'input/part2.pdf', 'input/part3.pdf']:
    reader = PdfReader(path)
    for page in reader.pages:
        writer.add_page(page)
with open('output/merged.pdf', 'wb') as f:
    writer.write(f)

# Rotate every page 90° clockwise
reader = PdfReader('input/landscape.pdf')
writer = PdfWriter()
for page in reader.pages:
    page.rotate(90)   # 90, 180, 270
    writer.add_page(page)
with open('output/portrait.pdf', 'wb') as f:
    writer.write(f)

# Append an existing PDF to another (preserves bookmarks)
from pypdf import PdfMerger  # deprecated in 4.x — use PdfWriter.append
writer = PdfWriter()
writer.append('input/cover.pdf')
writer.append('input/body.pdf')
writer.write('output/full.pdf')   # writer.write(file) — file is a path or file-like
```

## Watermark / Stamp

```python
from pypdf import PdfReader, PdfWriter
from reportlab.pdfgen import canvas
from reportlab.lib.pagesizes import letter
from io import BytesIO

# 1. Build a one-page watermark with reportlab
wm_buf = BytesIO()
c = canvas.Canvas(wm_buf, pagesize=letter)
c.saveState()
c.setFont('Helvetica', 40)
c.setFillColorRGB(0.5, 0.5, 0.5, alpha=0.3)
c.translate(300, 400)
c.rotate(45)
c.drawCentredString(0, 0, 'CONFIDENTIAL')
c.restoreState()
c.save()
wm_buf.seek(0)
wm_page = PdfReader(wm_buf).pages[0]

# 2. Overlay watermark on every page of source
reader = PdfReader('input/contract.pdf')
writer = PdfWriter()
for page in reader.pages:
    page.merge_page(wm_page)   # overwrites on top
    writer.add_page(page)

writer.add_metadata({'/Title': 'Contract — Confidential'})
with open('output/contract_watermarked.pdf', 'wb') as f:
    writer.write(f)
```

## AcroForm Filling

```python
from pypdf import PdfReader, PdfWriter

reader = PdfReader('input/form.pdf')
writer = PdfWriter()
writer.append(reader)   # copy all pages

# Inspect fields
fields = reader.get_fields()
print('Fields:', list(fields.keys()))
for name, field in fields.items():
    print(f'  {name}: type={field.get("/FT")}, value={field.get("/V")}')

# Fill fields
writer.update_page_form_field_values(
    writer.pages[0],
    {
        'name': 'Jane Harrison',
        'email': 'jane@example.com',
        'date': '2024-10-15',
        'agree': '/Yes',   # checkbox — name of the "on" state
    },
)

# Flatten (so the form is no longer editable) — set NeedAppearances to false
writer.catalog['/AcroForm'].update({'/NeedAppearances': False})

with open('output/form_filled.pdf', 'wb') as f:
    writer.write(f)
```

## Create from Scratch with `reportlab` 4+

```python
from reportlab.lib.pagesizes import letter
from reportlab.lib.styles import getSampleStyleSheet, ParagraphStyle
from reportlab.lib.units import inch
from reportlab.lib.colors import HexColor
from reportlab.lib.enums import TA_CENTER, TA_LEFT
from reportlab.platypus import (
    SimpleDocTemplate, Paragraph, Spacer, Table, TableStyle,
    PageBreak, Image, ListFlowable, ListItem,
)

doc = SimpleDocTemplate(
    'output/report.pdf', pagesize=letter,
    leftMargin=0.75*inch, rightMargin=0.75*inch,
    topMargin=0.75*inch, bottomMargin=0.75*inch,
    title='Q3 Performance Review', author='Operations Team',
)

styles = getSampleStyleSheet()
title_style = ParagraphStyle('Title', parent=styles['Title'],
                              fontSize=24, leading=28, alignment=TA_CENTER,
                              textColor=HexColor('#0F172A'))
body_style = ParagraphStyle('Body', parent=styles['BodyText'],
                             fontSize=11, leading=15, alignment=TA_LEFT,
                             textColor=HexColor('#1F2937'))

story = []
story.append(Paragraph('Q3 Performance Review', title_style))
story.append(Spacer(1, 0.2*inch))
story.append(Paragraph('Operations Team — October 2024', body_style))
story.append(Spacer(1, 0.3*inch))

story.append(Paragraph('Executive Summary', styles['Heading1']))
story.append(Paragraph(
    'Revenue grew <b>14%</b> quarter-over-quarter, driven by enterprise renewals '
    'and two new logos in the EMEA region.',
    body_style,
))

# Table
data = [
    ['Region', 'Q2 ARR', 'Q3 ARR', 'Growth %'],
    ['NA',   '$1.94M', '$2.10M', '+8%'],
    ['EMEA', '$1.15M', '$1.40M', '+22%'],
    ['APAC', '$0.62M', '$0.70M', '+12%'],
]
table = Table(data, colWidths=[1.2*inch, 1.2*inch, 1.2*inch, 1.2*inch])
table.setStyle(TableStyle([
    ('BACKGROUND', (0, 0), (-1, 0), HexColor('#0D9488')),
    ('TEXTCOLOR', (0, 0), (-1, 0), HexColor('#FFFFFF')),
    ('FONTNAME', (0, 0), (-1, 0), 'Helvetica-Bold'),
    ('FONTSIZE', (0, 0), (-1, -1), 10),
    ('ALIGN', (1, 0), (-1, -1), 'RIGHT'),
    ('GRID', (0, 0), (-1, -1), 0.5, HexColor('#CCCCCC')),
    ('ROWBACKGROUNDS', (0, 1), (-1, -1), [HexColor('#FFFFFF'), HexColor('#F9FAFB')]),
]))
story.append(table)
story.append(Spacer(1, 0.3*inch))

# Bullet list
bullets = [
    ListItem(Paragraph('NA: stable enterprise base', body_style)),
    ListItem(Paragraph('EMEA: new logos in DE and FR', body_style)),
    ListItem(Paragraph('APAC: Japan renewal closed', body_style)),
]
story.append(ListFlowable(bullets, bulletType='bullet'))

# Image
story.append(Spacer(1, 0.3*inch))
story.append(Image('chart.png', width=5*inch, height=3*inch))

# Page break + new section
story.append(PageBreak())
story.append(Paragraph('Next Quarter', styles['Heading1']))
story.append(Paragraph('Focus on APAC pipeline conversion and EMEA expansion.', body_style))

doc.build(story)
```

Use `canvas.Canvas` ONLY for fixed-coordinate drawing (forms, certificates, badges). For flowing text documents, always use `platypus` (above).

## OCR (scanned PDFs)

```python
import pdfplumber

# Detect: scanned pages have ~0 extractable text
with pdfplumber.open('input/scanned.pdf') as pdf:
    for i, page in enumerate(pdf.pages):
        text = page.extract_text() or ''
        if len(text.strip()) < 50:
            print(f'Page {i+1}: likely scanned (only {len(text)} chars)')
```

```bash
# OCR with ocrmypdf (wraps tesseract) — adds a text layer to a scanned PDF
ocrmypdf input/scanned.pdf output/scanned_ocr.pdf -l eng --rotate-pages --deskew --force-ocr

# --force-ocr: re-OCR even if a text layer exists
# --skip-text: only OCR pages with no text layer (default for mixed PDFs)
# -l eng+fra: multiple languages
# --output-type pdfa: PDF/A archival
```

```python
import subprocess
result = subprocess.run([
    'ocrmypdf', 'input/scanned.pdf', 'output/scanned_ocr.pdf',
    '-l', 'eng', '--rotate-pages', '--deskew',
], capture_output=True, text=True, timeout=600)
if result.returncode != 0:
    print(f'OCR failed: {result.stderr}')
```

Spot-check OCR accuracy on a sample page before claiming the text is fully accurate.

## Pandoc (Markdown → PDF)

```bash
# MD → PDF via LaTeX (requires xelatex / tectonic / lualatex installed)
pandoc input/report.md -o output/report.pdf --pdf-engine=xelatex

# With a template / styling
pandoc input/report.md -o output/report.pdf \
  --pdf-engine=xelatex \
  -V geometry:margin=1in \
  -V mainfont="Inter" \
  -V monofont="JetBrains Mono" \
  -V colorlinks=true \
  --toc --toc-depth=2

# If no LaTeX: use weasyprint or wkhtmltopdf as the engine
pandoc input/report.md -o output/report.pdf --pdf-engine=weasyprint
```

## Verification

```bash
# Confirm valid PDF + page count + metadata
pdfinfo output/report.pdf

# Text extraction round-trip — confirm expected content
pdftotext output/report.pdf - | head -50
# OR with python:
python -c "import pdfplumber; pdf=pdfplumber.open('output/report.pdf'); print(f'Pages: {len(pdf.pages)}'); print(pdf.pages[0].extract_text()[:200])"
```

```python
# Comprehensive verification
import subprocess
from pypdf import PdfReader
import pdfplumber

pdf_path = 'output/report.pdf'

# 1. pdfinfo — valid PDF + page count
result = subprocess.run(['pdfinfo', pdf_path], capture_output=True, text=True)
print('pdfinfo:' )
print(result.stdout)
assert result.returncode == 0, f'pdfinfo failed: {result.stderr}'

# 2. pypdf — reopen + page count
reader = PdfReader(pdf_path)
print(f'pypdf pages: {len(reader.pages)}')
print(f'pypdf metadata: {reader.metadata}')

# 3. pdfplumber — text extraction round-trip
with pdfplumber.open(pdf_path) as pdf:
    print(f'pdfplumber pages: {len(pdf.pages)}')
    sample = pdf.pages[0].extract_text() or ''
    print(f'Sample text (first 200 chars): {sample[:200]}')
    assert len(sample) > 0, 'No text extracted — possible scan or empty page'

# 4. File size sanity
import os
size_kb = os.path.getsize(pdf_path) / 1024
print(f'File size: {size_kb:.1f} KB')
```

## Common Gotchas

- **`pdftotext` is a CLI** (poppler-utils), `pdfplumber` is a Python library — different APIs, different output. Don't conflate them.
- **`pypdf.PdfReader` reads; `pypdf.PdfWriter` writes.** Never confuse them. To modify, copy pages from reader to writer.
- **`data_only` doesn't exist for PDF** — that's an openpyxl concept. For PDFs, "cached values" means whatever text the PDF contains.
- **AcroForm vs XFA** — `pypdf` supports AcroForm (the standard). XFA (Adobe XML forms) are not well-supported; you may need `pdftk` or Adobe tools.
- **`reportlab` `canvas` vs `platypus`** — `canvas` is for fixed-coordinate drawing (one page at a time, manual layout). `platypus` (SimpleDocTemplate + flowables) handles flowing text, automatic page breaks, tables. Use `platypus` for documents.
- **Pandoc PDF requires LaTeX** — install `texlive-xetex` / `tectonic` / `miktex`. Without LaTeX, use `--pdf-engine=weasyprint` (HTML/CSS rendering) or `--pdf-engine=wkhtmltopdf`.
- **Encrypted PDFs** — `PdfReader('encrypted.pdf', password='...')`. If owner-password only, `pypdf` can often read without it.
- **`pdfplumber` is slow** on large PDFs (100+ pages). For bulk extraction, use `pymupdf`.
- **OCR is not 100% accurate** — always spot-check. `ocrmypdf --skip-text` preserves existing text layers; `--force-ocr` re-OCRs everything (may degrade existing good text).
- **PDFs don't have "tables"** — `pdfplumber.extract_tables()` infers them from ruling lines / whitespace alignment. Accuracy varies by document.
