---
name: rigorous-coding
description: Apply epistemic rigor before, during, and after writing code. State assumptions explicitly, never claim unverified correctness, handle adversarial inputs (empty, missing, malformed, concurrent, large), run the 5-gate loop (scope → evidence → adversarial → verify → report), and exit honestly with VERIFIED vs ASSUMED labels. The keel skill.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: meta
  triggers: rigorous, verify, assumptions, adversarial, edge cases, honest exit, circuit breaker, root cause, anti-slop, code discipline, does this work, is this done, ship
  role: specialist
  scope: implementation
  output-format: code
  related-skills: spec-driven-development, agent-orchestration, debugging-wizard, testing-master, code-reviewer
---

# Rigorous Coding

The keel skill: the discipline that makes every other skill work. Before code, state what must be true. After code, prove what is true. Never blur the two. A bug is rarely a missing feature; it is almost always an unverified assumption presented as a fact.

## When to Use

- Writing or modifying any non-trivial code (≥1 branch or ≥1 external call)
- Reviewing, refactoring, or "fixing" a bug
- Answering "does this work?" or "is this done?"
- Shipping anything that will run in someone else's environment
- You feel the urge to say "this should work" — that sentence is the trigger

Skip for: one-line edits with no logic, throwaway scripts you will delete in 5 minutes, pure brainstorming where no claim is being made.

## The 5-Gate Cognitive Loop

Run this loop on every change. Skipping a gate is the most common source of bugs that "looked right."

1. **Scope** — What exactly am I changing and why? What is in/out of bounds? Write it down before touching code. Name the load-bearing assumption: "I am assuming X. If X is false, this whole approach is wrong."
2. **Evidence** — What evidence grounds this change? Read the actual code, the actual error, the actual spec. Cite `file:line`. If the evidence is "I think" or "usually", stop and gather real evidence.
3. **Adversarial** — Run the adversarial matrix against your change: **empty, missing, malformed, concurrent, large**. Also: what input would make this produce a wrong-but-plausible result? (See `references/adversarial-cases.md`.)
4. **Verify** — Run the actual verification: types compile, tests pass (existing + new for the change), lint clean, repro of the original failure now passes. "It compiles" is not verification. (See `references/verification-gates.md`.)
5. **Report** — Exit honestly. Label every claim as **VERIFIED** (you ran a check) or **ASSUMED** (you believe but did not check). Never blend them. (See `references/honest-exit.md`.)

## Reference Guide

| Topic | Reference | Load When |
|-------|-----------|-----------|
| The 5 gates in depth + per-language verification commands | `references/verification-gates.md` | Setting up verification, choosing what to run, building the gate checklist |
| Adversarial input matrix (empty / missing / malformed / concurrent / large) with worked examples | `references/adversarial-cases.md` | Designing edge-case tests, pre-commit self-review, hardening an external call |
| Honest exit format, VERIFIED vs ASSUMED labels, self-audit checklist | `references/honest-exit.md` | Writing the final report, deciding whether to ship |
| Circuit breakers, 2-strike rule, root-cause discipline, recovery from stuck debugging | `references/failure-recovery.md` | A fix did not work, debugging has stalled, you are tempted to retry the same approach |

## Constraints

### MUST DO

- **State assumptions before writing code.** Bulleted list: `Assumes: input is UTF-8; DB has index on user_id; caller holds the lock.` Name the load-bearing one explicitly so a reviewer can challenge it.
- **Cite evidence with `file:line`.** "The bug is in `auth.ts:142` because `user` is undefined when `req.headers.authorization` is missing" — not "the auth code looks wrong."
- **Handle the 5 adversarial cases** for any input or external call: empty, missing, malformed, concurrent, large. If a case cannot occur, prove it (e.g., "schema guarantees non-null") and write that proof as a comment near the access.
- **Verify by running, not by reading.** Run the type checker, the tests, a repro. Cite the command output, not your interpretation of it.
- **Fix root causes.** Ask "why" at least 3 times before patching. A patch that makes the symptom go away without explaining the cause is a future bug with a date.
- **Red-team your own work** for 60 seconds before declaring done. The author is the first attacker, not the last defender.
- **Exit with explicit VERIFIED / ASSUMED labels.** Distinguish what you checked from what you assumed.

### MUST NOT DO

- **No `as any`, `@ts-ignore`, `// eslint-disable`, empty `catch {}`, or `unwrap()`** to silence a type or error you do not understand. If you genuinely understand it and the suppression is correct, add a comment naming the constraint.
- **No shotgun debugging.** One hypothesis, one change, one verification. Two failed fixes from the same diagnosis → the diagnosis is wrong; re-diagnose. (See `references/failure-recovery.md`.)
- **No "this should work" / "I think this is right" / "probably fixed".** Either you verified it (say VERIFIED + the command) or you did not (say ASSUMED). There is no third category.
- **No happy-path-only code.** Every external call, every parse, every collection access gets the adversarial treatment.
- **No symptom patches presented as fixes.** If you patch a symptom, label it as a workaround and file the root cause. A silent symptom patch is a lie.
- **No claiming tests pass without running them** in the same environment as the change. "Tests should still pass" is ASSUMED, not VERIFIED.
- **Do not claim "done" while any gate is incomplete.** "Done except verification" is "not done."

## Code Examples

### Assumption block before code (Python)

```python
# ASSUMPTIONS:
# - `events` is non-empty and sorted by `ts` (caller contract, pipeline.py:88)
# - `window_ms` > 0 (validated at API layer, schema.py:14)
# - Single-threaded per shard (consumer.py:30, no shared mutable state)
# LOAD-BEARING: the sort assumption. If events can be unsorted, the
# sliding-window dedup below silently drops valid events.
def dedup_within_window(events: list[Event], window_ms: int) -> list[Event]:
    if not events:                       # adversarial: empty
        return []
    out = [events[0]]
    for ev in events[1:]:
        if ev.ts - out[-1].ts >= window_ms:
            out.append(ev)
    return out
```

### Adversarial matrix applied (TypeScript)

```typescript
// empty: null/undefined → null. missing: no email → null.
// malformed: not string / no '@' → null. concurrent: snapshot raw.
// large: > 1000 keys → throw (DoS guard).
function parseUser(raw: unknown): User | null {
  if (raw == null || typeof raw !== 'object') return null;
  const obj = raw as Record<string, unknown>;
  const email = obj.email;
  if (typeof email !== 'string' || !email.includes('@')) return null;
  if (Object.keys(obj).length > 1000)
    throw new Error(`parseUser: rejected ${Object.keys(obj).length} keys`);
  return { email };
}
```

### Honest exit block (final report)

```markdown
VERIFIED:
- `pytest tests/test_dedup.py -q` → 14 passed (Python 3.11, ran locally)
- `mypy src/dedup.py` → no issues
- Repro: `python repro.py` prints 3 events (was 0)
ASSUMED (please confirm):
- Prod shard count is 1:1 with consumer count (ops doc, not checked)
- Upstream `events` is always sorted (review comment, not tested vs prod data)
```

## Output Template

End every code-changing task with this block. If you cannot fill a field, that itself is information.

```markdown
## Rigorous-Coding Exit

**Scope:** <one line on what changed and why>
**Load-bearing assumption:** <the one assumption that, if false, invalidates the approach>

**VERIFIED:**
- <command> → <result>  (cite actual output, not interpretation)

**ASSUMED (not verified):**
- <claim> — <why not verified, who should confirm>

**Adversarial cases handled:** empty ✓ / missing ✓ / malformed ✓ / concurrent ✓ / large ✓
  (any ✗ → name the gap and the risk it creates)

**Root cause (if bug fix):** <3×why explanation, or "symptom patch — root cause still open: …">

**Circuit breakers tripped:** <0 / 1 / 2 — if 2, describe the re-diagnosis that followed>
```

## Knowledge Reference

- **Epistemic hygiene.** Separate belief from evidence; never let "should" substitute for "did." "Should work" is a confession that you have not checked.
- **Adversarial thinking** from security (red team) and testing (property-based, fuzzing). The author is the first attacker.
- **Circuit breakers** from SRE: a system that keeps failing the same way needs the diagnosis changed, not more retries.
- **Root-cause discipline** (5 Whys, Toyota Production System): fix the process that produced the bug, not just the bug.
- **This skill is the keel.** It is what makes `spec-driven-development`, `agent-orchestration`, `debugging-wizard`, `testing-master`, and every language skill actually trustworthy. Load it whenever you are about to claim something is done.
