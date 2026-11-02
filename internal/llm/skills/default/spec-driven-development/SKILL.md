---
name: spec-driven-development
description: Drive development from explicit specs, not vibes. Reverse-engineer specs from existing code, run structured requirements workshops (problem → user stories → EARS requirements → acceptance criteria → NFRs → edge cases), maintain a persistent PROGRESS.md planning file as working memory, and generate + validate technical docs (docstrings, OpenAPI, ADRs, READMEs).
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: meta
  triggers: spec, specification, requirements, EARS, reverse engineer, legacy code, undocumented, feature workshop, planning file, PROGRESS.md, ADR, docstring, OpenAPI, documentation, onboarding, acceptance criteria, user stories
  role: specialist
  scope: implementation
  output-format: code
  related-skills: rigorous-coding, agent-orchestration, system-architecture, code-reviewer
---

# Spec-Driven Development

Code without a spec is improvisation. Specs without code are fiction. This skill closes the loop: mine specs from existing code, write specs for new features, plan with persistent files, and produce documentation that is itself verified. The discipline is to never let "what we built" and "what we meant" drift apart silently.

## When to Use

- Inheriting or onboarding to an undocumented codebase (spec-mining)
- Defining a new feature or change before coding (feature workshop)
- Any multi-step task where you will lose the thread without a written plan (PROGRESS.md)
- Producing or updating API docs, READMEs, ADRs, or docstrings (documentation patterns)
- Preparing for a review where someone will ask "why does this exist?"

Skip for: one-line fixes, throwaway exploration, pure refactors whose intent is already documented upstream.

## Operating Loop

1. **Discover** — Is there an existing spec? If yes, read it. If no, decide: mine one from code (path A), or run a workshop to write one (path B). Most real work is a mix: mine the surrounding context, then workshop the new behavior.
2. **Plan** — Create `PROGRESS.md` in the working directory before touching code. Sections: Goal, Decisions, Completed Steps, Next Actions, Open Questions. Re-read before each major decision; update after each completed step. This is working memory on disk, not a journal.
3. **Specify** — Write functional requirements in EARS format (`When <trigger>, the <system> shall <response>`), acceptance criteria in Given/When/Then, and explicit non-functional requirements (performance, security, scalability). Distinguish observed behavior from inferred intent.
4. **Document** — Generate the documentation that matches the artifact: docstrings for functions, OpenAPI for HTTP APIs, ADRs for decisions with non-obvious trade-offs, README sections for onboarding. Validate every doc: `tsc --noEmit` for JSDoc examples, `python -m doctest` for Python, `redocly lint` for OpenAPI.
5. **Validate** — Reconcile spec ↔ code ↔ docs. Any drift is a bug in one of the three. Update the loser to match the winner; do not let them diverge silently.

## Reference Guide

| Topic | Reference | Load When |
|-------|-----------|-----------|
| Spec mining: reverse-engineer specs from existing/legacy code, EARS observations, Glob/Grep patterns | `references/spec-mining.md` | Onboarding, undocumented codebase, "figure out how this works" |
| Feature workshops: problem statement → user stories → EARS requirements → acceptance criteria → NFRs → edge cases | `references/feature-workshops.md` | Defining a new feature, gathering requirements, writing a spec doc |
| PROGRESS.md planning pattern: file-as-working-memory, read-before-decide, store-don't-stuff, error logging | `references/progress-md-pattern.md` | Starting any multi-step task, losing the thread, planning complex work |
| Documentation patterns: docstrings (Google/NumPy/Sphinx), JSDoc, OpenAPI 3.1, READMEs, ADRs, doc site generators | `references/documentation-patterns.md` | Writing or validating docs, choosing docstring style, building API docs |
| ADR template + worked example + when-to-write-one decision table | `references/adr-template.md` | Recording a non-obvious decision, justifying a trade-off, leaving a trail for future maintainers |

## Constraints

### MUST DO

- **Read before writing.** If a spec might already exist (in code, in `/docs`, in a ticket), find it before authoring a competing one. Cite the source.
- **Use EARS for functional requirements.** `When <trigger>, the <system> shall <response>.` Every requirement has a verifiable trigger and response.
- **Write acceptance criteria in Given/When/Then.** Each criterion must be testable in isolation. INVEST: Independent, Negotiable, Valuable, Estimable, Small, Testable.
- **Distinguish OBSERVED from INFERRED.** When mining, label every statement: "OBSERVED (code at file:line)" vs "INFERRED (likely intent, unverified)".
- **Create `PROGRESS.md` first for multi-step work.** Goal, Decisions, Completed Steps, Next Actions, Open Questions. Re-read before each major decision.
- **Validate documentation mechanically.** Run `doctest`, `tsc --noEmit`, `redocly lint`. Untested docs become lies.
- **Record non-obvious decisions as ADRs.** If a future maintainer would ask "why?", write an ADR. If the answer is "because that's the default," skip it.
- **Reconcile spec ↔ code ↔ docs at the end.** Any drift gets resolved, not papered over.

### MUST NOT DO

- **Do not invent behavior the code doesn't exhibit** when mining a spec. If you can't find it, mark it INFERRED or UNCERTAIN — never present an inference as an observation.
- **Do not skip the workshop** for a new feature. "I know what we need" produces specs that miss edge cases, security, and NFRs.
- **Do not accept vague requirements.** "Make it fast" → "p95 response < 200ms at 1000 RPS." "User-friendly" → specific UX criterion.
- **Do not write untestable acceptance criteria.** If you can't write a test that fails before and passes after, it isn't a criterion.
- **Do not stuff everything in context.** Use `PROGRESS.md` as external memory; keep only paths in your head.
- **Do not skip non-functional requirements.** Performance, security, scalability, observability. A spec with only functional requirements is half a spec.
- **Do not write docs that lie.** If the code changed and the docs didn't, fix the docs in the same PR.
- **Do not skip the reconciliation step.** Spec, code, and docs that disagree are three bugs pretending to be a project.

## Code Examples

### EARS-format functional requirement

```markdown
**FR-AUTH-001**: Login
While credentials are valid, when POST /auth/login is called,
the system shall return a JWT access token (15min TTL) and refresh token (7d TTL).

**FR-AUTH-002**: Invalid Login
When invalid credentials are provided,
the system shall return 401 and increment the failed-login counter.

**FR-AUTH-003**: Account Lockout
While failed-login count exceeds 5, when login is attempted,
the system shall reject the attempt and require password reset.
```

### Acceptance criterion (Given/When/Then)

```markdown
### AC-001: Successful Login
Given a registered user with valid credentials
When they submit the login form
Then they are redirected to the dashboard within 2 seconds
And a session is created
And a success toast is displayed

### AC-002: Account Lockout
Given a user with 5 failed login attempts
When they submit a 6th attempt with any credentials
Then they receive a "Account locked, reset password" message
And no failed-login counter is incremented
And a password-reset email is queued
```

### PROGRESS.md skeleton

```markdown
# PROGRESS: <feature name>

## Goal
<one sentence describing the end state>

## Decisions
- <decision> — <rationale, date>

## Completed Steps
- [x] <step> — <what was verified>

## Next Actions
- [ ] <next step, with owner if multi-person>

## Open Questions
- [ ] <question> — <who can answer>

## Errors Encountered
- <error> — <resolution or status>
```

Read this file before any major decision. Update it after every completed step. This is the "re-read" loop that prevents context drift across long tasks.

### ADR skeleton

```markdown
# ADR-001: <Decision title>

## Status
Accepted | Proposed | Superseded by ADR-XXX

## Context
<Why are we deciding? What constraints? What problem?>

## Decision
<What did we choose?>

## Consequences
### Positive
- <benefit>
### Negative
- <drawback>
### Neutral
- <side effect>

## Alternatives Considered
<What else was evaluated and why rejected?>
```

See `references/adr-template.md` for a full worked example.

## Output Template

The deliverable depends on the mode. Use the matching template:

```markdown
## Spec-Driven Deliverable

**Mode:** spec-mining | feature-workshop | planning | documentation

**Inputs read:**
- <file/ticket/spec> — <what you extracted from it>

**Produced:**
- <artifact path> — <what it contains>

**Verification:**
- <command> → <result>  (doctest, tsc, redocly lint, test run)

**Reconciliation:**
- spec ↔ code: <aligned | drift at <file:line> — fixed>
- code ↔ docs: <aligned | drift at <file:line> — fixed>

**Open Questions (carry-forward):**
- <question> — <owner>
```

## Knowledge Reference

- **EARS** (Easy Approach to Requirements Syntax, Alistair Mavin) — unambiguous trigger/response phrasing for functional requirements.
- **Given/When/Then** (BDD, Dan North) — testable acceptance criteria in scenario form.
- **INVEST** (Bill Wake) — qualities of good user stories: Independent, Negotiable, Valuable, Estimable, Small, Testable.
- **PROGRESS.md / file-as-working-memory** — context-engineering pattern from Manus: keep goals in the attention window by re-reading the plan file before each decision.
- **ADR** (Architecture Decision Records, Michael Nygard) — lightweight, versioned decision log that survives team turnover.
- **Docs-as-code** — treat documentation as a build artifact with validation gates, not a separate deliverable.
- **This skill pairs with `rigorous-coding`:** the spec is what you verify against; without a spec, "verified" has no referent.
