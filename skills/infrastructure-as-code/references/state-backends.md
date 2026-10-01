# Terraform State Backends, Workspaces, Import, Drift, `moved`

## Why State Matters

Terraform state is the map between Terraform configuration and real resources. Lose it / corrupt it / share it wrong → infrastructure chaos.

State must be:
1. **Remote** — never local `.tfstate` for team environments
2. **Locked** — only one apply at a time per state file
3. **Encrypted** at rest (state contains resource IDs + sometimes secrets)
4. **Versioned** — backend supports history (S3 versioning, GCS versioning)
5. **Backed up** — periodically export/import to a second location

## Backend Configurations

### AWS S3 + DynamoDB (1.9 and earlier)

```hcl
terraform {
  required_version = ">= 1.7.0"

  backend "s3" {
    bucket         = "my-tf-state"
    key            = "env/prod/vpc/terraform.tfstate"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "terraform-state-lock"
    acl            = "private"
  }
}
```

Bootstrap infrastructure (one-time, often hand-managed or via CloudFormation):

```hcl
resource "aws_s3_bucket" "terraform_state" {
  bucket = "my-tf-state"
  lifecycle { prevent_destroy = true }
}

resource "aws_s3_bucket_versioning" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id
  versioning_configuration { status = "Enabled" }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id
  rule {
    apply_server_side_encryption_by_default { sse_algorithm = "aws:kms" }
    bucket_key_enabled = true
  }
}

resource "aws_s3_bucket_public_access_block" "terraform_state" {
  bucket                  = aws_s3_bucket.terraform_state.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_dynamodb_table" "terraform_lock" {
  name           = "terraform-state-lock"
  billing_mode   = "PAY_PER_REQUEST"
  hash_key       = "LockID"
  attribute { name = "LockID"; type = "S" }
}
```

### AWS S3 + native lockfile (1.10+)

```hcl
terraform {
  required_version = ">= 1.10.0"

  backend "s3" {
    bucket       = "my-tf-state"
    key          = "env/prod/vpc/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true        # native S3 lockfile; DynamoDB not needed
  }
}
```

### Azure Blob (auto-locking via lease)

```hcl
terraform {
  backend "azurerm" {
    resource_group_name  = "terraform-state-rg"
    storage_account_name = "tfstatestorage"
    container_name       = "tfstate"
    key                  = "env/prod/vpc.tfstate"
    use_azuread_auth     = true
  }
}
```

### GCS (auto-locking)

```hcl
terraform {
  backend "gcs" {
    bucket = "my-tf-state"
    prefix = "env/prod/vpc"
  }
}
```

### Terraform Cloud / Enterprise

```hcl
terraform {
  backend "remote" {
    hostname     = "app.terraform.io"
    organization = "my-org"
    workspaces { name = "prod-vpc" }
  }
}
```

## Workspaces — Use with Care

Workspaces share config but keep separate state files. Useful for parallel envs (dev/staging/prod) of the same module.

```bash
terraform workspace list
terraform workspace new staging
terraform workspace select production
terraform workspace show
terraform workspace delete dev
```

Workspace-aware config:

```hcl
locals {
  environment = terraform.workspace

  vpc_cidr = {
    production = "10.0.0.0/16"
    staging    = "10.1.0.0/16"
    dev        = "10.2.0.0/16"
  }
}

resource "aws_vpc" "main" {
  cidr_block = local.vpc_cidr[local.environment]
  tags       = { Name = "${local.environment}-vpc" }
}
```

### Workspace pitfalls

- Workspaces share the **same code** — you can't have one env on `aws_provider 5.x` and another on `4.x`.
- Workspaces share the **same backend config** (same bucket, different key prefix).
- Production state is accessible from a dev workspace if IAM permits — be careful with permissions.
- Hard to find which workspace you're in (`terraform workspace show` — easy to forget).

### When to NOT use workspaces

Use **separate state files / directories** instead when:
- Environments have **different topologies** (different modules, different regions)
- Production needs stricter access control (separate IAM)
- You want different provider versions per env

Recommended layout (separate states, not workspaces):

```
environments/
├── production/
│   ├── main.tf       # uses module "vpc" source = "../../modules/vpc"
│   ├── backend.tf    # backend "s3" { key = "env/prod/vpc.tfstate" }
│   └── terraform.tfvars
├── staging/
│   ├── main.tf
│   ├── backend.tf    # key = "env/staging/vpc.tfstate"
│   └── terraform.tfvars
└── dev/
```

## State Operations

```bash
# List all resources in state
terraform state list

# Show details of one resource
terraform state show aws_vpc.main

# Move within state (refactor without destroy/create) — prefer `moved` blocks
terraform state mv aws_instance.old aws_instance.new

# Move between states
terraform state mv -state-out=../other/terraform.tfstate aws_vpc.main aws_vpc.main

# Remove from state (does NOT destroy real resource)
terraform state rm aws_instance.example

# Pull remote state to local file (backup)
terraform state pull > terraform.tfstate.backup

# Push local state to remote (use carefully)
terraform state push terraform.tfstate

# Force unlock a stuck lock (DANGER: confirm no other apply is running first!)
terraform force-unlock a1b2c3d4-e5f6-7890-abcd-ef1234567890
```

## `import` block (1.5+) — declarative import

Old way (imperative CLI): `terraform import aws_vpc.this vpc-12345` (one-shot, runtime-only).

New way (declarative, in config):

```hcl
import {
  to = aws_vpc.this
  id = "vpc-12345"
}

import {
  to = aws_subnet.private["a"]
  id = "subnet-111"
}

resource "aws_vpc" "this" {
  cidr_block = "10.0.0.0/16"
  # ... match real resource config
}
```

Then `terraform plan` shows the import as a no-op (if config matches). The import block stays in config forever — tracked in Git. Useful for batch-importing resources in code review.

Workflow:
1. Add `import {}` block for each resource to adopt.
2. Run `terraform plan` — Terraform reads each resource into state.
3. Adjust config until plan shows "no changes".
4. Apply — `import` blocks now persisted.
5. Optionally remove `import` blocks after first apply (state has them now).

## `moved` block — refactor without destroy/create

When refactoring resource addresses (renaming, moving into a module), use `moved` so Terraform updates state without destroying the old resource and creating a new one.

```hcl
# Before refactor: aws_instance.app
# After refactor:  module.app.aws_instance.main

moved {
  from = aws_instance.app
  to   = module.app.aws_instance.main
}
```

Terraform sees the `moved` block, updates state, then plans normally. No `terraform state mv` needed.

Multiple moves allowed:

```hcl
moved {
  from = aws_security_group.web
  to   = module.security.aws_security_group.web
}

moved {
  from = aws_security_group.db
  to   = module.security.aws_security_group.db
}
```

After everyone has applied, you can remove the `moved` blocks (they're transient).

## `removed` block (1.7+) — drop from state without destroying

```hcl
removed {
  from = aws_instance.legacy
  lifecycle { destroy = false }
}
```

Useful when you want Terraform to stop managing a resource but leave it running in the cloud.

## Drift Detection

Drift = real-world state diverges from Terraform state. Sources:
- Manual changes via console/CLI (someone "fixed" something)
- Another tool managing the same resource (CloudFormation + Terraform overlap)
- Cloud-side changes (auto-tagging, auto-remediation)

### Detect drift

```bash
# Refresh-only plan: Terraform updates state with real-world values
# and shows what would change as a result. Does NOT apply changes.
terraform plan -refresh-only -out=drift.tfplan

# Show the JSON for parsing
terraform show -json drift.tfplan > drift.json

# Extract drifted resources
jq '.resource_changes[] | select(.change.actions != ["no-op"]) | {address, actions: .change.actions}' drift.json
```

### Scheduled drift check (CI)

```yaml
# .github/workflows/drift-check.yaml
name: Drift Check
on:
  schedule: [{ cron: "0 6 * * *" }]   # daily 6 AM UTC
  workflow_dispatch:
jobs:
  drift:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
        with: { terraform_version: 1.7.0 }
      - run: terraform init -input=false
      - run: terraform plan -refresh-only -out=drift.tfplan -detailed-exitcode
        # exit code 0 = no drift, 2 = drift, 1 = error
      - if: ${{ failure() }}
        run: |
          terraform show -json drift.tfplan > drift.json
          # post to Slack, email, etc.
          jq '.resource_changes[] | select(.change.actions != ["no-op"])' drift.json
```

### Reconciling drift

1. **Investigate** — was the manual change intentional? Did it fix a bug?
2. **If intentional** — update Terraform config to match real state, then `plan` should be clean.
3. **If unintentional** — `terraform apply` to revert real state to match config.
4. **Document** — add a runbook entry; consider SCP/Policy to prevent recurrence.

## State File Security

### Encryption at rest
- S3: enable `encrypt = true` + SSE-KMS with customer-managed key
- Azure Blob: use Microsoft-managed or customer-managed keys
- GCS: enabled by default; use CMEK for stricter control

### Access control
- Restrict state bucket to break-glass role + CI role only
- Use SCP denying all other principals
- Log access via CloudTrail / Azure Monitor / Cloud Audit Logs

### What's in state (be afraid)
- RDS passwords (if not using `sensitive` + random provider)
- TLS private keys (if using `tls_private_key` resource)
- Sensitive input variables (if not marked `sensitive`)
- Resource ARNs / IDs (less sensitive but still useful to attackers)

Mark everything sensitive `sensitive = true` to redact from plan output:

```hcl
variable "db_password" {
  type      = string
  sensitive = true
}

output "db_endpoint" {
  value     = aws_db_instance.main.endpoint
  sensitive = false
}

output "db_password" {
  value     = aws_db_instance.main.password
  sensitive = true
}
```

## State Migration

### Migrate from one backend to another

```bash
terraform init -migrate-state
```

Terraform copies state from old backend to new backend. Confirm both backends are accessible first.

### Reconfigure (don't migrate)

```bash
terraform init -reconfigure
```

Starts fresh with new backend config; doesn't copy state. Useful when switching workspaces or environments.

### Partial backend config (per-env)

```hcl
# backend.tf
terraform {
  backend "s3" {}   # all config via -backend-config file
}
```

```hcl
# config/backend-prod.hcl
bucket         = "terraform-state-prod"
key            = "vpc/terraform.tfstate"
region         = "us-east-1"
encrypt        = true
dynamodb_table = "terraform-lock-prod"
```

```bash
terraform init -backend-config=config/backend-prod.hcl
```

## Best Practices

1. **Remote + locked + encrypted** for any non-trivial state.
2. **One state file per environment per component** (not one giant state).
3. **Separate state per blast radius** — VPC state separate from app state.
4. **Versioned bucket** — instant rollback if state corrupted.
5. **Periodic `state pull` backups** to a second location.
6. **Never commit `.tfstate`** to Git. Add to `.gitignore`.
7. **`prevent_destroy` on critical resources** (state bucket, KMS keys, RDS).
8. **`moved` blocks for refactors** — safer than `state mv`.
9. **`import` blocks for adoption** — reviewable, repeatable.
10. **Scheduled drift checks** — catch manual changes early.
