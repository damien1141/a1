# Hono 4 — Edge Runtime, Middleware, RPC, Streaming

Hono is a small, fast TypeScript web framework that runs on Node 20+, Bun, Deno, and Cloudflare Workers. It is the default for edge/serverless backends where cold start and bundle size matter.

## Setup

```bash
bun create hono@latest my-app        # Bun
npx create-hono@latest my-app        # Node
```

Pick a starter template: `cloudflare-workers`, `bun`, `node-server`, `deno`, `x-basic`.

### Node adapter

```typescript
// src/index.ts
import { Hono } from 'hono';
import { serve } from '@hono/node-server';

const app = new Hono();
app.get('/', (c) => c.json({ hello: 'world' }));

serve({ fetch: app.fetch, port: 3000 });
```

### Cloudflare Workers / Bun / Deno

```typescript
// src/index.ts
const app = new Hono();
app.get('/', (c) => c.json({ hello: 'world' }));
export default app;                   // runtime entrypoint — no serve() call
```

## App + Env types

```typescript
type Env = {
  Bindings: {                                          // Cloudflare bindings
    DB: D1Database;
    KV: KVNamespace;
    R2: R2Bucket;
    AI: Ai;
  };
  Variables: {                                         // request-scoped via c.set()
    user: User;
    requestId: string;
  };
};

const app = new Hono<Env>();
```

`Bindings` is the runtime environment (Workers KV/D1/R2/AI). `Variables` is per-request state. Use `c.env.X` for bindings, `c.get('user')` for variables.

## Routing

```typescript
app.get('/users/:id', (c) => {
  const id = c.req.param('id');                        // string
  return c.json({ id });
});

app.get('/posts/:id/comments/:cid', (c) => {
  const { id, cid } = c.req.param();                   // all params at once
  return c.json({ id, cid });
});

app.get('/file/:path{.*}', (c) => {                    // wildcard regex
  const path = c.req.param('path');
  return c.text(`serve ${path}`);
});

// Chained routes — REQUIRED for RPC type inference
const app = new Hono()
  .get('/users', (c) => c.json({ users: [] }))
  .post('/users', zValidator('json', userSchema), (c) => c.json({ ok: true }, 201))
  .get('/users/:id', (c) => c.json({ id: c.req.param('id') }));
```

**Critical:** chain routes with `.get().post().get()` — non-chained routes break the RPC type inference that powers `hc<AppType>()`.

## Route grouping

```typescript
const api = new Hono();
api.get('/users', (c) => c.json([]));

const app = new Hono().route('/api', api);              // mounts at /api/users
// or
const app = new Hono().basePath('/api');                // all routes prefixed
```

## Request access — `c.req`

```typescript
c.req.param('id')                  // path param
c.req.param()                      // all path params as object
c.req.query('page')                // query string param
c.req.queries('tags')              // multiple values: ?tags=A&tags=B → ['A','B']
c.req.header('authorization')      // request header
c.req.valid('json')                // validated body (after zValidator)
c.req.valid('query')
c.req.valid('param')
await c.req.json()                 // parse JSON body
await c.req.text()                 // parse text body
await c.req.formData()             // parse FormData
await c.req.parseBody()            // multipart or urlencoded
c.req.url                          // full URL
c.req.path                         // pathname
c.req.method                       // HTTP method
c.req.raw                          // underlying Request object
```

## Response — `c`

```typescript
c.json({ ok: true })                              // 200 application/json
c.json({ ok: true }, 201)                         // 201
c.json({ ok: true }, { headers: { 'X-Custom': 'x' } })
c.text('hello')                                   // text/plain
c.html('<h1>Hi</h1>')                             // text/html
c.redirect('/new')                                // 302
c.redirect('/new', 301)                           // 301
c.body('raw')                                     // raw body
c.header('Cache-Control', 'no-store')
c.status(204)
c.notFound()                                      // 404
```

## Middleware

Built-in:

```typescript
import { cors } from 'hono/cors';
import { logger } from 'hono/logger';
import { secureHeaders } from 'hono/secure-headers';
import { bearerAuth } from 'hono/bearer-auth';
import { jwt } from 'hono/jwt';
import { prettyJSON } from 'hono/pretty-json';
import { etag } from 'hono/etag';
import { compress } from 'hono/compress';
import { requestId } from 'hono/request-id';
import { bodyLimit } from 'hono/body-limit';
import { csrf } from 'hono/csrf';

app.use('*', logger(), secureHeaders(), requestId());
app.use('/api/*', cors({ origin: 'https://app.example.com', credentials: true }));
app.use('/api/*', bodyLimit({ maxSize: 1_000_000 }));      // 1MB
```

Custom middleware:

```typescript
import { createMiddleware } from 'hono/factory';

const auth = createMiddleware<Env>(async (c, next) => {
  const token = c.req.header('Authorization')?.replace('Bearer ', '');
  if (!token) return c.json({ error: 'unauthorized' }, 401);
  try {
    const user = await verifyJwt(token);
    c.set('user', user);
    await next();
  } catch {
    return c.json({ error: 'invalid token' }, 401);
  }
});

app.use('/api/*', auth);
```

Execution order: middleware runs in registration order; `await next()` calls the next handler; code after `next()` runs on the way back.

## Validation with Zod

```typescript
import { z } from 'zod';
import { zValidator } from '@hono/zod-validator';

const createUser = z.object({
  email: z.string().email(),
  password: z.string().min(8),
  name: z.string().optional(),
});

app.post('/users', zValidator('json', createUser), (c) => {
  const body = c.req.valid('json');                // typed: { email, password, name? }
  return c.json({ id: crypto.randomUUID(), ...body }, 201);
});

// Multiple validators chained
app.get('/users/:id',
  zValidator('param', z.object({ id: z.string().uuid() })),
  zValidator('query', z.object({ include: z.string().optional() })),
  (c) => {
    const { id } = c.req.valid('param');
    const { include } = c.req.valid('query');
    return c.json({ id, include });
  },
);
```

Validation targets: `json`, `form`, `query`, `header`, `param`, `cookie`. On failure, Hono returns 400 with `{ success: false, error: ... }` — override with a custom error handler:

```typescript
app.post('/users',
  zValidator('json', createUser, (result, c) => {
    if (!result.success) {
      return c.json({ type: 'https://api.example.com/errors/validation',
                      title: 'Validation Error', status: 422,
                      errors: result.error.issues.map((i) => ({ field: i.path.join('.'), message: i.message })) }, 422);
    }
  }),
  handler);
```

## OpenAPI from Zod — `@hono/zod-openapi`

```typescript
import { createRoute, OpenAPIHono } from '@hono/zod-openapi';
import { z } from '@hono/zod-openapi/types';

const app = new OpenAPIHono();

const createUserRoute = createRoute({
  method: 'post',
  path: '/users',
  request: { body: { content: { 'application/json': { schema: createUser } } } },
  responses: {
    201: { content: { 'application/json': { schema: UserSchema } }, description: 'Created' },
    422: { content: { 'application/json': { schema: ProblemSchema } }, description: 'Validation failed' },
  },
});

app.openapi(createUserRoute, (c) => { ... });

// /openapi.json — full OpenAPI 3.1 spec generated
app.doc('/openapi.json', { openapi: '3.1.0', info: { title: 'Example API', version: '1.0.0' } });
```

## RPC — type-safe client

```typescript
// server.ts
const app = new Hono().get('/users', (c) => c.json({ users: [] }))
                      .post('/users', zValidator('json', createUser), (c) => c.json({ id: '1' }, 201));
export type AppType = typeof app;

// client.ts (any TS app)
import { hc } from 'hono/client';
import type { AppType } from './server';

const client = hc<AppType>('http://localhost:8787/');
const res = await client.users.$post({ json: { email: 'a@b.com', password: 'password123' } });
const data = await res.json();                       // fully typed: { id: string }

// Type extraction
import type { InferRequestType, InferResponseType } from 'hono/client';
type Req = InferRequestType<typeof client.users.$post>;
type Res = InferResponseType<typeof client.users.$post, 200>;
```

Rules:
- Routes MUST be chained for inference to work.
- Client and server must share types — typically via a shared package or monorepo workspace.

## Streaming

```typescript
import { stream, streamText, streamSSE } from 'hono/streaming';

// Raw stream
app.get('/raw', (c) => stream(c, async (s) => {
  await s.write(new Uint8Array([0x48, 0x49]));
  await s.pipe(someReadableStream);
}));

// Text stream — useful for LLM token streaming
app.get('/llm', (c) => streamText(c, async (s) => {
  for (const token of llmTokens()) { await s.write(token); await s.sleep(10); }
}));

// SSE — server-sent events
app.get('/events', (c) => streamSSE(c, async (s) => {
  let id = 0;
  while (true) {
    await s.writeSSE({ data: JSON.stringify({ ts: Date.now() }), event: 'tick', id: String(id++) });
    await s.sleep(1000);
  }
}));
```

## Testing — `app.request()`

```typescript
import { describe, it, expect } from 'vitest';
import app from './index';

describe('users', () => {
  it('POST /users validates body', async () => {
    const res = await app.request('/users', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email: 'not-an-email', password: 'short' }),
    });
    expect(res.status).toBe(422);
  });

  it('GET /users/:id returns id', async () => {
    const res = await app.request('/users/123');
    expect(res.status).toBe(200);
    expect(await res.json()).toEqual({ id: '123' });
  });

  // With mock Workers bindings
  it('GET /data uses KV', async () => {
    const mockKV = { get: async () => 'cached' };
    const res = await app.request('/data', {}, { KV: mockKV });
    expect(await res.json()).toEqual({ value: 'cached' });
  });
});
```

`app.request()` does not start an HTTP server — it invokes the app directly. Mock `Bindings` via the third arg.

## Error handling

```typescript
app.notFound((c) => c.json({
  type: 'https://api.example.com/errors/not-found',
  title: 'Not Found', status: 404,
}, 404));

app.onError((err, c) => {
  console.error(err);
  if (err instanceof HTTPException) {
    return c.json({ type: 'https://api.example.com/errors/' + err.status, title: err.message, status: err.status }, err.status);
  }
  return c.json({ type: 'https://api.example.com/errors/internal', title: 'Internal Server Error', status: 500 }, 500);
});
```

## Factory pattern

```typescript
import { createFactory } from 'hono/factory';

const factory = createFactory<Env>();

const middleware = factory.createMiddleware(async (c, next) => {
  c.set('requestId', crypto.randomUUID());
  await next();
});

const handlers = factory.createHandlers(middleware, (c) => c.json({ ok: true }));
app.get('/api', ...handlers);
```

Use `createFactory` to share `Env` across the app without passing generics everywhere.

## Verification gates

- `tsc --noEmit` — type errors block.
- `npx hono request src/index.ts -P /users` — manual round-trip without starting a server.
- `vitest run` — integration tests via `app.request()`.
- `wrangler deploy --dry-run` (Workers) — bundle builds; no native module leakage.
- `wrangler tail` (Workers) — live logs in dev.
- `/openapi.json` (if using `@hono/zod-openapi`) — spec generates and validates.
