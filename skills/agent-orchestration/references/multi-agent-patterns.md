# Multi-Agent Patterns

Topologies for multi-agent workflows. The right pattern depends on coupling (how much do the subtasks depend on each other?) and verification needs (how easy is it to check each subagent's work?).

## Core principle

A multi-agent workflow is a coordination problem, not a model problem. The hard parts are: decomposing the work, defining the contracts between agents, integrating the results, and verifying that the integrated whole actually works. Pick the topology that minimizes coordination overhead for the work at hand. Don't pick a topology because it sounds sophisticated; pick it because the work fits it.

## The 5 topologies

### 1. Parent-Child (single delegate)

One parent delegates one scoped task to one child, verifies the result, integrates.

```
Parent ──delegates──▶ Child
       ◀──returns────
       (parent verifies + integrates)
```

**When:** Single well-scoped task with a clean blast radius. The default; reach for the others only when this doesn't fit.

**Example:** "Audit auth flows for missing rate limits. Return file:line for each unguarded endpoint. Read-only."

**Cost:** Low. One delegation, one verification, one integration.

**Failure mode:** Child overreaches (edits files outside scope) or underdelivers (returns prose instead of file:line). Fixed by the delegation contract — see `delegation-protocol.md`.

### 2. Fan-out / Fan-in (parallel delegates)

Parent launches N independent children in parallel, collects all results, integrates.

```
        ┌─▶ Child A ─┐
Parent ──┼─▶ Child B ─┼──▶ Parent (integrate)
        └─▶ Child C ─┘
```

**When:** N independent subtasks. The subtasks don't depend on each other; the integration is tractable.

**Example:** "Audit auth, audit DB queries for N+1, audit error handling for empty catches" — three independent read-only audits, integrated into a single report.

**Cost:** Latency = max(child durations), not sum. Coordination overhead is N prompts + 1 integration.

**Failure mode:** Children produce inconsistent output formats. Parent can't integrate without reformatting. Fix: specify the output format in every child prompt.

**When NOT to use:** Subtasks are sequential (B needs A's output). Use pipeline instead.

### 3. Pipeline (sequential stages)

Work flows through stages; each stage's output is the next stage's input.

```
Parent → Stage 1 → Stage 2 → Stage 3 → Parent (verify)
```

**When:** Subtasks are sequential by nature. Stage N genuinely cannot start until stage N-1 produces its output.

**Example:** "Mine spec from code → write feature spec → implement spec → write tests for spec." Each stage needs the previous stage's output.

**Cost:** Latency = sum(stage durations). No parallelism benefit; the value is in clean contracts between stages.

**Failure mode:** Each stage drifts from the original intent. By stage 4, you're solving a different problem than stage 1 started. Fix: re-read the original `PROGRESS.md` goal at each stage boundary; reconcile drift before continuing.

**When NOT to use:** The stages are actually independent. Use fan-out instead — pipeline serializes unnecessarily.

### 4. Critic (generator + critic)

One agent produces; another critiques; the producer revises.

```
Producer ──draft──▶ Critic ──feedback──▶ Producer ──revised──▶ ...
```

**When:** The work has a verifiable quality dimension that the producer can't self-assess well. Common for: code review, security review, writing quality, design critique.

**Example:** Producer writes the auth module; Critic reviews for security holes (red-team mode); Producer revises; repeat until Critic finds no critical issues.

**Cost:** 2x-3x single-agent cost. Worth it when the cost of shipping a flaw exceeds the cost of the critic pass.

**Failure mode:** Critic becomes a rubber stamp ("looks good") or adversarial for its own sake. Fix: critic uses a structured mode (see `red-team-critique.md`); producer can challenge critic's claims; parent resolves disputes.

**When NOT to use:** The work is mechanical (no judgment to critique) or trivially verifiable (run the tests).

### 5. Swarm (peer agents with shared state)

Multiple agents work concurrently on related subtasks, sharing a common workspace (file system, shared `PROGRESS.md`).

```
   ┌─ Agent A ─┐
   ├─ Agent B ─┤  (shared workspace: PROGRESS.md, notes/, src/)
   └─ Agent C ─┘
```

**When:** Large task with high internal parallelism but shared state. Common for: large refactors, multi-file features, "fix all instances of X across the codebase."

**Example:** Three agents each tackle a different module of a refactor; they share a `PROGRESS.md` that tracks which files are claimed, which are done, and what the interface contracts are.

**Cost:** Coordination overhead is highest. Concurrency hazards: two agents edit the same file; one agent's change breaks another's assumption.

**Failure mode:** Merge conflicts, interface drift, "I thought you were doing that." Fix: file-level ownership (no two agents touch the same file); explicit interface contracts in `PROGRESS.md`; parent arbitrates disputes.

**When NOT to use:** Almost always. Swarm is the highest-overhead topology and is rarely the best choice. Reach for fan-out or pipeline first.

## Topology selection

| Situation | Topology |
|-----------|----------|
| One well-scoped task | Parent-child |
| N independent tasks | Fan-out / fan-in |
| Sequential stages, each needs the previous | Pipeline |
| Quality dimension the producer can't self-assess | Critic |
| Large shared-state task with strong file-level isolation | Swarm (last resort) |

Decision shortcut: start with parent-child. If you have N independent tasks, fan-out. If they're sequential, pipeline. Only escalate to critic or swarm when the simpler topologies genuinely don't fit.

## Contracts between agents

Every agent-to-agent handoff needs a contract. The contract has three parts:

### 1. Interface — what the upstream produces

```text
Stage 1 produces: specs/{feature}.spec.md
  - Functional requirements in EARS format
  - Acceptance criteria in Given/When/Then
  - Non-functional requirements (performance, security, scalability)
  - Error handling matrix
```

### 2. Pre-conditions — what must be true before downstream starts

```text
Stage 2 (implementation) pre-conditions:
  - spec file exists at specs/{feature}.spec.md
  - spec has ≥1 functional requirement per user story
  - spec has ≥1 acceptance criterion per functional requirement
  - spec has explicit NFRs (not "should be fast")
```

### 3. Post-conditions — what downstream verifies before accepting

```text
Stage 2 verifies from Stage 1:
  - Every FR has a corresponding test in the implementation
  - Every AC has a test that would fail before and pass after
  - NFRs have measurable acceptance (e.g., p95 < 200ms test exists)
```

Without contracts, each agent optimizes locally and the integration produces a Frankenstein.

## Integration patterns

### Pattern A: Single integrator

The parent collects all subagent outputs and integrates them itself. No subagent sees another subagent's output. Simplest; lowest coordination overhead; works when the integration is mechanical.

### Pattern B: Contracted handoff

Upstream agent writes to a known location with a known format; downstream agent reads from that location. The contract is the format. Works when subagents need to chain (pipeline) or share state (swarm).

### Pattern C: Review-and-merge

Multiple agents produce alternative solutions to the same problem; a reviewer (parent or critic) picks one or merges. Works when the problem is ambiguous and you want options. High cost; reserve for genuinely ambiguous decisions.

## The PROGRESS.md handoff

In all topologies, `PROGRESS.md` (see `spec-driven-development/references/progress-md-pattern.md`) is the cross-agent coordination artifact. Each agent:

- Reads the parent's `PROGRESS.md` to understand the goal and its scope
- Maintains its own `PROGRESS.md` for its subtask (in its own working area)
- Writes its deliverable to the location the contract specifies
- Updates the parent's `PROGRESS.md` Next Actions when done

This avoids transcript bloat — no agent needs to ingest another agent's full conversation. Only the deliverable file crosses the boundary.

## Common anti-patterns

| Anti-pattern | Symptom | Fix |
|--------------|---------|-----|
| Over-delegation | Delegating a one-step task | Do it yourself; the prompt is longer than the work |
| Under-delegation | Parent tries to do everything in one context | Decompose; delegate the bounded pieces |
| No contracts | Subagents produce incompatible outputs | Define interface, pre-conditions, post-conditions before delegating |
| Trusting self-reports | "Subagent said tests pass" | Re-run the tests; cite the output |
| Serialized parallel work | Independent tasks run one after another "to be safe" | Launch in parallel; integrate at the end |
| Premature swarm | Reaching for swarm when fan-out would do | Start simpler; escalate only when forced |
| Critic rubber-stamp | Critic says "looks good" without finding issues | Use structured critique mode; require ≥3 specific challenges |
| Pipeline drift | Stage 4 solves a different problem than stage 1 | Re-read original goal at each stage boundary |
| Integration abdication | "The subagents will integrate themselves" | Parent always owns integration; never delegate it |

## Verification at the topology level

Each topology has a characteristic verification pattern:

| Topology | Verification |
|----------|--------------|
| Parent-child | Parent re-runs the child's claimed check; reads the changed files |
| Fan-out | Parent integrates; runs end-to-end check across all subtask outputs |
| Pipeline | Each stage verifies its input (pre-conditions) before starting; parent runs end-to-end at the end |
| Critic | Critic's structured pass is the verification; producer's revision is confirmed by critic |
| Swarm | Parent runs end-to-end; checks for interface drift between agents' outputs |

End-to-end verification is always the parent's job. The topology determines *what* the parent checks, not *whether* the parent checks.

## When to abandon a topology

If a topology isn't working — subagents keep producing incompatible outputs, integration keeps failing, the critic keeps finding the same class of issue — the topology is wrong for the work. Stop, re-decompose, and try a simpler topology. The most common escalation path is: swarm → fan-out (with stronger contracts) → parent-child (do it yourself).

A topology that produces more integration overhead than the work itself is not a topology; it's a tax. Cut it.
