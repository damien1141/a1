# Async with tokio

## The runtime

```rust
#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let data = fetch("https://x").await?;
    println!("{data}");
    Ok(())
}
```

For multi-threaded runtime: `#[tokio::main(flavor = "multi_thread", worker_threads = 4)]`.

## `async fn` and `Future`

`async fn` returns `impl Future<Output = T>`. Futures are **lazy** — they don't run until polled (i.e. `.await`ed or spawned).

```rust
async fn fetch(url: &str) -> anyhow::Result<String> {
    let body = reqwest::get(url).await?.text().await?;
    Ok(body)
}
```

## Concurrent fan-out

```rust
use futures::future;

let results = future::try_join_all(urls.iter().map(|u| fetch(u))).await?;
// try_join_all: fail-fast on first Err; returns Vec<T>

let (a, b, c) = future::try_join3(fetch_a(), fetch_b(), fetch_c()).await?;
// try_join3 / join3: fixed arity, returns tuple
```

For bounded concurrency:

```rust
use futures::stream::{self, StreamExt};

let results: Vec<_> = stream::iter(urls)
    .map(|u| fetch(u))
    .buffer_unordered(8)   // up to 8 in flight
    .collect()
    .await;
```

## `tokio::spawn` — detached tasks

```rust
let handle: tokio::task::JoinHandle<()> = tokio::spawn(async {
    background_work().await;
});

// later
handle.await?;
```

`spawn`ed tasks outlive the spawning future (until the runtime shuts down). They are not cancelled when the parent is — that's a feature and a footgun.

## `select!` — race and cancel

```rust
tokio::select! {
    result = fetch(url) => {
        match result {
            Ok(data) => println!("{data}"),
            Err(e) => eprintln!("{e}"),
        }
    }
    _ = tokio::time::sleep(Duration::from_secs(2)) => {
        eprintln!("timeout");
    }
}
```

`select!` polls all branches; the first to complete wins, and **the others are dropped (cancelled)**. Cancellation safety matters — only use cancellation-safe futures in `select!` branches.

Cancellation-safe: `tokio::time::sleep`, `tokio::sync::mpsc::Receiver::recv`, `tokio::io::AsyncRead` reads.
Not cancellation-safe: arbitrary `async fn` that may have started a side effect mid-await.

## `spawn_blocking` for CPU/blocking work

```rust
let hash = tokio::task::spawn_blocking(move || {
    expensive_hash(&data)
}).await?;
```

Use `spawn_blocking` for:
- Synchronous I/O (`std::fs`, blocking DB drivers)
- CPU-heavy computation (crypto, parsing huge files)

The async executor threads stay responsive because the blocking work runs on a separate thread pool.

## Channels

| Channel | Use |
|---|---|
| `tokio::sync::mpsc` | Multi-producer, single-consumer (most common) |
| `tokio::sync::broadcast` | Multi-producer, multi-consumer (fan-out) |
| `tokio::sync::oneshot` | One value, one-shot (request/response) |
| `tokio::sync::watch` | Single-value, multi-consumer (config updates) |

```rust
let (tx, mut rx) = tokio::sync::mpsc::channel(100);

tokio::spawn(async move {
    while let Some(msg) = rx.recv().await {
        process(msg).await;
    }
});

tx.send("hello").await?;
```

Prefer channels over `Arc<Mutex<T>>` for shared state — they compose with cancellation and avoid lock contention.

## Graceful shutdown

```rust
use tokio::signal;

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let server = spawn_server();
    signal::ctrl_c().await?;
    server.shutdown().await?;
    Ok(())
}
```

For HTTP servers, use `axum::serve(...).with_graceful_shutdown(...)`.

## Mutexes across `.await`

```rust
// WRONG — holds lock across await, risks deadlock under contention
let mut data = state.lock().unwrap();
process(&mut data).await;  // lock held during await

// RIGHT — clone or scope the lock
let snapshot = {
    let data = state.lock().unwrap();
    data.clone()
};
process_owned(snapshot).await;
```

Or use `tokio::sync::Mutex` (designed for async), but std `Mutex` is usually better for short critical sections.

## Cancellation safety checklist

A future is cancellation-safe if dropping it mid-execution leaves the system in a consistent state:
- No half-written bytes left in a stream
- No `Mutex` permanently locked
- No `oneshot` sender dropped before the receiver gets a value it expected
- Database transaction rolled back, not committed half-way

If a future is not cancellation-safe, never put it in `select!` or `tokio::time::timeout`.

## Common pitfalls

- `block_on` inside an async context — panics; restructure or use `spawn_blocking`
- `std::sync::Mutex` held across `.await` — may deadlock; release before awaiting
- `tokio::spawn` without storing the `JoinHandle` — errors are silently lost; use `tokio::task::spawn` with proper error handling
- `Future` returned but never `.await`ed — does nothing; `#[must_use]` warns but doesn't error
- `.unwrap()` on `JoinHandle::await` — task may have panicked; use `?` and propagate
- Forgetting `tokio::main` on `main` — `async fn main` won't run without the macro
