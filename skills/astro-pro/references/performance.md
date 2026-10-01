# Performance

Image optimization, script loading, partial hydration, caching, and Astro DB. Lighthouse ≥ 90 across all categories is the bar.

## Image Optimization (`astro:assets`)

Astro ships built-in image optimization. Import images from `src/` and use `<Image>` or `<Picture>`:

```astro
---
import { Image } from 'astro:assets';
import hero from '../assets/hero.jpg';
---
<Image
  src={hero}
  alt="Workshop floor"
  widths={[240, 540, 720, 1080]}
  sizes="(max-width: 800px) 100vw, 1080px"
  loading="eager"
  fetchpriority="high"
/>
```

```astro
---
import { Picture } from 'astro:assets';
import heroAvif from '../assets/hero.avif';
---
<Picture
  src={heroAvif}
  formats={['avif', 'webp']}
  alt="Workshop"
  widths={[240, 540, 1080]}
  sizes="100vw"
/>
```

Remote images: configure allowed domains in `astro.config.mjs`:

```ts
export default defineConfig({
  image: {
    domains: ['cdn.example.com'],
    remotePatterns: [{ protocol: 'https' }],
    service: { entrypoint: 'astro/assets/services/sharp' },
  },
});
```

`getImage()` for non-component use:

```ts
import { getImage } from 'astro:assets';
const optimized = await getImage({ src: hero, width: 540, format: 'webp' });
// optimized.src — URL to the optimized file
```

Rules:
- `<Image>` for raster (jpg/png). `<Picture>` for multiple formats with fallbacks.
- Always provide `widths` + `sizes` — without them, Astro ships one size fits all.
- `loading="lazy"` (default) for below-fold; `loading="eager" fetchpriority="high"` for above-fold hero images.
- SVGs pass through unchanged — no optimization, no `widths`.
- Use `format="avif"` for the smallest size; `webp` for broader compatibility.

## Script Loading

Astro processes `<script>` tags by default — bundles, type-checks, hoists. Use directives to change behavior:

```astro
<!-- Default: processed, bundled, type-checked, deferred (execute after HTML parse) -->
<script>
  import { init } from '../lib/init.js';
  init();
</script>

<!-- is:inline: raw, not processed, runs immediately -->
<script is:inline src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js" defer></script>

<!-- is:inline with define:vars: pass server values into raw script -->
<script is:inline define:vars={{ productId }}>
  window.productId = productId;
</script>
```

Rules:
- Default scripts are bundled and deduped — if the same module is imported in multiple components, it's included once.
- `is:inline` scripts run every time the component renders. Use for CDN libraries (Alpine, htmx, Stripe.js).
- Don't `import 'alpinejs'` and call `Alpine.start()` — use the CDN `<script defer>` pattern instead. Alpine self-initializes.
- For View Transitions: scripts in `is:inline` re-run on every navigation. Default (processed) scripts run once on initial load — re-init any stateful code in `astro:after-swap`.

## Partial Hydration

The single biggest performance lever in Astro. Static HTML ships zero JS by default; only islands with `client:*` directives hydrate.

| Directive | When | Ships JS? |
|---|---|---|
| (none) | Server-rendered only | No |
| `client:load` | On page load | Yes, immediately |
| `client:idle` | Browser idle | Yes, deferred |
| `client:visible` | Scrolled into view | Yes, deferred |
| `client:media="(min-width: 800px)"` | Media query matches | Yes, conditionally |
| `client:only="react"` | Client only, no SSR | Yes, after hydration |

Rule of thumb: default to no directive. Add `client:visible` for below-fold interactivity. Use `client:load` only for hero-region interactive components. `client:only` is a code smell — usually means the component has a SSR bug.

## Caching

SSR routes can cache per-request:

```astro
---
// src/pages/api/slow-data.ts
export const GET = () => {
  return new Response(JSON.stringify({ time: Date.now() }), {
    headers: {
      'Content-Type': 'application/json',
      'Cache-Control': 'public, s-maxage=60, stale-while-revalidate=86400',
    },
  });
};
```

`<Cache>` component (community `@inox-tools/cache`) or manual:

```astro
---
// Per-page caching (Cloudflare/Vercel edge)
export const prerender = false;
Astro.response.headers.set('Cache-Control', 's-maxage=60, stale-while-revalidate=600');
---
```

For static builds: cache at the CDN edge via headers set in `astro.config.mjs` or platform config (`vercel.json`, `_headers`, etc.).

## Astro DB (Astro Studio)

Astro DB is a managed SQLite-based database for Astro projects. Type-safe access via `db:astro`:

```ts
// astro.config.mjs
import { defineConfig } from 'astro/config';
import db from '@astrojs/db';

export default defineConfig({ integrations: [db()] });
```

```ts
// src/db/config.ts
import { defineDb, defineTable, column } from 'astro:db';

const Posts = defineTable({
  columns: {
    id: column.number({ primaryKey: true }),
    title: column.text(),
    pubDate: column.date(),
  },
});

export default defineDb({ tables: { Posts } });
```

```ts
import { db, Posts } from 'astro:db';
const all = await db.select().from(Posts);
const [first] = await db.insert(Posts).values({ title: 'Hello', pubDate: new Date() }).returning();
```

Note: Astro DB is in beta and tied to Astro Studio. For production workloads, prefer Postgres (Neon, Supabase) via `postgres` or Drizzle.

## Performance Budget

| Metric | Target |
|---|---|
| Lighthouse Performance | ≥ 90 |
| LCP (Largest Contentful Paint) | < 2.5s on 4G |
| CLS (Cumulative Layout Shift) | < 0.1 |
| INP (Interaction to Next Paint) | < 200ms |
| Total JS shipped (initial) | < 100KB gzipped |
| Image weight (above-fold) | < 200KB |
| Fonts | ≤ 2 families, `font-display: swap`, subset to latin |

## Font Loading

Self-host fonts via `@fontsource/*`:

```astro
---
// Layout.astro — fonts ship as part of the build, no external requests
import '@fontsource/inter/400.css';
import '@fontsource/inter/600.css';
import '@fontsource/inter/700.css';
import '@fontsource/jetbrains-mono/400.css';
---
```

Avoid Google Fonts CDN — it's render-blocking and adds a third-party request.

## Lazy-loading Below-the-Fold

```astro
---
import HeavyMap from '../components/HeavyMap.jsx';
---
<!-- Map only loads JS when the user scrolls to it -->
<HeavyMap client:visible />
```

For images below the fold, `loading="lazy"` is the default — no action needed.

## Anti-patterns

- **Shipping a framework just for one component** — if you only need a carousel, consider a vanilla JS island or htmx before pulling in React.
- **`client:load` everywhere** — slow hydration, large initial JS. Default to `client:visible`.
- **Using `<img>` instead of `<Image>`** — bypasses optimization, ships full-resolution images.
- **Google Fonts CDN** — render-blocking. Self-host via Fontsource.
- **Multiple analytics scripts in `<head>`** — use Partytown (`@astrojs/partytown`) to move them off the main thread.
- **`prefers-reduced-motion` ignored** — animations tank INP for users with motion sensitivity. Always honor it.
- **Background SVG base64-encoded** — Astro's image pipeline doesn't optimize CSS `url()` background images. Use JIT-safe URL-encoded SVGs (see `templates/svg-textures.md`).

## Verification

```bash
astro build                            # production build
npx lighthouse http://localhost:4321 --view   # Lighthouse report
npx @axe-core/cli http://localhost:4321       # a11y audit
ls -la dist/                            # check asset sizes
du -sh dist/_astro/*.js                 # total JS shipped
```
