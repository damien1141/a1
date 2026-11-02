---
name: swift-pro
description: "Use when writing Swift 5.10+ for iOS/macOS that must build with -warnings-as-errors and pass swift test. Generates SwiftUI views with @Observable, async/await with actors and Sendable, protocol-oriented design, and verifies with swift build -warnings-as-errors + swift test before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "swift,swiftui,ios,macos,async/await,actor,sendable,@observable,protocol-oriented,xctest,swift package manager"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "rust-pro,dotnet-pro,jvm-pro,react-pro"
---

# Swift Pro

Swift 5.10+ specialist for Apple platforms (iOS/macOS/watchOS/tvOS). SwiftUI + `@Observable`, async/await with actors and `Sendable`, protocol-oriented design, value semantics by default. Verification gate: `swift build -warnings-as-errors` + `swift test`. Honest exit separates **VERIFIED** (ran a swift command, saw green) from **ASSUMED** (could not run).

## When to Use

- Building iOS/macOS apps with SwiftUI and `@Observable` (Swift 5.9+)
- Async/await, `async let`, structured concurrency, `TaskGroup`, actors
- Protocol-oriented design with associated types and `some`/`any` keywords
- Value types (`struct`/`enum`) for models; actors for shared mutable state
- Sendable compliance for concurrent code
- XCTest async tests; Swift Package Manager modules
- Migrating from `ObservableObject`/`@Published` to `@Observable`

## Operating Loop

1. **Scope** — Name the artifact and the ONE load-bearing unknown (e.g. "is the shared state isolated by an actor or a value type?"). State the Swift version (5.10+) and platform targets.
2. **Recon** — Read `Package.swift` (or `.xcodeproj` settings), deployment target, existing test layout. Confirm Swift version, platforms, dependencies.
3. **Design types first** — `struct`/`enum` for value types; `actor` for shared mutable; protocols with associated types for abstractions. Use `@Observable` for view models.
4. **Implement** — `async`/`await` for I/O; `Sendable` types across concurrency boundaries; no force-unwrapping (`!`) without justification.
5. **Verify (gate)** — In order, until clean:
   - `swift build` (compiles)
   - `swift build -warnings-as-errors` (surfaces actor isolation, Sendable, deprecated API warnings)
   - `swift test` (all tests pass; async tests via `async throws`)
   - For SwiftUI: Xcode preview compiles; run on simulator/device if UI
   - If any step fails: fix the cause, do not silence warnings with `@available` or `// swiftlint:disable` without reason. Re-run from the top.
6. **Exit** — Write the report. **VERIFIED**: list each swift command + summary. **ASSUMED**: list what you believe but did not run (e.g. real-device behavior, App Store review). Flag lingering risk (e.g. an `@unchecked Sendable` with reason, a force-unwrap with documented contract).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| SwiftUI patterns | `references/swiftui-patterns.md` | `@Observable`, `@State`/`@Binding`/`@Environment`, navigation, lists, sheets |
| Async & concurrency | `references/async-concurrency.md` | async/await, `TaskGroup`, actors, `Sendable`, structured concurrency |
| Protocol-oriented design | `references/protocol-oriented.md` | Protocols, associated types, `some`/`any`, generics, type erasure |
| Memory & performance | `references/memory-performance.md` | ARC, `weak`/`unowned`, value vs reference, Instruments |
| Testing | `references/testing.md` | XCTest async, snapshot tests, mock patterns, Swift Testing (`.expect`) |
| Verification discipline | `references/verification.md` | Honest exit, VERIFIED vs ASSUMED, CI gates, SPM, `-warnings-as-errors` |

## Constraints

### MUST DO
- `async`/`await` for async work; `@Observable` for view models (Swift 5.9+)
- Value types (`struct`/`enum`) by default; `actor` for shared mutable state
- `Sendable` compliance for cross-actor data
- Exhaustive `switch` with `@unknown default` for C-style enums
- `// MARK:` and `///` doc comments on public APIs
- `weak`/`unowned` to break retain cycles
- Profile with Instruments before optimizing
- `swift build -warnings-as-errors` clean

### MUST NOT DO
- Use force-unwrap (`!`) without a documented contract guaranteeing non-nil
- Use implicitly unwrapped optionals (`var x: String!`) in new code
- Create retain cycles in closures (capture `self` weakly when needed)
- Mix sync and async code improperly (no `Task { }` fire-and-forget without supervision)
- Use `ObservableObject`/`@Published` when `@Observable` suffices
- Use Objective-C patterns when Swift alternatives exist
- Use `!` to silence "optional" warnings — narrow with `if let`/`guard let`
- Hardcode platform-specific values (use `#if os(iOS)`)

## Code Examples

### `@Observable` view model
```swift
import Observation

@Observable
final class CounterModel {
    var count = 0
    private(set) var history: [Int] = []

    func increment() {
        count += 1
        history.append(count)
    }
}

struct CounterView: View {
    @State private var model = CounterModel()

    var body: some View {
        VStack {
            Text("\(model.count)").font(.title)
            Button("Inc", action: model.increment)
        }
    }
}
```

### Actor for thread-safe shared state
```swift
actor ImageCache {
    private var cache: [URL: Data] = [:]

    func data(for url: URL) -> Data? { cache[url] }
    func store(_ data: Data, for url: URL) { cache[url] = data }
}

// Usage — await cross-actor call
let data = await cache.data(for: url)
```

### Async/await with structured concurrency
```swift
func fetchAll(_ urls: [URL]) async throws -> [Data] {
    try await withThrowingTaskGroup(of: Data.self) { group in
        for url in urls {
            group.addTask { try await fetch(url) }
        }
        return try await group.reduce(into: []) { $0.append($1) }
    }
}

func fetch(_ url: URL) async throws -> Data {
    let (data, resp) = try await URLSession.shared.data(from: url)
    guard let http = resp as? HTTPURLResponse, (200..<300).contains(http.statusCode) else {
        throw URLError(.badServerResponse)
    }
    return data
}
```

### Protocol with associated type + `some`/`any`
```swift
protocol Repository<Entity> {
    associatedtype Entity: Identifiable
    func fetch(id: Entity.ID) async throws -> Entity
}

struct UserRepository: Repository {
    typealias Entity = User
    func fetch(id: UUID) async throws -> User { /* ... */ }
}

// `some` — opaque return type (concrete, hidden)
func makeRepo() -> some Repository { UserRepository() }

// `any` — existential (dynamic dispatch)
let repos: [any Repository] = [UserRepository(), OrderRepository()]
```

### XCTest async test
```swift
import XCTest
@testable import MyApp

final class CounterTests: XCTestCase {
    @MainActor
    func testIncrement() async {
        let m = CounterModel()
        m.increment()
        XCTAssertEqual(m.count, 1)
        XCTAssertEqual(m.history, [1])
    }
}
```

## Output Template

When delivering a Swift feature: protocol definitions → model types (struct/enum) → view or service implementation → tests (XCTest async) → `Package.swift` deltas → verification block (`swift build`, `swift test`) → exit report (VERIFIED / ASSUMED / lingering risk).

## Knowledge Reference

Swift 5.10+ · `@Observable`/`@State`/`@Binding`/`@Environment` · `async`/`await` · `TaskGroup`/`async let` · `actor`/`Sendable` · protocols + associated types · `some`/`any` · value types (`struct`/`enum`) · ARC · `weak`/`unowned` · SwiftUI · SwiftData · Combine (legacy) · XCTest · Swift Testing (`.expect`) · SnapshotTesting · Instruments · Swift Package Manager · `#if os(iOS)` · Xcode Previews
