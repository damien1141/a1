# The PROGRESS.md Pattern

Use persistent markdown files as working memory on disk. This is the discipline that prevents long tasks from drifting: every decision, completed step, error, and open question is written down where you can re-read it before deciding the next move.

## The core principle

Context windows degrade. After ~50 tool calls, original goals drift out of the attention window ("lost in the middle"). The fix is not a bigger context window; it is a file you re-read before every major decision, so the goals are always in the recent-attention zone.

```
Start of context: [Original goal — far away, forgotten]
... many tool calls ...
End of context:   [Recently read PROGRESS.md — gets ATTENTION!]
```

## The 3-file pattern

For any non-trivial task, create three files in the working directory:

| File | Purpose | When to update |
|------|---------|----------------|
| `PROGRESS.md` | Track goal, decisions, completed steps, next actions, open questions | After every phase; before every major decision |
| `notes.md` | Store findings, research, snippets, errors encountered | During research, as you learn |
| `[deliverable].md` | The final output | At completion |

Single-file is fine for small tasks (just `PROGRESS.md`). Two-file (`PROGRESS.md` + deliverable) is fine for medium tasks. Three-file is for anything that spans research + implementation + delivery.

## PROGRESS.md skeleton

```markdown
# PROGRESS: <brief description>

## Goal
<one sentence describing the end state — not the work, the outcome>

## Decisions
- <decision> — <rationale> (<date>)

## Completed Steps
- [x] <step> — <what was verified>

## Next Actions
- [ ] <next step, ordered, with owner if multi-person>

## Open Questions
- [ ] <question> — <who can answer / what blocks it>

## Errors Encountered
- <error> — <resolution or current status>
```

### Why each section

**Goal** — Single sentence. Outcome, not activity. "User can export their data" not "Build export feature." Re-read this before every decision; if a step doesn't serve the goal, drop it.

**Decisions** — Every non-trivial choice gets recorded with rationale. Future-you (or a teammate) will ask "why did we pick S3 over local disk?" — the answer is here, not in your memory.

**Completed Steps** — Checkbox list. Mark `[x]` and add a one-line note on what was verified. This is your momentum: seeing progress written down makes it harder to abandon.

**Next Actions** — Ordered checklist of what to do next. The top item is always the next concrete action. If you can't name it, you don't have a plan; re-plan.

**Open Questions** — Things you don't know yet that block decisions. Each question has an owner or a path to resolution. Unowned questions are technical debt.

**Errors Encountered** — Every error logged with what was tried and what worked. This is the most undervalued section. Future-you hitting the same error saves 30 minutes by reading this. Future agents reading the worklog learn what failed and why.

## The loop

```
1. Create PROGRESS.md with goal and phases
2. Loop:
   a. Read PROGRESS.md           ← refresh goals in attention window
   b. Take the top Next Action
   c. Execute (research, code, test)
   d. Update PROGRESS.md         ← mark [x], add new findings, update Next Actions
   e. If error: log to Errors Encountered, don't hide it
3. When all Next Actions done and Goal met: deliver, write final summary
```

### Read before decide

Before any major decision, re-read `PROGRESS.md`. This is the single highest-leverage habit. The file is your attention anchor; reading it pulls the goal back into the recent-attention zone.

```text
[Many tool calls have happened...]
[Context is getting long...]
[Original goal might be forgotten...]

Read PROGRESS.md          ← This brings goals back into attention!
Now make the decision      ← Goals are fresh in context
```

### Update after act

After completing any phase, immediately update the file:

- Mark completed steps with `[x]`
- Update the Status / Next Actions section
- Log any errors encountered (with what you tried)
- Add new open questions as they surface

### Store, don't stuff

Large outputs go to files, not context. Keep only paths in working memory.

```text
WRONG: Read a 5000-line file into context to "remember" it
RIGHT: Read the relevant 50 lines, save key findings to notes.md,
       cite the file:line range in PROGRESS.md
```

### Log all errors

Every error goes in `Errors Encountered`. This builds knowledge for future tasks and prevents silent retries.

```markdown
## Errors Encountered
- [2025-01-03] FileNotFoundError: config.json not found → Created default config
- [2025-01-03] API timeout calling Stripe → Retried with exponential backoff, succeeded
- [2025-01-04] TypeError: Cannot read property 'token' of undefined → Root cause:
  user object not awaited properly (see auth.ts:88); fixed with `await getUser()`
```

## Worked example — bug fix

```markdown
# PROGRESS: Fix login bug

## Goal
Users on Firefox cannot log in; clicking "Submit" does nothing. Restore login for all browsers.

## Decisions
- Bug is in the auth module (confirmed via Sentry stack trace) — not in the form library
- Reproduce locally before fixing (avoid "works on my machine" trap)

## Completed Steps
- [x] Reproduce locally (Firefox 121, macOS) — confirmed silent failure
- [x] Locate relevant code — auth.ts:142, validateToken() is the failure point
- [x] Identify root cause — `user` object is undefined; `getUser()` is async but not awaited

## Next Actions
- [ ] Add `await` to `getUser()` call at auth.ts:142
- [ ] Add regression test: login flow completes on async path
- [ ] Verify in Firefox locally
- [ ] Deploy to staging, verify on staging Firefox

## Open Questions
- [ ] Why did this only fail in Firefox? Chrome seemed to work — is there a polyfill difference? — needs @frontend-team
- [ ] Are there other un-awaited async calls in auth.ts? — grep for `getUser(` across the file

## Errors Encountered
- TypeError: Cannot read property 'token' of undefined → Root cause: getUser() not awaited at auth.ts:142
- Initial fix attempt (add null check) did not solve — symptom patch, root cause was upstream
```

## Worked example — feature development

```markdown
# PROGRESS: Dark mode toggle

## Goal
Users can toggle dark mode in settings; preference persists across sessions.

## Decisions
- Use CSS custom properties for theming (matches existing system in theme.ts) — 2025-01-04
- Store preference in localStorage (no server-side user prefs yet) — 2025-01-04
- Default to system preference (`prefers-color-scheme`) on first visit — 2025-01-05

## Completed Steps
- [x] Research existing theme system — src/styles/theme.ts uses CSS vars, light only
- [x] Design implementation — toggle component + useTheme hook + CSS var overrides
- [x] Build toggle component — SettingsPage.tsx
- [x] Implement useTheme hook — src/hooks/useTheme.ts

## Next Actions
- [ ] Wire toggle to useTheme in SettingsPage
- [ ] Add dark palette to theme.ts (background, surface, text, accent)
- [ ] Test in light → dark → light cycle
- [ ] Verify system-preference default on fresh load
- [ ] Accessibility audit: contrast ratios pass WCAG AA

## Open Questions
- [ ] Should we support "system" as a third option (light / dark / system)? — needs PM input
- [ ] Server-side rendering: how do we avoid FOUC on first paint? — needs @frontend-infra

## Errors Encountered
- Initial toggle flashed light → dark on reload → fixed by reading localStorage before React hydrates (inline script in index.html)
```

## Anti-patterns

| Anti-pattern | Symptom | Fix |
|--------------|---------|-----|
| Use TodoWrite/chat for persistence | Plan disappears when context rolls over | Write to `PROGRESS.md` file |
| State goals once and forget | Goal drift after 30 tool calls | Re-read before each major decision |
| Hide errors and retry silently | Same error retries 3x; no learning | Log every error to the file |
| Stuff large content in context | Context bloats; model loses track | Store in `notes.md`, cite path |
| Skip the file for "small" tasks | Small task grows into 15-step task with no plan | Create the file at first sign of complexity |
| Update at end only | Decisions made mid-task aren't recorded | Update after every phase, not just at end |
| Vague Next Actions | "Work on the feature" — what specifically? | Name the concrete next step (file, function, action) |

## When to skip the pattern

- Single-file edits with no logic changes
- Quick lookups ("what does function X do?")
- Conversational/explanatory responses
- Anything completable in <3 steps

If you're unsure, create the file. The cost of an unnecessary `PROGRESS.md` is 60 seconds; the cost of not having one when you need it is hours of drift and rework.

## Multi-agent / delegation integration

When delegating to subagents (see `agent-orchestration`), `PROGRESS.md` is the handoff artifact. The parent's `PROGRESS.md` references the child's task and expected output; the child's `PROGRESS.md` (in its own scope) tracks its own work. This lets the parent re-read only its own file without ingesting the child's full transcript.

```markdown
# PROGRESS (parent): Big feature

## Next Actions
- [ ] Wait for subagent: "audit auth flows" → expect findings in notes/auth-audit.md
- [ ] After audit: design new auth based on findings
```

The subagent writes `notes/auth-audit.md`; the parent reads it when ready. No transcript bloat in either direction.
