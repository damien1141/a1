# Concurrency in Go

## The three primitives

| Primitive | When |
|---|---|
| Goroutine + channel | Producer/consumer, pipelines, fan-out/fan-in |
| `sync.WaitGroup` | Wait for N goroutines to finish (no result needed) |
| `errgroup.Group` | N goroutines, each returns an error; fail-fast on first |
| `sync.Mutex` / `RWMutex` | Protect shared mutable state |
| `sync.Once` | One-time initialization |
| `context.Context` | Cancellation, deadlines, request-scoped values |

## `errgroup` — the default for fan-out

```go
import "golang.org/x/sync/errgroup"

func fetchAll(ctx context.Context, urls []string) ([][]byte, error) {
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(8)  // bounded concurrency (1.21+)
    results := make([][]byte, len(urls))
    for i, u := range urls {
        i, u := i, u
        g.Go(func() error {
            data, err := fetch(ctx, u)
            if err != nil {
                return fmt.Errorf("fetch %s: %w", u, err)
            }
            results[i] = data
            return nil
        })
    }
    return results, g.Wait()
}
```

`errgroup.WithContext` cancels the group on first error. `SetLimit` bounds concurrency.

## Channels — pipelines

```go
func gen(ctx context.Context, n int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for i := range n {
            select {
            case out <- i:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}

func square(ctx context.Context, in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for v := range in {
            select {
            case out <- v * v:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}

// Compose
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
for v := range square(ctx, gen(ctx, 10)) {
    fmt.Println(v)
}
```

## Fan-out / fan-in

```go
func fanIn[T any](ctx context.Context, channels ...<-chan T) <-chan T {
    out := make(chan T)
    var wg sync.WaitGroup
    for _, ch := range channels {
        wg.Add(1)
        go func(c <-chan T) {
            defer wg.Done()
            for v := range c {
                select {
                case out <- v:
                case <-ctx.Done():
                    return
                }
            }
        }(ch)
    }
    go func() { wg.Wait(); close(out) }()
    return out
}
```

## Select — timeouts and cancellation

```go
select {
case res := <-result:
    return res, nil
case <-time.After(2 * time.Second):
    return nil, ErrTimeout
case <-ctx.Done():
    return nil, ctx.Err()
}
```

Always include `ctx.Done()` in selects that span a request.

## Mutexes — when channels don't fit

```go
type Cache struct {
    mu    sync.RWMutex
    items map[string]string
}

func (c *Cache) Get(key string) (string, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    v, ok := c.items[key]
    return v, ok
}

func (c *Cache) Set(key, value string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items[key] = value
}
```

Use `RWMutex` only when reads dominate writes by 10x+; otherwise plain `Mutex` is faster.

## `sync.Once` for initialization

```go
var (
    initOnce sync.Once
    config   *Config
    initErr  error
)

func Config() (*Config, error) {
    initOnce.Do(func() {
        config, initErr = loadConfig()
    })
    return config, initErr
}
```

## Graceful shutdown

```go
func run(ctx context.Context) error {
    ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
    defer cancel()

    server := &http.Server{Addr: ":8080", Handler: handler}
    errCh := make(chan error, 1)
    go func() { errCh <- server.ListenAndServe() }()

    select {
    case err := <-errCh:
        return err
    case <-ctx.Done():
        shutdownCtx, sCancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer sCancel()
        return server.Shutdown(shutdownCtx)
    }
}
```

## Anti-patterns

```go
// WRONG — goroutine leak: no cancellation, runs forever
go func() {
    for { poll() }
}()

// WRONG — send on closed channel
ch := make(chan int)
close(ch)
ch <- 1  // panic

// WRONG — copy sync.Mutex by value
type S struct{ mu sync.Mutex }
s := S{}
s2 := s  // Mutex copied — use *sync.Mutex or pointer

// WRONG — WaitGroup.Add inside the goroutine
wg.Add(1)
go func() {
    wg.Add(1)  // race: Add must be called before Wait returns
    defer wg.Done()
    ...
}()
```

## Race detector

Always run tests with `-race`. It catches data races at runtime (not at compile time):

```bash
go test -race -count=1 ./...
```

In CI, run with `-race` always. The overhead is 5-10x; worth it.

## Verification

- `go vet ./...` — catches `printf` misuse, lock copies, unreachable code
- `golangci-lint run` — includes `govet`, `errcheck`, `staticcheck`, `gosec`, `gocritic`
- `go test -race ./...` — runtime race detection
- For deep concurrency audits, use `go test -race -count=10` (runs tests multiple times to surface races)
