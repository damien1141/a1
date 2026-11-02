# Verification Discipline — Honest Exit

## The rule

Every implementation report ends with a block separating **VERIFIED** from **ASSUMED**.

```
VERIFIED:
  - swift build                       → Build complete
  - swift build -warnings-as-errors   → Build complete
  - swift test
      Test Suite 'All tests' passed at ...
      Executed 42 tests, with 0 failures
ASSUMED:
  - Real-device performance (simulator only in CI)
  - App Store review (privacy manifest, entitlements)
  - SwiftUI on iOS 16 (CI tests on iOS 17+)
Lingering risk:
  - @unchecked Sendable on src/Cache.swift (reason: NSLock-protected mutable state)
```

## VERIFIED — what counts

Only what you ran and saw:

- `swift build` (compiles)
- `swift build -warnings-as-errors` (no warnings)
- `swift test` with pass/fail counts
- A snapshot test you ran and reviewed

## ASSUMED — what to flag

- Real-device behavior (simulator doesn't reflect performance, GPU, sensors)
- App Store review (privacy, entitlements, App Sandbox)
- Cross-platform behavior you didn't run (iOS if you only tested macOS)
- SwiftUI on older OS versions
- Memory behavior under low-memory conditions
- Push notification delivery (APNs sandbox only)

## The verification loop

```bash
swift build
swift build -warnings-as-errors
swift test
# For Xcode projects:
xcodebuild -scheme MyApp -destination 'platform=iOS Simulator,name=iPhone 15' build test
```

If a step fails:

1. Read the error in full
2. Fix the root cause (not the symptom)
3. Re-run from the top — an earlier step may now break

Never:
- `// swiftlint:disable` without a reason
- `@available(*, deprecated)` to silence a warning — fix the API
- `try!` to silence an error — handle it
- Skip a test with `XCTSkip` to go green

## When you cannot run a gate

If the iOS simulator isn't available or signing is broken:

```
ASSUMED:
  - UI tests skipped (no simulator); unit tests green
  - Code signing not verified (no provisioning profile)
```

Do not silently omit the gate.

## CI parity

CI should run exactly what you ran locally, plus anything you couldn't:

```yaml
- uses: maxim-lobanov/setup-xcode@v1
  with: { xcode-version: '15.3' }
- run: swift build -warnings-as-errors
- run: swift test --parallel
- run: xcodebuild test -scheme MyApp -destination 'platform=iOS Simulator,name=iPhone 15'
```

For matrix testing across OS versions:
```yaml
strategy:
  matrix:
    destination:
      - 'platform=iOS Simulator,name=iPhone 15,OS=17.4'
      - 'platform=iOS Simulator,name=iPhone 15,OS=16.4'
```

## `-warnings-as-errors` — what it catches

- Unused variables / imports
- Deprecation warnings (`@available(*, deprecated)`)
- Actor isolation violations
- `Sendable` non-conformance
- Implicit captures in escaping closures
- Deprecated SwiftUI APIs (`NavigationView`, `ObservableObject` in some contexts)

Run with `-warnings-as-errors` always; warnings hide bugs.

## `@unchecked Sendable` audit

```bash
grep -rn "@unchecked Sendable" Sources/
```

Every `@unchecked Sendable` should have a `// reason:` comment explaining the synchronization strategy (lock, dispatch queue, atomic).

## Snapshot test review

When a snapshot test fails on CI:

1. Download the diff (`__SnapshotFailures__` dir)
2. Is the change intentional? → accept with `record: true`
3. Unintentional? → fix the code

Never blindly accept. Snapshot diffs catch regressions you didn't notice.

## Concurrency verification

For code using Swift Concurrency:

- Build with `-strict-concurrency=complete` (SPM) or `SWIFT_STRICT_CONCURRENCY=complete` (Xcode)
- Run with `Thread Sanitizer` (`xcodebuild test -enableThreadSanitizer YES`)
- Test cancellation: `Task { try await longOp() }.cancel()` and verify `CancellationError`

## SPM and `Package.swift`

```swift
// swift-tools-version: 5.10
import PackageDescription

let package = Package(
    name: "MyApp",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [.library(name: "MyApp", targets: ["MyApp"])],
    dependencies: [
        .package(url: "https://github.com/pointfreeco/swift-snapshot-testing.git", from: "1.17.0"),
    ],
    targets: [
        .target(name: "MyApp", dependencies: []),
        .testTarget(name: "MyAppTests", dependencies: ["MyApp", .product(name: "SnapshotTesting", package: "swift-snapshot-testing")]),
    ],
    swiftLanguageModes: [.v6]
)
```

`swiftLanguageModes: [.v6]` enables Swift 6 strict concurrency — `Sendable` checks at compile time.

## The exit checklist

Before writing the final report:

- [ ] `swift build` compiles
- [ ] `swift build -warnings-as-errors` clean
- [ ] `swift test` green
- [ ] No new `@unchecked Sendable` without reason
- [ ] No `// swiftlint:disable` without reason
- [ ] Snapshot tests reviewed (if any changed)
- [ ] Report lists VERIFIED (with command + summary) and ASSUMED (with reason)

If you cannot check a box, that fact goes in ASSUMED with the reason.
