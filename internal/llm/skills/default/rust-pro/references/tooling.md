# Cargo Tooling & Profiles

## Edition

```toml
[package]
name = "myapp"
version = "0.1.0"
edition = "2024"
rust-version = "1.75"   # MSRV
```

Edition 2024 is current (stabilized with Rust 1.85). New code uses 2024. Set `rust-version` to your MSRV; CI tests against it.

## `cargo` commands

| Command | Purpose |
|---|---|
| `cargo check` | Fast type-check, no codegen |
| `cargo build` | Compile |
| `cargo run` | Build and run |
| `cargo test` | Run all tests (unit + integration + doc) |
| `cargo bench` | Run benchmarks (requires `criterion` or built-in) |
| `cargo fmt` | Format code |
| `cargo fmt --check` | CI-friendly: non-zero exit if unformatted |
| `cargo clippy` | Lint (use `-- -D warnings` for strict) |
| `cargo doc` | Generate docs |
| `cargo audit` | Check deps for CVEs |
| `cargo tree` | Dependency tree |
| `cargo update` | Update `Cargo.lock` within semver bounds |
| `cargo publish` | Publish to crates.io |
| `cargo expand` | Show macro expansion (requires `cargo-expand`) |

## `rustfmt`

```toml
# rustfmt.toml
edition = "2024"
max_width = 100
```

Most projects use defaults. Run `cargo fmt` before every commit; `cargo fmt --check` in CI.

## `clippy` — the lint suite

```bash
cargo clippy --all-targets --all-features -- -D warnings
```

Lints you should never disable without a comment:
- `clippy::unwrap_used` — `unwrap` in production paths
- `clippy::dbg_macro` — `dbg!()` left in code
- `clippy::todo` — `todo!()` left in code
- `clippy::panic` — `panic!()` in library code

When you must disable:

```rust
#[allow(clippy::too_many_arguments, reason = "fixed API; refactor in v2")]
fn legacy_handler(a: i32, b: i32, c: i32, /* ... */) { /* ... */ }
```

Always include a `reason`.

## Profiles

```toml
[profile.dev]
debug = "line-tables-only"   # faster builds, enough for backtraces

[profile.release]
lto = "fat"                   # whole-program optimization
codegen-units = 1             # better optimization, slower compile
panic = "abort"               # smaller binary, no unwinding
strip = true                  # strip debug symbols

[profile.dev.package."*"]
opt-level = 2                 # optimize deps in dev for faster runtime
```

CI/dev trade-off: `lto = "fat"` + `codegen-units = 1` slows compile by 2-5x but yields 5-15% smaller/faster binaries. Use only for release.

## Workspace layout

```toml
# Cargo.toml (workspace root)
[workspace]
members = ["crates/*"]
resolver = "2"

[workspace.dependencies]
serde = { version = "1.0", features = ["derive"] }
tokio = { version = "1", features = ["full"] }
```

```toml
# crates/mylib/Cargo.toml
[dependencies]
serde = { workspace = true }
```

Workspaces share `Cargo.lock` and a `target/` dir — faster builds, consistent dep versions.

## `cargo-audit`

```bash
cargo install cargo-audit
cargo audit
```

Checks `Cargo.lock` against the RustSec advisory DB. Run in CI on every PR and nightly.

For deeper checks: `cargo-deny` (licenses, bans, advisories, sources).

## `cargo-flamegraph`

```bash
cargo install flamegraph
cargo flamegraph --bin myapp
```

Generates an SVG call-stack profile. Requires `perf` (Linux) or `dtrace` (macOS).

## `cargo-nextest`

```bash
cargo install cargo-nextest
cargo nextest run
```

Faster test runner with better output and isolation. Drop-in replacement for `cargo test` in CI.

## Features

```toml
[features]
default = ["tls"]
tls = ["dep:rustls"]
json = ["dep:serde_json"]

[dependencies]
rustls = { version = "0.23", optional = true }
serde_json = { version = "1", optional = true }
```

- Features are additive — never use them to remove functionality
- Use `dep:` to avoid implicit feature names matching dep names
- Test with `--all-features` and `--no-default-features` in CI

## MSRV

```toml
[package]
rust-version = "1.75"
```

CI tests against MSRV:

```yaml
- uses: dtolnay/rust-toolchain@1.75
- run: cargo check --workspace
```

Use `cargo msrv` to verify and find the actual MSRV.

## CI sketch

```yaml
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: dtolnay/rust-toolchain@stable
        with: { components: clippy, rustfmt }
      - uses: Swatinem/rust-cache@v2
      - run: cargo fmt --check
      - run: cargo clippy --all-targets --all-features -- -D warnings
      - run: cargo test --all-features
      - run: cargo audit --deny warnings
```

## Common pitfalls

- `cargo build` for release — use `cargo build --release`
- Forgetting `--all-features` in clippy/test — feature-gated code goes unchecked
- Pinning exact versions in `Cargo.toml` — use `^` (default) semver bounds
- Adding `features = ["full"]` on `tokio` when you need 3 features — bloats binary
- Not setting `resolver = "2"` — workspace feature unification surprises
