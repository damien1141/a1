# Anti-Patterns, Flaky Tests, Quality Gates & Reports

## Testing Anti-Patterns

### 1. Testing the mock instead of the behaviour
```typescript
// ❌ Tests the mock, not the result
it('calls the API', () => {
  const mockApi = vi.fn().mockResolvedValue({ data: 'test' });
  const service = new UserService(mockApi);
  service.getUser(1);
  expect(mockApi).toHaveBeenCalledWith(1);   // mock call is the only assertion
});

// ✅ Tests real behaviour; mock call is secondary evidence
it('returns user data from API', async () => {
  const mockApi = vi.fn().mockResolvedValue({ id: 1, name: 'Alice' });
  const service = new UserService(mockApi);
  const user = await service.getUser(1);
  expect(user?.name).toBe('Alice');             // real output
  expect(mockApi).toHaveBeenCalledWith(1);      // secondary
});
```

### 2. Test-only methods in production
```typescript
// ❌ Production polluted with test concerns
class UserCache {
  private cache = new Map<number, User>();
  getUser(id: number) { return this.cache.get(id); }
  _resetForTesting() { this.cache.clear(); }    // ❌
}

// ✅ Fresh instances per test
function createFreshCache() { return new UserCache(); }   // test helper
```

### 3. Mocking without understanding
```typescript
// ❌ Everything mocked; what did we actually test?
it('processes order', async () => {
  vi.mock('./inventory');
  vi.mock('./payment');
  vi.mock('./shipping');
  vi.mock('./notifications');
  const result = await processOrder(order);
  expect(result.success).toBe(true);
});

// ✅ Real inventory against test DB; mock only external payment
it('processes order with real inventory', async () => {
  const inventory = new InventoryService(testDb);
  const payment = mockPaymentGateway();
  const processor = new OrderProcessor(inventory, payment);
  const result = await processor.process(order);
  expect(result.success).toBe(true);
  expect(await inventory.getStock(order.itemId)).toBe(originalStock - 1);
});
```

### 4. Incomplete mocks
```typescript
// ❌ Mock returns minimal stub — production crashes on missing fields
const mockUserApi = vi.fn().mockResolvedValue({ id: 1, name: 'Test' });

// ✅ Factory fills complete defaults
const mockUserApi = vi.fn().mockResolvedValue(
  UserFactory.create({ name: 'Test' })
);
```

### 5. Tests as afterthought
```typescript
// ❌ "We'll add tests later" → never happens
// Day 1: 500 LOC. Day 30: catastrophic bug in prod. Day 31: "Why no tests?"

// ✅ Test-first; feature + test ship together
it('rejects duplicate usernames', async () => {
  await createUser({ username: 'alice' });
  await expect(createUser({ username: 'alice' }))
    .rejects.toThrow('Username already exists');
});
```

### Detection checklist
| Warning sign | Anti-pattern |
|---|---|
| `expect(mock).toHaveBeenCalled()` with no behaviour assertion | Testing the mock |
| Methods starting with `_` or `ForTesting` in production | Test-only methods |
| Every dependency mocked | Over-mocking |
| Mocks return `{ success: true }` only | Incomplete mocks |
| Test files added weeks after feature ships | Tests as afterthought |
| 100% line coverage, bugs still ship | Weak assertions; add mutation testing |

## Flaky Tests

A flaky test passes sometimes and fails sometimes without code changes. Flaky tests destroy trust in the suite. The correct response is **quarantine + root-cause**, never "re-run until green".

### Root causes (ranked by frequency)

| # | Cause | Symptom | Fix |
|---|---|---|---|
| 1 | Race condition / missing await | Passes locally, fails in CI | Use auto-waiting locators; never `waitForTimeout`; audit `async`/`await` |
| 2 | Test order dependence | Passes alone, fails after another test | Reset state in `beforeEach`; never share mutable module state |
| 3 | Time dependence | Fails at midnight, on DST change, on slow CI | Inject a clock; freeze time in tests |
| 4 | Randomness | Different seed each run | Seed the generator explicitly |
| 5 | Network / external API | Intermittent 500s, timeouts | Mock the boundary; use a fake server (`msw`, `responses`) |
| 6 | Hidden global state | env vars, working directory, locale | Set explicitly in test setup; reset in teardown |
| 7 | Resource cleanup missed | Tests pass early, fail late in suite | Add teardown; check `beforeEach`/`afterEach` symmetry |
| 8 | Floating point / ordering | "Expected [a,b,c], got [b,a,c]" | Sort before comparing; compare as sets |

### Hunting flaky tests
```bash
# Run a single test 10× to confirm flakiness
pytest tests/test_flaky.py::test_x --count=10        # via pytest-repeat
npx playwright test tests/x.spec.ts --repeat-each=10
go test -run TestX -count=50 ./...

# Run the whole suite with randomised order
pytest -p randomly                                       # pytest-randomly
vitest run --sequence.randomize                          # or use sharding
```

### Quarantine discipline
```python
# pytest
@pytest.mark.skip(reason="Flaky: timing-dependent, see #4521")
def test_known_flaky(): ...

# or, with xdist tracking
@pytest.mark.xfail(reason="Flaky: race condition in EventBus")
def test_known_flaky(): ...
```
- Quarantine must include a ticket link
- Quarantined tests don't count toward coverage
- Set a SLA: quarantine >2 weeks → delete and rewrite

### When a test is "flaky" because production is racy
Sometimes the test isn't flaky — production has a race condition. The test is doing its job. Don't quarantine; fix the race.

## Quality Gates

A quality gate is a set of conditions that must pass before code merges or deploys. Make them explicit and enforced by CI — not "best effort".

### Pre-merge gate (every PR)
```
- [ ] Lint clean (ruff / eslint / golangci-lint / clippy)
- [ ] Type check clean (mypy --strict / tsc --noEmit / go vet / cargo check)
- [ ] Unit + integration tests green
- [ ] Coverage ≥ threshold (e.g. 80% lines, 75% branches)
- [ ] No new flaky tests
- [ ] No new SAST findings (semgrep, bandit)
- [ ] No new dependency vulnerabilities (npm audit / pip-audit / cargo audit)
- [ ] No secrets in diff (gitleaks)
- [ ] No new TODO/FIXME without a ticket link
```

### Pre-deploy gate (every release)
```
- [ ] Pre-merge gate green on release branch
- [ ] E2E suite green (Playwright, all browsers, all shards)
- [ ] Performance SLA met (k6 load test p95 < threshold)
- [ ] Security scan clean (trivy fs --severity CRITICAL,HIGH)
- [ ] Accessibility WCAG AA (axe-core)
- [ ] Smoke tests on staging green
- [ ] Rollback plan documented
```

### Feedback cycle targets
| Stage | Target | Trigger |
|---|---|---|
| Unit tests | <5 min | on save / push |
| Integration tests | <15 min | on commit |
| E2E tests | <30 min | on PR |
| Regression suite | <2 hours | nightly |
| Performance tests | <30 min | nightly + pre-release |
| Security scan | <10 min | on PR + nightly |

If a stage exceeds its target, split the suite or move tests down the pyramid.

### Shift-left
The earlier a defect is caught, the cheaper it is to fix. Roughly:
- Requirements phase: 1×
- Code phase: 6×
- Unit test phase: 10×
- Integration test phase: 40×
- System test phase: 100×
- Production: 1000×+

Activities:
- Review requirements for testability
- Create test cases during design (not after)
- TDD: write tests alongside code
- Static analysis on commit (pre-commit hooks)
- Security scanning pre-merge (semgrep, gitleaks)
- Coverage threshold enforced in CI

## Test Reports

A test report should let a reader answer, in under 30 seconds: *did the suite pass, what's the coverage, and what's blocking?*

### Template
```markdown
# Test Report: <Feature>

**Date**: YYYY-MM-DD        **Version**: vX.Y.Z        **Suite**: unit+integration

## Summary
| Metric | Value |
|---|---|
| Total tests | 247 |
| Passed | 245 |
| Failed | 1 |
| Skipped | 1 (ticket #4521) |
| Coverage (lines) | 87% (threshold 80%) ✓ |
| Coverage (branches) | 79% (threshold 75%) ✓ |
| Duration | 42s |

## Failures
### [HIGH] tests/orders/test_checkout.py::test_payment_retry
- **Error**: AssertionError: expected 200, got 503
- **Suspected cause**: payment gateway mock not returning retry-after header
- **Owner**: @alice
- **Action**: Fix mock; this is a test bug, not production

## Coverage gaps
- `src/api/admin.py` — 0% (no tests; risk accepted for v1)
- `src/services/payment.py:45-60` — error handling untested (ticket #4530)

## Sign-off
- [x] All critical issues addressed
- [x] Coverage meets threshold
- [x] Performance SLA met (separate report)
- [ ] E2E suite pending (running on staging)
```

### Severity definitions
| Severity | Criteria | Response time |
|---|---|---|
| **Critical** | Security vulnerability, data loss, system crash, blocks release | Immediate |
| **High** | Major functionality broken, severe perf regression | Before merge |
| **Medium** | Feature partially broken, workaround exists | Next sprint |
| **Low** | Cosmetic, edge case, minor inconvenience | Backlog |

### Quality metrics dashboard
| Metric | Excellent | Good | Needs work |
|---|---|---|---|
| Coverage | >90% | 70–90% | <70% |
| Defect leakage | <2% | 2–5% | >5% |
| Automation | >80% | 60–80% | <60% |
| MTTR | <24h | 24–48h | >48h |
| Flaky tests | 0 | <1% | >1% |

## Defect Management (5 Whys)
```markdown
1. Why did the defect occur? User input not validated
2. Why wasn't it validated? Validation logic missing
3. Why was it missing? Requirement unclear
4. Why was requirement unclear? Acceptance criteria incomplete
5. Why incomplete? No QA review in planning

Root cause: QA not involved in requirements phase
Prevention: Add QA to all planning meetings; require testable acceptance criteria
```

## Defect report template
```markdown
## [CRITICAL] <Title>

**Steps to reproduce**:
1. ...
2. ...

**Expected**: ...
**Actual**: ...
**Environment**: Chrome 120, prod, user role=admin
**Impact**: <business/user>
**Root cause**: <after investigation>
**Fix**: <PR link>
**Regression test**: <test file:line>
```

## Quick Reference

| Need | Action |
|---|---|
| Quarantine flaky test | Skip with ticket link; SLA 2 weeks |
| Verify stability | `--repeat-each=10` / `--count=10` |
| Find race condition | Run with `-race` (Go), `--detectRace` (Node), thread sanitizer |
| Set coverage threshold | `--cov-fail-under=80` (pytest), `thresholds.lines=80` (Vitest) |
| Block PR on regression | Coverage threshold + SAST + secret scan in CI |
| Investigate coverage gap | `coverage report --show-missing` |
| Measure test strength | Mutation testing (Stryker, mutmut) |
