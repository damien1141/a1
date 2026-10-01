# Feedback Writing, Severity Rubric & Receiving Feedback

## Writing Feedback

### Good vs bad feedback

#### Be specific, not vague
```markdown
❌ "This is confusing"
✅ "This function handles both validation and persistence. Consider
   splitting into `validateUser()` and `saveUser()` for single
   responsibility and easier testing."
```

#### Be actionable, not just critical
```markdown
❌ "Fix the query"
✅ "This will cause N+1 queries — one per post. Use `include: [Author]`
   to eager load authors in a single query. See: [docs link]"
```

#### Be constructive, not demanding
```markdown
❌ "Add tests"
✅ "Missing test for the case when `email` is already taken. Add a test
   that verifies 409 is returned with the appropriate error message."
```

#### Ask questions, don't assume
```markdown
❌ "This is wrong"
✅ "I notice this returns null instead of throwing. Is that intentional?
   The other methods throw on not-found. Should this be consistent?"
```

### Praise specifically
```markdown
"Great use of early returns here — much more readable than nested ifs."
"Nice extraction of this validation logic into a reusable function."
"Excellent error messages — they'll help debugging in production."
"Good choice using a discriminated union here instead of optional fields."
"Appreciate the comprehensive test coverage, especially the edge cases."
```

### Feedback by category

#### Critical (must fix before merge)
```markdown
**[CRITICAL] Security: SQL Injection**
Location: `src/users/service.ts:45`

The query uses string interpolation:
`SELECT * FROM users WHERE id = ${id}`

This is vulnerable to SQL injection. Use a parameterized query:
`db.query('SELECT * FROM users WHERE id = $1', [id])`
```

#### Major (should fix)
```markdown
**[MAJOR] Performance: N+1 Query**
Location: `src/posts/service.ts:23`

Current code fetches users in a loop (N+1 problem):
```typescript
for (const post of posts) {
  post.author = await User.findById(post.authorId);
}
```

Suggestion: Use eager loading:
```typescript
const posts = await Post.findAll({ include: [User] });
```

Impact: ~100 extra DB queries per request with current approach.
```

#### Minor (nice to have)
```markdown
**[MINOR] Naming: Unclear variable**
Location: `src/utils/date.ts:12`

`d` is unclear. Consider `createdDate` or `timestamp` for readability.

**[MINOR] Style: Prefer const**
Location: `src/config/index.ts:8`

`let config` is never reassigned. Use `const` for immutability.
```

#### Question format
```markdown
**[QUESTION]**
Location: `src/orders/service.ts:67`

What's the expected behavior when the user has an existing pending order?
Should this:
- Return the existing order?
- Create a new one anyway?
- Return an error?
```

### Summary format
```markdown
## Summary

Overall this is a solid implementation of the user registration flow.
The validation logic is clean and the error handling is comprehensive.

**Blocking Issues**: 1 critical (SQL injection)
**Suggestions**: 2 major, 3 minor

Once the SQL injection is fixed, this is ready to merge. The major
suggestions are performance improvements worth considering.
```

## Severity Rubric (CVSS-based)

Use CVSS 3.1 to rate consistently. Compute the score with the [FIRST calculator](https://www.first.org/cvss/calculator/3.1). The qualitative rating maps to response SLAs.

### Severity definitions
| Severity | CVSS | Response time | Examples |
|---|---|---|---|
| **Critical** | 9.0–10.0 | Immediate; block release | RCE, auth bypass, full data access, SQL injection on critical endpoint |
| **High** | 7.0–8.9 | 24–48 hours; block merge | Privilege escalation, sensitive data exposure, weak crypto on auth |
| **Medium** | 4.0–6.9 | 1–2 weeks; fix in next sprint | CSRF on non-critical form, XSS on user-generated content, missing rate limit |
| **Low** | 0.1–3.9 | Next release | Missing security header, verbose error message, info disclosure of version |
| **Info** | n/a | Backlog | Best-practice suggestion, no security impact |

### Mapping to OWASP Top 10
| OWASP | Typical severity |
|---|---|
| A01 Broken Access Control | High–Critical (IDOR can leak all tenant data) |
| A02 Cryptographic Failures | High (plaintext passwords = Critical) |
| A03 Injection | Critical (SQLi on auth = full compromise) |
| A04 Insecure Design | varies; usually Medium–High |
| A05 Security Misconfiguration | Medium (debug mode) to Critical (default creds on admin) |
| A06 Vulnerable Components | depends on CVE; High if RCE |
| A07 Auth Failures | High (weak password policy) to Critical (auth bypass) |
| A08 Data Integrity Failures | Medium–High |
| A09 Logging Failures | Low–Medium (forensics impact) |
| A10 SSRF | High–Critical (can hit internal services, cloud metadata) |

### Rating process
1. Identify the attack vector (network / adjacent / local / physical)
2. Identify the attack complexity (low / high)
3. Identify privileges required (none / low / high)
4. Identify user interaction (none / required)
5. Identify scope (unchanged / changed)
6. Identify impact (confidentiality / integrity / availability — none/low/high)
7. Compute base score
8. Adjust for temporal + environmental factors

### Example finding entry
```
ID: SEC-001
Severity: High (CVSS 8.1 — AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N)
Title: SQL Injection in user search endpoint
File: src/api/users.py:42
CWE: CWE-89
OWASP: A03:2021 — Injection
Description: User-supplied input is concatenated directly into a SQL query.
Impact: An attacker can read, modify, or delete database contents.
Remediation: Use parameterized queries.
Effort: 1 hour
Priority: Immediate
```

## Receiving Feedback

### Core mindset
> "Verify before implementing. Ask before assuming. Technical correctness over social comfort."

Code review feedback is a technical discussion, not a social one. Focus on the code, not on feelings.

### Six-step process

1. **Read completely** — without reacting. Read the entire comment before forming any response.
2. **Restate requirements** — rephrase the reviewer's feedback in your own words to confirm understanding.
3. **Check against codebase** — verify the feedback against actual code conditions before responding.
4. **Evaluate technical soundness** — does the feedback apply to your specific stack and context?
5. **Respond with substance** — technical acknowledgment or reasoned objection.
6. **Implement one at a time** — address each piece of feedback individually with verification.

### Forbidden phrases (agreement theater)
| Phrase | Why it's wrong |
|---|---|
| "You're absolutely right!" | Sycophantic; adds no information |
| "Great point!" | Empty praise, not a response |
| "Excellent feedback!" | Flattery, not engagement |
| "Thanks for catching this!" | Unnecessary; just fix it |
| "I really appreciate..." | Social fluff, not technical |

```markdown
❌ "You're absolutely right! Great catch on that null check! Thanks so much!"
✅ "Fixed. Added null check at line 42."
```

The code change shows you understood. Words are redundant.

### When to push back

| Situation | How to respond |
|---|---|
| Breaks existing functionality | "This change would break Feature X (see test at tests/feature-x.spec.ts:34)" |
| Lacks full codebase context | "This pattern exists because of Y (see architecture.md#constraints)" |
| Violates YAGNI | "This flexibility isn't needed yet — only one caller exists" |
| Is technically incorrect | "This actually works because of Z (link to docs)" |
| Conflicts with established architecture | "This conflicts with our JWT approach (see auth/README.md)" |

### Good pushback format
```markdown
This conflicts with [X]. [Evidence]. Was that the intent, or should we [alternative]?

Example:
This conflicts with our JWT authentication architecture (see auth/token.js:45).
Switching to sessions would require restructuring the API middleware.
Was that the intent, or should we keep JWT?
```

### Bad pushback
```markdown
❌ "I don't think that's right."
❌ "That won't work."
❌ "We've always done it this way."
❌ "That's too much work."
```

## Verification Before Claiming Fixed

### Checklist
Before writing "Fixed" or "Done":
- [ ] Change is implemented
- [ ] Tests pass (full suite, not just changed files)
- [ ] Specific behavior mentioned in feedback is verified
- [ ] Edge cases are tested
- [ ] No unintended side effects introduced

### Acceptable responses
```markdown
✅ "Fixed. Added null check. Tests pass."
✅ "Fixed at line 42. Verified with test case X."
✅ "Implemented. All 47 tests pass."
```

### Unacceptable responses
```markdown
❌ "I think this addresses your concern."
❌ "Should be fixed now."
❌ "Done, I believe."
❌ "Fixed (probably)."
```

### When you can't verify
```markdown
✅ "Implemented the change, but I'm unable to verify because [specific reason].
    Can you confirm on your end?"
```

## Anti-Patterns

| Pattern | Problem | Fix |
|---|---|---|
| Defensive responses | Creates conflict, wastes time | Assume good faith, respond technically |
| Apologetic responses | Unprofessional, adds noise | Just fix it |
| Delayed responses | Blocks review cycle | Respond within hours, not days |
| Vague responses | Leaves reviewer uncertain | Be specific about changes |
| Ignoring feedback | Disrespectful, creates friction | Address every point |
| Sycophantic responses | "Great catch!" wastes reader time | State the fix only |

## Quick Reference

| Situation | Response |
|---|---|
| Reviewer is correct | "Fixed. [What you changed]." |
| You need clarification | "To confirm: you're suggesting [restatement]?" |
| Reviewer is incorrect | "This works because [evidence]. [Link to proof]." |
| You disagree on approach | "This conflicts with [X]. Should we [alternative]?" |
| You learned something | "I wasn't aware of [X]. Fixed at line [N]." |
| You can't verify | "Implemented. Unable to verify because [reason]." |
