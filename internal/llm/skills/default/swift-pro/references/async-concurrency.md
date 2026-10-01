# Async & Concurrency in Swift

## `async`/`await` — the default

```swift
func fetchUser(id: String) async throws -> User {
    let url = URL(string: "https://api.example.com/users/\(id)")!
    let (data, resp) = try await URLSession.shared.data(from: url)
    guard let http = resp as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
        throw URLError(.badServerResponse)
    }
    return try JSONDecoder().decode(User.self, from: data)
}
```

Rules:
- `async` functions can `await` other async functions
- `async throws` for fallible async work — `try await`
- Top-level await requires `@main struct` with `async` main, or `Task { }`
- Don't wrap existing async APIs with `withCheckedThrowingContinuation` if a native `async` version exists

## Structured concurrency — `TaskGroup`

```swift
func fetchAll(_ urls: [URL]) async throws -> [Data] {
    try await withThrowingTaskGroup(of: Data.self) { group in
        for url in urls {
            group.addTask { try await fetch(url) }
        }
        var results: [Data] = []
        for try await data in group {
            results.append(data)
        }
        return results
    }
}
```

`withThrowingTaskGroup` cancels all children on first throw. `withTaskGroup` (non-throwing) for parallel work that can't fail.

## `async let` — concurrent tuple

```swift
async let user = fetchUser(id)
async let posts = fetchPosts(userId: id)
let (u, p) = try await (user, posts)
```

`async let` starts a child task; awaiting both runs them concurrently. Use for fixed arity.

## `Task` — detached vs inherited

```swift
// Inherits actor context and task values
Task { /* runs on caller's actor */ }

// Detached — fresh context, no inheritance
Task.detached(priority: .userInitiated) { /* runs on global executor */ }
```

Prefer `Task { }` over `Task.detached { }` unless you specifically need to escape the current actor.

## Cancellation

```swift
// Cooperative — check periodically
func process() async {
    for chunk in chunks {
        if Task.isCancelled { return }
        await process(chunk)
    }
}

// Or throw on cancellation
func process() async throws {
    for chunk in chunks {
        try Task.checkCancellation()
        await process(chunk)
    }
}
```

Cancellation is cooperative — Swift doesn't forcibly stop tasks. `Task.checkCancellation()` throws `CancellationError`.

## Actors — isolated shared state

```swift
actor Counter {
    private var value = 0

    func increment() { value += 1 }
    func current() -> Int { value }
}

let c = Counter()
Task { await c.increment(); print(await c.current()) }
```

All access to actor state goes through `await`. The actor serializes access — no data races.

## `@globalActor` — `@MainActor`

```swift
@MainActor
final class ViewModel {
    var title = ""  // main-actor-isolated
}

@MainActor
func updateUI() { /* main thread */ }
```

`@MainActor` marks code that must run on the main thread (UI updates). Swift Concurrency enforces this at compile time — no more `DispatchQueue.main.async`.

## `Sendable` — concurrency-safe types

```swift
struct User: Sendable {  // value type with all-Sendable fields — automatic
    let id: UUID
    let name: String
}

final class Cache: @unchecked Sendable {  // opt out — you promise thread-safety
    private let lock = NSLock()
    private var items: [String: Data] = [:]

    func set(_ key: String, _ data: Data) {
        lock.lock(); defer { lock.unlock() }
        items[key] = data
    }
}
```

`Sendable` types can cross actor boundaries safely:
- `struct`/`enum` with all-Sendable fields — automatic
- `final class` with immutable fields — automatic
- `actor` — always Sendable
- Other classes — opt out with `@unchecked Sendable` (only with manual synchronization)

## `nonisolated` — escape the actor for stateless methods

```swift
actor Bank {
    private var balances: [Account: Decimal] = [:]

    nonisolated func description() -> String { "Bank" }  // no actor access needed

    func balance(of account: Account) -> Decimal {
        balances[account, default: 0]
    }
}
```

`nonisolated` lets `Sendable`-safe methods be called without `await`.

## Async sequences

```swift
for try await event in eventSource {
    handle(event)
}

// Custom AsyncSequence
struct Counter: AsyncSequence {
    struct AsyncIterator: AsyncIteratorProtocol {
        var current: Int
        let max: Int
        mutating func next() async -> Int? {
            guard current < max else { return nil }
            defer { current += 1 }
            return current
        }
    }

    let start: Int
    let max: Int
    func makeAsyncIterator() -> AsyncIterator {
        AsyncIterator(current: start, max: max)
    }
}
```

Use for paginated APIs, file lines, server-sent events, Combine replacements.

## Bridge legacy callback APIs

```swift
func legacyFetch(_ url: URL, completion: @escaping (Data?, Error?) -> Void)

// Wrap with continuation — only when no native async version exists
func fetch(_ url: URL) async throws -> Data {
    try await withCheckedThrowingContinuation { cont in
        legacyFetch(url) { data, error in
            if let data { cont.resume(returning: data) }
            else { cont.resume(throwing: error ?? URLError(.unknown)) }
        }
    }
}
```

Call `cont.resume` exactly once — calling twice crashes; never calling leaks.

## Common pitfalls

- `Task { }` fire-and-forget without storing the handle — can't cancel, errors silently lost
- Calling `Task.detached` from `@MainActor` to "do work in background" — usually unnecessary; `Task { }` already runs off-main unless the caller is on `@MainActor`
- Forgetting `await` on actor methods — compile error, but easy to miss the fix
- `@MainActor` on everything — over-isolation hurts concurrency; mark only true UI code
- `@unchecked Sendable` without manual locking — silent data races
- Holding a `Mutex` or lock across `await` — deadlock risk
- Mixing `DispatchQueue` and Swift Concurrency — pick one; prefer Concurrency for new code
- `AsyncSequence` that never ends — caller's `for await` never returns; ensure termination
