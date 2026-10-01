# Type System & mypy --strict

## Target: `mypy --strict` clean

`mypy --strict` turns on every reasonable check. Do not weaken it; fix the code instead. The flags it enables (subset): `disallow_untyped_defs`, `disallow_incomplete_defs`, `check_untyped_defs`, `disallow_untyped_decorators`, `no_implicit_optional`, `warn_return_any`, `warn_unused_ignores`, `warn_redundant_casts`, `strict_equality`.

## Modern syntax (3.10+)

```python
# Use builtin generics and | unions everywhere
def parse(items: list[dict[str, int]]) -> dict[str, list[int]] | None: ...

# None union shorthand
def find(key: str) -> str | None: ...

# Type aliases are explicit in 3.12
type Matrix = list[list[float]]

# Older code: use TypeAlias
from typing import TypeAlias
Vector: TypeAlias = list[float]
```

## Protocol — structural typing

```python
from typing import Protocol, runtime_checkable


@runtime_checkable
class Closeable(Protocol):
    def close(self) -> None: ...


class FileHandle:  # no inheritance, just structural conformance
    def close(self) -> None: ...


def shutdown(c: Closeable) -> None:
    c.close()

shutdown(FileHandle())  # OK
```

Use `Protocol` for duck-typed boundaries; use `ABC` only when you need shared behavior + construction.

## Generics: TypeVar, ParamSpec, TypeVarTuple

```python
from typing import TypeVar, ParamSpec, Callable, Concatenate

T = TypeVar("T")
P = ParamSpec("P")


def first(xs: list[T]) -> T:
    return xs[0]


# Decorator that preserves the wrapped signature
def log(fn: Callable[P, T]) -> Callable[P, T]:
    def inner(*args: P.args, **kwargs: P.kwargs) -> T:
        print(f"calling {fn.__name__}")
        return fn(*args, **kwargs)
    return inner
```

## Self type for fluent / builder APIs

```python
from typing import Self


class Builder:
    def __init__(self) -> None:
        self._parts: list[str] = []

    def with_part(self, p: str) -> Self:
        self._parts.append(p)
        return self

    def build(self) -> str:
        return "-".join(self._parts)
```

## Overloads for precise call signatures

```python
from typing import overload


@overload
def parse(x: int) -> int: ...
@overload
def parse(x: str) -> str: ...
def parse(x: int | str) -> int | str:
    return x  # implementation must cover all cases
```

## Literal types and exhaustiveness

```python
from typing import Literal, assert_never


Mode = Literal["r", "w", "a"]


def open_mode(mode: Mode) -> None:
    if mode == "r":
        ...
    elif mode == "w":
        ...
    elif mode == "a":
        ...
    else:
        assert_never(mode)  # mypy enforces exhaustiveness
```

## TypedDict for dict shapes

```python
from typing import TypedDict


class UserPayload(TypedDict):
    id: int
    name: str
    email: str | None
```

For total=False (optional keys) use `total=False` on the class or `Required[]`/`NotRequired[]` per-key (3.11+).

## Exception design

```python
class AppError(Exception):
    """Base for all app errors."""

class ConfigError(AppError): ...
class NetworkError(AppError): ...

# Preserve cause
raise ConfigError(f"bad port {port}") from ValueError(port)
```

## `# type: ignore` discipline

- Never bare `# type: ignore`; always include the code: `# type: ignore[attr-defined]`
- Add a one-line comment explaining why
- `warn_unused_ignores = true` will flag stale ignores — fix or remove them
- If you cannot fix the type without an ignore, prefer narrowing with `assert isinstance(...)` or `cast`

## Common mypy errors and fixes

| Error | Fix |
|---|---|
| `Function is missing a return type annotation` | Add `-> T` |
| `Incompatible return value type (got "None", expected "T")` | Missing branch; add return or change to `T \| None` |
| `Argument 1 has incompatible type "X \| None"` | Narrow with `if x is not None:` |
| `Unsupported operand types for + ("int" and "None")` | Same — narrow first |
| `Missing positional argument` | Check call site vs signature |
| `Item "None" of "Optional[X]" has no attribute "Y"` | Narrow or use `assert x is not None` |

## Config block (pyproject.toml)

```toml
[tool.mypy]
python_version = "3.11"
strict = true
warn_return_any = true
warn_unused_ignores = true
warn_redundant_casts = true
strict_equality = true
# Per-package overrides for untyped deps
[[tool.mypy.overrides]]
module = ["legacy.*"]
ignore_missing_imports = true
```

## Anti-patterns

- `Any` — replace with `object` + narrowing, or a `Protocol`
- `Optional[X]` / `Union[X, Y]` in new code — use `X | None` / `X | Y`
- `Dict[str, Any]` for known shapes — use `TypedDict`
- `cast(T, x)` to silence mypy — narrow with `isinstance` or fix the source
- Bare `# type: ignore` — always include a code and a reason
