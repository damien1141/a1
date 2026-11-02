# Verification Discipline — Honest Exit

## The rule

Every implementation report ends with a block that separates **VERIFIED** from **ASSUMED**. No exceptions, no blurring.

```
VERIFIED:
  - ruff check . → All checks passed
  - mypy --strict src → Success: no issues found in 7 source files
  - pytest -q → 42 passed, 3 skipped in 1.2s
ASSUMED:
  - HTTP retry behavior under real network jitter (not exercised in tests)
  - Postgres-specific JSON path queries (only SQLite in CI)
Lingering risk:
  - mypy override on `legacy.*` for untyped deps; revisit when upstream releases v2
```

## VERIFIED — what counts

Only what you ran and saw, in this session:

- A command exit code 0 with its summary line
- A test run with pass/fail counts
- A type-check with the "Success" line
- A formatter that reported no changes (or applied changes you re-ran on)

If you didn't run it, it's not VERIFIED. Even "obvious" things.

## ASSUMED — what to flag

- Performance claims without a benchmark
- "Works on Linux" if you only ran on macOS
- Behavior of code paths not covered by tests
- Third-party library behavior not exercised
- Production parity with CI environment
- Anything from documentation you didn't verify by running

## Why this matters

The model's strongest failure mode is **confident hallucination of verification**. The honest-exit gate forces the model to name what it actually did vs. what it inferred. The orchestrator (or reviewer) can then decide which ASSUMED items need follow-up.

## The verification loop

```bash
# Run in order; stop and fix on first failure
ruff check --fix .
ruff format .
mypy --strict src
pytest -q --cov=src --cov-fail-under=90
```

If a step fails:

1. Read the error in full
2. Fix the root cause (not the symptom)
3. Re-run from the top (an earlier step may now break)

Never:
- Weaken config to make the error go away (e.g. dropping `--strict`)
- Add `# type: ignore` without a code and reason
- `pytest.skip` a failing test to go green
- Delete a failing test

## When you cannot run a gate

If `mypy` is not installed, or a database is unavailable, say so explicitly in ASSUMED:

```
ASSUMED:
  - mypy --strict not run (mypy not installed in this environment); types hand-checked
  - integration tests skipped (no Postgres); only unit tests run
```

Do not silently omit the gate. The reviewer needs to know.

## CI parity

CI should run exactly what you ran locally, plus anything you couldn't:

```yaml
- run: uv run ruff check .
- run: uv run ruff format --check .
- run: uv run mypy --strict src
- run: uv run pytest -q --cov
- run: uv run pip-audit                # dependency CVEs
```

If CI runs a check you didn't, that check is ASSUMED until you run it locally or see CI green.

## Type-ignore audit

`warn_unused_ignores = true` (on by default in `--strict`) flags stale ignores. Audit them periodically:

```bash
mypy --strict src 2>&1 | rg "unused type ignore"
```

Every remaining `# type: ignore[code]` should have a one-line comment explaining the reason.

## Pre-commit hook

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/astral-sh/ruff-pre-commit
    rev: v0.5.0
    hooks:
      - id: ruff
        args: [--fix]
      - id: ruff-format
  - repo: https://github.com/pre-commit/mirrors-mypy
    rev: v1.10.0
    hooks:
      - id: mypy
        args: [--strict]
        additional_dependencies: [httpx, anyio]
```

Catches issues before they reach CI. But pre-commit is a convenience, not a substitute for the explicit verification gate in your report.

## The exit checklist

Before writing the final report, confirm:

- [ ] `ruff check .` clean
- [ ] `ruff format --check .` clean
- [ ] `mypy --strict <pkg>` clean
- [ ] `pytest -q` green, coverage met
- [ ] No new `# type: ignore` without a code
- [ ] No `pytest.skip` or `xfail` added to silence failures
- [ ] Report lists VERIFIED (with command + summary) and ASSUMED (with reason)

If you cannot check a box, that fact goes in ASSUMED with the reason.
