# Testing with pytest

## Layout

```
project/
├── src/myapp/
│   └── config.py
├── tests/
│   ├── conftest.py          # shared fixtures
│   ├── unit/
│   │   └── test_config.py
│   └── integration/
│       └── test_api.py
└── pyproject.toml
```

Use the `src/` layout — prevents accidental imports from cwd, matches installed package.

## Fixtures — the dependency injection of pytest

```python
import pytest
from pathlib import Path
from myapp.config import AppConfig


@pytest.fixture
def tmp_config_file(tmp_path: Path) -> Path:
    cfg = tmp_path / "config.toml"
    cfg.write_text('host = "localhost"\nport = 8080\n')
    return cfg


def test_loads(tmp_config_file: Path) -> None:
    cfg = AppConfig.from_file(tmp_config_file)
    assert cfg.host == "localhost"
    assert cfg.port == 8080
```

Fixture scope: `function` (default), `class`, `module`, `package`, `session`. Use `session` for expensive resources (DB connection), and yield cleanup:

```python
@pytest.fixture(scope="session")
def db():
    engine = create_engine(TEST_DSN)
    yield engine
    engine.dispose()
```

## parametrize — table-driven tests

```python
import pytest


@pytest.mark.parametrize(
    "port,valid",
    [
        (1, True),
        (80, True),
        (65535, True),
        (0, False),
        (-1, False),
        (65536, False),
    ],
)
def test_port_validation(port: int, valid: bool) -> None:
    if valid:
        AppConfig(host="h", port=port)
    else:
        with pytest.raises(ValueError):
            AppConfig(host="h", port=port)
```

## conftest.py — shared fixtures and hooks

```python
# tests/conftest.py
import pytest
from myapp.app import create_app


@pytest.fixture
def app():
    return create_app(testing=True)


@pytest.fixture
def client(app):
    return app.test_client()
```

Fixtures in `conftest.py` are available to all tests in the same dir tree without import.

## Mocking — prefer dependency injection

The cleanest test isolates the SUT by injecting fakes. When you must patch:

```python
import pytest
from myapp.fetcher import fetch
from unittest.mock import AsyncMock


@pytest.mark.asyncio
async def test_fetch(monkeypatch):
    fake_http = AsyncMock(return_value=bytes([1, 2, 3]))
    monkeypatch.setattr("myapp.fetcher.http_get", fake_http)
    result = await fetch("http://x")
    assert result == bytes([1, 2, 3])
    fake_http.assert_awaited_once()
```

- Prefer `monkeypatch` over `unittest.mock.patch` for module attributes and env vars
- Use `AsyncMock` for async functions
- Don't mock what you don't own — wrap third-party calls behind your own interface, then fake the interface

## Coverage

```toml
[tool.pytest.ini_options]
addopts = "--cov=myapp --cov-report=term-missing --cov-fail-under=90"

[tool.coverage.run]
branch = true
source = ["myapp"]

[tool.coverage.report]
exclude_lines = [
    "pragma: no cover",
    "if TYPE_CHECKING:",
    "raise NotImplementedError",
    "\\.\\.\\.",
]
```

Coverage is a floor, not a ceiling. 90% with branch coverage catches most missing paths.

## Hypothesis — property-based testing

```python
from hypothesis import given, strategies as st


@given(st.integers(min_value=1, max_value=65535))
def test_port_in_range(port: int):
    AppConfig(host="h", port=port)  # should not raise
```

Use Hypothesis for parsers, round-trip properties, and edge discovery. It finds bugs `parametrize` misses.

## Async tests

```python
import pytest


@pytest.mark.asyncio
async def test_fetch_all():
    result = await fetch_all(["a", "b"])
    assert len(result) == 2
```

Configure in `pyproject.toml`:

```toml
[tool.pytest.ini_options]
asyncio_mode = "auto"
```

## Markers and strict markers

```toml
[tool.pytest.ini_options]
markers = [
    "slow: long-running tests",
    "integration: needs external services",
]
addopts = "--strict-markers"
```

Run a subset: `pytest -m "not slow"`.

## Common anti-patterns

- `assert True` placeholders — delete or implement
- `try/except: pass` in tests — let it fail loudly
- Testing implementation details (private methods) — test public behavior
- One giant test function — split by assertion; `parametrize` the rest
- `time.sleep` in tests — use `event.wait(timeout=...)` or `freezegun`
- Shared mutable state across tests — fixtures should be function-scoped by default
- `print` for debugging — use `--pdb` or `pytest -s`

## Verification gate

```bash
pytest -q --cov=myapp --cov-fail-under=90 --cov-report=term-missing
```

A failing test means a failing build. Never commit with `xfail` to silence a real failure.
