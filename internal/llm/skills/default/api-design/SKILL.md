---
name: api-design
description: Senior API architect for REST (OpenAPI 3.1), GraphQL (Apollo Federation 2.5+), WebSocket (Socket.IO), and gRPC. Use when designing resource models, schema-first GraphQL, real-time channels, or protobuf contracts. Invoke for versioning, idempotency, pagination, RFC 7807 error envelopes, DataLoader N+1 prevention, backpressure, and protocol selection.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: backend
  triggers: API design, REST, OpenAPI, GraphQL, Apollo Federation, DataLoader, WebSocket, Socket.IO, gRPC, protobuf, API versioning, pagination, idempotency, error envelope, RFC 7807, real-time
  role: specialist
  scope: implementation
  output-format: code
  related-skills: node-backend, python-backend, system-architecture, database-pro
---

# API Design

Designs wire contracts across REST, GraphQL, WebSocket, and gRPC. Picks the protocol from a decision table, then ships a spec-first artifact (OpenAPI 3.1 / SDL / `.proto`) that compiles, lints, and mocks before any handler is written.

## When to Use

New API surface (public or internal); protocol selection (REST/GraphQL/WebSocket/gRPC/SSE); hardening existing APIs (pagination, idempotency, error envelope, versioning); GraphQL federation / DataLoader / depth-complexity limits; real-time push (chat, presence); cross-language service-to-service protobuf contracts.

## Operating Loop

1. **Pick the protocol** — apply the Decision Table below; one protocol per concern.
2. **Spec first** — write OpenAPI 3.1 / GraphQL SDL / `.proto` before handlers. Lint: `npx @redocly/cli lint openapi.yaml` / `rover supergraph compose` / `buf lint`.
3. **Mock and verify** — `npx @stoplight/prism-cli mock openapi.yaml` or `npx wscat -c ws://localhost:3000`. Client can call the contract before server logic exists.
4. **Implement handlers** — controllers/resolvers/socket events mirror the spec; never drift.
5. **Harden** — versioning, idempotency keys, pagination, rate limits, RFC 7807 errors, authn/authz.
6. **Verify gates** — lint passes, mock round-trips, contract test suite green (Pact/spectator), `tsc --noEmit` on generated clients, load test for real-time.
7. **Plan evolution** — `Sunset`/`Deprecation` header, changelog, migration window ≥ 2 minor versions.

## Protocol Decision Table

| Need | Pick | Why | Reject |
|---|---|---|---|
| CRUD over HTTP, cacheable, predictable | **REST + OpenAPI 3.1** | HTTP semantics, CDN cache, broad tooling | GraphQL (over-fetch), gRPC (browser-hostile) |
| Client-driven shape, many clients, nested graph | **GraphQL** | one round-trip, typed schema, codegen | REST (N+1 round-trips), gRPC (no client flexibility) |
| Bidirectional low-latency push (chat, presence) | **WebSocket (Socket.IO)** | full-duplex, rooms, acks | SSE (server→client only), REST polling |
| Server→client stream, simple, HTTP-friendly | **SSE** | auto-reconnect, HTTP/2 multiplex, proxy-safe | WebSocket (overkill), long-poll (chatty) |
| Internal service-to-service, schema-strict, streaming | **gRPC + protobuf** | binary, bidi-stream, polyglot codegen | REST (verbose), GraphQL (over-flexible) |
| Async fire-and-forget across services | **Message broker** (Kafka/NATS/AMQP) | durable, replay, decoupled | REST sync (couples uptime) |

**Default rule:** Public API → REST. Many-shaped clients → GraphQL. Real-time → WebSocket. Service mesh → gRPC. Ship REST first when unsure; GraphQL/gRPC are refactor targets once pain is measured.

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| REST + OpenAPI 3.1 (versioning, pagination, idempotency, errors) | `references/rest-openapi.md` | Building REST endpoints, OpenAPI specs, RFC 7807 |
| GraphQL (schema, federation, DataLoader, complexity) | `references/graphql-federation.md` | SDL, federation subgraphs, N+1, persisted queries |
| WebSocket + Socket.IO (channels, reconnection, backpressure) | `references/websocket-realtime.md` | Real-time, rooms, presence, Redis adapter, scaling |
| gRPC + protobuf contracts | `references/grpc-contracts.md` | Service-to-service, bidi-stream, buf, versioning |
| Protocol selection + comparison tables | `references/protocol-decisions.md` | Choosing REST vs GraphQL vs WS vs gRPC vs SSE |

## Constraints

### MUST DO
- Spec-first: contract compiles/lints before handler code is written.
- Pagination on **every** collection endpoint — cursor over offset at scale.
- RFC 7807 `application/problem+json` for all REST errors with stable `type` URIs.
- `Idempotency-Key` header on `POST`/`PUT` that creates state or charges money.
- Version REST in URI (`/v1/`) for public APIs; document deprecation via `Sunset` header.
- GraphQL: per-request DataLoader instance + depth limit + complexity limit + persisted queries in prod.
- WebSocket: heartbeat ping/pong, sticky sessions, message queue during disconnect, presence cleanup on `disconnect`.
- gRPC: backward-compatible field additions only (proto3 `optional`), never reuse field numbers.
- Authn/authz documented in the spec (`securitySchemes`), enforced in middleware — never in handlers.

### MUST NOT DO
- Use verbs in REST resource URIs (`/getUser` → `/users/{id}`).
- Mix protocols within one endpoint (no "REST endpoint that upgrades to WS mid-call").
- Return bare strings/arrays at the REST root — wrap in `{data, ...meta}` envelope consistently.
- Ship GraphQL without depth + complexity limits — malicious nested queries will DoS you.
- Store WebSocket presence in process memory without a clustering plan (Redis or shared store).
- Make breaking proto field changes (renumber, reuse, change type) — add a new field instead.
- Expose implementation details (DB column names, internal IDs) in the public contract.
- Skip contract tests — server and client must conform to the spec, not to each other.

## Code Examples

### REST — OpenAPI 3.1 essentials (full spec in `references/rest-openapi.md`)

```yaml
openapi: "3.1.0"
info: { title: Example API, version: "1.2.0" }
paths:
  /v1/users:
    get:                                            # cursor pagination
      operationId: listUsers
      parameters:
        - { name: cursor, in: query, schema: { type: string } }
        - { name: limit,  in: query, schema: { type: integer, default: 20, maximum: 100 } }
      responses:
        "200": { description: Paginated users, content: { application/json: { schema: { $ref: "#/components/schemas/UserPage" } } } }
        "429": { $ref: "#/components/responses/TooManyRequests" }
    post:                                           # Idempotency-Key on state-changing POST
      operationId: createUser
      parameters:
        - { name: Idempotency-Key, in: header, required: true, schema: { type: string, format: uuid } }
      responses:
        "201": { description: Created }
        "409": { $ref: "#/components/responses/Conflict" }
components:
  schemas:
    Problem:                                        # RFC 7807 — reuse for every 4xx/5xx
      type: object
      required: [type, title, status]
      properties:
        type:   { type: string, format: uri }       # stable URI, never bare string
        title:  { type: string }
        status: { type: integer }
        detail: { type: string }
        errors: { type: array, items: { type: object } }    # field-level validation
  responses:
    Conflict: { description: Email exists, content: { application/problem+json: { schema: { $ref: "#/components/schemas/Problem" } } } }
    TooManyRequests:
      description: Rate limited
      headers: { Retry-After: { schema: { type: integer } } }
      content: { application/problem+json: { schema: { $ref: "#/components/schemas/Problem" } } }
```
Errors use `Content-Type: application/problem+json`; `type` is a documented URI; `errors[]` for field-level validation.

### GraphQL — DataLoader N+1 prevention (one instance per request)

```js
const context = ({ req }) => ({
  loaders: {
    user: new DataLoader(async (ids) => {
      const users = await db.users.findMany({ where: { id: { in: ids } } });
      const byId = new Map(users.map((u) => [u.id, u]));
      return ids.map((id) => byId.get(id) ?? null);   // preserve input order
    }),
  },
});
const resolvers = { Review: { author: (r, _a, { loaders }) => loaders.user.load(r.authorId) } };
```
Federation subgraph SDL, depth/complexity limits, persisted queries — see `references/graphql-federation.md`.

### WebSocket — Socket.IO with auth, Redis adapter, presence

```js
io.use((socket, next) => {                              // auth before connection accepted
  const t = socket.handshake.auth.token;
  if (!t) return next(new Error("auth required"));
  try { socket.data.user = jwt.verify(t, JWT_SECRET); next(); } catch { next(new Error("invalid token")); }
});
io.adapter(createAdapter(pub, sub));                    // fan-out across instances
io.on("connection", (socket) => {
  const { userId } = socket.data.user;
  pub.hSet("presence", userId, socket.id);
  socket.on("message", ({ roomId, text }, ack) => {
    io.to(roomId).emit("message", { userId, text, ts: Date.now() });
    ack({ ok: true });                                  // client must ack → backpressure signal
  });
  socket.on("disconnect", () => pub.hDel("presence", userId));
});
```
Sticky sessions, heartbeat, reconnect/jitter, full scaling plan — see `references/websocket-realtime.md`.

### gRPC — proto3 with backward-compatible evolution

```proto
service OrderService {
  rpc CreateOrder(CreateOrderRequest) returns (Order);
  rpc StreamUpdates(OrderId) returns (stream OrderUpdate);        // server-stream
  rpc SyncInventory(stream Sku) returns (stream InventoryDelta);  // bidi-stream
}
message Order {
  string id = 1;
  string customer_id = 2;
  repeated LineItem items = 3;
  string legacy_coupon = 4 [deprecated = true];   // NEVER reuse numbers, NEVER change type
}
```
buf lint/build, Connect-RPC, backward-compat rules — see `references/grpc-contracts.md`.

## Output Template

1. **Protocol choice** — decision table row applied, one-line rationale.
2. **Contract artifact** — OpenAPI 3.1 YAML / GraphQL SDL / `.proto` / event schema.
3. **Lint/mock evidence** — `redocly lint` / `rover compose` / `buf lint` pass; mock round-trip confirmed.
4. **Hardening summary** — versioning, pagination, idempotency, error envelope, rate limit, authn.
5. **Verification gates** — contract test command(s), generated-client `tsc --noEmit`, load-test plan for real-time.
6. **Evolution plan** — deprecation window, breaking-change policy, changelog pointer.

Separate VERIFIED (lint passed, mock round-tripped, contract test green) from ASSUMED (perf under load, authn at scale) in the report.

## Knowledge Reference

OpenAPI 3.1, JSON Schema 2020-12, RFC 7807/9457; GraphQL SDL + Apollo Federation 2.5+ + DataLoader + complexity/depth; Socket.IO 4 + Redis adapter + sticky sessions + backpressure; gRPC (unary/stream/bidi) + proto3 + buf + Connect-RPC; SSE, Webhooks, HMAC, OAuth 2.0, JWT, mTLS.
