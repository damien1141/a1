# Project Structure & Modules

## Standard layout

```
myapp/
├── go.mod
├── go.sum
├── cmd/
│   └── myapp/
│       └── main.go          # entry point; thin
├── internal/                # private to this module
│   ├── port/
│   │   ├── port.go
│   │   └── port_test.go
│   └── server/
│       └── server.go
├── pkg/                     # OK to import from external modules (use sparingly)
│   └── api/
│       └── api.go
├── api/
│   └── openapi.yaml         # contracts
├── .golangci.yml
├── Makefile
└── README.md
```

- `cmd/<name>/main.go` — minimal: parse flags, wire deps, call `internal/`
- `internal/` — enforce privacy via the Go toolchain; cannot be imported by other modules
- `pkg/` — only for code you explicitly want to expose to external importers; most projects don't need it

## `go.mod`

```go
module github.com/me/myapp

go 1.22

require (
    github.com/spf13/cobra v1.8.1
    golang.org/x/sync v0.7.0
)
```

- The `go 1.22` directive sets the language version (controls loop var semantics, etc.)
- `toolchain` directive pins the Go binary: `toolchain go1.22.4`
- Use `go mod tidy` to clean up `go.mod`/`go.sum`

## Versioning

Tag releases with SemVer: `v1.2.3`. For v2+, the module path must include the major version:

```go
module github.com/me/myapp/v2
```

This prevents accidental v1→v2 breaks.

## Internal packages

`internal/` is enforced by the compiler. Use it liberally:

```
internal/
├── config/
├── domain/        # core types and business rules
├── infra/         # adapters: db, http client, queue
├── service/       # use cases orchestrating domain + infra
└── ports/         # interfaces the services depend on
```

This is "ports & adapters" / hexagonal layout in Go terms.

## Functional options

```go
type Server struct {
    addr string
    timeout time.Duration
}

type Option func(*Server)

func WithTimeout(d time.Duration) Option {
    return func(s *Server) { s.timeout = d }
}

func NewServer(opts ...Option) *Server {
    s := &Server{addr: ":8080", timeout: 30 * time.Second}
    for _, opt := range opts {
        opt(s)
    }
    return s
}

srv := NewServer(WithTimeout(10*time.Second))
```

Functional options are the idiomatic way to make constructors configurable without breaking changes.

## `log/slog` (1.21+)

```go
import "log/slog"

logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))

slog.SetDefault(logger)

slog.Info("started", "addr", addr, "port", port)
// {"time":"...","level":"INFO","msg":"started","addr":":8080","port":8080}
```

Structured logging, no external deps. Use `slog` over `log` in new code.

## Configuration

- 12-factor: env vars for config
- `os.Getenv` for simple cases; `github.com/spf13/viper` or `github.com/kelseyhightower/envconfig` for typed config
- Always validate at startup; fail fast on bad config

```go
type Config struct {
    Port int    `env:"PORT" default:"8080"`
    DSN  string `env:"DSN"  required:"true"`
}
```

## Build tags

```go
//go:build linux

package myapp

// Linux-only code
```

Use build tags for platform-specific code, not for "dev vs prod" toggles (use config for that).

## Makefile

```makefile
.PHONY: verify
verify:
	go test -race -count=1 ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: cover
cover:
	go test -coverprofile=cover.out ./...
	go tool cover -func=cover.out | tail -1

.PHONY: tidy
tidy:
	go mod tidy
```

## `.golangci.yml`

```yaml
linters:
  enable:
    - errcheck
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - unused
    - gosec
    - gocritic
    - revive
linters-settings:
  errcheck:
    check-type-assertions: true
```

## Common pitfalls

- Putting business logic in `main.go` — push it into `internal/`
- Circular imports between `domain` and `infra` — `domain` defines ports; `infra` imports `domain`, never the reverse
- Exporting everything "just in case" — start unexported, export when there's a real external consumer
- One giant `utils` package — split by concern (`internal/strings/`, `internal/time/`)
- `package main` containing tests — tests belong in `package myapp` or `myapp_test`
