# Content Collections (Astro 5 Content Layer API)

Astro 5 replaces the legacy `src/content/config.ts` + folder-convention model with the **Content Layer API**: collections are defined in `src/content.config.ts`, loaders are explicit (`glob` / `file` / `fetch`), and entries are identified by a stable `id` instead of a slug derived from the file path.

## Defining a Collection

```ts
// src/content.config.ts
import { defineCollection, z } from 'astro:content';
import { glob } from 'astro/loaders';

const posts = defineCollection({
  // glob loader — read files from disk
  loader: glob({ pattern: '**/*.{md,mdx}', base: './src/content/posts' }),
  schema: z.object({
    title: z.string(),
    pubDate: z.coerce.date(),
    updatedDate: z.coerce.date().optional(),
    hero: z.string().optional(),
    tags: z.array(z.string()).default([]),
    draft: z.boolean().default(false),
    author: z.string().reference(() => authors),
  }),
});

// file loader — single JSON/YAML/data file as a collection
import { file } from 'astro/loaders';
const authors = defineCollection({
  loader: file('src/data/authors.json'),
  schema: z.object({
    name: z.string(),
    bio: z.string(),
    avatar: z.string().optional(),
  }),
});

// fetch loader — remote API as a collection
import { fetch } from 'astro/loaders';
const products = defineCollection({
  loader: fetch('https://api.shop.com/products.json'),
  schema: z.object({
    id: z.string(),
    name: z.string(),
    price: z.number(),
  }),
});

export const collections = { posts, authors, products };
```

Key changes from Astro 4:
- File is `src/content.config.ts` (not `src/content/config.ts`).
- `loader` is required — `glob()`, `file()`, or `fetch()`.
- Collection entries use `entry.id` (the loader-provided stable ID) — not `entry.slug`.
- `render(entry)` is imported from `astro:content`, not destructured from `entry`.
- No more `type: 'content' | 'data'` — the loader determines type.
- Reference: `import { reference } from 'astro:content'` is no longer needed; pass the collection variable directly.

## Zod Schema Patterns

```ts
z.object({
  // Required
  title: z.string(),
  // Coerced (frontmatter strings → real types)
  pubDate: z.coerce.date(),
  readingTime: z.coerce.number().optional(),
  // Enums
  status: z.enum(['draft', 'published', 'archived']).default('draft'),
  // Arrays with defaults
  tags: z.array(z.string()).default([]),
  // Nested objects
  hero: z.object({
    src: z.string(),
    alt: z.string(),
    credit: z.string().optional(),
  }).optional(),
  // Unions
  layout: z.union([z.literal('default'), z.literal('wide')]).default('default'),
  // Custom validation
  slug: z.string().regex(/^[a-z0-9-]+$/).optional(),
})
  .strict() // reject unknown keys
  .refine((data) => data.updatedDate ? data.updatedDate >= data.pubDate : true, {
    message: 'updatedDate must be after pubDate',
    path: ['updatedDate'],
  });
```

## Querying

```ts
import { getCollection, getEntry, getEntryBySlug, render } from 'astro:content';

// All entries (raw — not sorted)
const allPosts = await getCollection('posts');

// Filter
const published = (await getCollection('posts'))
  .filter((p) => p.data.status === 'published' && !p.data.draft)
  .sort((a, b) => b.data.pubDate.getTime() - a.data.pubDate.getTime());

// Single entry by ID
const post = await getEntry('posts', 'my-first-post');

// By slug (legacy) — only if you used entry.id derived from slug
// Astro 5: prefer getEntry('collection', 'id') directly.

// Render — get the Content component + headings + remarkPluginFrontmatter
const { Content, headings, remarkPluginFrontmatter } = await render(post);
```

## Rendering in a Page

```astro
---
// src/pages/blog/[...slug].astro
import { getCollection, render } from 'astro:content';
export async function getStaticPaths() {
  const posts = (await getCollection('posts')).filter((p) => !p.data.draft);
  return posts.map((p) => ({ params: { slug: p.id }, props: { post: p } }));
}
const { post } = Astro.props;
const { Content } = await render(post);
---
<article>
  <h1>{post.data.title}</h1>
  <time datetime={post.data.pubDate.toISOString()}>{post.data.pubDate.toLocaleDateString()}</time>
  <Content />
</article>
```

## Reference (foreign-key) between collections

```ts
// posts reference authors
const posts = defineCollection({
  loader: glob({ pattern: '**/*.md', base: './src/content/posts' }),
  schema: z.object({
    title: z.string(),
    author: z.string().reference(() => authors), // typed link
  }),
});

const authors = defineCollection({
  loader: file('src/data/authors.json'),
  schema: z.object({ name: z.string(), bio: z.string() }),
});

// usage
const post = await getEntry('posts', 'hello');
const author = await getEntry(post.data.author); // typed lookup
```

## MDX

Install `@astrojs/mdx`, then:

```ts
// astro.config.mjs
import mdx from '@astrojs/mdx';
export default defineConfig({ integrations: [mdx()] });
```

```ts
// collection accepts .mdx
loader: glob({ pattern: '**/*.{md,mdx}', base: './src/content/posts' })
```

MDX files can import components directly:

```mdx
---
title: Hello
---
import Chart from '../../components/Chart.jsx';

Here is a chart:

<Chart data={[1,2,3]} />
```

## Live collections (Astro 5.5+)

`fetch()` loader supports `download` and `disabled` options for content that should refresh:

```ts
const events = defineCollection({
  loader: fetch('https://api.example.com/events.json', {
    parser: (text) => JSON.parse(text).events,
  }),
  schema: z.object({ id: z.string(), name: z.string(), date: z.coerce.date() }),
});
```

In dev: re-fetched on each request. In build: fetched once at build time.

## Common Gotchas

- **`entry.id` not `entry.slug`** — Astro 5 dropped slug. For glob loaders, `id` is the file path relative to `base`, without extension. For `file()` loaders, `id` is the object key. For `fetch()` loaders, you must provide a `parser` that returns objects with a unique `id` field.
- **`render(entry)` is async** — always `await` it. Returns `{ Content, headings, remarkPluginFrontmatter }`.
- **Schema is enforced at build time** — invalid frontmatter fails `astro build` with a clear error pointing to the offending file. Frontmatter is **not** editable at runtime.
- **Collection values are editable; schema fields are not** — when de-slopifying, you can edit a post's `title` value but cannot rename the `title` field without migrating all entries.
- **Order is not guaranteed** — `getCollection` returns in loader order (filesystem order for glob). Always sort explicitly.
- **No `_globals` folder** — Astro 5 removed the convention of underscore-prefixed "draft" files. Use the `draft: z.boolean().default(false)` schema field instead.

## Verification

```bash
astro check        # catches schema type mismatches in .astro files
astro build        # enforces frontmatter schemas at build time
```

Build-time error example:
```
Error: posts/hello.md frontmatter does not match collection schema.
  Expected: string
  Received: number
  Path: pubDate
```
