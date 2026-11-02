---
name: node-backend
description: TypeScript backend on Node 20+ or Bun. Use when building NestJS 10+ enterprise APIs (modules, DI, guards, pipes, interceptors, class-validator DTOs, Swagger) or Hono 4 edge apps (middleware, RPC, streaming, Cloudflare Workers). Includes when-to-use-each decision table, validation, auth, testing, and runtime choice (Bun vs Node).
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: backend
  triggers: NestJS, Hono, Node.js backend, TypeScript backend, dependency injection, controller, service, module, guard, interceptor, pipe, DTO, class-validator, Bun, edge runtime, Cloudflare Workers, RPC, Zod
  role: specialist
  scope: implementation
  output-format: code
  related-skills: api-design, typescript-pro, database-pro, testing-master
---

# Node Backend

Builds TypeScript backends on NestJS 10+ (enterprise, DI, modules) and Hono 4 (edge, RPC, streaming). Picks the framework from a decision table, then ships typed controllers/handlers with validation, auth, and tests. Runs on Node 20+ or Bun.

## When to Use

New REST/GraphQL API on Node — pick NestJS or Hono via decision table; enterprise backend (modules, DI, guards, pipes, interceptors, Swagger); edge/serverless (Hono on Workers/Bun/Deno); Express→NestJS migration; runtime choice (Node 20+ vs Bun).

## Operating Loop

1. **Pick framework + runtime** — apply the Decision Table below.
2. **Scaffold** — `nest new app` (NestJS) or `bun create hono app` (Hono). Enable strict TS: `"strict": true`, `tsc --noEmit` in CI.
3. **Design modules/routes** — NestJS: one module per bounded context (controller + service + DTO + entity). Hono: one router per feature, chained for RPC type inference.
4. **Validate input** — NestJS: `class-validator` DTOs + global `ValidationPipe`. Hono: `zValidator` with Zod.
5. **Auth** — guards/middleware (JWT/Passport), enforced before handlers.
6. **Test** — NestJS: Jest unit + Supertest e2e. Hono: `app.request()` integration tests.
7. **Verify gates** — `tsc --noEmit`, `eslint`, `npm test`, `npm run build`. For Hono: `npx hono request` round-trips endpoints.

## Framework Decision Table

| Need | Pick | Why | Reject |
|---|---|---|---|
| Enterprise backend, DI, modules, multi-team | **NestJS 10+** | opinionated structure, DI, Swagger, guards/interceptors | Hono (too thin for big teams) |
| Edge/serverless (Cloudflare Workers, Bun, Deno) | **Hono 4** | tiny cold start, multi-runtime, RPC types | NestJS (heavy, serverless-hostile) |
| Hot path, max throughput, minimal abstraction | **Hono** | fastest Node WS framework; uWebSockets.js adapter | Express (slow), NestJS (overhead) |
| Existing Express app, want DI without rewrite | **NestJS** (Express adapter) | gradual migration path | Hono (different API surface) |
| Full-stack monolith with JSX/SSR | **Hono** (JSX middleware) | first-class JSX, streaming, layouts | NestJS (no native JSX) |
| GraphQL primary | **NestJS** (`@nestjs/graphql`) | code-first SDL, federation | Hono (use Helix/Envelop if needed) |
| Microservices + queues + observability built-in | **NestJS** | `@nestjs/microservices`, OpenTelemetry | Hono (DIY everything) |

**Default rule:** New enterprise API → NestJS. New edge/serverless → Hono. Both can coexist in a monorepo.

## Runtime Decision Table

| Need | Pick |
|---|---|
| LTS, max ecosystem compat, production battle-tested | **Node 20+ LTS** |
| Startup speed, raw throughput, TS-first, single binary | **Bun** |
| Edge (Cloudflare Workers, Deno Deploy) | **Hono on Workers/Deno** (Node compat varies) |
| Library author targeting all runtimes | **Node 20+ + isomorphic code** |

Bun is production-ready for many workloads but verify native modules (`sharp`, `node-canvas`) work before committing.

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| NestJS 10+ (modules, DI, controllers, services, Swagger) | `references/nestjs-core.md` | Building NestJS modules, DI, controllers, Swagger docs |
| NestJS guards, pipes, interceptors, DTOs (class-validator) | `references/nestjs-pipeline.md` | Auth, validation, transform, error handling |
| Hono 4 (routing, middleware, RPC, streaming, JSX) | `references/hono-edge.md` | Edge apps, RPC types, SSE, Workers bindings |
| Auth (JWT/Passport), testing, observability | `references/auth-testing.md` | JWT, Passport, Jest, Supertest, OpenTelemetry |
| Express migration + runtime choice (Node vs Bun) | `references/migration-runtime.md` | Express→NestJS, Node→Bun, perf tradeoffs |

## Constraints

### MUST DO
- `strict: true` in `tsconfig.json`; `tsc --noEmit` in CI — type errors block merge.
- Validate every input via DTO (NestJS `class-validator`) or Zod schema (Hono `zValidator`). Never trust `req.body`.
- Inject dependencies via constructor (NestJS) or `c.set`/`c.get` (Hono) — never `new Service()` in handlers.
- Throw typed HTTP exceptions (NestJS) or use `c.json({...}, 422)` (Hono) — never bare `throw new Error`.
- Load config from env via `ConfigModule` (NestJS) or `c.env` (Hono on Workers) — never hardcode.
- Write unit tests for services (Jest) and integration tests for routes (`app.request()` for Hono, Supertest for NestJS).
- Document REST endpoints with Swagger decorators (NestJS) or Zod-to-OpenAPI (Hono `@hono/zod-openapi`).
- Log with structured logger (pino) + correlation ID; propagate across service calls.

### MUST NOT DO
- Use `any` type — use `unknown` + narrowing or proper DTOs.
- Pass raw `req.body` to services — always validate first.
- Hardcode hostnames, ports, secrets in source.
- Create circular module dependencies in NestJS (use `forwardRef()` only as last resort).
- Mix sync and async in handlers — `async` all the way down.
- Skip error handling for "happy path only" — every external call needs a try/catch or filter.
- Run Hono with `app.request()` in tests using bindings you didn't mock — Workers KV/D1 won't exist.
- Chain Hono routes with `app.get(...).get(...)` if you need RPC types — non-chained routes lose inference.

## Code Examples

### NestJS — Controller + DTO + Service with DI

```typescript
// dto/create-user.dto.ts
import { IsEmail, IsString, MinLength } from 'class-validator';
import { ApiProperty } from '@nestjs/swagger';

export class CreateUserDto {
  @ApiProperty({ example: 'a@b.com' }) @IsEmail() email: string;
  @ApiProperty({ minLength: 8 }) @IsString() @MinLength(8) password: string;
}

// users.controller.ts
@ApiTags('users') @Controller('users')
export class UsersController {
  constructor(private readonly users: UsersService) {}

  @Post() @HttpCode(HttpStatus.CREATED)
  @ApiCreatedResponse({ description: 'Created' })
  create(@Body() dto: CreateUserDto) { return this.users.create(dto); }
}

// users.service.ts
@Injectable()
export class UsersService {
  constructor(@InjectRepository(User) private readonly repo: Repository<User>) {}

  async create(dto: CreateUserDto): Promise<User> {
    if (await this.repo.findOneBy({ email: dto.email }))
      throw new ConflictException('Email already registered');
    return this.repo.save(this.repo.create(dto));
  }
}

// users.module.ts
@Module({
  imports: [TypeOrmModule.forFeature([User])],
  controllers: [UsersController],
  providers: [UsersService],
  exports: [UsersService],
})
export class UsersModule {}
```

### Hono — Chained routes with Zod validation + RPC types

```typescript
import { Hono } from 'hono';
import { zValidator } from '@hono/zod-validator';
import { z } from 'zod';

const createUser = z.object({ email: z.string().email(), password: z.string().min(8) });

const app = new Hono()
  .post('/users', zValidator('json', createUser), (c) => {
    const body = c.req.valid('json');          // fully typed
    return c.json({ id: crypto.randomUUID(), email: body.email }, 201);
  })
  .get('/users/:id', (c) => {
    const id = c.req.param('id');              // typed as string
    return c.json({ id });
  });

export type AppType = typeof app;              // RPC: client infers both routes

// Client (any TS app)
import { hc } from 'hono/client';
import type { AppType } from './server';
const client = hc<AppType>('http://localhost:8787/');
const res = await client.users[':id'].$get({ param: { id: '123' } });   // typed
```

### Hono — SSE streaming + Cloudflare Workers bindings

```typescript
import { Hono } from 'hono';
import { streamSSE } from 'hono/streaming';

type Env = { Bindings: { KV: KVNamespace; DB: D1Database } };
const app = new Hono<Env>()
  .get('/events', (c) => streamSSE(c, async (stream) => {
    let id = 0;
    while (true) {
      const val = await c.env.KV.get('last-update');
      await stream.writeSSE({ data: val ?? '', event: 'update', id: String(id++) });
      await stream.sleep(1000);
    }
  }));

export default app;                            // Workers entrypoint
```

## Output Template

1. **Framework/runtime choice** — decision table row applied, one-line rationale.
2. **Module/router layout** — files listed; NestJS module graph or Hono router tree.
3. **DTOs + validation** — `class-validator` decorators or Zod schemas for every input.
4. **Auth + middleware** — guard/middleware list, JWT/Passport config.
5. **Tests** — unit (service) + integration (route); commands listed.
6. **Verify gates** — `tsc --noEmit`, `eslint`, `npm test`, `npm run build`; for Hono add `npx hono request` round-trips.

Separate VERIFIED (type-checks, tests pass, build succeeds) from ASSUMED (perf under load, production auth scale).

## Knowledge Reference

NestJS 10+ (modules, DI, `@Injectable`, guards/pipes/interceptors/filters, class-validator, Swagger, TypeORM/Prisma, Passport, Jest/Supertest); Hono 4 (routing, `c.set`/`c.get`, `c.env`, Zod validator, RPC `hc<AppType>`, streaming, JSX, `createFactory`, `app.request()` testing, Workers/Bun/Deno adapters); Node 20+ LTS, Bun, pino, OpenTelemetry, helmet, CORS, rate limiting.
