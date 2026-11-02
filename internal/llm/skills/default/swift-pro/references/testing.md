# Testing in Swift

## XCTest — async tests

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

    func testFetchAll_concurrent() async throws {
        let urls = [URL(string: "https://x/1")!, URL(string: "https://x/2")!]
        let results = try await fetchAll(urls)
        XCTAssertEqual(results.count, 2)
    }
}
```

Mark `async` for async work; `@MainActor` if the SUT requires the main thread. XCTest awaits the function automatically.

## Setup / teardown

```swift
final class RepoTests: XCTestCase {
    var repo: UserRepository!

    override func setUp() async throws {
        try await super.setUp()
        repo = try await UserRepository.makeTest()
    }

    override func tearDown() async throws {
        repo = nil
        try await super.tearDown()
    }
}
```

Prefer `setUp async throws` (async variant) over the sync version when setup is async.

## `XCTestExpectation` — for callback-based async

```swift
func testLegacyCallback() {
    let exp = expectation(description: "callback fired")
    legacyFetch { result in
        XCTAssertEqual(result, "ok")
        exp.fulfill()
    }
    wait(for: [exp], timeout: 1.0)
}
```

For modern `async` code, this is unnecessary — just `await`.

## Swift Testing (`.expect`, `#expect`) — modern alternative

```swift
import Testing
@testable import MyApp

@Suite struct CounterTests {
    @Test func increment() {
        let m = CounterModel()
        m.increment()
        #expect(m.count == 1)
        #expect(m.history == [1])
    }

    @Test(arguments: [1, 5, 100])
    func increment_repeated(times: Int) {
        let m = CounterModel()
        for _ in 0..<times { m.increment() }
        #expect(m.count == times)
    }
}
```

Swift Testing (introduced 2024) is the modern alternative to XCTest:
- `#expect` over `XCTAssertEqual`
- `@Suite` / `@Test` over classes and methods
- `@Test(arguments:)` for parametric tests
- Better failure messages

Both work; XCTest is still the default in Xcode-generated files.

## Mocking — protocols over mock frameworks

```swift
protocol Fetcher {
    func fetch(_ id: String) async throws -> Data
}

struct MockFetcher: Fetcher {
    var result: Result<Data, Error>
    func fetch(_ id: String) async throws -> Data { try result.get() }
}

let svc = Service(fetcher: MockFetcher(result: .success(Data())))
```

Swift mocking libraries (Mockingbird, SwiftMock) exist, but a hand-rolled `MockFetcher` is often clearer. Define protocols for dependencies, inject mocks.

## Snapshot testing — `swift-snapshot-testing`

```swift
import SnapshotTesting

class ViewTests: XCTestCase {
    func testCounterView() {
        let view = CounterView()
        assertSnapshot(of: view, as: .image(layout: .device(config: .iPhone13)))
    }
}
```

Snapshot tests catch UI regressions. `assertSnapshot` writes a PNG; on mismatch, you review and accept with `record: true`.

## UI tests — `XCUITest`

```swift
import XCTest

final class LoginUITests: XCTestCase {
    func testLogin() throws {
        let app = XCUIApplication()
        app.launch()

        app.textFields["Email"].tap()
        app.textFields["Email"].typeText("a@b.c")
        app.secureTextFields["Password"].tap()
        app.secureTextFields["Password"].typeText("secret")
        app.buttons["Login"].tap()

        XCTAssertTrue(app.staticTexts["Welcome"].waitForExistence(timeout: 2))
    }
}
```

UI tests run the app in a simulator and drive it via accessibility identifiers. Slow; use sparingly for critical flows.

## Performance tests

```swift
func testSortPerformance() throws {
    let data = (0..<10_000).shuffled()
    measure {
        _ = data.sorted()
    }
}
```

`measure` runs the block multiple times and reports stats. `measureMetrics([.clock, .cpu])` for finer detail. Set a baseline; CI fails on regression.

## Test structure — Arrange / Act / Assert

```swift
@Test func create_throws_on_empty_name() {
    // Arrange
    let req = CreateRequest(name: "", price: 10)

    // Act + Assert
    #expect(throws: ValidationError.self) {
        try Product.create(req)
    }
}
```

## Tests for SwiftUI views

```swift
import XCTest
@testable import MyApp

final class CounterViewTests: XCTestCase {
    @MainActor
    func testIncrement_updatesDisplay() {
        let model = CounterModel()
        let view = CounterView(model: model)
        // Use ViewInspector or hosting controller for UI assertions
        // Or test the model directly — easier
        model.increment()
        XCTAssertEqual(model.count, 1)
    }
}
```

UI logic is best tested by extracting it into the `@Observable` model and testing the model. Use `ViewInspector` for structural view assertions.

## Common pitfalls

- `Task { }` fire-and-forget in tests — test exits before the task completes; `await` the task or use `XCTestExpectation`
- `setUp` (sync) instead of `setUp async throws` — can't call async setup
- `XCTAssertEqual` on floating-point — use `XCTAssertEqual(a, b, accuracy: 0.001)`
- Tests that depend on order — JUnit-style; Swift doesn't guarantee order
- `try!` in tests — fine if you want a crash on test setup failure; otherwise `try`
- Snapshot tests on CI without fixing simulator — fonts/screenshots vary by simulator
- Hardcoded dates in tests — use a clock protocol and inject a fake
