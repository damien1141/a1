# CLI UX Patterns & Cross-Platform Packaging

## TTY Detection

Always check if stdout/stdin is a terminal before using colors or interactive prompts.

### Per language

```javascript
// Node
const useColor = process.stdout.isTTY && !process.env.NO_COLOR && !process.env.CI;
const isInteractive = process.stdin.isTTY && !process.env.CI;
```

```python
# Python
import sys
use_color = sys.stdout.isatty() and not os.environ.get("NO_COLOR") and not os.environ.get("CI")
is_interactive = sys.stdin.isatty() and not os.environ.get("CI")
```

```go
// Go
import "golang.org/x/term"
useColor := term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == "" && os.Getenv("CI") == ""
isInteractive := term.IsTerminal(int(os.Stdin.Fd())) && os.Getenv("CI") == ""
```

```rust
// Rust
use std::io::IsTerminal;
let use_color = std::io::stdout().is_terminal()
    && std::env::var("NO_COLOR").is_err()
    && std::env::var("CI").is_err();
let is_interactive = std::io::stdin().is_terminal() && std::env::var("CI").is_err();
```

### Standards to honor

| Env var | Effect |
|---|---|
| `NO_COLOR` (any value) | Disable all color ([no-color.org](https://no-color.org)) |
| `CI=true` | Disable color + interactivity (auto-detected by most CI) |
| `TERM=dumb` | Disable color (vintage Unix convention) |
| `CLICOLOR=0` | Disable color (BSD convention) |
| `CLICOLOR_FORCE=1` | Force color even when piped |

## SIGINT (Ctrl+C) Handling

### What should happen

1. Catch the signal
2. Print clean message to stderr (`\nOperation cancelled`)
3. Clean up resources (temp files, locks, partial output)
4. Exit with code 130 (= 128 + SIGINT=2)

### Per language

**Node:**
```javascript
let cleaning = false;
process.on('SIGINT', async () => {
  if (cleaning) process.exit(130);   // 2nd Ctrl+C forces exit
  cleaning = true;
  console.error('\nCleaning up...');
  await cleanup();
  process.exit(130);
});
```

**Python:**
```python
import signal, sys, asyncio

cleaning = False

def handler(signum, frame):
    global cleaning
    if cleaning:
        sys.exit(130)
    cleaning = True
    print("\nCleaning up...", file=sys.stderr)
    cleanup()
    sys.exit(130)

signal.signal(signal.SIGINT, handler)
```

**Go:**
```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
go func() {
    <-sigCh
    fmt.Fprintln(os.Stderr, "\nCleaning up...")
    cleanup()
    os.Exit(130)
}()
```

**Rust:**
```rust
use std::sync::atomic::{AtomicBool, Ordering};
use std::process;

static CANCELLED: AtomicBool = AtomicBool::new(false);

ctrlc::set_handler(|| {
    if CANCELLED.swap(true, Ordering::SeqCst) {
        process::exit(130);   // 2nd Ctrl+C
    }
    eprintln!("\nCleaning up...");
    // can't do async cleanup in handler — set flag and check in main loop
    process::exit(130);
}).unwrap();
```

### Trap patterns

For long-running operations, periodically check the cancellation flag:

```rust
for item in items {
    if CANCELLED.load(Ordering::SeqCst) {
        cleanup();
        process::exit(130);
    }
    process_item(item);
}
```

## Color Usage

### Semantic colors

| Color | Use |
|---|---|
| Red | Errors, destructive actions |
| Yellow | Warnings, deprecations |
| Green | Success, completion |
| Blue | Info, neutral |
| Cyan | Code, commands, technical |
| Magenta | Highlights, special |
| Gray/dim | Less important, metadata |

### Always pair color with symbol

Don't rely on color alone — colorblind users won't see it.

```
✓ Success (green + checkmark)
✗ Error   (red + cross)
⚠ Warning (yellow + warning)
ℹ Info    (blue + i)
```

### When to disable

```javascript
const useColor =
  process.stdout.isTTY &&
  !process.env.NO_COLOR &&
  !process.env.CI &&
  process.env.TERM !== 'dumb';
```

## Progress Indicators

### Choose by determinacy

| Type | When | Example |
|---|---|---|
| Determinate progress bar | You know the total | File download, batch processing |
| Spinner (indeterminate) | Unknown duration | API call, DB query |
| Multi-step | Sequential phases | Build → test → deploy |
| Multi-progress | Parallel operations | Deploy API + web + db simultaneously |

### Good progress bar

```
[████████████░░░░░░░░] 60% | 120/200 MB | 2.4 MB/s | ETA: 33s
```

Components:
- Visual bar (20-40 chars)
- Percentage
- Current/total (with units)
- Rate (when applicable)
- ETA (when known)

### Disable progress in CI

```javascript
const showProgress = process.stderr.isTTY && !process.env.CI;
if (showProgress) {
  bar.start(100, 0);
} else {
  console.error('Processing 100 items...');
}
```

Note: write progress to **stderr**, not stdout. Keeps stdout clean for piping.

## Help Text Design

### Structure

```
USAGE
  mycli <command> [options]

COMMANDS
  init [name]        Initialize a new project
  deploy <env>       Deploy to environment
  config <subcmd>    Manage configuration
  completions <sh>   Generate shell completions

OPTIONS
  -h, --help         Show help
  -v, --version      Show version
  --config <file>    Config file path
  --verbose          Verbose output
  --no-color         Disable color

EXAMPLES
  # Initialize a project
  mycli init my-app

  # Deploy to production
  mycli deploy production --dry-run

  # Generate bash completions
  mycli completions bash >> ~/.bashrc

Learn more: https://docs.mycli.dev
```

### Guidelines

- Show examples — users learn by copying
- Use `# comments` to explain each example
- Link to full docs
- Show exit codes if non-standard
- Subcommand help should have its own examples

## Error Messages

### Good

```
✗ Error: Config file not found at /path/to/config.yml

Searched locations:
  • ./mycli.config.yml
  • ~/.config/mycli/config.yml
  • /etc/mycli/config.yml

Solutions:
  • Run 'mycli init' to create a config file
  • Use --config to specify a different location
  • Check file permissions
```

### Bad

```
ENOENT
```

### Pattern

`[Context] → [Problem] → [Solution]`

Always include the **suggested fix** — saves the user a Google search.

## Verbose + Debug Modes

| Flag | What |
|---|---|
| (default) | Only success/failure messages |
| `--verbose` | Each step + intermediate values |
| `--debug` | Verbose + internal state + raw API responses |
| `DEBUG=*` (env) | Equivalent to `--debug` |

```javascript
const VERBOSE = options.verbose || process.env.DEBUG;
const DEBUG = process.env.DEBUG === '*';

if (VERBOSE) console.error(`[verbose] loading config from ${path}`);
if (DEBUG)   console.error(`[debug] config contents:`, config);
```

## Shell Completions

### Generate per shell

```bash
# bash
mycli completions bash > /etc/bash_completion.d/mycli
# OR user-local:
mycli completions bash > ~/.local/share/bash-completion/completions/mycli

# zsh
mycli completions zsh > "${fpath[1]}/_mycli"
# typically ~/.zfunc/_mycli (add to fpath in ~/.zshrc)

# fish
mycli completions fish > ~/.config/fish/completions/mycli.fish

# PowerShell
mycli completions powershell | Out-String | Invoke-Expression
# OR save to profile:
mycli completions powershell >> $PROFILE
```

### Framework support

| Framework | Completions built-in? |
|---|---|
| **commander** | No — use `commander-completions` plugin |
| **yargs** | Yes — `.completion('completion')` |
| **oclif** | Yes — `mycli completions <shell>` |
| **click** | Yes — `click-shell-completion` |
| **typer** | Yes — `--install-completion <shell>` |
| **cobra** | Yes — `mycli completion <shell>` |
| **clap + clap_complete** | Yes — call `generate()` in a `completions` subcommand |

## Cross-Platform Packaging

### Per language

| Language | Primary | Alternatives |
|---|---|---|
| Node | `npm install -g mycli` | `npx mycli` (no install), `pnpm`, `yarn` |
| Python | `pipx install mycli` | `pip install`, `uv tool install`, `conda` |
| Go | Homebrew + GitHub Releases | `go install`, Scoop (Windows), AUR |
| Rust | `cargo install mycli` | Homebrew, cargo-dist, GitHub Releases |

### Single-binary distribution (Go, Rust)

```bash
# Build matrix
GOOS=linux   GOARCH=amd64 go build -o mycli-linux-amd64
GOOS=linux   GOARCH=arm64 go build -o mycli-linux-arm64
GOOS=darwin  GOARCH=amd64 go build -o mycli-darwin-amd64
GOOS=darwin  GOARCH=arm64 go build -o mycli-darwin-arm64
GOOS=windows GOARCH=amd64 go build -o mycli-windows-amd64.exe
```

For Rust:
```bash
rustup target add x86_64-unknown-linux-musl
rustup target add aarch64-apple-darwin
cargo build --release --target x86_64-unknown-linux-musl
cargo build --release --target aarch64-apple-darwin
```

### Homebrew tap

```ruby
# Formula/mycli.rb
class Mycli < Formula
  desc "My awesome CLI"
  homepage "https://github.com/myorg/mycli"
  url "https://github.com/myorg/mycli/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "..."
  head "https://github.com/myorg/mycli.git", branch: "main"

  depends_on "rust" => :build   # for Rust; omit for Go (static binary)

  def install
    system "cargo", "install", *std_cargo_args  # Rust
    # OR
    system "go", "build", *std_go_args           # Go
  end

  test do
    assert_match "mycli #{version}", shell_output("#{bin}/mycli --version")
  end
end
```

Users install: `brew install myorg/tap/mycli`

### Scoop (Windows)

```json
# bucket/mycli.json
{
  "version": "0.1.0",
  "architecture": {
    "64bit": {
      "url": "https://github.com/myorg/mycli/releases/download/v0.1.0/mycli-windows-amd64.exe",
      "hash": "..."
    }
  },
  "bin": "mycli.exe",
  "homepage": "https://github.com/myorg/mycli"
}
```

Users: `scoop install myorg/mycli`

### AUR (Arch)

```bash
# PKGBUILD
pkgname=mycli
pkgver=0.1.0
pkgrel=1
pkgdesc="My awesome CLI"
url="https://github.com/myorg/mycli"
arch=('x86_64')
depends=('gcc-libs')
source=("$pkgname-$pkgver.tar.gz::$url/archive/refs/tags/v$pkgver.tar.gz")
sha256sums=('...')

build() {
  cd "$pkgname-$pkgver"
  cargo build --release
}

package() {
  cd "$pkgname-$pkgver"
  install -Dm755 "target/release/$pkgname" "$pkgdir/usr/bin/$pkgname"
  install -Dm644 "completions/_$pkgname" "$pkgdir/usr/share/zsh/site-functions/_$pkgname"
  install -Dm644 "completions/$pkgname.bash" "$pkgdir/usr/share/bash-completion/completions/$pkgname"
}
```

### GitHub Releases + Installer Script

```bash
# install.sh (curl | sh)
#!/bin/sh
set -e

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
esac

VERSION=$(curl -fsSL https://api.github.com/repos/myorg/mycli/releases/latest | grep tag_name | cut -d '"' -f 4)
URL="https://github.com/myorg/mycli/releases/download/${VERSION}/mycli-${OS}-${ARCH}"

curl -fsSL "$URL" -o /usr/local/bin/mycli
chmod +x /usr/local/bin/mycli
echo "Installed mycli ${VERSION}"
```

Users install: `curl -fsSL https://mycli.dev/install.sh | sh`

For PowerShell:

```powershell
# install.ps1
$os = if ($IsWindows) { "windows" } else { "linux" }
$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
$version = (Invoke-RestMethod https://api.github.com/repos/myorg/mycli/releases/latest).tag_name
$url = "https://github.com/myorg/mycli/releases/download/$version/mycli-$os-$arch.exe"
Invoke-WebRequest $url -OutFile "$env:LOCALAPPDATA\Programs\mycli\mycli.exe"
```

Users install: `irm https://mycli.dev/install.ps1 | iex`

## Release Checklist

Before tagging a release:

- [ ] All tests pass on Linux + macOS + Windows
- [ ] `--version` shows the new version
- [ ] `--help` renders correctly
- [ ] Shell completions generate for bash, zsh, fish, PowerShell
- [ ] TTY detection works (`mycli foo | cat` produces no colors)
- [ ] SIGINT handler exits 130 cleanly
- [ ] Startup time <50ms (Rust/Go) or <300ms (Node/Python) — `hyperfine`
- [ ] CHANGELOG.md updated
- [ ] Homebrew tap formula updated (or automated via goreleaser/cargo-dist)
- [ ] Signed binaries (cosign / GPG) if security-sensitive

## Common Pitfalls

1. **Colors in CI logs** — looks awful in Jenkins/GitHub Actions. Detect `CI=true`.
2. **`Ctrl+C` dumps stack trace** — install SIGINT handler.
3. **Exit 0 on error** — breaks `mycli && next-cmd` shell patterns.
4. **Stdout polluted with logs** — `mycli deploy | jq` fails. Use stderr.
5. **Hardcoded `/usr/local/bin`** in installers — fails on macOS (SIP) and Linux without sudo. Use `~/.local/bin`.
6. **No completion command** — users must memorize every subcommand.
7. **Different flag names across subcommands** — `--verbose` here, `--debug` there. Be consistent.
8. **Heavy deps in always-loaded module** — slow startup for `--version`.
9. **Forgetting Windows path separators** — use `path.join()` / `filepath.Join()` / `Path::join()`.
10. **Releasing without testing on actual Windows** — Windows console has Unicode + ANSI quirks.
