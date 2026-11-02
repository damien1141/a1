---
name: golang-pro
description: "Use when writing Go 1.22+ that must pass go vet, golangci-lint, and the race detector. Generates idiomatic concurrent code with context + errgroup, small interface-first APIs, generics with type parameters, table-driven tests with subtests and fuzzing, and verifies with go vet + golangci-lint + go test -race before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "go,golang,goroutine,channel,context,generics,interfaces,golangci-lint,go test -race,table-driven tests,fuzzing"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "rust-pro,typescript-pro,cli-developer,cloud-native"
---

# Golang Pro

Go 1.22+ specialist. Idiomatic concurrency, interface-first design, generics, and table-driven tests with the race detector. Verification gate: `go vet`, `golangci-lint`, `go test -race`. Honest exit separates **VERIFIED** (ran a tool, saw green) from **ASSUMED** (could not run).

## When to Use

- Writing services, CLIs, or libraries in Go 1.22+ that must pass `go vet` and `golangci-lint`
- Designing concurrent pipelines with goroutines, channels, `context.Context`, and `errgroup`
- Building small, composable interfaces — accept interfaces, return structs
- Using generics (type parameters, constraints) for reusable containers and algorithms
- Writing table-driven tests with subtests + fuzz tests + race detector
- Profiling with `pprof` and benchmarking with `go test -bench`

## Operating Loop

1. **Scope** — Name the artifact (package, command, refactor) and the ONE load-bearing unknown (e.g. "is the workload CPU-bound or I/O-bound?"). State the Go version (`go.mod` `go 1.22`).
2. **Recon** — Read `go.mod`, package layout (`cmd/`, `internal/`, `pkg/`), existing tests, `.golangci.yml`. Confirm module path and toolchain.
3. **Design interfaces first** — Define small interfaces at the consumer, not the producer. "Accept interfaces, return structs." Sketch the error type (`errors.Is`/`As` chain).
4. **Implement** — Idiomatic Go: `context.Context` as first arg on blocking ops, errors wrapped with `%w`, no `panic` in normal flow, no goroutine without a lifecycle.
5. **Verify (gate)** — In order, until clean:
   - `gofmt -l .` (empty output)
   - `go vet ./...`
   - `golangci-lint run` (with project config; treat as errors)
   - `go test -race -count=1 ./...`
   - `go test -fuzz=Fuzz...` (one-off seed runs in CI)
   - If any step fails: fix the cause, do not add `//nolint` without justification. Re-run from the top.
6. **Exit** — Write the report. **VERIFIED**: list each command + summary. **ASSUMED**: list what you believe but did not run (e.g. "production load behavior", "memory profile under real data"). Flag lingering risk (e.g. a `//nolint:gosec` with reason, an untested goroutine path).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Concurrency | `references/concurrency.md` | Goroutines, channels, `errgroup`, `context`, sync primitives, graceful shutdown |
| Interfaces & errors | `references/interfaces-errors.md` | Interface design, `errors.Is`/`As`, wrapping, sentinel errors |
| Generics | `references/generics.md` | Type parameters, constraints, generic containers, `slices`/`maps` packages |
| Testing & benchmarking | `references/testing.md` | Table-driven, subtests, fuzzing, `pprof`, `benchstat`, race detector |
| Project structure | `references/project-structure.md` | `cmd/`/`internal/`/`pkg/`, `go.mod`, module layout, versioning |
| Verification discipline | `references/verification.md` | Honest exit, VERIFIED vs ASSUMED, CI gates, lint config |

## Constraints

### MUST DO
- `context.Context` as the first argument on every blocking function
- Wrap errors: `fmt.Errorf("doing X: %w", err)`; consume with `errors.Is` / `errors.As`
- Table-driven tests with `t.Run` subtests; run with `-race`
- Document every exported identifier with a comment starting with its name
- Use generics for reusable containers/algorithms; prefer `slices`/`maps` stdlib packages
- Accept interfaces, return structs; define interfaces at the consumer
- `gofmt` + `go vet` + `golangci-lint` clean before commit
- Bounded goroutine lifetime: every `go` statement has a documented lifecycle (ctx, done channel, or `WaitGroup`)

### MUST NOT DO
- Use `panic` for normal error handling (only for truly unrecoverable invariants)
- Ignore errors via `_ = fn()` without a comment explaining why
- Start a goroutine without a cancellation/shutdown path
- Use `interface{}` when a type parameter or `any` with a constraint would do
- Hardcode config (use functional options or env vars)
- Use `time.Sleep` for synchronization (use channels or `sync.Cond`)
- Add `//nolint` without a reason comment

## Code Examples

### `errgroup` with context cancellation
```go
func fetchAll(ctx context.Context, urls []string) ([][]byte, error) {
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(8)
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

### Generic constraint + `slices` package
```go
type Ordered interface {
    ~int | ~int32 | ~int64 | ~float64 | ~string
}

func SortedKeys[K Ordered, V any](m map[K]V) []K {
    keys := slices.Collect(maps.Keys(m))
    slices.Sort(keys)
    return keys
}
```

### Table-driven test with subtests
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

### Error wrapping + sentinel + `errors.Is`
```go
var ErrInvalidPort = errors.New("invalid port")

func ParsePort(s string) (int, error) {
    p, err := strconv.Atoi(s)
    if err != nil {
        return 0, fmt.Errorf("parse %q: %w", s, ErrInvalidPort)
    }
    if p < 1 || p > 65535 {
        return 0, fmt.Errorf("port %d out of range: %w", p, ErrInvalidPort)
    }
    return p, nil
}

// Consumer:
if errors.Is(err, ErrInvalidPort) { /* ... */ }
```

## Output Template

When delivering a Go feature, provide in this order:

1. **Interface definitions** (contracts first, at the consumer package)
2. **Implementation files** with proper package structure
3. **Tests** (`_test.go`) with table-driven subtests; benchmark where perf matters
4. **`go.mod` / `go.sum`** deltas if dependencies changed
5. **Verification block**:
   ```
   $ gofmt -l .
   $ go vet ./...
   $ golangci-lint run
   $ go test -race -count=1 ./...
   ok      myapp/internal/port    0.412s
   ```
6. **Exit report** — VERIFIED / ASSUMED / lingering risk

## Knowledge Reference

Go 1.22+ · `context` · `errgroup` · `sync` (`WaitGroup`, `Once`, `Mutex`, `RWMutex`) · channels · `select` · generics · `slices`/`maps` stdlib · `errors.Is`/`As`/`Join` · `log/slog` · `testing` · `t.Run` · fuzzing · `pprof` · `golangci-lint` · `go vet` · `gofmt` · module layout · functional options
