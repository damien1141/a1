# Modern C++20/23 Features

## Concepts — replace SFINAE

```cpp
#include <concepts>

// Built-in concepts: std::integral, std::floating_point, std::totally_ordered,
// std::movable, std::copyable, std::default_initializable, std::formattable, ...

template<std::totally_ordered T>
T clamp_val(T v, T lo, T hi) { return std::clamp(v, lo, hi); }

// Custom concept
template<typename T>
concept Numeric = std::integral<T> || std::floating_point<T>;

template<Numeric T>
T sum(std::span<const T> xs) {
    T s = 0;
    for (auto x : xs) s += x;
    return s;
}

// Requires clause for multiple constraints
template<typename T>
requires std::movable<T> && std::copyable<T>
T clone(const T& v) { return T(v); }
```

## Ranges & views — lazy pipelines

```cpp
#include <ranges>
#include <vector>
#include <algorithm>

auto result(std::vector<int>& xs) {
    namespace rv = std::views;
    return xs
        | rv::filter([](int x) { return x > 0; })
        | rv::transform([](int x) { return x * 2; })
        | rv::take(10);
}

// Views are lazy — no allocation until you materialize
std::vector<int> out = result(xs) | std::ranges::to<std::vector>();  // C++23
```

`std::ranges::to<Container>()` (C++23) materializes a view into a container. Pre-C++23, use `std::ranges::copy`.

## `std::span` and `std::string_view` — non-owning views

```cpp
#include <span>
#include <string_view>

void process(std::span<const int> xs);          // accepts vector, array, C array
void log(std::string_view msg);                 // accepts std::string, const char*, string_view

std::vector<int> v = {1, 2, 3};
process(v);                                     // OK
process(std::array<int, 3>{1, 2, 3});           // OK

// Beware: string_view does not own; don't store it past the owner's lifetime
```

## `std::expected<T, E>` (C++23) — typed errors

```cpp
#include <expected>

std::expected<int, std::string> parse_port(std::string_view s) {
    try {
        int v = std::stoi(std::string(s));
        if (v < 1 || v > 65535) return std::unexpected("out of range");
        return v;
    } catch (const std::exception& e) {
        return std::unexpected(e.what());
    }
}

auto r = parse_port("8080");
if (r) use(*r);
else log(r.error());
```

Monadic operations (C++23): `.and_then`, `.or_else`, `.transform`, `.transform_error`.

## Coroutines — `co_await`/`co_return`/`co_yield`

```cpp
#include <coroutine>
#include <generator>  // C++23

std::generator<int> range(int start, int stop) {
    for (int i = start; i < stop; ++i) {
        co_yield i;
    }
}

for (int x : range(0, 10)) {
    std::cout << x << '\n';
}
```

`std::generator<T>` (C++23) is the standard lazy generator. For async coroutines, you need a coroutine return type (e.g. `cppcoro::task` or a custom one).

## Modules — `import`/`export`

```cpp
// math.cppm (module interface unit)
export module math;

export int add(int a, int b) { return a + b; }

// main.cpp
import math;
int main() { return add(2, 3); }
```

Modules replace headers for large projects — faster compilation, no header order issues. Tooling is still maturing; CMake 3.28+ has first-class module support.

## `std::format` (C++20) / `std::print` (C++23)

```cpp
#include <format>
#include <print>

std::string s = std::format("port={} host={}", 8080, "localhost");
std::println("hello {}", "world");  // C++23
```

Type-safe, faster than `printf`, no `iostream` overhead.

## `constexpr` / `consteval` / `constinit`

```cpp
constexpr int factorial(int n) {
    return n <= 1 ? 1 : n * factorial(n - 1);
}
static_assert(factorial(5) == 120);

consteval int square(int x) { return x * x; }  // must run at compile time
int x = square(5);  // 25, computed at compile time

constinit int global = compute_at_init();  // initialized at compile time, mutable at runtime
```

## Structured bindings

```cpp
std::map<std::string, int> m = {{"a", 1}, {"b", 2}};

for (const auto& [key, value] : m) {
    std::println("{}={}", key, value);
}

auto [a, b, c] = std::array{1, 2, 3};
```

## `if constexpr` — compile-time branching

```cpp
template<typename T>
void process(T v) {
    if constexpr (std::integral<T>) {
        std::println("integer: {}", v);
    } else if constexpr (std::floating_point<T>) {
        std::println("float: {:.2}", v);
    } else {
        static_assert(sizeof(T) == 0, "unsupported");
    }
}
```

`if constexpr` discards untaken branches — no instantiation errors.

## `std::jthread` and `stop_token` (C++20)

```cpp
#include <thread>
#include <stop_token>

void worker(std::stop_token st) {
    while (!st.stop_requested()) {
        // do work
    }
}

std::jthread t(worker);  // auto-joins on destruction
t.request_stop();        // cooperative cancellation
```

`jthread` joins on destruction (no more leaked threads). `stop_token` enables cooperative cancellation.

## `<execution>` — parallel algorithms

```cpp
#include <execution>
#include <algorithm>

std::vector<int> xs = /* ... */;
std::sort(std::execution::par, xs.begin(), xs.end());
std::for_each(std::execution::par_unseq, xs.begin(), xs.end(), [](int x) { /* ... */ });
```

`par` = parallel, `par_unseq` = parallel + vectorized. Requires careful reasoning about data races — `unseq` forbids locks.

## Attributes

| Attribute | Purpose |
|---|---|
| `[[nodiscard]]` | Caller must use the return value |
| `[[deprecated("use X")]]` | Compile warning on use |
| `[[likely]]` / `[[unlikely]]` | Branch prediction hint |
| `[[fallthrough]]` | Intentional switch fallthrough |
| `[[maybe_unused]]` | Suppress unused warning |
| `[[noreturn]]` | Function never returns |
