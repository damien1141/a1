# Standard Library Essentials (Python 3.11+)

## pathlib — paths, not strings

```python
from pathlib import Path


def load_config(app_dir: Path) -> dict[str, str]:
    cfg_path = app_dir / "config" / "app.conf"
    if not cfg_path.is_file():
        return {}
    return {
        k.strip(): v.strip()
        for line in cfg_path.read_text().splitlines()
        if "=" in line
        for k, _, v in [line.partition("=")]
    }
```

- `Path(__file__).parent` for module-relative paths
- `Path.home()`, `Path.cwd()` for system paths
- `read_text()` / `read_bytes()` / `write_text()` — no `with open()` needed for small files
- `mkdir(parents=True, exist_ok=True)` instead of `os.makedirs(..., exist_ok=True)`

## dataclasses — value types

```python
from dataclasses import dataclass, field


@dataclass(slots=True)
class Server:
    host: str
    port: int = 8080
    tags: list[str] = field(default_factory=list)
```

- `slots=True` (3.10+) — faster attribute access, less memory, no `__dict__`
- `frozen=True` — immutable, hashable, can be a dict key
- `kw_only=True` (3.10+) — force keyword args, avoids the "mutable default ordering" trap
- `field(default_factory=...)` for mutable defaults; **never** `[]`/`{}`/`set()`
- `__post_init__` for validation; with `frozen=True` use `object.__setattr__(self, "x", v)`

## functools

```python
from functools import cached_property, singledispatch, lru_cache, partial


class Dataset:
    @cached_property
    def size(self) -> int:
        return sum(1 for _ in self._iter())  # computed once, cached


@singledispatch
def serialize(obj) -> str:
    raise TypeError(f"unsupported: {type(obj)}")

@serialize.register
def _(obj: int) -> str:
    return str(obj)


@lru_cache(maxsize=256)
def fib(n: int) -> int:
    return n if n < 2 else fib(n - 1) + fib(n - 2)
```

- `cached_property` for cheap memoization on instances
- `singledispatch` for type-based dispatch (alternative to overloading)
- `lru_cache` for pure functions; pass `typed=True` if `1` and `1.0` should be distinct keys

## itertools & collections.abc

```python
from itertools import batched, chain, groupby
from collections.abc import Iterable, Iterator, Sequence


# batched (3.12+) — chunk an iterable
for chunk in batched(range(10), 3):
    print(chunk)  # (0,1,2), (3,4,5), (6,7,8), (9,)


# groupby requires sorted input
for key, group in groupby(sorted(items, key=by_category), key=by_category):
    process(key, list(group))
```

Use `collections.abc` (not `typing`) for runtime checks and base classes: `Iterable`, `Mapping`, `Sequence`, `Awaitable`, `AsyncIterator`.

## contextlib

```python
from contextlib import contextmanager, ExitStack, suppress


@contextmanager
def open_many(paths):
    with ExitStack() as stack:
        files = [stack.enter_context(open(p)) for p in paths]
        yield files


# Suppress specific exceptions without bare except
with suppress(FileNotFoundError):
    Path("cache.tmp").unlink()
```

- `ExitStack` for dynamic numbers of context managers
- `suppress(*exc)` replaces `try/except: pass`
- `asynccontextmanager` for async equivalents

## tomllib — read TOML (3.11+)

```python
import tomllib
from pathlib import Path


def load_pyproject(path: Path = Path("pyproject.toml")) -> dict:
    with path.open("rb") as f:
        return tomllib.load(f)
```

Reads only (no write). For writing, use `tomli_w` or rewrite via a templating approach.

## logging — structured, not print

```python
import logging

logger = logging.getLogger(__name__)


def process(item: dict) -> None:
    logger.info("processing", extra={"item_id": item["id"]})
```

- One logger per module: `logging.getLogger(__name__)`
- Never `print` in a library
- Configure handlers at the program entry point, not in libraries
- For structured output, use `structlog` or `logging` with a JSON formatter

## typing utilities

```python
from typing import override, final, Self, Literal, assert_never


class Base:
    def run(self) -> None: ...


class Sub(Base):
    @override
    def run(self) -> None:
        ...


@final
class Const:  # cannot be subclassed
    ...
```

## subprocess — use run, never call

```python
import subprocess


def git_commit(msg: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", "commit", "-m", msg],
        check=True,
        capture_output=True,
        text=True,
    )
```

- `check=True` to raise on non-zero exit
- `text=True` (formerly `universal_newlines=True`) for str output
- Never `shell=True` with untrusted input — command injection

## exceptions — exception groups (3.11+)

```python
try:
    await fetch_all(urls)
except* NetworkError as eg:
    for e in eg.exceptions:
        log.warning("net error: %s", e)
except* ValueError:
    log.error("bad input")
```

Use `except*` (with the star) to handle `ExceptionGroup` raised by `TaskGroup`.
