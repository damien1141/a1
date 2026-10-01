# Integrations

Installing and configuring Astro integrations. Astro 5 supports React, Vue, Svelte, Solid, Preact, MDX, Tailwind (v3 bridge only), and SSR adapters (Node, Cloudflare, Vercel, Netlify, Deno).

## Install Pattern

Use the `astro add` CLI — it updates `astro.config.*` and installs the package:

```bash
npx astro add react vue svelte mdx
npx astro add node         # SSR adapter
npx astro add tailwind     # Tailwind v3 only (see below)
```

`astro add` writes to `astro.config.mjs` (or `.ts`/`.js`) and runs the package manager install. Manual install requires both `npm install <pkg>` AND updating `integrations: []` / `adapter:` in the config.

## Manual Config

```ts
// astro.config.mjs
import { defineConfig } from 'astro/config';
import react from '@astrojs/react';
import vue from '@astrojs/vue';
import svelte from '@astrojs/svelte';
import mdx from '@astrojs/mdx';
import node from '@astrojs/node';

export default defineConfig({
  output: 'server', // or 'static' (default) with per-route prerender:false
  integrations: [react(), vue(), svelte(), mdx()],
  adapter: node({ mode: 'standalone' }),
});
```

## Framework Integrations

### React (`@astrojs/react`)

```ts
import react from '@astrojs/react';
export default defineConfig({
  integrations: [react()],
});
```

```astro
---
import Counter from '../components/Counter.jsx'; // .jsx or .tsx
---
<Counter client:load initial={0} />
```

Supports React 18 and 19. For 19, ensure `react@19` and `react-dom@19` are installed.

### Vue (`@astrojs/vue`)

```ts
import vue from '@astrojs/vue';
export default defineConfig({
  integrations: [vue({ appEntrypoint: '/src/entrypoints/app.js' })],
});
```

```astro
---
import Cart from '../components/Cart.vue';
---
<Cart client:visible />
```

Vue 3.4+. Composition API, `<script setup>`, and Pinia all work.

### Svelte (`@astrojs/svelte`)

```ts
import svelte from '@astrojs/svelte';
export default defineConfig({ integrations: [svelte()] });
```

```astro
---
import Toggle from '../components/Toggle.svelte';
---
<Toggle client:idle />
```

Svelte 5 (runes) supported. Svelte 4 still works.

### Solid (`@astrojs/solid-js`)

```ts
import solid from '@astrojs/solid-js';
export default defineConfig({ integrations: [solid()] });
```

Solid 1.8+. Always use `client:only="solid"` if the component touches browser APIs at module-eval time, since Solid compiles to non-SSR-friendly code in some cases.

## MDX (`@astrojs/mdx`)

```ts
import mdx from '@astrojs/mdx';
export default defineConfig({
  integrations: [mdx({
    syntaxHighlight: 'shiki',
    shikiConfig: { theme: 'github-dark' },
    gfm: true, // GitHub-flavored markdown
    remarkPlugins: [],
    rehypePlugins: [],
  })],
});
```

`.mdx` files can be in `src/pages/` (routes) or `src/content/` (collection entries).

## SSR Adapters

| Adapter | Use |
|---|---|
| `@astrojs/node` | Node.js server (Express-compatible via `mode: 'middleware'`, standalone via `mode: 'standalone'`) |
| `@astrojs/cloudflare` | Cloudflare Pages / Workers |
| `@astrojs/vercel` | Vercel (Edge or Node) |
| `@astrojs/netlify` | Netlify Functions |
| `@astrojs/deno` | Deno Deploy |

```ts
import node from '@astrojs/node';
export default defineConfig({
  output: 'server',
  adapter: node({ mode: 'standalone' }), // produces dist/server/entry.mjs
});
```

Run: `node ./dist/server/entry.mjs`

## Tailwind (v3 vs v4)

### Tailwind v4 (recommended for new projects)

No `@astrojs/tailwind` integration. Use the `@tailwindcss/vite` plugin directly:

```ts
// astro.config.mjs
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'astro/config';

export default defineConfig({
  vite: { plugins: [tailwindcss()] },
});
```

```css
/* src/styles/global.css */
@import "tailwindcss";

@theme {
  --color-surface: oklch(0.98 0.01 80);
  --font-heading: "Inter", system-ui, sans-serif;
}
```

```astro
---
import '../styles/global.css';
---
```

See `tailwind-v4.md` for the full setup.

### Tailwind v3 (legacy / existing projects)

```bash
npx astro add tailwind
```

```ts
// astro.config.mjs
import tailwind from '@astrojs/tailwind';
export default defineConfig({ integrations: [tailwind({ applyBaseStyles: false })] });
```

```js
// tailwind.config.mjs
export default {
  content: ['./src/**/*.{astro,html,js,jsx,ts,tsx,vue,svelte,md,mdx}'],
  theme: { extend: { /* ... */ } },
};
```

**Do not mix v3 and v4 syntax.** Pick one per project. v4 dropped `tailwind.config.js`, `@tailwind base/components/utilities`, and most plugin patterns.

## Compatibility Matrix

| Integration | Astro 5 | Notes |
|---|---|---|
| `@astrojs/react` | ✓ | React 18 / 19 |
| `@astrojs/vue` | ✓ | Vue 3.4+ |
| `@astrojs/svelte` | ✓ | Svelte 5 |
| `@astrojs/solid-js` | ✓ | Solid 1.8+ |
| `@astrojs/preact` | ✓ | |
| `@astrojs/mdx` | ✓ | MDX 3 |
| `@astrojs/node` | ✓ | |
| `@astrojs/cloudflare` | ✓ | |
| `@astrojs/vercel` | ✓ | |
| `@astrojs/netlify` | ✓ | |
| `@astrojs/deno` | ✓ | |
| `@astrojs/tailwind` | ⚠ | v3 bridge only — prefer `@tailwindcss/vite` for v4 |
| `@astrojs/partytown` | ✓ | Move third-party scripts off main thread |
| `@astrojs/sitemap` | ✓ | Auto sitemap.xml |
| `@astrojs/rss` | ✓ | RSS feed generation |
| `@astrojs/react` + Next ISR-style | n/a | Astro does not do ISR; use on-demand builder / Actions for cache revalidation |

## Common Gotchas

- **`astro add` modifies `astro.config.*` automatically** — if your config is `.ts` with custom types, the CLI may strip them. Review the diff.
- **Multiple framework integrations can coexist** — React + Vue + Svelte in one project works. Bundle size grows. Prefer one framework unless you have a real reason.
- **`client:only="react"` requires the framework name** — without it, Astro can't know which renderer to use. Always specify: `client:only="react"`, `client:only="vue"`, `client:only="svelte"`, `client:only="solid-js"`.
- **Adapter order matters for some platforms** — Cloudflare requires `output: 'server'` or hybrid. Vercel supports both. Check platform docs.
- **MDX + content collections require `@astrojs/mdx` installed** — `.mdx` files in a collection are skipped silently if the integration isn't there.
- **Tailwind v4 + `@tailwindcss/vite` + Astro**: don't import Tailwind in `astro.config.mjs` `vite.plugins` more than once. Don't also add `@astrojs/tailwind`. Pick one.
- **`@astrojs/check` + `typescript`** are required for `astro check` — install them as devDeps:
  ```bash
  npm i -D @astrojs/check typescript
  ```

## Verification

```bash
astro check        # confirms integration types resolve
astro build        # confirms integrations + adapter compile
npx astro info     # prints versions of installed integrations + adapters
```
