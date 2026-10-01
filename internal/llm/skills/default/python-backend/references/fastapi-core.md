# FastAPI 0.110+ Core — Pydantic v2, Async Deps, Lifespan, OpenAPI

FastAPI is an async-first Python web framework built on Starlette and Pydantic. Use it for async microservices, serverless APIs, and any backend where OpenAPI auto-generation and async I/O matter.

## Project layout

```
app/
  __init__.py
  main.py                    FastAPI app, lifespan, router includes
  config.py                  pydantic-settings env config
  database.py                async engine, session, Base
  dependencies.py            shared deps (current_user, db, rate limit)
  models/                    SQLAlchemy ORM models
  schemas/                   Pydantic v2 schemas (input/output)
  routers/                   APIRouter per feature
  crud/                      DB access functions
  services/                  business logic (no HTTP, no DB)
  core/                      security, exceptions, middleware
tests/
  conftest.py                fixtures (async client, db)
  test_users.py
```

## Pydantic v2 schemas

Pydantic v2 is a rewrite — 5-50× faster, Rust core, new API. Never mix with v1 syntax.

```python
from pydantic import BaseModel, EmailStr, Field, field_validator, model_validator, model_config, ConfigDict

class UserCreate(BaseModel):
    model_config = ConfigDict(str_strip_whitespace=True, extra="forbid")  # extra="forbid" → 422 on unknown fields

    email: EmailStr
    password: str = Field(min_length=8, max_length=128)
    name: str | None = Field(default=None, max_length=200)
    role: list[str] = Field(default_factory=list)

    @field_validator("password")
    @classmethod
    def _strong_password(cls, v: str) -> str:
        if not any(c.isupper() for c in v): raise ValueError("must contain uppercase")
        if not any(c.isdigit() for c in v): raise ValueError("must contain digit")
        return v

    @model_validator(mode="after")
    def _check_role(self) -> "UserCreate":
        if "admin" in self.role and self.password.startswith("password"):
            raise ValueError("admin password cannot start with 'password'")
        return self

class UserResponse(BaseModel):
    model_config = ConfigDict(from_attributes=True)          # allows model_validate(orm_obj)

    id: int
    email: EmailStr
    name: str | None = None
    created_at: datetime

class UserUpdate(BaseModel):
    model_config = ConfigDict(extra="forbid")
    email: EmailStr | None = None
    name: str | None = None
    password: str | None = Field(default=None, min_length=8)
```

### v1 → v2 migration

| v1 (forbidden) | v2 (correct) |
|---|---|
| `from pydantic import BaseModel, validator` | `from pydantic import BaseModel, field_validator` |
| `@validator("field")` | `@field_validator("field")` + `@classmethod` |
| `@root_validator` | `@model_validator(mode="after")` |
| `class Config: ...` | `model_config = ConfigDict(...)` |
| `Model.parse_obj(d)` | `Model.model_validate(d)` |
| `Model.dict()` | `Model.model_dump()` |
| `Model.json()` | `Model.model_dump_json()` |
| `class Config: orm_mode=True` | `model_config = ConfigDict(from_attributes=True)` |
| `Field(..., regex=r"...")` | `Field(..., pattern=r"...")` |

## Endpoints + routing

```python
# routers/users.py
from typing import Annotated
from fastapi import APIRouter, Depends, HTTPException, Query, status
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

router = APIRouter(prefix="/users", tags=["users"])

@router.get("/", response_model=list[UserResponse])
async def list_users(
    db: Annotated[AsyncSession, Depends(get_db)],
    cursor: str | None = None,
    limit: Annotated[int, Query(ge=1, le=100)] = 20,
) -> list[UserResponse]:
    stmt = select(User).order_by(User.id.desc()).limit(limit)
    if cursor:
        stmt = stmt.where(User.id < int(cursor))
    result = await db.execute(stmt)
    return [UserResponse.model_validate(u) for u in result.scalars().all()]

@router.post("/", response_model=UserResponse, status_code=status.HTTP_201_CREATED)
async def create_user(
    payload: UserCreate,
    db: Annotated[AsyncSession, Depends(get_db)],
) -> UserResponse:
    if await crud.get_user_by_email(db, payload.email):
        raise HTTPException(status.HTTP_409_CONFLICT, "email already registered")
    user = await crud.create_user(db, payload)
    return UserResponse.model_validate(user)
```

Rules:
- Type hints drive DI and OpenAPI — never omit them.
- `Annotated[X, Depends(...)]` is the modern syntax (3.9+).
- `response_model` filters output — only declared fields are returned, blocking accidental leakage.
- `Query(ge=1, le=100)` validates query params before the handler runs.
- Raise `HTTPException` for HTTP errors; let other exceptions bubble to exception handlers.

## Dependency injection

```python
# dependencies.py
from typing import Annotated
from fastapi import Depends, Header, HTTPException, status

async def get_db() -> AsyncSession:
    async with AsyncSessionLocal() as session:
        async with session.begin():
            yield session

async def get_current_user(
    authorization: Annotated[str, Header()],
    db: Annotated[AsyncSession, Depends(get_db)],
) -> User:
    token = authorization.removeprefix("Bearer ")
    try:
        payload = jwt.decode(token, JWT_SECRET, algorithms=["HS256"])
    except jwt.InvalidTokenError:
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, "invalid token")
    user = await db.get(User, int(payload["sub"]))
    if not user:
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, "user not found")
    return user

CurrentUser = Annotated[User, Depends(get_current_user)]

# usage
@router.get("/me")
async def me(user: CurrentUser) -> UserResponse:
    return UserResponse.model_validate(user)
```

Depends can be a function or a class; class-based deps use `__call__` for the resolver. Deps are cached per-request by default — `use_cache=False` to disable.

## Lifespan (replaces `on_event`)

`@app.on_event("startup")` is deprecated. Use `lifespan`:

```python
from contextlib import asynccontextmanager
from fastapi import FastAPI

@asynccontextmanager
async def lifespan(app: FastAPI):
    # Startup
    app.state.redis = await create_redis_pool()
    app.state.db_engine = create_async_engine(DATABASE_URL, pool_size=20, max_overflow=10)
    yield
    # Shutdown
    await app.state.redis.close()
    await app.state.db_engine.dispose()

app = FastAPI(lifespan=lifespan, title="Example API", version="1.0.0")
```

Lifespan runs before any request and after all requests finish — perfect for connection pools, warm caches, scheduled task startup.

## Async SQLAlchemy 2.x

```python
# database.py
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine, AsyncSession
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column

engine = create_async_engine("postgresql+asyncpg://user:pass@host/db", pool_size=20, max_overflow=10, echo=False)
AsyncSessionLocal = async_sessionmaker(engine, expire_on_commit=False)

class Base(DeclarativeBase):
    pass

# models/user.py
class User(Base):
    __tablename__ = "users"
    id: Mapped[int] = mapped_column(primary_key=True)
    email: Mapped[str] = mapped_column(unique=True, index=True)
    password_hash: Mapped[str] = mapped_column()
    name: Mapped[str | None] = mapped_column(default=None)
    created_at: Mapped[datetime] = mapped_column(server_default=func.now())

# crud.py
async def get_user_by_email(db: AsyncSession, email: str) -> User | None:
    result = await db.execute(select(User).where(User.email == email))
    return result.scalar_one_or_none()

async def create_user(db: AsyncSession, payload: UserCreate) -> User:
    user = User(email=payload.email, password_hash=hash_password(payload.password), name=payload.name)
    db.add(user)
    await db.flush()                  # get the ID without committing
    return user
```

Rules:
- `asyncpg` driver (not `psycopg2`) for async.
- `expire_on_commit=False` — otherwise attribute access after commit triggers a sync refresh.
- `session.begin()` wraps in a transaction; commit on success, rollback on exception.
- `select(User).where(...)` is the 2.x syntax; never the legacy `User.query`.
- `result.scalar_one_or_none()` returns the row or None (or raises `MultipleResultsFound` if >1).

## Alembic migrations

```bash
pip install alembic
alembic init migrations
```

`migrations/env.py` — wire async engine + import models:
```python
from app.database import Base
from app.models import user  # noqa: F401  (must import to register)
target_metadata = Base.metadata

# Use sync engine for migrations — Alembic doesn't run async natively
sync_url = DATABASE_URL.replace("+asyncpg", "+psycopg2")
```

```bash
alembic revision --autogenerate -m "create users table"
alembic upgrade head
alembic downgrade -1                  # rollback one revision
```

Always inspect generated migrations — autogenerate misses: check constraints, partial indexes, enum renames.

## Background tasks

```python
from fastapi import BackgroundTasks

@router.post("/emails/")
async def send_email(payload: EmailRequest, bg: BackgroundTasks):
    bg.add_task(send_email_async, payload.to, payload.body)
    return {"status": "queued"}
```

For durable queues, prefer Celery / RQ / Dramatiq — `BackgroundTasks` die with the process, not suitable for prod-critical work. WebSocket endpoints follow the same pattern as in `api-design/references/websocket-realtime.md`; for multi-instance scaling, store connection registry in Redis, not process memory.

## OpenAPI customization

```python
from fastapi.openapi.utils import get_openapi

def custom_openapi():
    if app.openapi_schema: return app.openapi_schema
    schema = get_openapi(title="Example API", version="1.0.0", routes=app.routes)
    schema["components"]["securitySchemes"] = {"BearerAuth": {"type": "http", "scheme": "bearer", "bearerFormat": "JWT"}}
    schema["security"] = [{"BearerAuth": []}]
    app.openapi_schema = schema
    return app.openapi_schema

app.openapi = custom_openapi
```

`/docs` → Swagger UI; `/redoc` → ReDoc; `/openapi.json` → raw spec. In production, gate `/docs` behind auth or disable.

## Configuration

```python
from pydantic_settings import BaseSettings, SettingsConfigDict

class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8", extra="ignore")
    database_url: str
    jwt_secret: str
    jwt_expires_minutes: int = 15
    redis_url: str = "redis://localhost:6379/0"
    cors_origins: list[str] = ["http://localhost:3000"]

settings = Settings()                  # raises on missing required env
```

## Exception handlers

```python
@app.exception_handler(RequestValidationError)
async def validation_handler(request: Request, exc: RequestValidationError):
    return JSONResponse(status_code=422, content={
        "type": "https://api.example.com/errors/validation",
        "title": "Validation Error", "status": 422,
        "detail": "Request body failed validation",
        "errors": [{"field": ".".join(str(p) for p in e["loc"]), "message": e["msg"]} for e in exc.errors()],
    }, media_type="application/problem+json")

@app.exception_handler(Exception)
async def fallback_handler(request: Request, exc: Exception):
    logger.exception("unhandled", extra={"path": request.url.path})
    return JSONResponse(status_code=500, content={
        "type": "https://api.example.com/errors/internal",
        "title": "Internal Server Error", "status": 500,
    }, media_type="application/problem+json")
```

Output matches RFC 7807 — same envelope as the api-design skill.

## Verification gates

- `mypy --strict app/` — type errors block.
- `ruff check app/ && ruff format --check app/` — lint + format clean.
- `pytest` — async tests pass (pytest-asyncio mode=auto).
- `uvicorn app.main:app --reload` — app boots without errors.
- `/docs` and `/openapi.json` load; all endpoints documented.
- `/health/live` returns 200; `/health/ready` checks DB + Redis.
- `alembic upgrade head` runs cleanly on a fresh DB.
