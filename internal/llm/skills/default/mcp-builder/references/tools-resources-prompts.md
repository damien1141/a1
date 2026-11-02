# Tools, Resources, and Prompts Reference

The three capability primitives an MCP server exposes. Plus response formats, common patterns, and schema-design rules.

## Tools (functions the LLM can call)

### Definition

A tool has: `name`, `description`, `inputSchema` (JSON Schema), and a handler.

```jsonc
{
  "name": "search_knowledge_base",
  "description": "Search the knowledge base using semantic search. Returns top 5 relevant documents with excerpts.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "query": { "type": "string", "minLength": 1, "maxLength": 500, "description": "Search query" },
      "max_results": { "type": "integer", "minimum": 1, "maximum": 50, "default": 5 },
      "filters": { "type": "object", "description": "Metadata filters" }
    },
    "required": ["query"]
  }
}
```

### Schema Design Rules

1. **Required fields only when truly required** — use Optional/defaults liberally
2. **Enums for categorical fields** — constrains output, reduces hallucination
3. **Bounds on numerics** — `minimum`, `maximum` prevent extreme inputs
4. **Pattern on strings** — regex for emails, IDs, paths
5. **maxLength on free text** — prevents runaway generation
6. **Descriptions on every field** — they're part of the prompt to the LLM
7. **Don't use `additionalProperties: true`** — be explicit about what you accept

### Response Formats

```jsonc
// Text (most common)
{ "content": [{ "type": "text", "text": "result string" }] }

// Multiple text blocks
{ "content": [
  { "type": "text", "text": "Found 3 results:" },
  { "type": "text", "text": "1. First\n2. Second\n3. Third" }
]}

// Image
{ "content": [
  { "type": "image", "data": "<base64>", "mimeType": "image/png" }
]}

// Embedded resource (client resolves/reads)
{ "content": [{
  "type": "resource",
  "resource": { "uri": "file:///data/results.json", "mimeType": "application/json",
                "text": "{\"results\": [...]}" }
}]}

// Tool error (LLM sees this and can recover)
{ "content": [{ "type": "text", "text": "User not found" }], "isError": true }
```

### Common Tool Patterns

#### Database Query Tool

```python
import re
from pydantic import BaseModel, Field

class QueryArgs(BaseModel):
    table: str = Field(..., pattern=r"^[a-zA-Z_][a-zA-Z0-9_]*$")  # SQL-safe
    filter: dict[str, str] = Field(default_factory=dict)
    limit: int = Field(default=10, ge=1, le=100)

@mcp.tool()
async def query_database(args: QueryArgs) -> str:
    # Parameterized queries only — no string interpolation
    where_clause, params = build_where(args.filter)
    rows = await db.fetch(
        f"SELECT * FROM {args.table} WHERE {where_clause} LIMIT $1",
        *params, args.limit
    )
    return json.dumps([dict(r) for r in rows])
```

#### Filesystem Tool (with path validation)

```typescript
import path from "node:path";
import fs from "node:fs/promises";

const ALLOWED_ROOT = path.resolve(process.env.ALLOWED_ROOT || "./data");

function safePath(userPath: string): string {
  const resolved = path.resolve(ALLOWED_ROOT, userPath);
  if (!resolved.startsWith(ALLOWED_ROOT + path.sep) && resolved !== ALLOWED_ROOT) {
    throw new McpError(ErrorCode.InvalidParams, "Path traversal denied");
  }
  return resolved;
}

server.tool("read_file", { path: z.string().min(1) }, async ({ path: p }) => {
  const content = await fs.readFile(safePath(p), "utf-8");
  return { content: [{ type: "text", text: content }] };
});
```

#### HTTP API Tool (with timeout)

```python
import httpx

@mcp.tool()
async def fetch_api(url: str, method: str = "GET") -> str:
    if not url.startswith("https://"):
        raise ValueError("Only HTTPS allowed")
    async with httpx.AsyncClient(timeout=30.0) as client:
        try:
            r = await client.get(url, headers={"User-Agent": "MCP-Server/1.0"})
            r.raise_for_status()
            return r.text
        except httpx.HTTPError as e:
            raise McpError(INTERNAL_ERROR, f"HTTP failed: {e}")
```

#### Long-Running Job (async)

```typescript
server.tool("start_job", { type: z.string() }, async ({ type }) => {
  const jobId = await jobQueue.enqueue(type);
  return { content: [{ type: "text", text: JSON.stringify({ jobId, status: "queued" }) }] };
});

server.tool("check_job", { jobId: z.string().uuid() }, async ({ jobId }) => {
  const status = await jobQueue.getStatus(jobId);
  return { content: [{ type: "text", text: JSON.stringify(status) }] };
});
```

#### Tool with Progress (streaming)

```python
from mcp.server.fastmcp import Context

@mcp.tool()
async def process_large_file(ctx: Context, file_id: str) -> str:
    chunks = list_chunks(file_id)
    for i, chunk in enumerate(chunks):
        await process(chunk)
        await ctx.report_progress(i + 1, len(chunks))  # client shows progress
    return "Done"
```

### Best Practices

1. **Descriptive names**: `search_knowledge_base` not `search`
2. **Clear descriptions**: include what the tool returns, not just what it does
3. **Idempotency keys** for write operations
4. **Rate-limiting** per user/tool (see security reference)
5. **Timeouts** on every external call (30s default)
6. **Structured output** (JSON in text) — easier for LLM to parse than free text
7. **Don't expose destructive operations without confirmation** — client may show a confirm dialog

## Resources (data the LLM can read)

### Static Resources

```jsonc
{
  "uri": "file:///config/settings.json",
  "name": "Application Settings",
  "description": "Current application configuration",
  "mimeType": "application/json"
}
```

### Resource Templates (dynamic URIs)

```jsonc
{
  "uriTemplate": "user://{userId}/profile",
  "name": "User Profile",
  "description": "Get user profile by ID",
  "mimeType": "application/json"
}
```

The client expands the template with arguments; the server resolves the URI.

### Resource Contents

```jsonc
// Text
{ "uri": "file:///config.json", "mimeType": "application/json", "text": "{...}" }

// Binary
{ "uri": "file:///image.png", "mimeType": "image/png", "blob": "<base64>" }
```

### When to Use Resources vs Tools

| Use a **resource** when | Use a **tool** when |
|---|---|
| Data is read-only | Action has side effects |
| Client should be able to browse/list | Action requires specific arguments |
| Content is addressable by URI | Action is a function call |
| You want the LLM to read at its own pace | You want a discrete result |
| e.g., config files, schemas, docs | e.g., search, send_email, create_ticket |

### Subscriptions (push updates)

```python
@mcp.resource("config://app")
async def app_config() -> str:
    return json.dumps(get_config())

# After config changes, notify subscribers:
await ctx.session.send_resource_updated(uri="config://app")
```

Clients that subscribed via `resources/subscribe` get a `notifications/resources/updated` push.

## Prompts (reusable templates)

### Definition

```jsonc
{
  "name": "code_review",
  "description": "Generate code review comments",
  "arguments": [
    { "name": "language", "description": "Programming language", "required": true },
    { "name": "code", "description": "Code to review", "required": true }
  ]
}
```

### Handler

```python
@mcp.prompt()
def code_review(language: str, code: str) -> list:
    """Generate code review comments."""
    return [
        {"role": "user", "content": {
            "type": "text",
            "text": f"Review this {language} code:\n\n{code}"
        }}
    ]
```

### When to Use Prompts

- **Reusable prompt templates** the client can render and send to any LLM
- **Domain-specific instructions** (e.g., "analyze this log file", "summarize this contract")
- **Multi-message prompts** (system + user + assistant setup)
- **Templated prompts** that take arguments

### Best Practices

1. **Descriptive names + descriptions** — these appear in the client's prompt picker
2. **Required vs optional arguments** — be explicit
3. **Return message arrays**, not single strings
4. **Test rendering** with all argument combinations
5. **Don't hardcode secrets** in prompts — pass via arguments

## Common Patterns Across All Three

### Dynamic Registration

```python
# Register tools at runtime based on config
def setup_tools(server: FastMCP, config: dict):
    for tool_config in config["tools"]:
        # Dynamically create and register
        server.tool(tool_config["name"], tool_config["schema"], tool_config["handler"])
    # Then notify clients the list changed
```

### Capability Listing (don't over-advertise)

Only advertise capabilities you actually implement. If you don't implement `resources/subscribe`, don't claim `subscribe: true` in your `resources` capability. Clients will call advertised methods and break if they're missing.

### Notification Patterns

```typescript
// After tool list changes
server.sendToolListChanged();

// After resource changes (push to subscribers)
server.sendResourceUpdated({ uri: "config://app" });

// After resource list changes (new resource added)
server.sendResourceListChanged();

// After prompt list changes
server.sendPromptListChanged();
```

Use these to keep clients in sync without polling.

## Anti-Patterns

1. **Tool that returns free-text errors** — use `isError: true` so the LLM knows it failed
2. **Resource with unbounded size** — cap text length; chunk large resources
3. **Prompt that hardcodes context** — pass via arguments
4. **Tool with no description** — the LLM won't know when to call it
5. **Tool with `additionalProperties: true`** — be explicit; the LLM can't guess what to send
6. **Schema with no bounds** — `limit: int` without `max` lets the LLM ask for 1B rows
7. **Resource URI collision** — each resource needs a unique URI
8. **Blocking handler in async server** — use `asyncio.to_thread` or `await` properly
