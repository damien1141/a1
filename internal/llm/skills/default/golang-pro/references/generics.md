# Generics in Go (1.18+)

## Type parameters

```go
func Map[T, U any](xs []T, fn func(T) U) []U {
    out := make([]U, len(xs))
    for i, x := range xs {
        out[i] = fn(x)
    }
    return out
}

doubled := Map([]int{1, 2, 3}, func(x int) int { return x * 2 })
upper := Map([]string{"a", "b"}, strings.ToUpper)
```

## Constraints

```go
type Number interface {
    ~int | ~int32 | ~int64 | ~float32 | ~float64
}

func Sum[T Number](xs []T) T {
    var sum T
    for _, x := range xs {
        sum += x
    }
    return sum
}

type MyInt int  // ~int allows this
Sum([]MyInt{1, 2, 3})  // OK
```

The `~` token means "and types whose underlying type is X" — essential for type aliases.

## The `cmp` package (1.21+)

```go
import "cmp"

func Max[T cmp.Ordered](a, b T) T {
    if a > b { return a }
    return b
}
```

`cmp.Ordered` is the standard constraint for `>`/`<`-comparable types. Use it instead of rolling your own.

## The `slices` and `maps` packages (1.21+)

```go
import "slices"
import "maps"

xs := []int{3, 1, 2}
slices.Sort(xs)              // in-place
sorted := slices.Clone(xs)
slices.Reverse(sorted)

m := map[string]int{"a": 1, "b": 2}
for k := range maps.Keys(m) { /* ... */ }  // iter.Seq[K] (1.23+)
```

Prefer these over hand-rolled helpers. They cover Sort/Stable/Reverse/Contains/Index/BinarySearch/Compact/Group/etc.

## Generic types

```go
type Stack[T any] struct {
    items []T
}

func (s *Stack[T]) Push(x T) { s.items = append(s.items, x) }
func (s *Stack[T]) Pop() (T, bool) {
    var zero T
    if len(s.items) == 0 {
        return zero, false
    }
    x := s.items[len(s.items)-1]
    s.items = s.items[:len(s.items)-1]
    return x, true
}

s := &Stack[int]{}
s.Push(1)
```

## Constraints with methods

```go
type Stringer interface {
    String() string
}

func Print[T Stringer](xs []T) {
    for _, x := range xs {
        fmt.Println(x.String())
    }
}
```

Constraints can be type sets (unions of `~T`) or method sets, or both:

```go
type Constrained interface {
    ~int | ~string
    String() string
}
```

## Type inference

Go infers type arguments in most cases:

```go
slices.Sort(xs)            // infers T from xs
slices.Contains(xs, 42)    // infers T from xs and 42
```

Specify explicitly only when inference fails.

## Generic constraints in the standard library

| Package | Constraint | Use |
|---|---|---|
| `cmp` | `Ordered` | `<`, `>`, `<=`, `>=` |
| `constraints` (x/exp) | `Signed`, `Unsigned`, `Integer`, `Float`, `Complex` | Numeric category |
| `slices` | `any` mostly | Slice operations |
| `maps` | `any` mostly | Map operations |

## When NOT to use generics

- The function operates on `string` only — just write `func(s string)`
- The generic adds no real abstraction — a `Stack[int]` is fine, but a `Stack[T]` with one use site is over-engineering
- Reflection would be simpler for fully dynamic cases (rare)
- The stdlib already provides it — use `slices`/`maps`/`cmp` instead

## Pitfalls

- `~T` constraints include named types whose underlying type is T; useful but can surprise
- Methods on generic types cannot add new type parameters: `func (s Stack[T]) Push(x T)` is fine; `func (s Stack[T]) Push[U any](x U)` is a compile error
- Interface methods with type parameters in signatures are not allowed (no `interface { Foo[T any](T) }`)
- `any` is an alias for `interface{}` — use `any` in new code

## Iterators (1.23+)

```go
import "iter"

// iter.Seq[V]  = func(yield func(V) bool)
// iter.Seq2[K,V] = func(yield func(K, V) bool)

func Lines(r io.Reader) iter.Seq[string] {
    return func(yield func(string) bool) {
        sc := bufio.NewScanner(r)
        for sc.Scan() {
            if !yield(sc.Text()) {
                return
            }
        }
    }
}

for line := range Lines(r) {
    fmt.Println(line)
}
```

`slices.Values`, `slices.All`, `maps.Keys`, `maps.Values` return these iterators.
