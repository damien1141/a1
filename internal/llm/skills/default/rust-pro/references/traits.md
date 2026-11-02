# Traits

## Basic trait

```rust
pub trait Drawable {
    fn draw(&self);
}

impl Drawable for Circle {
    fn draw(&self) { /* ... */ }
}
```

## Default methods

```rust
pub trait Describable {
    fn describe(&self) -> String { String::from("(no description)") }
}
```

Override only when needed.

## Associated types

```rust
pub trait Container {
    type Item;
    fn add(&mut self, item: Self::Item);
    fn get(&self, i: usize) -> Option<&Self::Item>;
}

impl<T> Container for Vec<T> {
    type Item = T;
    fn add(&mut self, item: T) { self.push(item); }
    fn get(&self, i: usize) -> Option<&T> { self.as_slice().get(i) }
}
```

Use associated types when there's one canonical `Item` per impl. Use generic parameters when the same impl could serve multiple `Item`s.

## Trait bounds

```rust
// Inline bound
fn sum<T: Iterator<Item = i64>>(it: T) -> i64 { it.sum() }

// where clause (preferred for complex bounds)
fn process<T>(xs: T) -> i64
where
    T: IntoIterator<Item = i64>,
{
    xs.into_iter().sum()
}
```

## Object safety (`dyn Trait`)

A trait is object-safe if:
- No `Self` in method signatures (return or param)
- No generics on methods
- All methods are dispatchable (no `where Self: Sized` escape)

```rust
pub trait Logger: Send + Sync {
    fn log(&self, msg: &str);  // object-safe
}

// Box<dyn Logger> works because Logger is object-safe
pub struct App { logger: Box<dyn Logger> }
```

`impl Trait` for static dispatch, `dyn Trait` for runtime polymorphism. Default to generics; reach for `dyn` when:
- You have many implementations and binary size matters
- You need to store heterogeneous types in a collection
- A plugin system requires runtime registration

## Sealed traits

Prevent downstream crates from implementing your trait:

```rust
mod private { pub trait Sealed {} }

pub trait MyTrait: private::Sealed {
    fn do_thing(&self);
}

// Only types you bless can implement MyTrait
impl private::Sealed for i32 {}
impl MyTrait for i32 {
    fn do_thing(&self) { /* ... */ }
}
```

Use sealed traits for crate-public APIs that shouldn't be extended by users.

## Generic associated types (GATs)

```rust
pub trait Lender {
    type Lend<'a> where Self: 'a;
    fn lend<'a>(&'a self) -> Self::Lend<'a>;
}

impl Lender for Vec<i32> {
    type Lend<'a> = &'a [i32] where Self: 'a;
    fn lend<'a>(&'a self) -> &'a [i32] { self.as_slice() }
}
```

GATs enable borrowing-associated types (e.g. `Iterator`-like patterns, lending iterators).

## `impl Trait` in return position

```rust
pub fn map<I, F, B>(it: I, f: F) -> impl Iterator<Item = B>
where
    I: Iterator,
    F: FnMut(I::Item) -> B,
{
    it.map(f)
}
```

`impl Trait` hides the concrete type. Use it for closures and complex iterator chains. For public APIs, prefer named types or `Box<dyn Trait>` if the return is unbounded.

## Trait objects and lifetimes

```rust
pub struct Registry<'a> {
    items: Vec<Box<dyn Display + 'a>>,
}
```

Default lifetime for `dyn Trait` is `'static`. To store borrowed trait objects, add an explicit lifetime.

## `From`/`Into` for conversions

```rust
impl From<i32> for MyId {
    fn from(v: i32) -> Self { MyId(v) }
}

fn take_id(id: impl Into<MyId>) {
    let id: MyId = id.into();
    // ...
}

take_id(42);  // works via From
```

Implement `From`, get `Into` for free.

## Newtype pattern

```rust
pub struct UserId(pub i32);  // newtype

impl UserId {
    pub fn new(v: i32) -> Self { Self(v) }
}

// Can implement traits for UserId even if i32 already implements them
impl std::fmt::Display for UserId {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(f, "user:{}", self.0)
    }
}
```

Newtypes give type safety without runtime cost — distinct `UserId` vs `OrderId` even though both are `i32`.

## Common pitfalls

- `dyn Trait` for hot paths — vtable indirection hurts; prefer generics
- Trait with `Self` in signature — breaks object safety
- Massive trait with 20 methods — split into smaller traits
- Forgetting `Send + Sync` bounds — breaks `Arc<T>` usage
- `impl Trait` in arguments when a generic is clearer — `fn f<T: Trait>(x: T)` is more flexible than `fn f(x: impl Trait)` (allows turbofish)
