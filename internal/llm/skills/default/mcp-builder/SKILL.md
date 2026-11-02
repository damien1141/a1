---
name: mcp-builder
description: Designs, builds, debugs, and ships Model Context Protocol servers and clients that connect AI systems (Claude, GPT, agents) to external tools, data sources, and prompts. Implements JSON-RPC 2.0 protocol, defines tools/resources/prompts with Zod or Pydantic schemas, configures stdio/SSE/HTTP transports, handles capability negotiation, sampling, and roots, and enforces permission scopes and auth. Use when building MCP servers (TypeScript or Python SDKs), integrating external APIs/databases/filesystems as tools for an LLM, or debugging protocol-compliance issues.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: ai
  triggers: MCP, Model Context Protocol, MCP server, MCP client, Claude integration, JSON-RPC, tool calling, resources, prompts, stdio, SSE, HTTP transport, capability negotiation, sampling, roots, Zod, Pydantic, FastMCP
  role: specialist
  scope: implementation
  output-format: code
  related-skills: typescript-pro, python-pro, api-design, testing-master
---

# MCP Builder

Model Context Protocol (MCP) is the open JSON-RPC 2.0 standard for connecting AI systems to external tools, data, and prompts. A **server** exposes capabilities (tools, resources, prompts); a **client** (Claude Desktop, an agent runtime, your custom app) discovers and invokes them. This skill is the canonical reference for building both, against the **2024-11-05 / 2025-06-18** spec revisions.

## When to Use

- Building a server that exposes an API, DB, filesystem, or internal tool to an LLM
- Integrating Claude Desktop / Cursor / your agent with custom tools
- Exposing application state, config, or docs as resources an LLM can read
- Defining reusable prompt templates that the client renders
- Debugging protocol-compliance issues with an existing server
- Adding auth, rate-limiting, or permission scopes to an MCP server
- Building a custom MCP client that connects to multiple servers

## Operating Loop

1. **Scope** — Define the minimum useful capability set. List every tool, resource, prompt with a one-sentence purpose. **Gate:** each capability has a clear owner, schema, and at least one happy-path test case.
2. **Choose SDK + transport** — TypeScript SDK (`@modelcontextprotocol/sdk`) or Python SDK (`mcp` / FastMCP). Transport: `stdio` for local tools, `HTTP+SSE` (or streamable HTTP in 2025-06-18+) for remote. **Gate:** transport matches deployment topology; secrets plan written.
3. **Define schemas** — Every tool input is a Zod/Pydantic schema with descriptions and bounds. Every resource has a URI scheme. Every prompt has named arguments. **Gate:** `npx @modelcontextprotocol/inspector` shows all capabilities with valid schemas.
4. **Implement handlers** — Tool handlers validate inputs (never trust client args), execute, return structured content. Resource handlers resolve URIs. Prompt handlers return message arrays. **Gate:** every handler has a happy-path test and an error-path test.
5. **Harden** — Add auth (for HTTP), rate-limiting, input sanitization (path traversal, SQL injection), output size caps, and timeouts. **Gate:** security checklist passed (see `references/security-and-transports.md`).
6. **Test + debug** — Run inspector; run unit tests with mock transports; run integration tests against a real client. **Gate:** protocol compliance verified; no `console.log` to stdout (stdio transport).
7. **Deploy** — Package, ship, register in client config (`~/.claude/claude_desktop_config.json` or equivalent). **Gate:** client can list tools, call a tool, read a resource, get a prompt end-to-end.

## Server Anatomy

```
MCP Server
├── Tools      (functions the LLM can call)         → tools/list, tools/call
├── Resources  (data the LLM can read)              → resources/list, resources/read
├── Prompts    (templates the client can render)    → prompts/list, prompts/get
└── Transports (stdio | HTTP+SSE | streamable HTTP)
```

Plus optional server-side features: **sampling** (server asks client's LLM to generate), **roots** (client exposes filesystem roots to server), **notifications** (server pushes updates), **capability negotiation** (initialize handshake).

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| Protocol spec | `references/protocol.md` | JSON-RPC 2.0 message format, lifecycle, capability negotiation, version compatibility |
| TypeScript SDK | `references/typescript-sdk.md` | Building servers/clients in Node.js with `@modelcontextprotocol/sdk` + Zod |
| Python SDK | `references/python-sdk.md` | Building servers/clients in Python with `mcp` / FastMCP + Pydantic |
| Tools, resources, prompts | `references/tools-resources-prompts.md` | Defining capabilities, schema patterns, response formats, common tool patterns |
| Transports & security | `references/security-and-transports.md` | stdio/HTTP/SSE, auth, permission scopes, rate-limiting, injection defense |
| Testing & debugging | `references/testing-debugging.md` | Inspector, unit tests, mock transports, common protocol bugs |

## Code Examples

### TypeScript — minimal server with tool + resource

```typescript
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";

const server = new McpServer({ name: "my-server", version: "1.0.0" });

server.tool(
  "get_weather",
  "Fetch current weather for a location",
  {
    location: z.string().min(1).describe("City name or lat,lng"),
    units: z.enum(["celsius", "fahrenheit"]).default("celsius"),
  },
  async ({ location, units }) => {
    const data = await fetchWeather(location, units);
    return { content: [{ type: "text", text: JSON.stringify(data) }] };
  }
);

server.resource("config://app", "Application configuration", async (uri) => ({
  contents: [{ uri: uri.href, mimeType: "application/json",
               text: JSON.stringify(getConfig()) }],
}));

await server.connect(new StdioServerTransport());
```

### Python — FastMCP equivalent

```python
from mcp.server.fastmcp import FastMCP
from pydantic import BaseModel, Field
import json

mcp = FastMCP("my-server")

class WeatherArgs(BaseModel):
    location: str = Field(..., min_length=1, description="City or lat,lng")
    units: str = Field("celsius", pattern="^(celsius|fahrenheit)$")

@mcp.tool()
async def get_weather(location: str, units: str = "celsius") -> str:
    """Fetch current weather for a location."""
    data = await fetch_weather(location, units)
    return json.dumps(data)

@mcp.resource("config://app")
async def app_config() -> str:
    """Application configuration."""
    return json.dumps(get_config())

if __name__ == "__main__":
    mcp.run()  # defaults to stdio
```

### Register with Claude Desktop

```jsonc
// ~/Library/Application Support/Claude/claude_desktop_config.json (macOS)
// or ~/.config/Claude/claude_desktop_config.json (Linux)
{
  "mcpServers": {
    "my-server": {
      "command": "node",
      "args": ["/abs/path/to/server.js"],
      "env": { "API_KEY": "..." }   // secrets via env, never in args
    },
    "remote-server": {
      "url": "https://api.example.com/mcp",
      "headers": { "Authorization": "Bearer ..." }
    }
  }
}
```

### Verify with the Inspector

```bash
npx @modelcontextprotocol/inspector node server.js
# Opens a web UI: list tools, call them, inspect JSON-RPC traffic
```

**Gate:** inspector shows all tools with correct schemas; calling each tool returns well-formed content (or a structured error).

## Constraints

### MUST DO
- Validate every tool input with a schema (Zod / Pydantic) — never trust client args
- Use `console.error` / `logging` (Python) for logs; **stdout is reserved for protocol** on stdio transport
- Implement proper JSON-RPC 2.0 error responses (codes -32602 invalid params, -32603 internal, -32001..-32099 custom)
- Negotiate protocol version in `initialize`; verify client compatibility
- Add timeouts to every external call (30s default; shorter for user-facing tools)
- Make tool calls idempotent where possible (accept `idempotency_key`)
- Sanitize path inputs (resolve + check against allowed root) for filesystem tools
- Sanitize SQL inputs (parameterized queries only) for DB tools
- For HTTP transport: add auth, rate-limiting, and request size caps
- Return structured content (`{type:"text",text:...}` or `{type:"image",...}` or `{type:"resource",...}`) — never raw strings
- Declare capabilities honestly in initialize (don't claim `resources` if you don't implement `resources/list`)
- Handle `notifications/initialized` before serving requests
- Test happy path AND error path for every tool

### MUST NOT DO
- `console.log` to stdout on stdio transport (corrupts the protocol stream)
- Hardcode secrets in server code or client config args — use env vars
- Block the event loop in async handlers (use `asyncio.to_thread` for sync I/O in Python)
- Mix sync and async code in handlers
- Skip input validation "because the LLM sends good args" — it won't
- Return unstructured errors (plain `throw new Error("...")` loses context) — wrap in `McpError` / `McpError`
- Expose filesystem without path validation (path traversal is the #1 MCP security bug)
- Expose DB tools without parameterized queries (SQL injection)
- Ignore protocol version (servers and clients must agree; mismatches cause silent failures)
- Cache resources indefinitely without a `notifications/resources/updated` mechanism
- Ship without testing through the Inspector first
- Allow unbounded output size (cap text length; chunk large resources)

## Output Template

When delivering MCP work, provide:

1. **Capability manifest** — table of tools/resources/prompts with name, purpose, schema sketch
2. **Server implementation** — full source with Zod/Pydantic schemas, handlers, error handling
3. **Transport + auth config** — stdio command OR HTTP endpoint + auth scheme
4. **Client registration** — JSON snippet for `claude_desktop_config.json` (or equivalent)
5. **Test plan** — inspector walkthrough, unit tests (happy + error path per tool), integration test
6. **Security notes** — input validation, path/SQL sanitization, rate-limiting, secret handling
7. **Verification** — commands to run inspector + unit tests; expected output

## Knowledge Reference

Protocol: JSON-RPC 2.0 (request/response/notifications), initialize handshake, capability negotiation, protocol versions (`2024-11-05`, `2025-06-18` streamable HTTP). Primitives: tools (name, description, inputSchema, handler), resources (uri, name, mimeType, contents), prompts (name, arguments, messages). Transports: stdio (newline-delimited JSON), HTTP+SSE (POST + SSE stream), streamable HTTP (2025-06-18+). Server-side features: sampling (server→client LLM call), roots (client→server filesystem roots), notifications (`resources/updated`, `tools/list_changed`). Security: input validation (Zod/Pydantic), path traversal defense, SQL parameterization, auth (Bearer, OAuth 2.1 for remote), rate-limiting, permission scopes. Tooling: `@modelcontextprotocol/inspector`, `@modelcontextprotocol/create-server`, MCP SDK TypeScript + Python, Claude Desktop / Cursor / Cline as clients.
