# Memory & Performance

## RAII — every resource tied to a lifetime

```cpp
class LockGuard {
    std::mutex& m_;
public:
    explicit LockGuard(std::mutex& m) : m_(m) { m_.lock(); }
    ~LockGuard() { m_.unlock(); }
    LockGuard(const LockGuard&) = delete;
    LockGuard& operator=(const LockGuard&) = delete;
};
// Use std::lock_guard / std::scoped_lock instead of writing your own
```

Resources: memory, files, sockets, locks, GL handles, GPU buffers. Each must have a destructor that releases it.

## Smart pointers — pick the right ownership

| Pointer | Ownership | When |
|---|---|---|
| `std::unique_ptr<T>` | Single owner, movable | Default for heap objects |
| `std::shared_ptr<T>` | Shared, ref-counted | When ownership is genuinely shared |
| `std::weak_ptr<T>` | Non-owning observer of `shared_ptr` | Break cycles, cache |
| `std::raw T*` | Non-owning view | Borrowing within a known lifetime |
| `T&` | Non-null reference | Function parameters that can't be null |

```cpp
auto p = std::make_unique<Widget>(args);  // single owner
auto shared = std::make_shared<Widget>(args);  // ref-counted

std::shared_ptr<Widget> sp = /* ... */;
std::weak_ptr<Widget> wp = sp;  // doesn't keep Widget alive
if (auto locked = wp.lock()) {  // returns shared_ptr or empty
    use(*locked);
}
```

## `make_unique` / `make_shared` over `new`

```cpp
// GOOD — single allocation, exception-safe
auto p = std::make_shared<Widget>(args);

// BAD — two allocations, leak risk if an exception fires between
std::shared_ptr<Widget> p(new Widget(args));
```

## Move semantics

```cpp
class Buffer {
    std::byte* data_;
    std::size_t size_;
public:
    Buffer(Buffer&& o) noexcept
        : data_(std::exchange(o.data_, nullptr)), size_(std::exchange(o.size_, 0)) {}

    Buffer& operator=(Buffer&& o) noexcept {
        if (this != &o) {
            delete[] data_;
            data_ = std::exchange(o.data_, nullptr);
            size_ = std::exchange(o.size_, 0);
        }
        return *this;
    }
};
```

`noexcept` move is critical — `std::vector` falls back to copy if move can throw.

## `std::string_view` / `std::span` — pass non-owning views

```cpp
void process(std::span<const int> xs);   // accepts vector, array, C array
void log(std::string_view msg);          // accepts std::string, const char*, view
```

- Avoids copies on function boundaries
- Does NOT extend the owner's lifetime — don't store past the owner

## Cache-friendly data layout

```cpp
// AOS (array of structs) — cache-unfriendly for streaming
struct Particle { float x, y, z; float vx, vy, vz; };
std::vector<Particle> particles;

// SOA (struct of arrays) — cache-friendly for SIMD streaming
struct Particles {
    std::vector<float> x, y, z;
    std::vector<float> vx, vy, vz;
};
```

For high-throughput loops over millions of elements, SOA can be 3-10x faster.

## SIMD — `<experimental/simd>` or intrinsics

```cpp
#include <xsimd/xsimd.hpp>  // or use std::experimental::simd

void square(float* out, const float* in, std::size_t n) {
    std::size_t i = 0;
    using batch = xsimd::batch<float>;
    std::size_t vec_size = batch::size;
    for (; i + vec_size <= n; i += vec_size) {
        batch b = xsimd::load_unaligned(in + i);
        b = b * b;
        xsimd::store_unaligned(out + i, b);
    }
    for (; i < n; ++i) out[i] = in[i] * in[i];
}
```

Profile before adding SIMD — the autovectorizer often does the right thing for simple loops.

## Allocators

```cpp
// PMR (polymorphic memory resource) — C++17
#include <memory_resource>

std::pmr::monotonic_buffer_resource mbr{buffer, sizeof(buffer)};
std::pmr::vector<int> v{&mbr};
v.push_back(42);  // allocates from buffer, never frees until mbr is destroyed
```

`monotonic_buffer_resource` is great for short-lived allocations (per-request, per-frame) — no free, just bump-allocate and reset.

## `constexpr` evaluation

```cpp
constexpr int fib(int n) { return n < 2 ? n : fib(n-1) + fib(n-2); }
constexpr int x = fib(20);  // computed at compile time, zero runtime cost
```

Push work to compile time when possible — template metaprogramming with `consteval`/`constinit`.

## Avoiding allocations in hot paths

- Pre-allocate buffers; reuse across calls
- `std::string_view` instead of `std::string` returns
- `reserve()` before bulk `push_back`
- `std::pmr` with a monotonic buffer for per-request scratch
- `small_vector` (Folly) or `boost::container::small_vector` for stacks with inline storage

## Profiling

```bash
# perf (Linux)
perf record -- ./myapp
perf report

# FlameGraph
perf record -F 99 -g -- ./myapp
perf script | stackcollapse-perf.pl | flamegraph.pl > flame.svg

# Callgrind (Valgrind)
valgrind --tool=callgrind ./myapp
kcachegrind callgrind.out.*

# Benchmark library
# Google Benchmark, or Catch2's benchmark macros
```

## Common pitfalls

- `new`/`delete` in application code — use smart pointers or stack
- Shared `shared_ptr` everywhere — most ownership is single; `unique_ptr` is faster and clearer
- `std::move` on a `const T&` — silently copies (move ctor needs non-const rvalue)
- Returning `const T` by value — blocks move semantics
- `std::vector<bool>` — packed bits; not a real `bool` array; use `std::vector<char>` or `std::bitset`
- Excess `noexcept` — if a callee throws, `std::terminate` fires; only mark `noexcept` after auditing
- `memcpy`-ing non-trivially-copyable types — UB; use copy/move ctors
