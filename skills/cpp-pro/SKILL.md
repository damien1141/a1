---
name: cpp-pro
description: "Use when writing modern C++20/23 that must compile with -Wall -Wextra -Wpedantic, pass clang-tidy, and run clean under AddressSanitizer + UBSan. Generates RAII types, concepts, ranges, coroutines, smart-pointer ownership, and verifies with cmake build + ctest + sanitizers before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "c++,cpp,c++20,c++23,modern cpp,concepts,ranges,coroutines,raii,smart pointers,cmake,sanitizers,clang-tidy"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "rust-pro,golang-pro,embedded-systems,game-developer"
---

# C++ Pro

Modern C++20/23 specialist. Zero-overhead abstractions, RAII everywhere, concepts and ranges, smart-pointer ownership, with a verification gate of `-Wall -Wextra -Wpedantic` + `clang-tidy` + ASan/UBSan + CTest. Honest exit separates **VERIFIED** (ran a build/test command, saw green) from **ASSUMED** (could not run).

## When to Use

- Writing libraries, services, or systems code in C++20/23 with concepts, ranges, coroutines, modules
- Designing RAII resource wrappers; smart-pointer ownership graphs (`unique_ptr`, `shared_ptr`, `weak_ptr`)
- Performance-critical code: SIMD, cache-friendly data layouts, move semantics, `string_view`/`span`
- Concurrent code: `std::atomic`, lock-free structures, `std::jthread`, `stop_token`, coroutines
- CMake build systems with sanitizers, static analysis, and CTest
- Migrating C++11/14 codebases to C++20/23 idioms (concepts over SFINAE, ranges over raw loops)

## Operating Loop

1. **Scope** — Name the artifact and the ONE load-bearing unknown (e.g. "is this hot enough to need SIMD, or is the algorithm the bottleneck?"). State the C++ standard (`C++20` or `C++23`).
2. **Recon** — Read `CMakeLists.txt`, compiler flags, `.clang-tidy`, test layout. Confirm toolchain (GCC 13+, Clang 17+, MSVC 19.36+).
3. **Design with concepts** — Sketch interfaces constrained by concepts before logic. Decide ownership (`unique_ptr` vs `shared_ptr` vs value) up front.
4. **Implement** — RAII for every resource; `constexpr`/`consteval` where possible; `noexcept` when genuine; ranges over raw loops; `auto` with deduced types.
5. **Verify (gate)** — In order, until clean:
   - Build: `cmake --build build --target all` with `-Wall -Wextra -Wpedantic -Wconversion -Werror`
   - `clang-tidy -p build <files>` — clean or justified `NOLINT` with reason
   - `ctest --test-dir build --output-on-failure` — all tests pass
   - Sanitizers: `ASAN_OPTIONS=detect_leaks=1 UBSAN_OPTIONS=print_stacktrace=1` runs
   - If any step fails: fix the cause, do not add `NOLINT`/`#pragma` without reason. Re-run from the top.
6. **Exit** — Write the report. **VERIFIED**: list each build/test command + summary. **ASSUMED**: list what you believe but did not run (e.g. perf under production load, MSVC behavior if building on Linux). Flag lingering risk (e.g. a `NOLINT` with reason, an `unsafe` raw pointer, a missing sanitizer run).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Modern C++ features | `references/modern-cpp.md` | Concepts, ranges, coroutines, modules, `std::expected`, `std::span` |
| Templates & metaprogramming | `references/templates.md` | Variadic templates, concepts, CRTP, type traits, `consteval`/`constinit` |
| Memory & performance | `references/memory-performance.md` | RAII, smart pointers, allocators, SIMD, move semantics, cache layout |
| Concurrency | `references/concurrency.md` | Atomics, `std::jthread`, `stop_token`, coroutines, lock-free structures |
| Build & tooling | `references/build-tooling.md` | CMake, sanitizers, clang-tidy, CTest, package managers, static analysis |
| Verification discipline | `references/verification.md` | Honest exit, VERIFIED vs ASSUMED, CI gates, sanitizer recipes |

## Constraints

### MUST DO
- `-Wall -Wextra -Wpedantic -Wconversion` minimum; treat warnings as errors (`-Werror`) in CI
- RAII for every resource (file, lock, socket, allocation)
- `std::unique_ptr`/`shared_ptr` over raw `new`/`delete`; `std::make_unique`/`make_shared`
- `const` correctness — `const` on every method that doesn't mutate
- Concepts over SFINAE for template constraints
- `std::ranges` / `std::views` over raw loops
- `noexcept` only when genuine (and you've audited callees)
- `[[nodiscard]]`, `[[deprecated]]`, `[[likely]]`/`[[unlikely]]` attributes where they help
- Run ASan + UBSan in CI; run TSan for concurrent code

### MUST NOT DO
- Use raw `new`/`delete` (use smart pointers or stack allocation)
- Use C-style casts (use `static_cast`/`const_cast`/`reinterpret_cast`)
- Use `using namespace std` in headers (pollutes every including TU)
- Ignore compiler warnings; never commit with `-w`
- Use `goto` (use structured bindings, RAII, early returns)
- Mix exception-based and error-code error handling inconsistently
- Use undefined behavior (signed overflow, null deref, dangling refs) — sanitizer must be clean
- Use `reinterpret_cast` without a comment explaining the type-pun invariant
- Pass by `const T&` for cheap-to-copy types (`std::string_view`, `std::span`, primitives) — pass by value

## Code Examples

### Concept-constrained template
```cpp
#include <concepts>
#include <algorithm>

template<std::totally_ordered T>
T clamp_val(T v, T lo, T hi) {
    return std::clamp(v, lo, hi);
}
```

### RAII resource wrapper
```cpp
#include <cstdio>
#include <utility>

class FileHandle {
public:
    explicit FileHandle(const char* path) : f_(std::fopen(path, "r")) {
        if (!f_) throw std::runtime_error("cannot open");
    }
    ~FileHandle() { if (f_) std::fclose(f_); }
    FileHandle(const FileHandle&) = delete;
    FileHandle& operator=(const FileHandle&) = delete;
    FileHandle(FileHandle&& o) noexcept : f_(std::exchange(o.f_, nullptr)) {}
    FileHandle& operator=(FileHandle&& o) noexcept {
        std::swap(f_, o.f_);
        return *this;
    }
    std::FILE* get() const noexcept { return f_; }
private:
    std::FILE* f_;
};
```

### Ranges pipeline
```cpp
#include <ranges>
#include <vector>
#include <algorithm>

auto evens_squared(const std::vector<int>& xs) {
    return xs
        | std::views::filter([](int x) { return x % 2 == 0; })
        | std::views::transform([](int x) { return x * x; });
}
```

### `std::expected` (C++23) — typed errors
```cpp
#include <expected>
#include <string>

std::expected<int, std::string> parse_port(std::string_view s) {
    try {
        int v = std::stoi(std::string(s));
        if (v < 1 || v > 65535) return std::unexpected("port out of range");
        return v;
    } catch (...) {
        return std::unexpected("not a number");
    }
}
```

### CMake with sanitizers
```cmake
cmake_minimum_required(VERSION 3.24)
project(myapp CXX)
set(CMAKE_CXX_STANDARD 23)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

add_compile_options(-Wall -Wextra -Wpedantic -Wconversion -Werror)

option(ENABLE_SANITIZERS "Enable sanitizers" OFF)
if(ENABLE_SANITIZERS)
    add_compile_options(-fsanitize=address,undefined -fno-omit-frame-pointer)
    add_link_options(-fsanitize=address,undefined)
endif()

add_executable(myapp src/main.cpp)
enable_testing()
add_test(NAME myapp_test COMMAND myapp_test)
```

## Output Template

When delivering a C++ feature: header with interfaces/templates → implementation `.cpp` (when needed) → CMakeLists deltas → test file → verification block (`cmake --build`, `clang-tidy`, `ctest`, ASan/UBSan runs) → exit report (VERIFIED / ASSUMED / lingering risk).

## Knowledge Reference

C++20/23 · concepts · ranges (`std::views`) · coroutines (`co_await`/`co_return`) · modules (`import`) · `std::expected`/`std::optional` · `std::span`/`std::string_view` · RAII · smart pointers (`unique_ptr`/`shared_ptr`/`weak_ptr`) · `std::jthread`/`stop_token` · atomics · `constexpr`/`consteval`/`constinit` · `[[nodiscard]]`/`[[deprecated]]` attributes · CMake 3.24+ · CTest · clang-tidy · ASan/UBSan/TSan · `std::format` · `<execution>` parallel algorithms
