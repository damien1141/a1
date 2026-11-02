# Test Pyramid & Layer Selection

## The Pyramid

```
              ▲
             / \
            / E2E\          ← few, slow, high-value (critical user journeys)
           /─────\
          /  Integ \
         /─────────\
        /   Unit    \      ← many, fast, pin logic & edge cases
       /─────────────\
```

| Layer | Share | Speed | Cost to maintain | What it pins |
|---|---|---|---|---|
| **Unit** | ~70% | <100ms each | Low | Pure logic, edge cases, error paths |
| **Integration** | ~20% | 100ms–2s | Medium | Module boundaries, DB, real I/O against test doubles |
| **E2E** | ~10% | 2–30s | High | Critical user journeys across the whole system |

The pyramid is a default, not a law. Inverted pyramids (lots of E2E, few unit tests) are slow, flaky, and expensive. Hourglass shapes (lots of unit + lots of E2E, no integration) miss boundary bugs.

## When Each Layer Pays Off

### Write a unit test when
- The function has branches, edge cases, or math
- You can test it without I/O (or by mocking the boundary)
- It runs in <100ms
- It will catch regressions during refactoring

### Write an integration test when
- Two real modules must agree (e.g. repository + database, controller + service + real validators)
- A bug would only surface with real I/O (e.g. transaction rollback, foreign key constraint)
- The unit test would have to mock so much it tests nothing
- You're verifying a third-party SDK is used correctly

### Write an E2E test when
- The flow crosses the entire stack (browser → API → DB → response)
- It protects a revenue-critical or security-critical user journey (login, checkout, signup, password reset)
- A regression here would mean a customer-visible outage

### Do NOT write an E2E test when
- A unit or integration test could catch the same bug
- The flow is rarely used or has low business value
- The UI is changing rapidly (the test will be expensive to maintain)
- You're testing third-party code you can't fix anyway

## Layer Selection Decision Table

| Question | If yes → |
|---|---|
| Does the bug live in one pure function? | Unit |
| Does it require a real DB / filesystem / message queue? | Integration |
| Does it require a real browser clicking through the UI? | E2E |
| Does it require two services agreeing on a payload? | Contract test (Pact) |
| Does it need to verify a property holds for all inputs in a class? | Property-based (hypothesis / fast-check) |
| Is the test going to be flaky by nature (timing, animations)? | Refactor production code first; do NOT paper over with retries |

## Test Sizes (Google's Framework)

| Size | Allowed | Time bound |
|---|---|---|
| **Small** (unit) | Single process, no I/O, no blocking, no network | ≤1 min, usually <100ms |
| **Medium** (integration) | One process, local I/O (test DB, test filesystem), localhost network | ≤5 min |
| **Large** (E2E) | Anything goes: cross-process, real browser, real network | ≤15 min |

Mark tests with their size so CI can run small tests on every push and large tests nightly.

## Anti-Pyramid Smells

| Smell | What it means | Fix |
|---|---|---|
| Ice-cream cone (lots of E2E, few unit) | E2E suite is slow and flaky | Push logic down into pure functions; unit test those; keep only critical journeys as E2E |
| Hourglass (lots of unit + E2E, no integration) | Boundary bugs slip through | Add integration tests at the repository / adapter layer |
| Cupcake (lots of unit, no integration, no E2E) | Looks great in coverage, ships broken flows | Add E2E for the top 3–5 critical journeys |
| Coverage-driven (100% line coverage, no edge cases) | Tests pass but bugs ship | Add property-based tests, mutation testing |

## Critical Journey Priorities (P0–P3)

| Priority | Coverage | Example |
|---|---|---|
| **P0** | Mandatory E2E | Registration, login, core feature, password reset |
| **P1** | E2E on critical releases | Payment, checkout, settings, common flows |
| **P2** | Integration + occasional E2E | Edge cases, admin features |
| **P3** | Unit only | Rare scenarios, internal tools |

## Pyramid in Microservices

In a microservice architecture, each service owns its own pyramid:

- **Unit** — service logic, domain rules
- **Integration** — service + its own DB + its own downstream clients (mocked)
- **Contract** (Pact) — agreements with consumers and providers; replaces heavy cross-service E2E
- **E2E** — only for the top-level user journey across services; kept in a separate repo

Cross-service E2E tests are the most expensive and flaky tests in a system. Prefer contract tests at the API boundary.

## Quick Reference

| Layer | When | Speed | Share |
|---|---|---|---|
| Unit | Logic, edge cases | <100ms | ~70% |
| Integration | Boundaries, real I/O | 100ms–2s | ~20% |
| E2E | Critical user journeys | 2–30s | ~10% |
| Contract | Service-to-service payload agreements | <1s | per boundary |
| Property | Invariants over input classes | varies | per invariant |

## Common Mistakes

| Mistake | Fix |
|---|---|
| Testing implementation details (private methods, mock call counts alone) | Test observable behaviour through the public API |
| Mocking everything | Mock only trust boundaries; run real code between them |
| Treating coverage as a goal | Coverage is a lower bound, not a quality signal. Add mutation testing to find weak assertions |
| One giant E2E test per feature | Split into small, focused E2E tests with shared fixtures |
| Sharing state between E2E tests | Reset state in `beforeEach` via API; never rely on test order |
