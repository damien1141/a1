# Security Audit — OWASP Top 10 2021 + STRIDE + SAST/DAST/SCA + Secret Scanning

This reference consolidates the security audit discipline from three source skills (secure-code-guardian's OWASP prevention + security-reviewer's SAST/DAST/penetration testing). It is the deep-dive loaded when a review needs to actively hunt vulnerabilities, not just spot them by eye.

## OWASP Top 10 (2021)

| # | Category | Prevention summary |
|---|---|---|
| **A01** | Broken Access Control | Deny by default; server-side authorisation; verify ownership (IDOR); least privilege |
| **A02** | Cryptographic Failures | Encrypt at rest + transit (TLS 1.2+); bcrypt/argon2 for passwords; no MD5/SHA-1/DES/ECB |
| **A03** | Injection | Parameterised queries / ORM; `execFile` not `exec`; allowlist input |
| **A04** | Insecure Design | Threat-model with STRIDE; fail-safe defaults; segregate tiers |
| **A05** | Security Misconfiguration | Security headers (CSP, HSTS, X-Frame-Options); disable defaults; no debug in prod |
| **A06** | Vulnerable & Outdated Components | `npm audit` / `pip-audit` / `cargo audit` / `trivy fs`; pin versions; SBOM |
| **A07** | Identification & Auth Failures | Strong passwords; MFA; lockout; secure session cookies; short-lived JWT |
| **A08** | Software & Data Integrity Failures | Signed updates; CI/CD pipeline integrity; no `pickle`/`eval`; deserialise with schema |
| **A09** | Security Logging & Monitoring Failures | Log auth events, privilege escalation, input validation failures; centralised logs |
| **A10** | Server-Side Request Forgery (SSRF) | Validate URLs against allowlist; block internal IPs; disable redirect following |

### A01 — Broken Access Control
```typescript
// ❌ No authorisation check — IDOR
app.get('/documents/:id', async (req, res) => {
  const doc = await Document.findById(req.params.id);
  res.json(doc);
});

// ✅ Verify ownership
app.get('/documents/:id', async (req, res) => {
  const doc = await Document.findOne({ _id: req.params.id, userId: req.user.id });
  if (!doc) return res.status(404).json({ error: 'Not found' });
  res.json(doc);
});

// ✅ Role-based middleware
function requireRole(...roles: string[]) {
  return (req, res, next) => {
    if (!roles.includes(req.user.role)) return res.status(403).json({ error: 'Forbidden' });
    next();
  };
}
```
Check both **horizontal** (user A accessing user B's data) and **vertical** (regular user accessing admin routes) escalation.

### A02 — Cryptographic Failures
- Hash passwords with bcrypt (≥10 rounds, 12 recommended) or argon2id
- Never store passwords in plaintext or with reversible encryption
- Never use MD5, SHA-1, DES, ECB mode
- TLS 1.2+ for transit; HSTS header
- Encrypt PII at rest with AES-256-GCM; rotate keys
- Don't roll your own crypto

### A03 — Injection (SQL, NoSQL, Command, LDAP, XPath)
```typescript
// ❌ SQL injection
db.query(`SELECT * FROM users WHERE id = ${userId}`);
// ❌ Command injection
exec(`ls ${userInput}`);

// ✅ Parameterised SQL
db.query('SELECT * FROM users WHERE id = $1', [userId]);
// ✅ ORM
await prisma.user.findUnique({ where: { id: userId } });
// ✅ execFile with args array (no shell)
execFile('convert', ['-resize', '100x100', safeFilename]);
// ✅ Or use a library instead of shelling out
await sharp(inputPath).resize(100, 100).toFile(outputPath);
```

### A04 — Insecure Design
- Threat-model new features with STRIDE before writing code
- Fail-safe defaults (deny by default); segregate tenant data
- Rate-limit by user, not just IP; re-validate on server (never trust client-side)

### A05 — Security Misconfiguration
- Set security headers (see `secure-coding.md`)
- Disable default credentials, debug mode, directory listing in prod
- Remove unused features, endpoints, dependencies
- Patch and upgrade on a cadence
- Set error pages to generic messages (no stack trace leaks)

### A06 — Vulnerable Components
```bash
npm audit --audit-level=high                  # Node
pip-audit                                     # Python
cargo audit                                   # Rust
gosec ./... && govulncheck ./...              # Go
trivy fs --security-checks vuln .             # all languages
snyk test                                     # commercial
```
- Subscribe to security advisories for your dependencies
- Pin transitive dependencies; use a lockfile
- Generate an SBOM (`npm sbom`, `cyclonedx-py`)
- Have a patch SLA: Critical 24h, High 7d, Medium 30d

### A07 — Auth Failures
- Password requirements: ≥12 chars, mixed case, digit, symbol; check against breach lists (`HaveIBeenPwned` API)
- MFA via TOTP (authenticator apps) or WebAuthn
- Account lockout after 5 failed attempts (15-min window)
- Session cookies: `httpOnly`, `secure`, `sameSite=strict`, short `maxAge`
- JWT: short-lived access tokens (15m), long-lived refresh tokens (7d), allowlist algorithm, verify `iss`/`aud`/`exp`
- Generic error messages: "Invalid credentials" — never reveal whether email exists

### A08 — Data Integrity Failures
- No `pickle.loads` on untrusted input — use `json.loads` with a schema
- No `eval` on user input
- Verify CI/CD pipeline signatures (Sigstore, SLSA)
- Sign software updates; verify signatures on install
- Validate deserialised data against a strict schema (Zod, pydantic, JSON Schema)

### A09 — Logging & Monitoring Failures
Log security events:
- Successful + failed authentication
- Privilege escalation attempts
- Input validation failures (especially SQL/XSS payload-shaped)
- Access control denials
- Configuration changes
- High-value transactions

Don't log:
- Passwords, even hashed
- Full session tokens
- PII beyond what's needed for forensics
- Credit card numbers (PCI DSS forbids)

Ship logs to a central, tamper-evident store (SIEM). Alert on anomalies.

### A10 — SSRF
```typescript
async function safeFetch(url: string) {
  const parsed = new URL(url);
  if (!['http:', 'https:'].includes(parsed.protocol)) throw new Error('Bad protocol');
  const ips = await dns.lookup(parsed.hostname, { all: true });
  for (const { address } of ips) {
    if (isPrivateIp(address)) throw new Error('Internal IP blocked');
  }
  return await fetch(url, { redirect: 'manual' });   // no redirect following
}
```
Block cloud metadata IPs (169.254.169.254) explicitly.

## STRIDE Threat Modeling

| Letter | Threat | Example | Mitigation |
|---|---|---|---|
| **S** | Spoofing | Attacker pretends to be another user | Strong auth, MFA, mutual TLS |
| **T** | Tampering | Attacker modifies data in transit or at rest | TLS, signatures, integrity checks |
| **R** | Repudiation | User denies an action they took | Audit logs with user ID + timestamp; signed logs |
| **I** | Information disclosure | Sensitive data leaks to unauthorised party | Encryption, access control, redacted logs |
| **D** | Denial of service | Attacker floods service | Rate limiting, autoscaling, circuit breakers |
| **E** | Elevation of privilege | User gains admin rights | Least privilege, RBAC, server-side authorisation |

### Process
1. Draw a data flow diagram (DFD) of the feature
2. Walk each element and data flow with STRIDE; list threats per category
3. Rate each threat by likelihood × impact
4. Pick mitigations; document accepted risks in an ADR
5. Re-run when the architecture changes

## SAST / DAST / SCA Tooling

### SAST — analysing source code
| Tool | Languages | Strengths |
|---|---|---|
| **semgrep** | All | Custom rules, OWASP ruleset; default choice |
| **bandit** | Python | Low false-positive rate |
| **eslint-plugin-security** | JS/TS | Catches `eval`, regex DoS, `child_process` |
| **gosec** | Go | Integrates with `govulncheck` |
| **Brakeman** | Ruby/Rails | Rails-aware |
| **SonarQube** | All | Self-hosted, CI-integrated |

```bash
semgrep --config auto .                                  # everything
semgrep --config p/owasp-top-ten .
bandit -r src/ -f json -o bandit.json -ll               # Python, high+
npx eslint --ext .js,.ts . --plugin security
gosec -fmt=json -out=gosec.json ./...
```

### DAST — attacking a running app
| Tool | Use |
|---|---|
| **OWASP ZAP** | Web app scanner; proxy + active scan |
| **Burp Suite** | Manual pentest proxy |
| **sqlmap** | SQL injection automation |
| **nuclei** | Template-based CVE/misconfig scanning |
| **nmap** | Network service discovery |
| **ffuf / gobuster** | Directory + vhost fuzzing |

### SCA — dependency vulnerabilities
| Tool | Ecosystem |
|---|---|
| `npm audit --audit-level=high` | Node |
| `pip-audit` | Python |
| `cargo audit` | Rust |
| `govulncheck` | Go (only vulnerable code paths) |
| `trivy fs` | All + containers + IaC |
| `snyk test` | Commercial, prioritised |

### All-in-one
```bash
trivy fs --security-checks vuln,secret,config .
```

## Secret Scanning

| Tool | Best for | Notes |
|---|---|---|
| **gitleaks** | Git history + working tree | Fast; pre-commit hook available |
| **trufflehog** | Deep scanning | Verifies secrets by hitting the API |
| **GitHub Secret Scanning** | GitHub repos | Auto-push protection |
| **GitGuardian** | Commercial | SaaS dashboards |

### Common secret patterns
| Type | Pattern |
|---|---|
| AWS Access Key | `AKIA[0-9A-Z]{16}` |
| GitHub Token | `ghp_[A-Za-z0-9]{36}` |
| Slack Token | `xox[baprs]-[A-Za-z0-9-]+` |
| Stripe Key | `sk_live_[A-Za-z0-9]{24}` |
| Private Key | `-----BEGIN [A-Z ]*PRIVATE KEY-----` |
| JWT | `eyJ[A-Za-z0-9_-]*\.eyJ[A-Za-z0-9_-]*\.` |
| Google API Key | `AIza[0-9A-Za-z_-]{35}` |

### Remediation when a secret is found in git history
1. **Rotate immediately** — assume it's compromised
2. **Remove from history** — `git filter-repo --replace-text passwords.txt` (preferred) or BFG; force-push and notify collaborators
3. **Add to `.gitignore`** to prevent re-commit
4. **Move to env vars or a secret manager** (Vault, AWS Secrets Manager, doppler)

### Pre-commit hook
```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/gitleaks/gitleaks
    rev: v8.18.0
    hooks:
      - id: gitleaks
```

## Vulnerability Patterns Quick Reference

| Vulnerability | Input vector | Prevention |
|---|---|---|
| SQL injection | Query params, form fields | Parameterised queries / ORM |
| XSS (reflected/stored/DOM) | User content | Output encoding; CSP; sanitise with DOMPurify |
| Path traversal | File paths | `path.basename` + resolve check |
| Command injection | Shell args | `execFile` with array args; no shell |
| IDOR | Resource IDs | Authorisation check on every object access |
| Insecure deserialisation | Serialised data | JSON only; schema-validate; no `pickle`/`eval` |
| CSRF | Cross-site POST | SameSite cookies; synchronizer token |
| SSRF | URLs | Allowlist; block internal IPs; no redirect following |
| Open redirect | URLs | Validate `next` param against allowlist |
| Mass assignment | JSON body | Allowlist fields; DTOs |
| Sensitive data exposure | Logs, error responses | Redact; generic errors |
| XXE | XML input | Disable DTDs; use JSON |

## Verification Gate

Before merging a security-sensitive change:
- [ ] `semgrep --config auto .` — no new Critical/High findings
- [ ] `bandit -r src/ -ll` / `eslint --plugin security` / `gosec ./...` — clean
- [ ] `npm audit --audit-level=high` / `pip-audit` / `cargo audit` — no new High/Critical CVEs
- [ ] `gitleaks detect --source .` — zero secrets in diff
- [ ] `trivy fs --security-checks vuln,secret,config .` — clean
- [ ] Manual OWASP review of auth, input, crypto, access control — done
- [ ] STRIDE walked for new attack surface — done
- [ ] Findings documented with CWE + CVSS + remediation

## Rules of Engagement (for active/pentest testing)

1. **Scope verification** — only test authorised targets; get written sign-off
2. **Time windows** — respect testing hours
3. **DoS prevention** — avoid resource exhaustion
4. **Data handling** — don't exfiltrate real data
5. **Stop on discovery** — don't exploit beyond proof of concept
6. **Immediate reporting** — critical findings reported ASAP
7. **Documentation** — record all actions
8. **Cleanup** — remove test artefacts

## Quick Reference

| Need | Tool |
|---|---|
| Multi-language SAST | `semgrep --config auto .` |
| Dependency scan | `npm audit` / `pip-audit` / `cargo audit` / `govulncheck` |
| All-in-one | `trivy fs --security-checks vuln,secret,config .` |
| Secret scan | `gitleaks detect --source .` |
| Web DAST | OWASP ZAP, Burp Suite |
| Threat model | STRIDE the DFD |

See `references/tooling-and-pentest.md` for the full pentest methodology, per-language toolchain, and infrastructure scanning.
