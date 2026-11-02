---
name: cli-developer
description: Designs and ships production-grade CLI tools across Node.js (commander/yargs/oclif), Python (click/typer), Go (cobra/bubbletea), and Rust (clap). Use for argument parsing, subcommand hierarchy, config layers, TTY detection, SIGINT handling, shell completions, progress/spinners, cross-platform packaging, and startup-time optimization. Produces benchmarked CLIs (<50ms startup), complete with `--help` text, exit codes, and shell completion scripts.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: devops
  triggers: CLI, command-line, terminal app, argument parsing, shell completion, interactive prompt, progress bar, commander, yargs, oclif, click, typer, argparse, rich, cobra, viper, bubbletea, clap, TTY, SIGINT, exit code, cross-platform packaging
  role: specialist
  scope: implementation
  output-format: code
  related-skills: rust-pro, golang-pro, python-pro, typescript-pro
---

# CLI Developer

## When to Use

- Building a CLI tool in Node.js, Python, Go, or Rust
- Designing subcommand hierarchy, flags, config layers, and shell completions
- Adding interactive prompts, progress bars, spinners, TTY-aware output
- Implementing graceful SIGINT handling and meaningful exit codes
- Packaging for cross-platform distribution (npm, PyPI, Homebrew, Scoop, AUR, GitHub Releases)
- Optimizing startup time (target: <50ms for simple CLIs)

## Operating Loop

1. **Analyze UX** — list every command and its expected `--help` output before writing code. Validate by walking through user workflows.
2. **Design commands** — pick subcommand hierarchy, flag names (consistent across the CLI), positional args, config-file format. Confirm no existing signatures break.
3. **Implement** — pick the framework per language (see Reference Guide). Wire up commands, then run `<cli> --help` and `<cli> --version` to verify output.
4. **Polish UX** — TTY detection (no colors when piped), SIGINT handler (clean exit code 130), error messages with context and suggested fix, progress indicators, shell completions.
5. **Test & benchmark** — cross-platform smoke tests; startup-time benchmark (`hyperfine '<cli> --version'` target <50ms); completion scripts tested in bash/zsh/fish/PowerShell.

### Validation Gates

| Gate | Command | When |
|---|---|---|
| Help renders | `<cli> --help` and `<cli> <subcmd> --help` | Per command added |
| Version renders | `<cli> --version` | Per release |
| TTY detection | `<cli> foo \| cat` should produce no colors | Pre-merge |
| SIGINT handling | `Ctrl+C` mid-run → exit code 130, clean stderr | Pre-merge |
| Exit codes | success=0, error=1, misuse=2, not-found=127, SIGINT=130 | Per command |
| Startup time | `hyperfine --warmup 3 '<cli> --version'` < 50ms | Pre-release |
| Completions | `<cli> completions bash` outputs valid script; tested in shell | Pre-release |
| Cross-platform | Run on Linux + macOS + Windows for every release | Pre-release |
| Lint | `eslint` (Node), `ruff` (Python), `golangci-lint` (Go), `clippy` (Rust) | Pre-commit |

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| Subcommands, flags, config layers, exit codes, plugins, errors | `references/cli-design.md` | Designing CLI structure |
| Node.js (commander, yargs, oclif, inquirer, chalk, ora) | `references/node-cli.md` | Building Node CLI |
| Python (click, typer, argparse, rich) | `references/python-cli.md` | Building Python CLI |
| Go (cobra, viper, bubbletea) | `references/go-cli.md` | Building Go CLI |
| Rust (clap, ratatui, indicatif) | `references/rust-cli.md` | Building Rust CLI |
| UX patterns: TTY, SIGINT, colors, progress, completions, packaging | `references/ux-patterns.md` | Polish + distribution |

## Constraints

### MUST DO
- Keep startup time under 50ms for trivial invocations (`--version`, `--help`); lazy-load heavy dependencies.
- Provide clear, actionable error messages: `[Context] → [Problem] → [Solution]`.
- Support `--help` and `--version` flags on every command and subcommand.
- Use consistent flag naming (long form `--verbose`, short form `-v`) across the entire CLI.
- Handle SIGINT (Ctrl+C) gracefully — clean up resources, exit code 130.
- Detect TTY before applying colors / interactive prompts. Honor `NO_COLOR` and `CI` env vars.
- Always provide non-interactive fallbacks (flags or env vars) for CI/CD environments.
- Use POSIX exit codes: 0=success, 1=error, 2=misuse, 127=not-found, 130=SIGINT.
- Write logs/diagnostics to stderr; reserve stdout for primary output (so piping works).
- Ship shell completions for bash, zsh, fish, and PowerShell.

### MUST NOT DO
- Print to stdout when output will be piped (use stderr for diagnostics).
- Use colors when output is not a TTY (test with `<cli> | cat`).
- Require interactive input in CI/CD environments.
- Hardcode paths (`/usr/local/bin`) or platform-specific logic — use `os.homedir()` / `os.UserHomeDir()` / `Path.home()`.
- Block on synchronous I/O unnecessarily — use async reads or stream processing.
- Break existing command signatures — flag/subcommand renames are breaking changes; bump major version.
- Ship without `--help` text or without completions.
- Use `console.log` for errors — use stderr.
- Bundle unnecessary deps in the binary (increases startup time).
- Skip startup-time benchmarking — Rust/Go have no excuse; Node/Python need extra care.

## Code Examples

### Rust (clap) — see `references/rust-cli.md` for full reference

```rust
use clap::{Parser, Subcommand};
use std::process;

#[derive(Parser)]
#[command(name = "mycli", version, about, propagate_version = true)]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Deploy to environment
    Deploy {
        environment: String,
        #[arg(short, long)] dry_run: bool,
        #[arg(short, long)] force: bool,
    },
    /// Generate shell completions
    Completions { #[arg(value_enum)] shell: clap_complete::Shell },
}

fn main() {
    let cli = Cli::parse();
    ctrlc::set_handler(|| { eprintln!("\nCancelled"); process::exit(130); }).unwrap();
    match cli.command {
        Commands::Deploy { environment, dry_run, force: _ } => {
            if dry_run { eprintln!("Would deploy to {environment}"); } else { deploy(&environment); }
        }
        Commands::Completions { shell } => {
            let mut cmd = Cli::command();
            clap_complete::generate(shell, &mut cmd, "mycli", &mut std::io::stdout());
        }
    }
}
```

For Node (commander), Python (typer), and Go (cobra) equivalents, see the corresponding reference files. All four share: subcommands, flags, `--help`, `--version`, SIGINT handler, TTY detection, exit codes 0/1/2/127/130.

## Output Template

```
## CLI Implementation

### Language & framework
- Language: <Node 20 / Python 3.11 / Go 1.22 / Rust 1.78>
- Framework: <commander / typer / cobra / clap>
- Version: <X.Y.Z>

### Command structure
mycli
├── deploy <env> [--dry-run] [--force]
├── config <get|set|list>
└── completions <bash|zsh|fish|powershell>

### Config layers (priority high → low)
1. CLI flags
2. Env vars (MYCLI_*)
3. Project config (./.myclirc)
4. User config (~/.config/mycli/config.yml)
5. Defaults

### Validation gates
- `<cli> --help` renders ✓
- `<cli> --version` renders ✓
- TTY detection: `<cli> deploy dev | cat` → no colors ✓
- SIGINT: exit 130 ✓
- Exit codes: 0/1/2/127/130 ✓
- Startup: `hyperfine '<cli> --version'` → <X>ms (<50ms target)
- Completions: bash/zsh/fish/powershell ✓

### Distribution
- Node: `npm install -g mycli` (bin entry in package.json)
- Python: `pipx install mycli` (entry_points in pyproject.toml)
- Go: Homebrew tap + GitHub Releases (cross-compiled binaries)
- Rust: Homebrew + cargo install + GitHub Releases

### Verified vs Assumed
- VERIFIED: gates above pass on Linux + macOS + Windows
- ASSUMED: <list any platform-specific edge cases not yet tested>
```

## Knowledge Reference

- POSIX utility argument conventions (GNU getopt_long, `--flag=value` and `--flag value`)
- [no-color.org](https://no-color.org/) standard — `NO_COLOR` env var disables color
- POSIX exit codes: 0 success, 1 general error, 2 misuse, 126 not executable, 127 not found, 128+N killed by signal N (so SIGINT = 130)
- Shell completion specs: bash (Bash Completion Project), zsh (`_arguments`), fish (`complete`), PowerShell (`Register-ArgumentCompleter`)
- Frameworks: commander/yargs/oclif (Node), click/typer (Python), cobra/bubbletea (Go), clap/ratatui (Rust)
- Distribution: npm, PyPI, Homebrew, Scoop, AUR, cargo, GitHub Releases, winget
- Performance: lazy imports (Node), lazy module loading (Python `TYPE_CHECKING`), static binaries (Go/Rust), shebang `#!/usr/bin/env` for scripts
- TUI: bubbletea (Go), ratatui (Rust), textual (Python), ink (Node)
