# Systematic Debugging Method

> **NO FIXES WITHOUT ROOT CAUSE INVESTIGATION FIRST.**

Jumping to fixes without understanding causes creates more bugs. Systematic debugging prevents the "fix one thing, break two more" cycle.

## The Seven-Step Loop

1. **Reproduce** — establish consistent reproduction steps; document them
2. **Isolate** — narrow to the smallest failing case
3. **Hypothesise** — form a specific, written hypothesis
4. **Test** — change one variable; record the result
5. **Fix** — implement the smallest fix addressing the root cause
6. **Verify** — run the full suite; manually confirm the repro works
7. **Prevent** — add a regression test, guard, or alert

## The Four Mandatory Phases

```
┌─────────────────────────────────────────────────────────────┐
│  PHASE 1: ROOT CAUSE INVESTIGATION                          │
│  ├── Read error messages thoroughly                         │
│  ├── Reproduce reliably with documented steps               │
│  ├── Examine recent changes (git log, git bisect)           │
│  ├── Trace data flow backward from the failure              │
│  └── Add diagnostic instrumentation at boundaries           │
├─────────────────────────────────────────────────────────────┤
│  PHASE 2: PATTERN ANALYSIS                                  │
│  ├── Find similar working implementations                   │
│  ├── Study reference implementations completely             │
│  └── Document all differences                              │
├─────────────────────────────────────────────────────────────┤
│  PHASE 3: HYPOTHESIS TESTING                                │
│  ├── Form specific, written hypothesis                      │
│  ├── Predict the outcome before testing                     │
│  ├── Test with minimal, isolated changes                    │
│  └── One variable at a time                                 │
├─────────────────────────────────────────────────────────────┤
│  PHASE 4: IMPLEMENTATION                                    │
│  ├── Create failing test case first                         │
│  ├── Implement single fix addressing root cause             │
│  └── Verify no new breakage (full suite)                    │
└─────────────────────────────────────────────────────────────┘
```

## Phase 1 — Root Cause Investigation

### Step 1.1: Read error messages thoroughly
```
TypeError: Cannot read property 'map' of undefined
    at UserList.render (UserList.tsx:24)
    at renderWithHooks (react-dom.js:14985)
    at mountIndeterminateComponent (react-dom.js:17811)
```
Key questions:
- What exact operation failed?
- Where in the code (file, line)?
- What was the call stack?
- Are there multiple errors or just one?

Don't read just the first line. Read the entire stack. The frame that crashed is at the top; the cause is usually several frames up.

### Step 1.2: Reproduce reliably
```markdown
## Reproduction Steps
1. Navigate to /users
2. Click "Load More" button
3. Wait for loading spinner
4. ERROR: "Cannot read property 'map' of undefined"

## Environment
- Browser: Chrome 120
- User role: Admin
- Data state: 50+ users in database
```
Document exact steps that reproduce the bug 100% of the time. If you can't reproduce reliably, you don't understand the bug yet — gather more info before going further.

### Step 1.3: Examine recent changes
```bash
git log --oneline -10                          # what changed recently?
git log -p UserList.tsx                        # diff history of the failing file
git bisect start HEAD v1.2.0                   # binary-search the regression
```

### Step 1.4: Trace data flow backward
```typescript
// Error happens here:
users.map(u => u.name)                         // users is undefined

// Trace backward: where does users come from?
const users = props.users;
// Where do props come from?
<UserList users={data.users} />
// Where does data come from?
const { data } = useQuery(GET_USERS);

// ROOT CAUSE: query returns { users: null } when loading
```

### Step 1.5: Add diagnostic instrumentation
```typescript
// At boundaries — temporary, remove before commit
console.log('[UserList] props:', JSON.stringify(props));
console.log('[UserList] users type:', typeof props.users);
console.log('[API] response:', response);
```
Better: use a debugger (see `references/tools-by-language.md`) so you don't have to commit-and-remove prints.

## Phase 2 — Pattern Analysis

### Step 2.1: Locate similar working implementations
```bash
grep -r "useQuery" src/components/ --include="*.tsx"
# Find how other lists handle loading states
grep -r "loading" src/components/*List* --include="*.tsx"
```

### Step 2.2: Study the reference implementation completely
```typescript
// WORKING: ProductList.tsx
function ProductList({ products, loading }) {
  if (loading) return <Spinner />;
  if (!products) return null;                  // ← handles undefined
  return products.map(p => <ProductItem key={p.id} {...p} />);
}

// BROKEN: UserList.tsx
function UserList({ users, loading }) {
  if (loading) return <Spinner />;
  // Missing: !users check
  return users.map(u => <UserItem key={u.id} {...u} />);   // 💥 crashes
}
```

### Step 2.3: Document all differences
| Aspect | Working (ProductList) | Broken (UserList) |
|---|---|---|
| Null check | `if (!products)` | Missing |
| Default value | `products ?? []` | None |
| Loading handled | Before render | Before render |
| Error handled | Returns ErrorState | Missing |

## Phase 3 — Hypothesis Testing

### Step 3.1: Form a specific, written hypothesis
```markdown
## Hypothesis #1
**Statement:** The crash occurs because `users` is undefined when the
query is complete but returns no data.

**Prediction:** Adding a null check before `.map()` will prevent the crash.

**Test:** Add `if (!users) return null;` before the map call.
```

### Step 3.2: Test with minimal changes — one variable at a time
```typescript
// Change ONLY one thing
function UserList({ users, loading }) {
  if (loading) return <Spinner />;
  if (!users) return null;                     // ← single change
  return users.map(u => <UserItem key={u.id} {...u} />);
}
```

### Step 3.3: Record results
| Hypothesis | Change | Result | Conclusion |
|---|---|---|---|
| #1: Null check | Add `if (!users)` | ✓ Pass | Confirmed |

Do NOT test multiple hypotheses simultaneously. If you change three things and the bug disappears, you don't know which change fixed it.

## Phase 4 — Implementation

### Step 4.1: Create a failing regression test first
```typescript
describe('UserList', () => {
  it('handles undefined users gracefully', () => {
    const { container } = render(<UserList users={undefined} loading={false} />);
    expect(container).not.toThrow();
    expect(screen.queryByRole('list')).not.toBeInTheDocument();
  });
});
```

### Step 4.2: Implement the smallest fix addressing the root cause
```typescript
function UserList({ users, loading }: UserListProps) {
  if (loading) return <Spinner />;
  if (!users || users.length === 0) {
    return <EmptyState message="No users found" />;
  }
  return <ul role="list">{users.map(u => <UserItem key={u.id} {...u} />)}</ul>;
}
```

### Step 4.3: Verify no new breakage
```bash
npm test                                       # full suite
npm test UserList                              # specific component
npm run test:integration                       # integration
# Manually verify in browser:
# 1. Normal case: 50 users
# 2. Empty case: 0 users
# 3. Loading case: spinner
# 4. Error case: error message
```

## The Three-Fix Threshold

> **After 3 failed fix attempts → STOP.**

Three failures in different locations signals architectural problems, not isolated bugs.

```
Fix Attempt 1: Added null check → New error in child component
Fix Attempt 2: Fixed child component → New error in parent
Fix Attempt 3: Fixed parent → Original error returns
                              ↓
                    STOP. QUESTION ARCHITECTURE.
```

### At the threshold, do this
1. Stop fixing symptoms
2. Document the pattern of failures
3. Identify the architectural assumptions being violated
4. Propose a structural change rather than a patch
5. Discuss with the team before proceeding

## Red Flags Requiring Process Reset

When you notice these, stop and restart from Phase 1:

| Red flag | Why it's wrong |
|---|---|
| Proposing solutions before tracing data flow | Guessing, not debugging |
| Making multiple simultaneous changes | Can't identify which change worked |
| Skipping test creation | Bug will recur |
| "Let's try this and see if it works" | Shotgun debugging |
| Fixing without understanding the cause | Band-aid, not cure |
| Disabling a failing test to make CI green | Hiding the bug |
| Adding `try/catch` to swallow the error | Hiding the symptom |

## Decision Flowchart

```
Bug reported → Can reproduce?
  No  → Get more info (logs, env, user actions)
  Yes → Trace data flow backward from the failure
        → Understand the cause?
             No  → Study similar working implementations; compare differences
             Yes → Write a specific, written hypothesis with predicted outcome
                   → Write a failing regression test
                   → Implement smallest fix addressing root cause
                   → Test passes?
                        No, attempts < 3 → Return to Phase 1
                        No, attempts ≥ 3 → STOP. Question architecture.
                        Yes → Done (full suite green, regression test added)
```

## Verification Before Claiming Fixed

### Checklist
Before writing "Fixed" or "Done":
- [ ] Change is implemented
- [ ] Regression test written (fails before, passes after)
- [ ] Full test suite passes (not just the changed file)
- [ ] Original reproduction steps no longer trigger the bug
- [ ] Edge cases tested (empty, null, max, concurrent)
- [ ] No unintended side effects
- [ ] Debug code removed (prints, breakpoints, log spam)
- [ ] Prevention added (guard, type, lint rule, alert)

### Acceptable responses
```markdown
✅ "Fixed. Added null check at line 42. Full suite (247 tests) passes."
✅ "Fixed at line 42. Verified with test_negative_quantity."
```

### Unacceptable responses
```markdown
❌ "I think this addresses your concern."
❌ "Should be fixed now."
❌ "Fixed (probably)."
```

## Quick Reference

| Phase | What you do | Output |
|---|---|---|
| 1. Root cause | Read stack, reproduce, trace data flow | Written reproduction steps + root cause hypothesis |
| 2. Pattern analysis | Compare with working code | Difference table |
| 3. Hypothesis testing | One change at a time | Pass/fail log per hypothesis |
| 4. Implementation | Failing test → fix → green | Regression test + minimal diff |

---

*Adapted from [obra/superpowers](https://github.com/obra/superpowers) by Jesse Vincent (@obra), MIT License, and the debugging-wizard source skill.*
