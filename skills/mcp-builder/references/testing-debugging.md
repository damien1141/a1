# Testing & Debugging Reference

Tools and techniques for verifying MCP server correctness, debugging protocol issues, and shipping with confidence.

## The MCP Inspector

The Inspector is the canonical tool for interactively testing an MCP server. It connects to your server, renders the protocol messages in a web UI, and lets you call tools/resources/prompts manually.

```bash
# Run against any server (TypeScript or Python, any transport)
npx @modelcontextprotocol/inspector node server.js
npx @modelcontextprotocol/inspector python server.py
npx @modelcontextprotocol/inspector uv run server.py
npx @modelcontextprotocol/inspector --url https://api.example.com/mcp
```

Opens a web UI at http://localhost:6274 with:
- **Tools tab** — list all tools, see schemas, call with custom args, view responses
- **Resources tab** — list resources, expand templates, read contents
- **Prompts tab** — list prompts, fill arguments, view rendered messages
- **JSON-RPC tab** — raw message log (requests, responses, notifications)
- **Notifications tab** — see server-pushed notifications in real time

### Inspector Workflow

1. **Connect** — server starts; inspector shows `initialize` result + negotiated capabilities
2. **Verify capabilities** — confirm tools/resources/prompts tabs appear as expected
3. **List tools** — check every tool has a name, description, and valid schema
4. **Call each tool with happy-path args** — verify structured response
5. **Call each tool with invalid args** — verify `INVALID_PARAMS` error
6. **Read each resource** — verify content + mimeType
7. **Render each prompt** — verify message array
8. **Check JSON-RPC tab** — no malformed messages, no stdout pollution

**Gate:** inspector shows clean protocol traffic, all capabilities work end-to-end.

## Unit Testing

### TypeScript (Vitest)

```typescript
import { test, expect, beforeEach, afterEach } from "vitest";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { createServer } from "../src/server.js";

let client: Client;
let server: any;

beforeEach(async () => {
  server = createServer();
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  client = new Client({ name: "test", version: "1.0" }, { capabilities: {} });
  await Promise.all([
    client.connect(clientTransport),
    server.connect(serverTransport),
  ]);
});

afterEach(async () => {
  await client.close();
});

test("lists expected tools", async () => {
  const { tools } = await client.listTools();
  const names = tools.map(t => t.name);
  expect(names).toContain("get_weather");
  expect(names).toContain("search_docs");
});

test("get_weather returns temperature", async () => {
  const result = await client.callTool({
    name: "get_weather",
    arguments: { location: "Berlin", units: "celsius" },
  });
  expect(result.content[0].type).toBe("text");
  const data = JSON.parse(result.content[0].text);
  expect(data).toHaveProperty("temp");
  expect(typeof data.temp).toBe("number");
});

test("get_weather rejects empty location", async () => {
  await expect(client.callTool({
    name: "get_weather",
    arguments: { location: "" },
  })).rejects.toThrow();
});

test("read config resource", async () => {
  const result = await client.readResource({ uri: "config://app" });
  expect(result.contents[0].mimeType).toBe("application/json");
  const config = JSON.parse(result.contents[0].text);
  expect(config).toHaveProperty("version");
});

test("render code_review prompt", async () => {
  const result = await client.getPrompt({
    name: "code_review",
    arguments: { language: "python", code: "print('hi')" },
  });
  expect(result.messages).toHaveLength(1);
  expect(result.messages[0].role).toBe("user");
});
```

### Python (pytest-asyncio)

```python
# tests/test_server.py
import pytest
import json
from mcp.shared.memory import create_connected_server_and_client_session
from myserver import mcp

@pytest.fixture
async def session():
    async with create_connected_server_and_client_session(mcp) as s:
        yield s

@pytest.mark.asyncio
async def test_list_tools(session):
    result = await session.list_tools()
    names = [t.name for t in result.tools]
    assert "get_weather" in names
    assert "search_docs" in names

@pytest.mark.asyncio
async def test_get_weather_happy_path(session):
    result = await session.call_tool("get_weather",
        arguments={"location": "Berlin", "units": "celsius"})
    assert result.content[0].type == "text"
    data = json.loads(result.content[0].text)
    assert "temp" in data
    assert isinstance(data["temp"], (int, float))

@pytest.mark.asyncio
async def test_get_weather_rejects_empty(session):
    with pytest.raises(Exception):
        await session.call_tool("get_weather", arguments={"location": ""})

@pytest.mark.asyncio
async def test_get_weather_rejects_bad_units(session):
    with pytest.raises(Exception):
        await session.call_tool("get_weather",
            arguments={"location": "Berlin", "units": "kelvin"})

@pytest.mark.asyncio
async def test_resource_config(session):
    result = await session.read_resource("config://app")
    assert result.contents[0].mimeType == "application/json"
    config = json.loads(result.contents[0].text)
    assert "version" in config

@pytest.mark.asyncio
async def test_prompt_code_review(session):
    result = await session.get_prompt("code_review",
        arguments={"language": "python", "code": "print('hi')"})
    assert len(result.messages) == 1
    assert result.messages[0].role == "user"
```

## Test Coverage Rules

Every tool/resource/prompt needs at minimum:
- **Happy-path test** — valid input → expected output
- **Error-path test** — invalid input → `INVALID_PARAMS` error
- **Edge-case test** — empty input, max-length input, boundary values

For tools with side effects:
- **Idempotency test** — calling twice with same args is safe
- **Cleanup test** — temporary state is cleaned up

## Integration Testing

Unit tests with `InMemoryTransport` are fast but skip the transport layer. For full confidence, test against a real subprocess:

```python
# tests/test_integration.py
import pytest
from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

@pytest.mark.asyncio
async def test_end_to_end():
    params = StdioServerParameters(command="python", args=["server.py"])
    async with stdio_client(params) as (read, write):
        async with ClientSession(read, write) as session:
            await session.initialize()
            tools = await session.list_tools()
            assert any(t.name == "get_weather" for t in tools.tools)

            result = await session.call_tool("get_weather",
                arguments={"location": "Berlin"})
            assert result.content[0].type == "text"
```

This catches transport bugs (stdout pollution, message framing) that in-memory tests miss.

## Debugging Common Issues

### "Server not responding" / Hangs on Initialize

**Cause:** stdout pollution (the most common MCP bug).

**Diagnose:** Run the server standalone and check what hits stdout:
```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | python server.py 2>/dev/null
```
If you see anything other than JSON-RPC messages on stdout, that's the bug.

**Fix:** Replace every `print()` / `console.log()` with `logging.info()` to stderr / `console.error()`.

### Tools Don't Appear in Client

**Causes:**
1. `tools/list` handler not implemented
2. `tools` capability not advertised in `initialize`
3. Handler throws on `tools/list` (check stderr logs)
4. Tool name has invalid characters (use lowercase + underscores)

**Diagnose:** Use the Inspector — if tools tab is empty, your `list_tools` isn't working. If inspector shows tools but client doesn't, the client may be filtering.

### Tool Call Returns Wrong Format

**Wrong:**
```python
return "some string"                    # missing content wrapper
return {"text": "some string"}          # missing content array
```

**Right:**
```python
return {"content": [{"type": "text", "text": "some string"}]}
```

### "Method not found" Error

**Cause:** Client called a method (e.g., `resources/subscribe`) that the server didn't advertise in `initialize.capabilities`.

**Fix:** Either implement the method and advertise the capability, or accept that the client can't use that feature.

### Async Handler Blocks

**Symptom:** Tool call hangs; other tools stop responding.

**Cause:** Sync I/O in an async handler (e.g., `requests.get()` instead of `httpx.AsyncClient`).

**Fix (Python):** Use async libraries (`httpx`, `asyncpg`, `aioboto3`) OR wrap sync calls:
```python
result = await asyncio.to_thread(requests.get, url)  # runs in threadpool
```

**Fix (TypeScript):** Use `await` for all I/O; never call sync blocking functions in handlers.

### Resource Subscription Not Working

**Causes:**
1. Server didn't advertise `resources.subscribe` capability
2. Server doesn't send `notifications/resources/updated` after changes
3. Client didn't subscribe before the change

**Fix:** Verify capability advertisement, ensure notifications are sent, test with Inspector.

### Protocol Version Mismatch

**Symptom:** Client silently falls back to old behavior; new features don't work.

**Diagnose:** Check the negotiated `protocolVersion` in the Inspector's `initialize` response. If it's older than expected, one side is out of date.

**Fix:** Update SDK on the older side; verify both sides support the same version.

## Logging

### Python

```python
import logging, sys

# Configure at startup — stderr only on stdio transport
logging.basicConfig(
    level=logging.DEBUG,  # use INFO in production
    format="%(asctime)s %(name)s %(levelname)s %(message)s",
    stream=sys.stderr,
)
logger = logging.getLogger("my-server")

# Per-request logging
@mcp.tool()
async def get_weather(location: str) -> str:
    logger.info(f"weather called: location={location}")
    try:
        result = await fetch_weather(location)
        logger.debug(f"weather result: {result}")
        return json.dumps(result)
    except Exception as e:
        logger.exception(f"weather failed for {location}")
        raise
```

### TypeScript

```typescript
// stderr only on stdio transport
const log = {
  info: (...args: unknown[]) => console.error("[INFO]", ...args),
  error: (...args: unknown[]) => console.error("[ERROR]", ...args),
  debug: (...args: unknown[]) => {
    if (process.env.DEBUG) console.error("[DEBUG]", ...args);
  },
};

// Or use the SDK's logging notification (client sees it in Inspector)
server.sendLoggingMessage({
  level: "info",
  logger: "weather-tool",
  data: `Called with location=${location}`,
});
```

## Debugging Workflow

1. **Reproduce with the Inspector** — narrow down to the smallest failing call
2. **Check stderr logs** — look for stack traces, validation errors
3. **Inspect JSON-RPC traffic** — Inspector shows raw messages; verify format
4. **Isolate the layer** — protocol bug? handler bug? external API bug?
5. **Write a failing test** — capture the bug as a regression test
6. **Fix** — one change at a time
7. **Re-run full test suite** — ensure no regression
8. **Re-test in Inspector** — confirm fix end-to-end

## Pre-Ship Verification Checklist

Before declaring an MCP server done:

- [ ] Inspector connects cleanly; no protocol errors
- [ ] All tools listed with correct schemas and descriptions
- [ ] Every tool has happy-path + error-path unit tests
- [ ] Integration test passes (subprocess + real client)
- [ ] No `console.log` / `print` to stdout (stdio transport)
- [ ] All inputs validated (Zod / Pydantic)
- [ ] Path/SQL/command inputs sanitized
- [ ] Timeouts on all external calls
- [ ] Auth on HTTP transport (if applicable)
- [ ] Rate limiting in place (if applicable)
- [ ] Logs go to stderr / `notifications/message`
- [ ] Tested against the target client (Claude Desktop, Cursor, etc.)
- [ ] README documents server capabilities and setup
- [ ] `package.json` / `pyproject.toml` pins dependencies
- [ ] `npm test` / `pytest` passes locally and in CI
