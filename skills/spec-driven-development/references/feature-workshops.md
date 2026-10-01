# Feature Workshops

Structured requirements gathering for new features. Use when defining a feature from scratch, gathering requirements before implementation, or writing a specification that another engineer will build from.

## The two hats

Run the workshop wearing two perspectives and switch deliberately:

- **PM Hat** — user value, business goals, success metrics, scope. Asks "who, what, why."
- **Dev Hat** — technical feasibility, security, performance, edge cases, integration. Asks "how, what-if, what-when-broken."

A workshop that stays in one hat produces half a spec. PM-only specs miss security and edge cases. Dev-only specs miss user value and scope boundaries. Switch explicitly; mark which hat produced each section.

## The 5-phase workshop

### Phase 1 — Discover (problem statement)

Open-ended questions first. Don't propose solutions; understand the problem.

```text
- Tell me about this feature in your own words.
- What problem are we solving? Who experiences it? How often?
- What does success look like? How will we measure it?
- What's in scope? What's explicitly out of scope?
```

Then narrow with structured choices: target user (single select), priority (must / should / nice-to-have), scope (MVP / full / phased).

**Output of Phase 1:** problem statement, target user, priority, scope.

```markdown
## Problem Statement
Users cannot export their data for compliance (GDPR Article 20). Support
receives ~15 requests/week, each handled manually at ~30min/request.

## Target User
End user (consumer), self-serve via account settings.

## Priority
Must-have (regulatory deadline Q3).

## Scope
MVP: single-user export, JSON + CSV, last 24 months.
Out of scope: org-wide export, scheduled exports, exports older than 24 months.
```

### Phase 2 — User stories

Write stories from the user's perspective. Each story is a single user, a single goal, a single value.

```markdown
### US-001: Self-serve data export
As a registered user,
I want to download my account data,
So that I can comply with GDPR data portability.

### US-002: Choose export format
As a user exporting my data,
I want to choose between JSON and CSV,
So that I can use the format my tooling supports.

### US-003: Be notified when large export is ready
As a user with a large account history,
I want to receive an email when my export is ready,
So that I don't have to keep a browser tab open.
```

### Phase 3 — Functional requirements (EARS)

Convert each story into one or more EARS requirements. EARS = Easy Approach to Requirements Syntax.

**EARS patterns:**

| Type | Pattern | Example |
|------|---------|---------|
| Ubiquitous | `The system shall <action>.` | The system shall encrypt all passwords using bcrypt. |
| Event-driven | `When <trigger>, the system shall <action>.` | When the user clicks Export, the system shall queue an export job. |
| State-driven | `While <state>, the system shall <action>.` | While a job is running, the system shall display a progress indicator. |
| Conditional | `While <state>, when <trigger>, the system shall <action>.` | While the user is logged in, when they click Export, the system shall queue an export job scoped to their account. |
| Optional | `Where <feature> is enabled, the system shall <action>.` | Where two-factor auth is enabled, the system shall require a verification code before starting an export. |

**Workshop output:**

```markdown
## Functional Requirements

### FR-EXP-001: Initiate Export
While the user is logged in, when they click "Export my data",
the system shall create an export job and display a progress page.

### FR-EXP-002: Format Selection
While the user is initiating an export, when they select a format (JSON or CSV),
the system shall include only that format in the resulting download.

### FR-EXP-003: Export Scope
When an export job runs,
the system shall include all user data from the last 24 months.

### FR-EXP-004: Notification
When an export job completes,
the system shall send an email to the user with a download link valid for 24 hours.

### FR-EXP-005: Auth on Download
While the download link is valid, when the user clicks it,
the system shall require re-authentication before serving the file.

### FR-EXP-006: Link Expiry
When a download link is older than 24 hours,
the system shall reject the request with a 410 Gone response.
```

### Phase 4 — Non-functional requirements

NFRs are where workshops usually skimp. Force each category to be addressed explicitly:

```markdown
## Non-Functional Requirements

### Performance
- Export job p95 start time: < 5 seconds (queue + DB query kickoff)
- Export job throughput: 10k records/minute minimum
- Download response time: < 2 seconds TTFB for files up to 100MB

### Security
- Authentication required for all endpoints
- Authorization: user can only export their own data
- Download link is single-use OR time-boxed (24h) — pick one
- Export file encrypted at rest; URL is unguessable (≥128 bits entropy)
- PII fields (email, phone) included by default; user can deselect at request time

### Scalability
- Concurrent export jobs: 50 (queue depth limit; reject with 429 above)
- Max export size: 500MB; above → split into multiple files
- Export retention: 24 hours, then auto-delete

### Observability
- Log: job start, job complete, job failure, download events
- Metrics: job count, job duration p50/p95/p99, failure rate, download count
- Alert: failure rate > 5% over 5 minutes

### Compliance
- GDPR Article 20 (data portability) — format must be machine-readable
- Audit log: who requested, when, what was included, when downloaded
```

If a category is genuinely not applicable, say so explicitly: "Scalability: not applicable, feature is admin-only with expected concurrency of 1." Don't just omit it.

### Phase 5 — Acceptance criteria (Given/When/Then)

Each FR gets one or more acceptance criteria. Criteria must be testable in isolation. Use INVEST: Independent, Negotiable, Valuable, Estimable, Small, Testable.

```markdown
## Acceptance Criteria

### AC-001: Successful Export Initiation (happy path)
Given a logged-in user with at least 1 record in the last 24 months
When they click "Export my data" and select JSON format
Then an export job is created with status "queued"
And the user is redirected to a progress page

### AC-002: Empty Export
Given a logged-in user with zero records in the last 24 months
When they click "Export my data"
Then an export job is created with status "completed"
And the resulting file contains only `[]` (JSON) or headers (CSV)

### AC-004: Concurrent Job Limit
Given a user with an in-progress export job
When they click "Export my data" again
Then the system returns 429 with `{"error": "Export already in progress"}`
And no new job is created

### AC-005: Download Link Expiry
Given a download link that was generated 24+ hours ago
When the user clicks the link
Then the system returns 410 Gone
And the underlying file has been deleted

### AC-007: Cross-User Access Denied
Given a download link belonging to user A
When user B (logged in) attempts to use it
Then the system returns 403 Forbidden
And an audit log entry is created (potential abuse signal)
```

### Phase 6 — Error handling matrix

Enumerate the failure modes and the system's response. This is where Dev Hat earns its keep.

```markdown
## Error Handling

| Error Condition | HTTP | User Message | Side Effect |
|-----------------|------|--------------|-------------|
| Not authenticated | 401 | "Please log in to continue" | None |
| Not authorized (cross-user) | 403 | "You don't have permission" | Audit log entry |
| Job already in progress | 429 | "Export already in progress" | None |
| Export too large (>500MB) | 413 | "Data too large to export, contact support" | Job fails fast |
| Queue full (>50 jobs) | 503 | "System busy, try again in a few minutes" | None |
| Link expired | 410 | "Link expired, request a new export" | File deleted |
| Internal error | 500 | "Something went wrong, support notified" | Alert fired |
```

### Phase 7 — Edge cases

Brainstorm the weird inputs and conditions. Each gets a decision: handle, reject, or defer.

```markdown
## Edge Cases

- User has 0 records → AC-002 (empty file)
- User has 10M records → split into chunks of 1M, multi-file download
- User account deleted between job start and completion → cancel job, log
- Daylight saving boundary in 24-month window → use UTC, document the convention
- Export includes soft-deleted records? → no, only active (default; flag for product)
- Export of nested data (comments on posts) → JSON: nested; CSV: flat with prefixes
```

### Phase 8 — Implementation TODO

Convert the spec into a buildable checklist. Each item is a single PR-sized unit.

```markdown
## Implementation TODO

### Backend
- [ ] DB migration: `export_jobs` table (id, user_id, status, format, created_at, completed_at, file_url, expires_at)
- [ ] POST /exports endpoint with auth + format validation
- [ ] GET /exports/:id for status polling
- [ ] GET /exports/:id/download with re-auth + expiry check
- [ ] Background worker: query user data, serialize to JSON/CSV, upload to S3, update job
- [ ] Cron: delete expired files and job records
- [ ] Email template: "Your export is ready"

### Frontend
- [ ] Export button in account settings + format selector
- [ ] Progress page with polling
- [ ] Error states (429, 410, 500)

### Testing & Observability
- [ ] Unit: job lifecycle, format serialization, expiry logic
- [ ] Integration: full flow end-to-end
- [ ] Load: 50 concurrent jobs, 10M records
- [ ] Metrics: job count, duration p50/p95/p99, failure rate
- [ ] Audit log: every export requested, every download
```

## Pre-discovery with subagents (for multi-domain features)

For features touching 3+ layers (auth, DB, UI, external API), front-load technical context with parallel exploration subagents *before* the workshop. This lets the interview focus on decisions, not exploration.

```text
Feature: user profile with avatar upload (touches auth, storage, UI, image processing)

Pre-discovery (parallel):
  - Subagent 1 (architecture): analyze current user model, storage patterns, image handling
  - Subagent 2 (security): identify file-upload risks for this stack (mime sniffing, path traversal, SSRF)
  - Subagent 3 (framework): how does this project handle API endpoints, multipart, file storage?

Collect findings → feed into the workshop → questions like "where do we store avatars?"
come with concrete options ("S3 with presigned URLs matches existing pattern, see upload.ts:42")
instead of open-ended exploration.
```

See `agent-orchestration` skill for the delegation protocol.

## Workshop anti-patterns

| Anti-pattern | Symptom | Fix |
|--------------|---------|-----|
| Solution-first | "We'll build it with React and S3" before understanding the problem | Start with problem statement, defer tech |
| Happy-path only | No error matrix, no edge cases | Force Phases 6 and 7; refuse to ship without them |
| Vague NFRs | "Should be fast", "should be secure" | Quantify: p95 < X ms, encrypt with Y, rate limit Z/min |
| Untestable criteria | "The system should be user-friendly" | Rewrite as Given/When/Then with concrete assertion |
| Scope creep mid-workshop | "While we're at it, can we also…" | Add to Out of Scope; refuse to expand MVP without re-prioritizing |
| Single-hat workshop | All PM or all Dev | Force the other hat for at least one phase |
| No open questions | Everything decided | Genuine uncertainty exists; surface it explicitly |

## Save location

`specs/{feature_name}.spec.md`

Required sections: Overview, Functional Requirements (EARS), Non-Functional Requirements, Acceptance Criteria (Given/When/Then), Error Handling, Implementation TODO. Recommended: Out of Scope, Open Questions.
