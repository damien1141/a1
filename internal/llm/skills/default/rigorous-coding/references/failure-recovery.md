# Failure Recovery

What to do when the fix doesn't fix it. Most stuck debugging sessions are not a lack of effort — they are a refusal to update the diagnosis. Circuit breakers force the update.

## The core principle

When a fix fails, the temptation is to try a slightly different fix from the same mental model. After all, the diagnosis "feels" right; the fix just wasn't quite there. This is how engineers spend four hours trying five variations of the same wrong hypothesis.

The circuit breaker rule:

> **Two failed fix attempts from the same diagnosis → the diagnosis is wrong. Stop fixing. Re-diagnose.**

Strike 1 might be a typo. Strike 2 is a signal. Strike 3, if you attempt it from the same model, is a self-inflicted wound.

## The 2-strike protocol

```text
Strike 1: Hypothesis A → fix → run tests → still failing.
  ↓ Maybe the fix was wrong. Try once more with a sharper version.

Strike 2: Hypothesis A' (refined A) → fix → run tests → still failing.
  ↓ STOP. The model producing these fixes is wrong.

Do NOT attempt Strike 3 from the same model. Do instead:
  1. Re-read the actual error message verbatim, not your summary of it.
  2. Re-read the actual code at the failure site, not your memory of it.
  3. Add a print/log/trace right at the failure. Capture the actual runtime values.
  4. Reproduce in isolation: new file, smallest input that triggers the bug.
  5. Form hypothesis B grounded in observed values, not assumed ones.
  6. Before implementing B, write down what result would falsify B. (See the-fool / evidence-audit thinking.)
  7. Implement B. Run the test. If it fails, that's Strike 1 against B — not Strike 3 against A.
```

The strike counter resets when the diagnosis changes, because a new diagnosis is a new investigation. What kills hours is running the same diagnosis through five fix variations without ever questioning it.

## Why engineers skip the breaker

| Reason | Reality |
|--------|---------|
| "I'm so close, one more tweak" | You've been "so close" for 90 minutes. The closeness is a feeling, not a measurement. |
| "Restarting from scratch wastes the work" | The wrong-model work doesn't transfer. You'll re-derive the same wrong hypothesis. |
| "Re-reading the error is for juniors" | Juniors read the error. Seniors summarize it from memory and miss the detail that matters. |
| "The fix must be right, the test must be wrong" | Sometimes true (flaky test). Verify the test independently before assuming this. |
| "I don't want to admit I was wrong" | The bug is the adversary, not your ego. Wrong diagnoses are normal; clinging to them is the failure. |

## Re-diagnosis techniques

### 1. Read the error verbatim

Engineers summarize errors. Summaries drop details. The dropped detail is often the bug.

```text
What you remember:    "TypeError in the auth code somewhere"
The actual error:     "TypeError: Cannot read properties of undefined (reading 'length')
                       at validateScopes (auth.ts:142)
                       at /node_modules/express/lib/router/layer.js:95
                       ..."
What it actually means: validateScopes is being called with an undefined argument.
                        The bug is at the call site, not in validateScopes.
```

Read the error. Read the file. Read the line. Don't summarize until you've read it three times.

### 2. Print the values

Add a log line at the failure site that dumps every local variable. Run the repro. Read the actual values. Most stuck debugging ends within 5 minutes of printing the values, because the values contradict the model.

```python
def dedup_within_window(events, window_ms):
    print(f"DEBUG dedup: events={events!r} window_ms={window_ms!r}", flush=True)
    # ... existing code ...
```

Yes, this is the "junior" technique. It works because it converts assumptions about values into observations of values. Use it.

### 3. Isolate the repro

A repro inside a 50k-line codebase is a repro with 50k confounding variables. A repro in a 30-line file is a repro with 0.

```python
# repro_1234.py — copy the minimum to trigger the bug
from mypkg.dedup import dedup_within_window
events = [Event(ts=1), Event(ts=2)]
print(dedup_within_window(events, 10))
# Expected: [Event(ts=1), Event(ts=2)]
# Actual:   [Event(ts=1)]   ← bug reproduced in isolation
```

If you cannot reproduce in isolation, the bug depends on something you haven't identified. That something is the bug.

### 4. Bisect

If the bug appeared "recently" and you have version control:

```bash
git bisect start
git bisect bad HEAD
git bisect good v1.2.3    # last known good
# git will check out commits; run the repro at each
git bisect run ./repro.sh
# ends at the offending commit
```

The offending commit's diff is your new evidence. It narrows the search from "the whole codebase" to "these 40 lines."

### 5. Rubbery duck at the literal level

Explain the bug out loud, in order, including every value and every line. Don't summarize. The act of saying "and then `events` is `[{ts:1}, {ts:2}]` and `window_ms` is `10` so we enter the loop and `out[-1].ts` is `1` so `ev.ts - out[-1].ts` is `1` which is less than `10` so we drop the second event — wait, that's the bug" is the act of finding the bug. The duck is optional; the literal narration is not.

## Root cause vs symptom

A symptom patch makes the visible failure go away. A root-cause fix makes the failure mechanism impossible.

| Symptom patch | Root-cause fix |
|---------------|----------------|
| `try { riskyThing() } catch { /* ignore */ }` | Fix why `riskyThing` throws |
| `if (x === undefined) x = defaultValue` | Fix why `x` is undefined |
| Restart the service every hour via cron | Fix the memory leak |
| Raise the timeout from 5s to 30s | Fix why the query is slow |
| Disable the failing test | Fix the code the test exercises |

Symptom patches are sometimes correct (you don't control the upstream bug, so you catch and degrade). The discipline is: **label them as symptom patches, file the root cause, and put a time bound on the patch.** A symptom patch that lives forever is a root cause you stopped looking for.

### The 3-Why rule

Before patching, ask "why" at least three times.

```text
Symptom: Login returns 500.
Why 1: Because `user.email` is null when we try to send the welcome email.
Why 2: Because `user.email` was never set during signup.
Why 3: Because the signup form's email field has name="email_address" but the
        backend reads req.body.email.

Root cause: form/backend field name mismatch.
Fix: rename the form field to "email" (or accept both names server-side).

Symptom patch (if you can't fix the form right now):
  - Skip welcome email if user.email is null
  - File ticket to fix the form
  - Add a metric so you know how many users hit this
```

Without the 3 whys, you ship "skip welcome email if null" and 5% of users never get a welcome email forever. With the 3 whys, you ship the same patch but with a ticket and a metric, and the form gets fixed next sprint.

## When to call it: "I'm stuck"

The hardest discipline is recognizing stuck-ness in yourself. Signals:

- You've made the same kind of change twice and it didn't work.
- You're reading the same 50 lines of code for the third time.
- You're googling variants of the same query.
- You've said "but that should work" more than twice.
- You're considering a `try/catch` to "make it go away."

When you notice these, stop. Execute the 2-strike protocol. If you've already executed it and you're still stuck, escalate: pair with someone, write up the literal repro and the literal observations, and ask for help. Stuck-for-an-hour is a normal debugging state; stuck-for-four-hours is a process failure.

## Recovery is part of rigor

Failure recovery is not separate from `rigorous-coding` — it is `rigorous-coding` under the hardest conditions. The 5-gate loop assumes forward progress; the circuit breakers guarantee that when forward progress stalls, you notice and change approach rather than grinding the same groove deeper. A rigorous engineer is not one who never gets stuck; one who notices they are stuck fast, and has a protocol for getting unstuck.
