# Honest Exit

The exit report is the contract between you and the next reader (reviewer, oncall, future-you, downstream agent). Its job is to make the difference between "I checked" and "I believe" impossible to miss.

## The two labels

| Label | Meaning | Required evidence |
|-------|---------|-------------------|
| **VERIFIED** | I ran a check and observed the result. | The command and the actual output (or a faithful summary that the reader could reproduce). |
| **ASSUMED** | I believe this is true but did not check it. | The reason it was not checked, and who should check it. |

There is no third label. "Probably" is ASSUMED. "Should be" is ASSUMED. "I'm pretty sure" is ASSUMED. "Tests pass" without a command is ASSUMED. The labels are binary because the difference between a checked claim and an unchecked one is the difference between knowledge and belief.

## Why this matters

Most production incidents have a sentence in the postmortem like: "We assumed the upstream service would reject malformed input." That assumption was presented as a fact during code review, during testing, during deployment. The honest-exit discipline exists to make that sentence impossible to write — because the assumption would have been labeled ASSUMED in the exit report, and a reviewer would have asked the obvious question.

The cost of labeling is 30 seconds per claim. The cost of not labeling is incidents.

## The exit block

```markdown
## Rigorous-Coding Exit

**Scope:** <one line on what changed and why>
**Load-bearing assumption:** <the one assumption that, if false, invalidates the approach>

**VERIFIED:**
- <command> → <result>  (cite actual output)
- <command> → <result>

**ASSUMED (not verified):**
- <claim> — <why not verified, who should confirm>

**Adversarial cases handled:** empty ✓ / missing ✓ / malformed ✓ / concurrent ✓ / large ✓
  (any ✗ → name the gap and the risk it creates)

**Root cause (if bug fix):** <3×why explanation, or "symptom patch — root cause still open: …">

**Circuit breakers tripped:** <0 / 1 / 2 — if 2, describe the re-diagnosis>
```

### Worked example — good exit

```markdown
## Rigorous-Coding Exit

**Scope:** Add retry-with-exponential-backoff to `fetchUser` in `auth.ts` to absorb sporadic 503s from the identity provider during deploys.
**Load-bearing assumption:** The 503s originate from the upstream IdP, not from our own gateway retrying. If false, retries amplify load and make the outage worse.

**VERIFIED:**
- `pnpm test src/auth.test.ts` → 18 passed (includes new `retries on 503` test)
- `tsc --noEmit` → no type errors
- `pnpm tsx repro_503.ts` → succeeds after 2 retries (was: throws on first 503)
- Manual: `kubectl logs deploy/auth -f` during a deploy → saw "retrying after 503" then success

**ASSUMED (not verified):**
- 503s are from the IdP not our gateway — needs confirmation from ops; checked IdP dashboard trends but not gateway logs for the same window.
- Backoff ceiling of 30s is acceptable for the login flow — needs PM sign-off; not blocking but should be confirmed.

**Adversarial cases handled:** empty ✓ (no headers → no retry) / missing ✓ (no auth header → no retry, original 401 path) / malformed ✓ (non-503 error → no retry, throw) / concurrent ✓ (each call has own retry state, no shared mutable) / large ✓ (max 3 retries, no unbounded loop)

**Root cause:** Sporadic 503s during IdP deploys. 3×why: (1) 503 returned → (2) IdP returns 503 during rolling restart → (3) IdP has no graceful drain. Fix is client-side retry because we don't control the IdP. Root cause on IdP side filed as TICKET-4521.

**Circuit breakers tripped:** 0
```

### Worked example — bad exit (do not do this)

```markdown
## Done

Fixed the auth bug. Added retry logic. Tests should still pass. The 503s should go away now.
```

Why it's bad:

- "Fixed" — VERIFIED or ASSUMED? Can't tell. No command cited.
- "Tests should still pass" — ASSUMED presented as fact.
- "The 503s should go away" — prediction presented as accomplishment.
- No scope, no load-bearing assumption, no adversarial cases, no root cause.

A reviewer reading the bad exit has no way to know what to double-check. A reviewer reading the good exit knows exactly what to verify (the two ASSUMED items) and what is already checked.

## Self-audit before writing the exit

Run this checklist against your own work. Every ✗ becomes either a fix or an ASSUMED entry.

```text
[ ] Did I run the type checker? Cite the command.
[ ] Did I run the test suite? Cite the command and the count.
[ ] Did I write a test for the new behavior? Cite the test name.
[ ] Did I reproduce the original failure and confirm it's gone? Cite the repro.
[ ] Did I run lint? Cite the command.
[ ] Did I check the diff for debug prints / TODOs / commented code? Cite the grep.
[ ] Did I run the adversarial matrix? Cite the 5 decisions.
[ ] Did I state the root cause with at least 3 whys? (Or label as symptom patch.)
[ ] Did I name the load-bearing assumption?
[ ] If I touched config or env, did I document the new keys / vars?
```

Each unchecked box is a claim you are about to make without evidence. Either run the check (and move the result to VERIFIED) or label it ASSUMED with a reason.

## Common exit smells

| Smell | What it sounds like | What it really means | Fix |
|-------|---------------------|----------------------|-----|
| Optimism verb | "should work", "ought to pass", "probably fixed" | ASSUMED, undeclared | Run the check or label ASSUMED |
| Credentialing | "tests pass" without command | Either not run or run somewhere irrelevant | Cite the actual command |
| Scope inflation | "While I was in there I also fixed…" | Gate 1 (scope) was violated | Revert the unrelated changes or split the PR |
| Hidden symptom patch | "Fixed the crash by catching the exception" | Root cause not addressed | Label as workaround, file root cause ticket |
| Future tense as past | "Will reduce latency by 50%" | Prediction, not measurement | Either measure or remove the claim |
| Hedge stack | "I think this should mostly handle the edge cases" | Three layers of uncertainty hiding "I didn't test" | Convert each layer to a specific check |
| Verified-by-vibes | "The code looks correct" | Reading is not running | Run something |

## Honest exit under pressure

Under deadline pressure, the temptation is to skip the labels and ship. The discipline inverts: the pressure is exactly when labels matter most, because the cost of a wrong "done" is borne by someone else (oncall, customer, teammate). Two pressure-resistant practices:

1. **Lower the bar for ASSUMED, not for VERIFIED.** Under pressure you may have more ASSUMED items — that's honest. What you may not have is VERIFIED items that are actually ASSUMED.
2. **Name the next checker.** Every ASSUMED item gets a name: who will verify it, and when. "ASSUMED: works on Windows — needs @teammate to run the build before merge." An ASSUMED without an owner is a deferred incident.

## Relationship to the keel

The honest exit is the final gate of the 5-gate loop in `verification-gates.md`. It is the moment where you convert the work of the previous four gates into a communication that a reader can act on. A great scope, great evidence, great adversarial pass, and great verification, all hidden behind a sloppy exit, is a great job presented as a mediocre one. The exit is the work's public face; make it as rigorous as the work it summarizes.
