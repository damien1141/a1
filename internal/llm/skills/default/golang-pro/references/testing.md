# Testing, Benchmarking, and Fuzzing

## Table-driven tests

```go
func TestParsePort(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    int
        wantErr bool
    }{
        {"valid", "8080", 8080, false},
        {"zero", "0", 0, true},
        {"negative", "-1", 0, true},
        {"overflow", "70000", 0, true},
        {"empty", "", 0, true},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            got, err := ParsePort(tc.input)
            if (err != nil) != tc.wantErr {
                t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
            }
            if !tc.wantErr && got != tc.want {
                t.Errorf("got %d, want %d", got, tc.want)
            }
        })
    }
}
```

`t.Run` enables `-run TestParsePort/valid` filtering and parallel subtests with `t.Parallel()`.

## Parallel tests

```go
func TestFetchAll(t *testing.T) {
    t.Parallel()
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            t.Parallel()
            // ...
        })
    }
}
```

Capture `tc` carefully — Go 1.22+ has per-iteration loop variables, but pre-1.22 needs `tc := tc`.

## Test helpers — `t.Helper()`

```go
func mustParse(t *testing.T, s string) int {
    t.Helper()
    p, err := ParsePort(s)
    if err != nil {
        t.Fatalf("mustParse %q: %v", s, err)
    }
    return p
}
```

`t.Helper()` makes the helper's line number not show up in failure output — points to the caller instead.

## Fixtures and `t.Cleanup`

```go
func newTestDB(t *testing.T) *sql.DB {
    t.Helper()
    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil { t.Fatal(err) }
    t.Cleanup(func() { db.Close() })
    if _, err := db.Exec(schema); err != nil { t.Fatal(err) }
    return db
}
```

`t.Cleanup` runs in LIFO order, after the test (and its subtests) finish.

## `httptest` for HTTP

```go
func TestClient(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        fmt.Fprintln(w, `{"ok":true}`)
    }))
    t.Cleanup(srv.Close)

    c := NewClient(srv.URL)
    ok, err := c.Ping(context.Background())
    if err != nil { t.Fatal(err) }
    if !ok { t.Fatal("expected ok") }
}
```

## Benchmarks

```go
func BenchmarkSort(b *testing.B) {
    xs := rand.Perm(1000)
    b.ResetTimer()
    for range b.N {
        sorted := slices.Clone(xs)
        slices.Sort(sorted)
    }
}
```

Run: `go test -bench=. -benchmem`. Compare with `benchstat`:

```bash
go test -bench=. -count=10 > old.txt
# ... make change ...
go test -bench=. -count=10 > new.txt
benchstat old.txt new.txt
```

## Fuzzing (1.18+)

```go
func FuzzParsePort(f *testing.F) {
    f.Add("8080")
    f.Add("0")
    f.Add("-1")

    f.Fuzz(func(t *testing.T, input string) {
        p, err := ParsePort(input)
        if err != nil {
            return  // error is fine
        }
        if p < 1 || p > 65535 {
            t.Fatalf("accepted invalid port: %q -> %d", input, p)
        }
    })
}
```

Run: `go test -fuzz=FuzzParsePort -fuzztime=30s`. Corpus lives in `testdata/fuzz/FuzzParsePort/`.

The fuzz target must not panic on valid inputs; it should only fail on contract violations.

## Race detector

```bash
go test -race -count=1 ./...
```

The race detector catches data races at runtime. Always run in CI. For more coverage, run with `-count=20` to surface races that depend on scheduling.

## Coverage

```bash
go test -coverprofile=cover.out ./...
go tool cover -func=cover.out          # summary
go tool cover -html=cover.out -o cov.html
```

Target ≥80% line coverage, but prioritize branch coverage (especially error paths). Don't chase 100% — some code (defensive panics, `unreachable`) is fine uncovered.

## Test packages

Two layouts:
- `package myapp` (in-package test) — access to unexported names; use for white-box tests
- `package myapp_test` (external test) — only exported API; use for black-box / API contract tests

Mix both: white-box for internal logic, black-box for the public API.

## Common anti-patterns

- `t.Fatal` in goroutines — use `t.Errorf` and a `sync.WaitGroup`; `t.Fatal` only works on the test goroutine
- `time.Sleep` to wait for async work — use channels or `eventually` helpers
- Testing implementation details — test behavior, not internals
- Shared global state — use fresh fixtures per test
- `t.Skip` without reason — `t.Skip("requires postgres")` with a clear condition

## Verification

```bash
gofmt -l .
go vet ./...
golangci-lint run
go test -race -count=1 -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1
```

All must be green. Race detector on, always.
