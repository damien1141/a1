# Auth — JWT, OAuth2, SimpleJWT, Permissions

Authentication and authorization patterns for FastAPI and Django. JWT for stateless APIs, OAuth2 password flow for first-party clients, SimpleJWT for DRF, role/permission systems.

## FastAPI — OAuth2 password flow + JWT

```python
# core/security.py
from datetime import datetime, timedelta, timezone
from jose import JWTError, jwt
from passlib.context import CryptContext

pwd_ctx = CryptContext(schemes=["bcrypt"], deprecated="auto")
ACCESS_TOKEN_EXPIRE_MINUTES = 15
REFRESH_TOKEN_EXPIRE_DAYS = 7

def hash_password(plain: str) -> str:
    return pwd_ctx.hash(plain)

def verify_password(plain: str, hashed: str) -> bool:
    return pwd_ctx.verify(plain, hashed)

def create_access_token(sub: str, scopes: list[str] | None = None) -> str:
    payload = {
        "sub": sub,
        "exp": datetime.now(timezone.utc) + timedelta(minutes=ACCESS_TOKEN_EXPIRE_MINUTES),
        "iat": datetime.now(timezone.utc),
        "type": "access",
        "scopes": scopes or [],
    }
    return jwt.encode(payload, settings.jwt_secret, algorithm="HS256")

def create_refresh_token(sub: str) -> str:
    payload = {
        "sub": sub,
        "exp": datetime.now(timezone.utc) + timedelta(days=REFRESH_TOKEN_EXPIRE_DAYS),
        "type": "refresh",
    }
    return jwt.encode(payload, settings.jwt_secret, algorithm="HS256")

def decode_token(token: str) -> dict:
    try:
        return jwt.decode(token, settings.jwt_secret, algorithms=["HS256"])
    except JWTError as e:
        raise HTTPException(401, f"invalid token: {e}")
```

```python
# routers/auth.py
from fastapi.security import OAuth2PasswordRequestForm
from typing import Annotated

@router.post("/token")
async def login(
    form: Annotated[OAuth2PasswordRequestForm, Depends()],
    db: Annotated[AsyncSession, Depends(get_db)],
) -> TokenResponse:
    user = await crud.get_user_by_email(db, form.username)
    if not user or not verify_password(form.password, user.password_hash):
        raise HTTPException(401, "incorrect email or password", headers={"WWW-Authenticate": "Bearer"})
    access = create_access_token(str(user.id), scopes=user.scopes)
    refresh = create_refresh_token(str(user.id))
    return TokenResponse(access_token=access, refresh_token=refresh, token_type="bearer")

@router.post("/token/refresh")
async def refresh_token(
    refresh: str,
    db: Annotated[AsyncSession, Depends(get_db)],
) -> TokenResponse:
    payload = decode_token(refresh)
    if payload.get("type") != "refresh":
        raise HTTPException(401, "not a refresh token")
    user = await db.get(User, int(payload["sub"]))
    if not user:
        raise HTTPException(401, "user not found")
    return TokenResponse(
        access_token=create_access_token(str(user.id), scopes=user.scopes),
        refresh_token=create_refresh_token(str(user.id)),
        token_type="bearer",
    )
```

```python
# dependencies.py
from fastapi import Security
from fastapi.security import OAuth2PasswordBearer, SecurityScopes

oauth2_scheme = OAuth2PasswordBearer(tokenUrl="/auth/token", scopes={
    "read": "Read access",
    "write": "Write access",
    "admin": "Admin access",
})

async def get_current_user(
    security_scopes: SecurityScopes,
    token: Annotated[str, Depends(oauth2_scheme)],
    db: Annotated[AsyncSession, Depends(get_db)],
) -> User:
    payload = decode_token(token)
    if payload.get("type") != "access":
        raise HTTPException(401, "not an access token")
    for scope in security_scopes.scopes:
        if scope not in payload.get("scopes", []):
            raise HTTPException(403, f"missing scope: {scope}")
    user = await db.get(User, int(payload["sub"]))
    if not user:
        raise HTTPException(401, "user not found")
    return user

# usage
@router.post("/articles/", response_model=ArticleResponse)
async def create_article(
    payload: ArticleCreate,
    user: Annotated[User, Security(get_current_user, scopes=["write"])],
    db: Annotated[AsyncSession, Depends(get_db)],
) -> ArticleResponse:
    ...
```

Rules:
- Short access token (15min) + long refresh token (7d).
- Refresh tokens: rotate on use (issue new refresh with each refresh); detect reuse → revoke all of user's tokens.
- `WWW-Authenticate: Bearer` header on 401 — clients expect it.
- Scope strings: simple verbs (`read`, `write`, `admin`), not hierarchical paths.
- Verify `type` claim — refresh tokens must not work as access tokens and vice versa.

## FastAPI — OAuth2 social login (Google example)

```python
from authlib.integrations.starlette_client import OAuth

oauth = OAuth()
oauth.register(
    name="google",
    client_id=settings.google_client_id,
    client_secret=settings.google_client_secret,
    server_metadata_url="https://accounts.google.com/.well-known/openid-configuration",
    client_kwargs={"scope": "openid email profile"},
)

@router.get("/auth/google/login")
async def google_login(request: Request):
    redirect_uri = request.url_for("google_callback")
    return await oauth.google.authorize_redirect(request, redirect_uri)

@router.get("/auth/google/callback", name="google_callback")
async def google_callback(request: Request, db: Annotated[AsyncSession, Depends(get_db)]):
    token = await oauth.google.authorize_access_token(request)
    user_info = token["userinfo"]
    user = await crud.upsert_oauth_user(db, email=user_info["email"], provider="google", subject=user_info["sub"])
    access = create_access_token(str(user.id), scopes=user.scopes)
    return RedirectResponse(f"/?access={access}")
```

## Django + DRF — SimpleJWT

```bash
pip install djangorestframework-simplejwt
```

```python
# settings.py
from datetime import timedelta
SIMPLE_JWT = {
    "ACCESS_TOKEN_LIFETIME": timedelta(minutes=15),
    "REFRESH_TOKEN_LIFETIME": timedelta(days=7),
    "ROTATE_REFRESH_TOKENS": True,           # issue new refresh on each refresh
    "BLACKLIST_AFTER_ROTATION": True,        # revoke old refresh
    "ALGORITHM": "HS256",
    "SIGNING_KEY": settings.SECRET_KEY,
    "AUTH_HEADER_TYPES": ("Bearer",),
}

REST_FRAMEWORK = {
    "DEFAULT_AUTHENTICATION_CLASSES": [
        "rest_framework_simplejwt.authentication.JWTAuthentication",
        "rest_framework.authentication.SessionAuthentication",  # for admin
    ],
    "DEFAULT_PERMISSION_CLASSES": ["rest_framework.permissions.IsAuthenticated"],
}
```

```python
# urls.py
from rest_framework_simplejwt.views import TokenObtainPairView, TokenRefreshView, TokenVerifyView
urlpatterns = [
    path("api/token/", TokenObtainPairView.as_view(), name="token_obtain_pair"),
    path("api/token/refresh/", TokenRefreshView.as_view(), name="token_refresh"),
    path("api/token/verify/", TokenVerifyView.as_view(), name="token_verify"),
]
```

## DRF permissions

```python
# permissions.py
from rest_framework import permissions

class IsOwnerOrReadOnly(permissions.BasePermission):
    def has_object_permission(self, request, view, obj):
        if request.method in permissions.SAFE_METHODS:
            return True
        return obj.author_id == request.user.id

class IsAdminOrReadOnly(permissions.BasePermission):
    def has_permission(self, request, view):
        if request.method in permissions.SAFE_METHODS:
            return True
        return request.user.is_staff

# usage
class ArticleViewSet(viewsets.ModelViewSet):
    permission_classes = [permissions.IsAuthenticatedOrReadOnly, IsOwnerOrReadOnly]
```

Rules:
- `has_permission` — checked on every request (list, create).
- `has_object_permission` — checked on retrieve/update/delete (after `get_queryset`).
- Combine permission classes — all must pass (AND).
- `IsAuthenticated` / `IsAdminUser` / `IsAuthenticatedOrReadOnly` built-in; subclass for custom logic.

## Password hashing

```python
# FastAPI — passlib with bcrypt
from passlib.context import CryptContext
pwd_ctx = CryptContext(schemes=["bcrypt"], deprecated="auto", bcrypt__rounds=12)
hashed = pwd_ctx.hash(password)
ok = pwd_ctx.verify(password, hashed)

# Django — built-in
from django.contrib.auth.hashers import make_password, check_password
hashed = make_password(password)              # uses PBKDF2 by default
ok = check_password(password, hashed)

# Django — switch to argon2
# settings.py
PASSWORD_HASHERS = [
    "django.contrib.auth.hashers.Argon2PasswordHasher",
    "django.contrib.auth.hashers.PBKDF2PasswordHasher",
]
```

Rules:
- Argon2 for new apps (memory-hard, GPU-resistant). bcrypt acceptable. Never MD5/SHA1/plain.
- 12+ bcrypt rounds (or argon2 `memory_cost=65536, time_cost=3, parallelism=4`).
- Never log passwords; never put plaintext in errors.
- Use `set_password`/`check_password` helpers; never write your own.

## Session auth (Django-first-party)

```python
# settings.py
AUTHENTICATION_BACKENDS = ["django.contrib.auth.backends.ModelBackend"]
SESSION_COOKIE_HTTPONLY = True
SESSION_COOKIE_SECURE = True              # HTTPS only
SESSION_COOKIE_SAMESITE = "Lax"           # or "Strict" for same-site apps
CSRF_COOKIE_SECURE = True
CSRF_COOKIE_HTTPONLY = True
CSRF_COOKIE_SAMESITE = "Lax"

# Login flow
from django.contrib.auth import authenticate, login, logout
def login_view(request):
    user = authenticate(request, username=email, password=password)
    if user:
        login(request, user)              # sets session cookie
        return redirect("/")
    return render(request, "login.html", {"error": "invalid"})
```

Use sessions for first-party browser apps (admin, internal tools); JWT for API clients (mobile, SPA, third-party).

## API keys (third-party clients)

```python
# FastAPI
async def get_api_key(
    x_api_key: Annotated[str, Header(alias="X-API-Key")],
    db: Annotated[AsyncSession, Depends(get_db)],
) -> ApiKey:
    key = await crud.get_api_key(db, x_api_key)
    if not key or key.expires_at < datetime.now(timezone.utc):
        raise HTTPException(401, "invalid api key")
    await crud.touch_api_key(db, key)        # update last_used_at
    return key

# Django
class ApiKeyAuthentication(authentication.BaseAuthentication):
    def authenticate(self, request):
        key = request.META.get("HTTP_X_API_KEY")
        if not key: return None
        try:
            return (ApiKey.objects.get(key=key, expires_at__gt=timezone.now()).user, None)
        except ApiKey.DoesNotExist:
            raise authentication.AuthenticationFailed("invalid api key")
```

API keys: hash at rest (never store plaintext), short expiry, scoped, revocable per-client.

## Rate limiting

FastAPI (slowapi):
```python
from slowapi import Limiter
from slowapi.util import get_remote_address

limiter = Limiter(key_func=get_remote_address)
app.state.limiter = limiter

@router.post("/login")
@limiter.limit("5/minute")
async def login(request: Request, ...): ...
```

Django (django-ratelimit):
```python
from django_ratelimit.decorators import ratelimit
from django.views.decorators.http import require_POST

@require_POST
@ratelimit(key="ip", rate="5/m", block=True)
def login_view(request): ...
```

Rate-limit auth endpoints aggressively: 5/min for login, 3/hour for password reset.

## Verification gates

- Login endpoint: valid creds → 200 + tokens; invalid → 401; rate-limited → 429.
- Token refresh: expired refresh → 401; rotation issues new refresh; old refresh revoked.
- Scope/permission check: missing scope → 403; correct scope → 200; unauthenticated → 401.
- Object-level permission: owner reads → 200; non-owner reads public → 200; non-owner writes → 403.
- Logout: refresh token added to blacklist; reusing it → 401.
- Password reset: token expires in 1h; can be used once; email sent.
