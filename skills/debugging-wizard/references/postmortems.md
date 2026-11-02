# Postmortems

A postmortem is the written artefact produced after an incident. Its purpose is **learning**, not blame. A good postmortem turns one team's bad day into every team's future good day.

## Blameless Postmortems

### Core principle
> "Postmortems are blameless. The goal is to understand how the system failed, not who to punish."

People operate the system they're given. If a person could push a button that broke production, the system allowed that button to exist without a safeguard. Fix the system, not the person.

### What blameless looks like
```markdown
✅ "The deploy pipeline lacked a canary stage, so the bad commit reached 100% of
   traffic in one roll."
❌ "Alice pushed the bad commit without checking the dashboard."
```

Both describe the same incident. The first suggests a fix (canary deploys). The second suggests firing Alice — and the next time something breaks, no one will report it honestly.

### Cultural rules
- Assume everyone did what made sense at the time, given the information they had
- Focus on the system: tooling, processes, safeguards, alerting
- Action items are about preventing recurrence, not punishing actors
- Postmortems are shared widely — the org learns together
- No postmortem is used in performance reviews (or the postmortem process dies)

## When to Write a Postmortem

| Trigger | Write a postmortem? |
|---|---|
| Customer-visible outage | Yes |
| Data loss / corruption | Yes |
| Security incident | Yes (separate security retro if needed) |
| SLO burn — error budget exhausted | Yes |
| Near-miss (caught before customer impact) | Yes — near-misses are cheap lessons |
| Paged outside business hours | Yes |
| Routine deploy that went fine | No |
| Bug caught in QA | No — file a bug |

When in doubt, write one. The cost is an hour; the value is preventing the next incident.

## Postmortem Template

```markdown
# Postmortem: <One-line summary>

**Date**: YYYY-MM-DD
**Authors**: <names>
**Status**: Draft | Review | Final
**Severity**: SEV1 (critical) | SEV2 (high) | SEV3 (medium)
**Impact**: <customers affected, duration, business impact>

## TL;DR
<2-3 sentences: what happened, who was affected, for how long>

## Impact
- Duration: HH:MM from first alert to resolution
- Users affected: N (~X% of traffic)
- SLO impact: Y hours of error budget consumed
- Business impact: <$Z>, <N lost orders>, etc.

## Timeline (all times UTC)
- 14:02 — Error rate spike on /api/orders; PagerDuty alerted on-call
- 14:05 — On-call acknowledged; began investigation
- 14:10 — Identified recent deploy as likely cause (commit abc123)
- 14:15 — Rolled back to previous release
- 14:18 — Error rate returned to baseline
- 14:30 — Incident declared resolved
- 14:45 — Postmortem owner assigned

## Root Cause
<The technical cause, stated precisely. "Database connection pool exhausted
because the new code path opened a connection per request without releasing it.
Under load, the pool filled and requests queued until they timed out.">

## Contributing Factors
- No canary stage in the deploy pipeline — bad commit reached 100% immediately
- The connection pool metric wasn't alertable — we couldn't see saturation
- Code review missed the missing `release()` because the pattern was new to the codebase
- Load test in CI used 10 RPS; production peak is 1000 RPS

## What went well
- Detection was fast (3 min from impact to alert)
- Rollback worked cleanly
- On-call had the runbook to hand

## What went badly
- The bug shipped to production at all
- We had no canary
- The connection pool had no alerting
- The deploy happened during peak traffic

## Where we got lucky
- The rollback worked (we've been burned before by non-reversible deploys)
- A user on Twitter flagged it before our alert fired (the alert was misconfigured)
- The bad commit was small — easy to spot in the diff

## Action Items
| # | Action | Owner | Ticket | Priority | Due |
|---|---|---|---|---|---|
| 1 | Add canary stage to deploy pipeline (5% → 25% → 100% over 30 min) | @platform | OPS-4521 | P0 | 1 week |
| 2 | Add connection pool saturation alert (warn at 70%, page at 85%) | @sre | OPS-4522 | P0 | 3 days |
| 3 | Add load test at 1000 RPS to CI | @qa | OPS-4523 | P1 | 2 weeks |
| 4 | Lint rule: flag `acquire()` without matching `release()` in same scope | @platform | OPS-4524 | P1 | 2 weeks |
| 5 | Move deploys out of peak traffic window (10:00–14:00 UTC) | @eng | OPS-4525 | P2 | 1 month |

## Appendix
- PagerDuty incident: <link>
- Deploy diff: <commit abc123>
- Monitoring dashboards during incident: <screenshot>
- Slack channel: #incident-2024-03-15
```

## 5 Whys

A technique for drilling past symptoms to root cause. Ask "why" repeatedly until you reach a system-level cause (not a person-level one).

```markdown
1. Why did the API return 500s?
   → The database connection pool was exhausted.

2. Why was the pool exhausted?
   → The new code path opened a connection per request without releasing it.

3. Why didn't the connection get released?
   → The new code used a different ORM helper that didn't auto-release.

4. Why wasn't that caught in review?
   → The reviewer wasn't familiar with the new ORM pattern.

5. Why was an unfamiliar pattern merged during peak traffic?
   → We have no deploy window restriction and no canary stage.

Root cause: deploy pipeline lacks a canary stage; unfamiliar patterns ship unguarded during peak.
```

Notice the chain ends at a **system** cause (no canary), not a person cause ("the reviewer should have caught it"). The system cause has an action item; the person cause has none.

### Anti-patterns
- **Stopping at "human error"** — that's where the interesting part starts
- **Stopping at "the deploy was bad"** — why did the deploy pipeline allow it?
- **Using 5 Whys to assign blame** — "Why did Alice push this?" leads nowhere

## SMART Action Items

Action items are the only part of a postmortem that prevents recurrence. They must be:
- **Specific** — not "improve monitoring" but "add connection pool saturation alert"
- **Measurable** — you can tell when it's done
- **Assignable** — one owner, named
- **Realistic** — actually achievable
- **Time-bound** — has a due date

### Anti-patterns
| Bad action item | Why it's bad | Better |
|---|---|---|
| "Be more careful with deploys" | Not specific, not measurable, not assignable | "Add canary stage to deploy pipeline (5/25/100%)" |
| "Improve monitoring" | Vague | "Add connection pool saturation alert (warn 70%, page 85%)" |
| "Engineering team to review" | No owner, no due date | "@platform to add lint rule by 2024-04-01 (OPS-4524)" |
| "Train developers on ORM" | Vague, hard to verify | "Add ORM connection lifecycle section to onboarding doc; track completion" |

## Tracking Action Items

A postmortem without tracked action items is theatre. The format doesn't matter (Jira, Linear, GitHub Issues) — what matters is:
- Each item has a ticket linked from the postmortem
- Each item has one owner
- Each item has a due date
- A recurring review (weekly or monthly) checks stale items
- Items past due get escalated

## Severity Levels (example scale)

| Level | Meaning | Response |
|---|---|---|
| **SEV1** | Critical — full outage, data loss, security breach | Page on-call + management; war room; postmortem required |
| **SEV2** | High — major feature broken, partial outage | Page on-call; postmortem required |
| **SEV3** | Medium — minor feature broken, workaround exists | Notify on-call during business hours; postmortem if recurring |
| **SEV4** | Low — cosmetic, edge case | File a bug; no postmortem |

## The Postmortem Meeting

Within 5 business days of the incident:
1. Owner presents the timeline and root cause (15 min)
2. Group asks clarifying questions (10 min)
3. Group pressure-tests the action items — are they SMART? Are they the right actions? (15 min)
4. Owner incorporates feedback; circulates the final version
5. Action items get tickets; PM/EM tracks to closure

Invite: on-call, responders, service owners, interested engineers. Open invitation — anyone in the org can attend.

## Common Anti-Patterns

| Anti-pattern | Problem | Fix |
|---|---|---|
| Blameful language ("Alice broke prod") | People hide the next incident | Rewrite in passive voice focusing on system |
| No action items | Recurrence is inevitable | Require ≥1 action item per contributing factor |
| Vague action items | Items never get done | SMART criteria |
| Action items untracked | Items rot | Tickets with owners + due dates |
| Postmortem never shared | Org doesn't learn | Post in #incidents, link in weekly digest |
| Postmortem used in perf reviews | People stop writing them | Cultural rule: never |
| Root cause = "human error" | Stops at the symptom | Keep asking why until you reach a system cause |
| "We'll be more careful next time" | Not an action | What will make us safer? That's the action |
| Recurring incidents with no pattern | Each postmortem is isolated | Tag and review quarterly; look for systemic issues |

## Postmortem Review Cadence

- **Weekly**: new postmortems reviewed in the engineering all-hands (5 min each)
- **Monthly**: action item review — anything overdue? Anything stalled?
- **Quarterly**: pattern review — are we seeing the same root cause across multiple incidents? What systemic change would prevent the class?
- **Annually**: are our SEV1s trending down? Are action item close rates improving?

## Quick Reference

| Need | Template / technique |
|---|---|
| Write the postmortem | Use the template above; fill every section |
| Find root cause | 5 Whys — stop when you reach a system cause |
| Make action items stick | SMART + ticket + owner + due date + weekly review |
| Decide whether to write one | Customer-visible impact, data loss, security, or near-miss → yes |
| Set severity | SEV1 (critical) → SEV4 (low) |
| Schedule the meeting | Within 5 business days; open invite |
| Track patterns | Quarterly review of all postmortems |
