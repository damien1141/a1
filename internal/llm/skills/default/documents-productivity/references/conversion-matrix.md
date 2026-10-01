# Conversion Matrix

Cross-format conversion: DOCX↔PDF↔MD↔HTML, XLSX↔CSV↔JSON, PPTX→PDF. Primary tools: `pandoc` (documents), `libreoffice --headless` (renderer-dependent), `python-docx`/`openpyxl`/`python-pptx`/`pypdf` (programmatic).

## Matrix

| From | To | Tool | Fidelity | Notes |
|---|---|---|---|---|
| MD | DOCX | `pandoc` | High | `--reference-doc=template.docx` for styles |
| MD | PDF | `pandoc` + LaTeX | Medium | Requires `xelatex`/`tectonic`/`lualatex` |
| MD | HTML | `pandoc` | High | `--standalone --css=styles.css` |
| MD | ODT | `pandoc` | High | |
| MD | EPUB | `pandoc` | High | `--toc --toc-depth=2` |
| HTML | DOCX | `pandoc` | Medium | Styles may not map cleanly |
| HTML | PDF | `pandoc` + `weasyprint` | Medium | `--pdf-engine=weasyprint` |
| DOCX | MD | `pandoc` | Medium | Lossy: revision history, comments drop |
| DOCX | PDF | `libreoffice --headless` | High | Renderer-dependent; preserves layout |
| DOCX | HTML | `pandoc` | Medium | |
| DOCX | ODT | `libreoffice --headless` | High | |
| XLSX | CSV | `pandas` / `csv` | High | One sheet at a time; formulas become values |
| XLSX | JSON | `pandas` | High | `df.to_json(orient='records')` |
| XLSX | PDF | `libreoffice --headless` | Medium | Layout may shift; one sheet per PDF |
| CSV | XLSX | `pandas` / `openpyxl` | High | |
| CSV | JSON | `pandas` / `json` | High | |
| TSV | CSV | `csv` module | High | Change delimiter |
| PPTX | PDF | `libreoffice --headless` | High | One slide per page |
| PPTX | PNG (per slide) | `libreoffice` + `pdftoppm` | High | For thumbnails |
| PDF | DOCX | `pandoc` (extract first) | Low | PDF→MD via `pdftotext`, then MD→DOCX |
| PDF | TXT | `pdftotext` / `pdfplumber` | Medium | Layout may be lost |
| PDF | PNG (per page) | `pdftoppm` / `pymupdf` | High | |
| PDF | PDF (split/merge) | `pypdf` | High | See `pdf.md` |

## Pandoc — Document Conversion

```bash
# MD → DOCX with template (carries styles: Heading 1, Body Text, etc.)
pandoc input/report.md -o output/report.docx --reference-doc=templates/corporate.docx

# MD → PDF via LaTeX (requires xelatex / tectonic)
pandoc input/report.md -o output/report.pdf \
  --pdf-engine=xelatex \
  -V geometry:margin=1in \
  -V mainfont="Inter" \
  -V monofont="JetBrains Mono" \
  -V colorlinks=true \
  --toc --toc-depth=2

# MD → PDF without LaTeX (weasyprint — renders via HTML/CSS)
pandoc input/report.md -o output/report.pdf --pdf-engine=weasyprint --css=styles.css

# MD → HTML standalone
pandoc input/report.md -o output/report.html --standalone --css=styles.css --toc

# DOCX → MD (lossy)
pandoc input/report.docx -o output/report.md --wrap=none

# HTML → DOCX
pandoc input/report.html -o output/report.docx

# Combine multiple MD files into one DOCX with auto TOC
pandoc chapter1.md chapter2.md chapter3.md -o output/book.docx --toc --toc-depth=2

# MD → EPUB
pandoc input/book.md -o output/book.epub --toc --toc-depth=2 --metadata title="My Book"
```

Pandoc round-trips are lossy. DOCX→MD→DOCX loses revision history, comments, and some formatting. Always keep the original.

## LibreOffice — Renderer-Dependent Conversion

```bash
# DOCX → PDF
libreoffice --headless --convert-to pdf --outdir output/ input/report.docx

# XLSX → PDF (one sheet per page; layout may shift)
libreoffice --headless --convert-to pdf --outdir output/ input/sales.xlsx

# PPTX → PDF (one slide per page)
libreoffice --headless --convert-to pdf --outdir output/ input/deck.pptx

# DOCX → ODT (OpenDocument Text)
libreoffice --headless --convert-to odt --outdir output/ input/report.docx

# DOCX → HTML
libreoffice --headless --convert-to html --outdir output/ input/report.docx

# PPTX → PNG (via PDF intermediate)
libreoffice --headless --convert-to pdf --outdir output/ input/deck.pptx
pdftoppm -png -r 100 output/deck.pdf output/slide
# Produces output/slide-1.png, output/slide-2.png, ...
```

LibreOffice must be installed (`apt install libreoffice` / `brew install libreoffice`). Headless mode runs without a display. Use `--outdir` to control output location (default is the source dir).

Common flags:
- `--convert-to pdf:writer_pdf_Export` — explicit filter name (rarely needed)
- `--convert-to pdf:writer_pdf_Export:'{"Quality":{"type":"long","value":"90"}}'` — JSON filter options
- `--norestore --nofirststartwizard --nologo --nolockcheck` — quiet mode for scripting

### Programmatic LibreOffice

```python
import subprocess
import shutil
from pathlib import Path

def convert(input_path: str, output_dir: str, target_format: str = 'pdf') -> Path:
    """Convert any LibreOffice-supported file to target format."""
    if not shutil.which('libreoffice'):
        raise RuntimeError('libreoffice not installed; offer pandoc as fallback')
    out = Path(output_dir)
    out.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        ['libreoffice', '--headless', '--convert-to', target_format,
         '--outdir', str(out), input_path],
        check=True,
        timeout=120,
    )
    output_path = out / (Path(input_path).stem + f'.{target_format}')
    if not output_path.exists():
        raise RuntimeError(f'Expected output at {output_path}, not created')
    return output_path

# Usage
pdf = convert('input/report.docx', 'output/', 'pdf')
pdf = convert('input/deck.pptx', 'output/', 'pdf')
pdf = convert('input/sales.xlsx', 'output/', 'pdf')
```

## XLSX ↔ CSV / JSON

```python
import pandas as pd

# XLSX → CSV (one sheet at a time)
xls = pd.ExcelFile('input/sales.xlsx')
for sheet in xls.sheet_names:
    df = pd.read_excel(xls, sheet_name=sheet, dtype={'SKU': str, 'ZipCode': str})
    df.to_csv(f'output/{sheet}.csv', index=False)

# CSV → XLSX (multiple CSVs into one workbook, one sheet each)
csv_files = ['input/jan.csv', 'input/feb.csv', 'input/mar.csv']
with pd.ExcelWriter('output/q1.xlsx', engine='openpyxl') as writer:
    for csv in csv_files:
        df = pd.read_csv(csv, dtype={'SKU': str})
        sheet_name = Path(csv).stem
        df.to_excel(writer, sheet_name=sheet_name, index=False)

# XLSX → JSON
df = pd.read_excel('input/sales.xlsx', dtype={'SKU': str})
df.to_json('output/sales.json', orient='records', indent=2, date_format='iso')

# JSON → XLSX
df = pd.read_json('input/sales.json')
df.to_excel('output/sales.xlsx', index=False, engine='openpyxl')

# CSV → JSON
df = pd.read_csv('input/data.csv', dtype={'SKU': str})
df.to_json('output/data.json', orient='records', indent=2)
```

## PDF → Text (extraction only, not true "conversion")

```bash
# pdftotext (poppler-utils) — CLI, fast
pdftotext input/report.pdf output/report.txt
pdftotext -layout input/report.pdf output/report-layout.txt   # preserve columns
pdftotext -f 5 -l 10 input/report.pdf -   # pages 5-10 to stdout

# pdfplumber — Python library with table extraction
python -c "import pdfplumber; print(pdfplumber.open('input/report.pdf').pages[0].extract_text())"
```

PDF → DOCX is not a clean conversion. PDFs don't have semantic structure (paragraphs, headings, tables) — they have positioned text. Options:
1. `pdftotext -layout` → text file → `pandoc` text→DOCX (loses all formatting, preserves text)
2. `pdfplumber.extract_tables()` → manually rebuild tables in `python-docx`
3. Adobe Acrobat Pro (manual, not scriptable)

## Batch Conversion

```python
import subprocess
from pathlib import Path

def batch_convert(input_dir: str, output_dir: str, target_format: str = 'pdf'):
    """Batch convert all .docx/.xlsx/.pptx files in input_dir to target_format."""
    input_path = Path(input_dir)
    output_path = Path(output_dir)
    output_path.mkdir(parents=True, exist_ok=True)

    results = []
    for ext in ('*.docx', '*.xlsx', '*.pptx', '*.odt', '*.ods', '*.odp'):
        for f in input_path.glob(ext):
            try:
                out = convert(str(f), str(output_path), target_format)
                results.append((f.name, str(out), 'OK'))
            except Exception as e:
                results.append((f.name, None, str(e)))
    return results

results = batch_convert('input/', 'output/', 'pdf')
for name, out, status in results:
    print(f'{name}: {status}')
```

## Verification (every conversion)

```python
import subprocess
from pathlib import Path

def verify_conversion(input_path: str, output_path: str, expected_format: str):
    """Verify a converted file: exists, valid header, non-zero size."""
    out = Path(output_path)
    if not out.exists():
        raise AssertionError(f'Output not created: {output_path}')
    if out.stat().st_size < 100:
        raise AssertionError(f'Output suspiciously small: {out.stat().st_size} bytes')

    # Format-specific checks
    if expected_format == 'pdf':
        result = subprocess.run(['pdfinfo', str(out)], capture_output=True, text=True)
        if result.returncode != 0:
            raise AssertionError(f'pdfinfo failed: {result.stderr}')
        print(f'PDF OK: {result.stdout.splitlines()[0]}')
    elif expected_format in ('docx', 'xlsx', 'pptx'):
        result = subprocess.run(['unzip', '-t', str(out)], capture_output=True, text=True)
        if result.returncode != 0:
            raise AssertionError(f'OOXML corrupt: {result.stderr}')
        print(f'{expected_format.upper()} OK (valid ZIP)')
    elif expected_format == 'md':
        text = out.read_text(encoding='utf-8')
        if len(text.strip()) < 50:
            raise AssertionError('Markdown output suspiciously short')
        print(f'MD OK ({len(text)} chars)')

verify_conversion('input/report.docx', 'output/report.pdf', 'pdf')
```

## Common Gotchas

- **LibreOffice is slow to start** (~3s) — for batch jobs, all conversions in one session are faster than spawning per-file.
- **LibreOffice locks files** — if a file is open in LibreOffice GUI, headless conversion fails with "locked for editing." Use `--nolockcheck` to bypass (risky).
- **Pandoc + LaTeX requires LaTeX** — install `texlive-xetex` (Linux), `mactex-no-gui` (macOS), or `tectonic` (cross-platform single-binary).
- **PDF → DOCX is not clean** — PDFs have positioned text, not semantic structure. Layout-based conversion is lossy.
- **XLSX → CSV loses formulas** — `pandas` writes computed values (if cached), not formulas. Use `openpyxl` to read formulas and re-evaluate (Python doesn't evaluate Excel formulas natively).
- **Date/time semantics shift** — Excel stores dates as days since 1900-01-01 (with the 1900 leap year bug). `pandas` handles this, but be explicit: `df['date'] = pd.to_datetime(df['date'])`.
- **Leading zeros drop in CSV** — `pd.read_csv(dtype={'SKU': str})` or `openpyxl` with `cell.number_format = '@'`.
- **Encoding** — always specify `encoding='utf-8'` on file open. Excel on Windows defaults to cp1252.
- **LibreOffice rendering ≠ Excel/Word rendering** — fonts, complex charts, SmartArt may render differently. Visual QA in the target application.
- **`--convert-to` overwrites without warning** — if `output/report.pdf` exists, it's replaced. Use `--outdir` to a fresh directory.
