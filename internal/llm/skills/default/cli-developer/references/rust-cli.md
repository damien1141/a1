# Rust CLI Development

## Why Rust for CLIs?

- **Startup time**: typically 1-5ms (static binary, no runtime init)
- **Memory footprint**: minimal
- **Single binary distribution**: no runtime to install
- **Type safety**: catches flag-parsing bugs at compile time
- **Cargo**: best-in-class package manager + build tool

Trade-off: longer compile times; steeper learning curve.

## Frameworks

| Crate | Purpose | Notes |
|---|---|---|
| **clap** | Argument parsing (recommended) | v4.x, derive macros |
| **ratatui** | TUI apps | Successor to `tui-rs` |
| **indicatif** | Progress bars + spinners | Most popular |
| **console** | Terminal styling, colors, input | Pair with `dialoguer` |
| **dialoguer** | Interactive prompts | Select, multi-select, input |
| **inquire** | Modern alternative to `dialoguer` | Nicer API |
| **clap_complete** | Shell completion generation | Works with clap derive |

## clap (v4, derive)

```toml
# Cargo.toml
[package]
name = "mycli"
version = "0.1.0"
edition = "2021"

[dependencies]
clap = { version = "4.5", features = ["derive", "env"] }
clap_complete = "4.5"
ctrlc = "3.4"
indicatif = "0.17"
console = "0.15"

[profile.release]
lto = true
codegen-units = 1
strip = true
panic = "abort"   # smaller binary
```

```rust
// src/main.rs
use clap::{Parser, Subcommand};
use std::process;

/// My awesome CLI
#[derive(Parser, Debug)]
#[command(name = "mycli", version, about, long_about = None, propagate_version = true)]
struct Cli {
    /// Enable verbose output
    #[arg(short, long, global = true)]
    verbose: bool,

    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand, Debug)]
enum Commands {
    /// Initialize a new project
    Init {
        /// Project name
        #[arg(default_value = "my-app")]
        name: String,

        /// Project template
        #[arg(short, long, default_value = "default")]
        template: String,

        /// Overwrite existing files
        #[arg(short, long)]
        force: bool,
    },

    /// Deploy to environment
    Deploy {
        /// Target environment
        #[arg(value_enum)]
        environment: Environment,

        /// Preview without executing
        #[arg(short, long)]
        dry_run: bool,

        /// Config file path
        #[arg(short, long, env = "MYCLI_CONFIG")]
        config: Option<String>,
    },

    /// Manage configuration
    Config {
        #[command(subcommand)]
        action: ConfigAction,
    },

    /// Generate shell completions
    Completions {
        /// Target shell
        #[arg(value_enum)]
        shell: clap_complete::Shell,
    },
}

#[derive(clap::ValueEnum, Clone, Debug)]
enum Environment {
    Dev,
    Staging,
    Prod,
}

#[derive(Subcommand, Debug)]
enum ConfigAction {
    /// Get a config value
    Get { key: String },
    /// Set a config value
    Set { key: String, value: String },
    /// List all config
    List,
}

fn main() {
    let cli = Cli::parse();

    // SIGINT handler
    ctrlc::set_handler(|| {
        eprintln!("\nOperation cancelled");
        process::exit(130);
    }).expect("Error setting Ctrl-C handler");

    if cli.verbose {
        eprintln!("[debug] verbose mode enabled");
    }

    match &cli.command {
        Commands::Init { name, template, force } => init_project(name, template, *force),
        Commands::Deploy { environment, dry_run, config } => {
            deploy(environment, *dry_run, config.as_deref())
        }
        Commands::Config { action } => match action {
            ConfigAction::Get { key } => println!("{}", get_config(key)),
            ConfigAction::Set { key, value } => set_config(key, value),
            ConfigAction::List => list_config(),
        },
        Commands::Completions { shell } => {
            let mut cmd = Cli::command();
            clap_complete::generate(*shell, &mut cmd, "mycli", &mut std::io::stdout());
        }
    }
}

fn init_project(name: &str, template: &str, force: bool) {
    println!("Creating {name} from {template} (force={force})");
}

fn deploy(env: &Environment, dry_run: bool, _config: Option<&str>) {
    let env_str = match env {
        Environment::Dev => "dev",
        Environment::Staging => "staging",
        Environment::Prod => "prod",
    };
    if dry_run {
        eprintln!("Would deploy to {env_str}");
    } else {
        println!("Deploying to {env_str}...");
    }
}

fn get_config(_key: &str) -> String { "value".into() }
fn set_config(_key: &str, _value: &str) {}
fn list_config() {}
```

Build + run:

```bash
cargo run -- --help
cargo run -- deploy prod --dry-run
cargo run -- --version
cargo build --release  # produces target/release/mycli
```

## clap features

### Required vs optional args

```rust
#[derive(Parser)]
struct Cli {
    /// Required positional
    name: String,

    /// Optional positional (defaults to None)
    nickname: Option<String>,

    /// Optional with default
    #[arg(default_value = "default")]
    template: String,
}
```

### Boolean flags

```rust
#[derive(Parser)]
struct Cli {
    /// Force (presence = true)
    #[arg(short, long)]
    force: bool,

    /// Negatable: --force / --no-force
    #[arg(long, default_value_t = true, action = clap::ArgAction::Set)]
    force2: bool,
}
```

### Counted flags (`-vvv`)

```rust
#[derive(Parser)]
struct Cli {
    #[arg(short, long, action = clap::ArgAction::Count)]
    verbose: u8,
}
// -v = 1, -vv = 2, -vvv = 3
```

### Variadic args

```rust
#[derive(Parser)]
struct Cli {
    /// Files to process (variadic)
    files: Vec<String>,
}
```

### Subcommands with custom parser

```rust
#[derive(Subcommand)]
enum Commands {
    /// Add a new entry
    Add {
        #[arg(value_parser = clap::value_parser!(u32).range(1..))]
        id: u32,
        name: String,
    },
}
```

### Loading config from env (clap `env` feature)

```rust
#[derive(Parser)]
struct Cli {
    /// API token (or set MYCLI_API_TOKEN env var)
    #[arg(long, env = "MYCLI_API_TOKEN")]
    api_token: String,
}
```

User can either pass `--api-token xxx` or set `MYCLI_API_TOKEN=xxx`. Clap shows both in `--help`.

### Conflicts + requirements

```rust
#[derive(Parser)]
struct Cli {
    #[arg(long, conflicts_with = "output_file")]
    stdout: bool,

    #[arg(long, required_unless_present = "stdout")]
    output_file: Option<String>,
}
```

## Shell Completions

```rust
use clap_complete::Shell;

// In your command:
Commands::Completions { shell } => {
    let mut cmd = Cli::command();
    clap_complete::generate(*shell, &mut cmd, "mycli", &mut std::io::stdout());
}
```

Or generate at build time via `build.rs`:

```rust
// build.rs
use clap_complete::{generate_to, Shell};

fn main() -> std::io::Result<()> {
    let outdir = std::env::var_os("OUT_DIR").unwrap();
    let mut cmd = clap::Command::new("mycli");
    for shell in [Shell::Bash, Shell::Zsh, Shell::Fish, Shell::PowerShell] {
        generate_to(shell, &mut cmd, "mycli", &outdir)?;
    }
    Ok(())
}
```

Install:

```bash
mycli completions bash > /etc/bash_completion.d/mycli
mycli completions zsh  > ~/.zfunc/_mycli
mycli completions fish > ~/.config/fish/completions/mycli.fish
mycli completions powershell | Out-String | Invoke-Expression
```

## Colored Output (console)

```rust
use console::style;

fn main() {
    // Auto-detects TTY + NO_COLOR env var
    println!("{} Info: starting", style("ℹ").blue());
    println!("{} Success: done", style("✔").green());
    println!("{} Warning: deprecated", style("⚠").yellow());
    println!("{} Error: failed", style("✖").red());

    // Styled strings
    let bold = style("Important").bold();
    let dim = style("less important").dim();
    println!("{bold} vs {dim}");

    // Force-disable for CI
    let colored = console::user_attended();
    if !colored {
        // colors auto-disabled
    }
}
```

## Progress + Spinners (indicatif)

```rust
use indicatif::{ProgressBar, ProgressStyle};
use std::thread;
use std::time::Duration;

fn main() {
    // Determinate progress bar
    let pb = ProgressBar::new(100);
    pb.set_style(ProgressStyle::with_template(
        "{spinner:.green} [{bar:40.cyan/blue}] {pos}/{len} ({eta})"
    ).unwrap().progress_chars("#>-"));

    for _ in 0..100 {
        pb.inc(1);
        thread::sleep(Duration::from_millis(50));
    }
    pb.finish_with_message("Done");

    // Spinner (indeterminate)
    let spinner = ProgressBar::new_spinner();
    spinner.set_message("Installing dependencies...");
    spinner.enable_steady_tick(Duration::from_millis(80));
    thread::sleep(Duration::from_secs(3));
    spinner.finish_with_message("Dependencies installed");
}
```

## Interactive Prompts (inquire or dialoguer)

```rust
use inquire::{Text, Select, Confirm, MultiSelect, Password};

fn main() {
    // Text input
    let name = Text::new("Project name:")
        .with_default("my-app")
        .prompt()
        .unwrap();

    // Select (single)
    let env = Select::new("Select environment:",
        vec!["development", "staging", "production"])
        .prompt()
        .unwrap();

    // Multi-select
    let features = MultiSelect::new("Select features:",
        vec!["TypeScript", "ESLint", "Prettier", "Jest"])
        .prompt()
        .unwrap();

    // Confirm
    let ok = Confirm::new("Deploy to production?")
        .with_default(false)
        .prompt()
        .unwrap();

    // Password (masked)
    let pw = Password::new("Enter password:")
        .prompt()
        .unwrap();
}
```

## Error Handling

Use `anyhow` for application errors and `thiserror` for library errors.

```rust
use anyhow::{Context, Result};

fn main() -> Result<()> {
    let config = load_config().context("Failed to load config")?;
    let data = std::fs::read_to_string("input.txt")
        .with_context(|| format!("Failed to read input.txt"))?;
    Ok(())
}
```

For typed exit codes:

```rust
use std::process;

fn main() {
    if let Err(e) = run() {
        eprintln!("Error: {e:#}");
        process::exit(match e.downcast_ref::<CliError>() {
            Some(CliError::InvalidArgs) => 2,
            Some(CliError::NotFound) => 127,
            _ => 1,
        });
    }
}

#[derive(thiserror::Error, Debug)]
enum CliError {
    #[error("invalid arguments: {0}")]
    InvalidArgs(String),
    #[error("not found: {0}")]
    NotFound(String),
    #[error(transparent)]
    Io(#[from] std::io::Error),
}

fn run() -> Result<(), CliError> {
    let args = parse()?;
    Ok(())
}
```

## SIGINT

```rust
use ctrlc;
use std::process;

fn main() {
    ctrlc::set_handler(|| {
        eprintln!("\nOperation cancelled");
        process::exit(130);
    }).expect("Error setting Ctrl-C handler");

    // Long-running work...
}
```

For more control (cleanup before exit), use `signal-hook`:

```rust
use signal_hook::{consts::SIGINT, iterator::Signals};
use std::{process, thread, time::Duration};

fn main() {
    let mut signals = Signals::new(&[SIGINT]).unwrap();
    thread::spawn(move || {
        for sig in signals.forever() {
            eprintln!("\nReceived signal {sig}, cleaning up...");
            // cleanup...
            process::exit(130);
        }
    });

    // main work
    loop { thread::sleep(Duration::from_secs(1)); }
}
```

## Config Layers

```toml
# Cargo.toml
[dependencies]
serde = { version = "1", features = ["derive"] }
toml = "0.8"
dirs = "5"
```

```rust
use serde::Deserialize;
use std::{fs, path::PathBuf};
use dirs::config_dir;

#[derive(Deserialize, Default, Debug)]
struct Config {
    environment: Option<String>,
    timeout: Option<u32>,
    verbose: Option<bool>,
}

fn load_config(explicit: Option<PathBuf>) -> Config {
    let mut merged = Config::default();

    let paths = [
        Some(PathBuf::from("/etc/mycli/config.toml")),
        config_dir().map(|p| p.join("mycli").join("config.toml")),
        Some(PathBuf::from(".myclirc")),
        explicit,
    ];

    for path in paths.iter().flatten() {
        if path.exists() {
            if let Ok(contents) = fs::read_to_string(path) {
                if let Ok(cfg) = toml::from_str::<Config>(&contents) {
                    // Merge (later overrides earlier)
                    if cfg.environment.is_some() { merged.environment = cfg.environment; }
                    if cfg.timeout.is_some() { merged.timeout = cfg.timeout; }
                    if cfg.verbose.is_some() { merged.verbose = cfg.verbose; }
                }
            }
        }
    }

    merged
}
```

## Testing CLIs

```rust
// tests/cli.rs
use assert_cmd::Command;
use predicates::prelude::*;

#[test]
fn test_version() {
    Command::cargo_bin("mycli")
        .unwrap()
        .arg("--version")
        .assert()
        .success()
        .stdout(predicate::str::contains("mycli "));
}

#[test]
fn test_help() {
    Command::cargo_bin("mycli")
        .unwrap()
        .arg("--help")
        .assert()
        .success()
        .stdout(predicate::str::contains("USAGE"));
}

#[test]
fn test_deploy_dry_run() {
    Command::cargo_bin("mycli")
        .unwrap()
        .args(["deploy", "prod", "--dry-run"])
        .assert()
        .success()
        .stderr(predicate::str::contains("Would deploy to prod"));
}

#[test]
fn test_invalid_env_exits_2() {
    Command::cargo_bin("mycli")
        .unwrap()
        .args(["deploy", "invalid"])
        .assert()
        .failure()
        .code(2);
}
```

## Build & Distribution

### Cargo (basic)

```bash
cargo build --release
# binary at target/release/mycli

# Install to ~/.cargo/bin
cargo install --path .

# Users install:
cargo install mycli  # from crates.io
cargo install --git https://github.com/myorg/mycli
```

### Cross-compile

```bash
rustup target add x86_64-unknown-linux-musl
rustup target add aarch64-apple-darwin

cargo build --release --target x86_64-unknown-linux-musl
cargo build --release --target aarch64-apple-darwin
```

### Release with cargo-dist (recommended)

```toml
# Cargo.toml
[workspace.metadata.dist]
cargo-dist-version = "0.13.0"
ci = ["github"]
installers = ["shell", "powershell", "homebrew", "npm"]
targets = ["x86_64-unknown-linux-gnu", "aarch64-apple-darwin", "x86_64-apple-darwin", "x86_64-pc-windows-msvc"]
```

```bash
cargo dist init
cargo dist build
cargo dist plan
```

GitHub Action auto-releases on tag push. Generates installers:
- `curl -fsSL https://mycli.dev/install.sh | sh` (shell)
- `irm https://mycli.dev/install.ps1 | iex` (PowerShell)
- Homebrew tap formula
- npm package wrapper (so `npx mycli` works)

### Homebrew tap (manual)

```ruby
# Formula/mycli.rb
class Mycli < Formula
  desc "My awesome CLI"
  homepage "https://github.com/myorg/mycli"
  url "https://github.com/myorg/mycli/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "..."
  head "https://github.com/myorg/mycli.git", branch: "main"

  depends_on "rust" => :build

  def install
    system "cargo", "install", *std_cargo_args
  end

  test do
    assert_match "mycli #{version}", shell_output("#{bin}/mycli --version")
  end
end
```

## Startup Time

Rust CLIs are typically 1-5ms cold-start. To verify:

```bash
hyperfine --warmup 3 './target/release/mycli --version'
```

To keep it fast:
1. **Avoid heavy `lazy_static!` initialization** at startup
2. **Lazy-load optional features** behind feature flags
3. **`panic = "abort"`** in release profile (smaller binary, no unwinding tables)
4. **`lto = true`, `codegen-units = 1`** for optimization
5. **`strip = true`** removes debug symbols (smaller binary)

## Common Pitfalls

1. **`println!` for errors** — goes to stdout. Use `eprintln!` or `eprint!`.
2. **`process::exit()` inside `?` chain** — skips Drop. Use `Result` and exit at top.
3. **Forgetting `propagate_version = true`** — subcommands don't get `--version`.
4. **`clap::ArgAction::Set` vs `SetTrue`** — `Set` requires value, `SetTrue` doesn't.
5. **`env = "VAR"` requires `env` feature** — enable in `Cargo.toml`.
6. **No `#[arg(value_enum)]`** — stringly typed; lose validation.
7. **Not handling SIGINT** — Ctrl+C dumps panic. Always install handler.
8. **Heavy work in `main`** — hard to test. Move to functions returning `Result`.
9. **Forgetting `[profile.release]` optimizations** — binary is bigger + slower.
10. **`unwrap()` on user input** — panics on bad input. Use `?` with `Result`.

## Best Practices

1. **Use `clap` derive** — less boilerplate, type-safe
2. **Define `value_enum` for restricted choices** — auto-generates `--help` enum + completions
3. **`env` feature** — let users set flags via env vars
4. **`clap_complete`** — always ship completions
5. **`anyhow`** for apps, `thiserror`** for libs
6. **`cargo dist`** for cross-platform release
7. **Test with `assert_cmd` + `predicates`** — end-to-end CLI tests
8. **Benchmark with `hyperfine`** — keep startup <5ms
9. **`console` for colors** — auto-detects TTY + NO_COLOR
10. **`indicatif` for progress** — handles TTY vs non-TTY automatically
