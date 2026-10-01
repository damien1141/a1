# Packaging & Project Layout (PEP 621)

## The src/ layout

```
myapp/
├── pyproject.toml
├── README.md
├── LICENSE
├── src/
│   └── myapp/
│       ├── __init__.py
│       ├── config.py
│       └── ...
└── tests/
    ├── conftest.py
    └── test_*.py
```

Why `src/`? Without it, `pytest` from the project root imports the local package, not the installed one. With `src/`, you test what users install.

## pyproject.toml — PEP 621 metadata

```toml
[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[project]
name = "myapp"
version = "0.1.0"
description = "Demo application"
readme = "README.md"
license = { text = "MIT" }
requires-python = ">=3.11"
authors = [{ name = "Author", email = "a@b.c" }]
dependencies = [
    "httpx>=0.27,<1.0",
    "anyio>=4.2",
]

[project.optional-dependencies]
dev = [
    "pytest>=8.0",
    "pytest-asyncio>=0.23",
    "pytest-cov>=5.0",
    "mypy>=1.10",
    "ruff>=0.5",
    "hypothesis>=6.100",
]

[project.scripts]
myapp = "myapp.cli:main"

[tool.hatch.build.targets.wheel]
packages = ["src/myapp"]
```

## Dependency pinning

- **Library** (published on PyPI): loose bounds (`>=X,<Y`). Don't pin exact versions.
- **Application** (deployed service): exact pins via a lockfile (`uv lock`, `pip-compile`). Reproducibility over flexibility.

## uv — modern package manager

`uv` (Astral) is the fast modern replacement for pip + venv + pip-tools:

```bash
uv venv
uv pip install -e ".[dev]"
uv run pytest
uv lock      # generate cross-platform lockfile
uv sync      # install from lockfile
```

Falls back to `pip` if `uv` is unavailable.

## Build & publish

```bash
# Build distributions
uv build                    # or: python -m build

# Publish
uv publish --token $PYPI_TOKEN
```

Build artifacts land in `dist/`: a `.whl` and a `.tar.gz`.

## Versioning

- Use `hatch-vcs` or `setuptools-scm` to derive version from git tags
- Or hardcode in `pyproject.toml` for simple projects
- Follow SemVer: MAJOR.MINOR.PATCH

## Tooling config (single source of truth)

Put ruff, mypy, and pytest config in `pyproject.toml`, not in separate files. Single file = easier audits.

```toml
[tool.ruff]
target-version = "py311"
line-length = 100

[tool.ruff.lint]
select = ["E", "F", "I", "UP", "B", "SIM", "RUF", "C4"]

[tool.mypy]
python_version = "3.11"
strict = true

[tool.pytest.ini_options]
testpaths = ["tests"]
addopts = "-ra --strict-markers"
```

## Entry points

```toml
[project.scripts]
myapp = "myapp.cli:main"

[project.entry-points."myapp.plugins"]
builtin = "myapp.plugins.builtin"
```

`myapp` becomes a console command. Plugins are discoverable via `importlib.metadata.entry_points`.

## Module structure

- `__init__.py` re-exports the public API; keep it thin
- Subpackages by domain (`myapp.users`, `myapp.billing`), not by type (`myapp.models`, `myapp.views`)
- `__all__` lists the public names; everything else is private
- `if TYPE_CHECKING:` imports for circular-dependency breaking

## CI sketch

```yaml
# .github/workflows/ci.yml
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: astral-sh/setup-uv@v3
      - run: uv sync --frozen
      - run: uv run ruff check .
      - run: uv run ruff format --check .
      - run: uv run mypy --strict src
      - run: uv run pytest -q --cov
```

## Common pitfalls

- Forgetting `requires-python = ">=3.11"` — users on 3.9 install and crash
- Mixing `setup.py` + `pyproject.toml` — pick one (pyproject)
- Committing `*.egg-info` — add to `.gitignore`
- Not pinning dev tools — flaky CI when a new ruff release adds rules
- Importing `myapp` from repo root during tests — use `src/` layout
