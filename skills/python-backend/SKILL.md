---
name: python-backend
description: Senior Python backend on FastAPI 0.110+ (Pydantic v2, async deps, OpenAPI, lifespan) and Django 5 + DRF (serializers, viewsets, signals). Use for REST APIs, async SQLAlchemy, JWT/OAuth2, Alembic migrations, async views, or choosing FastAPI vs Django. S3 storage covered as a reference file.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: backend
  triggers: FastAPI, Pydantic v2, async Python, Django, DRF, Django REST Framework, Django ORM, serializer, viewset, Python web, SQLAlchemy async, Alembic, JWT, OAuth2, lifespan, async views, signals
  role: specialist
  scope: implementation
  output-format: code
  related-skills: api-design, python-pro, database-pro, testing-master
---

# Python Backend

Builds Python backends on FastAPI 0.110+ (async-first, Pydantic v2, OpenAPI auto-gen) and Django 5 + DRF (batteries-included, ORM, admin, async views). Picks the framework from a decision table, then ships typed schemas, async DB layers, auth, and tests. S3 storage patterns covered as a reference file.

## When to Use

New Python REST API — pick FastAPI or Django via decision table; FastAPI (async endpoints, Pydantic v2, dependency injection, lifespan, OpenAPI); Django (ORM, admin, DRF viewsets/serializers, async views, signals); async SQLAlchemy + Alembic; JWT/OAuth2 auth; S3 file storage; async testing (pytest-asyncio + httpx).

## Operating Loop

1. **Pick framework** — apply the Decision Table below.
2. **Scaffold** — `fastapi new app` or `django-admin startproject app`. Enforce `mypy --strict` + `ruff check` in CI.
3. **Design schemas/models** — FastAPI: Pydantic v2 with `field_validator`/`model_validator`. Django: ORM models with indexes + `Meta.ordering`.
4. **Implement endpoints/views** — FastAPI: `APIRouter` + `Depends` for DI. Django: DRF `ModelViewSet` + `select_related`/`prefetch_related`.
5. **Migrations** — Alembic (`alembic revision --autogenerate`) for FastAPI; `makemigrations` for Django.
6. **Auth** — OAuth2 password flow + JWT (FastAPI) or SimpleJWT (Django).
7. **Test** — pytest-asyncio + httpx (FastAPI) or pytest-django + APITestCase (Django).
8. **Verify gates** — `mypy`, `ruff`, `pytest`, OpenAPI doc loads at `/docs` (FastAPI) or `/api/schema` (drf-spectacular).

## Framework Decision Table

| Need | Pick | Why | Reject |
|---|---|---|---|
| Async-first, microservice, OpenAPI auto-gen | **FastAPI** | native async, Pydantic v2, `/docs` free | Django (sync ORM roots, heavier) |
| Admin UI, ORM, batteries-included monolith | **Django + DRF** | admin, auth, sessions, ORM, mature ecosystem | FastAPI (DIY everything) |
| Multi-tenant SaaS with staff admin | **Django** | admin + tenant middleware patterns ready | FastAPI (no admin) |
| High-throughput async I/O (websockets, SSE, fan-out) | **FastAPI** | async-native, Starlette under the hood | Django (ORM sync, async views newer) |
| Existing Django team / codebase | **Django** | leverage team knowledge; don't fight the framework | FastAPI (rewrite cost > benefit) |
| Serverless functions (Lambda, Cloud Run) | **FastAPI** | cold start ~50ms vs Django ~300ms | Django (heavier cold start) |
| GraphQL primary | **Strawberry** (FastAPI or Django) | type-safe schema, code-first | Graphene (older, slower) |

**Default rule:** New async microservice or serverless → FastAPI. New monolith with admin → Django. Both can coexist in a monorepo.

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| FastAPI 0.110+ (Pydantic v2, async deps, lifespan, OpenAPI) | `references/fastapi-core.md` | Building FastAPI endpoints, async SQLAlchemy, lifespan |
| Django 5 + DRF (models, serializers, viewsets, async views, signals) | `references/django-drf.md` | Building Django/DRF APIs, ORM optimization |
| Auth — JWT/OAuth2, SimpleJWT, permissions | `references/auth-python.md` | JWT, OAuth2 password flow, SimpleJWT, scoped permissions |
| Testing async Python (pytest-asyncio, httpx, pytest-django) | `references/testing-python.md` | Async tests, APITestCase, factories, fixtures |
| S3 storage (django-storages, boto3, presigned URLs) | `references/s3-storage.md` | Django STORAGES dict, public/private backends, presigned URLs |

## Constraints

### MUST DO
- Type hints everywhere (`mypy --strict` in CI); FastAPI requires them for DI.
- Pydantic v2 syntax: `field_validator`, `model_validator`, `model_config` — never v1 `@validator` / `class Config`.
- Async I/O throughout (`async def`, `await`) — never sync DB calls inside async endpoints.
- `Annotated[X, Depends(...)]` for FastAPI deps; `select_related`/`prefetch_related` for Django ORM.
- Index frequently queried columns; `Meta.indexes` for composite indexes.
- Environment-driven config (`pydantic-settings` or `django-environ`); never hardcode secrets.
- Migrations checked in; never run with `migrate --run-syncdb` in prod.
- Tests for every endpoint + service method; `pytest` green before merge.
- Document endpoints (FastAPI auto-gen via type hints; Django via drf-spectacular).

### MUST NOT DO
- Use Pydantic v1 syntax (`@validator`, `class Config`, `parse_obj`).
- Mix sync and async DB code — async engine + async session, or sync engine + sync session; never cross.
- Use raw SQL without parameterization (`cursor.execute("SELECT ... %s", (val,))`).
- Store secrets in `settings.py`; never `DEBUG=True` in production.
- Skip migrations; never manually edit DB schema.
- Expose `passwordHash` or internal IDs in API responses — use explicit response schemas.
- Use Django ORM `.objects.all()` in loops (N+1) — `select_related` for FK, `prefetch_related` for M2M.
- Block the event loop with sync I/O in FastAPI handlers — offload to `run_in_executor` if unavoidable.

## Code Examples

### FastAPI — Pydantic v2 schema + async endpoint + DI

```python
from typing import Annotated
from fastapi import APIRouter, Depends, HTTPException, status
from pydantic import BaseModel, EmailStr, field_validator, model_config
from sqlalchemy.ext.asyncio import AsyncSession

class UserCreate(BaseModel):
    model_config = model_config(str_strip_whitespace=True)
    email: EmailStr
    password: str
    name: str | None = None

    @field_validator("password")
    @classmethod
    def _strong(cls, v: str) -> str:
        if len(v) < 8: raise ValueError("password must be >= 8 chars")
        return v

class UserResponse(BaseModel):
    model_config = model_config(from_attributes=True)
    id: int
    email: EmailStr
    name: str | None = None

router = APIRouter(prefix="/users", tags=["users"])
DbDep = Annotated[AsyncSession, Depends(get_db)]

@router.post("/", response_model=UserResponse, status_code=status.HTTP_201_CREATED)
async def create_user(payload: UserCreate, db: DbDep) -> UserResponse:
    if await crud.get_user_by_email(db, payload.email):
        raise HTTPException(status.HTTP_409_CONFLICT, "email already registered")
    return UserResponse.model_validate(await crud.create_user(db, payload))
```

### Django + DRF — Model, Serializer, ViewSet with ORM optimization

```python
# models.py
class Article(models.Model):
    title = models.CharField(max_length=255, db_index=True)
    author = models.ForeignKey("auth.User", on_delete=models.CASCADE, related_name="articles")
    published_at = models.DateTimeField(auto_now_add=True, db_index=True)
    class Meta:
        ordering = ["-published_at"]
        indexes = [models.Index(fields=["author", "published_at"])]

# serializers.py
class ArticleSerializer(serializers.ModelSerializer):
    author_username = serializers.CharField(source="author.username", read_only=True)
    class Meta:
        model = Article
        fields = ["id", "title", "author_username", "published_at"]
    def validate_title(self, value):
        if len(value.strip()) < 3: raise serializers.ValidationError("Title must be >= 3 chars.")
        return value.strip()

# views.py
class ArticleViewSet(viewsets.ModelViewSet):
    serializer_class = ArticleSerializer
    permission_classes = [permissions.IsAuthenticatedOrReadOnly]
    def get_queryset(self):
        return Article.objects.select_related("author").all()   # avoid N+1
    def perform_create(self, serializer):
        serializer.save(author=self.request.user)
```

### FastAPI — Lifespan (replaces `on_event("startup")`)

```python
from contextlib import asynccontextmanager
from fastapi import FastAPI

@asynccontextmanager
async def lifespan(app: FastAPI):
    app.state.db = await init_db_pool()                          # startup
    yield
    await app.state.db.close()                                   # shutdown

app = FastAPI(lifespan=lifespan)
```

### Django — Async view (Django 5)

```python
async def async_article_list(request):
    articles = await Article.objects.select_related("author").afirst()
    return JsonResponse({"title": articles.title})
```

## Output Template

1. **Framework choice** — decision table row applied, one-line rationale.
2. **Schemas/models** — Pydantic v2 schemas or Django models with indexes.
3. **Endpoints/views** — `APIRouter` + `Depends` (FastAPI) or `ModelViewSet` (Django).
4. **Migrations** — Alembic revision files or Django `makemigrations` output.
5. **Auth** — OAuth2/JWT flow (FastAPI) or SimpleJWT config (Django).
6. **Tests** — pytest-asyncio + httpx (FastAPI) or pytest-django + APITestCase.
7. **Verify gates** — `mypy --strict`, `ruff check`, `pytest`, OpenAPI loads.

Separate VERIFIED (type-checks, tests pass) from ASSUMED (perf under load, prod auth scale).

## Knowledge Reference

FastAPI 0.110+ (Pydantic v2, `Annotated` deps, `lifespan`, OpenAPI auto-gen, BackgroundTasks, WebSocket); async SQLAlchemy 2.x (`AsyncSession`, `select`); Alembic; `pydantic-settings`; `python-jose`/`pyjwt`; `passlib[bcrypt]`; httpx; pytest-asyncio; Django 5 (async views, ASGI, ORM/QuerySet, `select_related`/`prefetch_related`, signals, `Meta.indexes`); DRF (ModelViewSet, serializers, permissions, SimpleJWT, django-filter, drf-spectacular); pytest-django, factory-boy; django-storages, boto3.
