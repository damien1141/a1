# Red-Team Critique

Structured critical reasoning to stress-test ideas, plans, and decisions before they are committed. Five modes, each with a method and a deliverable. The discipline: steelman first, attack second, synthesize third.

## Core principle

The court jester was the one person who could speak truth to the king. Not naive, but strategically unbound by convention, hierarchy, or politeness. Red-team critique applies the same freedom to your own plans: argue the strongest opposing position, find the failure modes, audit the evidence — not to be contrarian, but to find weaknesses before adversaries (real ones, or production, or time) do.

The work is structured in three movements:

1. **Steelman** — restate the position in its strongest form. If you can't steelman it, you don't understand it well enough to critique it.
2. **Attack** — apply the chosen mode's method. Produce 3-5 strong challenges, not 10 weak ones.
3. **Synthesize** — integrate the challenges into a stronger position. End with synthesis or a named genuine trade-off. Never end with just a pile of objections.

## The 5 modes

| Mode | Method | Output |
|------|--------|--------|
| **Expose My Assumptions** | Socratic questioning | Assumption inventory + probing questions + experiments |
| **Argue the Other Side** | Hegelian dialectic + steel manning | Steelmanned thesis + antithesis + synthesis + confidence |
| **Find the Failure Modes** | Pre-mortem + second-order thinking | Ranked failure narratives + warning signs + mitigations |
| **Attack This** | Red teaming | Adversary profiles + attack vectors + perverse incentives |
| **Test the Evidence** | Falsificationism + evidence weighting | Claims + falsification criteria + evidence grades |

## Mode selection

Pick by signal, not by preference.

| User signal | Recommended mode |
|-------------|------------------|
| "Is this the right approach?" | Socratic (assumptions not yet examined) |
| "I'm about to commit to X" | Dialectic (need strongest counter before committing) |
| "What could go wrong?" | Pre-mortem (explicitly asking about failure) |
| "Is this secure/safe?" | Red team (adversarial framing) |
| "The data shows that…" | Evidence audit (claims need falsification) |
| "Everyone agrees that…" | Socratic (consensus signals unexamined assumptions) |
| "This will definitely work" | Pre-mortem (overconfidence needs failure imagination) |
| "Studies show…" | Evidence audit (cited evidence needs quality assessment) |

By decision type: technology choice → Dialectic+Pre-mortem; architecture → Pre-mortem+Red team; business strategy → Dialectic+Evidence audit; security → Red team+Pre-mortem; data-driven → Evidence audit+Socratic; trade-off → Dialectic+Socratic.

## Mode 1 — Socratic Questioning

Probe what's being taken for granted. Don't argue; ask. Every question should create a moment of "I hadn't thought about that."

### Question categories

| Category | Pattern | Example |
|----------|---------|---------|
| Definitional | "When you say X, what specifically do you mean?" | "When you say 'scalable,' do you mean 10x or 1000x?" |
| Evidential | "What evidence supports this?" | "What data shows users want this feature?" |
| Logical | "Does X necessarily lead to Y?" | "Does adding caching necessarily improve UX?" |
| Perspective-shifting | "How would [stakeholder] see this?" | "How would the on-call engineer feel about this?" |
| Consequential | "What happens next?" | "After we migrate, what's the first thing that breaks?" |

### Assumption detection signals

| Signal phrase | Hidden assumption |
|---------------|-------------------|
| "Obviously…" | The speaker hasn't questioned this |
| "Everyone knows…" | Consensus hasn't been verified |
| "It just makes sense…" | The reasoning chain hasn't been articulated |
| "We always…" | Historical pattern assumed to be optimal |
| "There's no other way…" | Alternatives haven't been explored |
| "It's simple…" | Complexity has been underestimated |
| "Users want…" | User research may be absent or stale |

### Output template (abbreviated)

```markdown
## Assumption Inventory
| # | Assumption | Type | Confidence |

## Probing Questions
### [Theme 1]
1. [Question targeting assumption #X]

## Suggested Experiments
| Assumption | Experiment | Effort | Signal |
```

## Mode 2 — Dialectic Synthesis

Construct the strongest counter-argument and drive toward synthesis.

### Process

1. Restate the thesis — steelman the user's position first
2. Construct the antithesis — build the strongest opposing argument
3. Present the clash — show where thesis and antithesis genuinely conflict
4. Drive toward synthesis — propose a position incorporating the best of both

### Steel manning checklist

- [ ] Made the position stronger, not weaker?
- [ ] Would the user recognize this as their view (or better)?
- [ ] Included the strongest evidence for their side?
- [ ] Attacking this version, not an easier one?

### Synthesis patterns

- **Conditional** — X true when A, Y true when B (e.g., microservices for payments; monolith for admin)
- **Scope partitioning** — apply X to domain A, Y to domain B (event sourcing for audit; CRUD for users)
- **Temporal** — start with X, migrate to Y when trigger Z (monolith now; extract when team > 3 squads)
- **Risk mitigation** — proceed with X but add safeguards from Y (adopt framework, keep abstraction for swap-back)
- **Hybrid extraction** — take the strongest element from each (microservices deploy + shared DB)

### Confidence rating

| Level | Meaning | Action |
|-------|---------|--------|
| HIGH | Synthesis clearly stronger than either side | Proceed with synthesis |
| MEDIUM | Plausible but untested | Identify riskiest assumption; suggest experiment |
| LOW | Both sides have irreconcilable claims | Name the genuine trade-off; let user decide by priorities |
| PIVOT | Antithesis stronger than thesis | Recommend reconsidering original position |

## Mode 3 — Pre-Mortem Analysis

Invert the question: "It's 6 months from now and this has failed. Why?" This bypasses optimism bias by making failure the starting point.

### Process

1. Set the scene: "Imagine it's [timeframe] from now. This plan has failed."
2. Generate specific failure narratives (not "it didn't scale" — name trigger, chain, consequence)
3. Rank by likelihood × impact
4. Trace consequence chains at least 2 orders deep
5. Identify early warning signs
6. Design mitigations (concrete actions, not "be careful")

### Failure narrative specificity checklist

- [ ] Names a specific trigger (not "something goes wrong")
- [ ] Includes a number or threshold
- [ ] Describes the chain of events, not just the end state
- [ ] Could actually happen (not a fantasy scenario)

### Second-order consequence chains

```
Trigger: Key engineer leaves during migration
  → 1st order: Migration timeline slips 4 weeks
    → 2nd order: Overlap with legacy system extends, doubling operational cost
      → 3rd order: Budget overrun triggers executive review, project descoped
```

### Inversion check

Ask: "What would guarantee this fails?" Then check if any of those conditions exist now.

### Output template (abbreviated)

```markdown
## Pre-Mortem: [Plan Name]
**Timeframe:** [When failure would be evident]
### Failure Narratives
#### 1. [Title] — Likelihood: H/M/L | Impact: H/M/L
[Specific narrative: trigger → chain → consequence → root cause]
**Consequence chain:** 1st → 2nd → 3rd order
### Early Warning Signs
| Signal | Predicts | Check frequency |
### Mitigations
| Failure | Mitigation | Effort | Reduces risk by |
### Inversion Check
**What guarantees failure:** [Top 3 conditions]
**Do any exist now?** [Yes/No with specifics]
```

## Mode 4 — Red Team Adversarial

Adopt the mindset of an adversary. Not just security — competitors, disgruntled users, perverse incentives, regulators all count.

**Process:** (1) identify the asset; (2) construct specific adversary personas; (3) map attack vectors; (4) rank by likelihood × impact; (5) design defenses.

### Adversary personas

| Persona | Motivation | Typical vectors |
|---------|-----------|-----------------|
| External attacker | Financial gain, data theft | API exploitation, credential stuffing, injection |
| Competitor | Market advantage | Feature copying, FUD, talent poaching |
| Disgruntled insider | Revenge, financial gain | Privilege escalation, data exfiltration |
| Careless user | None (accidental) | Misconfiguration, weak passwords |
| Regulator | Compliance enforcement | Audit findings, data handling violations |
| Opportunistic gamer | Personal benefit | Business-logic loopholes, referral fraud |

### Perverse incentive questions

- "How will people game this?" — loopholes in business logic
- "What behavior does this reward that we don't want?" — misaligned incentives
- "What's the cheapest way to get the reward without the effort?" — shortcut exploitation
- "If we measure X, what Y gets sacrificed?" — Goodhart's Law
- "Who benefits from this failing?" — adversaries with motive

## Mode 5 — Evidence Audit

Audit whether claims are actually supported by evidence. Karl Popper: a claim is only meaningful if you can specify what would disprove it.

### Process

1. Extract claims (often implicit) from the proposal
2. Design falsification criteria for each claim
3. Assess evidence quality (A-F grading)
4. Check for cognitive biases
5. Surface competing explanations

### Claim types (often hidden)

- **Causal** — "X causes Y" (hidden in: "our refactor improved performance")
- **Predictive** — "X will happen" ("users will adopt this feature")
- **Comparative** — "X is better than Y" ("React is the better choice for us")
- **Existential** — "X exists/doesn't" ("there's no alternative that meets our needs")
- **Universal** — "X is always true" ("microservices always improve velocity")
- **Quantitative** — "X is N" ("this will save 200 hours per quarter")

### Evidence grading

| Grade | Description |
|-------|-------------|
| A | Controlled experiment, large sample, reproducible |
| B | Observational data, reasonable sample, consistent with other evidence |
| C | Case study, small sample, single source — needs corroboration |
| D | Anecdote, opinion, vendor marketing — do not base decisions alone on this |
| F | No evidence cited — claim is unsupported |

### Unfalsifiable claims (red flag)

- Vague outcome: "This will improve things" — no measurable criterion
- Moving goalposts: "It'll work eventually" — no time boundary
- Circular reasoning: "Best because experts recommend" — evidence is claim restated
- Unfalsifiable hedge: "Might help in some cases" — true by definition

## Multi-mode sequencing

Some situations benefit from running 2 modes in sequence: **Socratic → Dialectic** (untested idea: surface assumptions, then argue counter); **Pre-mortem → Red team** (high-stakes launch: internal failures then external attacks); **Evidence audit → Socratic** (data-driven proposal: audit evidence, then question interpretation); **Dialectic → Pre-mortem** (strategic decision: argue counter, then stress-test survivor).

## Anti-patterns

- **Strawman** — attacking a weak version; fix: steelman first, attack the strong version
- **Objection stacking** — 10 weak objections to look thorough; fix: 3-5 strong challenges
- **Nihilism** — leaving only objections, no synthesis; fix: always end with synthesis or named trade-off
- **Generic skepticism** — "what if it doesn't work?"; fix: "what if at 50K users the DB pool exhausts?"
- **Generic attack vectors** — "hackers might attack"; fix: specific persona + specific vector + likelihood × impact
- **Skipping steelman** — going straight to attack; fix: steelman first; if you can't, you don't understand it
- **No confidence rating** — "I think the synthesis is good"; fix: rate HIGH/MEDIUM/LOW/PIVOT; if MEDIUM, name the test

## Synthesis is mandatory

A critique that ends with objections is nihilism. Every red-team pass ends with either:

- A **synthesis**: a strengthened position integrating the challenges, with a confidence rating
- A **named genuine trade-off**: "X is faster but less safe; Y is safer but slower. The choice depends on [explicit priority]."

Never end with "so there are problems." Always end with "so here is the stronger position, or here is the trade-off that must be decided."
