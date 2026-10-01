# Secure Coding Patterns

Implementation patterns for the OWASP Top 10 mitigations. Loaded when the review identifies a security fix to recommend, or when implementing secure code from scratch.

## Password Hashing

### bcrypt (Node)
```typescript
import bcrypt from 'bcrypt';
const SALT_ROUNDS = 12;   // ≥10; 12 balances security vs perf

async function hashPassword(plaintext: string): Promise<string> {
  return bcrypt.hash(plaintext, SALT_ROUNDS);
}
async function verifyPassword(plaintext: string, hash: string): Promise<boolean> {
  return bcrypt.compare(plaintext, hash);   // constant-time
}
```

### argon2 (Python, recommended for new code)
```python
from argon2 import PasswordHasher
ph = PasswordHasher()                       # argon2id by default
hash = ph.hash(password)
try:
    ph.verify(hash, password)               # raises VerifyMismatchError on bad
except Exception:
    return False
```

### Password policy
```typescript
function validatePassword(password: string): { valid: boolean; errors: string[] } {
  const errors: string[] = [];
  if (password.length < 12) errors.push('Minimum 12 characters');
  if (!/[a-z]/.test(password)) errors.push('Requires lowercase');
  if (!/[A-Z]/.test(password)) errors.push('Requires uppercase');
  if (!/\d/.test(password)) errors.push('Requires digit');
  if (!/[@$!%*?&]/.test(password)) errors.push('Requires special character');
  return { valid: errors.length === 0, errors };
}
```
Check against breach lists (HaveIBeenPwned API) at signup.

## JWT

```typescript
import jwt from 'jsonwebtoken';

const JWT_SECRET = process.env.JWT_SECRET!;   // never hardcode
const ACCESS_TOKEN_EXPIRY = '15m';
const REFRESH_TOKEN_EXPIRY = '7d';

function generateAccessToken(userId: string): string {
  return jwt.sign({ sub: userId, type: 'access' }, JWT_SECRET, {
    algorithm: 'HS256',
    expiresIn: ACCESS_TOKEN_EXPIRY,
    issuer: 'your-app',
    audience: 'your-app',
  });
}

function verifyToken(token: string): jwt.JwtPayload {
  const payload = jwt.verify(token, JWT_SECRET, {
    algorithms: ['HS256'],          // allowlist algorithm — prevents alg=none attacks
    issuer: 'your-app',
    audience: 'your-app',
  });
  if (typeof payload === 'string') throw new Error('Invalid payload');
  return payload;
}
```

### JWT anti-patterns
- Don't store JWTs in `localStorage` (XSS steals them) — use `httpOnly` cookie
- Don't use `alg: none` — ever
- Don't put sensitive data in the payload (it's base64, not encrypted)
- Don't use long-lived access tokens; use refresh tokens for sessions
- Don't share the secret across services; use asymmetric (RS256) for multi-service

## Parameterized SQL

```typescript
// ❌ NEVER
db.query(`SELECT * FROM users WHERE email = '${email}'`);

// ✅ Positional parameters
const { rows } = await pool.query(
  'SELECT id, email, role FROM users WHERE email = $1',
  [email]
);

// ✅ ORM
const user = await prisma.user.findUnique({ where: { email } });

// ✅ Query builder
const user = await knex('users').where({ email }).first();
```

```python
# psycopg2 / psycopg3
cursor.execute("SELECT * FROM users WHERE email = %s", (email,))

# SQLAlchemy
session.query(User).filter_by(email=email).first()
```

## Input Validation

### Zod (TypeScript)
```typescript
import { z } from 'zod';

const LoginSchema = z.object({
  email: z.string().email().max(254),
  password: z.string().min(8).max(128),
});

export function validateLoginInput(raw: unknown) {
  const result = LoginSchema.safeParse(raw);
  if (!result.success) {
    throw new Error('Invalid credentials format');   // generic — never echo raw
  }
  return result.data;
}
```

### pydantic v2 (Python)
```python
from pydantic import BaseModel, EmailStr, Field

class LoginIn(BaseModel):
    email: EmailStr
    password: str = Field(min_length=8, max_length=128)
```

### Path traversal prevention
```typescript
import path from 'path';

function getSecurePath(baseDir: string, userInput: string): string {
  const sanitized = path.basename(userInput);          // strip ../
  const fullPath = path.resolve(baseDir, sanitized);
  if (!fullPath.startsWith(path.resolve(baseDir))) {
    throw new Error('Invalid path');
  }
  return fullPath;
}
```

### URL validation
```typescript
function validateUrl(input: string, allowedHosts: string[]): URL {
  const url = new URL(input);
  if (!['http:', 'https:'].includes(url.protocol)) throw new Error('Invalid protocol');
  if (!allowedHosts.includes(url.hostname)) throw new Error('Host not allowed');
  return url;
}
```

### File upload validation
```typescript
const ALLOWED_TYPES = ['image/jpeg', 'image/png', 'image/gif'];
const MAX_SIZE = 5 * 1024 * 1024;

function validateUpload(file: Express.Multer.File) {
  if (!ALLOWED_TYPES.includes(file.mimetype)) throw new Error('Invalid file type');
  if (file.size > MAX_SIZE) throw new Error('File too large');
  // Verify magic bytes, not just extension
  const type = fileTypeFromBuffer(fs.readFileSync(file.path));
  if (!type || !ALLOWED_TYPES.includes(type.mime)) throw new Error('Invalid content');
}
```

## XSS Prevention

### Output encoding (React auto-escapes)
```typescript
function SafeComponent({ userInput }: { userInput: string }) {
  return <div>{userInput}</div>;   // safe — auto-escaped
}

// If you must render HTML, sanitise first
import DOMPurify from 'dompurify';
function HtmlContent({ html }: { html: string }) {
  return <div dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(html) }} />;
}
```

### Don't use innerHTML / document.write
```typescript
// ❌
element.innerHTML = userInput;
document.write(userInput);

// ✅
element.textContent = userInput;
```

### Strict DOMPurify
```typescript
const clean = DOMPurify.sanitize(dirty, {
  ALLOWED_TAGS: ['b', 'i', 'em', 'strong', 'a'],
  ALLOWED_ATTR: ['href'],
});
const textOnly = DOMPurify.sanitize(dirty, { ALLOWED_TAGS: [] });
```

## CSRF Prevention

### SameSite cookies (modern default)
```typescript
res.cookie('session', token, {
  httpOnly: true,
  secure: true,
  sameSite: 'strict',   // or 'lax' for top-level GETs
  maxAge: 900000,
});
```

### Synchronizer token
```typescript
// Set CSRF cookie (readable by JS for the header)
res.cookie('csrf', token, { httpOnly: false, secure: true, sameSite: 'strict' });

// Client sends in header
fetch('/api/action', {
  method: 'POST',
  headers: { 'X-CSRF-Token': getCookie('csrf') },
});

// Server validates
if (req.cookies.csrf !== req.headers['x-csrf-token']) {
  return res.status(403).json({ error: 'CSRF validation failed' });
}
```

## Security Headers

### Helmet (Express)
```typescript
import helmet from 'helmet';
app.use(helmet());   // sensible defaults

// Or configure individually
app.use(helmet({
  contentSecurityPolicy: {
    directives: {
      defaultSrc: ["'self'"],
      scriptSrc: ["'self'"],
      styleSrc: ["'self'", "'unsafe-inline'"],
      imgSrc: ["'self'", "data:", "https:"],
      connectSrc: ["'self'", "https://api.example.com"],
      objectSrc: ["'none'"],
      frameSrc: ["'none'"],
      upgradeInsecureRequests: [],
    },
  },
  hsts: { maxAge: 31536000, includeSubDomains: true, preload: true },
}));
```

### Manual headers
```typescript
res.setHeader('X-Frame-Options', 'DENY');
res.setHeader('X-Content-Type-Options', 'nosniff');
res.setHeader('Strict-Transport-Security', 'max-age=31536000; includeSubDomains');
res.setHeader('Referrer-Policy', 'strict-origin-when-cross-origin');
res.setHeader('Permissions-Policy', 'geolocation=(), microphone=(), camera=()');
```

| Header | Value | Purpose |
|---|---|---|
| Content-Security-Policy | `default-src 'self'` | XSS mitigation |
| X-Frame-Options | `DENY` | Clickjacking |
| X-Content-Type-Options | `nosniff` | MIME sniffing |
| Strict-Transport-Security | `max-age=31536000; includeSubDomains` | Force HTTPS |
| Referrer-Policy | `strict-origin-when-cross-origin` | Privacy |
| Permissions-Policy | `geolocation=(), camera=()` | Lock down APIs |

## CORS

```typescript
import cors from 'cors';

// Strict — explicit allowlist
app.use(cors({
  origin: ['https://example.com', 'https://app.example.com'],
  methods: ['GET', 'POST', 'PUT', 'DELETE'],
  allowedHeaders: ['Content-Type', 'Authorization'],
  credentials: true,
  maxAge: 86400,
}));

// Dynamic origin validation
app.use(cors({
  origin: (origin, callback) => {
    const allowed = ['https://example.com'];
    if (!origin || allowed.includes(origin)) callback(null, true);
    else callback(new Error('Not allowed by CORS'));
  },
}));
```

Never use `origin: '*'` with `credentials: true` — browsers reject it. Use an explicit allowlist.

## Rate Limiting

```typescript
import rateLimit from 'express-rate-limit';

// General API
const apiLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 100,
  standardHeaders: true,
  legacyHeaders: false,
});
app.use('/api/', apiLimiter);

// Strict for auth
const authLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 5,
  skipSuccessfulRequests: true,   // don't count successes
  message: { error: 'Too many attempts' },
});
app.post('/api/login', authLimiter, loginHandler);
```

## Account Lockout

```typescript
const MAX_ATTEMPTS = 5;
const LOCKOUT_MS = 15 * 60 * 1000;

async function handleLoginAttempt(email: string, success: boolean) {
  const key = `login:attempts:${email}`;
  if (success) { await redis.del(key); return; }
  const attempts = await redis.incr(key);
  await redis.expire(key, LOCKOUT_MS / 1000);
  if (attempts >= MAX_ATTEMPTS) {
    await redis.set(`login:locked:${email}`, '1', 'PX', LOCKOUT_MS);
    throw new Error('Account locked. Try again later.');
  }
}
```

## Secure Endpoint (Full Flow)

```typescript
import express from 'express';
import bcrypt from 'bcrypt';
import jwt from 'jsonwebtoken';
import rateLimit from 'express-rate-limit';
import helmet from 'helmet';

const app = express();
app.use(helmet());
app.use(express.json({ limit: '10kb' }));

const authLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 10,
  standardHeaders: true,
});

app.post('/api/login', authLimiter, async (req, res) => {
  // 1. Validate input (Zod)
  const { email, password } = validateLoginInput(req.body);

  // 2. Authenticate — parameterized query, constant-time compare
  const user = await getUserByEmail(email);
  if (!user || !(await bcrypt.compare(password, user.passwordHash))) {
    // Generic message — do not reveal whether email exists
    return res.status(401).json({ error: 'Invalid credentials' });
  }

  // 3. Authorize — issue scoped, short-lived token
  const token = jwt.sign(
    { sub: user.id, role: user.role },
    process.env.JWT_SECRET!,
    { algorithm: 'HS256', expiresIn: '15m', issuer: 'your-app', audience: 'your-app' }
  );

  // 4. Secure response — token in httpOnly cookie, not body
  res.cookie('token', token, { httpOnly: true, secure: true, sameSite: 'strict' });
  res.json({ message: 'Authenticated' });
});
```

## Sensitive Data Handling

```typescript
// ❌ Logs password
logger.info('User login', { email, password });

// ✅ Redact
logger.info('User login', { email, password: '[REDACTED]' });

// ❌ Error exposes internals
res.status(500).json({ error: err.stack });

// ✅ Generic error
res.status(500).json({ error: 'Internal server error' });
// Log the full stack server-side only
logger.error('Unhandled error', { stack: err.stack, path: req.path });
```

## Secrets Management

| Storage | When to use |
|---|---|
| Env vars | Local dev, simple services |
| `.env` (gitignored) | Same |
| Vault / AWS Secrets Manager / GCP Secret Manager | Production, rotation, audit |
| Sealed Secrets / External Secrets Operator | Kubernetes |
| doppler / dotenv-vault | Team secret sharing with rotation |

Never commit secrets to git. Use a pre-commit hook (`gitleaks`) to enforce.

## Quick Reference

| Need | Pattern |
|---|---|
| Password hash | bcrypt (12 rounds) or argon2id |
| JWT | Short-lived (15m) + refresh (7d); allowlist alg; httpOnly cookie |
| SQL | Parameterized or ORM |
| Input validation | Zod / pydantic / JSON Schema |
| Path safety | `path.basename` + resolve check |
| XSS | Output encoding (React auto-escapes); DOMPurify for HTML |
| CSRF | SameSite=strict + synchronizer token |
| Headers | helmet() defaults |
| CORS | Explicit allowlist, never `*` with credentials |
| Rate limit | 100/15min general, 5/15min auth |
| Secrets | Env (dev) / Vault (prod); gitleaks pre-commit |
