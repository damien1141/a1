# Anti-Slop Rules (Astro)

Adapted from `unslopify-astro`. Subtract only — never redecorate. The build never gets worse. Every edit passes one test: would a human who charged this amount have done this?

## Value Anchor

Set the anchor before any edit (the amount a human would charge to build this site):

1. Argument passed (`/unslopify-astro $6000`).
2. User instruction ("keep it premium" → $6,000).
3. Repo signal: display font file, photography-led layout, source copy with materials/edition sizes/lead times/"by appointment".
4. Default: $5,000.

Values between floors map up: $2,500+ uses the $5,000 floor.

| | $1,500 | $5,000 |
|---|---|---|
| voice | blunt tradesman | quiet provenance: materials, method, count, lead time |
| type | system stack. headlines 2-2.5rem, weight 600-700 | keep the display face in repo. 3-4.5rem, weight 300-500 |
| space | sections 3-5rem. container ~1100px | sections 6-10rem. air is the signal |
| measure | ~65ch | 30-45ch |
| palette | light bg, near-black text, one accent | warm neutrals: paper/bone bg, near-black ink, one muted accent |
| motion | hover only, 200ms max | hover only, 400ms max. image opacity crossfade allowed |
| imagery | keep what is real | keep it. full-bleed photography is legitimate |

**Zero keyframes at any value.** Record the anchor and what decided it in the report.

## Banned Punctuation (zero tolerance in rendered copy)

| Character | Name | Action |
|---|---|---|
| `—` | em dash | kill. replace with period, comma, or split into two sentences |
| `–` | en dash used as pause | kill. hyphen for ranges, or the word "to" |
| `…` | ellipsis | kill. end the sentence |
| `!` | exclamation | kill. zero exclamations in marketing copy |
| any emoji | decoration | delete outright. never substitute with another glyph |

Exempt: code fences, `<pre>`/`<code>`, `set:html` blobs, URLs, slugs, filenames, package names, identifiers. A slug containing an em dash keeps it — renaming breaks routes.

## Banned Words

Delete or rewrite every instance in user-facing copy:

seamless, elevate, robust, unlock, empower, streamline, supercharge, revolutionize, transform, reimagine, unleash, delve, embark, leverage, harness, thrive, flourish, craft/crafted/curate (see whitelist), cutting-edge, state-of-the-art, game-changing, next-level, world-class, best-in-class, hassle-free, effortless, blazing-fast, lightning-fast, unparalleled, unrivaled, unprecedented, future-proof, turnkey, plug-and-play, immersive, captivating, delightful, stunning, gorgeous, sleek, vibrant, intuitive, user-friendly, pixel-perfect, fully-customizable, bespoke, journey, ecosystem, synergy, paradigm, realm, landscape, treasure trove, powerhouse, one-stop shop, silver bullet, secret sauce, holy grail, gold standard, cornerstone, bedrock, deep dive, north star

Fake-premium tells, banned at every anchor: exquisite, opulent, sumptuous, decadent, indulgent, pamper, epitome, testament, timeless, luxury/luxurious as self-description.

## Banned Phrases / Sentence Shapes

- "look no further"
- "in today's fast-paced world" / "in the ever-evolving landscape of"
- "whether you're a ... or a ..." (pick who it is for, say that)
- "it's not just X, it's Y" (say what it is, once)
- "take X to the next level"
- "gone are the days" / "say goodbye to" / "say hello to"
- "meet your new ..."
- "we're passionate about" / "we believe" / "our mission is"
- "imagine a world where" / "what if we told you"
- "the future of X is here"
- "bring your vision to life" / "your vision, our expertise"
- "the possibilities are endless"
- "stay ahead of the curve" / "peace of mind" / "at your fingertips"
- "with just a few clicks" / "like never before" / "second to none"
- "more than just" / "not your average" / "everything you need"
- "harness the power of"
- "built for the modern ..." / "designed with you in mind" / "attention to detail"
- "from concept to completion" / "results that speak for themselves"
- "trusted by industry leaders" / "join thousands of ..."
- "don't settle for ..."
- "where X meets Y" / "the art of X" / "a testament to" / "indulge in" / "treat yourself"
- "experience true luxury" / "discover luxury"
- rhetorical question openers: "looking for X?" / "tired of X?" / "struggling with X?" (state the offer instead)
- tricolon headlines: "fast. reliable. affordable." (pick the one that is true and prove it)
- "supercharge your X" verb-your-noun headlines (say what the thing does)

## Banned Button Labels

"get started", "learn more", "discover", "explore", "see what's possible", "start your journey".

Replace with what actually happens on click: "get a quote", "see the work", "call us", or at the $5,000 anchor: "view the collection", "book an appointment", "inquire about a commission".

Trailing arrow: banned when the label is empty ("learn more ->"). Allowed at the $5,000 anchor with a concrete label, plain text or thin glyph, no animation on the arrow.

## CSS Slop (delete on sight, every anchor)

- gradient text: `background-clip: text` / `-webkit-background-clip: text` (Tailwind: `bg-clip-text` + `text-transparent`)
- glassmorphism: `backdrop-filter: blur()` (if a sticky header needs legibility, give it a solid background instead)
- decorative `@keyframes`: float, blob, morph, marquee, scroll, aurora, gradient-shift, shimmer, pulse, glow, spin, bounce
- any `animation` on a decorative element
- scroll-reveal: `.reveal` classes, `[data-animate]`, IntersectionObserver wiring that exists only for entrance effects (UNLESS the project is the high-end-visual-design pipeline which uses one global observer — that's a load-bearing pattern, not slop)
- glow shadows: colored `box-shadow`, `0 0 Npx rgba(...)`
- gradient borders (background-clip trick or animated `@property --angle`)
- animated gradient buttons
- hover `transform: scale()` on cards
- decorative blur orbs, grid/dot background overlays (decorative; archetype textures are NOT decorative — they are the design), noise/grain textures (the Industrial grain is load-bearing, NOT slop)
- custom cursors, magnetic buttons, anything `position: fixed` that is purely decorative
- any `transition` longer than the anchor budget ($1,500: 200ms, $5,000: 400ms)

## Banned Palette (grep targets)

Hex: `#4f46e5 #6366f1 #818cf8 #7c3aed #8b5cf6 #a855f7 #9333ea #c026d3 #d946ef #ec4899 #db2777`

Tailwind: `indigo/violet/purple/fuchsia/pink` at 400-800.

(Allowed: archetype-specific palettes — Industrial amber, Luxury sage/espresso, Tech electric blue, Clinical teal. These are load-bearing design decisions, not slop.)

## Removal Traps (each has shipped a broken page)

- **Reveal amputation** — if CSS hides content by default (`.reveal { opacity: 0 }`), removing the observer JS without removing that CSS leaves a blank page. Remove both in the same edit. The number-one way to break a site.
- **Sticky header losing `backdrop-filter`** — give it a solid background in the same edit.
- **Keyframe orphans** — delete `@keyframes` together with the `animation:` declarations that reference them, and any `prefers-reduced-motion` blocks guarding only the motion you just deleted.
- **Fonts** — at $5,000, keep the display face already in the repo; that's preservation, not addition. Never add or download font files at any anchor. Removing a Google Fonts `<link>` is subtraction; adding one is not.
- **Color tokens** — if slop colors are CSS custom properties, change the token once, not every usage.
- **$5,000 over-flattening** — kill the ornament, not the air. Do not compress $5,000 whitespace or type scale to $1,500 numbers.

## Structure Slop (kill outright)

- badge/pill above the h1 ("new", "announcing", sparkle pill). A plain-text kicker per Pass 3 is not a badge.
- eyebrow/kicker labels: banned at $1,500; conditional at $5,000.
- the two-button hero pair (primary + ghost "learn more") → one button.
- three identical feature cards with icon blobs → plain text list or asymmetric layout ($5,000: asymmetric image-plus-text grid).
- "trusted by" logo marquee without real, verifiable logos ($5,000 alternative: one quiet line of real press or stockist names from source).
- stat bars: apply the stats heuristic. Never round up.
- testimonial walls with 5-star svg rows → at most one quote that already exists in the source data, with the name it came with, or none. Never fabricate.
- pricing cards with "most popular" ribbons → plain list or table ($5,000: a commissions or "by appointment" section with real lead times).
- full-bleed gradient CTA banner → plain section, one button.
- blog teasers with dummy posts → cut, or link real posts only.
- 4-column footer of dead links → business name, what it does, contact, legal.
- emoji bullet lists → plain bullets or prose.

## Whitelist Rules

- Proper nouns and brand names containing banned words: keep, list in report. Includes brand names containing "luxury" or "timeless".
- "craft/craftsmanship": keep only where hands touch the product. True for a joiner or a jewelry house; false for a design studio.
- Domain vocabulary: "journey" on a travel site, "curate" at a gallery, "craft" at a bakery are the trade's own words. Keep, list in report.
- Banned words or characters inside URLs, package names, identifiers, code fences, `<pre>`/`<code>`, or `set:html` blobs: leave untouched. Slugs keep their punctuation; renaming breaks routes.
- CMS-sourced copy you cannot edit in this repo: flag in report, do not fake-edit.

## Audit Gauntlet (mandatory, run all)

```bash
# package manager
if [ -f pnpm-lock.yaml ]; then PM=pnpm
elif [ -f yarn.lock ]; then PM=yarn
elif [ -f bun.lockb ] || [ -f bun.lock ]; then PM=bun
else PM=npm; fi
echo "PM=$PM"

# punctuation (eyeball hits: code fences and slugs are exempt, whitelist them)
rg -n '[—–…]' src/ || echo CLEAN_PUNCTUATION

# buzzwords
rg -niE '\b(seamless(ly)?|elevat(e|es|ed|ing)|robust|unlock(s|ed|ing)?|empower(s|ed|ing)?|streamline[sd]?|supercharge[sd]?|revolutioniz(e|es|ing)|cutting[- ]edge|state[- ]of[- ]the[- ]art|game[- ]chang(er|ing)?|next[- ]level|world[- ]class|best[- ]in[- ]class|hassle[- ]free|effortless(ly)?|blazing[- ]fast|lightning[- ]fast|unparalleled|unrivaled|unprecedented|future[- ]proof|turnkey|immersive|captivating|delight(ful)?|stunning|gorgeous|sleek|vibrant|intuitive|user[- ]friendly|pixel[- ]perfect|bespoke|curate[sd]?|craft(ed|ing|smanship)?|leverage[sd]?|harness(es|ed|ing)?|embark(s|ed|ing)?|delve[sd]?|realm(s)?|synergy|paradigm|journey|ecosystem|treasure[- ]trove|powerhouse|one[- ]stop[- ]shop|silver[- ]bullet|secret[- ]sauce|holy[- ]grail|gold[- ]standard|cornerstone|bedrock|unleash(es|ed|ing)?|reimagin(e|es|ing)|thrive[sd]?|flourish(es|ed)?|exquisite|opulent|sumptuous|decaden(t|ce)|indulgen(t|ce)|pamper(s|ed|ing)?|epitome(s)?|testament|timeless|luxur(y|ious))\b' src/ || echo CLEAN_WORDS

# banned phrases
rg -rniE '(look no further|in today.?s fast[- ]paced|in (the|an) ever[- ]evolving|whether you.?re a|it.?s not just .{1,40} it.?s|to the next level|gone are the days|say goodbye to|say hello to|meet your new|we.?re passionate|we believe|our mission (is|to)|imagine a world|what if we told you|the future of .{1,30} is here|built for the modern|designed with (you|your) in mind|attention to detail|bring your vision to life|your vision, our|the possibilities are endless|stay ahead of the curve|peace of mind|at your fingertips|with just a few clicks|like never before|second to none|everything you need to|more than just|not your average|harness the power of|from concept to completion|results that speak|trusted by (industry|leading|thousands)|join thousands|tired of|struggling with|don.?t settle|where [a-z]+ meets [a-z]+|the art of|a testament to|indulge in|treat yourself|experience (true )?luxury|discover luxury)' src/ || echo CLEAN_PHRASES

# emoji
rg -n '[\x{1F300}-\x{1FAFF}\x{2600}-\x{27BF}]' src/ || echo CLEAN_EMOJI

# css crimes (any keyframe or animation is a finding at every anchor; high-end-visual-design archetype textures are NOT findings — they're load-bearing)
rg -niE '(-webkit-)?background-clip: ?text|backdrop-filter|@keyframes|animation:|linear-gradient|radial-gradient|conic-gradient' src/ || echo CLEAN_CSS

# tailwind slop utilities
rg -noE '(bg-gradient-to-[a-z]+|bg-clip-text|text-transparent|backdrop-blur(-[a-z0-9]+)?|animate-[a-z-]+|hover:scale-[0-9]+|drop-shadow(-[a-z]+)?)' src/ || echo CLEAN_TW

# neon template palette
rg -niE '#(4f46e5|6366f1|818cf8|7c3aed|8b5cf6|a855f7|9333ea|c026d3|d946ef|ec4899|db2777)\b' src/ || echo CLEAN_PALETTE
rg -noE '\b(indigo|violet|purple|fuchsia|pink)-(4|5|6|7|8)00\b' src/ || echo CLEAN_PALETTE_TW

# hidden defaults
rg -rniE '(opacity: ?0|visibility: ?hidden|opacity-0\b)' src/ || echo CLEAN_HIDDEN

# transition durations (eyeball against the anchor budget)
rg -niE 'transition[^;]*[0-9]+(\.[0-9]+)?(s|ms)' src/ || echo CLEAN_TRANSITIONS

# build
$PM run build 2>&1 | tail -20

# subtraction proof
git diff --shortstat 2>/dev/null || echo NO_GIT
```

Every non-CLEAN hit gets fixed or whitelisted with a reason. A build failure only passes if the identical failure exists in the baseline.

## Slop Score

1 point each: buzzword, em dash, gradient, glow shadow, badge pill, rhetorical question, arrow-suffix button with an empty label.
2 points each: glassmorphism, fake stat, testimonial wall, identical feature-card trio, logo marquee, keyframe animation.

Whitelisted hits score 0. Score before and after. Ship only at 0.

## Report Format

```
## unslop report
anchor: $N (what decided it)
baseline: build passed/failed before edits (errors, if failed)
files touched: N (list)
killed: X buzzwords, Y em dashes, Z css crimes, W structure patterns
gaps: facts the copy now needs from the owner (cut, not faked)
sample rewrites: 3 before/after pairs, biggest changes
locales: which were touched; parallel cuts kept in sync: yes/no
gauntlet: <paste actual terminal output, every line>
build: <paste actual tail output> + no new errors vs baseline: yes/no
diff: <git diff --shortstat> net negative: yes/no (or "no git: unverified")
slop score: before -> after
whitelisted: <item + reason>
verified: <what you checked and how>
unverified: <what you could not check, e.g. visual appearance>
```

Report observed output, not intended behavior. If a check was skipped, say so.
