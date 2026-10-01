# TypeScript SDK Reference

`@modelcontextprotocol/sdk` is the official TypeScript SDK for MCP servers and clients. Pairs with **Zod** for schema validation.

## Setup

```bash
mkdir my-server && cd my-server
npm init -y
npm install @modelcontextprotocol/sdk zod
# Or scaffold:
npx @modelcontextprotocol/create-server my-server
```

## High-Level API: `McpServer` (recommended)

The high-level API handles protocol plumbing; you just register tools/resources/prompts.

```typescript
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";

const server = new McpServer({ name: "my-server", version: "1.0.0" });

// Tool with Zod schema — description is part of the prompt to the LLM
server.tool(
  "get_weather",                                          // name
  "Fetch current weather for a location",                  // description
  {                                                        // Zod schema
    location: z.string().min(1).describe("City or lat,lng"),
    units: z.enum(["celsius", "fahrenheit"]).default("celsius"),
  },
  async ({ location, units }) => {                         // handler
    const data = await fetchWeather(location, units);
    return { content: [{ type: "text", text: JSON.stringify(data) }] };
  }
);

// Resource (static URI)
server.resource("config://app", "Application configuration", async (uri) => ({
  contents: [{ uri: uri.href, mimeType: "application/json",
               text: JSON.stringify(getConfig()) }],
}));

// Resource template (dynamic URI)
server.resource(
  "user-profile",
  "user://{userId}/profile",
  "Get user profile by ID",
  { userId: z.string().min(1) },
  async (uri, { userId }) => ({
    contents: [{ uri: uri.href, mimeType: "application/json",
                 text: JSON.stringify(await getUser(userId)) }],
  })
);

// Prompt
server.prompt(
  "code-review",
  "Generate code review comments",
  { language: z.string(), code: z.string() },
  async ({ language, code }) => ({
    messages: [{
      role: "user",
      content: { type: "text",
                 text: `Review this ${language} code:\n\n${code}` },
    }],
  })
);

await server.connect(new StdioServerTransport());
```

## Low-Level API: `Server` + `setRequestHandler`

Use when you need fine control (custom capability flags, raw message access, non-standard methods).

```typescript
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
  ListToolsRequestSchema, CallToolRequestSchema,
  ListResourcesRequestSchema, ReadResourceRequestSchema,
  ListPromptsRequestSchema, GetPromptRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";

const server = new Server(
  { name: "my-server", version: "1.0.0" },
  { capabilities: { resources: {}, tools: {}, prompts: {} } }
);

server.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: [{
    name: "get_weather",
    description: "Get current weather",
    inputSchema: {
      type: "object",
      properties: {
        location: { type: "string", minLength: 1 },
        units: { type: "string", enum: ["celsius","fahrenheit"], default: "celsius" },
      },
      required: ["location"],
    },
  }],
}));

server.setRequestHandler(CallToolRequestSchema, async (request) => {
  if (request.params.name !== "get_weather") {
    throw new Error(`Unknown tool: ${request.params.name}`);
  }
  const args = WeatherSchema.parse(request.params.arguments);  // Zod validation
  const data = await fetchWeather(args.location, args.units);
  return { content: [{ type: "text", text: JSON.stringify(data) }] };
});

await server.connect(new StdioServerTransport());
```

Prefer the high-level API unless you have a specific reason — the low-level API is verbose and error-prone.

## Schema Validation with Zod

```typescript
import { z } from "zod";

// Tool input schema — descriptions are critical (they go to the LLM)
const SearchArgs = z.object({
  query: z.string().min(1).max(500).describe("Search query"),
  max_results: z.number().int().min(1).max(50).default(5).describe("Max results"),
  filters: z.record(z.string(), z.string()).default({}).describe("Metadata filters"),
});

// In the handler, Zod parses and validates
async ({ query, max_results, filters }) => { /* ... */ }
```

### Common Schema Patterns

```typescript
// Enum (constrains output)
z.enum(["billing", "bug", "howto", "account", "other"])

// Optional with default
z.string().default("celsius")

// Nullable
z.string().nullable()

// Array with bounds
z.array(z.object({ id: z.string(), action: z.enum(["update","delete"]) }))
  .min(1).max(100)

// Union
z.union([z.string(), z.number()])

// Refined
z.string().refine(s => s.startsWith("sk_"), "Must be a secret key")
```

## Error Handling

```typescript
import { McpError, ErrorCode } from "@modelcontextprotocol/sdk/types.js";

server.tool("risky_op", { ... }, async (args) => {
  try {
    const result = await doSomething(args);
    return { content: [{ type: "text", text: JSON.stringify(result) }] };
  } catch (error) {
    if (error instanceof ValidationError) {
      // Client error — return as a tool error (not protocol error)
      return {
        content: [{ type: "text", text: `Validation failed: ${error.message}` }],
        isError: true,    // signals tool-level error to the LLM
      };
    }
    // Protocol-level error
    throw new McpError(ErrorCode.InternalError, `Tool failed: ${error.message}`);
  }
});
```

**Two error modes:**
- `isError: true` in the result → the LLM sees the error text and can retry/recover
- `throw McpError` → the client sees a protocol error; the LLM may not see what went wrong

Use `isError` for expected failures (validation, not-found); use `McpError` for protocol/infra bugs.

## Client Implementation

```typescript
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";

const client = new Client(
  { name: "my-client", version: "1.0.0" },
  { capabilities: {} }
);

const transport = new StdioClientTransport({
  command: "node",
  args: ["./server.js"],
  env: { API_KEY: process.env.API_KEY },  // pass secrets via env
});

await client.connect(transport);

const { tools } = await client.listTools();
const result = await client.callTool({
  name: "get_weather",
  arguments: { location: "Berlin", units: "celsius" },
});
console.log(result.content);
```

### Multiple Servers (client aggregation)

Most agent runtimes connect to multiple MCP servers and expose all their tools to the LLM. Use one `Client` per server; aggregate tool lists; route `callTool` by name.

## HTTP/SSE Transport (Remote Server)

```typescript
import { SSEClientTransport } from "@modelcontextprotocol/sdk/client/sse.js";
// 2025-06-18 streamable HTTP:
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

const transport = new StreamableHTTPClientTransport(
  new URL("https://api.example.com/mcp"),
  { requestInit: { headers: { Authorization: `Bearer ${token}` } } }
);
await client.connect(transport);
```

## Logging (Don't Pollute stdout)

```typescript
// WRONG on stdio transport — breaks the protocol
console.log("Server starting...");

// RIGHT — stderr is for logs
console.error("Server starting...");

// BETTER — use the SDK's logging notification (client can display)
server.sendLoggingMessage({
  level: "info",
  logger: "weather-tool",
  data: `Fetched weather for ${location}`,
});
```

## Notifications (Server-Pushed)

```typescript
// After updating a resource, notify subscribed clients
server.sendResourceUpdated({ uri: "config://app" });

// After tool list changes (e.g., dynamic tool registration)
server.sendToolListChanged();

// After prompt list changes
server.sendPromptListChanged();
```

## Testing

```typescript
import { test, expect } from "vitest";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { createServer } from "./server";

test("get_weather returns temperature", async () => {
  const server = createServer();
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();

  const client = new Client({ name: "test", version: "1.0" }, { capabilities: {} });
  await Promise.all([
    client.connect(clientTransport),
    server.connect(serverTransport),
  ]);

  const result = await client.callTool({
    name: "get_weather",
    arguments: { location: "Berlin" },
  });
  expect(result.content[0].type).toBe("text");
  const data = JSON.parse(result.content[0].text);
  expect(data).toHaveProperty("temp");
});
```

`InMemoryTransport` lets you test the full protocol stack without spawning a subprocess.

## Project Layout

```
my-server/
├── package.json
├── tsconfig.json
├── src/
│   ├── server.ts          # main entry
│   ├── tools/
│   │   ├── weather.ts     # one file per tool group
│   │   └── search.ts
│   ├── resources/
│   │   └── config.ts
│   └── prompts/
│       └── code-review.ts
└── tests/
    └── weather.test.ts
```

## Best Practices

1. **High-level API** for new servers; low-level only when necessary
2. **Zod schemas** for every tool input; descriptions are part of the LLM's prompt
3. **`console.error` only** on stdio transport; never `console.log`
4. **`isError: true`** for expected tool failures; `McpError` for protocol errors
5. **`InMemoryTransport`** for unit tests; subprocess for integration tests
6. **One file per tool group** for maintainability
7. **Pin SDK version** — the spec is evolving; minor versions can break behavior
8. **Handle `SIGTERM`** for clean shutdown when the client closes the transport
