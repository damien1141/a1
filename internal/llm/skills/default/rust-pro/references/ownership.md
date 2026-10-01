# Ownership, Borrows, Lifetimes

## The three ownership operations

```rust
fn take(s: String) { /* s dropped here */ }       // move
fn borrow(s: &String) { /* read */ }              // shared borrow
fn borrow_mut(s: &mut String) { s.push_str("!"); } // mutable borrow
```

- **Move** — ownership transfers; the original is no longer valid
- **Shared borrow (`&T`)** — many readers, no writers; `T: Copy` types are copied instead of moved
- **Mutable borrow (`&mut T`)** — one writer, no other borrows alive

## Two strikes rule

When the borrow checker rejects the same fix twice, stop iterating — the *shape* is wrong, not the syntax. Common shape problems:

- Self-referential structs (use `Pin<Box<T>>` or restructure)
- Holding a `MutexGuard` across `.await` (restructure to release before `.await`)
- Orphan trait implementations (wrap in a newtype)
- Cyclic references (use `Weak<T>` or an arena)

## Lifetimes

```rust
// Returned reference lives as long as both inputs
fn longest<'a>(x: &'a str, y: &'a str) -> &'a str {
    if x.len() > y.len() { x } else { y }
}

// Struct holding a borrow — lifetime on the struct
struct Excerpt<'a> { part: &'a str }

impl<'a> Excerpt<'a> {
    fn announce(&self, msg: &str) -> &'a str {
        println!("{msg}");
        self.part
    }
}
```

Lifetime elision rules (compiler fills these):
1. One input lifetime → output gets it
2. `&self`/`&mut self` method → output gets `self`'s lifetime
3. Otherwise, explicit annotations required

## Smart pointers — pick the right one

| Type | Use case |
|---|---|
| `Box<T>` | Heap, single owner |
| `Rc<T>` | Shared ownership, single-threaded |
| `Arc<T>` | Shared ownership, multi-threaded |
| `RefCell<T>` | Interior mutability, single-threaded (runtime borrow check) |
| `Mutex<T>` / `RwLock<T>` | Interior mutability, multi-threaded |
| `Cell<T>` | `Copy` types only, single-threaded, no borrow check |

```rust
use std::sync::Arc;
use std::sync::Mutex;

let counter = Arc::new(Mutex::new(0));
let c2 = Arc::clone(&counter);
std::thread::spawn(move || {
    *c2.lock().unwrap() += 1;
});
```

## `Cow` — borrow or clone

```rust
use std::borrow::Cow;

fn normalize(input: &str) -> Cow<str> {
    if input.contains('\t') {
        Cow::Owned(input.replace('\t', "    "))  // allocate
    } else {
        Cow::Borrowed(input)                      // no alloc
    }
}
```

Use `Cow` when most calls don't need to allocate but some do.

## `Pin` — for self-referential / immovable types

Mostly relevant for async. `async fn` returns a `Future` that may be self-referential; the runtime pins it before polling. You rarely write `Pin` manually — but you'll see `Pin<Box<dyn Future>>` and `Unpin` bounds.

```rust
// Box::pin a future for heap-allocated, self-referential state
let fut = Box::pin(async { 42 });
fut.await;
```

## Builder pattern (fallible construction)

```rust
pub struct Server { addr: String, port: u16 }

pub struct ServerBuilder { addr: Option<String>, port: Option<u16> }

impl ServerBuilder {
    pub fn new() -> Self { Self { addr: None, port: None } }
    pub fn addr(mut self, a: impl Into<String>) -> Self { self.addr = Some(a.into()); self }
    pub fn port(mut self, p: u16) -> Self { self.port = Some(p); self }
    pub fn build(self) -> Result<Server, BuildError> {
        Ok(Server {
            addr: self.addr.ok_or(BuildError::MissingAddr)?,
            port: self.port.unwrap_or(8080),
        })
    }
}

impl Default for ServerBuilder { fn default() -> Self { Self::new() } }
```

`impl Default for Builder` so callers can `Builder::default().addr(...).build()`.

## Interior mutability — `RefCell` for tests and mocks

```rust
struct MockLogger { messages: std::cell::RefCell<Vec<String>> }

impl MockLogger {
    fn new() -> Self { Self { messages: Default::default() } }
    fn messages(&self) -> Vec<String> { self.messages.borrow().clone() }
}

impl Logger for MockLogger {
    fn log(&self, msg: &str) { self.messages.borrow_mut().push(msg.into()); }
}
```

`RefCell::borrow_mut` panics on double-borrow — fine for tests, not for production hot paths.

## `Drop` and RAII

```rust
pub struct TempFile { path: std::path::PathBuf }

impl Drop for TempFile {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.path);
    }
}
```

RAII: resources tied to lifetimes. Prefer `Drop` over manual `close()` methods.

## Common pitfalls

- `String` vs `&str` — functions should take `&str`; return `String` only when you actually allocate
- `Vec<T>` vs `&[T]` — functions should take `&[T]`
- `Box<dyn Trait>` when generics fit — loses monomorphization, adds vtable indirection
- Holding `MutexGuard` across `.await` — deadlock; clone the data out first
- `Rc<T>` across threads — won't compile; use `Arc<T>`
- Forgetting `impl Default for Builder` — ergonomic regression
- `unwrap()` on `lock()` — fine in tests, use `?` in production (poisoned mutex is a real failure mode)
