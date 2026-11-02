# Report Templates

Two templates: a code review report (broad quality + security) and a security review report (deep security audit). Use the second when the review is security-focused.

## Code Review Report Template

```markdown
# Code Review: [PR Title]

## Summary
[1-2 sentence overview of the changes and overall assessment]

**Verdict**: [ ] Approve  |  [x] Request Changes  |  [ ] Comment

## Stage 1 — Spec Compliance
- [ ] All required features present
- [ ] No scope creep
- [ ] No interpretation gaps
**Result**: ✅ Pass  /  ❌ Issues (return to author)

## Critical Issues (Must Fix)

### 1. [File:Line] Security: SQL Injection Risk
- **Current**: String interpolation in query
- **Suggested**: Use parameterized query
- **Impact**: Potential data breach
- **CWE**: CWE-89, OWASP A03:2021
- **Severity**: Critical (CVSS 9.8)

```typescript
// Current (vulnerable)
const query = `SELECT * FROM users WHERE id = ${id}`;
// Suggested (secure)
db.query('SELECT * FROM users WHERE id = $1', [id]);
```

## Major Issues (Should Fix)

### 1. [File:Line] Performance: N+1 Query
- **Current**: Fetching users in loop
- **Suggested**: Eager loading with include
- **Impact**: ~100 extra DB queries per request

### 2. [File:Line] Logic: Missing edge case
- **Current**: No handling for empty array
- **Suggested**: Add guard clause

## Minor Issues (Nice to Have)

### 1. [File:Line] Naming: Unclear variable name
- **Current**: `d`
- **Suggested**: `createdDate`

### 2. [File:Line] Style: Inconsistent formatting
- **Current**: Mixed quotes
- **Suggested**: Use single quotes consistently

## Positive Feedback
- Clean separation of concerns in service layer
- Comprehensive input validation on DTOs
- Good test coverage for edge cases
- Excellent error messages
- Discriminated union used correctly

## Questions for Author
- What's the expected behavior when X happens?
- Should this support pagination for large datasets?
- Is the retry logic intentional or accidental?

## Test Coverage Assessment
- [ ] Happy path tested
- [x] Error cases tested
- [ ] Edge cases tested (missing empty array test)
- [x] Integration tests present

## Automated Tool Results

| Tool | Critical | High | Medium | Low |
|---|---|---|---|---|
| semgrep | 1 | 2 | 4 | 8 |
| npm audit | 0 | 2 | 4 | 10 |
| gitleaks | 0 | — | — | — |

## Checklist
- [x] No security vulnerabilities
- [ ] Performance is acceptable (N+1 issue)
- [x] Code is readable
- [x] Tests are adequate
- [x] Documentation is present
```

### Verdict guidelines
| Verdict | When |
|---|---|
| **Approve** | No blocking issues; minor suggestions only |
| **Request Changes** | Critical or major issues must be fixed |
| **Comment** | Questions need answers; no blocking issues |

### Time-boxing
| Section | Suggested time |
|---|---|
| Context & understanding | 5 min |
| Spec compliance | 10 min |
| Critical/security review | 10 min |
| Logic & performance | 15 min |
| Tests review | 10 min |
| Writing report | 10 min |
| **Total** | ~60 min |

## Security Review Report Template

```markdown
# Security Review Report

## Executive Summary

| Field | Value |
|---|---|
| **Application** | [Application Name] |
| **Review Date** | YYYY-MM-DD |
| **Reviewer** | [Name] |
| **Scope** | [Files/modules reviewed] |
| **Overall Risk Level** | Critical / High / Medium / Low |

### Key Findings
- X Critical vulnerabilities requiring immediate attention
- Y High-severity issues to address before deployment
- Z Medium/Low issues for future consideration

## Findings Summary

| Severity | Count | Response SLA | Status |
|---|---|---|---|
| Critical | X | Immediate | Block release |
| High | X | 24–48 hours | Block merge |
| Medium | X | 1–2 weeks | Next sprint |
| Low | X | Next release | Backlog |

## Detailed Findings

### [CRITICAL] SQL Injection in User Search

| Field | Value |
|---|---|
| **ID** | SEC-001 |
| **Location** | `src/api/users.ts:45` |
| **CWE** | CWE-89 |
| **CVSS** | 9.8 (Critical) — AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H |
| **OWASP** | A03:2021 — Injection |

**Description**
User input directly concatenated into SQL query without sanitization.

**Vulnerable Code**
```typescript
const query = `SELECT * FROM users WHERE name LIKE '%${searchTerm}%'`;
```

**Proof of Concept**
```
GET /api/users?search=' OR '1'='1
```

**Impact**
- Full database read access
- Data modification / deletion
- Potential RCE via SQL features (e.g. `COPY TO PROGRAM`)

**Remediation**
Use parameterized queries:
```typescript
const query = 'SELECT * FROM users WHERE name LIKE $1';
db.query(query, [`%${searchTerm}%`]);
```

**Effort**: 1 hour    **Priority**: Immediate

---

### [HIGH] Weak Password Requirements

| Field | Value |
|---|---|
| **ID** | SEC-002 |
| **Location** | `src/auth/validation.ts:12` |
| **CWE** | CWE-521 |
| **CVSS** | 7.5 (High) |
| **OWASP** | A07:2021 — Auth Failures |

**Description**
Password policy requires only 6 characters with no complexity.

**Current Policy**
```typescript
const isValid = password.length >= 6;
```

**Impact**
- Susceptible to brute force and dictionary attacks

**Remediation**
```typescript
const isValid =
  password.length >= 12 &&
  /[A-Z]/.test(password) &&
  /[a-z]/.test(password) &&
  /[0-9]/.test(password) &&
  /[^A-Za-z0-9]/.test(password);
```

**Effort**: 30 min    **Priority**: Before deployment

## Automated Scan Results

### Dependency Vulnerabilities
| Package | Severity | CVE | Fix |
|---|---|---|---|
| lodash | High | CVE-2021-23337 | Upgrade to 4.17.21 |

### SAST Findings
| Tool | Critical | High | Medium | Low |
|---|---|---|---|---|
| Semgrep | 1 | 3 | 5 | 8 |
| npm audit | 0 | 2 | 4 | 10 |
| gitleaks | 0 | — | — | — |

### Secret Scan
| Tool | Result |
|---|---|
| gitleaks | 0 findings in diff |
| git history scan | 1 historical finding (rotated) |

## Recommendations

### Immediate (This Sprint)
1. Fix SQL injection (SEC-001)
2. Apply parameterized queries globally
3. Update vulnerable dependencies

### Short-term (Next Sprint)
1. Strengthen password policy (SEC-002)
2. Add input validation middleware
3. Enable security headers (helmet)

### Long-term
1. Implement SAST in CI/CD pipeline (semgrep + gitleaks on every PR)
2. Schedule quarterly security reviews
3. Security training for developers
4. Threat-model new features with STRIDE before implementation

## Appendix

### Tools Used
- Semgrep v1.x with `p/owasp-top-ten`
- npm audit (Node 20)
- Gitleaks v8.x
- Trivy v0.50 (fs + secret + config)
- Manual review (auth, input, crypto, access control)

### References
- OWASP Top 10 2021
- CWE Database (https://cwe.mitre.org)
- CVSS 3.1 Calculator (https://www.first.org/cvss/calculator/3.1)
- STRIDE threat modeling
```

## Severity Definitions (cross-reference)

| Severity | CVSS | Response time |
|---|---|---|
| Critical | 9.0–10.0 | Immediate; block release |
| High | 7.0–8.9 | 24–48 hours; block merge |
| Medium | 4.0–6.9 | 1–2 weeks; next sprint |
| Low | 0.1–3.9 | Next release |
| Info | n/a | Backlog |

## Quick Checks Before Submitting a Report

- [ ] All critical issues have clear remediation with code examples
- [ ] Major issues explain the impact
- [ ] At least one positive comment included
- [ ] Questions are specific and answerable
- [ ] Verdict matches the issues found
- [ ] Tool results are summarised (counts by severity)
- [ ] Findings reference CWE + OWASP category where applicable
- [ ] CVSS scores computed, not guessed
- [ ] Effort + priority included on each finding
- [ ] Scope and authorisation confirmed (for security reviews)
