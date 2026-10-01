# Templates & Metaprogramming

## Function templates

```cpp
template<typename T>
T max_of(T a, T b) { return a > b ? a : b; }
```

## Class templates

```cpp
template<typename T, std::size_t N>
class Array {
    T data_[N];
public:
    constexpr T& operator[](std::size_t i) { return data_[i]; }
    constexpr const T& operator[](std::size_t i) const { return data_[i]; }
    constexpr std::size_t size() const { return N; }
};
```

## Concepts — constraining templates

```cpp
template<typename T>
concept Addable = requires(T a, T b) {
    { a + b } -> std::same_as<T>;
};

template<Addable T>
T sum(std::span<const T> xs) {
    T s{};
    for (auto x : xs) s = s + x;
    return s;
}

// requires-expression with multiple constraints
template<typename T>
concept Hashable = requires(T v) {
    { std::hash<T>{}(v) } -> std::convertible_to<std::size_t>;
};
```

## Variadic templates — fold expressions

```cpp
template<typename... Ts>
auto sum_all(Ts... xs) {
    return (xs + ...);  // right fold: x1 + (x2 + (x3 + x4))
}

template<typename... Ts>
auto all_true(Ts... xs) {
    return (xs && ...);  // (x1 && x2) && x3 ...
}

template<typename... Ts>
void print_all(const Ts&... xs) {
    (std::println("{}", xs), ...);  // comma fold
}
```

## `if constexpr` — compile-time branching

```cpp
template<typename T>
auto convert(const T& v) {
    if constexpr (std::is_same_v<T, std::string>) {
        return v;
    } else if constexpr (std::is_arithmetic_v<T>) {
        return std::to_string(v);
    } else {
        static_assert(sizeof(T) == 0, "unsupported");
    }
}
```

## CRTP — static polymorphism

```cpp
template<typename Derived>
struct Addable {
    Derived& self() { return static_cast<Derived&>(*this); }
    const Derived& self() const { return static_cast<const Derived&>(*this); }

    Derived operator+(const Derived& o) const {
        Derived result = self();
        result += o;
        return result;
    }
};

struct Vec3 : Addable<Vec3> {
    float x, y, z;
    Vec3& operator+=(const Vec3& o) { x+=o.x; y+=o.y; z+=o.z; return *this; }
};
```

CRTP gives static dispatch (no vtable) when you don't need runtime polymorphism.

## Type traits

```cpp
#include <type_traits>

static_assert(std::is_integral_v<int>);
static_assert(std::is_class_v<std::string>);
static_assert(std::is_same_v<std::remove_const_t<const int>, int>);

// Conditional type alias
template<bool B, typename T, typename F>
using conditional_t = std::conditional<B, T, F>::type;
```

## `consteval` / `constinit` (C++20)

```cpp
consteval int fib(int n) {  // must run at compile time
    return n < 2 ? n : fib(n-1) + fib(n-2);
}
constexpr int x = fib(10);  // 55

constinit int counter = 0;  // compile-time init, runtime mutable
```

## Template argument deduction (CTAD)

```cpp
std::pair p{1, "hello"s};        // std::pair<int, std::string>
std::vector v{1, 2, 3};          // std::vector<int>
std::array a{1, 2, 3, 4};        // std::array<int, 4>

// Custom CTAD guide
template<typename T> struct Wrapper { T v; };
template<typename T> Wrapper(T) -> Wrapper<T>;
Wrapper w{42};                   // Wrapper<int>
```

## SFINAE — when concepts can't express it

```cpp
// Old SFINAE (avoid; use concepts)
template<typename T,
         typename = std::enable_if_t<std::is_integral_v<T>>>
T identity(T v) { return v; }

// Modern: concepts
template<std::integral T>
T identity(T v) { return v; }
```

## Variadic templates — recursive unpacking (legacy)

```cpp
// Pre-fold-expression style (avoid in new code)
template<typename T>
void print(const T& v) { std::println("{}", v); }

template<typename T, typename... Rest>
void print(const T& first, const Rest&... rest) {
    print(first);
    print(rest...);
}
```

Use fold expressions instead — clearer, faster to compile.

## Common pitfalls

- Templates defined in `.cpp` files — definitions must be in headers (or use explicit instantiation)
- Two-phase name lookup — dependent names need `typename`/`template`: `typename T::iterator it;`
- Excessive template recursion — slow compile, blows up binary; use variadic folds or `if constexpr`
- Forgetting `constexpr` on a function that could be compile-time — limits metaprogramming
- CTAD ambiguity — write explicit deduction guides when the compiler guesses wrong
- Concepts with `-> std::same_as<T>` being too strict — consider `std::convertible_to<T>` for flexibility
