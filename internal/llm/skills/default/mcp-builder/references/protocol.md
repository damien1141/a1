# MCP Protocol Reference

MCP is JSON-RPC 2.0 over a transport (stdio / HTTP+SSE / streamable HTTP). This reference covers message format, lifecycle, capability negotiation, and version compatibility. Spec revisions referenced: **2024-11-05** (initial public spec) and **2025-06-18** (streamable HTTP, OAuth 2.1).

## Message Types

### Request / Response

```jsonc
// Request
{ "jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": {} }

// Success
{ "jsonrpc": "2.0", "id": 1, "result": { "tools": [ ... ] } }

// Error
{ "jsonrpc": "2.0", "id": 1, "error": {
    "code": -32602, "message": "Invalid params",
    "data": { "details": "location is required" }
}}
```

### Notification (no response expected)

```jsonc
{ "jsonrpc": "2.0", "method": "notifications/resources/updated",
  "params": { "uri": "file:///project/data.json" } }
```

## Connection Lifecycle

```
1. Client opens transport (stdio process, HTTP connection)
2. Client → initialize { protocolVersion, capabilities, clientInfo }
3. Server → initialize result { protocolVersion, capabilities, serverInfo }
4. Client → notifications/initialized
5. Normal operation (requests, notifications, server-pushed notifications)
6. Client → shutdown (or transport closes)
```

### Initialize Handshake

```jsonc
// Client → Server
{ "jsonrpc": "2.0", "id": 1, "method": "initialize",
  "params": {
    "protocolVersion": "2024-11-05",
    "capabilities": {
      "roots": { "listChanged": true },
      "sampling": {}
    },
    "clientInfo": { "name": "claude-desktop", "version": "1.0.0" }
  }}

// Server → Client
{ "jsonrpc": "2.0", "id": 1, "result": {
    "protocolVersion": "2024-11-05",
    "capabilities": {
      "resources": { "subscribe": true, "listChanged": true },
      "tools": { "listChanged": true },
      "prompts": { "listChanged": true }
    },
    "serverInfo": { "name": "my-server", "version": "1.0.0" }
  }}

// Client → Server (no response)
{ "jsonrpc": "2.0", "method": "notifications/initialized" }
```

**Version compatibility:** the negotiated version is the *lower* of the two `protocolVersion` values the parties support. A 2025-06-18 server talking to a 2024-11-05 client falls back to 2024-11-05 behavior (no streamable HTTP, no OAuth 2.1). Servers must declare honestly which version they implement.

## Capability Negotiation

Capabilities advertised in `initialize` are binding for the session. Don't advertise a capability you don't implement.

| Capability | Direction | Means |
|---|---|---|
| `tools` | server | Server implements `tools/list`, `tools/call` |
| `resources` | server (+ `subscribe`, `listChanged`) | Server implements `resources/list`, `resources/read` |
| `prompts` | server (+ `listChanged`) | Server implements `prompts/list`, `prompts/get` |
| `logging` | server | Server sends `notifications/message` log events |
| `roots` | client (+ `listChanged`) | Client can provide filesystem roots to server |
| `sampling` | client | Client's LLM is available for server-initiated generation |
| `experimental.*` | either | Non-standard extensions |

## Core Methods

### Tools

```jsonc
tools/list → { tools: Tool[] }
tools/call { name: string, arguments: object } → {
  content: [{ type: "text"|"image"|"resource", ... }],
  isError?: boolean
}
notifications/tools/list_changed → {}   // server tells client to re-list
```

### Resources

```jsonc
resources/list { cursor?: string } → { resources: Resource[], nextCursor?: string }
resources/read { uri: string } → {
  contents: [{ uri: string, mimeType?: string, text?: string, blob?: string }]
}
resources/templates/list → { resourceTemplates: ResourceTemplate[] }   // dynamic URIs
resources/subscribe { uri: string } → {}      // requires server capability
resources/unsubscribe { uri: string } → {}
notifications/resources/list_changed → {}
notifications/resources/updated { uri: string } → {}   // server pushes change
```

### Prompts

```jsonc
prompts/list → { prompts: Prompt[] }
prompts/get { name: string, arguments?: object } → {
  messages: [{ role: "user"|"assistant", content: { type: "text"|"image"|"resource", ... }}],
  description?: string
}
notifications/prompts/list_changed → {}
```

### Sampling (server asks client's LLM)

```jsonc
sampling/createMessage {
  messages: [{ role, content }],
  modelPreferences?: { hints, costPriority, speedPriority, intelligencePriority },
  systemPrompt?: string,
  maxTokens: int,
  temperature?: number,
  stopSequences?: string[]
} → { role, content, model, stopReason }
```

The server uses this when it needs LLM reasoning to complete a tool call (e.g., summarize a doc before returning it). The **client** decides which model to use and may prompt the user for approval. Servers must not assume a specific model.

### Roots (client exposes filesystem roots)

```jsonc
roots/list → { roots: [{ uri: "file:///path", name?: string }] }
notifications/roots/list_changed → {}
```

Servers use roots to scope filesystem operations. Always resolve user-provided paths against a root before accessing.

### Ping (keepalive)

```jsonc
ping → {}    // both directions; clients use to detect dead connections
```

### Logging

```jsonc
// Server sends log entries as notifications
{ "jsonrpc": "2.0", "method": "notifications/message",
  "params": { "level": "info"|"warning"|"error", "logger": "mytool", "data": "..." }}
```

## Error Codes

```typescript
// JSON-RPC 2.0 standard
PARSE_ERROR: -32700,        // malformed JSON
INVALID_REQUEST: -32600,    // not a valid request object
METHOD_NOT_FOUND: -32601,   // server doesn't implement this method
INVALID_PARAMS: -32602,     // schema validation failed
INTERNAL_ERROR: -32603,     // unexpected server error

// MCP-specific (server-defined in -32000 to -32099 range)
RESOURCE_NOT_FOUND: -32001,
TOOL_EXECUTION_ERROR: -32002,
UNAUTHORIZED: -32003,
RATE_LIMIT_EXCEEDED: -32004,
CAPABILITY_NOT_SUPPORTED: -32005
```

Always include a human-readable `message` and a `data` field with details. Clients surface these to users or logs.

## Transports

### stdio (default for local tools)

- Server reads JSON-RPC messages from stdin, writes to stdout
- Each message is a single line of JSON (newline-delimited)
- **stderr** is for logs (not protocol)
- Used by Claude Desktop, Cursor for local servers
- Launch: client spawns server process, communicates over its stdio

### HTTP + SSE (legacy remote; 2024-11-05)

- Client POSTs JSON-RPC requests to a single endpoint
- Server responds via Server-Sent Events stream
- Two connections: POST for requests, GET for SSE
- Being replaced by streamable HTTP in 2025-06-18

### Streamable HTTP (2025-06-18+)

- Single endpoint, POST for requests, optional SSE for streaming responses
- Stateless-friendly; better for serverless deployments
- Supports `Mcp-Session-Id` header for session continuity
- Backward compatible: a 2024-11-05 client still works against a 2025-06-18 server in fallback mode

## Version Compatibility Matrix

| Server → Client | 2024-11-05 client | 2025-06-18 client |
|---|---|---|
| **2024-11-05 server** | Full compatibility | Full (client falls back) |
| **2025-06-18 server** | 2024-11-05 behavior (no streamable HTTP, no OAuth 2.1) | Full |

Servers should declare the latest version they fully support. Clients should support the latest plus one back.

## Best Practices

1. **Validate params with JSON Schema** at the protocol layer (Zod / Pydantic generate these for you)
2. **Return structured errors** with helpful messages — clients may surface them to users
3. **Check protocol version** in `initialize`; fail fast on incompatible clients
4. **Implement timeouts** (30s for tool calls; 5s for `ping`)
5. **Log to stderr / `notifications/message`** — never stdout on stdio transport
6. **Design tools statelessly** when possible; session state complicates load-balancing
7. **Make tool calls idempotent** — accept an `idempotency_key` for write operations
8. **Use notifications for real-time updates** — don't make clients poll `resources/list`
9. **Capability honesty** — only advertise what you implement; clients will call advertised methods
10. **Graceful shutdown** — handle `SIGTERM` / `SIGINT`, flush pending responses, close cleanly

## Common Protocol Bugs

| Symptom | Cause | Fix |
|---|---|---|
| Client says "server not responding" | stdout pollution (console.log) | Use stderr / `console.error` |
| Tools don't appear in client | Missing `tools/list` handler or capability not advertised | Implement both |
| "Method not found" | Server didn't implement the method the client called | Check capability advertisement matches implementation |
| Initialize hangs | Server didn't reply to `initialize` or client didn't send `notifications/initialized` | Both sides must complete the handshake |
| Resources don't update | Server didn't send `notifications/resources/updated` | Push notification after resource changes |
| Tool call returns wrong format | Returned `{text: "..."}` instead of `{content: [{type:"text", text:"..."}]}` | Use the content array structure |
| Async handler blocks | Sync I/O in async function | Use `asyncio.to_thread` (Python) or `await` properly (TS) |
