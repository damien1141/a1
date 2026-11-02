# Concurrency in Modern C++

## `std::jthread` — auto-joining thread with cancellation

```cpp
#include <thread>
#include <stop_token>
#include <iostream>

void worker(std::stop_token st, int id) {
    while (!st.stop_requested()) {
        std::println("worker {} ticking", id);
        std::this_thread::sleep_for(std::chrono::milliseconds(100));
    }
    std::println("worker {} exiting", id);
}

int main() {
    std::jthread t(worker, 1);
    std::this_thread::sleep_for(std::chrono::seconds(1));
    // t.request_stop() + auto join on destruction
}
```

`jthread` joins on destruction (no leak). `stop_token` enables cooperative cancellation.

## Atomics

```cpp
#include <atomic>

std::atomic<int> counter{0};

void increment() {
    counter.fetch_add(1, std::memory_order_relaxed);
}

// Memory orderings (use relaxed unless you need stronger)
// relaxed — no ordering, only atomic
// acquire — reads after this can't be reordered before
// release — writes before this can't be reordered after
// acq_rel — both
// seq_cst — total sequential consistency (default, slowest)
```

Use `relaxed` for counters, `acquire`/`release` for flag-based synchronization, `seq_cst` only when you need a global order.

## Lock-free SPSC queue

```cpp
template<typename T, std::size_t Capacity>
class SpscQueue {
    std::array<T, Capacity> buf_;
    std::atomic<std::size_t> head_{0};  // written by producer
    std::atomic<std::size_t> tail_{0};  // written by consumer

public:
    bool push(const T& v) {
        auto h = head_.load(std::memory_order_relaxed);
        auto next = (h + 1) % Capacity;
        if (next == tail_.load(std::memory_order_acquire)) return false;  // full
        buf_[h] = v;
        head_.store(next, std::memory_order_release);
        return true;
    }

    bool pop(T& out) {
        auto t = tail_.load(std::memory_order_relaxed);
        if (t == head_.load(std::memory_order_acquire)) return false;  // empty
        out = buf_[t];
        tail_.store((t + 1) % Capacity, std::memory_order_release);
        return true;
    }
};
```

Single-producer single-consumer — the simplest correct lock-free structure. Use for hot paths between two threads.

## `std::mutex` family — when locks are fine

```cpp
std::mutex m;
{
    std::lock_guard lock(m);  // C++17 CTAD
    shared_data.push_back(42);
}

std::shared_mutex rw;
{
    std::shared_lock rlock(rw);  // read lock
    auto snapshot = shared_data;
}
{
    std::unique_lock wlock(rw);  // write lock
    shared_data.clear();
}
```

- `std::mutex` — exclusive
- `std::shared_mutex` (C++17) — multiple readers OR one writer
- `std::recursive_mutex` — same thread can lock multiple times (avoid; usually a design smell)
- `std::scoped_lock(m1, m2, ...)` — deadlock-free multi-lock acquisition

## `std::async` and futures

```cpp
#include <future>

auto f1 = std::async(std::launch::async, [] { return compute(1); });
auto f2 = std::async(std::launch::async, [] { return compute(2); });
int sum = f1.get() + f2.get();
```

`std::async` is fine for one-off fan-out. For sustained workloads, use a thread pool (Boost.Asio, Intel TBB, or `std::execution` in C++26).

## `std::promise`/`std::future` for channel-like patterns

```cpp
std::promise<Result> p;
std::future<Result> f = p.get_future();

std::jthread worker([&p] {
    try {
        Result r = compute();
        p.set_value(r);
    } catch (...) {
        p.set_exception(std::current_exception());
    }
});

f.wait();
Result r = f.get();
```

## Coroutines — `co_await`/`co_return`/`co_yield`

```cpp
#include <generator>  // C++23

std::generator<int> naturals() {
    int i = 0;
    while (true) co_yield i++;
}

for (int x : naturals() | std::views::take(10)) {
    std::println("{}", x);
}
```

For async coroutines, you need a return type with `promise_type`. Libraries like CppCoro, Boost.Asio, or Folly Futures provide ready-made ones.

## Parallel algorithms

```cpp
#include <execution>
#include <algorithm>

std::vector<int> xs = /* ... */;

std::sort(std::execution::par, xs.begin(), xs.end());
std::for_each(std::execution::par, xs.begin(), xs.end(), [](int& x) { x *= 2; });

// par_unseq — vectorized + parallel; NO locks, exceptions, or allocators allowed inside
std::transform(std::execution::par_unseq, xs.begin(), xs.end(), xs.begin(),
               [](int x) { return x * x; });
```

## Common pitfalls

- Data races — undefined behavior; ASan + TSan catch them
- `std::async` with default policy (`std::launch::async | std::launch::deferred`) — may run synchronously; pass `std::launch::async` explicitly
- `.get()` on a `std::future` twice — undefined; second call throws
- `std::atomic<bool>` as a flag without proper ordering — use `release`/`acquire`, not `relaxed`
- Deadlock from inconsistent lock ordering — use `std::scoped_lock` for multi-lock
- Holding a `std::lock_guard` across a long operation — narrow the scope
- Forgetting `join()` or `detach()` on `std::thread` — `std::terminate` on destruction; use `jthread` instead
- False sharing — separate hot per-thread counters by 64+ bytes (cache line size)

## Verification

- `g++ -fsanitize=thread` — ThreadSanitizer (data races)
- `g++ -fsanitize=address` — AddressSanitizer (heap/stack overflow, use-after-free)
- `g++ -fsanitize=undefined` — UBSan (signed overflow, null deref, etc.)
- Run TSan for any code touching `std::atomic`/`std::thread`/`std::mutex`
- Run ASan + UBSan always in CI
