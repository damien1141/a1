# Error Handling

## `Result` and `Option` — the two error types

```rust
pub enum Result<T, E> { Ok(T), Err(E) }
pub enum Option<T> { Some(T), None }
```

- `Option<T>` — absence without a reason (lookup miss, optional config)
- `Result<T, E>` — failure with a typed cause

Use `Option` when "not found" is normal; `Result` when it's a real failure.

## `?` propagation

```rust
fn read_config(path: &Path) -> Result<Config, LoadError> {
    let text = std::fs::read_to_string(path)?;     // io::Error → LoadError via #[from]
    let cfg = toml::from_str(&text)?;              // toml::de::Error → LoadError
    Ok(cfg)
}
```

`?` returns early on `Err`, converts the error via `From`. Inside `main()`, `?` works if `main` returns `Result`.

## `thiserror` for libraries

```rust
#[derive(Debug, thiserror::Error)]
pub enum LoadError {
    #[error("io error: {0}")]
    Io(#[from] std::io::Error),
    #[error("parse error: {0}")]
    Parse(#[from] toml::de::Error),
    #[error("missing field: {field}")]
    Missing { field: String },
}
```

- `#[error("...")]` — Display impl
- `#[from]` — From impl (auto-conversion)
- `#[source]` — chain another error without conversion

Libraries should expose a typed error enum. Users can `match` and `errors::Error::is`/`downcast`.

## `anyhow` for applications

```rust
use anyhow::{Context, Result};

fn run() -> Result<()> {
    let cfg = read_config(path).context("loading config")?;
    let db = connect(&cfg.dsn).context("connecting to db")?;
    Ok(())
}
```

`anyhow::Result<T>` = `Result<T, anyhow::Error>`. `.context("...")` adds a message to the chain. Use `anyhow` at the binary boundary; convert to specific errors when crossing into library code.

## Custom error without macros

```rust
#[derive(Debug)]
pub enum LoadError {
    Io(std::io::Error),
    Parse(String),
}

impl std::fmt::Display for LoadError {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        match self {
            Self::Io(e) => write!(f, "io: {e}"),
            Self::Parse(s) => write!(f, "parse: {s}"),
        }
    }
}

impl std::error::Error for LoadError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self { Self::Io(e) => Some(e), _ => None }
    }
}

impl From<std::io::Error> for LoadError {
    fn from(e: std::io::Error) -> Self { Self::Io(e) }
}
```

`thiserror` does this for you — only hand-roll if you can't add the dep.

## Combinators

```rust
let v: Option<i32> = parse(s).map(|n| n * 2);
let v: Result<i32, E> = parse(s).and_then(|n| validate(n));
let v: i32 = opt.unwrap_or(0);
let v: i32 = opt.unwrap_or_else(|| default());
let v: Result<i32, E> = opt.ok_or_else(|| E::Missing);
```

Prefer combinators over `match` for short transforms; `match` reads better for complex branching.

## Sentinel errors (avoid)

```rust
// Avoid: magic boolean or string codes
pub fn find(id: i32) -> Option<User> { /* ... */ }

// Prefer: typed Result with context
pub fn find(id: i32) -> Result<User, FindError> { /* ... */ }
```

## Error context — the chain

```rust
let u = find_user(id)
    .with_context(|| format!("looking up user {id}"))?;
```

The final error has a chain like: "looking up user 42: io: connection refused". `anyhow`'s `{:?}` prints the whole chain; `{}` prints just the top message.

## Don't unwrap in production

| Pattern | OK in tests | Production |
|---|---|---|
| `.unwrap()` | Yes | No — use `?` |
| `.expect("msg")` | Yes | Rarely — use `?` |
| `.unwrap_or(default)` | Yes | Yes (intentional fallback) |
| `.unwrap_or_else(calc)` | Yes | Yes |
| `panic!()` | Yes (test setup) | No — only for true invariants |

## `panic` vs `Result`

- `panic!` — programmer error, invariant violation, unrecoverable
- `Result` — expected failure, recoverable, user-facing

Examples:
- Index out of bounds → `panic` (use `.get(i)` to return `Option`)
- File not found → `Result`
- Division by zero → `panic` if divisor is a constant; `Result` if user input
- DB connection refused → `Result`

## Logging errors without swallowing

```rust
if let Err(e) = background_task().await {
    tracing::error!(error = ?e, "background task failed");
    // decide: re-raise, retry, or continue
}
```

Never `let _ = ...` an error silently. Log it, propagate it, or comment why it's safe to ignore.

## `?` with `Option`

```rust
fn first_word(s: &str) -> Option<&str> {
    let w = s.split_whitespace().next()?;
    Some(w)
}
```

`?` on `Option` returns `None` early. Works in both `Option` and `Result` returning functions (with conversion).

## Common pitfalls

- `Box<dyn Error>` as the error type — loses type info; use `thiserror` enum
- Returning `String` as the error — no chain, no `errors::is`; use typed errors
- `.unwrap()` on `lock()` in async — poisons the mutex; use `?` with ` PoisonError` handling
- `?` between mismatched error types without `From` — won't compile; add `#[from]`
- Catching `panic` with `catch_unwind` — not for normal error handling; use `Result`
