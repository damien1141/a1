# Async Patterns (Python 3.11+)

## When async helps

Async is for **I/O-bound** fan-out: many sockets/files/DB calls in flight at once. For CPU-bound work, use processes (`concurrent.futures.ProcessPoolExecutor`) or `anyio.to_thread.run_sync`.

## The unit: coroutine + TaskGroup

```python
import asyncio


async def fetch(url: str) -> str:
    await asyncio.sleep(0.1)  # I/O stand-in
    return f"data:{url}"


async def main() -> None:
    urls = ["a", "b", "c"]
    async with asyncio.TaskGroup() as tg:
        tasks = [tg.create_task(fetch(u)) for u in urls]
    results = [t.result() for t in tasks]
    print(results)


asyncio.run(main())
```

`TaskGroup` (3.11+) cancels siblings on the first exception and raises an `ExceptionGroup` containing all failures. This replaces `asyncio.gather(...)` for new code where you want fail-fast; `gather(return_exceptions=True)` still wins when you want all results regardless.

## Timeouts

```python
async with asyncio.timeout(2.5):
    await slow_call()
# raises TimeoutError on expiry
```

Prefer `asyncio.timeout()` over `asyncio.wait_for()` (deprecated-style usage) for new code.

## Cancellation

```python
async def worker(stop: asyncio.Event) -> None:
    try:
        while not stop.is_set():
            await do_chunk()
    except asyncio.CancelledError:
        # cleanup, then re-raise unless you have a strong reason
        await flush()
        raise
```

Rules:
- Always re-raise `CancelledError` after cleanup unless you are the top-level supervisor
- Never `except Exception` without `CancelledError` first — `CancelledError` is `BaseException` in 3.8+
- Use `asyncio.Event` for cooperative shutdown signals

## Structured concurrency with anyio

`anyio` gives a portable backend (asyncio or trio) and stronger cancellation semantics:

```python
import anyio


async def main() -> None:
    async with anyio.create_task_group() as tg:
        tg.start_soon(fetch, "a")
        tg.start_soon(fetch, "b")
    # both done or cancelled together
```

Use `anyio.to_thread.run_sync(blocking_fn, *args)` to offload blocking calls without stalling the loop.

## HTTP with httpx

```python
import httpx


async def fetch_all(urls: list[str]) -> list[bytes]:
    async with httpx.AsyncClient(timeout=10.0) as client:
        responses = await asyncio.gather(*[client.get(u) for u in urls])
        for r in responses:
            r.raise_for_status()
        return [r.content for r in responses]
```

- Reuse a single `AsyncClient` (connection pooling)
- Pass `timeout=` explicitly; the default is 5s
- Use `client.stream()` for large bodies

## Async context managers and async iterators

```python
from contextlib import asynccontextmanager


@asynccontextmanager
async def db_conn(url: str):
    conn = await connect(url)
    try:
        yield conn
    finally:
        await conn.close()


async def events(source):
    async for item in source:
        yield transform(item)
```

## Bounded concurrency

```python
sem = asyncio.Semaphore(10)


async def fetch_one(client: httpx.AsyncClient, url: str) -> bytes:
    async with sem:
        r = await client.get(url)
        r.raise_for_status()
        return r.content
```

Or use `anyio.CapacityLimiter` for the portable version.

## Mixing sync and async

- **Blocking call in async?** Wrap with `await anyio.to_thread.run_sync(blocking_fn)`
- **Async call in sync?** Don't. Restructure, or use `asyncio.run()` at the entry point only
- **Never** call `asyncio.run()` inside a running loop
- **Never** use `time.sleep` in async — use `await asyncio.sleep`

## Anti-patterns

```python
# WRONG — blocks the event loop
async def fetch(url):
    time.sleep(2)
    return requests.get(url).text

# WRONG — fire-and-forget task, lost errors
async def handler(req):
    asyncio.create_task(process(req))  # error swallowed, ref dropped

# RIGHT
async def handler(req):
    task = asyncio.create_task(process(req))
    task.add_done_callback(handle_errors)
    background_tasks.add(task)
    task.add_done_callback(background_tasks.discard)
```

## Testing async

Use `pytest-asyncio` (or `anyio`'s pytest plugin):

```python
import pytest


@pytest.mark.asyncio
async def test_fetch_all():
    result = await fetch_all(["a", "b"])
    assert len(result) == 2
```

For time-sensitive tests, use a virtual clock: `anyio.move_on_after(0)` or `freezegun` for `datetime`.

## Verification

- `mypy --strict` catches missing `await` and `Coroutine` mismatches
- `pytest -q` with `pytest-asyncio` runs the coroutines
- Run under `PYTHONASYNCIODEBUG=1` to surface un-awaited coroutines and slow callbacks
- For race conditions, use `pytest-asyncio` + `hypothesis` stateful testing
