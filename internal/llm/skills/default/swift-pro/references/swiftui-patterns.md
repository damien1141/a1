# SwiftUI Patterns

## `@Observable` (Swift 5.9+) — the modern view model

```swift
import Observation

@Observable
final class CounterModel {
    var count = 0
    var name: String = ""
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
            Text("\(model.count)")
            Button("Inc", action: model.increment)
        }
    }
}
```

`@Observable` (macro) replaces `ObservableObject` + `@Published`. Views track only the properties they read — finer-grained updates than `objectWillChange`.

## Property wrappers — when to use each

| Wrapper | Purpose |
|---|---|
| `@State` | View-local, value-typed, owned by this view |
| `@Binding` | Reference to a parent's `@State` (two-way) |
| `@State private var model = VM()` | Owns a reference type (e.g. `@Observable` view model) |
| `@Environment` | Read values from the environment (system or injected) |
| `@EnvironmentObject` | (Legacy) shared `ObservableObject` from environment |
| `@Bindable` | Get `@Binding` accessors to an `@Observable`'s properties |
| `@ObservedObject` | (Legacy) reference to external `ObservableObject` |

### `@State` for value types vs `@Observable` for view models

```swift
// Value type — owned, mutated locally
struct ToggleView: View {
    @State private var isOn = false
    var body: some View { Toggle("On", isOn: $isOn) }
}

// @Observable view model — owned by this view, passed down as needed
struct SettingsView: View {
    @State private var vm = SettingsModel()
    var body: some View { Form { TextField("Name", text: $vm.name) } }
}

// External @Observable — use @Bindable to get $ bindings
struct EditorView: View {
    @Bindable var doc: Document  // passed in
    var body: some View { TextField("Title", text: $doc.title) }
}
```

## Navigation — `NavigationStack` (iOS 16+)

```swift
NavigationStack {
    List(items) { item in
        NavigationLink(item.title, value: item.id)
    }
    .navigationDestination(for: Item.ID.self) { id in
        DetailView(itemId: id)
    }
}
```

Prefer `NavigationStack` + `.navigationDestination(for:)` over the legacy `NavigationLink(destination:)` — the new API is more flexible and supports programmatic navigation.

## Sheets and alerts

```swift
struct ParentView: View {
    @State private var showingSheet = false
    @State private var itemToDelete: Item?

    var body: some View {
        List {
            ForEach(items) { item in
                Text(item.title).contextMenu { Button("Delete") { itemToDelete = item } }
            }
        }
        .sheet(isPresented: $showingSheet) { EditorView() }
        .alert("Delete?", isPresented: Binding(
            get: { itemToDelete != nil },
            set: { if !$0 { itemToDelete = nil } }
        )) {
            Button("Delete", role: .destructive) { /* delete */ }
            Button("Cancel", role: .cancel) {}
        }
    }
}
```

## Lists and `ForEach`

```swift
List {
    ForEach(items) { item in
        Text(item.title)
    }
    .onDelete { indexSet in items.remove(atOffsets: indexSet) }
    .onMove { from, to in items.move(fromOffsets: from, toOffset: to) }
}
.listStyle(.insetGrouped)
```

Use `id: \.self` only for value types with stable identity. Prefer `Identifiable` conformance.

## Async image loading

```swift
struct AsyncImageView: View {
    let url: URL
    @State private var data: Data?

    var body: some View {
        Group {
            if let data, let img = UIImage(data: data) {
                Image(uiImage: img).resizable().aspectRatio(contentMode: .fit)
            } else {
                ProgressView()
            }
        }
        .task(id: url) {
            data = try? await URLSession.shared.data(from: url).0
        }
    }
}
```

`.task(id:)` cancels and restarts when `id` changes. For production use `AsyncImage` (built-in) or a caching library like `Nuke`.

## Custom modifiers

```swift
extension View {
    func cardStyle() -> some View {
        self
            .padding()
            .background(Color(.secondarySystemBackground))
            .clipShape(RoundedRectangle(cornerRadius: 12))
    }
}

// Usage
Text("Hello").cardStyle()
```

## Preferences for child → parent communication

```swift
extension View {
    var sizeKey: some View { self.background(SizeReadingView()) }
}

struct SizePreferenceKey: PreferenceKey {
    static var defaultValue: CGSize = .zero
    static func reduce(value: inout CGSize, nextValue: () -> CGSize) {
        value = nextValue()
    }
}

// In child: .background(GeometryReader { proxy in Color.clear.preference(key: SizePreferenceKey.self, value: proxy.size) })
// In parent: .onPreferenceChange(SizePreferenceKey.self) { size in /* ... */ }
```

Use sparingly — most layout needs are met by `.frame`, `.background(GeometryReader)`, or `onGeometryChange` (iOS 18+).

## Common pitfalls

- `ObservableObject` + `@Published` in new code — use `@Observable`
- `@ObservedObject var vm` in a parent that should own the VM — use `@State`
- Massive `body` — extract subviews; SwiftUI diffs struct identity, not size
- `Task { }` fire-and-forget in views — use `.task { }` instead (auto-cancels on disappear)
- Force-unwrapping `UIImage(named:)` — returns nil if asset missing; handle gracefully
- `@Environment(\.colorScheme)` checked every render — fine, but cache if expensive
- Forgetting `.id()` on `ForEach` items that can be reordered — list glitches
