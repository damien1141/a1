# Python CLI Development

## Frameworks

| Framework | Best for | Notes |
|---|---|---|
| **click** | Most Python CLIs | Composable, decorator-based, mature |
| **typer** | Modern, type-hint-based | Built on click, easier for typed codebases |
| **argparse** | stdlib, no deps | Use only for tiny scripts |
| **rich-click** | Pretty help output | Wraps click with rich formatting |
| **textual** | TUI apps | Full-screen terminal UIs |

## typer (recommended for new projects)

Type hints drive the CLI — minimal boilerplate.

```python
#!/usr/bin/env python3
"""My CLI tool."""
from __future__ import annotations

import typer
from typing import Annotated, Optional

app = typer.Typer(
    name="mycli",
    help="My awesome CLI",
    no_args_is_help=True,
    add_completion=True,            # auto-adds `install-completions` command
)

@app.command()
def init(
    name: Annotated[str, typer.Argument(help="Project name")] = "my-app",
    template: Annotated[str, typer.Option("--template", "-t", help="Project template")] = "default",
    force: Annotated[bool, typer.Option("--force", "-f", help="Overwrite existing")] = False,
):
    """Initialize a new project."""
    typer.echo(f"Creating {name} from {template}...")
    # ...

@app.command()
def deploy(
    environment: Annotated[str, typer.Argument(help="Target environment")],
    dry_run: Annotated[bool, typer.Option("--dry-run", "-d", help="Preview only")] = False,
    config: Annotated[Optional[str], typer.Option("--config", help="Config file")] = None,
):
    """Deploy to environment."""
    if dry_run:
        typer.echo(f"Would deploy to {environment}", err=True)
        return
    deploy_env(environment, config)

# Subcommand group
config_app = typer.Typer(help="Manage configuration")
app.add_typer(config_app, name="config")

@config_app.command("get")
def config_get(key: str):
    """Get a config value."""
    typer.echo(get_config(key))

@config_app.command("set")
def config_set(key: str, value: str):
    """Set a config value."""
    set_config(key, value)

@config_app.command("list")
def config_list():
    """List all config."""
    for k, v in all_config().items():
        typer.echo(f"{k}={v}")

if __name__ == "__main__":
    app()
```

Run: `python -m mycli deploy prod --dry-run`

Typer auto-generates:
- `--help` for every command (from docstrings + type hints)
- `--install-completions` for bash/zsh/fish/PowerShell
- `--show-completion` to print the script

## click (mature, lower-level)

For finer control or if you don't want type hints driving everything.

```python
#!/usr/bin/env python3
import click

@click.group()
@click.version_option()
@click.help_option()
def cli():
    """My awesome CLI."""

@cli.command()
@click.argument("name", default="my-app")
@click.option("-t", "--template", default="default", help="Project template")
@click.option("-f", "--force", is_flag=True, help="Overwrite existing")
def init(name, template, force):
    """Initialize a new project."""
    click.echo(f"Creating {name} from {template}...")

@cli.command()
@click.argument("environment", type=click.Choice(["dev", "staging", "prod"]))
@click.option("-d", "--dry-run", is_flag=True, help="Preview only")
@click.option("--config", type=click.Path(exists=True), help="Config file")
def deploy(environment, dry_run, config):
    """Deploy to environment."""
    if dry_run:
        click.echo(f"Would deploy to {environment}", err=True)
        return
    deploy_env(environment, config)

# Subcommand group
@cli.group()
def config():
    """Manage configuration."""

@config.command("get")
@click.argument("key")
def config_get(key):
    click.echo(get_config(key))

@config.command("set")
@click.argument("key")
@click.argument("value")
def config_set(key, value):
    set_config(key, value)

if __name__ == "__main__":
    cli()
```

### click features

- **`click.Choice`** — restrict to enum values
- **`click.Path(exists=True)`** — validate file exists
- **`click.File`** — open file for you
- **`click.confirm`** — interactive yes/no
- **`click.prompt`** — interactive text input
- **`@click.pass_context`** — share state between commands
- **`click.echo`** — handles Unicode + Windows console quirks (use this over `print`)

```python
@cli.command()
@click.argument("env")
def deploy(env):
    if click.confirm(f"Deploy to {env}?", default=False):
        deploy_env(env)
```

## argparse (stdlib, no deps)

Only for tiny scripts.

```python
import argparse

parser = argparse.ArgumentParser(description="My CLI")
parser.add_argument("--version", action="version", version="%(prog)s 1.0.0")
sub = parser.add_subparsers(dest="command", required=True)

deploy_p = sub.add_parser("deploy", help="Deploy to environment")
deploy_p.add_argument("environment", choices=["dev", "staging", "prod"])
deploy_p.add_argument("--dry-run", action="store_true")

args = parser.parse_args()
if args.command == "deploy":
    deploy(args.environment, args.dry_run)
```

## Rich Output

`rich` is the Python equivalent of chalk + ora + cli-progress in one package.

```python
from rich.console import Console
from rich.progress import Progress, SpinnerColumn, BarColumn, TextColumn, TimeRemainingColumn
from rich.prompt import Prompt, Confirm
from rich.table import Table

console = Console()

# Colored output (auto-detects TTY)
console.print("[blue]ℹ[/blue] Info message")
console.print("[green]✔[/green] Success")
console.print("[yellow]⚠[/yellow] Warning")
console.print("[red]✖[/red] Error")

# Print to stderr
console.print("Error", style="red", stderr=True)

# Tables
table = Table(title="Environments")
table.add_column("Name")
table.add_column("Status")
table.add_column("Updated")
for env in envs:
    table.add_row(env.name, env.status, env.updated)
console.print(table)

# Progress bar
with Progress(
    SpinnerColumn(),
    TextColumn("[progress.description]{task.description}"),
    BarColumn(),
    TextColumn("[progress.percentage]{task.percentage:>3.0f}%"),
    TimeRemainingColumn(),
) as progress:
    task = progress.add_task("Deploying...", total=100)
    for i in range(100):
        progress.update(task, advance=1)
        time.sleep(0.05)

# Interactive prompts
name = Prompt.ask("Project name", default="my-app")
ok = Confirm.ask("Deploy to production?", default=False)
```

## Interactive Prompts (questionary / rich)

```python
import questionary

env = await questionary.select(
    "Select environment:",
    choices=["dev", "staging", "prod"],
).ask_async()

features = questionary.checkbox(
    "Select features:",
    choices=[
        {"name": "TypeScript", "checked": True},
        {"name": "ESLint", "checked": True},
        {"name": "Prettier"},
    ],
).ask()

pw = questionary.password("Enter password:").ask()
```

## Config Layers

```python
import os
from pathlib import Path
from dataclasses import dataclass, field
from typing import Optional
import tomllib  # Python 3.11+

@dataclass
class Config:
    environment: str = "development"
    timeout: int = 30
    verbose: bool = False
    api_token: Optional[str] = None

def load_config(explicit_path: Optional[Path] = None) -> Config:
    paths = [
        Path("/etc/mycli/config.toml"),
        Path.home() / ".config" / "mycli" / "config.toml",
        Path.cwd() / ".myclirc",
        explicit_path,
    ]
    merged = {}
    for p in paths:
        if p and p.exists():
            with open(p, "rb") as f:
                merged.update(tomllib.load(f))

    # Env vars override files
    for k in merged.keys():
        env_key = f"MYCLI_{k.upper()}"
        if env_key in os.environ:
            merged[k] = os.environ[env_key]

    return Config(**merged)
```

## Error Handling + SIGINT

```python
import sys
import signal
import typer

def setup_signal_handlers():
    def handler(signum, frame):
        typer.echo("\nOperation cancelled", err=True)
        sys.exit(130)
    signal.signal(signal.SIGINT, handler)

class CLIError(Exception):
    """User-facing CLI error with exit code."""
    def __init__(self, message: str, exit_code: int = 1):
        super().__init__(message)
        self.exit_code = exit_code

@app.command()
def deploy(env: str):
    """Deploy to environment."""
    try:
        if env not in ("dev", "staging", "prod"):
            raise CLIError(f"Invalid environment '{env}'. Expected: dev, staging, prod", exit_code=2)
        deploy_env(env)
    except CLIError as e:
        typer.echo(f"Error: {e}", err=True)
        sys.exit(e.exit_code)
    except FileNotFoundError as e:
        typer.echo(f"File not found: {e.filename}", err=True)
        typer.echo("Try: mycli init  OR  use --config flag", err=True)
        sys.exit(127)
    except PermissionError:
        typer.echo("Permission denied", err=True)
        typer.echo("Try: sudo mycli deploy  OR  check file permissions", err=True)
        sys.exit(77)
    except Exception as e:
        if os.environ.get("DEBUG"):
            import traceback; traceback.print_exc()
        else:
            typer.echo(f"Deploy failed: {e}", err=True)
        sys.exit(1)

if __name__ == "__main__":
    setup_signal_handlers()
    app()
```

## Packaging (pyproject.toml)

```toml
[project]
name = "mycli"
version = "1.0.0"
description = "My awesome CLI"
requires-python = ">=3.11"
dependencies = [
    "typer>=0.12.0",
    "rich>=13.0.0",
    "questionary>=2.0.0",
]

[project.optional-dependencies]
dev = ["pytest>=8.0", "pytest-cov"]

[project.scripts]
mycli = "mycli.__main__:app"

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"
```

```bash
# Install in dev mode
pip install -e .

# Build wheel
python -m build

# Publish to PyPI
twine upload dist/*

# Install via pipx (recommended for end users)
pipx install mycli
```

## Testing CLIs

Use `typer.testing.CliRunner` or `click.testing.CliRunner`:

```python
# tests/test_cli.py
from typer.testing import CliRunner
from mycli import app

runner = CliRunner()

def test_version():
    result = runner.invoke(app, ["--version"])
    assert result.exit_code == 0
    assert "1.0.0" in result.stdout

def test_deploy_dry_run():
    result = runner.invoke(app, ["deploy", "prod", "--dry-run"])
    assert result.exit_code == 0
    assert "Would deploy to prod" in result.stderr  # typer writes echo(err=True) to stderr

def test_invalid_env_exits_2():
    result = runner.invoke(app, ["deploy", "invalid"])
    assert result.exit_code == 2

def test_unknown_command():
    result = runner.invoke(app, ["bogus"])
    assert result.exit_code != 0
```

For end-to-end tests using the actual installed binary:

```python
import subprocess

def test_e2e_help():
    result = subprocess.run(["mycli", "--help"], capture_output=True, text=True)
    assert result.returncode == 0
    assert "USAGE" in result.stdout
```

## Shell Completions

Typer auto-installs completions via `mycli --install-completions`:

```bash
# Install for current shell
mycli --install-completion bash
mycli --install-completion zsh
mycli --install-completion fish
mycli --install-completion powershell

# Show completion script (manual install)
mycli --show-completion bash >> ~/.bashrc
```

For click, use `click-shell-completion`:

```python
from click_shell_completion import install_completion
install_completion(cli)
```

## Startup Time

Python CLIs are notoriously slow (300-1000ms) due to module loading. Tips:

1. **Lazy imports** — only load heavy deps inside commands:
   ```python
   @app.command()
   def deploy(env: str):
       import heavy_dep  # only loads when deploy runs
       heavy_dep.do_thing()
   ```
2. **Use `__slots__`** in dataclasses (faster attribute access)
3. **Avoid `import *`** — pulls in entire module trees
4. **Move optional deps to extras** — `pip install mycli[aws]`
5. **Don't put heavy logic at module top level**
6. **Consider `python -X importtime mycli --version`** to find slow imports

For ultra-fast CLIs, consider rewriting in Go or Rust.

## Distribution

| Channel | Command | Users |
|---|---|---|
| PyPI | `pip install mycli` | Python users |
| pipx | `pipx install mycli` | Recommended (isolated env) |
| uv | `uv tool install mycli` | Fast (Rust-based) |
| Homebrew | `brew install mycli` | macOS users |
| AUR | `yay install mycli` | Arch users |
| standalone binary | PyInstaller / Nuitka | No Python needed |

### Standalone binary (PyInstaller)

```bash
pip install pyinstaller
pyinstaller --onefile --name mycli src/mycli/__main__.py
# dist/mycli is a single executable
```

## Common Pitfalls

1. **`print()` instead of `typer.echo()` / `click.echo()`** — Unicode issues on Windows console.
2. **No `if __name__ == "__main__":`** — module side effects fire on import.
3. **Top-level imports of heavy deps** — slow startup for every command.
4. **`sys.exit(0)` on error** — breaks shell scripting.
5. **Forgetting `err=True`** — errors go to stdout, break piping.
6. **No `requires-python`** in pyproject — users on old Python get cryptic errors.
7. **No `[project.scripts]`** — `python -m mycli` works but `mycli` doesn't.
8. **`input()` for prompts** — no completion, no arrow keys, no mask. Use `questionary`.
9. **Forgetting `signal.signal(SIGINT, ...)`** — Ctrl+C dumps ugly traceback.
10. **`argparse` for new projects** — use typer or click; argparse is fine but verbose.
