---
name: agent-orchestration
description: Orchestrate multi-step work via three disciplines: delegate to focused subagents (parent reasons + integrates; child executes a scoped task with goal + blast-radius + done-criteria, not a transcript), enforce complete output (no placeholders, no truncation, mandatory complete-file drops), and red-team your own plans before commit (steelman then attack, name the load-bearing assumption).
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: meta
  triggers: delegate, subagent, orchestrate, multi-agent, full output, no truncation, no placeholders, red team, devil's advocate, pre-mortem, steelman, challenge, stress test, critique, failure modes
  role: specialist
  scope: implementation
  output-format: code
  related-skills: rigorous-coding, spec-driven-development, system-architecture, debugging-wizard
---

# Agent Orchestration

Three disciplines that make multi-agent and multi-step work actually work: **delegation** (split the work so each context stays focused), **output enforcement** (refuse to ship partial output), and **red-team critique** (attack your own plan before someone else does). Together they are the difference between a multi-agent workflow that produces verified, complete, stress-tested output and one that produces confident slop.

## When to Use

- A task is too large for one context window, or has parallelizable independent subtasks → **delegate**
- A task requires complete, unabridged output (full files, all components, no "rest omitted") → **output enforcement**
- A plan, architecture, or decision is about to be committed and the cost of being wrong is high → **red-team critique**
- You're orchestrating multiple agents and need to integrate their results → all three

Skip for: single-step tasks, trivial edits, conversations with no claim being made.

## Operating Loop

1. **Decompose** — Break the work into independent units. For each unit, decide: keep in parent (architectural judgment, integration, final verification) or delegate (bounded exploration, mechanical edits, focused checks, boilerplate from spec). See `references/delegation-protocol.md`.
2. **Delegate** — Launch each delegated unit as a focused subagent with: the goal (what done means), the blast radius (files it may touch, files it must not touch), and the done criteria (what to return). Do not hand off the transcript; hand off the contract.
3. **Enforce output** — Every artifact a subagent returns must be complete: no `// ... existing code ...`, no "rest of code omitted", no "implement here" placeholders. If output approaches the token limit, pause cleanly and resume — do not compress or skip. See `references/output-enforcement.md`.
4. **Verify subagent self-reports** — Subagent outputs are self-reports, not verified truth. Re-check material claims: read changed files, run tests, inspect diffs before relying on them.
5. **Integrate** — Combine subagent outputs in the parent. Reconcile interfaces, run end-to-end verification, fix integration drift. The parent owns the integration; the children own their pieces.
6. **Red-team before commit** — Before declaring done, run a structured critique of the plan and the result: steelman the approach, then attack it. Name the load-bearing assumption. Enumerate failure modes. See `references/red-team-critique.md`.
7. **Exit honestly** — Use the `rigorous-coding` exit labels: VERIFIED (you ran a check) vs ASSUMED (you believe but did not check). Multi-agent work amplifies assumptions; label them ruthlessly.

## Reference Guide

| Topic | Reference | Load When |
|-------|-----------|-----------|
| Delegation protocol: keep vs delegate, prompt shape, blast radius, done criteria, verifying self-reports | `references/delegation-protocol.md` | Splitting work, writing a subagent prompt, integrating subagent results |
| Output enforcement: banned patterns, long-output handling, complete-file drops, the pause-and-resume protocol | `references/output-enforcement.md` | Generating code or docs that must be production-complete, no truncation |
| Red-team critique: 5 modes (Socratic, dialectic, pre-mortem, red team, evidence audit), steelman-then-attack, failure-mode enumeration | `references/red-team-critique.md` | Stress-testing a plan, decision, or design before commit |
| Multi-agent patterns: parent-child, fan-out/fan-in, pipeline, critic, swarm topology selection | `references/multi-agent-patterns.md` | Designing a multi-agent workflow, choosing a topology, integrating parallel results |

## Constraints

### MUST DO

- **Hand off a contract, not a transcript.** A subagent prompt contains: the goal, the blast radius (files owned, files off-limits), the expected output format, and the done criteria. It does not contain the parent's whole conversation.
- **Define done criteria concretely.** "Return the changed paths and the test command output." Not "fix the bug." A subagent without done criteria will optimize for something you didn't ask for.
- **Verify subagent self-reports.** Read the changed files. Run the tests. Inspect the diff. A subagent claiming "tests pass" without a command is ASSUMED, not VERIFIED.
- **Enforce complete output.** No `// ... existing code ...`, no "rest omitted", no "implement here". If you asked for a full file, deliver the full file. If output is too long, pause cleanly and resume — see `references/output-enforcement.md`.
- **Steelman before attacking.** When red-teaming, restate the position in its strongest form first. A weak steelman signals a weak critique. Name the load-bearing assumption explicitly.
- **Enumerate failure modes concretely.** "It might not scale" is not a failure mode. "At 50K concurrent users the DB connection pool exhausts, causing cascading timeouts" is. See `references/red-team-critique.md`.
- **Reconcile integration in the parent.** Subagents produce pieces; the parent produces the whole. End-to-end verification is the parent's job, never delegated.

### MUST NOT DO

- **Do not delegate the integration.** The parent owns the integration. Delegating integration produces a Frankenstein of pieces that don't fit.
- **Do not delegate tiny one-step tasks.** If the task is smaller than the prompt you'd write to delegate it, do it yourself.
- **Do not delegate ambiguous product decisions or destructive operations.** Those need human judgment or explicit acceptance criteria the child can verify against.
- **Do not produce banned output patterns** — `// ...`, `// rest of code`, `// TODO`, `// similar to above`, `// implement here`, bare `...` standing in for omitted code, "for brevity", "the rest follows the same pattern", "I'll leave that as an exercise".
- **Do not compress remaining sections to fit a token limit.** Write at full quality to a clean breakpoint, then pause and resume. Quality is not negotiable for length.
- **Do not strawman the position you're critiquing.** If your critique only works against a weak version of the position, you haven't critiqued the position.
- **Do not stack minor objections** to create a false impression of weakness. Three real objections beats ten padding ones.
- **Do not skip the synthesis.** A critique that leaves only objections is nihilism; end with a strengthened position or a named genuine trade-off.
- **Do not present a subagent's self-report as your own verification.** Either you ran the check (VERIFIED) or you didn't (ASSUMED).

## Code Examples

### Strong delegation prompt

```text
Own only crates/tui/src/settings.rs and its tests. Preserve existing config
key names (do not rename). Add a regression test showing that provider-specific
API key changes do not restart the DeepSeek onboarding flow.

Done criteria:
- Test named `test_api_key_change_does_not_restart_onboarding` passes
- No other files modified
- Return: list of changed paths + `cargo test test_api_key_change` output

Do not touch: crates/tui/src/config.rs, crates/tui/src/main.rs, any docs.
```

### Weak delegation prompt (do not do this)

```text
Fix the settings bug.
```

Why weak: no file ownership, no done criteria, no off-limits, no output format. The subagent will guess, and you'll spend longer reconciling the guess than the fix would have taken.

### Output enforcement — banned vs acceptable

```text
BANNED (hard failure):
  // ... existing code ...
  // rest of code omitted
  // implement here
  // TODO: add error handling
  /* ... */
  // similar to above
  "for brevity, I'll skip..."
  "the rest follows the same pattern"
  bare "..." standing in for code

ACCEPTABLE:
  - The complete file, every line, runnable as-is
  - Or a clean breakpoint: "[PAUSED — 2 of 5 components complete.
    Send 'continue' to resume from: UserService]"
```

### Red-team — steelman then attack

```markdown
## Thesis (steelmanned)
Adopt microservices: independent deployment and scaling of components will
accelerate team velocity, especially given 4 teams working on different
release cycles. This eliminates the current deploy-queue bottleneck.

## Load-bearing assumption
The teams will actually deploy independently once the architecture allows it.
If coordination is required for business reasons (shared customer-facing
releases), the velocity benefit doesn't materialize.

## Strongest attack (3-5 points)
1. The deploy-queue bottleneck is a process problem, not an architecture
   problem. Microservices without a CI/CD upgrade will reproduce the queue
   per-service.
2. Data consistency across services is harder than in a monolith. The order
   flow spans 3 services; distributed transactions will eat the velocity gain.
3. Team size (4 teams, ~20 engineers) is below the typical microservices
   threshold (often cited ~50). Operational overhead may exceed coordination
   savings.

## Synthesis
Extract the 2 services with genuine independent-scaling needs (payments,
notifications). Keep the rest as a modular monolith. Revisit when team > 40
or deploy frequency hits weekly conflicts.
```

## Output Template

End every orchestrated task with this block.

```markdown
## Agent-Orchestration Exit

**Decomposition:** <units, kept vs delegated, why>

**Subagents launched:**
- <name> — <scope> — <done criteria> — <VERIFIED | ASSUMED result>

**Output enforcement:** no banned patterns ✓ / pause-and-resume used <N> times

**Integration verification:**
- <end-to-end check> → <result>

**Red-team pass:** <mode used> — <load-bearing assumption named> — <synthesis or genuine trade-off>

**Rigorous-coding labels:**
- VERIFIED: <commands run, outputs cited>
- ASSUMED: <claims not checked, who should confirm>
```

## Knowledge Reference

- **Delegation** from the `delegate` skill: parent reasons + integrates, child executes a scoped task. The contract is goal + blast-radius + done-criteria, not a transcript.
- **Output enforcement** from the `full-output-enforcement` skill: partial output is broken output. Banned patterns are hard failures. Long output pauses cleanly and resumes; it never compresses.
- **Red-team critique** from `the-fool`: 5 modes (Socratic, dialectic, pre-mortem, red team, evidence audit). Steelman first, attack second, synthesize third. The author is the first attacker.
- **Multi-agent topologies** — parent-child, fan-out/fan-in, pipeline, critic, swarm — chosen by coupling and verification needs, not by fashion.
- **This skill pairs with `rigorous-coding`** (the labels) and `spec-driven-development` (the PROGRESS.md handoff pattern). Multi-agent work without these two is just confident slop at scale.
