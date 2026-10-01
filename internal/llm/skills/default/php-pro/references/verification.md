# Verification Discipline — Honest Exit

## The rule

Every implementation report ends with a block separating **VERIFIED** from **ASSUMED**.

```
VERIFIED:
  - vendor/bin/pint --test
      Files: 12, Tests: 156, Failed: 0
  - vendor/bin/phpstan analyse --level=9
      [OK] No errors
  - vendor/bin/pest --coverage
      Tests: 42, Passed: 42, Coverage: 87.3%
ASSUMED:
  - Postgres-specific JSON queries (only SQLite in CI)
  - Octane state-leak audit under real load
  - Redis queue behavior with Horizon (not run in CI)
Lingering risk:
  - @phpstan-ignore-line on src/Service/LegacyImporter.php:42 (reason: untyped third-party SDK)
```

## VERIFIED — what counts

Only what you ran and saw:

- `pint --test` (or `php-cs-fixer fix --dry-run`) — PSR-12 clean
- `phpstan analyse --level=9` — `[OK] No errors`
- `phpunit`/`pest` — pass/fail count + coverage
- A migration SQL preview you reviewed (`php artisan migrate --pretend`)

## ASSUMED — what to flag

- Cross-DB behavior (only SQLite in CI, Postgres in prod)
- Octane state-leak behavior under real load
- Queue failure paths not exercised
- Performance under load
- Third-party SDK behavior not exercised
- Production opcache / JIT settings

## The verification loop

```bash
vendor/bin/pint --test                              # PSR-12
vendor/bin/phpstan analyse --level=9                # static analysis
vendor/bin/phpunit --coverage-text                  # or: vendor/bin/pest --coverage
# For migrations:
php artisan migrate --pretend --database=sqlite > /dev/null  # review SQL
```

If a step fails:

1. Read the error in full
2. Fix the root cause (not the symptom)
3. Re-run from the top — an earlier step may now break

Never:
- `@phpstan-ignore-line` / `@phpstan-ignore-next-line` without a reason
- `$this->markTestSkipped()` to silence failures
- Delete a failing test
- Weaken PHPStan level

## When you cannot run a gate

If PHPStan isn't installed or Postgres isn't available:

```
ASSUMED:
  - PHPStan not run (not installed in this env); types hand-checked
  - Integration tests skipped (no Postgres); unit tests green
```

Do not silently omit the gate.

## CI parity

CI should run exactly what you ran locally, plus anything you couldn't:

```yaml
- run: composer install --no-interaction --prefer-dist
- run: vendor/bin/pint --test
- run: vendor/bin/phpstan analyse --level=9
- run: vendor/bin/pest --coverage --coverage-clover=coverage.xml
- uses: actions/upload-artifact@v4
  with: { name: coverage, path: coverage.xml }
```

For cross-DB testing, run a matrix:
```yaml
strategy:
  matrix:
    db: [sqlite, postgres, mysql]
```

## PHPStan config (`phpstan.neon`)

```neon
includes:
    - vendor/larastan/larastan/extension.neon
parameters:
    level: 9
    paths:
        - app
        - tests
    checkMissingVarAnnotation: true
    checkGenericClassInNonGenericObjectType: false
    ignoreErrors:
        -
            message: '#Access to an undefined property#'
            path: app/Service/LegacyImporter.php
            reason: 'untyped third-party SDK; wrapping in v2'
```

Every `ignoreErrors` entry should have a `reason`. Audit periodically:

```bash
grep -A1 "ignoreErrors" phpstan.neon | grep -v "reason:"
```

## Larastan — Laravel-specific rules

```bash
composer require --dev larastan/larastan
```

Larastan understands Eloquent magic (`User::where(...)->first()`, `User::factory()`). Without it, PHPStan reports false positives on Laravel code.

## Pint config (`pint.json`)

```json
{
  "preset": "laravel",
  "rules": {
    "declare_strict_types": true,
    "ordered_imports": { "sort_algorithm": "alpha" },
    "single_quote": true,
    "trailing_comma_in_multiline": true
  }
}
```

`preset: laravel` is the sensible default. Override with project-specific rules.

## The exit checklist

Before writing the final report:

- [ ] `pint --test` clean
- [ ] `phpstan analyse --level=9` clean
- [ ] `phpunit`/`pest` green, coverage ≥ 80%
- [ ] No new `@phpstan-ignore-*` without reason
- [ ] No `$this->markTestSkipped()` to silence failures
- [ ] Migration SQL reviewed (if schema changed)
- [ ] Report lists VERIFIED (with command + summary) and ASSUMED (with reason)

If you cannot check a box, that fact goes in ASSUMED with the reason.
