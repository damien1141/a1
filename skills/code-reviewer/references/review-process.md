# Review Process & Checklist

## Two-Stage Review Architecture

```
                ┌─────────────────────┐
                │   Implementation    │
                └──────────┬──────────┘
                           │
                ┌──────────▼──────────┐
                │  STAGE 1: Spec      │
                │  Compliance Review  │
                └──────────┬──────────┘
                           │
            ┌──────────────┴──────────────┐
            │                              │
    ┌───────▼───────┐              ┌───────▼───────┐
    │   ✗ Issues    │              │   ✓ Compliant │
    │     Found     │              │               │
    └───────┬───────┘              └───────┬───────┘
            │                              │
            │                    ┌─────────▼─────────┐
            │                    │  STAGE 2: Code    │
            │                    │  Quality Review   │
            │                    └─────────┬─────────┘
            │                              │
            │                ┌─────────────┴─────────────┐
            │                │                            │
            │        ┌───────▼───────┐            ┌───────▼───────┐
            │        │   ✗ Issues    │            │   ✓ Approved  │
            │        └───────┬───────┘            └───────────────┘
            │                │
            └────────────────┴────────────────────┐
                                                │
                                      ┌─────────▼─────────┐
                                      │ Return to Author  │
                                      └───────────────────┘
```

**Critical:** Complete Stage 1 (spec compliance) BEFORE Stage 2 (code quality). Never review code quality for functionality that doesn't meet the specification. Code quality review is meaningless if the code doesn't implement the correct functionality.

- **Stage 1 (Spec):** Does it do the right thing?
- **Stage 2 (Quality):** Does it do the thing right?

## Stage 1: Spec Compliance Review

### Core Directive
> "The implementer finished suspiciously quickly. Their report may be incomplete, inaccurate, or optimistic."

Approach every review with professional skepticism. Verify claims independently.

### The Three Verification Categories

#### 1. Missing Requirements — features requested but not implemented
| Question | How to verify |
|---|---|
| Did they skip requested features? | Compare PR to original requirements line by line |
| Are edge cases handled? | Check error paths, empty states, boundaries |
| Were error scenarios addressed? | Look for try/catch, error boundaries, validation |
| Is the happy path complete? | Trace through primary use case manually |

```markdown
**Missing Requirement:** Issue #42 requested "password must be at least 12 characters"
**Found:** `validatePassword` only checks non-empty
**Status:** ❌ Incomplete
```

#### 2. Unnecessary Additions — scope creep and over-engineering
| Question | How to verify |
|---|---|
| Features beyond specification? | Compare to original requirements |
| Over-engineering? | Is complexity justified by requirements? |
| Premature optimisation? | Is performance cited without measurements? |
| Unrequested abstractions? | Helpers/utils for one-time use? |

```markdown
**Unnecessary Addition:** Added caching layer not in requirements
**Status:** ⚠️ Scope creep — discuss before merging
```

#### 3. Interpretation Gaps — misunderstandings of requirements
| Question | How to verify |
|---|---|
| Different understanding of requirements? | Ask author to explain their interpretation |
| Unclarified assumptions? | Look for comments like "assuming..." |
| Ambiguous specs resolved incorrectly? | Compare to similar existing features |

```markdown
**Interpretation Gap:** "Sort by date" implemented as ascending
**Expected:** Most recent first (typical UX pattern)
**Status:** ❓ Clarify — which sort order was intended?
```

### Spec compliance checklist
- [ ] Read the original issue/ticket completely
- [ ] Identify all explicit requirements
- [ ] Identify implicit requirements from context
- [ ] Note any acceptance criteria listed
- [ ] All required features present
- [ ] Edge cases covered (empty, null, max values)
- [ ] Error handling as specified
- [ ] Happy path fully functional
- [ ] UI matches mockups/specs if provided
- [ ] No unrequested features
- [ ] No speculative abstractions
- [ ] No premature optimisations
- [ ] Author's understanding matches spec
- [ ] Ambiguities resolved correctly
- [ ] Assumptions are documented and valid

## Stage 2: Code Quality Review

### Comprehensive checklist
| Category | Key questions |
|---|---|
| **Design** | Does it fit existing patterns? Right abstraction level? Could it be simpler? Extensible without modification? |
| **Logic** | Edge cases handled? Race conditions? Null checks? Order of operations correct? All code paths tested? |
| **Security** | Input validated? Auth checked? Authorisation enforced? Secrets safe? SQL parameterised? Output encoded? |
| **Performance** | N+1 queries? Memory leaks? Caching needed? Pagination? Blocking I/O on hot path? |
| **Tests** | Adequate coverage? Edge cases tested? Mocks appropriate? Tests assert behaviour, not implementation? |
| **Naming** | Clear, consistent, intention-revealing? |
| **Error handling** | Errors caught? Meaningful messages? Logged? No silent swallow? |
| **Documentation** | Public APIs documented? Complex logic explained? ADRs for non-obvious decisions? |
| **API design** | RESTful? Consistent naming? Versioned? Backwards compatible? Idempotent where appropriate? |

### Time-boxed review process

| Stage | Time | What you do |
|---|---|---|
| Context | 5 min | Read PR description, linked issues, expected changes |
| Structure | 10 min | File organisation, architectural fit, design patterns, breaking changes |
| Code details | 20 min | Logic, edge cases, error handling, security, performance, naming |
| Tests | 10 min | Coverage, quality, edge cases, mocks |
| Final pass | 5 min | Note positive patterns, prioritise feedback, write summary |
| **Total** | ~50 min | |

### Design questions
- Does this change belong in this file/module?
- Is the abstraction level appropriate?
- Could this be simpler?
- Does it follow existing patterns?
- Is it extensible without modification (OCP)?

### Logic questions
- What happens with null/undefined inputs?
- Are boundary conditions handled?
- Could there be race conditions?
- Is the order of operations correct?
- Are all code paths tested?

### Security questions
- Is all user input validated?
- Are SQL queries parameterised?
- Is output properly encoded?
- Are secrets handled safely (env vars, secret manager)?
- Is authentication checked? Authorisation enforced (horizontal + vertical)?

### Performance questions
- Are there N+1 query patterns?
- Is data fetched efficiently (batch, join, prefetch)?
- Are expensive operations cached?
- Could this cause memory leaks (unclosed resources, growing collections)?
- Is pagination implemented for list endpoints?

## Why Stage Order Matters

| Scenario | Waste from wrong order |
|---|---|
| Skip Stage 1 | Review 500 lines of code quality, then discover the wrong feature was built |
| Stage 2 first | Suggest refactoring, then realise the code shouldn't exist |
| Combined | Mix concerns, miss systematic issues |

## Disagreement Handling

If the author has left comments explaining a non-obvious choice, acknowledge their reasoning before suggesting an alternative. Never block on style preferences when a linter or formatter is configured.

If the author pushes back with technical reasoning, evaluate it against the codebase. Don't insist on a change that breaks existing functionality, violates YAGNI, or conflicts with established architecture. See `references/feedback-and-severity.md` for the receiving-feedback discipline.

## Common Mistakes to Avoid

| Mistake | Why it's wrong |
|---|---|
| Reviewing code style before spec compliance | Wasted effort if wrong thing was built |
| Assuming spec was followed | Verify independently |
| Skipping edge cases | Bugs hide in boundaries |
| Accepting "we can add it later" | Technical debt accumulates |
| Missing scope creep | Unreviewed code enters codebase |
| Reviewing without understanding the "why" | Surface feedback, not root feedback |
| Blocking on personal style preferences | When linters exist, defer to them |

## Quick Reference

| Stage | What it answers | Time |
|---|---|---|
| Stage 1 — Spec | Does it do the right thing? | 10 min |
| Stage 2 — Quality | Does it do the thing right? | 40 min |
| Tool runs | SAST / SCA / secrets | 5 min |
| Threat model (if needed) | STRIDE the new feature | 15 min |
| Report writing | Categorise findings | 10 min |
