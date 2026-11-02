# Verification Discipline — Honest Exit

## The rule

Every implementation report ends with a block separating **VERIFIED** from **ASSUMED**.

```
VERIFIED:
  - dotnet build -WarnAsError      → Build succeeded, 0 Warning(s)
  - dotnet test --collect:"XPlat Code Coverage"
      Passed: 42, Failed: 0, Skipped: 0
      coverage: 87.3%
  - dotnet format --verify-no-changes  → no changes needed
ASSUMED:
  - Production perf (benchmarks in CI only, no real load)
  - Postgres-specific behavior (only SQLite in unit tests; Testcontainers in CI)
  - Native AOT publish (not run; app uses reflection in JSON serialization)
Lingering risk:
  - #pragma warning disable CA1860 on Product.cs:42 (reason: false positive on EF navigation)
```

## VERIFIED — what counts

Only what you ran and saw in this session:

- `dotnet build` with 0 warnings (under `-WarnAsError`)
- `dotnet test` with pass/fail counts
- `dotnet format --verify-no-changes` clean
- A migration you generated and reviewed

## ASSUMED — what to flag

- Cross-platform behavior you didn't run (Windows if you built on Linux)
- Production DB behavior with seed data
- Performance claims without a benchmark
- Native AOT compatibility (reflection-heavy code)
- Third-party library behavior not exercised

## The verification loop

```bash
dotnet format --verify-no-changes
dotnet build -WarnAsError
dotnet test --collect:"XPlat Code Coverage"
# EF migrations (if schema changed):
dotnet ef migrations add <Name> --output-dir Migrations
dotnet ef database update
```

If a step fails:

1. Read the error in full
2. Fix the root cause (not the symptom)
3. Re-run from the top — an earlier step may now break

Never:
- `#pragma warning disable` without a reason comment
- `[Fact(Skip = "flaky")]` to silence failures
- Delete a failing test
- Weaken `TreatWarningsAsErrors`

## When you cannot run a gate

If the database is unavailable or `dotnet ef` isn't installed:

```
ASSUMED:
  - EF migration not generated (dotnet-ef not installed); schema manually reviewed
  - Integration tests skipped (no Postgres); unit tests green
```

Do not silently omit the gate.

## CI parity

CI should run exactly what you ran locally, plus anything you couldn't:

```yaml
- run: dotnet format --verify-no-changes
- run: dotnet build -WarnAsError -c Release
- run: dotnet test --collect:"XPlat Code Coverage" -c Release
- uses: actions/upload-artifact@v4
  with: { name: coverage, path: '**/coverage.cobertura.xml' }
```

Run a separate job with Testcontainers for real-DB integration tests.

## Analyzer config

```xml
<!-- .editorconfig -->
[*.cs]
dotnet_analyzer_diagnostic.severity = error

# Specific rules
dotnet_diagnostic.CA1707.severity = none          # naming
dotnet_diagnostic.CA1860.severity = suggestion    # avoid constant 'Any()'
csharp_style_namespace_declarations = file_scoped:warning
```

`dotnet format` respects `.editorconfig` analyzers. Use `severity = error` for rules you want to block CI.

## `#pragma` audit

```bash
grep -rn "#pragma warning disable" src/ | grep -v "// .*reason"
```

Every `#pragma` should have a reason. Use `dotnet format analyzers --verify-no-changes` to catch unused `#pragma`.

## EF migration review checklist

Before applying a migration:

- [ ] No unexpected `DROP TABLE` / `DROP COLUMN`
- [ ] Indexes created with appropriate `IF NOT EXISTS`
- [ ] Foreign keys have `ON DELETE` behavior
- [ ] `nullable` vs `required` matches the entity
- [ ] `Up` and `Down` are symmetric
- [ ] Data migrations (if any) handle existing rows

Generate SQL preview without applying:
```bash
dotnet ef migrations script 0 InitialCreate
```

## Pre-commit hooks

```bash
#!/bin/sh
# .git/hooks/pre-commit
dotnet format --verify-no-changes || exit 1
```

Or use `husky`/`pre-commit` frameworks. Convenience, not a substitute for the explicit gate.

## The exit checklist

Before writing the final report:

- [ ] `dotnet format --verify-no-changes` clean
- [ ] `dotnet build -WarnAsError` clean
- [ ] `dotnet test` green, coverage met
- [ ] No new `#pragma warning disable` without reason
- [ ] No `[Fact(Skip = ...)]` added to silence failures
- [ ] EF migrations reviewed (if schema changed)
- [ ] Report lists VERIFIED (with command + summary) and ASSUMED (with reason)

If you cannot check a box, that fact goes in ASSUMED with the reason.
