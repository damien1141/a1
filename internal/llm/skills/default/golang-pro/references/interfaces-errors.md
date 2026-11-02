# Interfaces & Errors

## Interface design principles

1. **Accept interfaces, return structs** — consumers define the interface they need
2. **Keep interfaces small** — `io.Reader` has one method; that's the gold standard
3. **Single-method interfaces compose** — `io.Reader` + `io.Writer` = `io.ReadWriter`
4. **Don't export interfaces prematurely** — start with a concrete type, extract interface when a second consumer needs it

```go
// Define at the consumer, not the producer
type Store interface {
    Get(ctx context.Context, key string) ([]byte, error)
}

type Service struct {
    store Store  // any type implementing Get works
}
```

## Implicit implementation

```go
type Closer interface { Close() error }

type File struct{ /* ... */ }
func (f *File) Close() error { /* ... */ return nil }

// File implicitly satisfies Closer — no `implements` keyword
var _ Closer = (*File)(nil)  // compile-time check
```

Use the `var _ Interface = (*Type)(nil)` pattern to assert conformance at compile time.

## `any` and type parameters

Prefer type parameters over `any` + type assertions:

```go
// GOOD — generic, type-safe
func Map[T, U any](xs []T, fn func(T) U) []U {
    out := make([]U, len(xs))
    for i, x := range xs {
        out[i] = fn(x)
    }
    return out
}

// BAD — loses type info
func MapAny(xs []any, fn func(any) any) []any { /* ... */ }
```

## `io.Reader` / `io.Writer` — the universal contract

```go
func Copy(dst io.Writer, src io.Reader) (int64, error) {
    return io.Copy(dst, src)
}

// Works with files, network, buffers, gzip, etc.
```

Design APIs around `io.Reader`/`io.Writer` for composability.

## Errors — wrap, don't replace

```go
if err != nil {
    return fmt.Errorf("doing X: %w", err)
}
```

- `%w` wraps (preserves the error for `errors.Is`/`As`)
- `%v` formats (loses the chain — use only when wrapping intentionally breaks `errors.Is`)
- `%w` can wrap multiple errors (1.20+): `fmt.Errorf("multi: %w; %w", e1, e2)`

## Sentinel errors

```go
var ErrNotFound = errors.New("not found")

func Find(id string) (*Item, error) {
    if /* not in store */ {
        return nil, fmt.Errorf("find %s: %w", id, ErrNotFound)
    }
    /* ... */
}

// Consumer
if errors.Is(err, ErrNotFound) { /* handle */ }
```

Sentinels are values, not types. Use them for cross-package error matching.

## Custom error types

```go
type ValidationError struct {
    Field   string
    Message string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("validation %s: %s", e.Field, e.Message)
}

// Consumer
var ve *ValidationError
if errors.As(err, &ve) {
    log.Printf("field %s: %s", ve.Field, ve.Message)
}
```

`errors.As` walks the chain and finds the first match of the target type.

## `errors.Join` (1.20+)

```go
err1 := validateA(x)
err2 := validateB(x)
if err1 != nil || err2 != nil {
    return errors.Join(err1, err2)
}
```

`errors.Is` and `errors.As` traverse joined errors.

## Don't expose internal errors

```go
// Internal
type repoError struct{ /* ... */ }

// Public sentinel
var ErrUserNotFound = errors.New("user not found")

func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
    u, err := s.repo.Find(ctx, id)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, fmt.Errorf("get user %s: %w", id, ErrUserNotFound)
        }
        // Wrap but don't leak SQL details to API callers
        return nil, fmt.Errorf("get user %s: %w", id, err)
    }
    return u, nil
}
```

## `panic`/`recover` — almost never

Use `panic` for:
- Truly unrecoverable invariants (e.g. `impossible` switch default)
- `init()` failures that should crash the program

Never for normal control flow. `recover` only at program boundaries (HTTP middleware, goroutine supervisors):

```go
func recoverer(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        defer func() {
            if rec := recover(); rec != nil {
                log.Printf("panic: %v", rec)
                http.Error(w, "internal error", http.StatusInternalServerError)
            }
        }()
        next.ServeHTTP(w, r)
    })
}
```

## Common pitfalls

- Returning `error` as `nil` inside a typed `*MyError` variable — returns a non-nil interface wrapping nil. Always return `nil` directly.
- Comparing errors with `==` instead of `errors.Is` — breaks with wrapping
- Wrapping without context — `return err` is fine; `return fmt.Errorf("%w", err)` adds nothing
- Defining large interfaces upfront — they ossify; extract when needed
