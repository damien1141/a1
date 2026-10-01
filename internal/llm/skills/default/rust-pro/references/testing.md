# Testing in Rust

## Unit tests — `#[cfg(test)]`

```rust
// src/parser.rs
pub fn parse(s: &str) -> Option<i32> {
    s.parse().ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_valid() {
        assert_eq!(parse("42"), Some(42));
    }

    #[test]
    fn rejects_garbage() {
        assert_eq!(parse("abc"), None);
    }
}
```

Unit tests live in the same file, in a `mod tests` gated by `#[cfg(test)]`. They have access to private items.

## Integration tests — `tests/`

```rust
// tests/api.rs
use myapp::Client;

#[test]
fn client_connects() {
    let client = Client::new("http://localhost:8080");
    assert!(client.ping().is_ok());
}
```

`tests/*.rs` are separate crates that import your library as a dependency. They only see the public API.

## Doc tests

```rust
/// Adds two numbers.
///
/// # Examples
///
/// ```
/// use myapp::add;
/// assert_eq!(add(2, 3), 5);
/// ```
pub fn add(a: i32, b: i32) -> i32 { a + b }
```

`cargo test --doc` runs the code blocks in doc comments. Treat them as compile-checked examples — they catch API drift.

## `assert!` family

```rust
assert!(cond);
assert_eq!(a, b);           // equality (requires PartialEq + Debug)
assert_ne!(a, b);           // inequality
assert!(matches!(v, Some(42)));  // pattern match

// Custom message
assert!(x > 0, "x must be positive, got {x}");

// Panics with a fixed message
panic!("not yet implemented");
```

## `should_panic` — testing failure paths

```rust
#[test]
#[should_panic(expected = "port out of range")]
fn rejects_bad_port() {
    parse_port(-1).unwrap();
}
```

Use sparingly — prefer returning `Result` from the test:

```rust
#[test]
fn rejects_bad_port() -> Result<(), ParseError> {
    match parse_port(-1) {
        Err(ParseError::OutOfRange) => Ok(()),
        other => panic!("expected OutOfRange, got {other:?}"),
    }
}
```

## Test organization

```rust
#[cfg(test)]
mod tests {
    use super::*;

    mod parse {
        use super::*;
        #[test] fn valid() { /* ... */ }
        #[test] fn invalid() { /* ... */ }
    }

    mod serialize {
        use super::*;
        #[test] fn round_trip() { /* ... */ }
    }
}
```

Run a subset: `cargo test parse::valid`.

## `proptest` — property-based

```rust
use proptest::prelude::*;

proptest! {
    #[test]
    fn parses_any_int(input in -1000i32..1000) {
        let s = input.to_string();
        prop_assert_eq!(parse(&s), Some(input));
    }
}
```

`proptest` generates inputs and shrinks failures. Use for parsers, round-trip properties, edge discovery.

## `insta` — snapshot testing

```rust
#[test]
fn renders_markdown() {
    let html = render("# Hello");
    insta::assert_snapshot!(html);
}
```

`cargo insta review` to accept changes. Use for output that should change rarely (HTML, JSON, error messages).

## `mockall` — mock generation

```rust
#[automock]
trait Fetcher {
    fn fetch(&self, url: &str) -> Result<String, Error>;
}

#[test]
fn handles_fetch_error() {
    let mut mock = MockFetcher::new();
    mock.expect_fetch()
        .returning(|_| Err(Error::Network));
    let svc = Service::new(Arc::new(mock));
    assert!(svc.run().is_err());
}
```

## Benchmarks — `criterion`

```rust
// benches/sort.rs
use criterion::{criterion_group, criterion_main, Criterion};

fn bench_sort(c: &mut Criterion) {
    c.bench_function("sort 1000", |b| {
        b.iter(|| {
            let mut xs: Vec<i32> = (0..1000).collect();
            xs.sort();
        })
    });
}

criterion_group!(benches, bench_sort);
criterion_main!(benches);
```

`cargo bench` runs them. Criterion produces statistical comparisons; use for regression detection.

## Fuzzing — `cargo-fuzz`

```rust
// fuzz/fuzz_targets/parse.rs
#![no_main]
use libfuzzer_sys::fuzz_target;

fuzz_target!(|data: &[u8]| {
    if let Ok(s) = std::str::from_utf8(data) {
        let _ = myapp::parse(s);
    }
});
```

`cargo fuzz run parse`. Use for parsers, deserializers, anything that takes untrusted input.

## Async tests — `tokio::test`

```rust
#[tokio::test]
async fn fetches_concurrently() {
    let results = fetch_all(vec!["a".into(), "b".into()]).await.unwrap();
    assert_eq!(results.len(), 2);
}
```

`#[tokio::test]` creates a current-thread runtime per test. For multi-threaded: `#[tokio::test(flavor = "multi_thread")]`.

## Test fixtures — functions, not macros

```rust
fn test_db() -> Db {
    let db = Db::open(":memory:").unwrap();
    db.exec(SCHEMA).unwrap();
    db
}

#[test]
fn inserts() {
    let db = test_db();
    db.insert("a").unwrap();
    assert_eq!(db.count(), 1);
}
```

Use `Drop` for cleanup, or scope with a guard struct.

## Verification

```bash
cargo fmt --check
cargo clippy --all-targets --all-features -- -D warnings
cargo test                  # unit + integration
cargo test --doc            # doc tests
cargo bench                 # benchmarks
```

All must be green. Clippy catches most anti-patterns; tests catch the rest.
