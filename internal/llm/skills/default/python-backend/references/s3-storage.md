# S3 Storage — django-storages, boto3, Presigned URLs

Production-grade file storage on AWS S3 via `django-storages` and `boto3`. Public (CDN-served) and private (presigned) media, static files, presigned download/upload URLs, CloudFront, IAM policies, S3 mocking in tests. Applies primarily to Django; FastAPI uses `boto3` directly with the same IAM/presigning patterns.

## Install + register

```bash
pip install django-storages[s3] boto3
```

```python
# settings.py
INSTALLED_APPS += ["storages"]
```

## Configure credentials

Load from env vars or attached IAM role. Never hardcode.

```python
# settings.py
import os
AWS_STORAGE_BUCKET_NAME = os.environ["AWS_STORAGE_BUCKET_NAME"]
AWS_S3_REGION_NAME = os.environ.get("AWS_S3_REGION_NAME", "us-east-1")
AWS_S3_CUSTOM_DOMAIN = f"{AWS_STORAGE_BUCKET_NAME}.s3.{AWS_S3_REGION_NAME}.amazonaws.com"
# On EC2/ECS/Lambda: omit keys entirely — boto3 uses the attached IAM role.
```

## STORAGES dict (Django 4.2+)

```python
# settings.py
STORAGES = {
    "default": {                                              # media uploads
        "BACKEND": "storages.backends.s3boto3.S3Boto3Storage",
        "OPTIONS": {
            "bucket_name": AWS_STORAGE_BUCKET_NAME,
            "location": "media",                              # prefix in bucket
            "default_acl": None,                              # rely on bucket policy, not per-object ACLs
            "file_overwrite": False,                          # protect against silent overwrites
            "querystring_auth": False,                        # public objects → clean URLs
        },
    },
    "staticfiles": {
        "BACKEND": "storages.backends.s3boto3.S3StaticStorage",
        "OPTIONS": {
            "bucket_name": AWS_STORAGE_BUCKET_NAME,
            "location": "static",
        },
    },
}

MEDIA_URL = f"https://{AWS_S3_CUSTOM_DOMAIN}/media/"
STATIC_URL = f"https://{AWS_S3_CUSTOM_DOMAIN}/static/"
```

Rules:
- `default_acl=None` — on buckets created after April 2023, ACLs are disabled by default. Setting anything else raises `AccessControlListNotSupported`. Public access belongs in a bucket policy.
- Distinct `location` prefixes for media vs static — `collectstatic` and uploads must not collide.
- `file_overwrite=False` — new uploads get unique filenames; protects against silent overwrite of existing files.
- `querystring_auth=False` for public buckets → clean URLs without `?AWSAccessKeyId=...`.

## Public vs private backends

```python
# storages.py
from storages.backends.s3boto3 import S3Boto3Storage

class PublicMediaStorage(S3Boto3Storage):
    bucket_name = AWS_STORAGE_BUCKET_NAME
    location = "media/public"
    default_acl = None
    file_overwrite = False
    querystring_auth = False                                  # public via bucket policy

class PrivateMediaStorage(S3Boto3Storage):
    bucket_name = AWS_STORAGE_BUCKET_NAME
    location = "media/private"
    default_acl = None
    file_overwrite = False
    querystring_auth = True                                   # presigned URLs only
    custom_domain = None                                      # must be None for presigning to work
```

```python
# models.py
from django.db import models
from .storages import PrivateMediaStorage

class Document(models.Model):
    owner = models.ForeignKey("auth.User", on_delete=models.CASCADE)
    title = models.CharField(max_length=255)
    file = models.FileField(storage=PrivateMediaStorage, upload_to="docs/")   # private
    avatar = models.FileField(storage=PublicMediaStorage, upload_to="avatars/")  # public
    uploaded_at = models.DateTimeField(auto_now_add=True)
```

Per-field storage: pass `storage=SomeBackend` to the field; rest of model uses `STORAGES["default"]`.

## Presigned URLs

```python
# views.py
from django.conf import settings
from storages.backends.s3boto3 import S3Boto3Storage

class DocumentViewSet(viewsets.ModelViewSet):
    queryset = Document.objects.all()
    serializer_class = DocumentSerializer
    permission_classes = [IsOwner]

    @action(detail=True, methods=["get"])
    def download_url(self, request, pk=None):
        doc = self.get_object()
        storage = doc.file.storage                              # PrivateMediaStorage
        url = storage.url(doc.file.name)                        # presigned GET URL
        return Response({"url": url, "expires_in": 3600})

    @action(detail=True, methods=["post"])
    def upload_url(self, request, pk=None):
        doc = self.get_object()
        storage = S3Boto3Storage()
        # Presigned POST → client uploads directly to S3, bypassing Django
        url, fields = storage.bucket.meta.client.generate_presigned_post(
            Bucket=storage.bucket_name,
            Key=f"media/private/docs/{doc.id}-{request.data['filename']}",
            ExpiresIn=3600,
            Conditions=[["content-length-range", 0, 10_485_760]],  # max 10 MB
        )
        return Response({"url": url, "fields": fields, "expires_in": 3600})
```

Rules:
- `querystring_auth=True` + `custom_domain=None` on private backends — presigning breaks if `custom_domain` is set.
- Presigned GET TTL ≤ 1h; rotate frequently.
- Presigned POST conditions: `content-length-range`, `acl`, `Content-Type` — restrict what clients can upload.
- Don't cache presigned URLs past their expiry — clients will hit 403.

## CloudFront in front of S3

```python
# settings.py
AWS_S3_CUSTOM_DOMAIN = "cdn.example.com"                       # CloudFront domain
# CloudFront origin = S3 bucket; serves public objects via CDN
```

For private files via CloudFront: use signed URLs/cookies from CloudFront (not S3 presigning). CloudFront signed URLs cache at edge — S3 presigned URLs bypass the cache.

## IAM policy (least privilege)

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
    "Resource": "arn:aws:s3:::my-bucket/media/*"
  }, {
    "Effect": "Allow",
    "Action": ["s3:ListBucket"],
    "Resource": "arn:aws:s3:::my-bucket",
    "Condition": {"StringLike": {"s3:prefix": ["media/*"]}}
  }]
}
```

Rules:
- Never grant `s3:*` or `s3:GetObject/*` on all buckets.
- Scope `GetObject`/`PutObject` to specific key prefixes.
- `ListBucket` scoped by `prefix` condition — clients can only see their own keys.
- Separate IAM users/roles per environment (dev/stage/prod).

## Testing storage code

### In-memory storage (Django 4.2+)

```python
# settings_test.py
STORAGES = {
    "default": {"BACKEND": "django.core.files.storage.InMemoryStorage"},
    "staticfiles": {"BACKEND": "django.core.files.storage.InMemoryStorage"},
}
```

### moto (mock S3 API)

```python
# tests/test_documents.py
import pytest
from moto import mock_aws
from django.core.files.uploadedfile import SimpleUploadedFile

@pytest.mark.django_db
@mock_aws
def test_upload_creates_s3_object(settings, client):
    # Configure storages to use a fake bucket
    settings.AWS_STORAGE_BUCKET_NAME = "test-bucket"
    # Create the fake bucket
    import boto3
    boto3.client("s3", region_name="us-east-1").create_bucket(Bucket="test-bucket")

    file = SimpleUploadedFile("doc.pdf", b"file content", content_type="application/pdf")
    res = client.post("/api/documents/", {"title": "Doc", "file": file})
    assert res.status_code == 201

    # Verify the object exists in mocked S3
    objs = boto3.client("s3").list_objects_v2(Bucket="test-bucket")
    assert any("doc.pdf" in o["Key"] for o in objs.get("Contents", []))
```

Use `InMemoryStorage` for unit tests (fast, no S3); `moto` for integration tests that need to verify presigning or bucket operations.

## Auditing an existing configuration

When reviewing a project that already uses S3:

1. **Credentials** — `grep -rn "AWS_SECRET_ACCESS_KEY\|aws_secret" settings/` → values come from `os.environ`/IAM role, never literals in the repo.
2. **ACLs** — `grep -rn "default_acl\|AWS_DEFAULT_ACL" .` → on buckets created after April 2023, every value must be `None`. `"public-read"`/`"private"` raises `AccessControlListNotSupported`.
3. **Storage backend** — Django 4.2+ uses the `STORAGES` dict. `DEFAULT_FILE_STORAGE`/`STATICFILES_STORAGE` were removed in 5.1 — silently ignored on 5.1/5.2/6.0.
4. **Locations** — `default` (media) and `staticfiles` have distinct `location` prefixes so `collectstatic` never collides with uploads.
5. **Region** — `region_name` matches the bucket's real region; `AWS_S3_CUSTOM_DOMAIN` includes the region segment for non-`us-east-1` buckets.
6. **Presigning** — for private backends: `querystring_auth=True` AND `custom_domain=None`. Presigned `.url()` results not cached past `AWS_QUERYSTRING_EXPIRE`.
7. **Overwrite cleanup** — where `file_overwrite=False`, replaced files are explicitly deleted (otherwise superseded objects leak).
8. **IAM** — policy grants only `Get/Put/Delete/ListBucket` on the bucket ARN, not broader S3 access.

## FastAPI direct S3

```python
import boto3
from botocore.client import Config
from fastapi import UploadFile

s3 = boto3.client("s3", region_name="us-east-1", config=Config(signature_version="s3v4"))

async def upload_to_s3(file: UploadFile, key: str) -> str:
    await file.seek(0)
    s3.upload_fileobj(file.file, Bucket="my-bucket", Key=key,
                      ExtraArgs={"ContentType": file.content_type})
    return f"s3://my-bucket/{key}"

def presigned_get(key: str, expires: int = 3600) -> str:
    return s3.generate_presigned_url(
        "get_object", Params={"Bucket": "my-bucket", "Key": key}, ExpiresIn=expires
    )

def presigned_post(key: str, expires: int = 3600, max_size: int = 10_485_760) -> dict:
    return s3.generate_presigned_post(
        Bucket="my-bucket", Key=key, ExpiresIn=expires,
        Conditions=[["content-length-range", 0, max_size]],
    )
```

Same IAM, presigning, and audit rules apply. For multi-part uploads > 100MB, use `boto3.s3.transfer.TransferConfig` and `upload_file` (handles multipart under the hood).

## Verification gates

- `collectstatic` succeeds; files land in `static/` prefix.
- Upload test: `POST` a file → 201; object visible in S3 (or mocked S3).
- Presigned URL test: GET URL → 200; URL expired → 403.
- IAM audit: policy JSON scopes to specific prefixes only.
- `grep -rn "AWS_SECRET" .` returns nothing committed.
- `STORAGES["default"]["OPTIONS"]["default_acl"]` is `None`.
- Tests use `InMemoryStorage` or `moto` — no real S3 calls in CI.
