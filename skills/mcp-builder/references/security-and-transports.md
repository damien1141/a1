# Transports & Security Reference

Transports (stdio / HTTP+SSE / streamable HTTP) and the security controls every production MCP server needs: auth, permission scopes, rate-limiting, input sanitization, and the 2025-06-18 OAuth 2.1 spec.

## Transport Selection

| Transport | Use Case | Pros | Cons |
|---|---|---|---|
| **stdio** | Local tools (Claude Desktop, Cursor) | Simple; no port management; auto-restart | Local only; one client per process |
| **HTTP+SSE** (2024-11-05) | Remote servers (legacy) | Works over HTTP; multi-client | Two connections; stateful; replaced by streamable HTTP |
| **Streamable HTTP** (2025-06-18+) | Remote servers (modern) | Single endpoint; stateless-friendly; serverless-compatible | Newer; some clients still on SSE |

**Rule:** Local tool → stdio. Remote service → streamable HTTP (or SSE for older clients).

## stdio Transport

The client spawns the server as a subprocess and communicates over its stdin/stdout.

```python
# Python
from mcp.server.stdio import stdio_server
async with stdio_server() as (read, write):
    await app.run(read, write, app.create_initialization_options())
```

```typescript
// TypeScript
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
await server.connect(new StdioServerTransport());
```

### stdio Rules (Critical)

1. **stdout is for protocol messages only** — newline-delimited JSON
2. **stderr is for logs** — `console.error` / `logging` to stderr
3. **Never `print()` / `console.log()` to stdout** — corrupts the protocol stream
4. **One client per process** — stdio servers can't serve multiple clients
5. **Client manages process lifecycle** — server should handle `SIGTERM` gracefully

```python
import signal, sys

def shutdown(signum, frame):
    cleanup()
    sys.exit(0)

signal.signal(signal.SIGTERM, shutdown)
signal.signal(signal.SIGINT, shutdown)
```

## HTTP + SSE Transport (2024-11-05)

Server exposes two endpoints: `POST /messages` for client requests, `GET /sse` for server-pushed responses/notifications.

```python
from mcp.server.sse import SseServerTransport
from starlette.applications import Starlette
from starlette.routing import Mount, Route

sse = SseServerTransport("/messages/")

async def handle_sse(request):
    async with sse.connect_sse(request.scope, request.receive, request._send) as streams:
        await app.run(streams[0], streams[1], app.create_initialization_options())

app_starlette = Starlette(routes=[
    Route("/sse", endpoint=handle_sse),
    Mount("/messages/", app=sse.handle_post_message),
])
```

## Streamable HTTP (2025-06-18+)

Single endpoint; POST for requests; response can be JSON or SSE stream (for server-pushed notifications).

```python
from mcp.server.streamable_http_manager import StreamableHTTPSessionManager

manager = StreamableHTTPSessionManager(app=app)

async def handle_http(scope, receive, send):
    await manager.handle_request(scope, receive, send)

app_starlette = Starlette(routes=[
    Mount("/mcp", app=handle_http),
])
```

### Session Management

Streamable HTTP uses `Mcp-Session-Id` header:
- Server returns a session ID in the `initialize` response
- Client includes it in subsequent requests
- Server can use it to maintain state across requests (or be stateless and re-initialize each time)

### Stateless Mode (Serverless-Friendly)

For serverless (AWS Lambda, Cloudflare Workers):
- Don't store session state in memory
- Each request re-initializes from auth context
- Use external storage (Redis, DB) for any needed session data
- Trade-off: no `notifications/resources/updated` (no persistent connection)

## Authentication

### Local (stdio) Servers

No transport-level auth needed — the client controls process launch. Still:
- Pass secrets via **environment variables** in the client config, not args
- Don't expose secrets in tool descriptions
- Use OS-level file permissions for any credentials files

```jsonc
// claude_desktop_config.json — secrets via env, NOT args
{
  "mcpServers": {
    "my-server": {
      "command": "node",
      "args": ["/abs/path/to/server.js"],
      "env": { "API_KEY": "sk-...", "DB_URL": "..." }
    }
  }
}
```

### Remote (HTTP) Servers — Bearer Token

```python
from starlette.middleware import Middleware
from starlette.middleware.authentication import AuthenticationMiddleware

async def auth_middleware(request, call_next):
    auth = request.headers.get("Authorization", "")
    if not auth.startswith("Bearer "):
        return JSONResponse({"error": "unauthorized"}, status_code=401)
    token = auth[7:]
    if not verify_token(token):
        return JSONResponse({"error": "invalid token"}, status_code=401)
    request.state.user = decode_token(token)
    return await call_next(request)
```

### OAuth 2.1 (2025-06-18+ spec)

The 2025-06-18 spec requires remote MCP servers to support OAuth 2.1 with:
- Authorization Code flow with PKCE (mandatory)
- Dynamic Client Registration (RFC 7591)
- Server metadata discovery (RFC 8414) at `/.well-known/oauth-authorization-server`
- Resource Indicators (RFC 8707) — `resource` parameter in token requests

Use a library: `authlib` (Python), `oauth4webapi` (Node.js). Don't roll your own OAuth.

## Permission Scopes

Tools can declare required scopes; clients check user permissions before calling.

```jsonc
// Tool with required scope (proposed extension, some clients support)
{
  "name": "delete_user",
  "description": "Permanently delete a user account",
  "inputSchema": { ... },
  "annotations": {
    "readOnlyHint": false,
    "destructiveHint": true,
    "idempotentHint": false,
    "openWorldHint": false
  }
}
```

### Tool Annotations (Standard)

```jsonc
"annotations": {
  "readOnlyHint": true,       // tool doesn't modify state
  "destructiveHint": false,   // tool is destructive (delete, drop)
  "idempotentHint": true,     // safe to retry
  "openWorldHint": true       // interacts with external entities
}
```

Clients use these hints to:
- Show confirmation dialogs for `destructiveHint: true`
- Auto-retry `idempotentHint: true` tools on transient failures
- Group read-only vs write tools in UI

### Custom Permission Scopes

For multi-tenant or role-based systems, implement your own scope check:

```python
@mcp.tool()
async def delete_user(ctx: Context, user_id: str) -> str:
    user = ctx.request_context.user
    if "admin" not in user.scopes:
        raise McpError(UNAUTHORIZED, "Requires admin scope")
    await db.delete_user(user_id)
    return "User deleted"
```

## Rate Limiting

```python
from asyncio import Lock
from collections import defaultdict
from datetime import datetime, timedelta

class RateLimiter:
    def __init__(self, limit: int = 10, window_seconds: int = 60):
        self.limit = limit
        self.window = timedelta(seconds=window_seconds)
        self.calls = defaultdict(list)
        self.lock = Lock()

    async def check(self, key: str) -> None:
        async with self.lock:
            now = datetime.now()
            self.calls[key] = [t for t in self.calls[key] if now - t < self.window]
            if len(self.calls[key]) >= self.limit:
                raise McpError(RATE_LIMIT_EXCEEDED, "Rate limit exceeded")
            self.calls[key].append(now)

# Per-user, per-tool rate limiting
rate_limiter = RateLimiter(limit=30, window_seconds=60)

@mcp.tool()
async def expensive_op(ctx: Context, arg: str) -> str:
    user = ctx.request_context.user
    await rate_limiter.check(f"{user.id}:expensive_op")
    # ... do work
```

For distributed deployments, use Redis-backed rate limiting instead of in-memory.

## Input Sanitization

### Path Traversal Defense

```typescript
import path from "node:path";

const ALLOWED_ROOT = path.resolve(process.env.ALLOWED_ROOT!);

function safePath(userPath: string): string {
  const resolved = path.resolve(ALLOWED_ROOT, userPath);
  // Critical: check the resolved path is inside ALLOWED_ROOT
  // path.resolve can escape via ".." — always validate after resolving
  if (resolved !== ALLOWED_ROOT && !resolved.startsWith(ALLOWED_ROOT + path.sep)) {
    throw new McpError(ErrorCode.InvalidParams, "Path traversal denied");
  }
  // Optional: also check symlinks don't escape
  const real = fs.realpathSync(resolved);
  if (real !== ALLOWED_ROOT && !real.startsWith(ALLOWED_ROOT + path.sep)) {
    throw new McpError(ErrorCode.InvalidParams, "Symlink escape denied");
  }
  return real;
}
```

### SQL Injection Defense

```python
# WRONG — string interpolation
query = f"SELECT * FROM {table} WHERE id = {user_id}"

# RIGHT — parameterized queries
rows = await db.fetch(
    "SELECT * FROM users WHERE id = $1", user_id
)

# For dynamic table names (rare), whitelist:
ALLOWED_TABLES = {"users", "orders", "products"}
if table not in ALLOWED_TABLES:
    raise ValueError("Invalid table")
```

### Command Injection Defense

```python
import subprocess, shlex

# WRONG — shell=True with user input
subprocess.run(f"convert {filename} out.png", shell=True)

# RIGHT — shell=False, args list
subprocess.run(["convert", filename, "out.png"], check=True)
```

### Output Sanitization

- **Cap text output length** — return first N chars + "...truncated" for long outputs
- **Redact secrets** — strip API keys, tokens, PII from tool output before returning
- **Validate resource contents** — don't expose internal paths, credentials in config files

## Secrets Management

```python
import os
from functools import lru_cache

@lru_cache
def get_secrets() -> dict:
    """Load secrets once at startup from env or secrets manager."""
    return {
        "api_key": os.environ["API_KEY"],
        "db_url": os.environ["DB_URL"],
    }

# In handlers — never log secrets
@mcp.tool()
async def call_api(query: str) -> str:
    secrets = get_secrets()
    # NEVER: logger.info(f"Calling API with key {secrets['api_key']}")
    async with httpx.AsyncClient() as client:
        r = await client.get("https://api.example.com/search",
            headers={"Authorization": f"Bearer {secrets['api_key']}"},
            params={"q": query})
    return r.text
```

For production: use AWS Secrets Manager, GCP Secret Manager, HashiCorp Vault, or Doppler — never commit secrets to code or config files.

## Network Security (HTTP Servers)

- **HTTPS only** — redirect HTTP to HTTPS; HSTS header
- **CORS** — restrictive; only allow known MCP clients
- **Request size limits** — reject bodies >1MB (configurable)
- **Connection timeouts** — 30s idle, 60s total
- **DDoS protection** — Cloudflare, AWS WAF, or rate-limit at edge
- **IP allowlisting** — for internal-only servers

## Security Checklist

Before shipping an MCP server:

- [ ] All tool inputs validated with schema (Zod / Pydantic)
- [ ] Path inputs validated against allowed root (no traversal)
- [ ] SQL uses parameterized queries (no interpolation)
- [ ] Command execution uses `shell=False` (no injection)
- [ ] Output capped and secrets redacted
- [ ] Secrets loaded from env / secrets manager (not code)
- [ ] Auth required for HTTP transport (Bearer or OAuth 2.1)
- [ ] Rate limiting per user/tool
- [ ] Timeouts on every external call
- [ ] Logs go to stderr (stdio transport) — never stdout
- [ ] Tool annotations set (`destructiveHint`, `readOnlyHint`)
- [ ] Permission scopes checked for sensitive tools
- [ ] HTTPS enforced (HTTP transport)
- [ ] Request size limits set
- [ ] Inspector test passed — no protocol errors
- [ ] Unit tests for happy path + error path per tool
- [ ] No secrets in tool descriptions or error messages
- [ ] Dependencies pinned and scanned (`npm audit` / `pip-audit`)

## Common Security Bugs

| Bug | Impact | Fix |
|---|---|---|
| Path traversal in filesystem tool | Read any file | `path.resolve` + startsWith check |
| SQL injection in DB tool | Data leak / deletion | Parameterized queries |
| Command injection in shell tool | RCE | `shell=False`, args list |
| Secrets in tool description | LLM may leak them | Don't include in description |
| `console.log` to stdout (stdio) | Protocol corruption | Use stderr |
| No auth on HTTP server | Anyone can call tools | Bearer token / OAuth 2.1 |
| Unbounded output | Memory exhaustion | Cap text length |
| No rate limiting | Cost / DoS | Per-user, per-tool limits |
| Trusting client-validated args | Bypass server validation | Always re-validate server-side |
