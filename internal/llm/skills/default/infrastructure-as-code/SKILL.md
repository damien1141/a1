---
name: infrastructure-as-code
description: Implements infrastructure as code with Terraform 1.7+ (and OpenTofu/Terragrunt) across AWS, Azure, GCP. Use for remote state backends, modules, workspaces, plan/apply workflow, import, drift detection, `moved` blocks, `for_each`, `lifecycle`, and policy as code (Sentinel/OPA/Checkov). Produces validated, plan-reviewed, policy-gated infrastructure with explicit approval gates before any destructive apply.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: devops
  triggers: Terraform, OpenTofu, Terragrunt, IaC, infrastructure as code, terraform plan, terraform apply, terraform import, terraform state, terraform module, workspaces, drift, moved block, for_each, lifecycle, Sentinel, OPA, conftest, checkov, tflint
  role: specialist
  scope: implementation
  output-format: code
  related-skills: cloud-native, sre-reliability, system-architecture
---

# Infrastructure as Code

## When to Use

- Provisioning cloud resources across AWS/Azure/GCP with reviewable, version-controlled changes
- Building reusable Terraform modules with versioned interfaces
- Migrating state backends, splitting states, or adopting workspaces
- Importing existing cloud resources into Terraform management
- Detecting and reconciling configuration drift
- Enforcing policy as code (tagging, encryption, public-access, region restrictions) before apply
- Evaluating OpenTofu (community fork) or Terragrunt (DRY layer) for an organization

## Operating Loop

1. **Analyze requirements** — what cloud resources, what environments, what interfaces with existing modules.
2. **Author/modify modules** — keep modules single-purpose; validate every input with `validation` blocks; pin `required_version = ">= 1.7.0"`.
3. **Configure backend** — remote state with locking + encryption (S3+DynamoDB / Azure Blob / GCS / Terraform Cloud).
4. **Validate** — `terraform fmt -check`, `terraform validate`, then `tflint --recursive`. Fix everything reported before proceeding.
5. **Plan** — `terraform plan -out=tfplan` and review the summary, especially `~ destroy` and `+/-` (force-replace). If plan fails (state drift, auth, dependency errors), follow error recovery below; re-validate before re-planning.
6. **Policy gate** — run `conftest test tfplan.json` (OPA) and/or `checkov -f tfplan.json` against policy files. Block on `deny`.
7. **Approve** — present plan summary to the user with destructive changes called out explicitly. Refuse to apply without explicit approval.
8. **Apply** — `terraform apply tfplan`. Confirm outputs.
9. **Verify** — drift-check (`terraform plan -refresh-only`) the next day; verify tags, encryption, and IAM with cloud-native audit commands.

### Error Recovery

| Failure | Fix |
|---|---|
| `terraform validate` errors | Fix syntax/types → re-run `validate` → repeat until clean |
| `tflint` warnings | Address each rule violation → re-run tflint |
| State drift (plan shows unexpected diffs) | `terraform plan -refresh-only` to reconcile state; or `terraform state rm` / `terraform import` for specific resources |
| Provider auth errors | Verify env vars / `~/.aws/credentials` / OIDC; `terraform init -upgrade` if plugins stale |
| Dependency / unknown value errors | Add explicit `depends_on`; restructure so outputs flow forward; re-plan |
| Locked state (failed apply) | `terraform force-unlock <id>` only after confirming no other apply is running |
| `Error: resource already exists` | `terraform import <addr> <id>` to adopt; then re-plan |

After any fix, return to step 4 (validate) before re-planning.

### Validation Gates

| Gate | Command | When |
|---|---|---|
| Format | `terraform fmt -check -recursive` | Pre-commit + CI |
| Validate | `terraform validate` | Pre-merge |
| Lint | `tflint --recursive` | Pre-merge |
| Plan | `terraform plan -out=tfplan && terraform show -json tfplan > tfplan.json` | Pre-apply (every PR) |
| Policy (OPA) | `conftest test tfplan.json --namespace terraform` | Pre-apply |
| Security scan | `checkov -f tfplan.json --framework terraform_plan` | Pre-apply |
| Test | `terraform test` (built-in, 1.6+) | Pre-merge for modules |
| Cost estimate | `terraform plan -out=tfplan && infracost breakdown --path tfplan` | Pre-apply (every PR) |

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| State backends, locking, workspaces, import, drift, `moved` blocks | `references/state-backends.md` | Setting up state, importing resources, handling drift |
| Module patterns, `for_each`, `lifecycle`, `dynamic`, conditional resources | `references/modules.md` | Authoring reusable modules |
| Plan/apply workflow, error recovery, target, refresh-only | `references/plan-apply-workflow.md` | Day-to-day operations |
| Testing (`terraform test`, Terratest), policy as code (OPA/Conftest/Checkov/Sentinel), TFLint | `references/testing-policy.md` | CI/CD gates |
| Terragrunt (DRY) + OpenTofu (fork) | `references/terragrunt-opentofu.md` | Scaling across many envs, evaluating license-fork |

## Constraints

### MUST DO
- Pin `required_version = ">= 1.7.0"` and pin every provider version with `~>` constraints.
- Use remote state with locking + encryption for any non-trivial environment. Never commit `terraform.tfstate`.
- Validate every input variable with `validation` blocks and document with `description`.
- Use `for_each` over `count` for resources with map/object inputs (avoids index-shift bugs on deletion).
- Tag every taggable resource; enforce required tags via `default_tags` provider block + OPA policy.
- Mark sensitive variables `sensitive = true`; never put secrets in plain text or `default`.
- Use `moved` blocks when refactoring resource addresses (e.g. into a module) to preserve state without destroy/create.
- Run `terraform plan` and require explicit human approval before `apply`; call out `destroy` and `force-replace` actions explicitly.
- Run policy as code (OPA or Sentinel) in CI; block merges on `deny`.
- Use `lifecycle { prevent_destroy = true }` on critical stateful resources (S3 state buckets, RDS instances, KMS keys).

### MUST NOT DO
- Commit `.terraform/`, `*.tfstate`, `*.tfstate.backup`, or `terraform.tfvars` containing secrets.
- Use `terraform apply -auto-approve` in CI without a human-approved plan file.
- Use `count` for resources with mutable keys (use `for_each`).
- Use `latest` for provider versions; pin with `~>`.
- Use `terraform state rm` to "fix" drift without understanding why drift occurred.
- Use `force-unlock` in production without confirming no apply is running.
- Mix provider versions across modules without `required_providers` constraints.
- Create circular module dependencies or pass resources (instead of their IDs) between modules.

## Code Examples

### Module structure (full module in `references/modules.md`)

```
modules/vpc/
├── main.tf          # resource definitions
├── variables.tf     # inputs (with validation)
├── outputs.tf       # outputs
├── versions.tf      # required_version = ">= 1.7.0" + required_providers
└── README.md
```

### `for_each` + `lifecycle` (full patterns in `references/modules.md`)

```hcl
locals {
  buckets = {
    logs = { retention = 90 }
    data = { retention = 365 }
  }
}

resource "aws_s3_bucket" "this" {
  for_each = local.buckets
  bucket   = "${var.name}-${each.key}"
  tags     = merge(var.tags, { Name = "${var.name}-${each.key}" })
}

resource "aws_s3_bucket" "state" {
  bucket = "my-tf-state"
  lifecycle { prevent_destroy = true }   # protect state bucket
}
```

### Remote backend (S3, 1.10+ native lockfile)

```hcl
terraform {
  required_version = ">= 1.7.0"
  backend "s3" {
    bucket       = "my-tf-state"
    key          = "env/prod/vpc/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true   # 1.10+ native lockfile; replaces DynamoDB
  }
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.40" }
  }
}
```

### `moved` block — refactor without destroy/create

```hcl
# When refactoring aws_instance.app -> module.app.aws_instance.main
moved {
  from = aws_instance.app
  to   = module.app.aws_instance.main
}
```

Terraform reads this during plan and updates state in place — no destroy + create.

### Drift detection (CI job)

```bash
terraform plan -refresh-only -out=drift.tfplan
terraform show -json drift.tfplan > drift.json
jq '.resource_changes[] | select(.change.actions != ["no-op"])' drift.json
```

## Output Template

```
## IaC Change

### Module
- Path: <modules/xxx>
- Version: <X.Y.Z>
- Provider: aws ~> <5.x>, azurerm ~> <4.x>, google ~> <5.x>

### Backend
- Type: <s3 | azurerm | gcs | cloud>
- Locking: <dynamodb | native | yes>
- Encryption: <on>

### Plan summary
- + create: <N>
- ~ update: <N>
- - destroy: <N>          ← call out explicitly
- +/- force-replace: <N>  ← call out explicitly

### Validation gates
- fmt -check ✓
- validate ✓
- tflint ✓
- conftest ✓  (policy: <namespace>)
- checkov ✓   (skipped: <CKV_*>)

### Approver
- Plan reviewed by: <name>
- Destructive changes accepted: <yes/no>

### Verified vs Assumed
- VERIFIED: validate, plan, policy gates pass; state clean (no drift)
- ASSUMED: <list cost/perf claims not yet benchmarked>
```

## Knowledge Reference

- Terraform 1.7+ features: `terraform test`, `import` block (1.5+), `moved` block (1.1+), `removed` block (1.7+), `ephemeral` resources (1.7+), native S3 lockfile (1.10+)
- OpenTofu 1.6+ community fork (MPL license); CLI-compatible drop-in
- Terragrunt 0.55+ for DRY, multiple envs, `terragrunt.hcl` inheritance
- OPA / Conftest / Checkov / tfsec / Sentinel policy engines
- State backends: S3+DynamoDB, Azure Blob (auto-lock), GCS (auto-lock), Terraform Cloud, Atlantis, Spacelift
- Cost estimation: Infracost, Terraform Cloud Cost Estimation
- Module registries: Terraform Registry, OCI registry (1.x), private Git
