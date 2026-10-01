---
name: rust-pro
description: "Use when writing Rust 2024 edition that must pass cargo clippy -D warnings, cargo fmt, and cargo test. Designs ownership/lifetimes, traits (object-safe vs generic), Result/Option combinators, async with tokio, and verifies with cargo fmt + clippy + test + audit before exit. Tauri desktop apps supported via the tauri reference."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "rust,cargo,clippy,borrow checker,ownership,lifetimes,tokio,async,traits,thiserror,anyhow,tauri"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "golang-pro,cpp-pro,cli-developer,system-architecture"
---

# Rust Pro

Rust 2024 edition specialist. Memory-safety-first, trait-driven, async-capable, with a verification gate of `cargo fmt` + `cargo clippy -D warnings` + `cargo test` + `cargo audit`. Honest exit separates **VERIFIED** (ran a cargo command, saw green) from **ASSUMED** (could not run).

Tauri 2 desktop apps are supported but **demoted to a single reference** — Rust itself is the focus. Load `references/tauri.md` only when the task mentions Tauri.

## When to Use

- Writing libraries, CLIs, or services in Rust 2024 edition that must pass `cargo clippy --all-targets -- -D warnings`
- Designing ownership and lifetimes; resolving borrow-checker errors without `unsafe` escape hatches
- Building trait-based APIs (object-safe vs generic, sealed traits, associated types, GATs)
- Async with `tokio`; cancellation safety, `spawn_blocking`, `select!`
- Error design with `thiserror` (libs) and `anyhow` (apps)
- Tauri 2 cross-platform desktop apps (load `references/tauri.md`)
- Polishing for release: `cargo audit`, `cargo flamegraph`, profile settings

## Operating Loop

1. **Scope** — Name the artifact and the ONE load-bearing unknown (e.g. "is the shared state `Arc<Mutex<T>>` or a channel?"). State edition (2024) and MSRV.
2. **Recon** — Read `Cargo.toml`, `Cargo.lock`, `rust-toolchain.toml`. For Tauri: read `tauri.conf.json`, `capabilities/`.
3. **Design ownership first** — Sketch ownership graph, borrow lifetimes, error type. Code second. Apply the memory-safety lattice (below).
4. **Local proof** — State the invariant, attempt to disprove it, run `cargo check`. **Two strikes rule**: when the compiler rejects the same fix twice, the shape is wrong — consult `references/ownership.md`.
5. **Implement** — `Result`/`?` for fallible paths, no `unwrap()` outside tests, `unsafe` only with a `// SAFETY:` comment.
6. **Verify (gate)** — In order, until clean:
   - `cargo fmt --check`
   - `cargo clippy --all-targets --all-features -- -D warnings`
   - `cargo test` (and `cargo test --doc`)
   - `cargo audit` (report if unavailable)
   - For Tauri: also `tauri dev` smoke + frontend build
   - If any step fails: fix the cause, do not add `#[allow(...)]` without justification. Re-run from the top.
7. **Exit** — Write the report. **VERIFIED**: list each cargo command + summary. **ASSUMED**: list what you believe but did not run. Flag lingering risk (e.g. an `#[allow(clippy::x)]` with reason, an `unsafe` block, an untested async cancellation path).

## Memory-safety lattice (axioms)

| Area | Rule |
|---|---|
| `unsafe` | Only with a `// SAFETY:` comment naming the invariant |
| Errors | No `unwrap()` outside tests; `thiserror` for libs, `anyhow` for apps |
| Async | `spawn_blocking` for CPU work; never `.lock().await` across `.await`; cancellation-safe |
| Concurrency | Prefer channels (`mpsc`, `broadcast`) over shared `Arc<Mutex<T>>` |
| Design | Builder for fallible construction; `Cow` for borrow-or-clone |
| API | `impl Trait`/generics for static dispatch; `dyn Trait` only when object-safe and needed |

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Ownership, borrows, lifetimes | `references/ownership.md` | Moves, borrows, lifetimes, smart pointers, `Cow`, `Pin`, builder; **two compiler strikes** |
| Traits | `references/traits.md` | Associated types, bounds, object safety, `dyn`, GATs, sealed traits |
| Error handling | `references/error-handling.md` | `Result`/`Option` combinators, `thiserror`, `anyhow`, `From`, `?` |
| Async | `references/async.md` | `tokio`, `join!/try_join!/select!`, timeout, channels, graceful shutdown |
| Testing | `references/testing.md` | Unit/integration/doc tests, `proptest`, `mockall`, `criterion`, `insta`, fuzzing |
| Tooling | `references/tooling.md` | `cargo fmt`/`clippy`/`audit`/`flamegraph`/`tree`, profiles, MSRV, edition |
| Tauri 2 | `references/tauri.md` | Cross-platform desktop apps with Tauri 2 (commands, state, events, capabilities) |

## Constraints

### MUST DO
- `cargo fmt` + `cargo clippy -D warnings` clean before commit
- `?` for error propagation; `Result<T, E>` on fallible APIs
- `// SAFETY:` comment on every `unsafe` block, naming the invariant
- `Arc<Mutex<T>>` for shared mutable state across threads; `Rc<RefCell<T>>` only single-threaded
- `spawn_blocking` for blocking/CPU work inside async
- Document every public item with `///` doc comments
- Edition 2024 in `Cargo.toml`; pin MSRV if lower
- Run `cargo audit` for known CVEs

### MUST NOT DO
- Use `unwrap()`/`expect()` outside tests (use `?` or `Result`)
- Use `unsafe` without a `// SAFETY:` comment
- Add `#[allow(clippy::...)]` without a reason comment
- Hold a `MutexGuard` across `.await` (deadlock risk)
- Use `Box<dyn Trait>` when generics fit
- Use `String` where `&str` suffices; `Vec<T>` where `&[T]` suffices
- Hardcode secrets or environment-specific config

## Code Examples

### `Result` + `?` + custom error with `thiserror`
```rust
#[derive(Debug, thiserror::Error)]
pub enum LoadError {
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
    #[error("parse: {0}")]
    Parse(String),
}

pub fn load_config(path: &Path) -> Result<Config, LoadError> {
    let text = std::fs::read_to_string(path)?;
    toml::from_str(&text).map_err(|e| LoadError::Parse(e.to_string()))
}
```

### Builder pattern (fallible construction)
```rust
pub struct ServerBuilder { addr: Option<String>, port: Option<u16> }

impl ServerBuilder {
    pub fn new() -> Self { Self { addr: None, port: None } }
    pub fn addr(mut self, a: impl Into<String>) -> Self { self.addr = Some(a.into()); self }
    pub fn port(mut self, p: u16) -> Self { self.port = Some(p); self }
    pub fn build(self) -> anyhow::Result<Server> {
        Ok(Server {
            addr: self.addr.ok_or_else(|| anyhow::anyhow!("addr required"))?,
            port: self.port.unwrap_or(8080),
        })
    }
}
```

### Async with tokio — cancellation-safe
```rust
async fn fetch_one(c: &reqwest::Client, url: &str) -> anyhow::Result<bytes::Bytes> {
    Ok(c.get(url).send().await?.error_for_status()?.bytes().await?)
}

pub async fn fetch_all(urls: Vec<String>) -> anyhow::Result<Vec<bytes::Bytes>> {
    let client = reqwest::Client::new();
    let futs = urls.iter().map(|u| fetch_one(&client, u));
    Ok(futures::future::try_join_all(futs).await?)
}
```

### Trait — generic vs `dyn`
```rust
pub fn sum<T: Iterator<Item = i64>>(it: T) -> i64 { it.sum() }  // static dispatch

pub trait Logger: Send + Sync { fn log(&self, msg: &str); }
pub struct App { logger: Box<dyn Logger> }  // dynamic, only when needed
```

### `cargo` release profile
```toml
[profile.release]
lto = "fat"
codegen-units = 1
panic = "abort"
```

## Output Template

When delivering a Rust feature: module file(s) with `///` docs → tests (`#[cfg(test)]` + `tests/`) → `Cargo.toml` deltas → verification block (`cargo fmt --check`, `cargo clippy -- -D warnings`, `cargo test`, `cargo audit`) → exit report (VERIFIED / ASSUMED / lingering risk).

## Knowledge Reference

Rust 2024 · ownership/borrows/lifetimes · `Arc`/`Mutex`/`RwLock` · `Rc`/`RefCell` · `Cow` · `Pin` · traits (associated types, GATs, sealed, object safety) · `Result`/`Option`/`?` · `thiserror`/`anyhow` · `tokio` · `futures` · `reqwest`/`axum` · `serde`/`toml` · `clap` · `tracing` · `cargo`/`clippy`/`rustfmt` · `cargo-audit`/`cargo-deny` · `criterion` · `proptest`/`cargo-fuzz` · `insta`/`mockall` · Tauri 2
