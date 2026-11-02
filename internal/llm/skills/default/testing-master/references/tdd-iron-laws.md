# TDD Iron Laws & Red-Green-Refactor

> **NO PRODUCTION CODE WITHOUT A FAILING TEST FIRST.**

Non-negotiable. If you wrote production code before writing a failing test, delete it and start over. No exceptions.

## The Three Iron Laws

### Law 1 — The Fundamental Rule
> You shall not write any production code unless it is to make a failing test pass.

Every line of production code must have a corresponding test that:
1. Was written first
2. Was observed to fail
3. Now passes because of that code

### Law 2 — Proof Through Observation
> If you didn't watch the test fail, you don't know if it tests the right thing.

Mandatory steps:
- Write the test
- Run it and **observe the failure**
- Verify the failure message is meaningful (names the missing symbol, the wrong value, the unexpected exception)
- Only then implement the fix

A test you have never seen fail proves nothing. It may pass for the wrong reason, or it may never execute the code it claims to test.

### Law 3 — The Final Rule
> Production code exists → a test exists that failed first. Otherwise → it's not TDD.

No middle ground. Code written without a prior failing test is not test-driven development, regardless of how many tests exist afterward.

## The Red-Green-Refactor Cycle

### RED — write one minimal failing test
```typescript
it('returns 0 for an empty array', () => {
  expect(sum([])).toBe(0);
});
// Run: ✗ FAIL — sum is not defined
```
- One test at a time
- Minimal scope
- Clear failure message
- Observe the red

### GREEN — implement simplest passing code
```typescript
function sum(numbers: number[]): number {
  return 0;
}
// Run: ✓ PASS
```
- Simplest possible implementation
- No extra features
- No optimisation
- Just make it pass — even returning a constant is fine for the first test

### REFACTOR — improve while tests stay green
```typescript
function sum(numbers: number[]): number {
  return numbers.reduce((acc, n) => acc + n, 0);
}
// Run: ✓ PASS (still)
```
- Tests must stay green
- Remove duplication
- Improve clarity
- No new functionality

## A Full Cycle (pytest)

```python
# RED
def test_calculate_discount_rejects_negative_price():
    with pytest.raises(ValueError, match="non-negative"):
        calculate_discount(price=-1, user_tier="premium")

# Run: ✗ FAIL — calculate_discount doesn't raise

# GREEN
def calculate_discount(*, price: int, user_tier: str) -> int:
    if price < 0:
        raise ValueError("price must be non-negative")
    return price  # wrong but passes the one test

# REFACTOR — next test drives the real logic
def test_calculate_discount_applies_premium_discount():
    assert calculate_discount(price=100, user_tier="premium") == 90

# GREEN (extended)
def calculate_discount(*, price: int, user_tier: str) -> int:
    if price < 0:
        raise ValueError("price must be non-negative")
    return int(price * 0.9) if user_tier == "premium" else price
```

## Bug Fix = Test First

When fixing a bug:

1. **RED** — write a test that exposes the bug
   ```typescript
   it('handles negative numbers in sum', () => {
     expect(sum([-1, -2, -3])).toBe(-6);
   });
   // Run: ✗ FAIL — got 0 instead of -6
   ```
2. **GREEN** — fix the bug
3. The bug is now fixed AND protected against regression

Never patch a bug without a failing test first. If you can't write a failing test, you don't understand the bug.

## Rationalisations to Reject

| Rationalisation | Why it's wrong |
|---|---|
| "I can manually test this quickly" | Manual testing doesn't prevent regression |
| "I'll write tests after to save time" | You'll skip edge cases and test implementation, not behaviour |
| "This is too simple to need a test" | Simple code changes; tests document expectations and catch regressions |
| "I've already written the code, I can't delete it now" | Sunk cost fallacy; delete it |
| "I know this works, I've done it before" | Your memory isn't documentation |
| "We're in a hurry" | Technical debt costs more than TDD |
| "This is just a prototype" | Prototypes ship. Write the test. |

## TDD Outside-In (London School)

For features that touch multiple layers:

1. Start with an E2E or integration test describing the behaviour from the outside
2. Let it drive the interfaces you need (controllers, services, repositories)
3. Drop down to unit tests for each new collaborator
4. Work your way back up as each unit passes

The outside-in test stays red until the whole stack is wired; unit tests turn red→green one at a time.

## TDD Inside-Out (Chicago / Classicist)

For algorithms and domain logic:

1. Start with a unit test on the innermost domain object
2. Build outwards, adding collaborators as the inner tests stabilise
3. Add an integration test at the end to confirm the assembly

## When TDD Is Hard (and what to do)

| Hard case | Cause | Response |
|---|---|---|
| Can't write a failing test | You don't understand the requirement | Stop. Write a spec. Ask the stakeholder. |
| Test is enormous | You're testing too much at once | Split the behaviour; write smaller tests |
| Test requires complex setup | Code has too many dependencies | Refactor for testability (dependency injection, pure functions) |
| Test is flaky | Hidden I/O, time, randomness, ordering | Extract the non-determinism and inject it |
| Test passes for the wrong reason | Assertion is too weak | Strengthen the assertion; add mutation testing |

## Verification Checklist

Before claiming any code is complete:

- [ ] Every production function has a corresponding test
- [ ] Each test was written before its implementation
- [ ] Each test was observed to fail first
- [ ] Tests verify behaviour, not implementation
- [ ] Refactoring kept all tests green
- [ ] No production code exists without a test

## Common Anti-Patterns

| Anti-pattern | Symptom | Fix |
|---|---|---|
| Test-after | "I'll add tests when it works" | Test-first or none — don't pretend |
| One huge test | "Tests everything" but breaks for any change | Split by behaviour; one assert per concept |
| Test-only methods | `_resetForTesting()` in production | Use fresh instances, dependency injection |
| Mock-first | Tests verify mocks, not behaviour | Test real output; mocks are secondary evidence |
| Green-by-accident | Test passes but doesn't exercise the code | Mutation test; if mutants survive, the test is weak |

---

*Adapted from [obra/superpowers](https://github.com/obra/superpowers) by Jesse Vincent (@obra), MIT License, and the test-master source skill.*
