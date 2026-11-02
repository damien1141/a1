# PPTX (PowerPoint) — python-pptx

Create, edit, inspect, and convert PowerPoint decks. `python-pptx` 0.6+ for slides/layouts/placeholders/shapes/text frames/tables/charts/images/notes. `.potx` templates for consistent master slides. One clear claim per slide — slides are for claims, not paragraphs.

## Library Selection

| Task | Library | Notes |
|---|---|---|
| Create PPTX from scratch | `python-pptx` | `Presentation()`, blank layout, add slides |
| Edit existing PPTX | `python-pptx` | Load, modify, save |
| Read structure / extract text | `python-pptx` | Iterate slides + shapes |
| Template-based generation | `python-pptx` + `.potx` | Use master layouts for design consistency |
| PPTX → PDF | `libreoffice --headless` | Renderer-dependent; preserves layout |
| PPTX → images | `libreoffice --headless` + `pdftoppm` or `pymupdf` | For preview thumbnails |

## Create a Deck

```python
from pptx import Presentation
from pptx.util import Inches, Pt, Emu
from pptx.dml.color import RGBColor
from pptx.enum.text import PP_ALIGN, MSO_ANCHOR
from pptx.enum.shapes import MSO_SHAPE

# 16:9 widescreen (default is 10" × 7.5" 4:3)
prs = Presentation()
prs.slide_width = Inches(13.333)
prs.slide_height = Inches(7.5)

# Blank layout (no placeholders) — index 6 in default template
blank = prs.slide_layouts[6]

slides_data = [
    ('Q3 Performance Review', 'Operations Team — October 2024'),
    ('Revenue grew 14%', 'Driven by enterprise renewals and two new EMEA logos.'),
    ('NA: $2.1M ARR (+8%)', 'Stable enterprise base; one churn offset by expansion.'),
    ('EMEA: $1.4M ARR (+22%)', 'New logos in Germany and France; pipeline healthy.'),
    ('APAC: $0.7M ARR (+12%)', 'Japan renewal closed; Australia pipeline building.'),
    ('Next quarter', 'Focus on APAC pipeline conversion and EMEA expansion.'),
]

for title_text, body_text in slides_data:
    slide = prs.slides.add_slide(blank)

    # Title (top of slide)
    title_box = slide.shapes.add_textbox(Inches(1), Inches(0.5), Inches(11.3), Inches(1.5))
    tf = title_box.text_frame
    tf.word_wrap = True
    p = tf.paragraphs[0]
    p.text = title_text
    p.font.size = Pt(40)
    p.font.bold = True
    p.font.color.rgb = RGBColor(0x0F, 0x17, 0x2A)
    p.font.name = 'Inter'

    # Body (below title)
    body_box = slide.shapes.add_textbox(Inches(1), Inches(2.5), Inches(11.3), Inches(4))
    tf = body_box.text_frame
    tf.word_wrap = True
    p = tf.paragraphs[0]
    p.text = body_text
    p.font.size = Pt(24)
    p.font.color.rgb = RGBColor(0x47, 0x55, 0x69)
    p.font.name = 'Inter'

    # Speaker notes
    slide.notes_slide.notes_text_frame.text = f'Talking points for: {title_text}'

prs.save('output/q3_review.pptx')
```

## Using Layout Placeholders

Instead of `add_textbox`, prefer built-in layouts with placeholders for design consistency:

```python
prs = Presentation('templates/corporate.potx')   # load template
# Layouts come from the template — inspect with prs.slide_layouts
for i, layout in enumerate(prs.slide_layouts):
    print(f'Layout {i}: {layout.name}')
    for ph in layout.placeholders:
        print(f'  placeholder idx={ph.placeholder_format.idx}, type={ph.placeholder_format.type}, name="{ph.name}"')

# Typical default layouts:
# 0: Title Slide (title + subtitle)
# 1: Title and Content (title + body)
# 5: Title Only
# 6: Blank

title_layout = prs.slide_layouts[0]
content_layout = prs.slide_layouts[1]

# Title slide
slide = prs.slides.add_slide(title_layout)
slide.shapes.title.text = 'Q3 Performance Review'
slide.placeholders[1].text = 'Operations Team — October 2024'

# Content slide with bulleted body
slide = prs.slides.add_slide(content_layout)
slide.shapes.title.text = 'Highlights'
body = slide.placeholders[1].text_frame
body.text = 'Revenue grew 14%'  # first bullet
p = body.add_paragraph()
p.text = 'EMEA added 2 new logos'
p.level = 0
p = body.add_paragraph()
p.text = 'APAC pipeline 3x'
p.level = 1  # indented
```

## Tables

```python
from pptx.util import Inches, Pt
from pptx.dml.color import RGBColor

slide = prs.slides.add_slide(blank)
slide.shapes.add_textbox(Inches(1), Inches(0.5), Inches(11.3), Inches(1)).text_frame.text = 'Regional Performance'

rows, cols = 4, 4
left, top, width, height = Inches(1), Inches(2), Inches(11), Inches(4)
table_shape = slide.shapes.add_table(rows, cols, left, top, width, height)
table = table_shape.table

# Headers
headers = ['Region', 'Q2 ARR', 'Q3 ARR', 'Growth %']
for col, h in enumerate(headers):
    cell = table.cell(0, col)
    cell.text = h
    p = cell.text_frame.paragraphs[0]
    p.font.bold = True
    p.font.size = Pt(14)
    p.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)
    cell.fill.solid()
    cell.fill.fore_color.rgb = RGBColor(0x0D, 0x94, 0x88)

# Data
data = [
    ('NA',   '$1.94M', '$2.10M', '+8%'),
    ('EMEA', '$1.15M', '$1.40M', '+22%'),
    ('APAC', '$0.62M', '$0.70M', '+12%'),
]
for row_idx, row_data in enumerate(data, start=1):
    for col_idx, value in enumerate(row_data):
        cell = table.cell(row_idx, col_idx)
        cell.text = value
        cell.text_frame.paragraphs[0].font.size = Pt(12)
```

## Charts

```python
from pptx.chart.data import CategoryChartData
from pptx.enum.chart import XL_CHART_TYPE

slide = prs.slides.add_slide(blank)
slide.shapes.add_textbox(Inches(1), Inches(0.5), Inches(11.3), Inches(1)).text_frame.text = 'ARR by Quarter'

chart_data = CategoryChartData()
chart_data.categories = ['Q1', 'Q2', 'Q3']
chart_data.add_series('NA',   (1.7, 1.94, 2.10))
chart_data.add_series('EMEA', (0.95, 1.15, 1.40))
chart_data.add_series('APAC', (0.55, 0.62, 0.70))

chart_frame = slide.shapes.add_chart(
    XL_CHART_TYPE.COLUMN_CLUSTERED,
    Inches(1), Inches(2), Inches(11), Inches(5),
    chart_data,
)
chart = chart_frame.chart
chart.has_legend = True
chart.legend.include_in_layout = False
chart.has_title = False  # title is in the slide, not the chart

# Style series colors
plot = chart.plots[0]
plot.has_data_labels = True
plot.data_labels.font.size = Pt(10)
plot.data_labels.number_format = '"$"0.0"M"'

series = plot.series[0]
series.format.fill.solid()
series.format.fill.fore_color.rgb = RGBColor(0x0D, 0x94, 0x88)
```

## Images

```python
from pptx.util import Inches

slide = prs.slides.add_slide(blank)
# Add picture — width OR height (preserve aspect ratio)
slide.shapes.add_picture('chart.png', Inches(1), Inches(1.5), width=Inches(11))

# Add picture with cropping
from pptx.enum.shapes import MSO_SHAPE
pic = slide.shapes.add_picture('photo.jpg', Inches(1), Inches(1.5), width=Inches(6))
pic.crop_left = 0.1   # 10% off left
pic.crop_right = 0.1
```

## Master Slides & Templates (.potx)

`.potx` is a PowerPoint template — same OOXML structure as `.pptx`. Load it as the base for new decks to inherit master design:

```python
prs = Presentation('templates/corporate.potx')
# All slide_layouts come from the template's slide_master
# New slides use those layouts → design consistency

# Edit the slide master directly (changes apply to ALL slides using it)
master = prs.slide_master
for shape in master.shapes:
    if shape.has_text_frame:
        print(f'Master shape: {shape.text_frame.text}')

# Edit a layout (changes apply to all slides using that layout)
layout = prs.slide_layouts[1]
for placeholder in layout.placeholders:
    print(f'Layout placeholder: {placeholder.name}')
```

To create a `.potx` from scratch: build a `.pptx`, then save with `.potx` extension (same format). To edit a `.potx`: load with `python-pptx`, modify, save with `.potx` extension.

## Read Existing PPTX

```python
from pptx import Presentation

prs = Presentation('input/existing.pptx')
print(f'Slides: {len(prs.slides)}')
print(f'Slide size: {prs.slide_width} × {prs.slide_height}')

for i, slide in enumerate(prs.slides):
    print(f'\n=== Slide {i+1} ===')
    print(f'Layout: {slide.slide_layout.name}')
    for shape in slide.shapes:
        if shape.has_text_frame:
            text = shape.text_frame.text[:80]
            print(f'  [text] {text}')
        if shape.has_table:
            print(f'  [table] {len(shape.table.rows)} rows × {len(shape.table.columns)} cols')
        if shape.shape_type == 13:  # PICTURE
            print(f'  [image] {shape.image.filename if hasattr(shape, "image") else "embedded"}')
    if slide.has_notes_slide:
        notes = slide.notes_slide.notes_text_frame.text
        print(f'  [notes] {notes[:80]}')
```

## Speaker Notes

```python
slide = prs.slides.add_slide(blank)
# ... add title etc ...

# Notes — creates notes_slide if it doesn't exist
notes_tf = slide.notes_slide.notes_text_frame
notes_tf.text = 'Opening hook: 14% growth is the headline.'
p = notes_tf.add_paragraph()
p.text = 'Then walk through regional breakdown.'
p.level = 1
```

## Verification

```python
from pptx import Presentation
import subprocess

# Reopen with parser
prs = Presentation('output/q3_review.pptx')
print(f'Slides: {len(prs.slides)}')
print(f'Size: {prs.slide_width} × {prs.slide_height} EMU')
for i, slide in enumerate(prs.slides):
    text_chunks = []
    for shape in slide.shapes:
        if shape.has_text_frame:
            text_chunks.append(shape.text_frame.text[:60])
    notes = slide.notes_slide.notes_text_frame.text[:60] if slide.has_notes_slide else ''
    print(f'Slide {i+1}: layout="{slide.slide_layout.name}", text={text_chunks}, notes="{notes}"')

# Integrity check
result = subprocess.run(['unzip', '-t', 'output/q3_review.pptx'], capture_output=True, text=True)
print('ZIP OK' if result.returncode == 0 else f'CORRUPT: {result.stderr}')
```

```bash
# Render to PDF for visual check (LibreOffice)
libreoffice --headless --convert-to pdf --outdir output/ output/q3_review.pptx
pdfinfo output/q3_review.pdf   # confirm page count matches slide count

# Render slides to PNG (for quick visual inspection)
libreoffice --headless --convert-to pdf --outdir output/ output/q3_review.pptx
pdftoppm -png -r 100 output/q3_review.pdf output/slide
ls output/slide-*.png
```

## Common Gotchas

- **Default template is 4:3 (10" × 7.5")** — set `prs.slide_width = Inches(13.333); prs.slide_height = Inches(7.5)` for 16:9.
- **`placeholder_format.idx` ≠ array index** — placeholder indices are template-defined. Iterate `layout.placeholders` and read `ph.placeholder_format.idx` to find the right one.
- **`text_frame.text = '...'` overwrites everything** — to add paragraphs, use `tf.add_paragraph()` and set `.text` on each.
- **Font fallback** — if a font isn't on the viewer's system, PowerPoint substitutes. Embed fonts (File → Options → Save → Embed fonts) for cross-system consistency.
- **Charts don't render the same in LibreOffice** — basic column/line/pie are safe. Complex types (sunburst, treemap, waterfall) may not display. Test with the target viewer.
- **Images are embedded** — adding a 5MB image makes the PPTX 5MB larger. Resize/compress images before adding.
- **`python-pptx` cannot run animations or transitions** — those are PPTX features but the library doesn't expose them. Edit the XML directly if needed.
- **`.potx` vs `.pptx`** — same format, different extension. `Presentation('templates/x.potx')` loads it; `prs.save('output/y.potx')` saves as a template.
- **Master slide edits propagate** — changing a layout's placeholder formatting affects every slide using that layout. Make a copy of the template first if experimenting.
- **One job per slide** — slides are for claims, not paragraphs. If the body text exceeds 30 words, split into two slides.
