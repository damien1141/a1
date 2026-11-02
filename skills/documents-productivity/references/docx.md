# DOCX (Word) — python-docx, pandoc, OOXML

Create, edit, inspect, and convert Word documents. `python-docx` 1.1+ for structured create/edit; `pandoc` for Markdown↔DOCX; OOXML `unzip` for low-level fixes. Preserve originals — write edited copies.

## Create with `python-docx` 1.1+

```python
from docx import Document
from docx.shared import Pt, Inches, RGBColor
from docx.enum.text import WD_ALIGN_PARAGRAPH

doc = Document()

# Title (level 0 = Title style)
title = doc.add_heading('Q3 Performance Review', level=0)
title.alignment = WD_ALIGN_PARAGRAPH.CENTER

# Headings (level 1-9)
doc.add_heading('Executive Summary', level=1)
doc.add_paragraph('Revenue grew 14% quarter-over-quarter.')

# Styled runs (a paragraph contains runs; runs hold formatting)
p = doc.add_paragraph()
p.add_run('Key metric: ').bold = True
p.add_run('$4.2M ARR').font.color.rgb = RGBColor(0x0D, 0x94, 0x88)
p.add_run(' (up from $3.7M).').italic = True

# Bullet list
for item in ['NA: $2.1M (+8%)', 'EMEA: $1.4M (+22%)', 'APAC: $0.7M (+12%)']:
    doc.add_paragraph(item, style='List Bullet')

# Numbered list
for step in ['Collect source data', 'Verify counts', 'Send to ops team']:
    doc.add_paragraph(step, style='List Number')

# Table with style
table = doc.add_table(rows=1, cols=3, style='Light Grid Accent 1')
hdr = table.rows[0].cells
hdr[0].text, hdr[1].text, hdr[2].text = 'Region', 'ARR', 'Growth'
for region, arr, growth in [('NA', '$2.1M', '+8%'), ('EMEA', '$1.4M', '+22%')]:
    row = table.add_row().cells
    row[0].text, row[1].text, row[2].text = region, arr, growth

# Image
doc.add_picture('chart.png', width=Inches(5.5))

# Page break
doc.add_page_break()

# Section-level header + footer
section = doc.sections[0]
section.footer.paragraphs[0].text = 'Confidential — Internal Use Only'
section.header.paragraphs[0].text = 'Q3 Performance Review'

# Metadata
doc.core_properties.author = 'Operations Team'
doc.core_properties.title = 'Q3 Performance Review'
doc.core_properties.comments = 'Draft — not for distribution'

doc.save('output/q3_review.docx')
```

## Edit Existing DOCX (preserve formatting)

```python
from docx import Document

doc = Document('input/existing.docx')

# Read paragraphs
for i, para in enumerate(doc.paragraphs):
    print(f'{i}: [{para.style.name}] {para.text[:80]}')

# Find and replace text in runs (preserves run formatting)
def replace_in_paragraph(para, old: str, new: str) -> bool:
    if old not in para.text:
        return False
    # Simple case: text is in a single run
    for run in para.runs:
        if old in run.text:
            run.text = run.text.replace(old, new)
            return True
    # Hard case: text spans multiple runs — rebuild
    full = ''.join(r.text for r in para.runs)
    if old in full:
        full = full.replace(old, new)
        # Clear all runs, set text on first
        for i, run in enumerate(para.runs):
            if i == 0:
                run.text = full
            else:
                run.text = ''
        return True
    return False

for para in doc.paragraphs:
    replace_in_paragraph(para, 'ACME Corp.', 'Harrison Roofing')

# Add a paragraph at a specific location
from docx.oxml.ns import qn
new_p = doc.add_paragraph('Inserted at end.')
# To insert elsewhere: manipulate the XML element's position via _element.addnext() / addprevious()

doc.save('output/existing_edited.docx')
```

## Read DOCX Structure

```python
from docx import Document

doc = Document('input/report.docx')

print(f'Paragraphs: {len(doc.paragraphs)}')
print(f'Tables: {len(doc.tables)}')
print(f'Sections: {len(doc.sections)}')

# All paragraphs (including those inside tables)
for table in doc.tables:
    for row in table.rows:
        for cell in row.cells:
            for para in cell.paragraphs:
                print(f'[cell] {para.text[:60]}')

# Inline shapes (images)
print(f'Inline shapes: {len(doc.inline_shapes)}')
for shape in doc.inline_shapes:
    print(f'  type={shape.type}, width={shape.width}, height={shape.height}')

# Headers/footers per section
for i, section in enumerate(doc.sections):
    print(f'Section {i}: header="{section.header.paragraphs[0].text}", footer="{section.footer.paragraphs[0].text}"')

# Core properties
print(f'Author: {doc.core_properties.author}, Title: {doc.core_properties.title}')
```

## Pandoc — Markdown ↔ DOCX

```bash
# MD → DOCX with a reference template (preserves styles)
pandoc input/report.md -o output/report.docx --reference-doc=templates/corporate.docx

# DOCX → MD (lossy — complex formatting may drop)
pandoc input/report.docx -o output/report.md --wrap=none

# HTML → DOCX
pandoc input/report.html -o output/report.docx

# DOCX → PDF (requires LaTeX: xelatex / tectonic)
pandoc input/report.docx -o output/report.pdf --pdf-engine=xelatex

# Combine multiple MD files into one DOCX with TOC
pandoc chapter1.md chapter2.md chapter3.md -o output/book.docx --toc --toc-depth=2
```

`--reference-doc` carries styles (Heading 1, Heading 2, Body Text, etc.) from the template into the output. Author the template in Word first, run `pandoc --print-default-data-file reference.docx > template.docx` to bootstrap.

## OOXML Inspection (low-level fixes)

`.docx` is a ZIP of XML. Inspect with `unzip`:

```bash
unzip -l input.docx                    # list contents
unzip -p input.docx word/document.xml | head -100   # main document body
unzip -p input.docx word/styles.xml | head -100     # style definitions
unzip -p input.docx word/header1.xml | head -100    # header
unzip -p input.docx word/footer1.xml | head -100    # footer
unzip -p input.docx docProps/core.xml               # metadata
unzip -p input.docx docProps/app.xml                # app-specific (Word count, etc.)
```

Low-level edit (last resort — `python-docx` is preferred):

```bash
mkdir docx_extract && cd docx_extract
unzip -q ../input.docx
# Edit word/document.xml with your XML editor (preserve namespaces!)
# Re-zip (must use ZIP_STORED for media, ZIP_DEFLATED for XML)
zip -r ../output.docx . -x ".*"
cd ..
unzip -t output.docx          # VERIFY integrity — corrupt OOXML is the #1 failure mode
```

Useful namespaces:
- `w:` — main document (`word/document.xml`)
- `r:` — relationships
- `wp:` — drawing (images)
- `a:` — drawingML
- `pic:` — picture
- `mc:` — markup compatibility

## Tracked Changes & Comments

`python-docx` does not have a high-level tracked-changes API. Access via XML:

```python
from docx import Document
from docx.oxml.ns import qn

doc = Document('input/tracked.docx')

# Find all insertions (w:ins) and deletions (w:del)
for ins in doc.element.body.iter(qn('w:ins')):
    author = ins.get(qn('w:author'))
    date = ins.get(qn('w:date'))
    text = ''.join(t.text or '' for t in ins.iter(qn('w:t')))
    print(f'INS by {author} on {date}: {text}')

for dele in doc.element.body.iter(qn('w:del')):
    author = dele.get(qn('w:author'))
    text = ''.join(t.text or '' for t in dele.iter(qn('w:delText')))
    print(f'DEL by {author}: {text}')

# Comments live in word/comments.xml — access via doc.part.package.parts
```

To accept all changes via OOXML: strip `w:ins` (keep content) and `w:del` (remove content). Or use `pandoc input.docx -o output.docx --track-changes=accept` (also `reject` or `all`).

## Mail Merge

Option A — `.dotx` template with merge fields + `docx-mailmerge`:

```bash
pip install docx-mailmerge
```

```python
from mailmerge import MailMerge

template = MailMerge('templates/letter.dotx')
template.merge(
    name='Jane Harrison',
    address='123 Main St, Chicago, IL 60601',
    amount='$4,200',
    due_date='November 15, 2024',
)
template.write('output/letter_jane.docx')
```

Option B — bulk merge from CSV:

```python
import csv
from mailmerge import MailMerge

with open('recipients.csv') as f:
    rows = list(csv.DictReader(f))

for row in rows:
    doc = MailMerge('templates/letter.dotx')
    doc.merge(**row)
    doc.write(f"output/letter_{row['id']}.docx")
```

Merge fields in the `.dotx` are `<<field_name>>` placeholders (Word's Insert → Quick Parts → Field → MergeField).

## Templates (.dotx)

`.dotx` is a Word template (same OOXML structure, different extension). To create a document from a template:

```python
from docx import Document
# Note: python-docx does NOT natively load .dotx as a template starter.
# Workaround: open the .dotx, save as .docx, then edit.
template = Document('templates/corporate.dotx')
template.save('output/new_doc.docx')   # now a regular .docx
# Edit the new doc
doc = Document('output/new_doc.docx')
doc.add_paragraph('New content.')
doc.save('output/new_doc.docx')
```

Or use `docx-mailmerge` (above) for proper template-driven generation.

## Verification

```python
from docx import Document
import subprocess

# Reopen with parser
doc = Document('output/q3_review.docx')
print(f'Paragraphs: {len(doc.paragraphs)}')
print(f'Tables: {len(doc.tables)}')
print(f'Inline shapes: {len(doc.inline_shapes)}')
print(f'Sections: {len(doc.sections)}')
print(f'First 5 paragraphs:')
for p in doc.paragraphs[:5]:
    print(f'  [{p.style.name}] {p.text[:80]}')

# Integrity check (OOXML is a ZIP)
result = subprocess.run(['unzip', '-t', 'output/q3_review.docx'], capture_output=True, text=True)
print('ZIP OK' if result.returncode == 0 else f'ZIP CORRUPT: {result.stderr}')
```

```bash
# Optional: render to PDF for visual check
libreoffice --headless --convert-to pdf --outdir output/ output/q3_review.docx
pdfinfo output/q3_review.pdf   # confirm PDF page count
```

## Common Gotchas

- **`python-docx` does not parse HTML** — for HTML→DOCX use `pandoc` or `docx2python`.
- **Run-level formatting** — text spanning multiple runs (common in edited documents) loses formatting when you do `para.text = ...`. Always edit run-by-run.
- **`add_paragraph` style must exist** — `'List Bullet'`, `'List Number'`, `'Title'`, `'Heading 1'` etc. are built-in; custom styles must be in the document or template.
- **Section breaks** — `doc.add_section()` starts a new section with its own headers/footers/page setup. Default is one section.
- **Images** — `add_picture` only takes a file path or file-like object. To embed from a URL, fetch first.
- **`python-docx` does not support equations, SmartArt, or charts** — for those, use `pandoc` or OOXML editing.
- **Pandoc round-trip is lossy** — DOCX→MD→DOCX loses revision history, comments, and some formatting. Always keep the original.
