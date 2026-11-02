# Debugging Strategies

Strategies for narrowing down where a bug lives. Pick the strategy that matches what you know.

## Binary Search

Divide and conquer to find the bug's location.

```markdown
1. Comment out / disable half the code
2. Test if bug still occurs
3. If yes: bug is in remaining half
4. If no: bug is in disabled half
5. Repeat until isolated
```

```typescript
// Bug in a data processing pipeline
async function process(data) {
  const step1 = await transform(data);
  // Bug somewhere below?
  const step2 = await validate(step1);
  console.log('After step2:', step2);          // checkpoint here
  const step3 = await enrich(step2);
  const step4 = await save(step3);
  return step4;
}
// If bug is in step3-4: bisect by commenting out enrich() and see if save still fails
```

Works on code, on data, on test cases, on dependency versions.

## Minimal Reproduction

Strip away everything until only the bug remains.

```markdown
1. Create new minimal project / test case
2. Add only code needed to reproduce
3. Remove dependencies one by one
4. Simplify inputs to smallest failing case
5. Document exact reproduction steps
```

```typescript
// Instead of debugging the full app, create a minimal test:
const input = { id: null };                    // minimal failing input
const result = processUser(input);
console.log(result);                            // isolate the exact failure
```

A minimal reproducer is also the best bug report you can file. Maintainers fix bugs with reproducers; they ignore bugs without.

## Git Bisect — Automated Regression Hunt

Find the exact commit that introduced the bug.

```bash
git bisect start
git bisect bad                                  # current commit is broken
git bisect good v1.0.0                          # last known good
# Git checks out the midpoint — test, then:
git bisect good   # or: git bisect bad
# Repeat until git says: "abc123 is the first bad commit"
git bisect reset
```

### Automated bisect with a test script
```bash
git bisect start HEAD v1.0.0
git bisect run npm test                         # runs test on each bisect step
# Git walks the history automatically and reports the first bad commit
git bisect reset
```

`git bisect run` is the killer feature — if you have a test that fails on the bug, git will binary-search the history without you.

### Bisect tips
- Mark `git bisect skip` for commits that don't build (don't waste time)
- Use `--first-parent` to skip merge commits: `git bisect start --first-parent HEAD v1.0.0`
- The "first bad commit" is often a small change — read its diff carefully
- If the bug is intermittent, bisect won't work — stabilise the repro first

## Time-Travel Debugging

Work backwards from the failure.

```markdown
1. Start at the error / failure point
2. What value caused it? Where did that come from?
3. Trace backwards through the code
4. Find where the value diverged from expected
```

```typescript
// Error: Cannot read 'name' of undefined at line 45
// Line 45: const name = user.name;
// Q: Why is user undefined?

// Line 40: const user = users.find(u => u.id === id);
// Q: Why didn't find() return a user?

// Check: Is the id correct? Are users populated?
console.log({ id, users, user });
```

### Time-travel debuggers (record + replay)
| Tool | Language | Feature |
|---|---|---|
| **rr** | C/C++/Rust | Record + reverse-execute under gdb |
| **replay.io** | JS/Node | Record + replay with DevTools |
| **Time Travel Debugging** | WinDbg | Windows native |
| **Java Flight Recorder** | JVM | Record events for later analysis |
| **Py-Undo / revive** | Python | Experimental |

With `rr`, you can `reverse-continue` — run backwards until a condition is true. Game-changing for Heisenbugs.

## Delta Debugging

When something recently broke.

```bash
# Check what changed
git diff HEAD~5..HEAD

# Check specific file history
git log -p --follow -- src/problematic-file.ts

# Find when the file last worked
git log --oneline -- src/problematic-file.ts
```

### Automated delta debugging (ccDelta / ddmerge)
Tools that automatically minimise a failing test case or a problematic input by repeatedly halving and testing. Useful for compiler bugs, parser bugs, and large failing inputs.

## Rubber Duck Debugging

Explain the problem step by step — to a person, a rubber duck, or a chat window.

```markdown
1. State what the code should do
2. Explain what it actually does
3. Walk through the code line by line
4. Describe what each line does
5. The discrepancy often becomes obvious
```

The act of explaining forces you to confront assumptions you didn't know you had. Half of all bugs are found during the explanation, before any code change.

## Divide by Data — Find the Outlier

If a bug only happens for some records, find what makes those records special.

```sql
-- Compare working vs failing records
SELECT * FROM orders WHERE id IN (working_id, failing_id);

-- Aggregate to find the pattern
SELECT status, COUNT(*)
FROM orders
WHERE created_at > '2024-01-01'
GROUP BY status;
```

```typescript
// Log every input; diff the working vs failing inputs
console.log(JSON.stringify(input));
// Then diff the two logs to find the differing field
```

## Change One Thing

The fundamental rule of experimentation. If you change three things and the bug disappears, you don't know which change fixed it — and you don't know what new bugs you introduced.

```markdown
❌ "I added a null check, updated the library, and changed the API endpoint. Bug's gone!"
✅ "I added a null check at line 42. Bug still reproduced. Reverted.
   I updated the library from 1.2 to 1.3. Bug still reproduced. Reverted.
   I changed the API endpoint from /v1 to /v2. Bug gone. Hypothesis confirmed."
```

## Watch the Right Variable

Beginners watch the variable that crashed. Experts watch where that variable came from.

```typescript
// Crash: users is undefined
console.log(users);                            // ❌ tells you it's undefined (you knew)
console.log(props);                            // ✅ tells you why
console.log(useQuery(GET_USERS));              // ✅✅ follows the chain
```

## Stop Adding Prints; Use a Debugger

`print` / `console.log` debugging requires a re-run for every question. A debugger lets you ask new questions at the breakpoint without re-running.

```python
# ❌ Slow iteration
print(f"users = {users}")
# re-run, see result, add another print, re-run...

# ✅ Fast iteration
breakpoint()
# at the prompt, inspect anything: users, props, callers, etc.
```

See `references/tools-by-language.md` for per-language debugger setup.

## Quick Reference

| Strategy | Best for |
|---|---|
| Binary search | Unknown bug location in a pipeline |
| Minimal repro | Complex bugs; reporting to maintainers |
| Git bisect | Regression bugs (something worked before) |
| Time-travel | Known error location, need to trace backwards |
| Delta debugging | Recent breakage (something changed) |
| Rubber duck | Logic errors, misunderstood requirements |
| Divide by data | Bug only affects some records |
| Change one thing | Hypothesis testing (always) |
| Use a debugger | Anything non-trivial (prefer over prints) |

## Strategy Selection

| If you know... | Use... |
|---|---|
| Nothing about where the bug is | Binary search on the pipeline |
| The bug was introduced recently | `git bisect` |
| The crash location but not the cause | Time-travel / backward tracing |
| The bug only affects some inputs | Divide by data |
| The bug is intermittent | Stabilise the repro first (eliminate nondeterminism); then bisect |
| The code is correct but output is wrong | Rubber duck the requirements |
| The bug is in a third-party dependency | Minimal repro → file issue |
| 3 fix attempts have failed | STOP — see `systematic-method.md` three-fix threshold |
