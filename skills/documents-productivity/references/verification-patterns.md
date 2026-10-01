# Verification Patterns

Every document output must be verified — never claim "looks good" without inspecting a render. Patterns for OOXML (DOCX/XLSX/PPTX), PDF, CSV, and brand assets.

## Universal Pattern

1. **Reopen with parser** — `python-docx` / `openpyxl` / `pypdf` / `python-pptx` must load the file without error.
2. **Check counts** — paragraphs / sheets / slides / pages match expectations.
3. **Extract representative text** — confirm expected content is present.
4. **Integrity check** — `unzip -t` for OOXML, `pdfinfo` for PDF, file size sanity.
5. **Render check (if renderer available)** — `libreoffice --headless --convert-to png|pdf` and inspect.
6. **Diff summary** (for edits) — list what changed.
7. **Limitations noted** — what could NOT be verified (visual QA, cross-version, font fallback).

## DOCX Verification

```python
from docx import Document
import subprocess

def verify_docx(path: str, expected_paragraphs: int | None = None) -> dict:
    """Verify a DOCX file. Returns a dict of checks."""
    checks = {}

    # 1. Parser reopens successfully
    try:
        doc = Document(path)
        checks['parser'] = 'OK'
    except Exception as e:
        return {'parser': f'FAIL: {e}'}

    # 2. Counts
    checks['paragraphs'] = len(doc.paragraphs)
    checks['tables'] = len(doc.tables)
    checks['sections'] = len(doc.sections)
    checks['inline_shapes'] = len(doc.inline_shapes)
    if expected_paragraphs is not None:
        checks['paragraphs_match'] = checks['paragraphs'] == expected_paragraphs

    # 3. Representative text — first 5 paragraphs
    checks['sample_paragraphs'] = [
        {'style': p.style.name, 'text': p.text[:80]}
        for p in doc.paragraphs[:5]
    ]

    # 4. Metadata
    checks['author'] = doc.core_properties.author
    checks['title'] = doc.core_properties.title

    # 5. Headers / footers per section
    checks['sections_header_footer'] = [
        {
            'header': s.header.paragraphs[0].text if s.header.paragraphs else '',
            'footer': s.footer.paragraphs[0].text if s.footer.paragraphs else '',
        }
        for s in doc.sections
    ]

    # 6. Integrity check (OOXML is a ZIP)
    result = subprocess.run(['unzip', '-t', path], capture_output=True, text=True)
    checks['zip_integrity'] = 'OK' if result.returncode == 0 else f'FAIL: {result.stderr}'

    return checks

result = verify_docx('output/q3_review.docx', expected_paragraphs=20)
for k, v in result.items():
    print(f'{k}: {v}')
```

```bash
# CLI verification
unzip -t output/q3_review.docx                 # integrity
libreoffice --headless --convert-to pdf --outdir output/ output/q3_review.docx   # render
pdfinfo output/q3_review.pdf                   # confirm page count
```

## XLSX Verification

```python
from openpyxl import load_workbook
import subprocess

def verify_xlsx(path: str, expected_sheets: list[str] | None = None) -> dict:
    checks = {}

    # 1. Parser reopens
    try:
        wb = load_workbook(path, data_only=False)
        checks['parser'] = 'OK'
    except Exception as e:
        return {'parser': f'FAIL: {e}'}

    # 2. Sheet names
    checks['sheets'] = wb.sheetnames
    if expected_sheets is not None:
        checks['sheets_match'] = set(expected_sheets).issubset(set(wb.sheetnames))

    # 3. Per-sheet dimensions + a sample formula
    checks['per_sheet'] = []
    for name in wb.sheetnames:
        ws = wb[name]
        sample = {
            'name': name,
            'dimensions': ws.dimensions,
            'max_row': ws.max_row,
            'max_col': ws.max_column,
            'freeze_panes': ws.freeze_panes,
            'defined_names': list(ws.defined_names) if hasattr(ws, 'defined_names') else [],
        }
        # Sample first 3 cells of row 1
        if ws.max_row >= 1 and ws.max_col >= 1:
            sample['header_row'] = [ws.cell(row=1, column=c).value for c in range(1, min(4, ws.max_col + 1))]
        # Find a formula cell
        for row in ws.iter_rows(min_row=1, max_row=min(20, ws.max_row)):
            for cell in row:
                if cell.value and isinstance(cell.value, str) and cell.value.startswith('='):
                    sample['sample_formula'] = f'{cell.coordinate}: {cell.value}'
                    break
            else:
                continue
            break
        checks['per_sheet'].append(sample)

    # 4. Charts count (per sheet)
    checks['charts'] = {}
    for name in wb.sheetnames:
        ws = wb[name]
        checks['charts'][name] = len(ws._charts) if hasattr(ws, '_charts') else 0

    # 5. Integrity
    result = subprocess.run(['unzip', '-t', path], capture_output=True, text=True)
    checks['zip_integrity'] = 'OK' if result.returncode == 0 else f'FAIL: {result.stderr}'

    return checks

result = verify_xlsx('output/sales_report.xlsx', expected_sheets=['Sales', 'Notes'])
import json
print(json.dumps(result, indent=2, default=str))
```

```bash
unzip -t output/sales_report.xlsx
libreoffice --headless --convert-to pdf --outdir output/ output/sales_report.xlsx
pdfinfo output/sales_report.pdf
```

## PDF Verification

```python
import subprocess
import os
from pypdf import PdfReader
import pdfplumber

def verify_pdf(path: str, expected_pages: int | None = None) -> dict:
    checks = {}

    # 1. pdfinfo — valid PDF header + page count
    result = subprocess.run(['pdfinfo', path], capture_output=True, text=True)
    if result.returncode != 0:
        return {'pdfinfo': f'FAIL: {result.stderr}'}
    checks['pdfinfo'] = result.stdout
    # Parse page count
    for line in result.stdout.splitlines():
        if line.startswith('Pages:'):
            checks['pages'] = int(line.split(':')[1].strip())
    if expected_pages is not None:
        checks['pages_match'] = checks.get('pages') == expected_pages

    # 2. pypdf reopens
    try:
        reader = PdfReader(path)
        checks['pypdf_pages'] = len(reader.pages)
        checks['pypdf_metadata'] = dict(reader.metadata) if reader.metadata else {}
    except Exception as e:
        checks['pypdf'] = f'FAIL: {e}'

    # 3. pdfplumber text extraction round-trip
    try:
        with pdfplumber.open(path) as pdf:
            checks['pdfplumber_pages'] = len(pdf.pages)
            sample_text = pdf.pages[0].extract_text() or ''
            checks['sample_text_first_200'] = sample_text[:200]
            checks['sample_text_length'] = len(sample_text)
            # Check all pages have some text (unless scanned)
            empty_pages = [i+1 for i, p in enumerate(pdf.pages) if not (p.extract_text() or '').strip()]
            checks['empty_pages'] = empty_pages
            if empty_pages:
                checks['warning'] = f'Pages with no extractable text: {empty_pages}. May be scanned — consider ocrmypdf.'
    except Exception as e:
        checks['pdfplumber'] = f'FAIL: {e}'

    # 4. File size
    size_kb = os.path.getsize(path) / 1024
    checks['file_size_kb'] = round(size_kb, 1)
    if size_kb > 50_000:
        checks['warning_size'] = f'Large PDF ({size_kb/1024:.1f} MB) — consider optimizing images.'

    return checks

result = verify_pdf('output/report.pdf', expected_pages=10)
import json
print(json.dumps(result, indent=2, default=str))
```

## PPTX Verification

```python
from pptx import Presentation
import subprocess

def verify_pptx(path: str, expected_slides: int | None = None) -> dict:
    checks = {}

    try:
        prs = Presentation(path)
        checks['parser'] = 'OK'
    except Exception as e:
        return {'parser': f'FAIL: {e}'}

    checks['slides'] = len(prs.slides)
    checks['slide_width'] = prs.slide_width
    checks['slide_height'] = prs.slide_height
    if expected_slides is not None:
        checks['slides_match'] = checks['slides'] == expected_slides

    # Per-slide summary
    checks['per_slide'] = []
    for i, slide in enumerate(prs.slides):
        text_chunks = []
        table_count = 0
        image_count = 0
        chart_count = 0
        for shape in slide.shapes:
            if shape.has_text_frame:
                text_chunks.append(shape.text_frame.text[:60])
            if shape.has_table:
                table_count += 1
            if shape.shape_type == 13:  # PICTURE
                image_count += 1
            if shape.has_chart:
                chart_count += 1
        notes = ''
        if slide.has_notes_slide:
            notes = slide.notes_slide.notes_text_frame.text[:60]
        checks['per_slide'].append({
            'index': i + 1,
            'layout': slide.slide_layout.name,
            'text': text_chunks,
            'tables': table_count,
            'images': image_count,
            'charts': chart_count,
            'notes': notes,
        })

    # Integrity
    result = subprocess.run(['unzip', '-t', path], capture_output=True, text=True)
    checks['zip_integrity'] = 'OK' if result.returncode == 0 else f'FAIL: {result.stderr}'

    return checks

result = verify_pptx('output/q3_review.pptx', expected_slides=6)
import json
print(json.dumps(result, indent=2, default=str))
```

```bash
unzip -t output/q3_review.pptx
libreoffice --headless --convert-to pdf --outdir output/ output/q3_review.pptx
pdfinfo output/q3_review.pdf   # page count should match slide count
```

## CSV / TSV Verification

```python
import csv

def verify_csv(path: str, delimiter: str = ',') -> dict:
    checks = {}
    with open(path, newline='', encoding='utf-8') as f:
        reader = csv.reader(f, delimiter=delimiter)
        rows = list(reader)

    checks['rows'] = len(rows)
    if rows:
        checks['columns'] = len(rows[0])
        checks['header'] = rows[0]
        # Check all rows have same column count
        col_counts = {len(r) for r in rows}
        checks['column_consistency'] = len(col_counts) == 1
        if len(col_counts) > 1:
            checks['column_counts'] = sorted(col_counts)
        # Sample first 3 data rows
        checks['sample_rows'] = rows[1:4]
    return checks

result = verify_csv('output/data.csv')
print(result)
```

## Brand Asset Verification

```python
from PIL import Image
import xml.etree.ElementTree as ET
import os

def verify_brand_assets(output_dir: str) -> dict:
    checks = {}
    d = output_dir

    # 1. SVG files are well-formed XML
    svg_files = [f for f in os.listdir(d) if f.endswith('.svg')]
    checks['svg_files'] = {}
    for svg in svg_files:
        try:
            ET.parse(os.path.join(d, svg))
            checks['svg_files'][svg] = 'valid XML'
        except ET.ParseError as e:
            checks['svg_files'][svg] = f'MALFORMED: {e}'

    # 2. PNG files open and have dimensions
    png_files = [f for f in os.listdir(d) if f.endswith('.png')]
    checks['png_files'] = {}
    for png in png_files:
        try:
            img = Image.open(os.path.join(d, png))
            checks['png_files'][png] = {'size': img.size, 'mode': img.mode}
        except Exception as e:
            checks['png_files'][png] = f'FAIL: {e}'

    # 3. File sizes are reasonable (no zero-byte files, no absurdly large)
    checks['file_sizes'] = {}
    for f in os.listdir(d):
        path = os.path.join(d, f)
        if os.path.isfile(path):
            size_kb = os.path.getsize(path) / 1024
            checks['file_sizes'][f] = f'{size_kb:.1f} KB'
            if size_kb < 1:
                checks['file_sizes'][f] += ' [SUSPICIOUSLY SMALL]'
            if size_kb > 1024 * 10:  # > 10MB
                checks['file_sizes'][f] += ' [LARGE]'

    return checks

result = verify_brand_assets('output/')
import json
print(json.dumps(result, indent=2))
```

## Render Check (LibreOffice)

When LibreOffice is available, render to PDF/PNG for visual inspection:

```python
import subprocess
from pathlib import Path

def render_to_pdf(input_path: str, output_dir: str) -> Path:
    """Render DOCX/XLSX/PPTX to PDF for visual QA."""
    out = Path(output_dir)
    out.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        ['libreoffice', '--headless', '--convert-to', 'pdf',
         '--outdir', str(out), input_path],
        check=True, timeout=120,
    )
    return out / (Path(input_path).stem + '.pdf')

def render_to_png(pdf_path: str, output_dir: str, dpi: int = 100) -> list[Path]:
    """Render PDF pages to PNG for visual inspection."""
    out = Path(output_dir)
    out.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        ['pdftoppm', '-png', '-r', str(dpi), pdf_path, str(out / 'page')],
        check=True,
    )
    return sorted(out.glob('page-*.png'))

# Usage
pdf = render_to_pdf('output/report.docx', 'output/preview/')
pngs = render_to_png(str(pdf), 'output/preview/')
print(f'Generated {len(pngs)} preview images')
# Inspect manually: open the PNGs in an image viewer
```

If no renderer is available, mark visual QA as ASSUMED:

```python
import shutil

if not shutil.which('libreoffice'):
    print('WARNING: libreoffice not available. Visual QA is ASSUMED, not verified.')
    print('Reopen with python-docx to confirm structure; visual appearance unverified.')
```

## Diff Summary (for edits)

```python
from docx import Document

def diff_docx(source_path: str, edited_path: str) -> dict:
    """Diff paragraph counts and text between source and edited DOCX."""
    src = Document(source_path)
    edt = Document(edited_path)

    src_paras = {p.text for p in src.paragraphs if p.text.strip()}
    edt_paras = {p.text for p in edt.paragraphs if p.text.strip()}

    return {
        'source_paragraphs': len(src.paragraphs),
        'edited_paragraphs': len(edt.paragraphs),
        'added_paragraphs': list(edt_paras - src_paras)[:5],
        'removed_paragraphs': list(src_paras - edt_paras)[:5],
        'source_tables': len(src.tables),
        'edited_tables': len(edt.tables),
    }

print(diff_docx('input/report.docx', 'output/report_edited.docx'))
```

## Honest Exit

Report VERIFIED vs ASSUMED:

- **VERIFIED** — gates you ran with real output: parser reopened, counts match, text extracted, integrity check passed, render exported and inspected.
- **ASSUMED** — what you could NOT verify: visual appearance on other systems, font rendering fidelity, cross-version compatibility, real print rendering.

Never claim "looks good" without a render. If a check was skipped, say so.
