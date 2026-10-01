# Protocol Selection — REST vs GraphQL vs WebSocket vs gRPC vs SSE

Decision tables and comparison criteria for picking the wire protocol. One protocol per concern; mixing is allowed across boundaries, never within one endpoint.

## Master decision table

| Need | Pick | Why | Reject |
|---|---|---|---|
| CRUD over HTTP, cacheable, predictable | **REST + OpenAPI 3.1** | HTTP semantics, CDN cache, broad tooling | GraphQL (over-fetch), gRPC (browser-hostile) |
| Client-driven shape, many clients, nested graph | **GraphQL** | one round-trip, typed schema, codegen | REST (N+1 round-trips), gRPC (no client flexibility) |
| Bidirectional low-latency push (chat, presence) | **WebSocket (Socket.IO)** | full-duplex, rooms, acks | SSE (server→client only), REST polling |
| Server→client stream, simple, HTTP-friendly | **SSE** | auto-reconnect, HTTP/2 multiplex, proxy-safe | WebSocket (overkill), long-poll (chatty) |
| Internal service-to-service, schema-strict, streaming | **gRPC + protobuf** | binary, bidi-stream, polyglot codegen | REST (verbose), GraphQL (over-flexible) |
| Async fire-and-forget across services | **Message broker** (Kafka/NATS/AMQP) | durable, replay, decoupled | REST sync (couples uptime) |
| Public API, third-party devs | **REST** | everyone understands HTTP; broad tooling | gRPC (clients you don't control), GraphQL (steep learning) |
| Mobile, intermittent network | **REST + retry/ETag** or **gRPC** | small payloads, idempotent, ETag cache | WebSocket (battery, NAT timeouts) |
| Edge/serverless function | **REST** (or **Hono/Cloudflare Workers**) | stateless, cold-start friendly | WebSocket (stateful, expensive on serverless) |

## Decision tree

```
1. Is this a public API for third parties?
   YES → REST + OpenAPI 3.1 (stop)
   NO  → continue

2. Is the traffic async, fire-and-forget, between services?
   YES → Message broker (Kafka/NATS/AMQP) (stop)
   NO  → continue

3. Is the traffic between internal services (you control both ends)?
   YES → gRPC + protobuf (stop)
   NO  → continue

4. Is the client driving the shape (many fields, many views)?
   YES → GraphQL (stop)
   NO  → continue

5. Is this real-time push from server?
   5a. Bidirectional (client also sends frequently) → WebSocket
   5b. Server→client only → SSE
   5c. Infrequent (every 30s+) → REST polling with ETag
   NO → continue

6. Default → REST + OpenAPI 3.1
```

## Comparison matrix

| Criterion | REST | GraphQL | WebSocket | gRPC | SSE |
|---|---|---|---|---|---|
| Transport | HTTP/1.1 or HTTP/2 | HTTP (POST) | WS / HTTP upgrade | HTTP/2 | HTTP/1.1 or HTTP/2 |
| Direction | Request/response | Request/response | Full-duplex | Full-duplex (stream) | Server→client |
| Schema format | OpenAPI 3.1 (JSON Schema) | SDL | Ad-hoc / Socket.IO events | protobuf | None / ad-hoc |
| Cacheable | Yes (HTTP cache, CDN) | Limited (persisted query, response cache) | No | No | Yes (HTTP cache) |
| Browser-native | Yes | Yes (with `graphql-ws` for subs) | Yes | No (use grpc-web / Connect-RPC) | Yes (`EventSource`) |
| Streaming | No (or SSE) | Subscriptions | Yes (bidi) | Yes (4 modes) | Yes (server→client) |
| Codegen | Strong (OpenAPI → TS/Go/Java) | Strong (SDL → TS/Go/Java) | Weak (manual) | Strong (proto → all languages) | Weak |
| Schema strictness | Medium | High | Low | Very high | Low |
| Best for | CRUD, public APIs | Client-flexible reads | Chat, presence, multiplayer | Service mesh | Notifications, live feed |
| Worst for | Nested graphs, many round-trips | Caching, simple CRUD | One-way push | Browser clients | Client→server |
| Tooling maturity | Very high | High | Medium | High (in JVM/Go ecosystem) | Medium |

## REST — when to pick

- Public API for external developers.
- CRUD over resources with stable shape.
- Cacheable responses (CDN, browser, ETag).
- Many clients with different stacks.
- Simple tooling requirement (curl, browser, any HTTP client).
- HTTP status code semantics matter.

Avoid REST when:
- Client needs nested data requiring 3+ round-trips → GraphQL.
- Server-push needed → SSE/WebSocket.
- Internal mesh with strict schema → gRPC.

## GraphQL — when to pick

- Many clients with different data needs (mobile vs web vs partner).
- Nested graph that requires multiple REST round-trips.
- Aggregation across multiple services (federation).
- Client teams want autonomy from server teams.

Avoid GraphQL when:
- Simple CRUD → REST is simpler.
- HTTP caching is critical → GraphQL POST is not cacheable.
- Service-to-service internal → gRPC is stricter.
- Team can't maintain DataLoader/depth/complexity discipline → GraphQL abuse will DoS you.

## WebSocket — when to pick

- Bidirectional, low-latency, persistent connection (chat, presence, multiplayer).
- High message frequency in both directions.
- Sub-protocol framing needed (binary protocols).

Avoid WebSocket when:
- One-way server push → SSE is simpler.
- Infrequent updates → polling is cheaper (battery on mobile).
- Public API for third parties → REST/SSE is more standard.
- Serverless/edge → stateful connections are expensive.

## SSE — when to pick

- Server→client only (notifications, live feed, progress).
- Auto-reconnect required (built into `EventSource`).
- HTTP/2 multiplexing with other requests on same connection.
- Proxy-friendly (no upgrade dance).
- Simple text payloads (or base64 binary).

Avoid SSE when:
- Client also needs to send frequent messages → WebSocket.
- Binary protocol → WebSocket or gRPC.
- Polyglot codegen needed → gRPC.

```js
// Server (Hono/Node)
app.get("/events", (c) => streamSSE(c, async (stream) => {
  for await (const event of eventBus.subscribe()) {
    await stream.writeSSE({ data: JSON.stringify(event), event: event.type, id: event.id });
  }
}));

// Client
const es = new EventSource("/events");
es.addEventListener("order-updated", (e) => console.log(JSON.parse(e.data)));
es.onerror = () => { /* browser auto-reconnects */ };
```

## gRPC — when to pick

- Service-to-service within your org (you control both ends).
- Polyglot services (Go, Java, Python, C#, Rust).
- Schema-strict contracts with breaking-change CI gate.
- Streaming (bidi or server-stream).
- Internal API never exposed to browsers directly.

Avoid gRPC when:
- Browser client required → use Connect-RPC or grpc-web (added complexity).
- Public API for third parties → they don't want to manage proto files.
- Simple CRUD → REST is fine.

## Message broker — when to pick

- Async fire-and-forget (order placed → notify → ship → bill).
- Decoupling uptime (consumer down doesn't block producer).
- Replay (consumer can re-process historical events).
- Fan-out to many consumers (event-driven architecture).

Avoid brokers when:
- Sync response required → caller needs an answer now.
- Simple request/response → broker adds latency and complexity.
- No durability requirement → in-process events are simpler.

## Hybrid architectures

Real systems mix protocols:

- **Public API**: REST + OpenAPI.
- **Internal mesh**: gRPC between services.
- **Real-time features**: WebSocket for chat, SSE for notifications, REST for everything else.
- **Async workflows**: Kafka/NATS for events between services.
- **Mobile clients**: REST with ETag + occasional WebSocket for push.

The rule: **one protocol per concern**, not one protocol for everything. A single service may expose REST publicly and gRPC internally; that's fine. A single endpoint should not switch protocols mid-flight.

## Versioning across protocols

| Protocol | Versioning strategy |
|---|---|
| REST | URI version (`/v1/`) for public; header (`Accept`) for internal |
| GraphQL | Schema evolution (additive only); deprecation directives; no URI version |
| gRPC | proto field numbers never reuse; `reserved` for deleted; new RPC methods for breaking behavior |
| WebSocket | Event name versioning (`message.v2`); negotiate on connect |
| SSE | Event name versioning; `Last-Event-ID` for replay |
| Message broker | Topic name versioning (`orders.v2.created`); schema registry for payload |

## Verification checklist

- [ ] One protocol picked per concern (no mixing within endpoint).
- [ ] Contract artifact written (OpenAPI / SDL / .proto / event schema).
- [ ] Lint passes (`redocly` / `rover` / `buf`).
- [ ] Mock round-trip confirmed (Prism / `wscat` / `grpcurl`).
- [ ] Authn/authz documented in contract.
- [ ] Versioning strategy explicit.
- [ ] Pagination / streaming / backpressure handled for the chosen protocol.
- [ ] Contract test (Pact / spectator) green.
- [ ] Load test planned for real-time endpoints (k6 / artillery / ghz).
- [ ] Deprecation policy documented for future breaking changes.
