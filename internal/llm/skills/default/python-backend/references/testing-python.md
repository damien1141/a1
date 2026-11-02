# Testing Async Python — pytest, httpx, pytest-django, factories

Async test patterns for FastAPI (pytest-asyncio + httpx `AsyncClient`) and Django (pytest-django + APITestCase + factory-boy). Goal: every endpoint and service method has a test; `pytest` green before merge.

## FastAPI async tests

```bash
pip install pytest pytest-asyncio httpx
```

```python
# conftest.py
import pytest
import pytest_asyncio
from httpx import AsyncClient, ASGITransport
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine, AsyncSession
from app.main import app
from app.database import Base, get_db

@pytest_asyncio.fixture
async def db_engine():
    engine = create_async_engine("postgresql+asyncpg://test:test@localhost/test_db", echo=False)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.drop_all)
        await conn.run_sync(Base.metadata.create_all)
    yield engine
    await engine.dispose()

@pytest_asyncio.fixture
async def db_session(db_engine):
    sessionmaker = async_sessionmaker(db_engine, expire_on_commit=False)
    async with sessionmaker() as session:
        yield session

@pytest_asyncio.fixture
async def client(db_session):
    async def override_get_db():
        yield db_session
    app.dependency_overrides[get_db] = override_get_db
    async with AsyncClient(transport=ASGITransport(app=app), base_url="http://test") as ac:
        yield ac
    app.dependency_overrides.clear()

@pytest_asyncio.fixture
async def auth_token(client):
    # create user + login
    await client.post("/users/", json={"email":"a@b.com","password":"Password1!"})
    res = await client.post("/auth/token", data={"username":"a@b.com","password":"Password1!"})
    return res.json()["access_token"]

@pytest.fixture
def auth_headers(auth_token):
    return {"Authorization": f"Bearer {auth_token}"}
```

```python
# test_users.py
import pytest

@pytest.mark.asyncio
async def test_create_user(client):
    res = await client.post("/users/", json={"email":"a@b.com","password":"Password1!"})
    assert res.status_code == 201
    assert res.json()["email"] == "a@b.com"
    assert "password" not in res.json()

@pytest.mark.asyncio
async def test_create_user_rejects_weak_password(client):
    res = await client.post("/users/", json={"email":"a@b.com","password":"short"})
    assert res.status_code == 422
    body = res.json()
    assert body["errors"][0]["field"] == "password"

@pytest.mark.asyncio
async def test_create_user_rejects_duplicate(client):
    payload = {"email":"a@b.com","password":"Password1!"}
    await client.post("/users/", json=payload)
    res = await client.post("/users/", json=payload)
    assert res.status_code == 409

@pytest.mark.asyncio
async def test_get_me_requires_auth(client):
    res = await client.get("/users/me")
    assert res.status_code == 401
    assert res.headers["WWW-Authenticate"] == "Bearer"

@pytest.mark.asyncio
async def test_get_me_with_token(client, auth_headers):
    res = await client.get("/users/me", headers=auth_headers)
    assert res.status_code == 200
    assert res.json()["email"] == "a@b.com"
```

Rules:
- `pytest-asyncio` mode = `auto` (`pyproject.toml`) — no `@pytest.mark.asyncio` boilerplate.
- Override FastAPI deps (`app.dependency_overrides[get_db] = ...`) for test DB.
- Use `httpx.AsyncClient` with `ASGITransport` — no need to start a server.
- Drop + recreate schema per test run (or per test) for isolation.
- For per-test isolation, wrap each test in a transaction and rollback at teardown.

## pytest config

```toml
# pyproject.toml
[tool.pytest.ini_options]
asyncio_mode = "auto"
testpaths = ["tests"]
addopts = "-ra --strict-markers --tb=short"
markers = ["integration: marks integration tests", "slow: marks slow tests"]

[tool.coverage.run]
source = ["app"]
omit = ["app/tests/*", "*/migrations/*"]
```

## Service unit tests (no HTTP)

```python
# test_users_service.py
import pytest
from app.services.users import create_user
from app.exceptions import ConflictError

@pytest.mark.asyncio
async def test_create_user_conflict(db_session):
    # Seed an existing user
    await create_user(db_session, UserCreate(email="a@b.com", password="Password1!"))
    with pytest.raises(ConflictError):
        await create_user(db_session, UserCreate(email="a@b.com", password="Password1!"))
```

Service tests are faster and clearer than HTTP tests. Use them for business logic; reserve HTTP tests for the HTTP layer (status codes, response shapes, auth).

## Django tests

```bash
pip install pytest pytest-django factory-boy
```

```python
# conftest.py
import pytest
from django.test import Client

@pytest.fixture
def client():
    return Client()

@pytest.fixture
def admin_user(db):
    from django.contrib.auth import get_user_model
    return get_user_model().objects.create_superuser("admin", "admin@example.com", "password123")

@pytest.fixture
def admin_client(client, admin_user):
    client.force_login(admin_user)
    return client
```

```python
# tests/test_articles.py
import pytest
from django.urls import reverse
from rest_framework import status
from apps.articles.models import Article
from apps.articles.tests.factories import ArticleFactory

@pytest.mark.django_db
def test_list_articles(client):
    ArticleFactory.create_batch(3)
    res = client.get("/api/articles/")
    assert res.status_code == status.HTTP_200_OK
    assert len(res.json()["results"]) == 3

@pytest.mark.django_db
def test_create_article_requires_auth(client):
    res = client.post("/api/articles/", {"title":"x","body":"y"})
    assert res.status_code in (401, 403)

@pytest.mark.django_db
def test_create_article_authenticated(client, admin_user):
    client.force_login(admin_user)
    res = client.post("/api/articles/", {"title":"Hello","body":"World"})
    assert res.status_code == status.HTTP_201_CREATED
    assert Article.objects.filter(title="Hello").exists()

@pytest.mark.django_db
def test_owner_can_update(client, admin_user):
    article = ArticleFactory(author=admin_user)
    client.force_login(admin_user)
    res = client.patch(f"/api/articles/{article.id}/", {"title":"Updated"})
    assert res.status_code == status.HTTP_200_OK
    article.refresh_from_db()
    assert article.title == "Updated"
```

## APITestCase (DRF built-in)

```python
from rest_framework.test import APITestCase
from rest_framework import status
from django.contrib.auth.models import User

class ArticleAPITest(APITestCase):
    def setUp(self):
        self.user = User.objects.create_user("alice", password="password123")

    def test_list_public(self):
        res = self.client.get("/api/articles/")
        self.assertEqual(res.status_code, status.HTTP_200_OK)

    def test_create_requires_auth(self):
        res = self.client.post("/api/articles/", {"title":"Test"})
        self.assertEqual(res.status_code, status.HTTP_401_UNAUTHORIZED)

    def test_create_authenticated(self):
        self.client.force_authenticate(self.user)
        res = self.client.post("/api/articles/", {"title":"Hello Django"})
        self.assertEqual(res.status_code, status.HTTP_201_CREATED)
```

## Factories (factory-boy)

```python
# apps/articles/tests/factories.py
import factory
from apps.articles.models import Article, Tag
from django.contrib.auth.models import User

class UserFactory(factory.django.DjangoModelFactory):
    class Meta: model = User
    username = factory.Sequence(lambda n: f"user{n}")
    email = factory.LazyAttribute(lambda o: f"{o.username}@example.com")

class TagFactory(factory.django.DjangoModelFactory):
    class Meta: model = Tag
    name = factory.Sequence(lambda n: f"tag-{n}")

class ArticleFactory(factory.django.DjangoModelFactory):
    class Meta: model = Article
    title = factory.Faker("sentence", nb_words=4)
    slug = factory.Sequence(lambda n: f"article-{n}")
    body = factory.Faker("paragraph")
    author = factory.SubFactory(UserFactory)
    status = "published"

    @factory.post_generation
    def tags(self, create, extracted, **kwargs):
        if not create: return
        if extracted: self.tags.set(extracted)
        else: self.tags.add(TagFactory())
```

Rules:
- One factory per model.
- `SubFactory` for FK; `post_generation` for M2M.
- Use `Faker` for realistic data (catches length/format bugs).
- `create_batch(3)` for bulk creation.

## pytest-django config

```toml
# pyproject.toml
[tool.pytest.ini_options]
DJANGO_SETTINGS_MODULE = "project.settings_test"
python_files = ["tests.py", "test_*.py"]
addopts = "-ra --tb=short --reuse-db"
markers = ["integration: integration tests"]
```

`--reuse-db` skips DB recreation between runs (2-5× faster); run `pytest --create-db` after schema changes.

## Test DB isolation

- **FastAPI:** Transactional rollback per test, or drop+recreate schema. Never let one test's data leak into another.
- **Django:** Each `TestCase` class wraps in a transaction; `pytest.mark.django_db` for fixture-level access.
- Use a separate test DB (never dev DB); reset before each CI run.

## Coverage

```bash
pytest --cov=app --cov-report=term-missing --cov-fail-under=80
```

Coverage is a floor, not a target. 80%+ is healthy; 100% with weak tests is meaningless. Focus coverage on:
- Service layer (business logic).
- Auth and permission checks.
- Edge cases (empty, null, max length, invalid types).
- Error paths (DB down, external API failure).

## Verification gates

- `pytest` — all tests pass.
- `pytest --cov=app --cov-fail-under=80` — coverage floor met.
- `mypy --strict` (CI) — type errors block.
- `ruff check` — lint clean.
- No skipped tests without a tracking ticket.
- Tests run in parallel (`pytest -n auto` with `pytest-xdist`) for speed.
