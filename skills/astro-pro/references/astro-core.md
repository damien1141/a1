# Astro 5 Core

Astro 5 islands architecture, server islands, Actions, View Transitions, middleware, endpoints, environment variables, integrations. Referenced from `astro-pro/SKILL.md`. Content collections live in `content-collections.md`; View Transitions deep-dive in `view-transitions.md`; integration install in `integrations.md`.

## `.astro` Component Anatomy

```astro
---
// Frontmatter script — runs on the SERVER at build/request time.
// Imports, props, fetches, compute. No browser APIs here.
import { ArrowRight } from 'lucide-astro';
import Layout from '../layouts/Layout.astro';

interface Props { title: string; href: string; }
const { title, href } = Astro.props;
const data = await fetch('https://api.example.com/...').then(r => r.json());
---
<!-- Template — JSX-like, but `class` not `className`, no JSX runtime. -->
<Layout title={title}>
  <h1 class="font-heading text-4xl">{title}</h1>
  <a href={href} class="group inline-flex items-center gap-2">
    Read <ArrowRight class="w-4 h-4 group-hover:translate-x-1 transition-transform" />
  </a>
</Layout>

<script is:inline>
  // `is:inline` — raw, not processed by Astro. Use for Alpine CDN, global observers.
  // Default (no directive) — processed, bundled, type-checked, hoisted.
</script>

<style>
  /* Scoped to this component by default. Use `is:global` to escape. */
  .local { color: var(--color-text-main); }
</style>
```

Key directives:
- `define:vars={{ count }}` — pass server variables into a `<script>` (the script re-renders with the value).
- `set:html={rawHtmlString}` — inject raw HTML (sanitize first).
- `class:list={['a', cond && 'b', { c: true }]}` — conditional classes.
- `is:inline` — skip processing/bundling (raw script).
- `is:global` — escape style scoping.

## Islands Architecture

The page is static HTML by default. Interactive components are "islands" — framework components (React/Vue/Svelte/Solid) hydrated with a `client:` directive. No directive = server-only, zero JS shipped.

| Directive | When it hydrates | Use |
|---|---|---|
| `client:load` | Immediately on page load | Critical interactive UI above the fold |
| `client:idle` | When browser is idle (requestIdleCallback) | Below-the-fold interactive UI |
| `client:visible` | When scrolled into view (IntersectionObserver) | Comments, widgets far down |
| `client:media="(max-width: 50em)"` | When media query matches | Mobile-only interactions |
| `client:only="react"` | Skips server render — client only | Components that touch browser APIs at render; specify framework |

```astro
---
import ReactCounter from '../components/Counter.jsx';
import VueCart from '../components/Cart.vue';
---
<ReactCounter client:load initial={0} />
<VueCart client:visible />
```

## Server Islands (Astro 5)

Defer a server-rendered component so it streams in after the initial HTML — useful for personalization, slow data, or A/B variants that shouldn't block the page.

```astro
---
// src/components/UserGreeting.astro — server-rendered, deferred
import { getUser } from '../lib/auth';
const user = await getUser(Astro.request);
---
<p>Hello, {user.name}</p>
```

```astro
<!-- src/pages/index.astro -->
<Layout>
  <h1>Static hero, ships instantly</h1>
  <UserGreeting slot="fallback"><p>Hello, guest</p></UserGreeting>
  <!-- Component mounts with the fallback, then streams the real content -->
</Layout>
```

Note: in Astro 5.x, server islands are activated via the `slot="fallback"` pattern or the `server:defer` directive depending on minor version — check `astro --version` and the release notes for your patch.

## Actions (`astro:actions`)

Typed server mutations called from forms or `fetch`. Type-safe input (zod), type-safe return. Replaces ad-hoc `export const POST` endpoints for form-handler use cases.

```ts
// src/actions/index.ts
import { defineAction, z, isActionError } from 'astro:actions';

export const createUser = defineAction({
  input: z.object({
    email: z.string().email(),
    name: z.string().min(1).max(100),
  }),
  handler: async (input, ctx) => {
    // ctx.locals — typed via src/env.d.ts
    // ctx.request, ctx.cookies, ctx.url available
    const user = await ctx.locals.db.user.create({ data: input });
    return { id: user.id, email: user.email };
  },
});

export const deleteUser = defineAction({
  accept: 'json', // default is 'form'; set 'json' for fetch API usage
  input: z.object({ id: z.string() }),
  handler: async ({ id }, ctx) => {
    const ok = await ctx.locals.db.user.delete({ where: { id } });
    if (!ok) return isActionError({ code: 'NOT_FOUND', message: 'User not found' });
    return { ok: true };
  },
});
```

Calling from a form (default `accept: 'form'`):

```astro
---
import { actions } from '../actions';
---
<form method="POST" action={actions.createUser}>
  <input name="email" type="email" required />
  <input name="name" required />
  <button>Create</button>
</form>
```

Calling via `fetch` (requires `accept: 'json'`):

```ts
import { actions } from '../actions';
const result = await actions.deleteUser({ id: '123' });
if (result.error) console.error(result.error.message);
else console.log(result.data);
```

Error codes: `BAD_REQUEST`, `UNAUTHORIZED`, `FORBIDDEN`, `NOT_FOUND`, `CONFLICT`, `PRECONDITION_FAILED`, `INTERNAL_SERVER_ERROR`. Use `isActionError({ code, message })` to return a typed error.

## Middleware

`src/middleware.ts` — runs on every request (in `server`/`hybrid` mode) before page/endpoint rendering.

```ts
// src/middleware.ts
import { defineMiddleware } from 'astro:middleware';

export const onRequest = defineMiddleware((ctx, next) => {
  ctx.locals.user = getUserFromCookie(ctx.cookies);
  return next();
});

// Multiple middleware:
import { sequence } from 'astro:middleware';
const auth = defineMiddleware(/* ... */);
const logging = defineMiddleware(/* ... */);
export const onRequest = sequence(auth, logging);
```

Type `ctx.locals`:

```ts
// src/env.d.ts
declare namespace App {
  interface Locals {
    user: { id: string; name: string } | null;
    db: import('@prisma/client').PrismaClient;
  }
}
```

## Endpoints (raw JSON / webhooks / non-form)

Use `export const GET` / `POST` / etc. in `src/pages/api/*.{ts,js}` for raw JSON responses, webhooks, file downloads, or any case where an Action's typed-form contract doesn't fit.

```ts
// src/pages/api/webhook.ts
export const POST = async ({ request }: APIContext) => {
  const sig = request.headers.get('stripe-signature');
  const body = await request.text();
  const event = stripe.webhooks.constructEvent(body, sig, import.meta.env.STRIPE_WEBHOOK_SECRET);
  if (event.type === 'checkout.session.completed') {
    await fulfillOrder(event.data.object);
  }
  return new Response(null, { status: 200 });
};

export const GET = ({ url }: APIContext) => {
  return Response.json({ time: Date.now(), q: url.searchParams.get('q') });
};
```

`APIContext` provides: `request`, `url`, `params`, `props` (from `getStaticPaths`), `cookies`, `redirect()`, `locals`, `site`, `generator`, `clientAddress`.

## Environment Variables

```ts
// astro.config.mjs — public + private env validation
import { defineConfig } from 'astro/config';

export default defineConfig({
  // import.meta.env.PUBLIC_* is exposed to client code
  // process.env.* is server-only
});
```

```ts
// src/env.d.ts — Astro 5 typed env (astro:env)
/// <reference types="astro/client" />
type ImportMetaEnv = {
  readonly DATABASE_URL: string;
  readonly PUBLIC_API_BASE: string;
};
```

Astro 5 `astro:env` module — typed, validated environment variables:

```ts
// astro.config.mjs
import { defineConfig, envField } from 'astro/config';

export default defineConfig({
  env: {
    schema: {
      DATABASE_URL: envField.string({ context: 'server', access: 'public' }),
      STRIPE_SECRET_KEY: envField.string({ context: 'server', access: 'secret' }),
      PUBLIC_API_BASE: envField.string({ context: 'client', access: 'public' }),
    },
  },
});
```

```ts
// usage
import { DATABASE_URL, STRIPE_SECRET_KEY } from 'astro:env/server';
import { PUBLIC_API_BASE } from 'astro:env/client';
const secret = import.meta.env.STRIPE_SECRET_KEY; // alternative pattern
```

Use `getSecret('NAME')` for runtime secrets (edge runtimes where static `import.meta.env` is not available).

## Build Modes

| `output` | Behavior |
|---|---|
| `static` (default) | All pages pre-rendered to HTML at build time. Endpoints return JSON only if explicitly `export const GET`. |
| `server` | All pages server-rendered on demand unless opted out with `export const prerender = true`. |
| `hybrid` | (Astro 4 name; in Astro 5, `output: 'static'` + `export const prerender = false` per-route achieves the same — hybrid was merged into static-with-opt-out.) |

```ts
// src/pages/dashboard.astro — opt OUT of prerender (SSR only)
export const prerender = false;
```

## Integration Compatibility Matrix (quick reference)

| Integration | Astro 5 | Notes |
|---|---|---|
| `@astrojs/react` | ✓ | React 18/19 |
| `@astrojs/vue` | ✓ | Vue 3.4+ |
| `@astrojs/svelte` | ✓ | Svelte 5 |
| `@astrojs/solid-js` | ✓ | Solid 1.8+ |
| `@astrojs/preact` | ✓ | |
| `@astrojs/mdx` | ✓ | MDX 3 |
| `@astrojs/node` | ✓ | Node SSR adapter |
| `@astrojs/cloudflare` | ✓ | Cloudflare Pages/Workers |
| `@astrojs/vercel` | ✓ | Vercel SSR |
| `@astrojs/netlify` | ✓ | Netlify Functions |
| `@astrojs/deno` | ✓ | Deno Deploy |
| `@astrojs/tailwind` | ⚠ | v3 bridge only. For Tailwind v4 use `@tailwindcss/vite` directly — no Astro integration needed. |

See `integrations.md` for install commands and gotchas.

## Verification Gate

```bash
astro check        # diagnostics: TypeScript + .astro template + integration types
astro build        # production build — must pass clean
tsc --noEmit       # if using strict tsconfig (extends astro/tsconfigs/strict)
```
