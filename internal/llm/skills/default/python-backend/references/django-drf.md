# Django 5 + DRF — Models, Serializers, ViewSets, Async Views, Signals

Django is a batteries-included Python web framework: ORM, admin, auth, sessions, migrations, security middleware. DRF (Django REST Framework) adds serializers, viewsets, content negotiation, and auth on top. Django 5 supports async views, async ORM methods, and ASGI deployment.

## Project layout

```
project/
  manage.py
  project/
    settings.py
    urls.py
    asgi.py                       ASGI entrypoint (uvicorn/gunicorn + uvicorn.workers.UvicornWorker)
    wsgi.py                       WSGI entrypoint (gunicorn)
  apps/
    users/
      models.py
      serializers.py
      views.py
      urls.py
      admin.py
      tests/
      migrations/
    orders/
      ...
```

One Django app per bounded context. Apps are reusable; keep them small and focused.

## Models — ORM with indexes and constraints

```python
# models.py
from django.db import models
from django.core.validators import MinLengthValidator

class Article(models.Model):
    title = models.CharField(max_length=255, db_index=True, validators=[MinLengthValidator(3)])
    slug = models.SlugField(unique=True, max_length=255)
    author = models.ForeignKey(
        "auth.User",
        on_delete=models.CASCADE,
        related_name="articles",
    )
    body = models.TextField()
    status = models.CharField(max_length=20, choices=[("draft","draft"),("published","published")], default="draft", db_index=True)
    tags = models.ManyToManyField("Tag", blank=True, related_name="articles")
    published_at = models.DateTimeField(auto_now_add=True, db_index=True)
    updated_at = models.DateTimeField(auto_now=True)

    class Meta:
        ordering = ["-published_at"]
        indexes = [
            models.Index(fields=["author", "published_at"]),         # composite
            models.Index(fields=["-published_at"], name="article_recent_idx"),
        ]
        constraints = [
            models.UniqueConstraint(fields=["slug"], name="unique_slug"),
            models.CheckConstraint(check=models.Q(title__length__gte=3), name="title_min_len"),
        ]

    def __str__(self): return self.title

class Tag(models.Model):
    name = models.CharField(max_length=50, unique=True)
```

Rules:
- `db_index=True` on frequently filtered/sorted fields.
- Composite indexes in `Meta.indexes` for multi-column WHERE clauses.
- `related_name` always — defaults to `<model>_set` which is fragile.
- `on_delete=models.CASCADE` for owned children; `PROTECT` for referenced entities you can't lose; `SET_NULL` for optional FKs.
- `auto_now_add` for created; `auto_now` for updated. Never editable by user.
- Use `constraints` (DB-level) over `validators` (Python-level) when possible — DB constraints are bulletproof.

## Migrations

```bash
python manage.py makemigrations
python manage.py migrate
python manage.py migrate app_name 0005            # rollback to specific migration
python manage.py sqlmigrate app_name 0006         # preview SQL without running
python manage.py squashmigrations app_name 0001 0010  # squash 10 migrations into 1
```

Always inspect `makemigrations` output before committing. Common misses:
- `null=True` on a non-nullable field → Django prompts for a default on existing rows; pick a sensible one.
- Renaming a field → Django generates a drop + add (data loss). Use `RenameField` manually.
- Renaming a model → use `RenameModel`.
- Large table additions → split into multiple migrations (`CreateModel` then `AddField` with `default=`).

```python
# Manual data migration
from django.db import migrations

def copy_slug_to_lowercase(apps, schema_editor):
    Article = apps.get_model("articles", "Article")
    for a in Article.objects.all():
        a.slug = a.slug.lower()
        a.save(update_fields=["slug"])

class Migration(migrations.Migration):
    dependencies = [("articles", "0005_auto")]
    operations = [
        migrations.RunPython(copy_slug_to_lowercase, migrations.RunPython.noop),
    ]
```

## DRF Serializers

```python
# serializers.py
from rest_framework import serializers
from .models import Article, Tag

class TagSerializer(serializers.ModelSerializer):
    class Meta:
        model = Tag
        fields = ["id", "name"]

class ArticleSerializer(serializers.ModelSerializer):
    author_username = serializers.CharField(source="author.username", read_only=True)
    tags = TagSerializer(many=True, read_only=True)
    tag_ids = serializers.PrimaryKeyRelatedField(
        many=True, write_only=True, queryset=Tag.objects.all(), source="tags"
    )

    class Meta:
        model = Article
        fields = ["id", "title", "slug", "author_username", "body", "status",
                  "tags", "tag_ids", "published_at", "updated_at"]
        read_only_fields = ["slug", "published_at", "updated_at"]

    def validate_title(self, value):
        if len(value.strip()) < 3:
            raise serializers.ValidationError("Title must be at least 3 characters.")
        return value.strip()

    def validate(self, attrs):
        # cross-field validation
        if attrs.get("status") == "published" and not attrs.get("body"):
            raise serializers.ValidationError({"body": "Published articles need a body."})
        return attrs

    def create(self, validated_data):
        tags = validated_data.pop("tags", [])
        article = Article.objects.create(**validated_data)
        article.tags.set(tags)
        return article

    def update(self, instance, validated_data):
        tags = validated_data.pop("tags", None)
        for k, v in validated_data.items():
            setattr(instance, k, v)
        instance.save()
        if tags is not None:
            instance.tags.set(tags)
        return instance
```

Rules:
- `source="author.username"` for nested reads — saves a serializer layer.
- `read_only=True` for fields set by the server (e.g., `author`, timestamps).
- `write_only=True` for write-only inputs (e.g., `tag_ids` to set tags).
- `validate_<field>` for single-field; `validate` for cross-field.
- Override `create` / `update` for nested writes (M2M, nested objects).

## DRF ViewSets

```python
# views.py
from rest_framework import viewsets, permissions, filters
from django_filters.rest_framework import DjangoFilterBackend
from .models import Article
from .serializers import ArticleSerializer

class ArticleViewSet(viewsets.ModelViewSet):
    serializer_class = ArticleSerializer
    permission_classes = [permissions.IsAuthenticatedOrReadOnly]
    queryset = Article.objects.select_related("author").prefetch_related("tags").all()
    filter_backends = [DjangoFilterBackend, filters.SearchFilter, filters.OrderingFilter]
    filterset_fields = ["status", "author"]
    search_fields = ["title", "body"]
    ordering_fields = ["published_at", "updated_at"]

    def get_queryset(self):
        qs = super().get_queryset()
        if not self.request.user.is_staff:
            qs = qs.filter(status="published")
        return qs

    def perform_create(self, serializer):
        serializer.save(author=self.request.user)
```

Rules:
- `select_related("author")` for FK — single SQL JOIN, no N+1.
- `prefetch_related("tags")` for M2M/reverse — second query, but batched.
- `get_queryset()` for per-request scoping (ownership, role).
- `perform_create` / `perform_update` to inject request context.
- `ModelViewSet` = full CRUD; `ReadOnlyModelViewSet` = list + retrieve; mix `CreateModelMixin` etc. for custom subsets.

## Async views (Django 5)

```python
import asyncio
import httpx
from django.http import JsonResponse

async def fetch_external(request):
    async with httpx.AsyncClient() as client:
        users, posts = await asyncio.gather(
            client.get("https://api.example.com/users"),
            client.get("https://api.example.com/posts"),
        )
    return JsonResponse({"users": users.json(), "posts": posts.json()})

# Async ORM (Django 4.2+)
async def async_article_list(request):
    articles = [a async for a in Article.objects.select_related("author").aiterator()[:20]]
    return JsonResponse({"titles": [a.title for a in articles]})
```

Rules:
- Deploy via ASGI (`uvicorn project.asgi:application` or `gunicorn -k uvicorn.workers.UvicornWorker`).
- Don't mix sync and async in one view — sync DB calls block the event loop.
- `aiterator()` for streaming large querysets; `acount()`, `afirst()`, `aget()` for single ops.
- Sync middleware runs in threadpool alongside async views; check middleware compatibility.

## Signals

```python
# signals.py
from django.db.models.signals import post_save, pre_delete
from django.dispatch import receiver
from .models import Article

@receiver(post_save, sender=Article)
def invalidate_cache(sender, instance: Article, created: bool, **kwargs):
    if created:
        cache.delete(f"user_articles:{instance.author_id}")

@receiver(pre_delete, sender=Article)
def archive_before_delete(sender, instance: Article, **kwargs):
    ArchivedArticle.objects.create(
        original_id=instance.id, title=instance.title, body=instance.body
    )
```

Connect in `apps.py` `ready()`:
```python
class ArticlesConfig(AppConfig):
    name = "articles"
    def ready(self):
        from . import signals  # noqa: F401
```

**Signal smell test:** if the signal does anything more than cache invalidation or simple denormalization, move the logic to a service method called explicitly. Signals are invisible to readers — explicit calls are debuggable.

## Admin

```python
# admin.py
from django.contrib import admin
from .models import Article, Tag

class TagInline(admin.TabularInline):
    model = Article.tags.through
    extra = 1

@admin.register(Article)
class ArticleAdmin(admin.ModelAdmin):
    list_display = ["title", "author", "status", "published_at"]
    list_filter = ["status", "published_at", "author"]
    search_fields = ["title", "body"]
    readonly_fields = ["published_at", "updated_at"]
    autocomplete_fields = ["author"]            # needs search_fields on User admin
    actions = ["publish_selected"]

    @admin.action(description="Publish selected")
    def publish_selected(self, request, queryset):
        queryset.update(status="published")

@admin.register(Tag)
class TagAdmin(admin.ModelAdmin):
    search_fields = ["name"]
```

Admin is a power-user tool — never expose to public traffic; gate behind staff status + IP allowlist in production.

## drf-spectacular — OpenAPI

```python
# settings.py
INSTALLED_APPS += ["drf_spectacular"]
REST_FRAMEWORK = {
    "DEFAULT_SCHEMA_CLASS": "drf_spectacular.openapi.AutoSchema",
    "DEFAULT_AUTHENTICATION_CLASSES": ["rest_framework_simplejwt.authentication.JWTAuthentication"],
}
SPECTACULAR_SETTINGS = {
    "TITLE": "Example API",
    "DESCRIPTION": "...",
    "VERSION": "1.0.0",
    "SERVE_INCLUDE_SCHEMA": False,
}

# urls.py
from drf_spectacular.views import SpectacularAPIView, SpectacularSwaggerView
urlpatterns = [
    path("api/schema/", SpectacularAPIView.as_view(), name="schema"),
    path("api/docs/", SpectacularSwaggerView.as_view(url_name="schema"), name="docs"),
]
```

```bash
python manage.py spectacular --color --validate > openapi.yaml
```

## Async tasks — Celery

```python
# tasks.py
from celery import shared_task

@shared_task(bind=True, max_retries=3, default_retry_delay=60)
def send_welcome_email(self, user_id: int):
    try:
        user = User.objects.get(pk=user_id)
        send_mail("Welcome!", "...", "no-reply@example.com", [user.email])
    except Exception as exc:
        raise self.retry(exc=exc)
```

Django BackgroundTasks is OK for trivial work; Celery (or RQ / Dramatiq) for prod queues.

## Verification gates

- `mypy --strict` (with `django-stubs` and `djangorestframework-stubs` installed) — type errors block.
- `ruff check && ruff format --check` — lint + format clean.
- `python manage.py test` or `pytest` (with `pytest-django`) — tests pass.
- `python manage.py check --deploy` — production check passes.
- `python manage.py makemigrations --check --dry-run` — no missing migrations (CI gate).
- `/api/docs/` (drf-spectacular) loads; `/api/schema/` validates.
- `python manage.py spectacular --validate` — OpenAPI spec is valid.
