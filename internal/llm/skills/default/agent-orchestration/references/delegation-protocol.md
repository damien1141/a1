# Delegation Protocol

How to split work between a parent agent (which reasons and integrates) and child agents (which execute scoped tasks). The discipline is in the contract: a subagent gets the goal, the blast radius, and the done criteria — not the parent's whole transcript.

## Core principle

A parent agent has limited context. Stuffing every subtask into that context degrades judgment, increases cost, and loses information to "lost in the middle" attention drift. Delegation trades one big context for many small focused ones, at the cost of integration overhead. The trade is worth it when the subtasks are independent and the integration is tractable.

## Keep vs delegate

### Keep in the parent

- Understanding the user's actual request and constraints (re-derive these from `PROGRESS.md`, not memory)
- Architecture, security, product, and release-risk decisions
- Cross-module integration and interface reconciliation
- Final review, test interpretation, and user-facing summary
- Anything where the cost of being wrong is high and the cost of doing it yourself is low

### Delegate to a subagent

- Read-only exploration over a bounded file set ("find all uses of deprecated API X")
- Mechanical edits with a clear file ownership boundary ("update these 3 config files to schema v2")
- Focused test or lint runs ("run the test suite for module X and return failures")
- Boilerplate generation from an explicit spec ("generate CRUD endpoints for the User model from this OpenAPI")
- Independent checks that can run while parent work continues ("audit auth flows while I write the API")

### Do not delegate

- Tiny one-step tasks (the prompt is longer than the work)
- Ambiguous product decisions (the subagent will guess, badly)
- Destructive operations without a clear acceptance criterion
- Final verification (the parent owns "is this done?")
- Cross-cutting refactors that touch many files at once (no clean blast radius)

## The delegation contract

Every delegation prompt contains four parts. Missing any part produces a subagent that optimizes for something other than what you wanted.

### 1. Goal — what done means

State the end state, not the activity. "Return a list of all places where `getUser()` is called without `await`, with file:line citations." Not "look for async bugs."

### 2. Blast radius — files owned, files off-limits

```text
Owns:
  - crates/tui/src/settings.rs
  - crates/tui/src/settings_test.rs

Must not touch:
  - crates/tui/src/config.rs (shared with other work)
  - crates/tui/src/main.rs (entry point, separate concern)
  - any file under docs/ (documentation is a separate task)
```

Without a blast radius, the subagent will edit whatever seems related, and you will spend the integration phase reverting collateral changes.

### 3. Output format — what to return

```text
Return:
  - List of changed file paths
  - Output of `cargo test test_api_key_change -- --nocapture`
  - One-paragraph summary of the approach taken (≤5 sentences)
```

Without an output format, you get whatever the subagent decided was interesting, which is rarely what you needed.

### 4. Done criteria — how to know it's finished

```text
Done when:
  - Test `test_api_key_change_does_not_restart_onboarding` exists and passes
  - No other files modified (verified via `git diff --name-only`)
  - Existing config key names preserved (verified via `git diff` — no renamed keys)
```

Without done criteria, the subagent either returns prematurely ("I think this is right") or wanders ("while I was in there I also refactored…").

## Strong vs weak prompts

### Weak (do not do this)

```text
Fix the settings bug.
```

Why weak: no file ownership, no done criteria, no off-limits, no output format. The subagent will guess what "the settings bug" refers to, will edit whatever seems plausible, and will return "I think I fixed it" with no evidence.

### Strong

```text
Own only crates/tui/src/settings.rs and its tests. Preserve existing config
key names (do not rename). Add a regression test showing that provider-specific
API key changes do not restart the DeepSeek onboarding flow.

Done when:
  - Test `test_api_key_change_does_not_restart_onboarding` passes
  - No other files modified (verify via `git diff --name-only`)
  - Existing config key names preserved (verify via `git diff`)

Return:
  - List of changed paths
  - Output of `cargo test test_api_key_change -- --nocapture`
  - ≤5-sentence summary of approach

Do not touch: crates/tui/src/config.rs, crates/tui/src/main.rs, docs/.
```

The strong prompt is longer than the weak one, but it eliminates the integration tax: the parent doesn't have to re-check what was changed, re-test what was tested, or revert unrelated edits. The prompt is the contract.

## Parallel delegation

Independent subagents can run in parallel. Launch them together; collect results when all are done.

```text
Parallel delegation example (independent audits):

  Subagent A: "Audit auth flows for missing rate limits. Return file:line for
               each unguarded endpoint. Read-only; do not edit."

  Subagent B: "Audit DB queries for N+1 patterns. Return file:line for each
               suspected N+1. Read-only; do not edit."

  Subagent C: "Audit error handling for swallowed exceptions (empty catch
               blocks). Return file:line for each. Read-only; do not edit."

Collect A, B, C → integrate into a single audit report in the parent.
```

Couple only when the subtasks actually depend on each other. A common mistake is to serialize independent audits "to be safe" — that just multiplies latency without improving quality.

## Verifying subagent self-reports

Subagent outputs are self-reports, not verified truth. The parent re-checks material claims before relying on them. "Material" means: a claim that, if wrong, would change the parent's next action.

| Subagent claim | Verification |
|-----------------|--------------|
| "Changed files: X, Y, Z" | `git diff --name-only` |
| "Tests pass" | Re-run the test command; cite the output |
| "No other files touched" | `git diff --name-only` against the expected set |
| "Function `foo` is called in 3 places" | `grep -rn 'foo('` |
| "This approach matches existing pattern" | Read the existing pattern and the new code side by side |
| "Performance is acceptable" | Ask for the benchmark, or run one |

The verification cost is paid by the parent because the parent owns the integration. A parent that trusts self-reports without verification is delegating its judgment along with its work.

## When delegation fails

| Failure mode | Symptom | Fix |
|--------------|---------|-----|
| Blast radius not respected | Subagent edits files outside its scope | Revert collateral; tighten the prompt; re-delegate or do it yourself |
| Done criteria ambiguous | Subagent returns "done" but didn't do the thing | Rewrite the done criteria as observable checks; re-delegate |
| Output format ignored | Subagent returns prose when you asked for a list | Re-request with the format explicit; if repeated, do it yourself |
| Subagent overreaches | "While I was in there I also…" | Revert the extras; add explicit "do not touch" list; re-delegate |
| Subagent stalls | Returns nothing or "I'm not sure" | The task is too ambiguous to delegate; do it yourself or re-spec |
| Integration drift | Each subagent's piece works alone; together they don't | Parent didn't define interface contracts; re-delegate with explicit contracts |

## The handoff pattern with PROGRESS.md

When delegating, the parent's `PROGRESS.md` references each subagent's task and the expected output location. The subagent writes its findings to a file (e.g., `notes/auth-audit.md`); the parent reads that file when ready. This avoids transcript bloat in both directions.

```markdown
# PROGRESS (parent)

## Next Actions
- [ ] Wait for subagent "audit-auth" → expect findings in notes/auth-audit.md
- [ ] After audit: design new auth based on findings
- [ ] Delegate "implement-auth-v2" with the design as input
```

The subagent's PROGRESS.md (in its own scope) tracks its own work. The parent never ingests the subagent's full transcript — only the deliverable file.

## Integration is the parent's job

The single most important rule: **integration is never delegated.** A subagent produces a piece. The parent produces the whole. End-to-end verification — does the integrated system actually work? — is the parent's responsibility, because the parent is the only one with the full picture.

A delegation workflow that ends with "the subagents said their pieces work" has not been verified. It has been asserted. The parent runs the end-to-end check; the parent owns the VERIFIED label; the parent signs the exit report.
