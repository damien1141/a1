# Migration + Runtime — Express→NestJS, Node→Bun, Performance

Migrating from Express to NestJS, choosing between Node 20+ and Bun, and performance tuning patterns. Includes when to stay, when to migrate, and how to do it incrementally.

## Express → NestJS migration

NestJS can run on top of Express (default) or Fastify. Existing Express middleware works unchanged; NestJS wraps it.

### Migration strategy — incremental

1. **Mount NestJS inside Express** as a sub-app on a path prefix. Routes migrate one at a time; the rest of the Express app keeps running.

```typescript
// legacy Express app
import express from 'express';
import { NestFactory } from '@nestjs/core';
import { AppModule } from './app.module';

const expressApp = express();
// ... existing middleware: cors, body parser, helmet, sessions ...

// Mount NestJS on /api/v2 — new code goes here
const nestApp = await NestFactory.create(AppModule, expressApp, { bodyParser: false });
await nestApp.init();
expressApp.use('/api/v2', nestApp.getHttpAdapter().getInstance());

// Existing Express routes keep running at root
expressApp.get('/api/v1/users', (req, res) => { /* ... */ });

expressApp.listen(3000);
```

2. **Migrate one route at a time.** Move `/api/v1/users` to `/api/v2/users` (NestJS), update clients, remove the Express handler. Don't rewrite everything at once.
3. **Run both versions in parallel.** Use the `Sunset` header on the v1 endpoint; emit `Deprecation: true`.
4. **Move shared middleware into NestJS** (`AppModule` global middleware) once enough routes have migrated.
5. **Cut over** when 100% of traffic is on v2; remove the Express app entirely.

### Anti-patterns to avoid

- **Big-bang rewrite.** Two months of dual maintenance is cheaper than a stalled migration.
- **Migrating without tests.** Characterize the existing Express behavior (snapshot tests) before moving anything.
- **Migrating with subtle behavior changes.** Same response shape, same status codes, same error messages — drift breaks clients silently.
- **Skipping the deprecation window.** Clients need time to move; give them ≥ 2 minor versions or 6 months.

### What changes when you migrate

| Concern | Express | NestJS |
|---|---|---|
| DI | manual (`require`) | constructor injection via `@Injectable` |
| Validation | joi/express-validator middleware | `class-validator` DTOs + `ValidationPipe` |
| Error handling | central `(err, req, res, next)` | exception filters + typed exceptions |
| Routing | `app.get('/users/:id', handler)` | `@Controller('users') @Get(':id')` |
| Swagger | `swagger-jsdoc` + `swagger-ui-express` | `@nestjs/swagger` decorators (auto-generated) |
| Testing | `supertest(app)` | `supertest(app.getHttpServer())` + `Test.createTestingModule` |
| Middleware | `app.use(mw)` | `@UseInterceptors`, `@UseGuards`, `app.useGlobalPipes` |

## Node 20+ vs Bun

| Criterion | Node 20+ LTS | Bun |
|---|---|---|
| Maturity | Production battle-tested, 30+ years of ecosystem | 1.0 in 2023; production-ready for many workloads |
| Startup time | ~80ms cold start | ~10ms cold start |
| TS support | Via `tsc` or `tsx` loader | Native — runs `.ts` directly |
| Bundler | `esbuild`, `webpack` | Built-in `bun build` |
| Test runner | Jest / Vitest | Built-in `bun test` (Jest-compatible API) |
| Package manager | npm / pnpm / yarn | `bun install` (10× faster) |
| Native modules | Full support | Most work; some gaps (sharp, node-canvas sometimes) |
| WebSocket | `ws`, `socket.io` | Built-in `Bun.WebSocket` |
| SQLite | `better-sqlite3` | Built-in `bun:sqlite` |
| HTTP server | `http` / `express` / `hono` | Built-in `Bun.serve` (3-5× faster) |
| Workers | `worker_threads` | `Bun.Worker` |
| Hot reload | `nodemon`, `tsx watch` | Built-in `bun --hot` |

### When to pick Bun

- You're starting fresh and don't depend on native modules.
- Cold start matters (serverless, CLI tools).
- You want native TS without a build step.
- You want one toolchain (runtime + package manager + test runner + bundler).

### When to stay on Node 20+ LTS

- Production deployment with strict LTS requirements.
- Heavy native module usage (sharp, node-canvas, node-pty, better-sqlite3 with native bindings).
- Long-tail ecosystem dependencies that haven't been Bun-tested.
- You need `cluster` module, `worker_threads` semantics that Bun doesn't fully replicate.

### Bun gotchas

- **Native modules** — `sharp` works via `npm:sharp`; some prebuilt binaries may not load. Test before committing.
- **`process` global** — Bun polyfills it, but some libs check `process.version` and fail.
- **`fs.promises.readFile`** — works, but `Bun.file()` is faster and Bun-native.
- **Jest compatibility** — `bun test` supports most Jest APIs, but advanced features (`jest.mock` factory scope, `jest.useFakeTimers` with modern timers) have edge cases.
- **Workers** — `Bun.Worker` API differs from `worker_threads`; not a drop-in replacement.

### Bun + Hono starter

```bash
bun create hono@latest my-app
# pick "bun" template
cd my-app
bun install
bun run dev                  # bun --hot src/index.ts
bun run test                 # bun test
```

```typescript
// src/index.ts
import { Hono } from 'hono';
import { logger } from 'hono/logger';

const app = new Hono();
app.use('*', logger());
app.get('/', (c) => c.json({ hello: 'world' }));

export default { port: 3000, fetch: app.fetch };
```

## Performance tuning

### Node 20+ performance

```bash
# Multi-core via cluster (or use PM2 / K8s)
NODE_OPTIONS="--max-old-space-size=4096" node dist/main.js

# Stream large responses instead of buffering
# Use Readable streams + pipe — never load entire file into memory
```

```typescript
// Stream a file from disk → HTTP response
import { createReadStream } from 'fs';
app.get('/download/:id', (req, res) => {
  const stream = createReadStream(filePath);
  stream.on('open', () => stream.pipe(res));
  stream.on('error', () => res.status(500).end());
});
```

- **Cluster mode** — one process per core. Use `cluster` module or PM2.
- **`--max-old-space-size`** — raise the V8 heap for memory-heavy workloads (default ~2GB).
- **Connection pooling** — DB pool size = 2 × cores for OLTP.
- **Streaming** — pipe large responses; never buffer.
- **Async throughout** — never block the event loop with sync I/O or CPU work. Offload to worker threads.

### Bun performance

```typescript
// Bun.serve is 3-5× faster than node:http
export default {
  port: 3000,
  fetch(req) {
    return new Response('hello');
  },
} satisfies Bun.ServeOptions;

// Bun.file is faster than fs.readFile for static files
const file = Bun.file('./large.json');
return new Response(file);

// Bun:sqlite — synchronous, in-process, very fast
import { Database } from 'bun:sqlite';
const db = new Database('app.db');
const users = db.query('SELECT * FROM users WHERE id = ?').all(id);
```

### Common perf anti-patterns

- **`JSON.parse(JSON.stringify(x))` for deep clone** — use `structuredClone(x)`.
- **Synchronous I/O in handlers** — `fs.readFileSync`, `crypto.pbkdf2Sync` (use `*Sync` only at startup).
- **`await` in a loop** — `Promise.all` instead.
- **Building large objects per request** — pool/cache them.
- **Logging without level gating** — `console.log` calls `JSON.stringify` even when level filtered. Use pino with `level: 'warn'`.
- **`Object.keys` / `Object.entries` in hot loops** — cache or use `Map`.
- **Regex in hot loops** — pre-compile (V8 caches, but explicit is clearer).
- **`Date.now()` for elapsed time** — use `performance.now()` (sub-ms precision).

### Benchmarks (sanity check)

| Endpoint shape | Target p99 latency (Node 20+) | Target p99 latency (Bun) |
|---|---|---|
| DB read + JSON response | < 20ms | < 10ms |
| DB write + JSON response | < 50ms | < 30ms |
| Static file via stream | < 5ms | < 2ms |
| Pure CPU work (1ms) | < 5ms | < 3ms |

Anything 10× worse = bug (missing index, N+1 query, sync I/O, missing cache).

## Worker threads (Node) / Bun.Worker

```typescript
// Node — offload CPU work
import { Worker } from 'worker_threads';
const worker = new Worker('./hash-worker.js', { workerData: { input: largeBuffer } });
worker.on('message', (hash) => { /* ... */ });

// Bun — equivalent
const worker = new Worker(new URL('./hash-worker.ts', import.meta.url));
worker.postMessage({ input: largeBuffer });
worker.onmessage = (e) => { /* e.data.hash */ };
```

Use for: image processing, crypto, JSON parsing of huge payloads, compression. Don't use for I/O — async I/O is already parallel.

## Container & deployment

### Dockerfile (Node 20+)

```dockerfile
FROM node:20-alpine AS build
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build && npm prune --production

FROM node:20-alpine
WORKDIR /app
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
USER node
EXPOSE 3000
CMD ["node", "dist/main.js"]
```

### Dockerfile (Bun)

```dockerfile
FROM oven/bun:1-alpine AS build
WORKDIR /app
COPY package.json bun.lockb ./
RUN bun install --frozen-lockfile --production
COPY . .
RUN bun build src/index.ts --target=bun --outdir=dist

FROM oven/bun:1-alpine
WORKDIR /app
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/dist ./dist
USER bun
EXPOSE 3000
CMD ["bun", "dist/index.js"]
```

Multi-stage build → small final image. Always run as non-root user (`USER node` / `USER bun`).

## Verification gates

- `tsc --noEmit` — type errors block.
- `npm test` — unit + e2e pass.
- `npm run build` — production build succeeds.
- `docker build` — image builds < 200MB.
- Load test (`k6` or `autocannon`): p99 latency meets target; no memory leaks across 100K requests; CPU usage plateaus (not runaway).
- Migration sanity: legacy endpoint behavior matches new endpoint response shape, status codes, error messages.
