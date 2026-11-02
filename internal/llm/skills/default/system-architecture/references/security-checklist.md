# Fullstack Security Checklist

Layered security checklist for any feature crossing DB → API → frontend. Run through this list BEFORE writing code; sign off before release. Covers OWASP Top 10, input validation, output encoding, authn/authz, secrets, headers, and audit.

## The 12-point checklist

Run through every item for any feature that handles user data, money, or privileged operations:

1. **Authentication** — server-side, never client-trusted.
2. **Authorization** — per-resource, not just per-endpoint.
3. **Input validation** — server-side, schema-strict, fail-closed.
4. **Output encoding** — context-aware (HTML, JS, URL, CSS).
5. **SQL parameterization** — no string concatenation.
6. **Secrets management** — env vars / secret manager, never in source.
7. **Transport security** — HTTPS only, HSTS, secure cookies.
8. **Security headers** — CSP, X-Frame-Options, X-Content-Type-Options, etc.
9. **CSRF protection** — for cookie-auth, not for token-auth.
10. **Rate limiting + brute-force protection** — on auth, password reset, expensive endpoints.
11. **Logging + audit** — security events logged; sensitive data redacted.
12. **Dependency hygiene** — no known-vulnerable deps; pinned versions; SBOM.

## 1. Authentication

| Pattern | Rule |
|---|---|
| Password storage | bcrypt (12+ rounds) or argon2; never MD5/SHA1/plain |
| Password reset | Token expires in 1h, single-use, rotate on use, email to verified address |
| MFA | TOTP (RFC 6238) or WebAuthn; backup codes; never SMS as primary |
| Session (cookie) | `HttpOnly`, `Secure`, `SameSite=Lax` or `Strict`; rotate on login; revoke on logout |
| JWT | Short-lived access (15min) + refresh (7d); rotate refresh on use; blacklist on logout |
| OAuth2 / OIDC | Use library (`authlib`, `passport`); verify `state` and `nonce`; check `iss`/`aud` |
| API keys | Hash at rest; scoped; revocable; expiry; rotate-able |

**Never:** trust a `userId` field in the request body (use the authenticated session/token); issue a token without `exp` claim; allow login with email + password reset token (token is single-purpose); log passwords, tokens, or session IDs.

## 2. Authorization

| Pattern | Rule |
|---|---|
| Per-endpoint | Auth required by default; opt-out with explicit annotation (`@Public`) |
| Per-resource | Owner check before returning/modifying: `if obj.owner_id != user.id: 403` |
| Role-based | `admin`, `user`, `viewer` — explicit role list per endpoint |
| Attribute-based | Fine-grained: `can_edit_published`, `can_delete_others` — checked per object |
| Tenant isolation | `tenant_id` in every query; verified against user's tenant |
| Defense in depth | Authn at gateway, authz at controller, owner check at service, constraint at DB |

```python
# FastAPI — defense in depth
@router.delete("/articles/{article_id}")
async def delete_article(article_id: int,
    user: Annotated[User, Security(get_current_user, scopes=["write"])],   # authn + scope
    db: Annotated[AsyncSession, Depends(get_db)]):
    article = await db.get(Article, article_id)
    if not article: raise HTTPException(404)
    if article.author_id != user.id and "admin" not in user.scopes:        # authz
        raise HTTPException(403)
    await db.delete(article)
```

**Authorization flaws to check:** IDOR (Insecure Direct Object Reference — `/api/orders/123` works for any logged-in user, even if order 123 belongs to someone else); missing function-level check (admin endpoints protected only by hiding the UI link); forceful browsing (`/admin/dashboard` accessible to non-admins because the route has no guard).

## 3. Input validation

| Pattern | Rule |
|---|---|
| Schema validation | Pydantic v2 / class-validator / Zod — strict, fail-closed on unknown fields |
| Type coercion | Explicit; reject mismatches (e.g., `?id=abc` when `int` expected) |
| Length limits | Min + max on every string; reject oversized payloads (`Content-Length` limit) |
| Range limits | Numeric ranges; reject out-of-range |
| Enum validation | Whitelist of allowed values |
| File upload | Validate MIME type + magic bytes; limit size; scan for malware if high-risk |
| Path traversal | Reject `..` in user-supplied paths; never concat user input into filesystem paths |
| SSRF | Whitelist outbound hosts; reject private IP ranges; never let users control target URLs |

```python
# Reject unknown fields
class UserCreate(BaseModel):
    model_config = ConfigDict(extra="forbid")          # critical: prevents mass assignment
    email: EmailStr
    password: str = Field(min_length=8, max_length=128)
    role: UserRole = UserRole.USER                       # not settable by client (no field)
```

**Mass assignment** is the classic failure: client sends `{"email":"x","role":"admin"}`, DTO accepts both, user gets admin. `extra="forbid"` (Pydantic) or `whitelist + forbidNonWhitelisted` (class-validator) blocks it.

## 4. Output encoding

| Context | Encoding |
|---|---|
| HTML body / attribute | Templating engine's auto-escape (`&lt;` `&gt;` `&amp;` `&quot;` `&#39;`) |
| JavaScript string | `\xHH` for `<` `>` `"` `'` `\` `/` `\n` `\r` |
| URL | `encodeURIComponent` (component) or `encodeURI` (full URL); reject `javascript:` scheme |
| CSS / JSON | `\HHHHHH` (CSS); `JSON.stringify` + reject `<` `>` for JSONP contexts |

**Never** use `dangerouslySetInnerHTML` (React), `v-html` (Vue), `[innerHTML]` (Angular), or `Markup()` (Jinja) with user data. If unavoidable, sanitize with DOMPurify first.

## 5. SQL parameterization

```python
cursor.execute("SELECT id, email FROM users WHERE id = %s", (user_id,))     # RIGHT — parameterized
cursor.execute(f"SELECT id, email FROM users WHERE id = {user_id}")         # WRONG — SQL injection
User.objects.filter(id=user_id)                                            # ORM (always parameterized)
```

Audit: `grep -rn "execute.*f'\|execute.*\".*+\|execute.*%" .` — flag every match. ORM raw query methods (`raw()`, `text()`, `cursor.execute()`) are the usual injection points.

## 6. Secrets management

| Source | When |
|---|---|
| Environment variables | Dev, simple prod |
| Secret manager (AWS Secrets Manager, Vault, GCP Secret Manager) | Prod, multi-tenant, rotated secrets |
| IAM role (no keys in code) | On EC2/ECS/Lambda with attached role |
| Mounted volume (K8s secret) | K8s pods |

**Never:** hardcode secrets in source; commit `.env` files; log secrets (redact in logger config); put secrets in client-side code (frontend bundles are public); reuse a secret across environments.

Audit: `git log --all -p | grep -iE "(password|secret|api_key|token).{0,5}=" | head`. Anything committed historically is compromised; rotate it.

## 7. Transport security

HTTPS only (redirect HTTP → HTTPS); HSTS (`max-age=31536000; includeSubDomains; preload`); TLS 1.2+ only; cookies `Secure` + `HttpOnly` for sessions; CORS explicit origin whitelist, never `*` with `credentials: true`.

## 8. Security headers

| Header | Value | Purpose |
|---|---|---|
| `Content-Security-Policy` | `default-src 'self'; script-src 'self' 'nonce-<random>'; object-src 'none'; base-uri 'none'` | XSS mitigation |
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains; preload` | Force HTTPS |
| `X-Frame-Options` | `DENY` or `SAMEORIGIN` | Clickjacking |
| `X-Content-Type-Options` | `nosniff` | MIME sniffing |
| `Referrer-Policy` | `no-referrer` or `strict-origin-when-cross-origin` | Referrer leakage |
| `Permissions-Policy` | `camera=(), microphone=(), geolocation=()` | Disable browser features |
| `Cross-Origin-Opener-Policy` | `same-origin` | Process isolation |
| `Cross-Origin-Embedder-Policy` | `require-corp` | Spectre mitigation |

Use `helmet` (Node) / `secure_headers` (Hono) / `django-csp` + `django-cors-headers` (Django) to set them.

## 9. CSRF protection

Required when using cookie-based auth. Not required for Bearer token auth (tokens aren't sent automatically by the browser).

| Pattern | How |
|---|---|
| CSRF token | Server issues token; client includes in `X-CSRF-Token` header; server verifies |
| SameSite cookie | `SameSite=Strict` (best) or `SameSite=Lax` (default) — blocks most CSRF |
| Double-submit cookie | Random value in cookie + header; server compares |
| Origin/Referer check | Verify `Origin` or `Referer` header matches expected host |

Django has built-in CSRF middleware; FastAPI requires manual setup (`starlette-csrf` or custom).

## 10. Rate limiting + brute-force protection

| Endpoint | Limit |
|---|---|
| Login | 5/min per IP + per email |
| Password reset | 3/hour per email |
| Sign-up | 10/min per IP |
| API key auth | 100/min per key (plan-dependent) |
| Public read endpoints | 1000/min per IP |
| Write endpoints | 100/min per user |

Return `429 Too Many Requests` with `Retry-After` header. After 5 failed logins, lock account for 15min (or require email reset).

## 11. Logging + audit

Log every security-relevant event: login success/failure (with email + IP); password reset request + completion; role/permission change; sensitive data access (admin viewing user PII); failed authorization (403s); rate limit hits; configuration changes.

```python
logger.info("login_failed", extra={"email": email, "ip": client_ip, "user_agent": ua})
# NEVER log passwords, tokens, session IDs, full card numbers
```

Redact in logger config: `redact: ["password", "*.password", "token", "authorization", "credit_card"]`. Retain logs ≥ 90 days (compliance may require 1-7 years). Ship to tamper-evident store (write-once, append-only).

## 12. Dependency hygiene

- `npm audit` / `pip-audit` / `yarn audit` / `govulncheck` in CI; fail on high/critical.
- Pin exact versions (`package-lock.json`, `requirements.txt` with `==`).
- Renovate / Dependabot for upgrade PRs.
- SBOM (`cyclonedx` / `spdx`) generated per release.
- License audit (GPL/AGPL may be incompatible with your product).
- Remove unused deps (attack surface reduction).

## OWASP Top 10 mapping

| OWASP risk | Checklist items |
|---|---|
| A01 Broken Access Control | 2 |
| A02 Cryptographic Failures | 1, 6, 7 |
| A03 Injection | 3, 4, 5 |
| A04 Insecure Design | All (defense in depth) |
| A05 Security Misconfiguration | 8, 12 |
| A06 Vulnerable Components | 12 |
| A07 Auth Failures | 1, 10 |
| A08 Software & Data Integrity | 12 |
| A09 Security Logging Failures | 11 |
| A10 SSRF | 3 |

## Three-perspective example

```python
# [Backend] — Authenticated route with parameterized query and scoped response
@router.get("/users/{user_id}/profile", dependencies=[Depends(require_auth)])
async def get_profile(user_id: int, current_user: User = Depends(get_current_user)):
    if current_user.id != user_id and "admin" not in current_user.scopes:
        raise HTTPException(403)               # 403 BEFORE any DB access — no timing leak via 404
    row = await db.fetchone("SELECT id, name, email FROM users WHERE id = $1", (user_id,))  # parameterized
    if not row: raise HTTPException(404)
    return ProfileResponse(**row)              # explicit schema — no password leakage

# [Frontend]
async function fetchProfile(userId: number) {
  if (!Number.isInteger(userId) || userId <= 0) throw new Error("Invalid user ID");  // client guard (UX, not security)
  const res = await apiFetch(`/users/${userId}/profile`);                            // attaches auth header
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}
```

**[Security]:** auth enforced server-side via `require_auth` (client header is convenience, not the gate); `ProfileResponse` schema excludes sensitive fields; 403 before DB access (no timing leak); parameterized query prevents SQL injection; client-side guard is UX, not security.

## Verification gates

- [ ] All 12 checklist items addressed (or explicitly N/A with rationale).
- [ ] `npm audit` / `pip-audit` clean (or known-acceptable exceptions documented).
- [ ] Security headers verified via `securityheaders.com` or `curl -I`.
- [ ] Auth flow tested: unauth → 401; wrong user → 403; correct user → 200; expired token → 401.
- [ ] IDOR test: try to access another user's resource → 403.
- [ ] SQL injection test: `?id=1 OR 1=1` → 422 or sanitized.
- [ ] XSS test: submit `<script>alert(1)</script>` → encoded, not executed.
- [ ] CSRF test: cross-origin POST without token → 403.
- [ ] Rate limit test: 6th login attempt → 429.
- [ ] Audit log: login events, 403s, password changes logged; no secrets in logs.
