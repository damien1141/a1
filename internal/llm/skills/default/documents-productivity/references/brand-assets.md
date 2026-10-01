# Brand Assets — Logos, Palettes, Identity Decks

Generate brand-kit imagery programmatically: logo systems (SVG generation, lockup variants, clear space, min size), color palettes (extract from image, generate harmonies), identity decks, social media kits. Uses `svgwrite` for SVG, `cairosvg`/`Pillow` for raster, `python-pptx`/`reportlab` for identity decks. Adapted from the `brandkit` source skill.

## Library Selection

| Task | Library | Notes |
|---|---|---|
| SVG logo generation | `svgwrite` | Pure Python; outputs clean SVG XML |
| SVG → PNG raster | `cairosvg` | Best quality; requires cairo system libs |
| SVG → PNG (alternative) | `Pillow` + `svglib` | Pure Python fallback |
| Image manipulation | `Pillow` (PIL) | Resize, composite, format conversion |
| Palette extraction | `Pillow` + `collections.Counter` | Quantize + count |
| Identity deck (PPTX) | `python-pptx` | See `pptx.md` |
| Identity deck (PDF) | `reportlab` | See `pdf.md` |
| PNG → SVG trace | `potrace` (CLI) | Raster-to-vector; not always clean |

## Logo Generation — SVG with `svgwrite`

```python
import svgwrite

# Monogram + meaning: 'A' (ascent) inside a circle (system)
dwg = svgwrite.Drawing('output/logo.svg', profile='full', size=('240', '280'))

# Outer circle (system boundary)
dwg.add(dwg.circle(center=(120, 120), r=110, fill='none', stroke='#0F172A', stroke_width=8))

# Triangle (ascent) — the 'A' shape
dwg.add(dwg.polygon(
    points=[(120, 40), (200, 200), (40, 200)],
    fill='#0F172A',
))

# Crossbar — completes the 'A' via negative space
dwg.add(dwg.line(start=(80, 150), end=(160, 150), stroke='#FFFFFF', stroke_width=12))

# Wordmark below
dwg.add(dwg.text(
    'ASCENT',
    insert=(120, 250),
    font_family='Inter, system-ui, sans-serif',
    font_size=28,
    font_weight=700,
    text_anchor='middle',
    fill='#0F172A',
))

dwg.save()
```

### Lockup Variants

A logo system needs multiple lockups for different contexts:

```python
import svgwrite

# 1. Stacked (logo above wordmark)
stacked = svgwrite.Drawing('output/lockup-stacked.svg', profile='full', size=('240', '320'))
stacked.add(stacked.circle(center=(120, 100), r=80, fill='none', stroke='#0F172A', stroke_width=6))
stacked.add(stacked.polygon(points=[(120, 50), (180, 150), (60, 150)], fill='#0F172A'))
stacked.add(stacked.line(start=(90, 125), end=(150, 125), stroke='#FFFFFF', stroke_width=8))
stacked.add(stacked.text('ASCENT', insert=(120, 230),
    font_family='Inter, system-ui, sans-serif', font_size=32, font_weight=700,
    text_anchor='middle', fill='#0F172A'))
stacked.add(stacked.text('Build with momentum.', insert=(120, 270),
    font_family='Inter, system-ui, sans-serif', font_size=16,
    text_anchor='middle', fill='#475569'))
stacked.save()

# 2. Horizontal (logo + wordmark side-by-side)
horiz = svgwrite.Drawing('output/lockup-horizontal.svg', profile='full', size=('600', '240'))
horiz.add(horiz.circle(center=(120, 120), r=80, fill='none', stroke='#0F172A', stroke_width=6))
horiz.add(horiz.polygon(points=[(120, 60), (180, 180), (60, 180)], fill='#0F172A'))
horiz.add(horiz.line(start=(90, 150), end=(150, 150), stroke='#FFFFFF', stroke_width=8))
horiz.add(horiz.text('ASCENT', insert=(240, 130),
    font_family='Inter, system-ui, sans-serif', font_size=56, font_weight=700,
    fill='#0F172A'))
horiz.add(horiz.text('Build with momentum.', insert=(240, 175),
    font_family='Inter, system-ui, sans-serif', font_size=20, fill='#475569'))
horiz.save()

# 3. Icon only (favicon, app icon)
icon = svgwrite.Drawing('output/icon.svg', profile='full', size=('240', '240'))
icon.add(icon.circle(center=(120, 120), r=110, fill='#0F172A'))
icon.add(icon.polygon(points=[(120, 40), (200, 200), (40, 200)], fill='#FFFFFF'))
icon.add(icon.line(start=(80, 150), end=(160, 150), stroke='#0F172A', stroke_width=12))
icon.save()

# 4. Reversed (for dark backgrounds)
rev = svgwrite.Drawing('output/logo-reversed.svg', profile='full', size=('240', '280'))
rev.add(rev.circle(center=(120, 120), r=110, fill='none', stroke='#FFFFFF', stroke_width=8))
rev.add(rev.polygon(points=[(120, 40), (200, 200), (40, 200)], fill='#FFFFFF'))
rev.add(rev.line(start=(80, 150), end=(160, 150), stroke='#0F172A', stroke_width=12))
rev.add(rev.text('ASCENT', insert=(120, 250),
    font_family='Inter, system-ui, sans-serif', font_size=28, font_weight=700,
    text_anchor='middle', fill='#FFFFFF'))
rev.save()
```

### Clear Space & Minimum Size

```python
import svgwrite

# Clear space diagram — dashed boundary showing the minimum padding
# Clear space = height of the logo's capital letter (or 1/4 of logo height)
clear = svgwrite.Drawing('output/clear-space.svg', profile='full', size=('400', '300'))

# The logo
clear.add(clear.circle(center=(140, 130), r=70, fill='none', stroke='#0F172A', stroke_width=4))
clear.add(clear.polygon(points=[(140, 70), (200, 190), (80, 190)], fill='#0F172A'))

# Dashed clear-space boundary (offset by 1 cap height = ~24px)
clear.add(clear.rect(insert=(40, 30), size=(200, 220),
    fill='none', stroke='#0D9488', stroke_width=1, stroke_dasharray='4 4'))

# Dimension labels
clear.add(clear.text('X', insert=(30, 130),
    font_family='Inter, system-ui, sans-serif', font_size=18, font_weight=700,
    fill='#0D9488', text_anchor='middle'))
clear.add(clear.text('minimum clear space = cap height', insert=(200, 280),
    font_family='Inter, system-ui, sans-serif', font_size=12, fill='#475569',
    text_anchor='middle'))
clear.save()
```

Document the minimum size in a brand guidelines doc: e.g., "Minimum logo width: 120px digital / 30mm print. Below this, use the icon-only variant."

## SVG → PNG Raster

```python
import cairosvg

# High-resolution PNG for social, favicon, app icon
cairosvg.svg2png(
    url='output/logo.svg',
    write_to='output/logo.png',
    output_width=480,
    output_height=560,
)

# Favicon set
for size in [16, 32, 48, 64, 180]:  # 180 = apple-touch-icon
    cairosvg.svg2png(
        url='output/icon.svg',
        write_to=f'output/favicon-{size}.png',
        output_width=size,
        output_height=size,
    )

# SVG → PDF (for print)
cairosvg.svg2pdf(url='output/logo.svg', write_to='output/logo.pdf')
```

If `cairosvg` is not available (cairo system libs missing), fall back to `Pillow` + `svglib`:

```python
from svglib.svglib import svg2rlg
from reportlab.graphics import renderPM

drawing = svg2rlg('output/logo.svg')
renderPM.drawToFile(drawing, 'output/logo.png', fmt='PNG', dpi=300)
```

## Color Palette Extraction

```python
from PIL import Image
from collections import Counter

def extract_palette(image_path: str, n: int = 6) -> list[tuple[str, int]]:
    """Extract the top N colors from an image. Returns [(hex, count), ...]."""
    img = Image.open(image_path).convert('RGB')
    # Quantize to reduce colors
    quantized = img.quantize(colors=n, method=Image.Quantize.MEDIANCUT)
    palette = quantized.getpalette()  # flat list [r,g,b,r,g,b,...]
    color_counts = Counter(quantized.getdata())

    result = []
    for color_idx, count in color_counts.most_common(n):
        r, g, b = palette[color_idx * 3:(color_idx + 1) * 3]
        hex_code = f'#{r:02X}{g:02X}{b:02X}'
        result.append((hex_code, count))
    return result

palette = extract_palette('input/brand-photo.jpg', n=6)
for hex_code, count in palette:
    print(f'{hex_code}  ({count} pixels)')
```

## Palette Harmonies

```python
import colorsys

def hex_to_hsl(h: str) -> tuple[float, float, float]:
    r, g, b = int(h[1:3], 16) / 255, int(h[3:5], 16) / 255, int(h[5:7], 16) / 255
    return colorsys.rgb_to_hls(r, g, b)  # returns (h, l, s)

def hsl_to_hex(h: float, l: float, s: float) -> str:
    r, g, b = colorsys.hls_to_rgb(h, l, s)
    return f'#{int(r*255):02X}{int(g*255):02X}{int(b*255):02X}'

def harmonies(base_hex: str) -> dict:
    h, l, s = hex_to_hsl(base_hex)
    return {
        'base':       base_hex,
        'complement': hsl_to_hex((h + 0.5) % 1.0, l, s),
        'analogous_1': hsl_to_hex((h + 1/12) % 1.0, l, s),
        'analogous_2': hsl_to_hex((h - 1/12) % 1.0, l, s),
        'triadic_1':   hsl_to_hex((h + 1/3) % 1.0, l, s),
        'triadic_2':   hsl_to_hex((h + 2/3) % 1.0, l, s),
        'split_comp_1': hsl_to_hex((h + 0.4) % 1.0, l, s),
        'split_comp_2': hsl_to_hex((h + 0.6) % 1.0, l, s),
        'tints':       [hsl_to_hex(h, min(1.0, l + 0.15 * i), s) for i in range(1, 4)],
        'shades':      [hsl_to_hex(h, max(0.0, l - 0.15 * i), s) for i in range(1, 4)],
    }

for name, color in harmonies('#0D9488').items():
    if isinstance(color, list):
        print(f'{name}: {color}')
    else:
        print(f'{name}: {color}')
```

## Palette Swatch Sheet

```python
import svgwrite

palette = ['#0F172A', '#0D9488', '#FBBF24', '#475569', '#FAFAFA', '#F87171']
names    = ['Ink',     'Teal',    'Amber',   'Slate',    'Bone',    'Alert']

sw = svgwrite.Drawing('output/palette.svg', profile='full', size=('720', '160'))
sw.add(sw.rect(insert=(0, 0), size=('720', '160'), fill='#FAFAFA'))
for i, (color, name) in enumerate(zip(palette, names)):
    x = 20 + i * 115
    sw.add(sw.rect(insert=(x, 20), size=('100', '100'), fill=color, stroke='#E5E7EB', stroke_width=1))
    sw.add(sw.text(name, insert=(x + 50, 135),
        font_family='Inter, system-ui, sans-serif', font_size=12, font_weight=600,
        text_anchor='middle', fill='#0F172A'))
    sw.add(sw.text(color, insert=(x + 50, 152),
        font_family='JetBrains Mono, monospace', font_size=10,
        text_anchor='middle', fill='#475569'))
sw.save()
```

## Social Media Kit

```python
import cairosvg
from PIL import Image

# Generate social banner variants from the logo SVG
formats = {
    'twitter_card':  (1200, 600),   # 2:1
    'og_image':      (1200, 630),   # 1.91:1
    'linkedin':      (1200, 627),
    'instagram_sq':  (1080, 1080),
    'instagram_st':  (1080, 1920),  # 9:16 story
    'facebook':      (1200, 628),
}

for name, (w, h) in formats.items():
    # Compose a banner: solid bg + logo centered
    banner = svgwrite.Drawing(f'output/social-{name}.svg', profile='full', size=(w, h))
    banner.add(banner.rect(insert=(0, 0), size=(w, h), fill='#FAFAFA'))
    # Center the logo at 30% of banner width
    logo_w = int(w * 0.30)
    logo_h = int(logo_w * (280 / 240))   # logo aspect ratio
    logo_x = (w - logo_w) // 2
    logo_y = (h - logo_h) // 2
    # Inline the logo SVG content here (or use svgwrite's add with a nested svg)
    banner.add(banner.circle(center=(w//2, h//2 - 30), r=logo_w//2,
        fill='none', stroke='#0F172A', stroke_width=int(logo_w * 0.033)))
    banner.add(banner.polygon(points=[
        (w//2, h//2 - 30 - logo_w//3),
        (w//2 + logo_w//3, h//2 - 30 + logo_w//3),
        (w//2 - logo_w//3, h//2 - 30 + logo_w//3),
    ], fill='#0F172A'))
    banner.save()
    # Rasterize
    cairosvg.svg2png(url=f'output/social-{name}.svg', write_to=f'output/social-{name}.png',
                     output_width=w, output_height=h)
```

## Identity Deck (PPTX)

Use `python-pptx` to build a brand identity deck — logo cover, construction, digital application, color, typography, physical application, image direction, system detail. See `pptx.md` for the PPTX API. One brand idea per slide. Use the master layout for consistency.

```python
from pptx import Presentation
from pptx.util import Inches, Pt
from pptx.dml.color import RGBColor

prs = Presentation()
prs.slide_width = Inches(13.333)
prs.slide_height = Inches(7.5)
blank = prs.slide_layouts[6]

# Slide 1: Logo cover
slide = prs.slides.add_slide(blank)
slide.shapes.add_picture('output/logo.png', Inches(5.5), Inches(2.5), width=Inches(2.5))

# Slide 2: Logo construction
slide = prs.slides.add_slide(blank)
slide.shapes.add_textbox(Inches(1), Inches(0.5), Inches(11.3), Inches(1)).text_frame.text = 'Logo Construction'
slide.shapes.add_picture('output/clear-space.svg', Inches(1), Inches(2), width=Inches(6))

# Slide 3: Color system
slide = prs.slides.add_slide(blank)
slide.shapes.add_textbox(Inches(1), Inches(0.5), Inches(11.3), Inches(1)).text_frame.text = 'Color System'
slide.shapes.add_picture('output/palette.svg', Inches(1), Inches(2), width=Inches(11))

# Slide 4: Typography
slide = prs.slides.add_slide(blank)
slide.shapes.add_textbox(Inches(1), Inches(0.5), Inches(11.3), Inches(1)).text_frame.text = 'Typography'
tf = slide.shapes.add_textbox(Inches(1), Inches(2), Inches(11), Inches(5)).text_frame
p = tf.paragraphs[0]
p.text = 'Inter — Headings & Body'
p.font.size = Pt(48); p.font.name = 'Inter'; p.font.bold = True
p2 = tf.add_paragraph()
p2.text = 'JetBrains Mono — Data & Code'
p2.font.size = Pt(32); p2.font.name = 'JetBrains Mono'

prs.save('output/identity_deck.pptx')
```

## Verification

```python
from PIL import Image
import os

# 1. SVG files are well-formed XML
import xml.etree.ElementTree as ET
for svg in ['output/logo.svg', 'output/lockup-horizontal.svg', 'output/icon.svg']:
    ET.parse(svg)   # raises ParseError on malformed XML
    print(f'{svg}: valid XML')

# 2. PNG files open and have expected dimensions
for png in ['output/logo.png', 'output/favicon-180.png']:
    img = Image.open(png)
    print(f'{png}: {img.size}, mode={img.mode}')

# 3. File sizes are reasonable
for f in ['output/logo.svg', 'output/logo.png', 'output/identity_deck.pptx']:
    size_kb = os.path.getsize(f) / 1024
    print(f'{f}: {size_kb:.1f} KB')

# 4. Color palette has expected number of colors
palette = extract_palette('output/logo.png', n=6)
print(f'Logo palette: {[c[0] for c in palette]}')
```

## Discipline

- **Subtract, don't redecorate** — a logo should feel researched and reduced, not ornamental. Kill the third flourish, not add a fourth.
- **One brand idea per board** — every panel must answer: what does this brand represent? What is the core metaphor? How does the logo express that?
- **No fabricated quotes, stats, or testimonials** — brand copy must be sourced from the brief. Missing facts go to a gap list, not invented.
- **Logo variants must be consistent** — same mark, same proportions, same construction logic across all lockups. Don't design a different logo for each variant.
- **Test on real backgrounds** — a logo that only works on white fails. Always generate a reversed (dark-bg) variant and verify contrast.
- **Minimum size matters** — a logo at 16×16px favicon must still be recognizable. If it isn't, design a simplified icon variant.
- **Never use copyrighted marks** — don't reproduce real-world logos (Nike, Apple, etc.) as "references." Use the reference's quality/rhythm, not its content.
