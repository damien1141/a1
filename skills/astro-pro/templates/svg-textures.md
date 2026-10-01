### SVG Textures (Tailwind v3 JIT-Safe)

**Use when:** applying a global background texture via a `fixed inset-0 -z-10 opacity-[X]` wrapper in `Layout.astro` (§5). One texture per archetype per build. Light theme = DARK stroke/dot; Dark theme (Tech only) = LIGHT stroke/dot. To invert any texture to the opposite theme, swap the stroke/fill color token per the archetype header below. Recommended opacity ranges noted — denser patterns need lower opacity. NO BASE64 — base64 breaks Tailwind v3 JIT scanning. Use these exact URL-encoded strings.

**Selection rule:** pick ONE texture per archetype per build. Do not stack two archetype textures in the same viewport (§11 Performance: one continuous pattern per viewport). The global texture layer in `Layout.astro` carries the single chosen pattern; sections inherit it transparently.

#### Industrial (LIGHT · stroke `#000` → invert to `#fff` for dark)
Default opacity `0.08`. Heavy-gauge utility patterns.

**Use when:** Rugged Industrial archetype. Swap `stroke=%23000` → `%23fff` to invert for a dark Industrial build.

- **Blueprint Grid** (default):
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2240%22%20height=%2240%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M0%2039H39V0%22%20stroke=%22%23000%22%20stroke-width=%221%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.08]` — corner L-shapes, structural.

- **Dashed Blueprint**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2240%22%20height=%2240%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M0%2039H39V0%22%20stroke=%22%23000%22%20stroke-width=%221%22%20stroke-dasharray=%222%203%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.08]` — dashed variant, in-progress feel.

- **Diagonal Hatch**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2220%22%20height=%2220%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M0%200L20%2020M10%200L20%2010M0%2010L10%2020%22%20stroke=%22%23000%22%20stroke-width=%221%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.06]` — 45° parallel lines, hazard-adjacent.

- **Crosshatch**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2220%22%20height=%2220%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M0%200L20%2020M0%2020L20%200%22%20stroke=%22%23000%22%20stroke-width=%220.5%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.05]` — X-grid, engraved metal.

- **Tread Plate** (diamond):
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2224%22%20height=%2224%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M12%204L20%2012L12%2020L4%2012Z%22%20stroke=%22%23000%22%20stroke-width=%220.75%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.07]` — diamond grid, stamped floor plate.

- **Corner Brackets**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2240%22%20height=%2240%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M0%206V0H6M34%200H40V6M40%2034V40H34M6%2040H0V34%22%20stroke=%22%23000%22%20stroke-width=%221%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.10]` — technical drawing frame, sparse and architectural.

#### Luxury (LIGHT · stroke `#3D2C1E` → invert to `#FDFBF7` for dark)
Default opacity `0.04`. Organic, considered, low-contrast.

**Use when:** Organic Luxury archetype. Swap `stroke=%233D2C1E` → `%23FDFBF7` to invert for a dark Luxury build.

- **Topographic Contours** (default):
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2760%27%20height=%2760%27%20xmlns=%27http://www.w3.org/2000/svg%27%3E%3Cpath%20d=%27M30%200C13.4%200%200%2013.4%200%2030s13.4%2030%2030%2030%2030-13.4%2030-30S46.6%200%2030%200zm0%2055c-13.8%200-25-11.2-25-25S16.2%205%2030%205s25%2011.2%2025%2025-11.2%2025-25%2025z%27%20fill=%27none%27%20stroke=%27%233D2C1E%27%20stroke-width=%271%27/%3E%3C/svg%3E')]
```
`opacity-[0.04]` — concentric ovals, terrain map.

- **Concentric Diamonds**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2260%22%20height=%2260%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M30%205L55%2030L30%2055L5%2030ZM30%2015L45%2030L30%2045L15%2030ZM30%2024L36%2030L30%2036L24%2030Z%22%20stroke=%22%233D2C1E%22%20stroke-width=%220.5%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.04]` — rotated square nests, art deco.

- **Art Deco Sunburst**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2280%22%20height=%2280%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M40%2040L40%200M40%2040L68.3%2011.7M40%2040L80%2040M40%2040L68.3%2068.3M40%2040L40%2080M40%2040L11.7%2068.3M40%2040L0%2040M40%2040L11.7%2011.7%22%20stroke=%22%233D2C1E%22%20stroke-width=%220.5%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.05]` — radial rays, atelier premium.

- **Overlapping Circles**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2260%22%20height=%2260%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Ccircle%20cx=%220%22%20cy=%220%22%20r=%2218%22%20fill=%22none%22%20stroke=%22%233D2C1E%22%20stroke-width=%220.5%22/%3E%3Ccircle%20cx=%2260%22%20cy=%220%22%20r=%2218%22%20fill=%22none%22%20stroke=%22%233D2C1E%22%20stroke-width=%220.5%22/%3E%3Ccircle%20cx=%220%22%20cy=%2260%22%20r=%2218%22%20fill=%22none%22%20stroke=%22%233D2C1E%22%20stroke-width=%220.5%22/%3E%3Ccircle%20cx=%2260%22%20cy=%2260%22%20r=%2218%22%20fill=%22none%22%20stroke=%22%233D2C1E%22%20stroke-width=%220.5%22/%3E%3C/svg%3E')]
```
`opacity-[0.04]` — floral overlap, organic scatter.

- **Wavy Lines**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2280%22%20height=%2220%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M0%2010Q20%200%2040%2010T80%2010%22%20stroke=%22%233D2C1E%22%20stroke-width=%220.75%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.05]` — flowing sine, silk current.

#### Precision Tech (DARK · stroke `#fff` — the only dark-theme archetype)
Default opacity `0.08`. Glows, circuits, nodes.

**Use when:** Precision Tech archetype (the only dark-theme archetype). These use LIGHT (`%23fff`) strokes by default — do NOT invert.

- **Tech Grid** (default):
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2732%27%20height=%2732%27%20xmlns=%27http://www.w3.org/2000/svg%27%3E%3Cpath%20d=%27M0%2032H32V0%27%20stroke=%22%23fff%22%20stroke-width=%221%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.08]` — fine corner grid.

- **Node Network**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2240%22%20height=%2240%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Ccircle%20cx=%225%22%20cy=%225%22%20r=%221.5%22%20fill=%22%23fff%22/%3E%3Ccircle%20cx=%2235%22%20cy=%225%22%20r=%221.5%22%20fill=%22%23fff%22/%3E%3Ccircle%20cx=%2220%22%20cy=%2220%22%20r=%221.5%22%20fill=%22%23fff%22/%3E%3Ccircle%20cx=%225%22%20cy=%2235%22%20r=%221.5%22%20fill=%22%23fff%22/%3E%3Ccircle%20cx=%2235%22%20cy=%2235%22%20r=%221.5%22%20fill=%22%23fff%22/%3E%3Cpath%20d=%22M5%205L20%2020L35%205M20%2020L5%2035M20%2020L35%2035%22%20stroke=%22%23fff%22%20stroke-width=%220.5%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.08]` — dots + connecting traces, neural.

- **Hex Grid**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2228%22%20height=%2232%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M14%200L28%208V24L14%2032L0%2024V8Z%22%20stroke=%22%23fff%22%20stroke-width=%220.5%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.06]` — hexagon outlines, infrastructure.

- **Circuit Traces**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2240%22%20height=%2240%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M0%205H15V15H25V5H40M0%2025H10V35H25V25H40%22%20stroke=%22%23fff%22%20stroke-width=%220.75%22%20fill=%22none%22/%3E%3Ccircle%20cx=%2215%22%20cy=%2215%22%20r=%221.5%22%20fill=%22%23fff%22/%3E%3Ccircle%20cx=%2225%22%20cy=%2225%22%20r=%221.5%22%20fill=%22%23fff%22/%3E%3Ccircle%20cx=%2210%22%20cy=%2235%22%20r=%221.5%22%20fill=%22%23fff%22/%3E%3C/svg%3E')]
```
`opacity-[0.07]` — right-angle traces + nodes, PCB.

- **Fine Dots**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2216%22%20height=%2216%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Ccircle%20cx=%222%22%20cy=%222%22%20r=%220.6%22%20fill=%22%23fff%22/%3E%3C/svg%3E')]
```
`opacity-[0.06]` — microscopic dot field, sub-grid.

#### Clinical Trust (LIGHT · fill `#0F172A` → invert to `#fff` for dark)
Default opacity `0.05`. Microscopic, sterile, precise.

**Use when:** Clinical Trust archetype. Swap `fill=%230F172A` / `stroke=%230F172A` → `%23fff` to invert for a dark Clinical build.

- **Micro Dot Grid** (default):
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2720%27%20height=%2720%27%20xmlns=%27http://www.w3.org/2000/svg%27%3E%3Ccircle%20cx=%272%27%20cy=%272%27%20r=%271%27%20fill=%27%230F172A%27/%3E%3C/svg%3E')]
```
`opacity-[0.05]` — dot grid, graph paper.

- **Plus Grid**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2220%22%20height=%2220%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M10%207V13M7%2010H13%22%20stroke=%22%230F172A%22%20stroke-width=%220.75%22/%3E%3C/svg%3E')]
```
`opacity-[0.05]` — crosshair pluses, calibration.

- **Fine Crosshatch**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2216%22%20height=%2216%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M0%2016L16%200M0%200L16%2016%22%20stroke=%22%230F172A%22%20stroke-width=%220.25%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.04]` — thin X-grid, etched glass.

- **Empty Squares**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2220%22%20height=%2220%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M5%205H15V15H5Z%22%20stroke=%22%230F172A%22%20stroke-width=%220.5%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.04]` — square outlines, assay plate.

- **Ruler Tick Marks**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2220%22%20height=%2220%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M0%200V3M5%200V2M10%200V3M15%200V2M20%200V3%22%20stroke=%22%230F172A%22%20stroke-width=%220.5%22/%3E%3C/svg%3E')]
```
`opacity-[0.06]` — top-edge ticks, measurement.

#### Universal (cross-archetype — swap stroke color to match the host archetype)
Default opacity `0.04–0.08` depending on density. Use sparingly to avoid archetype collision; prefer the archetype-native textures above and reach for these only when a section needs to read as "structural" without committing to the archetype's signature pattern.

**Use when:** a section must read as "structural" without committing to the archetype's signature pattern. Swap the stroke color to match the host archetype.

- **Noise / Grain** (use `mix-blend-overlay`):
```astro
bg-[url('data:image/svg+xml,%3Csvg%20xmlns=%27http://www.w3.org/2000/svg%27%20width=%27200%27%20height=%27200%27%3E%3Cfilter%20id=%27n%27%3E%3CfeTurbulence%20type=%27fractalNoise%27%20baseFrequency=%270.9%27%20numOctaves=%273%27/%3E%3C/filter%3E%3Crect%20width=%27200%27%20height=%27200%27%20filter=%27url(%23n)%27/%3E%3C/svg%3E')]
```
`opacity-[0.03]` — film grain, fixed overlay only (§11 Performance).

- **Diagonal Stripes** (hazard/utility):
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2210%22%20height=%2210%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M0%200L10%2010M0%205L5%2010M5%200L10%205%22%20stroke=%22%23000%22%20stroke-width=%221%22/%3E%3C/svg%3E')]
```
`opacity-[0.06]` — dense 45° stripes, caution.

- **Isometric Diamond Grid**:
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2220%22%20height=%2235%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Cpath%20d=%22M10%200L20%2017.5L10%2035L0%2017.5Z%22%20stroke=%22%23000%22%20stroke-width=%220.5%22%20fill=%22none%22/%3E%3C/svg%3E')]
```
`opacity-[0.05]` — diamond grid, architectural.

- **Bubbles** (organic scatter):
```astro
bg-[url('data:image/svg+xml,%3Csvg%20width=%2240%22%20height=%2240%22%20xmlns=%22http://www.w3.org/2000/svg%22%3E%3Ccircle%20cx=%2210%22%20cy=%2210%22%20r=%222%22%20fill=%22none%22%20stroke=%22%233D2C1E%22%20stroke-width=%220.5%22/%3E%3Ccircle%20cx=%2230%22%20cy=%2225%22%20r=%223%22%20fill=%22none%22%20stroke=%22%233D2C1E%22%20stroke-width=%220.5%22/%3E%3Ccircle%20cx=%2215%22%20cy=%2232%22%20r=%221.5%22%20fill=%22none%22%20stroke=%22%233D2C1E%22%20stroke-width=%220.5%22/%3E%3C/svg%3E')]
```
`opacity-[0.04]` — varied circles, aerated.
