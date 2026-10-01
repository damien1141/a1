# Contract, Property, Mutation Testing & Coverage Thresholds

Once unit + integration + E2E are in place, four advanced techniques close the remaining gaps: **contract tests** catch service-boundary drift, **property-based tests** catch edge cases you didn't think of, **mutation testing** measures assertion strength, and **coverage thresholds** prevent backsliding.

## Contract Testing (Pact)

Contract testing replaces heavy cross-service E2E with consumer-driven contracts. Each consumer writes a test that captures the interactions it expects from a provider; the contract is verified against the provider in isolation. No full stack required.

### When to use Pact
- Microservices where cross-service E2E is too slow or flaky
- A provider serves multiple consumers with different needs
- You want to verify a provider hasn't broken a consumer without standing up the whole system
- Public APIs consumed by third parties

### Consumer test (JavaScript)
```typescript
const { Pact } = require('@pact-foundation/pact');
const path = require('path');

const provider = new Pact({
  consumer: 'order-service',
  provider: 'user-service',
  port: 4001,
  log: path.resolve(__dirname, 'logs', 'pact.log'),
  dir: path.resolve(__dirname, 'pacts'),
});

beforeAll(() => provider.setup());
afterAll(() => provider.finalize());

test('fetches user by id', async () => {
  await provider.addInteraction({
    uponReceiving: 'a request for a user',
    withRequest: { method: 'GET', path: '/users/1' },
    willRespondWith: {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
      body: { id: 1, name: 'Alice', email: 'a@x.com' },
    },
  });

  const user = await fetchUser(1);
  expect(user).toEqual({ id: 1, name: 'Alice', email: 'a@x.com' });
});
```

### Provider verification (any language)
```bash
# Verify the provider against the consumer's pact
pact-verifier --pact-url=./pacts/order-service-user-service.json \
  --provider-base-url=http://localhost:8080 \
  --provider-states-setup-url=http://localhost:8080/test/setup
```

### Pact broker (CI)
Publish pacts and verification results to a Pact Broker. The broker can block a provider deploy if a consumer pact isn't verified, and block a consumer deploy if the provider can't satisfy the new contract. This is `can-i-deploy`:

```bash
pact-broker can-i-deploy --pacticipant order-service \
  --version $GIT_SHA --to-environment production
```

### When NOT to use Pact
- Single monolith — internal contract tests add ceremony for no benefit; use integration tests instead
- Genuinely stateful, multi-call workflows where the order of calls matters — Pact handles this, but the tests get complex; consider E2E for those flows
- Provider and consumer are the same team — a conversation is cheaper than a contract

## Property-Based Testing

Property-based tests assert that an invariant holds for **all inputs in a class**, not just the examples you thought of. The framework generates hundreds of inputs, runs the property, and **shrinks** any failing case to the smallest reproducer.

### hypothesis (Python)
```python
from hypothesis import given, strategies as st

@given(st.lists(st.integers()))
def test_sum_is_commutative(xs: list[int]) -> None:
    assert sum(xs) == sum(reversed(xs))

@given(st.lists(st.integers(min_value=0)))
def test_sum_is_non_negative(xs: list[int]) -> None:
    assert sum(xs) >= 0

@given(st.lists(st.integers()))
def test_sort_is_idempotent(xs: list[int]) -> None:
    once = sorted(xs)
    twice = sorted(once)
    assert once == twice
```

### fast-check (TypeScript)
```typescript
import { fc, test as fcTest } from '@fast-check/vitest';

fcTest.prop([fc.array(fc.integer())])('sum is commutative', (xs) => {
  expect(sum(xs)).toBe(sum([...xs].reverse()));
});

fcTest.prop([fc.array(fc.integer({ min: 0 }))])('sum is non-negative', (xs) => {
  expect(sum(xs)).toBeGreaterThanOrEqual(0);
});
```

### When property-based testing pays off
- Pure functions with clear invariants (parsers, serializers, math, codecs)
- Round-trip properties (`decode(encode(x)) === x`)
- Idempotence (`f(f(x)) === f(x)`)
- Commutativity / associativity
- State machine transitions (use `hypothesis.stateful` or `@fast-check/state`)

### When it doesn't
- Functions with side effects (mock the side effects; test the property on the pure core)
- UI tests (use generative testing sparingly — Playwright is the better tool)
- Cases where you can't express the invariant

### Shrinking
When a property fails, the framework shrinks the failing input to the smallest case that still fails. A failing test on a 100-element list often shrinks to a 2-element list. This is the killer feature — it finds the minimal reproducer for you.

## Mutation Testing

Mutation testing measures **how good your tests are**, not how much code they cover. The tool mutates the production code (flip `+` to `-`, `>` to `>=`, remove a line, invert a condition) and re-runs the tests. If a mutant survives, your tests didn't catch the bug — your coverage number is lying to you.

### Tools
- **Stryker** — JavaScript, TypeScript, C#, Java, Scala, Python (incremental)
- **mutmut** — Python
- **cargo-mutants** — Rust
- **PIT** — Java/JVM

### Stryker (TypeScript)
```bash
npm install --save-dev @stryker-mutator/core
npx stryker init
npx stryker run
```

```json
// stryker.conf.json
{
  "mutator": { "excludedMutations": ["StringLiteral"] },
  "testRunner": "vitest",
  "coverageAnalysis": "perTest",
  "thresholds": { "high": 80, "low": 60, "break": 50 }
}
```

### mutmut (Python)
```bash
pip install mutmut
mutmut run                 # run all mutations
mutmut results             # see surviving mutants
mutmut show <id>           # show a specific surviving mutant
```

### Reading mutation scores
- **Mutation score** = killed mutants / total mutants (excluding equivalents)
- Target ≥80% on critical modules
- A surviving mutant means: a real bug could be introduced here and your tests wouldn't catch it
- **Equivalent mutants** (mutants that produce semantically equivalent code) are false positives — flag and exclude them

### When to run mutation testing
- Not on every commit — it's slow (10x test suite time)
- Nightly on critical modules
- Before merging a high-risk change
- When you suspect your coverage number is inflated

## Coverage Thresholds

Coverage is a **lower bound**, not a quality signal. 100% line coverage with weak assertions ships bugs. But coverage that drops should still block the build — it's a backstop against "I'll add tests later".

### What to measure
| Metric | What it catches |
|---|---|
| **Line** | Did the line execute? |
| **Branch** | Did both sides of a conditional execute? |
| **Function** | Was the function called? |
| **Statement** | (JS) Did the statement execute? |
| **MC/DC** | (Aviation/safety) Did each boolean sub-condition independently affect the decision? |

Branch coverage is more honest than line coverage. A 4-branch `if/else if/else if/else` can hit 100% line coverage with only 2 of 4 branches.

### Reasonable thresholds
- **Lines**: 80% minimum, 90% target
- **Branches**: 75% minimum, 85% target
- **Functions**: 80% minimum, 90% target
- Critical modules (auth, payment, data integrity): push to 95%+ branch

### Configuring

#### pytest-cov
```bash
pytest --cov=app --cov-branch --cov-fail-under=80 --cov-report=html
```
```toml
# pyproject.toml
[tool.coverage.run]
branch = true
source = ["app"]
[tool.coverage.report]
fail_under = 80
show_missing = true
```

#### Vitest
```typescript
// vitest.config.ts
export default defineConfig({
  test: {
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html', 'lcov'],
      thresholds: {
        lines: 80, functions: 80, branches: 75, statements: 80,
        perFile: true,   // fail per-file, not just overall
      },
    },
  },
});
```

#### Go
```bash
go test -coverprofile=coverage.out -covermode=atomic ./...
go tool cover -func=coverage.out | grep total | awk '{print $3}'
# Parse and compare to threshold in CI
```

#### Rust
```bash
cargo install cargo-tarpaulin
cargo tarpaulin --out Html --fail-under 80
```

#### .NET
```xml
<!-- coverlet.collector + ReportGenerator -->
dotnet test --collect:"XPlat Code Coverage" /p:CoverletOutputFormat=cobertura
```

### Anti-patterns
- **Chasing 100% line coverage** — leads to tests that exercise code but assert nothing
- **Excluding files to hit the threshold** — don't exclude untested code; flag it instead
- **Coverage as a PR check but no mutation testing** — coverage alone misses weak assertions
- **Setting threshold too high day one** — start where you are, ratchet up over time

## How the Four Techniques Combine

| Technique | What it catches | Cost |
|---|---|---|
| Unit + integration tests | Bugs you thought of | Low |
| Contract tests (Pact) | Service-boundary drift | Medium |
| Property-based tests | Edge cases you didn't think of | Medium |
| Mutation testing | Weak assertions | High (run nightly) |
| Coverage thresholds | Backsliding | Very low |

Use them in this order: ship unit + integration + coverage thresholds first. Add contract tests when you have a second service. Add property-based tests for pure logic modules. Add mutation testing once the suite is stable and you want to harden it.

## Quick Reference

| Need | Tool |
|---|---|
| Consumer-driven contract | Pact (`@pact-foundation/pact`, `pact-python`, `pact-jvm`) |
| Property-based (Python) | `hypothesis` |
| Property-based (TS/JS) | `fast-check` |
| Property-based (Rust) | `proptest` |
| Property-based (Go) | `testing/quick`, `gopter` |
| Mutation (TS/JS/C#/Java) | Stryker |
| Mutation (Python) | mutmut |
| Mutation (Rust) | cargo-mutants |
| Coverage (Python) | `pytest-cov` / `coverage.py` |
| Coverage (JS/TS) | `v8` or `istanbul` provider in Vitest/Jest |
| Coverage (Go) | built-in `go test -cover` |
| Coverage (Rust) | `cargo-tarpaulin` |
| Coverage (.NET) | `coverlet` + `ReportGenerator` |
