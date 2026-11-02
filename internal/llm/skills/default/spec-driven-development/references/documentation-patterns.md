# Documentation Patterns

Generate and validate technical documentation. Covers docstrings, JSDoc, OpenAPI, READMEs, and doc-site setup. The discipline: docs are code — they get written, validated, and updated in the same PR as the code they describe. Untested docs become lies.

## The 5-phase workflow

1. **Discover** — Ask for format preference and exclusions. Don't assume Google-style vs NumPy-style; don't assume the user wants every private function documented.
2. **Detect** — Identify language and framework. The right API-doc strategy depends on the framework (FastAPI auto-generates OpenAPI; Express needs explicit Swagger; Django needs `drf-spectacular`).
3. **Analyze** — Find undocumented code. List public functions/classes without docstrings; flag stale docstrings (last edited before the code changed).
4. **Document** — Apply the consistent format. Match existing style in the file; don't introduce a second convention.
5. **Validate** — Test that examples compile/run. This is the step most people skip and it is the step that matters most.

## Validation commands

| Doc type | Validation | Run |
|----------|------------|-----|
| Python docstrings (doctest) | Examples run correctly | `python -m doctest -v module.py` |
| Python docstrings (module) | All doctests in a module | `pytest --doctest-modules` |
| TypeScript JSDoc | Examples type-check | `tsc --noEmit` (extracted examples) |
| OpenAPI 3.1 | Spec is valid | `npx @redocly/cli lint openapi.yaml` |
| OpenAPI 3.0 | Spec is valid | `npx @apidevtools/swagger-cli validate openapi.yaml` |
| Markdown links | No broken links | `linkchecker docs/` or `blc https://localhost:3000 -ro` |
| Doc site build | Site builds | `mkdocs build` / `docusaurus build` / `vitepress build` |

If validation fails: fix the docs and re-validate **before** reporting done. A doc with a broken example is worse than no doc — it actively misleads.

## Python docstrings

### Google style (most common, readable)

```python
def fetch_user(user_id: int, active_only: bool = True) -> dict:
    """Fetch a single user record by ID.

    Long description if needed. Keep it to 1-2 sentences and explain *why*,
    not *what* (the signature already says what).

    Args:
        user_id: Unique identifier for the user. Must be a positive integer.
        active_only: When True, raise UserNotFoundError for inactive users.
            Default True matches the common case (admin dashboard).

    Returns:
        A dict containing user fields: ``id``, ``name``, ``email``,
        ``created_at``. ``email`` is None for users who opted out.

    Raises:
        ValueError: If ``user_id`` is not a positive integer.
        UserNotFoundError: If no matching user exists, or (when
            ``active_only=True``) the user is inactive.

    Example:
        >>> fetch_user(42)
        {'id': 42, 'name': 'Ada', 'email': 'ada@example.com', 'created_at': ...}
    """
    if user_id <= 0:
        raise ValueError(f"user_id must be positive, got {user_id}")
    # ...
```

### NumPy style (scientific Python, more structured)

```python
def compute_similarity(vec_a: np.ndarray, vec_b: np.ndarray) -> float:
    """Compute cosine similarity between two vectors.

    Parameters
    ----------
    vec_a : np.ndarray
        First input vector, shape ``(n,)``. Must be finite (no NaN/Inf).
    vec_b : np.ndarray
        Second input vector, shape ``(n,)``. Must be the same length as ``vec_a``.

    Returns
    -------
    float
        Cosine similarity in the range ``[-1.0, 1.0]``. Returns ``0.0`` if
        either vector has zero norm.

    Raises
    ------
    ValueError
        If vectors have different lengths or contain non-finite values.
    """
```

### Sphinx style (reST, used by Sphinx builds)

```python
def fetch_user(user_id, active_only=True):
    """Fetch a single user record by ID.

    :param user_id: Unique identifier for the user.
    :type user_id: int
    :param active_only: When True, raise for inactive users. Default True.
    :type active_only: bool
    :returns: User record dict.
    :rtype: dict
    :raises ValueError: If user_id is not positive.
    :raises UserNotFoundError: If no matching user exists.
    """
```

Pick one style per project; match existing style if present. Mixing styles in one codebase is worse than any single choice.

## TypeScript / JavaScript JSDoc

```typescript
/**
 * Fetches a paginated list of products from the catalog.
 *
 * Longer description if needed. Explain non-obvious behavior: caching,
 * rate limiting, side effects.
 *
 * @param categoryId - The category to filter by. Must be a valid UUID.
 * @param page - Page number, 1-indexed. Default 1.
 * @param limit - Maximum items per page, 1-100. Default 20.
 * @returns Resolves to a page of product records.
 * @throws {NotFoundError} If the category does not exist.
 * @throws {ValidationError} If `limit` is outside 1-100.
 *
 * @example
 * // Fetch the second page of 10 electronics products
 * const page = await fetchProducts('electronics-uuid', 2, 10);
 * console.log(page.items);
 */
async function fetchProducts(
  categoryId: string,
  page = 1,
  limit = 20,
): Promise<ProductPage> {
  // ...
}
```

For TypeScript, prefer letting the types carry the structural info (return type, param types) and use JSDoc for *intent*, *constraints*, and *examples*. Don't duplicate the type signature in prose — that's what makes JSDoc drift from the code.

## OpenAPI 3.1

OpenAPI describes HTTP APIs in a machine-readable format. Auto-generated docs (Swagger UI, Redoc) come from this spec. Two strategies:

1. **Spec-first** — write `openapi.yaml` by hand, generate server stubs and clients. Best for public APIs where the contract is the source of truth.
2. **Code-first** — write code with annotations, generate `openapi.yaml` from it. Best for internal APIs where the code is the source of truth. Frameworks: FastAPI (Python, automatic), `drf-spectacular` (Django), `@nestjs/swagger` (NestJS), `zod-openapi` (Hono/Express).

### Minimal valid OpenAPI 3.1 spec

```yaml
openapi: 3.1.0
info:
  title: Users API
  version: 1.0.0
  description: User management for the example app.
servers:
  - url: https://api.example.com/v1
    description: Production
paths:
  /users/{user_id}:
    get:
      summary: Get a user by ID
      operationId: getUser
      parameters:
        - name: user_id
          in: path
          required: true
          schema:
            type: integer
            minimum: 1
          description: Unique user identifier.
      responses:
        '200':
          description: User found.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/User'
        '404':
          description: User not found.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Error'
        '400':
          description: Invalid user_id.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Error'
components:
  schemas:
    User:
      type: object
      required: [id, name, email]
      properties:
        id: { type: integer, example: 42 }
        name: { type: string, example: "Ada Lovelace" }
        email: { type: string, format: email, example: "ada@example.com" }
        created_at: { type: string, format: date-time }
    Error:
      type: object
      required: [error]
      properties:
        error: { type: string }
        details: { type: object, additionalProperties: true }
```

Validate: `npx @redocly/cli lint openapi.yaml`. Common lint failures: missing `operationId`, missing `description` on parameters, no `404` for `GET /resource/{id}`, missing `examples`.

## README

A README is the front door. It must answer, in order:

1. **What this is** — one sentence.
2. **Why it exists** — one paragraph; what problem it solves.
3. **How to run it** — `git clone`, install, configure, run. Tested commands.
4. **How to use it** — happy-path example for the primary use case.
5. **Where to learn more** — links to docs, API reference, contributing guide.

```markdown
# users-api

User management service for the example app. Handles accounts, auth, and profiles.

## Why

Centralizes user data so every service doesn't reimplement auth and profile
storage. Replaces the legacy `users-v1` service (deprecated, sunset 2025-Q3).

## Quickstart

```bash
git clone git@github.com:example/users-api.git
cd users-api
cp .env.example .env  # then edit values
docker compose up -d postgres
pnpm install
pnpm db:migrate
pnpm dev
```

API now live at http://localhost:3000. Swagger UI at /docs.

## Example

```bash
# Create a user
curl -X POST http://localhost:3000/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Ada", "email": "ada@example.com"}'

# Log in
curl -X POST http://localhost:3000/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email": "ada@example.com", "password": "..."}'
```

## Docs

- [API reference](https://api.example.com/docs)
- [Architecture](docs/architecture.md)
- [Contributing](CONTRIBUTING.md)
```

Don't write a README that's a feature catalog. Write one that gets a new contributor from `git clone` to a working local instance in 5 minutes.

## Doc sites

When the project is large enough to need a documentation site (not just a README), pick a generator that matches the stack:

| Generator | Stack | Use when |
|-----------|-------|----------|
| Docusaurus | React, MDX | React project, multi-version docs, blog |
| MkDocs Material | Python, Jinja2 | Python project, simple setup, mkdocstrings auto-API |
| VitePress | Vue, Vite | Vue project, fast builds, lightweight |
| Nextra | Next.js | Next.js project, MDX integration |
| Mintlify | React, hosted | Want AI search, hosted, no build infra |

All four support: search (Algolia DocSearch or local), syntax highlighting, versioning, and MDX. Pick the one matching your stack — running a Python doc site for a TypeScript project adds friction with no benefit.

## Documentation systems checklist

```text
[ ] Public functions/classes have docstrings
[ ] Docstring format matches existing style (Google / NumPy / Sphinx)
[ ] Examples in docstrings are tested (doctest, tsc)
[ ] OpenAPI spec is valid (redocly lint passes)
[ ] OpenAPI covers every public endpoint
[ ] README quickstart commands actually work (test them in a fresh clone)
[ ] README has a "why" section, not just a "what"
[ ] Architecture decisions are recorded as ADRs (see adr-template.md)
[ ] Doc site builds without warnings
[ ] Broken-link checker passes
[ ] Stale docs (last edited before code changed) are flagged in review
```

## The rule that ties it together

Docs and code drift because they are maintained by different people at different times. The fix is structural: docs live in the same repo as the code, are validated in the same CI as the code, and are reviewed in the same PR as the code. A doc change without a code change is a flag; a code change without a doc change is a flag. The two move together or they don't move at all.
