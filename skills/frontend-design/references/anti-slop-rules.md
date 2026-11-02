# Anti-Slop Rules

Audit rules for de-templating AI-generated frontend code. Subtract, do not redecorate. Real value is subtractive: type discipline, whitespace, accurate copy, restraint.

## Value anchor

Set the anchor before any edit: the amount a human would charge to build this site. Hold the number for every decision.

| | $1,500 | $5,000 |
|---|---|---|
| Voice | Blunt tradesman. Contractions, short declaratives | Quiet provenance: materials, method, count, lead time |
| Type | System stack. Headlines 2–2.5rem, weight 600–700 | Keep the display face already in the repo. 3–4.5rem, weight 300–500 |
| Space | Sections 3–5rem. Container ~1100px | Sections 6–10rem. Air is the signal |
| Measure | ~65ch | 30–45ch |
| Palette | Light bg, near-black text, one accent | Warm neutrals: paper/bone bg, near-black ink, one muted accent |
| Motion | Hover only, 200ms max | Hover only, 400ms max. Image opacity crossfade allowed |
| Imagery | Keep what is real | Keep it. Full-bleed photography is legitimate |

**Zero keyframes at any value.** Resolution order: argument → user instruction → repo signal → default $5,000. Values between floors map up: $2,500+ uses the $5,000 floor.

## Prime directives

1. **Subtract only.** No new features, components, files, dependencies. If insertions outnumber deletions, you gold-plated it. Stop and cut.
2. **Never fabricate.** Rewrite copy only from facts in the source. Unsupported claim: cut it, log it in the gap list. Never invent quotes, stats, names, testimonials. Never ship bracket placeholders.
3. **The build never gets worse.** Baseline in Pass 0. Rebuild after each pass. Final build failure that passed baseline → fix your edits before reporting.
4. **No permission-seeking mid-run.** Complete all passes, then report.
5. **Batch by pass.** Each pass runs across all files before the next pass starts.

Brand override: real brand assets (logo, brand colors, genuinely distinct voice) are preserved. Rules apply to slop only.

## Banned punctuation (zero tolerance in rendered copy)

| Character | Name | Action |
|---|---|---|
| `—` | em dash | Kill. Replace with period, comma, or split into two sentences |
| `–` | en dash used as pause | Kill. Hyphen for ranges, or the word "to" |
| `…` | ellipsis | Kill. End the sentence |
| `!` | exclamation | Kill. Zero exclamations in marketing copy |
| any emoji | decoration | Delete outright. Never substitute with another glyph |

## Banned words

Delete or rewrite every instance in user-facing copy:

seamless, elevate, robust, unlock, empower, streamline, supercharge, revolutionize, transform, reimagine, unleash, delve, embark, leverage, harness, thrive, flourish, craft/crafted/curate (see whitelist), cutting-edge, state-of-the-art, game-changing, next-level, world-class, best-in-class, hassle-free, effortless, blazing-fast, lightning-fast, unparalleled, unrivaled, unprecedented, future-proof, turnkey, plug-and-play, immersive, captivating, delightful, stunning, gorgeous, sleek, vibrant, intuitive, user-friendly, pixel-perfect, fully-customizable, bespoke, journey, ecosystem, synergy, paradigm, realm, landscape, treasure trove, powerhouse, one-stop shop, silver bullet, secret sauce, holy grail, gold standard, cornerstone, bedrock, deep dive, north star

**Fake-premium tells (banned at every anchor):** exquisite, opulent, sumptuous, decadent, indulgent, pamper, epitome, testament, timeless, luxury/luxurious as self-description.

## Banned phrases & sentence shapes

"look no further", "in today's fast-paced world", "in the ever-evolving landscape of", "whether you're a ... or a ..." (pick who it is for), "it's not just X, it's Y" (say what it is, once), "take X to the next level", "gone are the days", "say goodbye to", "say hello to", "meet your new ...", "we're passionate about", "we believe", "our mission is", "imagine a world where", "what if we told you", "the future of X is here", "bring your vision to life", "your vision, our expertise", "the possibilities are endless", "stay ahead of the curve", "peace of mind", "at your fingertips", "with just a few clicks", "like never before", "second to none", "more than just", "not your average", "everything you need", "harness the power of", "built for the modern ...", "designed with you in mind", "attention to detail", "from concept to completion", "results that speak for themselves", "trusted by industry leaders", "join thousands of ...", "don't settle for ...", "where X meets Y", "the art of X", "a testament to", "indulge in", "treat yourself", "experience true luxury", "discover luxury".

Rhetorical question openers: "looking for X?" / "tired of X?" / "struggling with X?" (state the offer instead).
Tricolon headlines: "fast. reliable. affordable." (pick the one that is true and prove it).
"supercharge your X" style verb-your-noun headlines (say what the thing does).

## Banned button labels

"get started", "learn more", "discover", "explore", "see what's possible", "start your journey".

Replace with what actually happens on click: "get a quote", "see the work", "call us", or at the $5,000 anchor: "view the collection", "book an appointment", "inquire about a commission".

Trailing arrow banned when label is empty ("learn more →"). Allowed at $5,000 anchor with a concrete label, plain text or thin glyph, no animation on the arrow.

## Voice rules (all registers)

1. Say what it does, who it is for, what it costs or how long it takes, and what happens next ("price on request" is valid at $5,000). That is the whole site.
2. Subject-verb-object. Short sentences at $1,500; measured sentences at $5,000, never past ~20 words.
3. Concrete nouns, real verbs. Drop adjectives unless they carry a fact.
4. No hedging, no passion claims, no mission statements, no rhetorical questions, no exclamations.
5. Mechanism over metric: "built as plain HTML first, so it loads on anything" beats unsourced "blazing fast". At $5,000, materials and method beat adjectives.
6. If a claim cannot be supported from source material, cut it.

## Before / after calibration

Hero ($1,500):
- Slop: "elevate your digital presence. we craft seamless, cutting-edge web experiences that empower your business to thrive."
- Fixed: "harrison roofing replaces roofs in leeds. quotes in 48 hours. most jobs done in two days."

CTA:
- Slop: "ready to embark on your journey? let's bring your vision to life. contact us today!"
- Fixed: "tell us what you need. we'll give you a price and a date."

Hero ($5,000):
- Slop: "step into a world of exquisite design. where timeless craftsmanship meets modern sensibility."
- Fixed: "signet rings in sterling and 9k gold. hand-engraved by two jewelers. the waiting list runs about five weeks."

## CSS slop (delete on sight, every anchor)

- **Gradient text** (`background-clip: text` / Tailwind `bg-clip-text` + `text-transparent`).
- **Glassmorphism** (`backdrop-filter: blur()` — give sticky headers a solid bg instead).
- **Decorative `@keyframes`** (float/blob/morph/marquee/scroll/aurora/gradient-shift/shimmer/pulse/glow/spin/bounce) and any `animation` on a decorative element.
- **Scroll-reveal entrance effects** (`.reveal` classes, `[data-animate]`, IntersectionObserver wiring that exists only for entrance effects).
- **Glow shadows** (colored `box-shadow`, `0 0 Npx rgba(...)`), **gradient borders** (background-clip trick or `@property --angle`), **animated gradient buttons**.
- **Hover `transform: scale()` on cards.**
- **Decorative blur orbs, grid/dot background overlays, noise/grain textures, custom cursors, magnetic buttons, anything `position: fixed` that is purely decorative.**
- **Any `transition` longer than the anchor budget** ($1,500: 200ms, $5,000: 400ms).

## Removal traps (each has shipped a broken page)

- **Reveal amputation.** If CSS hides content by default (`.reveal { opacity: 0 }`), removing the observer JS without removing that CSS leaves a blank page. Remove both in the same edit. The number one way this skill breaks a site.
- **Sticky header losing `backdrop-filter`.** Give it a solid background in the same edit. **Keyframe orphans.** Delete `@keyframes` together with the `animation:` declarations referencing them, and any `prefers-reduced-motion` blocks guarding only the motion you just deleted.
- **Fonts.** At $5,000, keep the display face already in the repo (preservation, not addition). Never add or download font files at any anchor. **Color tokens.** If slop colors are CSS custom properties, change the token once, not every usage. **$5,000 over-flattening.** Kill the ornament, not the air. Do not compress $5,000 whitespace or type scale to $1,500 numbers.

## Replacement floor

| Slop | $1,500 | $5,000 |
|---|---|---|
| Gradient backgrounds | One solid neutral | Warm paper or bone neutral |
| Glow/colored shadows | `border: 1px solid` neutral, or nothing | Same |
| `border-radius: 1rem+` everywhere | 0 to 6px | 0 to 6px |
| scale/shadow hover | Color, background, border, or underline, 200ms max | Same, 400ms max |
| 5rem tracking-tight headline | 2–2.5rem, weight 600–700, system stack | 3–4.5rem, weight 300–500, keep repo's display face |
| Full-bleed everything | Container ~1100px, body 65ch | Wider allowed; copy measure 30–45ch |
| 8–10rem section padding | 3–5rem | 6–10rem, keep the air |
| Dark neon palette | Light bg, near-black text, one accent | Dark warm (#141210 range, cream text) allowed if brand is real. No neon either way |
| Tailwind gradient/blur/animate utilities | Delete; solid colors, plain text | Same |

$5,000 allowances on top: full-bleed photography stays; a plain-text small-caps kicker is allowed (no pill, no border, no gradient, three words max, every section or none); gallery crossfade via opacity.

Motion budget: **zero keyframes at every anchor.** Transitions hover-only on `color`, `background-color`, `border-color`, `opacity`: 200ms at $1,500, 400ms at $5,000. Any surviving motion respects `prefers-reduced-motion`.

## Banned palette (grep targets)

Hex: `#4f46e5 #6366f1 #818cf8 #7c3aed #8b5cf6 #a855f7 #9333ea #c026d3 #d946ef #ec4899 #db2777`
Tailwind scale: `indigo/violet/purple/fuchsia/pink` at 400–800.

## Verification gauntlet (mandatory)

```bash
PM=$([ -f pnpm-lock.yaml ] && echo pnpm || ([ -f yarn.lock ] && echo yarn || echo npm))

# punctuation (eyeball hits: code fences and slugs are exempt)
rg -n '[—–…]' src/ || echo CLEAN_PUNCTUATION

# buzzwords (luxury/timeless may be real brand names — eyeball)
rg -niE '\b(seamless(ly)?|elevat(e|es|ed|ing)|robust|unlock(s|ed|ing)?|empower(s|ed|ing)?|streamline[sd]?|supercharge[sd]?|revolutioniz(e|es|ing)|cutting[- ]edge|next[- ]level|world[- ]class|hassle[- ]free|effortless(ly)?|blazing[- ]fast|lightning[- ]fast|unparalleled|bespoke|curate[sd]?|leverage[sd]?|harness(es|ed|ing)?|embark(s|ed|ing)?|journey|ecosystem|synergy|paradigm|powerhouse|exquisite|timeless|luxur(y|ious))\b' src/ || echo CLEAN_WORDS

# banned phrases
rg -rniE '(look no further|in today.?s fast[- ]paced|whether you.?re a|to the next level|gone are the days|say goodbye to|meet your new|we.?re passionate|we believe|our mission (is|to)|imagine a world|the future of .{1,30} is here|bring your vision to life|the possibilities are endless|stay ahead of the curve|peace of mind|at your fingertips|with just a few clicks|like never before|second to none|everything you need to|more than just|trusted by (industry|leading|thousands)|tired of|struggling with|don.?t settle|experience (true )?luxury|discover luxury)' src/ || echo CLEAN_PHRASES

# emoji
rg -n '[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}]' src/ || echo CLEAN_EMOJI

# CSS crimes (any keyframe or animation is a finding at every anchor)
rg -niE '(-webkit-)?background-clip: ?text|backdrop-filter|@keyframes|animation:|linear-gradient|radial-gradient|conic-gradient' src/ || echo CLEAN_CSS

# Tailwind slop utilities
rg -noE '(bg-gradient-to-[a-z]+|bg-clip-text|text-transparent|backdrop-blur(-[a-z0-9]+)?|animate-[a-z-]+|hover:scale-[0-9]+|drop-shadow(-[a-z]+)?)' src/ || echo CLEAN_TW

# neon palette
rg -niE '#(4f46e5|6366f1|818cf8|7c3aed|8b5cf6|a855f7|9333ea|c026d3|d946ef|ec4899|db2777)\b' src/ || echo CLEAN_PALETTE
rg -noE '\b(indigo|violet|purple|fuchsia|pink)-(4|5|6|7|8)00\b' src/ || echo CLEAN_PALETTE_TW

# build
$PM run build
```

Every non-CLEAN hit gets fixed or whitelisted with a reason. Build failure only passes if the identical failure exists in the baseline.

## Whitelist rules

- **Proper nouns & brand names** containing banned words: keep, list in report (includes brand names containing "luxury" or "timeless").
- **"craft/craftsmanship"**: keep only where hands touch the product (true for joiner/jewelry; false for design studio).
- **Domain vocabulary**: "journey" on travel, "curate" at a gallery, "craft" at a bakery — trade's own words. Keep, list in report.
- **Banned words/characters inside URLs, package names, identifiers, code fences, `<pre>`/`<code>`, `set:html` blobs**: leave untouched. Slugs keep punctuation; renaming breaks routes.
- **CMS-sourced copy you cannot edit**: flag in report, do not fake-edit.

## Slop score (1 point each)

buzzword, em dash, gradient, glow shadow, badge pill, rhetorical question, arrow-suffix button with empty label.
2 points each: glassmorphism, fake stat, testimonial wall, identical feature-card trio, logo marquee, keyframe animation.
Whitelisted hits score 0. Score before and after. **Ship only at 0.**

## Report format

```
anchor: $N (what decided it)
baseline: build passed/failed (errors)
files touched: N (list)
killed: X buzzwords, Y em dashes, Z css crimes, W structure patterns
gaps: facts the copy now needs from the owner (cut, not faked)
sample rewrites: 3 before/after pairs, biggest changes
gauntlet: <paste actual terminal output>
build: <tail output> + no new errors vs baseline: yes/no
diff: <git diff --shortstat> net negative: yes/no
slop score: before -> after
whitelisted: <item + reason>
verified / unverified: <what you checked / couldn't check>
```
