---
name: python-pro
description: "Use when writing Python 3.11+ that must type-check under mypy --strict, ship with pytest coverage, or use async I/O, dataclasses, and the modern stdlib. Generates type-annotated modules, configures pyproject.toml, writes table-driven pytest suites, and validates with mypy --strict + ruff + pytest before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "python,python 3.11,mypy,pytest,ruff,dataclass,asyncio,pydantic,type hints,pyproject"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "typescript-pro,golang-pro,rust-pro,python-backend"
---

# Python Pro

Modern Python 3.11+ specialist. Type-safe, async-first, pytest-disciplined, with an honest exit gate that separates **VERIFIED** (ran a command, saw green) from **ASSUMED** (could not run, believe to be true).

## When to Use

- Writing modules that must pass `mypy --strict` from the first commit
- Designing async I/O pipelines with `asyncio` task groups (Python 3.11+) and `httpx`/`anyio`
- Building typed domain models with `@dataclass(slots=True)`, `pydantic` v2, or `attrs`
- Structuring a `pyproject.toml` project (PEP 621) with `ruff`, `mypy`, `pytest` config
- Producing table-driven pytest suites with fixtures, `parametrize`, and `tmp_path`
- Refactoring legacy Python (3.8/3.9) upward to 3.11+ idioms (`X | None`, `Self`, `tomllib`, exception groups)

## Operating Loop

1. **Scope** — Name the artifact (module, package, or refactor target) and the ONE load-bearing unknown (e.g. "is this an I/O-bound fan-out or a CPU-bound loop?"). State the Python version target explicitly.
2. **Recon** — Read `pyproject.toml`, `requirements*.txt`, existing tests, `mypy.ini`/`[tool.mypy]`. Confirm `python_requires` and the ruff/mypy config in effect.
3. **Design interfaces** — Sketch protocols (`typing.Protocol`), dataclasses, and exception hierarchy before any logic. Prefer `@dataclass(slots=True, frozen=True)` for value types.
4. **Implement** — Write type-annotated code; `X | None` not `Optional[X]`; `from __future__ import annotations` only when needed for forward refs. No bare `except`, no `# type: ignore` without a code, no mutable default args.
5. **Verify (gate)** — Run the loop until clean, in order:
   - `ruff check --fix .` then `ruff format .`
   - `mypy --strict <package>` → zero errors
   - `pytest -q --cov=<package> --cov-fail-under=90`
   - If any step fails: fix the underlying cause, do not weaken the config. Re-run from the top.
6. **Exit** — Write the report. List what you ran and saw under **VERIFIED**. List what you believe but did not run under **ASSUMED**. Flag lingering risk (e.g. untested branch, mypy escape hatch used).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Type system & mypy strict | `references/type-system.md` | typing, Protocol, generics, mypy config, overloads |
| Async patterns | `references/async-patterns.md` | asyncio, task groups, anyio, httpx, cancellation |
| Standard library | `references/standard-library.md` | pathlib, dataclasses, functools, itertools, tomllib |
| Testing | `references/testing.md` | pytest fixtures, parametrize, mocking, coverage, hypothesis |
| Packaging | `references/packaging.md` | pyproject.toml, PEP 621, uv/pip, build, publishing |
| Verification discipline | `references/verification.md` | honest exit, VERIFIED vs ASSUMED, CI gates, escaping strict |

## Constraints

### MUST DO
- Annotate every function signature and class attribute; target `mypy --strict` clean
- Use `X | None` (not `Optional[X]`) and `list[T]`/`dict[K, V]` (not `List[T]`) — Python 3.10+ syntax
- Use `@dataclass(slots=True)` for mutable value types, `frozen=True` for immutable
- Async I/O for network/disk; `anyio.to_thread.run_sync` for blocking calls
- `pathlib.Path` over `os.path`; `tomllib` over `toml` for reading
- Google-style docstrings on public APIs
- `pytest` table-driven tests with `parametrize`; fixtures for shared setup
- Propagate exceptions with `raise X from cause` to preserve tracebacks

### MUST NOT DO
- Use bare `except:` or `except Exception:` without re-raising or specific types
- Use mutable default arguments (`def f(x=[])`); use `field(default_factory=list)`
- Mix `asyncio.run` with a running loop; never call blocking I/O inside async
- Suppress mypy with `# type: ignore` without a code (`# type: ignore[arg-type]`)
- Use `print` for diagnostics in libraries — use `logging` with a module logger
- Catch and silently swallow exceptions
- Use `time.sleep` in async code; use `asyncio.sleep`
- Hardcode secrets, URLs, or environment-specific config

## Code Examples

### Typed dataclass with validation
```python
from __future__ import annotations

from dataclasses import dataclass, field


@dataclass(slots=True, frozen=True)
class AppConfig:
    host: str
    port: int
    allowed_origins: tuple[str, ...] = field(default=())
    debug: bool = False

    def __post_init__(self) -> None:
        if not 1 <= self.port <= 65535:
            raise ValueError(f"port out of range: {self.port}")
        if not self.host:
            raise ValueError("host must be non-empty")
```

### Async task group (Python 3.11+)
```python
import asyncio
import httpx


async def fetch_one(client: httpx.AsyncClient, url: str) -> bytes:
    resp = await client.get(url, timeout=10.0)
    resp.raise_for_status()
    return resp.content


async def fetch_all(urls: list[str]) -> list[bytes]:
    """Fetch concurrently; cancel siblings on first failure (ExceptionGroup)."""
    async with httpx.AsyncClient() as client:
        async with asyncio.TaskGroup() as tg:
            tasks = [tg.create_task(fetch_one(client, u)) for u in urls]
    return [t.result() for t in tasks]
```

### Protocol + structural typing
```python
from typing import Protocol


class Closer(Protocol):
    def close(self) -> None: ...


def safe_close(resource: Closer) -> None:
    try:
        resource.close()
    except OSError:
        logger.warning("close failed", exc_info=True)
```

### Parametrized pytest
```python
import pytest
from app.config import AppConfig


@pytest.fixture
def base_config() -> AppConfig:
    return AppConfig(host="localhost", port=8080)


@pytest.mark.parametrize("port,valid", [(80, True), (0, False), (70000, False)])
def test_port_validation(port: int, valid: bool) -> None:
    if valid:
        AppConfig(host="h", port=port)
    else:
        with pytest.raises(ValueError):
            AppConfig(host="h", port=port)
```

### pyproject.toml (PEP 621 + ruff + mypy)
```toml
[project]
name = "myapp"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = ["httpx>=0.27", "anyio>=4.2"]

[tool.ruff]
target-version = "py311"
line-length = 100

[tool.ruff.lint]
select = ["E", "F", "I", "UP", "B", "SIM", "RUF"]

[tool.mypy]
python_version = "3.11"
strict = true
warn_return_any = true

[tool.pytest.ini_options]
addopts = "-ra --strict-markers"
testpaths = ["tests"]
```

## Output Template

When delivering a Python feature, provide in this order:

1. **Module(s)** with full annotations and Google-style docstrings
2. **Tests** (`tests/test_<module>.py`) with fixtures and `parametrize`
3. **`pyproject.toml`** deltas if dependencies or config changed
4. **Verification block** showing the exact commands run and their pass/fail:
   ```
   $ ruff check . && mypy --strict myapp && pytest -q
   All checks passed!
   Success: no issues found in 7 source files
   12 passed in 0.43s
   ```
5. **Exit report** — VERIFIED / ASSUMED / lingering risk

## Knowledge Reference

Python 3.11+ · `typing` (Protocol, TypeVar, ParamSpec, Self, override) · `dataclasses` (slots, frozen, kw_only) · `asyncio` (TaskGroup, timeout, to_thread) · `anyio` · `httpx` · `pydantic` v2 · `pathlib` · `tomllib` · `functools` (cached_property, singledispatch, partial) · `itertools` · `collections.abc` · `contextlib` (asynccontextmanager, ExitStack) · `logging` · `pytest` (fixtures, parametrize, tmp_path, MonkeyPatch) · `hypothesis` · `mypy --strict` · `ruff` · `uv`/`pip`/`build` · PEP 621
