# REST + OpenAPI 3.1

REST design rules, OpenAPI 3.1 spec patterns, versioning, pagination, idempotency, and RFC 7807 error envelopes. Lint with `npx @redocly/cli lint openapi.yaml` before any handler code ships.

## Resource Modeling Rules

1. **Nouns, not verbs** — `/users/{id}`, never `/getUser`. Verbs describe HTTP methods, not URLs.
2. **Plural nouns for collections** — `/orders`, `/orders/{id}/items`. Singular only for special singletons (`/me`, `/settings`).
3. **Sub-resources express hierarchy** — `/users/{id}/orders` lists orders for a user. Limit nesting to 2 levels; deeper → use query params.
4. **HTTP methods map to semantics** — `GET` (safe, idempotent), `PUT` (idempotent replace), `POST` (create, non-idempotent without key), `PATCH` (partial update), `DELETE` (idempotent).
5. **Status codes** — `200` OK, `201` Created (with `Location` header), `202` Accepted (async), `204` No Content, `400` bad request, `401` unauth, `403` forbidden, `404` not found, `409` conflict, `422` semantic failure, `429` rate limit, `500` server, `503` unavailable.

## OpenAPI 3.1 Essentials

OpenAPI 3.1 aligns with JSON Schema 2020-12. Prefer the flow style for terseness; expand when nesting gets deep.

### Minimal complete spec

```yaml
openapi: "3.1.0"
info:
  title: Example API
  version: "1.2.0"
  contact: { email: api@example.com }
  license: { name: MIT }
servers:
  - url: https://api.example.com/v1
    description: Production
paths:
  /users:
    get:
      operationId: listUsers
      tags: [Users]
      parameters:
        - { $ref: "#/components/parameters/Cursor" }
        - { name: limit, in: query, schema: { type: integer, default: 20, maximum: 100 } }
      responses:
        "200":
          description: Paginated users
          content:
            application/json:
              schema: { $ref: "#/components/schemas/UserPage" }
        "401": { $ref: "#/components/responses/Unauthorized" }
        "429": { $ref: "#/components/responses/TooManyRequests" }
    post:
      operationId: createUser
      parameters:
        - { $ref: "#/components/parameters/IdempotencyKey" }
      requestBody:
        required: true
        content:
          application/json: { schema: { $ref: "#/components/schemas/UserCreate" } }
      responses:
        "201":
          description: Created
          headers:
            Location: { schema: { type: string, format: uri } }
          content: { application/json: { schema: { $ref: "#/components/schemas/User" } } }
        "409": { $ref: "#/components/responses/Conflict" }
        "422": { $ref: "#/components/responses/Unprocessable" }
components:
  parameters:
    Cursor: { name: cursor, in: query, schema: { type: string }, description: Opaque cursor from previous page }
    IdempotencyKey:
      name: Idempotency-Key
      in: header
      required: true
      schema: { type: string, format: uuid }
      description: Deduplicates POST retries within 24h
  schemas:
    User:
      type: object
      required: [id, email, created_at]
      properties:
        id: { type: string, format: uuid, readOnly: true }
        email: { type: string, format: email }
        name: { type: string, nullable: true }
        created_at: { type: string, format: date-time, readOnly: true }
    UserCreate:
      type: object
      required: [email]
      properties:
        email: { type: string, format: email }
        name: { type: string }
    UserPage:
      type: object
      required: [data, pagination]
      properties:
        data: { type: array, items: { $ref: "#/components/schemas/User" } }
        pagination: { $ref: "#/components/schemas/CursorPage" }
    CursorPage:
      type: object
      required: [next_cursor, has_more]
      properties:
        next_cursor: { type: string, nullable: true }
        has_more: { type: boolean }
    Problem:
      type: object
      required: [type, title, status]
      properties:
        type: { type: string, format: uri, example: "https://api.example.com/errors/validation" }
        title: { type: string }
        status: { type: integer }
        detail: { type: string }
        instance: { type: string, format: uri }
        errors:
          type: array
          items:
            type: object
            properties:
              field: { type: string }
              message: { type: string }
  responses:
    Unauthorized: { description: Missing auth, content: { application/problem+json: { schema: { $ref: "#/components/schemas/Problem" } } } }
    Conflict: { description: Email exists, content: { application/problem+json: { schema: { $ref: "#/components/schemas/Problem" } } } }
    Unprocessable: { description: Validation failed, content: { application/problem+json: { schema: { $ref: "#/components/schemas/Problem" } } } }
    TooManyRequests:
      description: Rate limited
      headers: { Retry-After: { schema: { type: integer } } }
      content: { application/problem+json: { schema: { $ref: "#/components/schemas/Problem" } } }
  securitySchemes:
    BearerAuth: { type: http, scheme: bearer, bearerFormat: JWT }
security:
  - BearerAuth: []
```

Lint and mock:

```bash
npx @redocly/cli lint openapi.yaml
npx @stoplight/prism-cli mock openapi.yaml      # round-trip the contract before any handler
```

## Versioning

| Strategy | Where | Pros | Cons |
|---|---|---|---|
| URI (`/v1/`) | URL path | Visible, caches cleanly, simple routing | "Violates REST" (same resource, different URI) |
| Header (`Accept: application/vnd.x.v2+json`) | Accept header | URI stays clean | Invisible, hard to test in browser |
| Query (`?version=2`) | URL query | Easy | Easy to forget, cached wrong |
| Media type profile | Content-Type | RESTful, versioned schema | Verbose |

**Default: URI versioning for public APIs.** Header versioning is correct but costs you debuggability. Bump the major version (`/v2/`) only on breaking changes; non-breaking changes (add optional field, add endpoint) ship under the same version.

### Breaking vs non-breaking

| Breaking (bump version) | Non-breaking (same version) |
|---|---|
| Remove/rename field | Add optional field |
| Change field type | Add endpoint |
| Add required request field | Add response field |
| Change status code semantics | Fix bugs, improve perf |
| Change auth scheme | Add HTTP method to existing resource |

### Deprecation flow

```
Deprecation: true                       # signal sunset
Sunset: Wed, 31 Dec 2025 23:59:59 GMT   # hard removal date
Link: <https://api.example.com/v2/users>; rel="successor-version"
```

Migration window: ≥ 2 minor releases or ≥ 6 months, whichever is longer. Track usage by version header; only retire when traffic < 1%.

## Pagination

| Pattern | When | Cursor source |
|---|---|---|
| Offset (`?offset=20&limit=20`) | Small collections, admin UIs | Row number |
| Cursor (`?cursor=abc`) | Public APIs, large or changing data | Sort key of last row (e.g., `(created_at, id)`) |
| Keyset (`?after_id=123`) | Time-ordered, stable | Last ID |
| Page (`?page=2`) | Never at scale | Calculated |

**Default: opaque cursor.** Clients cannot compute the next cursor; they echo back `next_cursor` from the response. This survives inserts/deletes and is cheap on the DB (keyset scan vs `OFFSET`).

```sql
-- Cursor pagination SQL (keyset) — index on (created_at, id) makes this O(limit)
SELECT id, email, created_at
FROM users
WHERE (created_at, id) < ($1, $2)   -- cursor decodes to last row's (created_at, id)
ORDER BY created_at DESC, id DESC
LIMIT 21;                            -- fetch one extra to compute has_more
```

## Idempotency

POST/PUT that create state or charge money must accept `Idempotency-Key: <uuid>`. Server stores `(key, request_hash, response)` for 24h; same key returns the cached response, even on retry.

```python
# Server-side handler (pseudo)
async def create_user(payload, idempotency_key):
    cached = await redis.get(f"idem:{idempotency_key}")
    if cached: return cached                                # replay cached response
    existing_hash = await redis.get(f"idem-hash:{idempotency_key}")
    if existing_hash and existing_hash != hash(payload):
        raise Conflict("Idempotency-Key reused with different body")
    result = await db.insert(payload)
    await redis.setex(f"idem:{idempotency_key}", 86400, result)
    await redis.setex(f"idem-hash:{idempotency_key}", 86400, hash(payload))
    return result
```

Rules:
- Same key + same body → return cached response (status, body, headers).
- Same key + different body → `409 Conflict` (client bug).
- Key TTL ≥ 24h. Keys are per-merchant/per-client, not global.
- Safe methods (`GET`, `HEAD`) are inherently idempotent — no key needed.

## Error Envelope — RFC 7807 / RFC 9457

`Content-Type: application/problem+json`. The `type` URI is a stable, documented identifier — not a free-text message.

```json
{
  "type": "https://api.example.com/errors/validation",
  "title": "Validation Error",
  "status": 422,
  "detail": "The 'email' field must be a valid email address.",
  "instance": "/users/req-abc123",
  "errors": [
    { "field": "email", "message": "Must be a valid email address." }
  ]
}
```

- `type` — URI, machine-readable, resolve to a docs page.
- `title` — short human summary, stable per `type`.
- `status` — HTTP status (redundant with response, but required for inline body).
- `detail` — instance-specific human explanation.
- `instance` — request/correlation URI for debugging.
- `errors[]` — extension for field-level validation failures.

**Never** return raw stack traces, DB errors, or `{"error": "something went wrong"}`. Every error response is a `Problem`.

## Status code reference

| Code | Meaning | When |
|---|---|---|
| 200 | OK | Successful GET, PUT, PATCH |
| 201 | Created | POST that creates a resource (with `Location`) |
| 202 | Accepted | Async work started (return job URL) |
| 204 | No Content | Successful DELETE, PUT with no body |
| 400 | Bad Request | Malformed syntax (not a validation error) |
| 401 | Unauthorized | Missing/invalid auth |
| 403 | Forbidden | Authenticated but not allowed |
| 404 | Not Found | Resource doesn't exist (or hidden — pick one) |
| 409 | Conflict | Version conflict, duplicate, idempotency clash |
| 422 | Unprocessable Entity | Semantic validation failure |
| 429 | Too Many Requests | Rate limited (with `Retry-After`) |
| 500 | Internal Server Error | Server bug |
| 503 | Service Unavailable | Down for maintenance / overloaded |

## Filtering, sorting, sparse fieldsets

```
GET /users?filter[status]=active&sort=-created_at&fields[users]=id,email
```

- `filter[field]=value` — Brackets are verbose but parse cleanly.
- `sort=-created_at,name` — `-` prefix = descending.
- `fields[type]=a,b` — Sparse fieldset (GraphQL-style on REST).
- Operators via `filter[field][gt]=2024-01-01`. Document the operator vocabulary explicitly.

## Rate limiting

Headers (`RFC` drafts + de facto):
```
RateLimit-Limit: 1000
RateLimit-Remaining: 999
RateLimit-Reset: 60                          # seconds until window resets
Retry-After: 30                              # on 429 only
```

Pick a window (per-second or per-minute), pick a key (user, IP, API key), document the limit per plan. Always return 429 with `Retry-After`, never 503.

## CORS

```
Access-Control-Allow-Origin: https://app.example.com   # never * with credentials
Access-Control-Allow-Credentials: true
Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS
Access-Control-Allow-Headers: Authorization, Content-Type, Idempotency-Key
Access-Control-Expose-Headers: Location, Sunset, Deprecation
Access-Control-Max-Age: 600
```

Preflight `OPTIONS` must be cheap — short-circuit before any DB call.

## Verification gates

- `npx @redocly/cli lint openapi.yaml` — schema/style errors.
- `npx @stoplight/prism-cli mock openapi.yaml` — contract round-trips.
- Contract tests: Pact (consumer-driven) or Dredd/spectator (spec→server).
- Generated client `tsc --noEmit` — TS types compile.
- Load test collection endpoints with `k6` or `vegeta`; cursor pagination should remain flat under load, offset should degrade.
