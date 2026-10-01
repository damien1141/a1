# Spec Mining

Reverse-engineer specifications from existing code. Use when the codebase predates the team, when docs are missing or stale, when onboarding, or when planning changes to a feature whose official spec and actual behavior have diverged.

## The two hats

Mine specs wearing two perspectives, and label every observation with which hat produced it:

- **Arch Hat** — system architecture, data flow, module boundaries, technology stack. Concerned with *how* the system is built.
- **QA Hat** — observable behavior, edge cases, error responses, state transitions. Concerned with *what* the system does.

A good mined spec interleaves both: "OBSERVED: `POST /users` creates a row in `users` table (Arch, users.py:88) and returns 201 with the new ID (QA, users.py:92)."

## The 5-phase process

### Phase 1 — Scope

Identify the analysis boundary. "The whole system" is rarely tractable; pick a feature, a module, or a vertical slice.

```text
Scope: "Authentication and authorization" — covers auth/, guards/, middleware/jwt.ts.
Out of scope: user management CRUD, password reset email templates.
```

Write the scope down. You will need it when the analysis threatens to sprawl.

### Phase 2 — Explore (mechanical)

Use Glob and Grep systematically. Don't read code yet — just map the territory.

```bash
# Entry points
Glob: **/main.{ts,js,py,go}
Glob: **/app.{ts,js,py}
Glob: **/index.{ts,js}

# Routes / controllers
Glob: **/routes/**/*.{ts,js}
Glob: **/controllers/**/*.{ts,js,py}
Grep: @Controller|@Get|@Post|router\.|app\.get|app\.post

# Data models
Glob: **/models/**/*.{ts,js,py}
Glob: **/schema*.{ts,js,py,sql}
Glob: **/migrations/**
Grep: @Entity|class.*Model|schema\s*=|CREATE TABLE

# Business logic
Glob: **/services/**/*
Grep: async.*function|export.*class|def .+\(

# Auth & security
Glob: **/auth/**/*
Glob: **/guards/**/*
Grep: @Guard|middleware|passport|jwt|bcrypt|argon2

# External integrations
Grep: fetch\(|axios\.|HttpService|requests\.|http\.Client
Glob: **/integrations/**/*
Glob: **/clients/**/*

# Configuration
Glob: **/*.config.{ts,js}
Glob: **/.env*
Glob: **/config/**/*
Grep: os\.environ|config\[|settings\.|ConfigService|process\.env

# Technical debt markers
Grep: TODO|FIXME|HACK|XXX|@deprecated

# Tests reveal behavior the code doesn't document
Glob: **/*.spec.{ts,js}
Glob: **/*.test.{ts,js}
Glob: **/test_*.py
Glob: **/*_test.go
```

**Coverage checkpoint before reading:** do you have entry points, routes, models, auth, config, and tests mapped? If not, keep grepping. Reading code without a map wastes effort.

### Phase 3 — Trace (read and follow data flow)

Now read. Follow a single request or operation end-to-end: entry → validation → service → repository → external call → response. Cite `file:line` for every observation.

```text
POST /users
  → routes/users.ts:14       (route definition)
  → validators/user.dto.ts:8 (Zod schema: name, email required; role optional)
  → controllers/users.ts:23  (handler)
  → services/userService.ts:11 (createUser: hashes password with bcrypt rounds=12)
  → repositories/userRepo.ts:9 (INSERT INTO users ...)
  → services/emailService.ts:30 (queue welcome email)
  → controllers/users.ts:35 (return 201 with new user)
```

### Phase 4 — Document in EARS

Write each observed behavior in EARS format. EARS makes "the system does X" unambiguous.

**EARS patterns:**

| Type | Pattern | Use When |
|------|---------|----------|
| Ubiquitous | `The system shall <action>.` | Always true |
| Event-driven | `When <trigger>, the system shall <action>.` | Triggered by an event |
| State-driven | `While <state>, the system shall <action>.` | Continuous state |
| Conditional | `While <state>, when <trigger>, the system shall <action>.` | State + trigger (most common) |
| Optional | `Where <feature> is enabled, the system shall <action>.` | Feature-flagged |

**Observed examples:**

```markdown
**OBS-AUTH-001**: Login Flow
While credentials are valid, when POST /auth/login is called,
the system shall return a JWT access token (15min TTL) and refresh token (7d TTL).
[Source: auth/controller.ts:42, auth/service.ts:18]

**OBS-AUTH-002**: Invalid Login
When invalid credentials are provided,
the system shall return 401 and increment the failed-login counter.
[Source: auth/controller.ts:51, auth/service.ts:24]

**OBS-AUTH-003**: Account Lockout
While failed-login count exceeds 5, when login is attempted,
the system shall reject the attempt and require password reset.
[Source: auth/service.ts:30-35, middleware/lockout.ts:8]

**OBS-USER-001**: Email Validation
When email format is invalid,
the system shall return 400 with `{"error": "Invalid email format"}`.
[Source: validators/user.dto.ts:14, controllers/users.ts:28]
```

### Phase 5 — Flag uncertainties

Not everything is observable. Mark what you couldn't verify, and pose the question that would resolve it.

```markdown
## Uncertainties
- [ ] What triggers order status transition from `pending` → `processing`? Code has
      no explicit call site; suspected side-effect of payment webhook (unverified).
- [ ] Is soft delete implemented for users? `users` table has `deleted_at` column
      but no query filters on it. Either dead column or undocumented behavior.
- [ ] What external APIs are called? Found `fetch("https://api.stripe.com/...")` in
      payments/service.ts but no list of all external dependencies.
- [ ] Are there background jobs? No `@Cron`/`Bull`/`Queue` patterns found, but
      `worker.js` exists at repo root — purpose unclear.
```

## Mined spec template

Save as `specs/{project_name}_reverse_spec.md`.

```markdown
# Reverse-Engineered Specification: [System/Feature Name]

## Overview
[2-3 sentence summary based on Phase 1-3 findings]

## Architecture Summary

### Technology Stack
- **Language**: TypeScript 5.x
- **Framework**: NestJS 10.x
- **Database**: PostgreSQL 15
- **ORM**: Prisma 5.x
- **Auth**: JWT (RS256) + bcrypt (12 rounds)

### Module Structure
```
src/
├── auth/         # Authentication (JWT, guards)
├── users/        # User CRUD
├── orders/       # Order processing
└── common/       # Shared utilities
```

### Data Flow
```
Request → Guard → Controller → Service → Repository → Database
                                     ↓
                              External APIs (Stripe)
```

## Observed Functional Requirements (EARS)

### Authentication
**OBS-AUTH-001**: [EARS requirement] — [file:line]

### User Management
**OBS-USER-001**: [EARS requirement] — [file:line]

## Observed Non-Functional Requirements

### Security
- JWT signed with RS256 (config/jwt.ts:5)
- Passwords hashed with bcrypt, 12 rounds (auth/service.ts:20)
- Rate limiting: 100 req/min per IP (main.ts:33)

### Performance
- DB connection pool: 10 (config/database.ts:12)
- Response timeout: 30s (main.ts:40)
- Pagination: default 20, max 100 (controllers/users.ts:15)

### Error Handling
| Code | Condition | Response |
|------|-----------|----------|
| 400 | Validation failure | `{ error: string, details: object }` |
| 401 | Invalid/missing token | `{ error: "Unauthorized" }` |
| 404 | Resource not found | `{ error: "Not found" }` |
| 500 | Unhandled error | `{ error: "Internal server error" }` |

## Inferred Acceptance Criteria

### AC-001: [Feature]
Given [precondition observed in code]
When [action that triggers the path]
Then [observed response]

## Uncertainties and Questions
- [ ] [Question with what you tried]

## Recommendations
1. [Gap found during mining, e.g. "Missing input validation on PATCH /users/:id"]
2. [Dead code or stale column]
3. [Suggested follow-up: interview original author, check git blame]
```

## Analysis checklist (run before declaring the spec complete)

```text
[ ] All endpoints documented (routes mapped, methods, paths, auth)
[ ] All data models mapped (fields, types, relationships, indexes)
[ ] Authentication flow traced end-to-end
[ ] Authorization rules documented (who can do what)
[ ] Error responses documented (status code, body shape, trigger)
[ ] External dependencies listed (services called, why, contract)
[ ] Background jobs/cron identified (or confirmed absent)
[ ] Configuration enumerated (env vars, config files, defaults)
[ ] Uncertainties flagged with proposed resolution
[ ] Every observation has a file:line citation
```

## Discipline: observed vs inferred

The single most important rule in spec mining: **never present an inference as an observation.** If you didn't see it in the code, label it.

```markdown
OBSERVED: POST /users returns 201 with new user body. [users.ts:35]
INFERRED: The 201 response is the contract for all creation endpoints. (Pattern
          matches POST /orders and POST /products, but not documented anywhere.)
UNCERTAIN: Whether 201 is intentional or accidental consistency.
```

A reviewer reading OBSERVED trusts it and can verify. A reviewer reading INFERRED knows to challenge. A reviewer reading UNCERTAIN knows to ask. Blurring the labels defeats the entire exercise — the mined spec becomes another source of drift instead of a source of truth.
