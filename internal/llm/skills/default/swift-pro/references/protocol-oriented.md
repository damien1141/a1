# Protocol-Oriented Design

## Define capability protocols

```swift
protocol Repository<Entity> {
    associatedtype Entity: Identifiable
    func fetch(id: Entity.ID) async throws -> Entity
    func save(_ entity: Entity) async throws
}

struct UserRepository: Repository {
    typealias Entity = User
    func fetch(id: UUID) async throws -> User { /* ... */ }
    func save(_ user: User) async throws { /* ... */ }
}
```

Protocols with associated types (PATs) are Swift's interface abstraction. Use `associatedtype` when the impl picks the type; use generic parameters when the caller picks.

## Default implementations via extension

```swift
protocol Describable {
    var description: String { get }
}

extension Describable {
    var summary: String { "Item: \(description)" }
}

struct User: Describable {
    let name: String
    var description: String { name }
}

User(name: "Alice").summary  // "Item: Alice"
```

Extensions provide default behavior; conformers override when needed.

## `some` — opaque return type

```swift
func makeRepo() -> some Repository {
    UserRepository()
}
```

`some` returns a specific concrete type, hidden from the caller. The caller knows it satisfies `Repository` but not the concrete type. Faster than `any` (static dispatch).

## `any` — existential type

```swift
let repos: [any Repository] = [UserRepository(), OrderRepository()]

for repo in repos {
    let entity = try await repo.fetch(id: /* ... */)
}
```

`any Repository` is a box holding any conforming type. Use when you need heterogeneous collections — pays dynamic-dispatch cost.

## Generic functions vs `some`/`any`

```swift
// Generic — caller picks T
func process<R: Repository>(_ repo: R, id: R.Entity.ID) async throws -> R.Entity {
    try await repo.fetch(id: id)
}

// `some` — function picks, opaque to caller
func makeAndProcess(id: UUID) async throws -> User {
    let repo = UserRepository()
    return try await repo.fetch(id: id)
}

// `any` — heterogeneous collection
func processAll(_ repos: [any Repository]) async throws { /* ... */ }
```

Prefer generics for static dispatch; `some` for hiding impl details; `any` for runtime polymorphism.

## Type erasure — when `any` isn't enough

```swift
// Pre-Swift 5.7 — manual AnyRepository wrapper
// Post-Swift 5.7+ — use `any Repository` directly

let r: any Repository = UserRepository()
```

In Swift 5.7+, `any Protocol` works for almost all use cases. Manual `AnyFoo` wrappers are mostly unnecessary.

## Protocol composition

```swift
protocol Named { var name: String { get } }
protocol Aged { var age: Int { get } }

typealias Person = Named & Aged

func greet(_ p: Person) -> String { "Hi \(p.name), \(p.age)" }

struct User: Named, Aged {
    let name: String
    let age: Int
}
```

Combine small protocols; don't make one giant protocol.

## Sealed protocols (unofficial)

Swift doesn't have sealed protocols, but you can guide users by:
- Marking internal conformances as `fileprivate`
- Using `@testable import` only for tests
- Documenting "do not conform to this protocol"

## `@objc` protocols — only for Objective-C interop

```swift
@objc protocol LoginDelegate {
    func didLogin(user: User)
    @objc optional func didFail(error: Error)
}
```

Avoid in pure Swift code. `@objc` adds runtime overhead and limits to reference types and `@objc`-compatible methods.

## Value types + protocols

```swift
protocol Shape {
    func area() -> Double
}

struct Circle: Shape {
    let radius: Double
    func area() -> Double { .pi * radius * radius }
}

struct Square: Shape {
    let side: Double
    func area() -> Double { side * side }
}

let shapes: [any Shape] = [Circle(radius: 2), Square(side: 3)]
let total = shapes.reduce(0) { $0 + $1.area() }
```

Value types conforming to protocols give you polymorphism without reference semantics — no retain cycles, easy copying, thread-safe.

## Testing with protocols

```swift
protocol Fetcher {
    func fetch(_ id: String) async throws -> Data
}

struct LiveFetcher: Fetcher { /* real HTTP */ }
struct MockFetcher: Fetcher {
    var result: Result<Data, Error>
    func fetch(_ id: String) async throws -> Data { try result.get() }
}

class Service {
    let fetcher: any Fetcher
    init(fetcher: any Fetcher) { self.fetcher = fetcher }
}

// Tests inject MockFetcher; production injects LiveFetcher
```

Protocols make dependency injection natural — define the abstraction, inject the impl.

## Common pitfalls

- Protocol with `Self` references — can't be used as `any Protocol` (existential); use generics
- PAT stored in a property — pre-Swift 5.7, requires `any`; modern Swift handles it
- Massive protocols — split; small protocols compose
- Forgetting `associatedtype` constraints — `associatedtype Entity: Identifiable` adds useful bounds
- `@objc` on a Swift-only protocol — limits expressiveness; only for Objective-C interop
- Using classes for shared behavior — prefer protocol + struct composition
- Class inheritance where a protocol fits — inheritance is rigid; protocols compose
