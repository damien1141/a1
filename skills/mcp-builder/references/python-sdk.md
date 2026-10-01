# Python SDK Reference

Two flavors:
- **`mcp.server.Server`** — low-level, decorator-based
- **`mcp.server.fastmcp.FastMCP`** — high-level, Flask-like, recommended for new servers

Pairs with **Pydantic v2** for schema validation.

## Setup

```bash
mkdir my-server && cd my-server
python -m venv .venv && source .venv/bin/activate
pip install mcp pydantic httpx
# Or use uv:
uv init my-server && cd my-server && uv add mcp pydantic httpx
```

## FastMCP (recommended)

```python
from mcp.server.fastmcp import FastMCP
from pydantic import BaseModel, Field
import json

mcp = FastMCP("my-server")

# Tool — type hints + docstring drive the schema
class WeatherArgs(BaseModel):
    location: str = Field(..., min_length=1, description="City or lat,lng")
    units: str = Field("celsius", pattern="^(celsius|fahrenheit)$")

@mcp.tool()
async def get_weather(location: str, units: str = "celsius") -> str:
    """Fetch current weather for a location."""
    data = await fetch_weather(location, units)
    return json.dumps(data)

# Resource (static URI)
@mcp.resource("config://app")
async def app_config() -> str:
    """Application configuration."""
    return json.dumps(get_config())

# Resource template (dynamic URI)
@mcp.resource("user://{user_id}/profile")
async def user_profile(user_id: str) -> str:
    """Get user profile by ID."""
    return json.dumps(await get_user(user_id))

# Prompt
@mcp.prompt()
def code_review(language: str, code: str) -> str:
    """Generate code review comments."""
    return f"Review this {language} code:\n\n{code}"

if __name__ == "__main__":
    mcp.run()  # defaults to stdio; use mcp.run(transport="sse") for HTTP+SSE
```

### Type Hints → Schema

FastMCP generates the JSON schema from your function signature. Use Annotated + Field for constraints:

```python
from typing import Annotated, Literal
from pydantic import Field

@mcp.tool()
async def search(
    query: Annotated[str, Field(min_length=1, max_length=500, description="Search query")],
    max_results: Annotated[int, Field(ge=1, le=50, default=5)],
    category: Literal["docs", "tickets", "code"] = "docs",
) -> str:
    """Search the knowledge base."""
    ...
```

## Low-Level API: `Server` + decorators

Use when you need fine control over the protocol layer.

```python
from mcp.server import Server
from mcp.server.stdio import stdio_server
from mcp.types import Tool, TextContent, Resource, Prompt, PromptMessage
import asyncio, json

app = Server("my-server")

@app.list_tools()
async def list_tools() -> list[Tool]:
    return [Tool(
        name="get_weather",
        description="Get current weather for a location",
        inputSchema={
            "type": "object",
            "properties": {
                "location": {"type": "string", "minLength": 1},
                "units": {"type": "string", "enum": ["celsius","fahrenheit"]},
            },
            "required": ["location"],
        },
    )]

@app.call_tool()
async def call_tool(name: str, arguments: dict) -> list[TextContent]:
    if name == "get_weather":
        args = WeatherArgs(**arguments)  # Pydantic validation
        data = await fetch_weather(args.location, args.units)
        return [TextContent(type="text", text=json.dumps(data))]
    raise ValueError(f"Unknown tool: {name}")

@app.list_resources()
async def list_resources() -> list[Resource]:
    return [Resource(uri="config://app", name="App Config", mimeType="application/json")]

@app.read_resource()
async def read_resource(uri: str) -> str:
    if uri == "config://app":
        return json.dumps(get_config())
    raise ValueError(f"Unknown resource: {uri}")

async def main():
    async with stdio_server() as (read, write):
        await app.run(read, write, app.create_initialization_options())

if __name__ == "__main__":
    asyncio.run(main())
```

## Validation with Pydantic v2

```python
from pydantic import BaseModel, Field, field_validator
from typing import Literal

class SearchArgs(BaseModel):
    query: str = Field(..., min_length=1, max_length=500, description="Search query")
    max_results: int = Field(default=5, ge=1, le=50)
    category: Literal["docs", "tickets", "code"] = "docs"

    @field_validator("query")
    @classmethod
    def strip_query(cls, v: str) -> str:
        return v.strip()

class DBQueryArgs(BaseModel):
    table: str = Field(..., pattern=r"^[a-zA-Z_][a-zA-Z0-9_]*$")  # SQL-safe
    limit: int = Field(default=100, ge=1, le=1000)
    offset: int = Field(default=0, ge=0)
```

Always validate at the handler boundary — don't trust the client.

## Error Handling

```python
from mcp.shared.exceptions import McpError
from mcp.types import INTERNAL_ERROR, INVALID_PARAMS

@mcp.tool()
async def risky_op(arg: str) -> str:
    try:
        result = await do_something(arg)
        return json.dumps(result)
    except ValueError as e:
        # Validation / expected failure — return as tool error
        # (FastMCP wraps this as isError=true in the response)
        raise ValueError(f"Invalid input: {e}")
    except Exception as e:
        # Unexpected — protocol error
        raise McpError(INTERNAL_ERROR, f"Tool failed: {e}")
```

## Logging (stderr only on stdio transport)

```python
import logging, sys

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    stream=sys.stderr,   # CRITICAL: never stdout
)
logger = logging.getLogger("my-server")

# Or use the SDK's logging notification (client sees it)
import mcp.server.stdio as stdio
await app.request_context.session.send_log_message(
    level="info", logger="weather-tool", data=f"Fetched {location}"
)
```

## Client Implementation

```python
from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client
import asyncio

async def main():
    server_params = StdioServerParameters(
        command="python",
        args=["server.py"],
        env={"API_KEY": "..."},  # pass secrets via env
    )
    async with stdio_client(server_params) as (read, write):
        async with ClientSession(read, write) as session:
            await session.initialize()
            tools = await session.list_tools()
            print([t.name for t in tools.tools])

            result = await session.call_tool("get_weather",
                arguments={"location": "Berlin"})
            print(result.content)

asyncio.run(main())
```

### HTTP/SSE Client

```python
from mcp.client.sse import sse_client
# 2025-06-18 streamable HTTP:
from mcp.client.streamable_http import streamablehttp_client

async with streamablehttp_client("https://api.example.com/mcp",
        headers={"Authorization": f"Bearer {token}"}) as (read, write, _):
    async with ClientSession(read, write) as session:
        await session.initialize()
        # ... use session
```

## Notifications (Server-Pushed)

```python
from mcp.server.fastmcp import Context

@mcp.tool()
async def update_config(ctx: Context, key: str, value: str) -> str:
    save_config(key, value)
    # Tell clients the resource changed
    await ctx.session.send_resource_updated(uri="config://app")
    # Or: notify tool list changed (if you dynamically register tools)
    # await ctx.session.send_tool_list_changed()
    return "Config updated"
```

## Context Managers and Cleanup

```python
from contextlib import asynccontextmanager

@asynccontextmanager
async def db_connection():
    db = await connect_to_database()
    try:
        yield db
    finally:
        await db.close()

@mcp.tool()
async def query_db(query: str) -> str:
    async with db_connection() as db:
        rows = await db.fetch(query)
        return json.dumps([dict(r) for r in rows])
```

## Testing

```python
# tests/test_weather.py
import pytest
from mcp.shared.memory import create_connected_server_and_client_session
from myserver import mcp

@pytest.mark.asyncio
async def test_get_weather():
    async with create_connected_server_and_client_session(mcp) as session:
        result = await session.call_tool("get_weather",
            arguments={"location": "Berlin"})
        assert result.content[0].type == "text"
        data = json.loads(result.content[0].text)
        assert "temp" in data

@pytest.mark.asyncio
async def test_invalid_input():
    async with create_connected_server_and_client_session(mcp) as session:
        with pytest.raises(Exception):  # validation error
            await session.call_tool("get_weather", arguments={"location": ""})
```

## Project Layout

```
my-server/
├── pyproject.toml
├── src/
│   └── my_server/
│       ├── __init__.py
│       ├── server.py        # FastMCP instance + run()
│       ├── tools/
│       │   ├── weather.py
│       │   └── search.py
│       ├── resources/
│       │   └── config.py
│       └── prompts/
│           └── code_review.py
└── tests/
    └── test_weather.py
```

## Best Practices

1. **FastMCP** for new servers; low-level only when you need fine control
2. **Pydantic v2** for every tool input — type hints drive the schema
3. **`logging` to stderr** — never `print()` to stdout on stdio transport
4. **`asyncio.to_thread`** for sync I/O in async handlers (don't block the event loop)
5. **Context managers** for resources that need cleanup (DB, HTTP clients)
6. **`create_connected_server_and_client_session`** for in-memory unit tests
7. **Pin SDK version** — the spec is evolving
8. **Handle `SIGTERM`** for clean shutdown
9. **Type hints everywhere** — they become the JSON schema the LLM sees
10. **Docstrings matter** — they become the tool description
