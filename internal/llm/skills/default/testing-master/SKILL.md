---
name: testing-master
description: "Use when writing tests of any kind — unit, integration, E2E, contract, property, snapshot, or mutation. Designs test pyramids, runs red-green-refactor TDD, builds mocking strategies and fixtures, configures Playwright 1.45+ E2E with locators/fixtures/traces/network mocking, and enforces coverage thresholds before exit. Language-agnostic principles plus pytest, vitest/jest, go test, cargo test, dotnet test references."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: quality
  triggers: "test,testing,unit test,integration test,e2e,TDD,red-green-refactor,mock,fixture,parametrize,snapshot,mutation testing,coverage,Playwright,Pact,contract testing,property-based,hypothesis,fast-check,pytest,vitest,jest,go test,cargo test,dotnet test,flaky test,quality gate"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "code-reviewer,debugging-wizard,python-pro,typescript-pro,golang-pro"
---

# Testing Master

Full-stack testing specialist: test pyramid design, TDD discipline, mocking/fixtures/parametrization, Playwright 1.45+ E2E, contract and property-based testing, mutation testing, and coverage-gated exit. Framework-agnostic principles with language-specific reference cards.

## When to Use

- Designing or rebalancing a test suite across unit / integration / E2E layers
- Practising TDD red-green-refactor on a feature or bug fix
- Building mocking strategies, factories, fixtures, or parametrized table-driven tests
- Writing Playwright 1.45+ E2E (locators, fixtures, parallelism, traces, network mocking, visual comparisons)
- Adding contract tests (Pact) or property-based tests (hypothesis / fast-check)
- Enforcing coverage thresholds; hunting flaky tests
- Picking the right pattern for pytest, vitest/jest, go test, cargo test, or dotnet test

## Operating Loop

1. **Classify** — Name the artefact and its pyramid layer (unit / integration / E2E). State the ONE load-bearing risk the tests must pin down.
2. **Red** — Write one minimal failing test. Run it and **observe the failure** — a test you have never seen fail proves nothing.
3. **Green** — Simplest passing code. No extra features, no premature optimisation.
4. **Refactor** — Improve names, extract helpers, remove duplication. Tests stay green. No new behaviour.
5. **Harden** — Add edge cases (empty, null, boundary, error path) via `parametrize` or property-based generators. Mock only at trust boundaries (external APIs, DB, clock).
6. **Verify (gate)** — Run until clean:
   - Unit + integration: `pytest -q` / `vitest run` / `go test ./...` / `cargo test` / `dotnet test`
   - Coverage: `pytest --cov --cov-fail-under=80` (or `vitest run --coverage --coverage.thresholds.lines=80`)
   - E2E: `npx playwright test` with `trace: 'retain-on-failure'`
   - Flaky sweep: `--repeat-each=10` on new E2E tests
   - Optional mutation check: `mutmut run` / `stryker run` — surviving mutants = weak assertions
   - If any step fails: fix root cause, do not weaken thresholds.
7. **Exit** — Report VERIFIED (ran, saw green) vs ASSUMED (believed). List coverage %, flaky quarantines, untested branches.

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Test pyramid & layer selection | `references/test-pyramid.md` | deciding unit vs integration vs E2E, suite ratios, when each layer pays off |
| TDD iron laws & red-green-refactor | `references/tdd-iron-laws.md` | practising TDD, bug-fix-test-first, rationalisations to reject |
| Mocking, fixtures, parametrize, snapshots | `references/mocking-and-fixtures.md` | mocking strategies, factories, fixtures, parametrize, snapshot tests |
| Playwright 1.45+ E2E | `references/playwright-e2e.md` | locators, fixtures, parallelism, traces, network mocking, visual testing, POM |
| Contract, property, mutation testing | `references/advanced-techniques.md` | Pact contract tests, hypothesis/fast-check property tests, mutation testing, coverage thresholds |
| Framework reference cards | `references/framework-reference.md` | pytest, vitest/jest, go test, cargo test, dotnet test patterns |
| Anti-patterns, flaky tests, quality gates | `references/quality-and-antipatterns.md` | test review, flaky test hunting, quality gates, test reports, shift-left |

## Constraints

### MUST DO
- Write the failing test first; observe it fail before implementing
- Test observable behaviour, not implementation details (no asserting on private methods or mock call counts alone)
- Cover happy path AND error/edge cases (empty, null, max, boundary, concurrent)
- Mock only at trust boundaries (external APIs, DB, clock, filesystem); keep real implementations between them
- Use factories for complete mock objects with sensible defaults
- Keep tests independent — no order dependence, no shared mutable state
- Use semantic locators in E2E (`getByRole`, `getByLabel`) over CSS classes
- Set coverage thresholds in CI; fail the build when they drop
- Add a regression test for every bug fix before patching
- Quarantine flaky tests with a ticket link and fix them — never re-run until green

### MUST NOT DO
- Write production code before a failing test exists
- Test mock behaviour instead of real output (`expect(mock).toHaveBeenCalled()` without asserting the result)
- Use `waitForTimeout` or arbitrary sleeps — wait for element state or response
- Use CSS-class selectors when role/label/test-id exists
- Ship `_resetForTesting()` methods on production classes — use fresh instances per test
- Skip error-path testing (only the success branch of a try/catch)
- Use production data in tests — use fixtures or factories
- Ignore flaky tests — quarantine and root-cause them

## Code Examples

### TDD red-green-refactor (Vitest)
```typescript
// RED — minimal failing test
import { describe, it, expect } from 'vitest';
import { calculateDiscount } from './pricing';

it('applies 10% discount for premium users', () => {
  expect(calculateDiscount({ price: 100, userTier: 'premium' })).toBe(90);
});
// Run: ✗ FAIL — calculateDiscount is not defined

// GREEN — simplest passing code, then REFACTOR with named constant
const PREMIUM_DISCOUNT = 0.9;
export function calculateDiscount({ price, userTier }: { price: number; userTier: string }): number {
  return userTier === 'premium' ? price * PREMIUM_DISCOUNT : price;
}
```

### Mock at trust boundary only (pytest)
```python
from unittest.mock import AsyncMock
import pytest
from app.user_service import UserService

@pytest.fixture
def mock_repo() -> AsyncMock:
    repo = AsyncMock()
    repo.find_by_id.return_value = {"id": "1", "name": "Alice", "email": "a@x.com"}
    return repo

async def test_returns_user_when_found(mock_repo: AsyncMock) -> None:
    service = UserService(mock_repo)
    user = await service.get_user("1")
    assert user["name"] == "Alice"                          # primary: real output
    mock_repo.find_by_id.assert_awaited_once_with("1")      # secondary: boundary call
```

### Playwright — fixture + role locator + network mock
```typescript
// fixtures.ts
import { test as base, expect } from '@playwright/test';
export const test = base.extend<{ apiMock: void }>({
  apiMock: async ({ page }, use) => {
    await page.route('**/api/users', (r) =>
      r.fulfill({ status: 200, json: [{ id: 1, name: 'Alice' }] }));
    await use();
  },
});
export { expect };

// tests/users.spec.ts
import { test, expect } from '../fixtures';
test('renders users from API', async ({ page, apiMock }) => {
  await page.goto('/users');
  await expect(page.getByRole('heading', { name: 'Users' })).toBeVisible();
  await expect(page.getByText('Alice')).toBeVisible();
});
```

### Coverage gate (Vitest)
```typescript
// vitest.config.ts
export default defineConfig({
  test: { coverage: {
    provider: 'v8',
    thresholds: { lines: 80, functions: 80, branches: 75, statements: 80 },
  } },
});
// CI: vitest run --coverage  → exits non-zero if thresholds missed
```

## Output Template

When delivering a tested feature, provide in this order:

1. **Test files** with parametrized cases and edge cases
2. **Production code** that satisfies the tests (or the diff)
3. **Framework config** deltas (`pyproject.toml`, `vitest.config.ts`, `playwright.config.ts`) if thresholds changed
4. **Verification block** — exact commands and results:
   ```
   $ pytest -q --cov=app --cov-fail-under=80
   24 passed, coverage: 87% (threshold 80%)
   $ npx playwright test --repeat-each=10 tests/checkout.spec.ts
   10 passed, 0 flaky
   ```
5. **Exit report** — VERIFIED / ASSUMED / coverage % / flaky quarantines / untested branches

## Knowledge Reference

Test pyramid · TDD red-green-refactor · iron laws (no code without failing test; observe the red) · mocking at trust boundaries · fixtures & factories · `parametrize` · snapshot tests (sparingly) · mutation testing (mutmut, Stryker) · coverage thresholds (lines/branches, fail-under) · property-based (hypothesis, fast-check) · contract testing (Pact) · Playwright 1.45+ (locators, fixtures, traces, network mocking, visual comparisons, sharding, storageState) · framework cards: pytest, vitest/jest, go test, cargo test, dotnet test · flaky quarantine · quality gates · shift-left · axe-core
