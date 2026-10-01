# Memory & Performance

## ARC — Automatic Reference Counting

Swift uses ARC for reference types (`class`, `actor`, closures). The compiler inserts `retain`/`release` calls. You must avoid retain cycles.

```swift
class Node {
    var next: Node?  // strong — risk of cycle
    weak var parent: Node?  // weak — breaks cycle
}

let parent = Node()
let child = Node()
child.parent = parent
parent.next = child  // weak parent breaks the cycle
```

## `weak` vs `unowned`

| Modifier | Use when |
|---|---|
| `weak` | Reference may become nil (outlives the owner) |
| `unowned` | Reference is guaranteed to outlive the owner (won't be nil) |
| `unowned(safe)` | Default — traps on access to dead ref |
| `unowned(unsafe)` | No check — UB if accessed after death |

```swift
class View {
    weak var delegate: Delegate?  // delegate may outlive View
}

class Task {
    unowned let owner: Owner  // owner guaranteed to outlive Task
}
```

## Closures and retain cycles

```swift
class Service {
    var onComplete: (() -> Void)?

    func start() {
        onComplete = { /* captures self strongly — cycle if Service holds closure */ }
    }
}

// Break with [weak self]
onComplete = { [weak self] in
    guard let self else { return }
    self.finish()
}
```

Closures capture by reference. `[weak self]` / `[unowned self]` breaks cycles when the closure is stored long-term.

Short-lived closures (passed to a function, called synchronously) don't need `weak self`:
```swift
URLSession.shared.dataTask(with: url) { data, _, _ in
    self.handle(data)  // OK — closure is short-lived
}
```

## Value types — value semantics

```swift
struct User {
    var name: String
    var age: Int
}

var a = User(name: "Alice", age: 30)
var b = a       // copied
b.age = 31
print(a.age)    // 30 — a unchanged
```

Value types copy on assignment — no shared mutable state, no retain cycles, thread-safe by default. Prefer `struct` over `class` for models.

## Reference types — when classes make sense

- Identity matters (two `User` instances with same data are different users)
- Shared mutable state (an `actor` or `class` with sync)
- Objective-C interop
- Subclass hierarchy (rare; prefer protocols)

## Copy-on-write — `String`, `Array`, `Dictionary`, `Set`

```swift
var a = [1, 2, 3]
var b = a       // shares storage
b.append(4)     // b copies, a unchanged
```

Stdlib collections share storage until mutation. This is invisible to you — value semantics preserved.

For your own types, implement COW manually:
```swift
struct Box {
    private var ref: NSMutableData  // reference type inside

    var data: Data {
        get { ref as Data }
        set { if !isKnownUniquelyReferenced(&ref) { ref = NSMutableData(data: newValue) } else { ref.setData(newValue) } }
    }
}
```

`isKnownUniquelyReferenced` checks if the inner ref is uniquely owned — if so, mutate in place; otherwise copy.

## Instruments — profile before optimizing

- **Allocations** — heap growth, retain cycles
- **Leaks** — cycle detection
- **Time Profiler** — CPU hot spots
- **Swift Concurrency** — task graph, actor hops

Run Instruments on a real device; the simulator doesn't reflect real performance.

## Avoiding allocations in hot paths

- `String` → `Substring` for slices (no allocation)
- `Array` → pre-allocate with `reserveCapacity`
- `Array<T>` → `ContiguousArray<T>` for homogeneous, perf-critical
- `Dictionary` lookups — `if let v = dict[key]` (one hash) over `dict[key] != nil` then `dict[key]!` (two hashes)
- `withUnsafeBufferPointer` for zero-copy interop with C

## `@inlinable` / `@usableFromInline`

```swift
@inlinable
func square(_ x: Int) -> Int { x * x }
```

`@inlinable` exposes the body across module boundaries for the optimizer to inline. Use for tiny hot-path functions in libraries.

## Concurrency performance

- `actor` — serial; multiple `await`s serialize
- For read-heavy shared state, consider `let` immutable (no isolation needed) or a partitioned actor pool
- `Task` per work item is cheap, but spawning millions is not — use `TaskGroup` with bounded fan-out
- `async let` for 2-3 fixed concurrent ops; `TaskGroup` for N

## Common pitfalls

- `var delegate: Delegate?` (strong) in a view holding a controller holding the view → cycle; use `weak`
- Closures stored long-term capturing `self` strongly → cycle; use `[weak self]`
- `unowned` when the lifetime isn't actually guaranteed → crash
- `class` for a model with no identity → `struct` is simpler and safer
- Forgetting `Equatable`/`Hashable` on value types → breaks `Set`/`Dictionary` keys; auto-synthesize with `Hashable` conformance
- `Array.filter { ... }.map { ... }` allocating twice — chain with `lazy` if you can't afford the intermediate
- `String` indexing is O(n) — `Array<String>` or `Data` for byte-level work
