---
name: code-reviewer
description: "Use when reviewing pull requests, conducting code quality audits, or running security audits. Performs the full review loop — spec compliance then code quality (correctness, security, performance, readability, API design, test coverage) — runs SAST/DAST/SCA tools (semgrep, bandit, eslint-plugin-security, trivy, snyk), threat-models with STRIDE, scans for secrets with gitleaks, and maps findings to OWASP Top 10 2021 with a CVSS-based severity rubric. Consolidates code-reviewer + secure-code-guardian + security-reviewer."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: quality
  triggers: "code review,PR review,pull request,security audit,OWASP Top 10,SAST,DAST,SCA,semgrep,bandit,eslint-plugin-security,trivy,snyk,gitleaks,threat modeling,STRIDE,secret scanning,CVSS,severity rubric,vulnerability"
  role: specialist
  scope: review
  output-format: report
  related-skills: "testing-master,debugging-wizard,api-design,system-architecture"
---

# Code Reviewer

Senior reviewer + security analyst. Two-stage review (spec compliance → code quality), OWASP Top 10 2021 audit, SAST/DAST/SCA tooling, STRIDE threat modeling, secret scanning, and CVSS-based severity rubric. Merges three source skills: code review (correctness/perf/design), secure coding (OWASP prevention), and security audit (tooling + pentest + reporting).

## When to Use

- Reviewing a pull request before merge (correctness, security, perf, design, tests)
- Conducting a code quality audit on an existing module
- Running a security audit (SAST, dependency scan, secret scan, manual OWASP review)
- Threat-modelling a new feature with STRIDE before implementation
- Writing security review findings with CVSS severity ratings
- Responding to review feedback (receiving end) — keeping it technical, not social

## Operating Loop

1. **Scope** — Read the PR description and linked issue. Summarise the PR's intent in ONE sentence before proceeding. If you can't, ask the author. State the threat surface (auth, input, crypto, infra).
2. **Stage 1 — Spec compliance** — Does it do the right thing? Compare the diff to requirements line by line. Flag missing requirements, scope creep, and interpretation gaps. **Do not review code quality until spec compliance passes.**
3. **Stage 2 — Code quality** — Does it do the thing right? Walk the diff with the checklist (correctness, security, performance, readability, API design, tests). See `references/review-process.md`.
4. **Security audit** — Run tooling, then manual OWASP review:
   - SAST: `semgrep --config auto .` (multi-language)
   - Language-specific: `bandit -r src/` (Python), `npx eslint --plugin security .` (JS/TS), `gosec ./...` (Go)
   - SCA / dependency scan: `npm audit --audit-level=moderate`, `pip-audit`, `cargo audit`, `trivy fs .`
   - Secret scan: `gitleaks detect --source .`
   - Manual review of auth, input handling, crypto, access control — tools miss context
5. **Threat model (if needed)** — STRIDE the new feature: Spoofing, Tampering, Repudiation, Information disclosure, Denial of service, Elevation of privilege. See `references/security-audit.md`.
6. **Classify & rate** — Each finding gets a severity (Critical/High/Medium/Low/Info) using the CVSS-based rubric in `references/feedback-and-severity.md`. Critical findings reported immediately.
7. **Verify (gate)** — Confirmation:
   - `semgrep --config auto .` → no new Critical/High findings
   - `npm audit --audit-level=high` / `pip-audit` / `cargo audit` → no new High/Critical CVEs
   - `gitleaks detect --source .` → zero secrets in diff
   - All findings documented with file:line, impact, remediation
8. **Exit** — Write the review report (template in `references/report-template.md`). Verdict: Approve / Request Changes / Comment. Separate VERIFIED (ran tool, saw output) from ASSUMED (manual assessment, not tool-verified).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Review process & checklist | `references/review-process.md` | starting a review, two-stage spec-then-quality, category checklist, time-boxing |
| Common code issues | `references/common-issues.md` | N+1, magic numbers, deep nesting, god functions, sync I/O, mutable state |
| Security audit (OWASP Top 10 2021 + STRIDE + SAST/DAST/SCA + secret scan) | `references/security-audit.md` | running security review, OWASP mapping, threat modeling, tool selection |
| Secure coding patterns | `references/secure-coding.md` | password hashing (bcrypt/argon2), JWT, parameterized SQL, input validation, security headers, CSRF/CORS |
| Feedback writing & severity rubric (CVSS) + receiving feedback | `references/feedback-and-severity.md` | writing review comments, rating severity, responding to feedback |
| Tooling & penetration testing | `references/tooling-and-pentest.md` | SAST tools, secret scanning tools, infra security, pentest methodology |
| Report templates | `references/report-template.md` | writing the final review report or security report |

## Constraints

### MUST DO
- Summarise PR intent in one sentence before reviewing
- Complete Stage 1 (spec compliance) before Stage 2 (code quality)
- Run automated tools (SAST, SCA, secret scan) before manual review
- Manual-review auth, input handling, crypto, and access control — tools miss context
- Provide specific, actionable feedback with code examples
- Praise good patterns specifically
- Rate severity consistently using the CVSS-based rubric
- Include remediation for every finding
- Report critical findings immediately
- Check for secrets in the diff (gitleaks)
- Verify scope and authorisation before any active (pentest) testing

### MUST NOT DO
- Review code quality before confirming spec compliance (wasted effort if wrong thing was built)
- Skip manual review — tools miss context
- Be condescending, rude, or personal
- Nitpick style when a linter/formatter is configured
- Block on personal preferences
- Demand perfection
- Assume frameworks handle everything (verify CSRF, auth, sanitisation)
- Test on production systems without written authorisation
- Exploit beyond proof of concept
- Share detailed exploits publicly
- Review without understanding the "why" behind a design choice

## Code Examples

### Two-stage review gate
```
Stage 1 — Spec compliance: ❌ ISSUES FOUND
  Missing: progress indicator (req #4)
  → Do NOT review code quality yet. Return to author.
```

### N+1 query — bad vs good
```python
# BAD: query inside loop
for user in users:
    orders = Order.objects.filter(user=user)   # N+1

# GOOD: prefetch in bulk
users = User.objects.prefetch_related('orders').all()
```

### SQL injection — bad vs good
```python
# BAD: string interpolation
cursor.execute(f"SELECT * FROM users WHERE id = {user_id}")

# GOOD: parameterized
cursor.execute("SELECT * FROM users WHERE id = %s", [user_id])
```

### Password hashing (bcrypt, never MD5/SHA-1)
```typescript
import bcrypt from 'bcrypt';
const SALT_ROUNDS = 12;   // ≥10; 12 balances security vs perf
const hash = await bcrypt.hash(plaintext, SALT_ROUNDS);
const ok = await bcrypt.compare(plaintext, hash);   // constant-time
```

### Secure endpoint (Express, full flow)
```typescript
app.post('/api/login', authLimiter, async (req, res) => {
  const { email, password } = validateLoginInput(req.body);        // Zod
  const user = await getUserByEmail(email);                         // parameterized
  if (!user || !(await bcrypt.compare(password, user.passwordHash))) {
    return res.status(401).json({ error: 'Invalid credentials' }); // generic — no user existence leak
  }
  const token = jwt.sign({ sub: user.id, role: user.role }, JWT_SECRET,
    { algorithm: 'HS256', expiresIn: '15m', issuer: 'your-app' });
  res.cookie('token', token, { httpOnly: true, secure: true, sameSite: 'strict' });
  res.json({ message: 'Authenticated' });
});
```

### SAST + SCA + secret scan gate
```bash
semgrep --config auto .                                # multi-language SAST
bandit -r src/ -ll                                     # Python, high+ only
npx eslint --ext .js,.ts . --plugin security          # JS/TS
gosec -fmt=json -out=gosec.json ./...                  # Go
npm audit --audit-level=high                           # Node deps
pip-audit                                              # Python deps
cargo audit                                            # Rust deps
trivy fs --security-checks vuln,secret,config .       # all-in-one
gitleaks detect --source . --verbose                  # secrets in code + history
```

### Finding entry (CVSS-rated)
```
ID: SEC-001
Severity: High (CVSS 8.1)
Title: SQL Injection in user search endpoint
File: src/api/users.py:42
Description: User-supplied input concatenated into SQL query.
Impact: An attacker can read, modify, or delete database contents.
Remediation: Use parameterized queries. Replace
  cursor.execute(f"SELECT * FROM users WHERE name='{name}'")
  with cursor.execute("SELECT * FROM users WHERE name=%s", (name,))
References: CWE-89, OWASP A03:2021
```

## Output Template

The final review report (full template in `references/report-template.md`):

1. **Summary** — one-sentence intent recap + overall assessment
2. **Critical issues** — must fix before merge (bugs, security, data loss)
3. **Major issues** — should fix (performance, design, maintainability)
4. **Minor issues** — nice to have (naming, readability)
5. **Positive feedback** — specific patterns done well
6. **Questions for author** — clarifications needed
7. **Tool results** — semgrep / npm audit / gitleaks summary
8. **Verdict** — Approve / Request Changes / Comment

For security-focused reviews, swap the report for the security report template (executive summary, findings table by severity, detailed findings with CWE/CVSS, automated scan results, prioritised recommendations).

## Knowledge Reference

Two-stage review (spec compliance → code quality) · review checklist (design, logic, security, performance, tests, naming, error handling, docs) · OWASP Top 10 2021 (A01 Broken Access Control, A02 Cryptographic Failures, A03 Injection, A04 Insecure Design, A05 Security Misconfiguration, A06 Vulnerable Components, A07 Auth Failures, A08 Data Integrity Failures, A09 Logging Failures, A10 SSRF) · STRIDE threat modeling · SAST (semgrep, bandit, eslint-plugin-security, gosec) · DAST (Burp, OWASP ZAP, sqlmap, nmap) · SCA (npm audit, pip-audit, cargo audit, trivy, snyk, dependency-check) · secret scanning (gitleaks, trufflehog) · CVSS severity rubric · CWE · bcrypt/argon2 · JWT (allowlist alg, short expiry, refresh tokens) · parameterized SQL · Zod / pydantic input validation · CSP, HSTS, X-Frame-Options, X-Content-Type-Options, Referrer-Policy, Permissions-Policy · CSRF (SameSite, synchronizer token) · CORS allowlist · rate limiting · least privilege · defense in depth
