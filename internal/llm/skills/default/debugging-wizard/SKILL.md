---
name: debugging-wizard
description: "Use when investigating errors, parsing stack traces, finding root causes of unexpected behavior, troubleshooting crashes, or running log analysis. Applies systematic debugging (reproduce → isolate → hypothesize → test → fix → verify → prevent), reads stack traces, runs git bisect and binary search, uses time-travel and interactive debuggers (pdb/ipdb, delve, gdb/lldb, Chrome DevTools, React DevTools), reads profilers, classifies bugs into taxonomies (off-by-one, null deref, race, memory leak, infinite loop, cache invalidation), and writes blameless postmortems."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: quality
  triggers: "debug,error,bug,exception,traceback,stack trace,troubleshoot,crash,root cause,git bisect,binary search,time-travel debugging,pdb,ipdb,delve,gdb,lldb,chrome devtools,react devtools,profiler,postmortem,race condition,memory leak,off-by-one,cache invalidation"
  role: specialist
  scope: analysis
  output-format: analysis
  related-skills: "testing-master,code-reviewer,sre-reliability"
---

# Debugging Wizard

Systematic debugger. Reproduce reliably, isolate to the smallest failing case, form written hypotheses, test one variable at a time, fix the root cause, verify with a regression test, and prevent recurrence. Per-language tooling (pdb/ipdb, delve, gdb/lldb, Chrome DevTools, React DevTools), profiler-driven analysis, and blameless postmortem discipline.

## When to Use

- Investigating an error, crash, or unexpected behaviour
- Parsing a stack trace you don't fully understand
- Hunting a regression (something worked last week, broken now) — `git bisect`
- Diagnosing a flaky test, race condition, or memory leak
- Reading a profiler flame graph to find a perf bottleneck
- Writing a postmortem after an incident

## Operating Loop

1. **Reproduce** — Establish consistent reproduction steps. Document them. If you can't reproduce, gather more info (logs, environment, user actions) before going further — never debug what you can't see.
2. **Isolate** — Narrow down to the smallest failing case. Strip dependencies, simplify inputs, comment out half the pipeline (binary search).
3. **Hypothesise** — Form a specific, **written** hypothesis: "X is happening because Y. If I change Z, the bug will disappear." Predict the outcome before testing.
4. **Test** — Change ONE variable. Run. Record the result. If the hypothesis is disproved, return to step 3 with a new hypothesis. Do not stack changes.
5. **Fix** — Implement the smallest fix that addresses the root cause, not the symptom. Write a failing regression test first; the fix makes it pass.
6. **Verify** — Run the full test suite (not just the changed file). Manually verify the original repro now works. Confirm no new breakage.
7. **Prevent** — Add a regression test, a guard, a type annotation, a lint rule, or an alert that would have caught this bug earlier. Write a postmortem if the bug was customer-impacting.
8. **Exit** — Report: root cause, evidence, fix, prevention. Separate VERIFIED (ran, saw) from ASSUMED (inferred, didn't run). If you hit 3 failed fix attempts in different locations, STOP — that signals an architectural problem, not a bug. See `references/systematic-method.md`.

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Systematic method & 4 phases | `references/systematic-method.md` | complex bugs, multiple failed fixes, root-cause discipline, three-fix threshold |
| Strategies (binary search, git bisect, time-travel, minimal repro) | `references/strategies.md` | regression hunting, isolating unknown bug location, narrowing inputs |
| Bug taxonomies (off-by-one, null deref, race, leak, infinite loop, cache) | `references/bug-taxonomies.md` | recognising bug patterns from symptoms |
| Tools by language (pdb/ipdb, delve, gdb/lldb, Chrome DevTools, React DevTools) | `references/tools-by-language.md` | picking the right debugger for the language |
| Observability & profiling | `references/observability-and-profiling.md` | logging strategies, profiler use, traces, metrics |
| Postmortems | `references/postmortems.md` | blameless RCA, 5 whys, postmortem template, action items |

## Constraints

### MUST DO
- Reproduce the issue first; never debug what you can't see
- Gather complete error messages and stack traces (full stack, not just the top frame)
- Test ONE hypothesis at a time; change ONE variable per experiment
- Form a written hypothesis with a predicted outcome before testing
- Read error messages thoroughly — don't just read the first line
- Add a regression test after fixing
- Remove all debug code (print statements, breakpoints, log spam) before committing
- Document findings for future reference (postmortem for customer-impacting bugs)
- Use a debugger (pdb/delve/gdb/DevTools) instead of print statements where possible
- Stop and question architecture after 3 failed fix attempts

### MUST NOT DO
- Guess without testing
- Make multiple changes at once (you won't know which one worked)
- Skip reproduction steps
- Assume you know the cause before tracing data flow
- Debug in production without safeguards (rollback plan, monitoring, change window)
- Leave `console.log` / `print` / `debugger` / `breakpoint()` statements in committed code
- Fix the symptom instead of the root cause
- Disable a failing test to make the suite green
- Add a "fix" without a regression test
- Continue past 3 failed fix attempts without stepping back

## Code Examples

### Stack trace reading
```
TypeError: Cannot read properties of undefined (reading 'map')
    at UserList.render (UserList.tsx:24)        ← where it crashed
    at renderWithHooks (react-dom.js:14985)     ← caller
    at mountIndeterminateComponent (react-dom.js:17811)
```
Key questions: What exact operation failed? Where (file, line)? What was the call stack? Are there multiple errors or one?

### pdb — interactive Python debugging
```bash
python -m pdb script.py            # launch from start
python -m pdb -c continue script.py  # post-mortem on first exception
```
```python
# Inside the code:
breakpoint()                        # Python 3.7+, drops into pdb
# Common pdb commands:
#   n   next line (step over)
#   s   step into
#   c   continue
#   b 42  set breakpoint at line 42
#   p var  print variable
#   pp var  pretty-print
#   w   where (stack trace)
#   l   list source
```

### Node.js — Chrome DevTools inspector
```bash
node --inspect-brk dist/main.js     # pause at first line, attach DevTools
# Chrome: open chrome://inspect → click "inspect"
# Sources panel: breakpoints, watch, step through, call stack, scope
```

### Git bisect — automated regression hunt
```bash
git bisect start
git bisect bad                          # current commit is broken
git bisect good v1.2.0                  # last known good
# Git checks out midpoint — test, then:
git bisect good   # or: git bisect bad
# Or fully automated:
git bisect start HEAD v1.2.0
git bisect run npm test                 # runs test on each bisect step
git bisect reset                        # when done
```

### Delve (Go)
```bash
dlv debug ./cmd/server                  # build & attach
dlv attach <pid>                        # attach to running process
dlv test ./pkg/...                      # debug tests
# (dlv) break main.go:55
# (dlv) continue
# (dlv) print myVar
# (dlv) goroutines                      # list goroutines (race conditions)
```

## Output Template

When delivering a debugging report, provide in this order:

1. **Root cause** — one-sentence statement of what specifically caused the issue
2. **Evidence** — stack trace, log lines, or test that proves the cause
3. **Reproduction** — minimal steps that trigger the bug 100% of the time
4. **Fix** — code change that resolves it (smallest diff that addresses root cause)
5. **Regression test** — test that fails before the fix and passes after
6. **Verification** — exact commands run and results:
   ```
   $ pytest tests/test_orders.py::test_negative_quantity
   FAILED (as expected, before fix)
   $ # apply fix
   $ pytest tests/test_orders.py::test_negative_quantity
   PASSED
   $ pytest -q                              # full suite, no regressions
   247 passed in 18s
   ```
7. **Prevention** — guard / type / lint rule / alert / postmortem link
8. **Exit report** — VERIFIED / ASSUMED / lingering risk

## Knowledge Reference

Systematic debugging (reproduce → isolate → hypothesise → test → fix → verify → prevent) · four mandatory phases (root-cause investigation, pattern analysis, hypothesis testing, implementation) · three-fix threshold (stop and question architecture after 3 failed attempts) · stack trace reading · binary search · `git bisect` (with `--run` automation) · minimal reproduction · delta debugging · rubber duck · time-travel debugging · bug taxonomies (off-by-one, null deref, race condition, memory leak, infinite loop, cache invalidation, closure capture, stale state, N+1, type coercion) · tools: pdb/ipdb (Python), Delve (Go), gdb/lldb (C/C++/Rust), Chrome DevTools (Node/browser), React DevTools, VS Code debugger · logging (structured, levels, redaction) · profiling (CPU flame graphs, memory, allocation, lock contention) · distributed tracing (OpenTelemetry) · blameless postmortems · 5 whys · action items with owners + dates
