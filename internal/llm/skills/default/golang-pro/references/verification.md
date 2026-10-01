# Verification Discipline — Honest Exit

## The rule

Every implementation report ends with a block separating **VERIFIED** from **ASSUMED**. No exceptions.

```
VERIFIED:
  - gofmt -l .        → (empty)
  - go vet ./...      → (no output)
  - golangci-lint run → (no output)
  - go test -race -count=1 ./...
    ok  myapp/internal/port    0.412s
    ok  myapp/internal/server  0.891s
ASSUMED:
  - pprof profile under production load (not run; bench in CI only)
  - Behavior on darwin/arm64 (only linux/amd64 in CI)
Lingering risk:
  - //nolint:gosec on file open in internal/server/io.go (reason: trusted input)
```

## VERIFIED — what counts

Only what you ran and saw in this session:

- A command exit 0 with its output
- A test run with pass counts
- A `go vet` with no findings
- A benchmark you actually ran (with `benchstat` if comparing)

If you didn't run it, it's not VERIFIED.

## ASSUMED — what to flag

- Performance claims without a benchmark
- Cross-platform behavior you didn't test
- Concurrency correctness not exercised by `-race`
- Third-party library behavior you didn't run
- Production parity with CI environment

## The verification loop

```bash
# Run in order; stop and fix on first failure
gofmt -l .                    # must be empty
go vet ./...
golangci-lint run
go test -race -count=1 ./...
```

If a step fails:

1. Read the error in full
2. Fix the root cause (not the symptom)
3. Re-run from the top (an earlier step may now break)

Never:
- Add `//nolint` to silence — fix the code or document why with a reason comment
- `t.Skip` a failing test to go green
- Delete a failing test
- Weaken `golangci-lint` config to make errors go away

## When you cannot run a gate

If `golangci-lint` isn't installed or a DB is missing, say so:

```
ASSUMED:
  - golangci-lint not run (not installed in this env); go vet ran clean
  - integration tests skipped (no Postgres); only unit tests run
```

Do not silently omit the gate.

## CI parity

CI should run exactly what you ran locally, plus anything you couldn't:

```yaml
# .github/workflows/ci.yml
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - run: go mod download
      - run: gofmt -l . | tee /dev/stderr | (! read)  # fail if non-empty
      - run: go vet ./...
      - uses: golangci/golangci-lint-action@v6
      - run: go test -race -count=1 -coverprofile=cover.out ./...
      - run: go tool cover -func=cover.out | tail -1
```

If CI runs a check you didn't, it's ASSUMED until you see it green.

## `//nolint` audit

`golangci-lint` reports `//nolint` comments with `nolintlint`:

```yaml
linters-settings:
  nolintlint:
    require-explanation: true
    require-specific: true
```

Every `//nolint:linterX // reason` must explain why. Audit periodically:

```bash
golangci-lint run --disable-all --enable=nolintlint ./...
```

## Race detector depth

```bash
go test -race -count=20 ./...    # surface races that depend on scheduling
```

In CI, run with `-count=1` (deterministic) but add a periodic `-count=20` job or run on PRs touching concurrency code.

## Pre-commit hook

```bash
#!/bin/sh
# .git/hooks/pre-commit
gofmt -l . | tee /dev/stderr | (! read) || exit 1
go vet ./... || exit 1
```

Catches issues before they reach CI. But pre-commit is convenience, not a substitute for the explicit gate in your report.

## The exit checklist

Before writing the final report:

- [ ] `gofmt -l .` empty
- [ ] `go vet ./...` clean
- [ ] `golangci-lint run` clean
- [ ] `go test -race ./...` green
- [ ] No new `//nolint` without a reason
- [ ] No `t.Skip` added to silence failures
- [ ] Report lists VERIFIED (with command + summary) and ASSUMED (with reason)

If you cannot check a box, that fact goes in ASSUMED with the reason.
