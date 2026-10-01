# Terragrunt + OpenTofu

## Terragrunt — DRY for Terraform

Terragrunt is a thin wrapper around Terraform that:
1. **Keeps your backend config DRY** — define once, inherit everywhere
2. **Keeps your provider config DRY** — same
3. **Keeps your CLI flags DRY** — common `-var-file` patterns
4. **Supports parallel execution** across many modules (`terragrunt run-all apply`)
5. **Streams apply logs** with module context

Use when: you have 10+ environments or stacks of related modules that share config.

### Install

```bash
brew install terragrunt
# or download from https://github.com/gruntwork-io/terragrunt/releases
```

Terragrunt 0.55+ supports the new `terragrunt.hcl` syntax and `run` command (replaces `apply`/`plan` directly).

### Repository layout

```
infrastructure-live/
├── _envcommon/                    # shared module invocations
│   ├── vpc.hcl
│   └── eks.hcl
├── production/
│   ├── _envcommon -> ../../_envcommon    # symlink or path
│   ├── us-east-1/
│   │   ├── prod-vpc/
│   │   │   └── terragrunt.hcl     # inherits _envcommon/vpc.hcl
│   │   └── prod-eks/
│   │       └── terragrunt.hcl
│   └── eu-west-1/
│       └── prod-vpc/
│           └── terragrunt.hcl
├── staging/
│   └── us-east-1/
│       └── staging-vpc/
│           └── terragrunt.hcl
└── terragrunt.hcl                 # root config (backend, providers)
```

### Root `terragrunt.hcl` — DRY backend

```hcl
# infrastructure-live/terragrunt.hcl
remote_state {
  backend = "s3"
  config = {
    bucket         = "my-tf-state-${get_aws_account_id()}"
    key            = "${path_relative_to_include()}/terraform.tfstate"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "terraform-state-lock"
  }
  generate = {
    path      = "backend.tf"
    if_exists = "overwrite_terragrunt"
  }
}

# Generate provider blocks per env
generate "provider" {
  path      = "provider.tf"
  if_exists = "overwrite_terragrunt"
  contents  = <<EOF
provider "aws" {
  region = "us-east-1"
  default_tags {
    tags = {
      Environment = local.environment
      ManagedBy   = "Terragrunt"
      Project     = "my-project"
    }
  }
}
EOF
}

locals {
  environment = regex("production|staging|dev", get_parent_terragrunt_dir())
}
```

### Per-module `terragrunt.hcl`

```hcl
# production/us-east-1/prod-vpc/terragrunt.hcl
include "root" {
  path = find_in_parent_folders("root.hcl")
}

terraform {
  source = "git::git@github.com:org/tf-modules.git//vpc?ref=v1.3.0"
}

inputs = {
  name       = "prod-vpc"
  cidr_block = "10.0.0.0/16"
  private_subnets = {
    a = { cidr_block = "10.0.1.0/24", az = "us-east-1a" }
    b = { cidr_block = "10.0.2.0/24", az = "us-east-1b" }
    c = { cidr_block = "10.0.3.0/24", az = "us-east-1c" }
  }
}
```

### Shared envcommon pattern

```hcl
# _envcommon/vpc.hcl
terraform {
  source = "git::git@github.com:org/tf-modules.git//vpc?ref=${local.vpc_module_version}"
}

locals {
  vpc_module_version = "v1.3.0"
}

inputs = {
  enable_dns_hostnames = true
  enable_dns_support   = true
  tags                 = { ManagedBy = "Terragrunt" }
}
```

```hcl
# production/us-east-1/prod-vpc/terragrunt.hcl
include "envcommon" {
  path   = "${find_in_parent_folders("_envcommon")}/vpc.hcl"
  expose = true
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  name       = "prod-vpc"
  cidr_block = "10.0.0.0/16"
  private_subnets = {
    a = { cidr_block = "10.0.1.0/24", az = "us-east-1a" }
  }
}
```

Inputs are merged: envcommon provides defaults, env-specific overrides.

### Commands (Terragrunt 0.55+)

```bash
# Plan
terragrunt plan
terragrunt run --all plan      # across all modules

# Apply
terragrunt apply
terragrunt run --all apply     # apply all modules in dependency order

# Plan/apply a single module
cd production/us-east-1/prod-vpc
terragrunt plan

# Apply many modules in parallel
terragrunt run --all apply --terragrunt-parallelism 4

# Destroy
terragrunt destroy

# Output
terragrunt output
```

### Dependency ordering

```hcl
# prod-eks/terragrunt.hcl
dependency "vpc" {
  config_path = "../prod-vpc"
}

inputs = {
  vpc_id     = dependency.vpc.outputs.vpc_id
  subnet_ids = values(dependency.vpc.outputs.private_subnet_ids)
}
```

`terragrunt run --all apply` will apply `prod-vpc` before `prod-eks` automatically.

### Pitfalls

1. **`run --all` with destructive changes** — applies across many modules. Always `--all plan` first.
2. **Circular dependencies** — Terragrunt detects them but they're hard to fix.
3. **State key collision** — every module needs a unique `key`. Use `path_relative_to_include()`.
4. **Forgetting `generate = { if_exists = "overwrite_terragrunt" }`** — generated files get stale.

## OpenTofu — Community Fork

OpenTofu is a Linux Foundation community fork of Terraform, created in 2023 after HashiCorp's BSL license change. It's CLI-compatible and state-compatible.

### Why OpenTofu

| Concern | Terraform (HashiCorp) | OpenTofu |
|---|---|---|
| License | BSL 1.1 (non-OSI) | MPL 2.0 (OSI) |
| Provider compatibility | Yes | Yes (registry mirrors) |
| State file compatibility | Yes | Yes (read/write either) |
| `for_each` on `import` blocks | No (yet) | Yes (1.9+) |
| Native provider functions | Yes | Yes |
| Provider-defined functions | Yes | Yes |
| Ephemeral resources | 1.7+ | 1.8+ |
| Removed state encryption | N/A | Yes (1.8+, optional) |
| Backed by | HashiCorp/IBM | Linux Foundation |

### Install

```bash
# Homebrew
brew install opentofu

# Or download
curl -fsSL https://get.opentofu.org/install/opentofu_install.sh | bash -s -- --install-method standalone
```

### Drop-in replacement

```bash
# Same commands as Terraform
tofu init
tofu plan
tofu apply
tofu import aws_vpc.this vpc-12345
tofu state list
tofu test
```

`.terraform.lock.hcl` is compatible. State files are compatible. Provider registry (OpenTofu has its own mirror of the Terraform Registry) is compatible.

### Switching from Terraform to OpenTofu

```bash
# 1. Uninstall Terraform (or alias)
alias terraform='tofu'   # optional, for muscle memory

# 2. Re-init to pick up OpenTofu's provider mirrors
rm -rf .terraform .terraform.lock.hcl
tofu init

# 3. Plan (should show no changes)
tofu plan

# 4. Continue as before
tofu apply
```

### State encryption (OpenTofu 1.8+)

```hcl
# encryption.tf
terraform {
  encryption {
    key_provider "pbkdf2" "my_key" {
      passphrase = var.encryption_passphrase
    }

    method "aes_gcm" "my_method" {
      keys = key_provider.pbkdf2.my_key
    }

    state {
      method = method.aes_gcm.my_method
    }

    plan {
      method = method.aes_gcm.my_method
    }
  }
}
```

State file at rest is encrypted; passphrase required to read or write.

### OpenTofu-only features (some also backported to Terraform)

- `for_each` and `for` on `import` blocks (1.9+)
- State encryption (1.8+)
- Native provider functions in `terraform console`
- Community governance — RFCs public

### When to pick OpenTofu

| Pick OpenTofu if | Pick Terraform if |
|---|---|
| You want a permissive license (MPL) | You're on Terraform Cloud/Enterprise and need TFC features |
| You're a vendor embedding IaC in a product | You need HashiCorp support contract |
| You want community governance | You need a feature OpenTofu hasn't ported yet (rare) |
| You want state encryption | You want provider-defined ephemeral resources maturity (TFC) |

### When to keep using Terraform

- On Terraform Cloud / Enterprise — no benefit to switching.
- Using Sentinel for policy as code — Sentinel is HashiCorp-proprietary; OpenTofu supports OPA.
- Heavily invested in `terraform`-named CI workflows — alias `tofu` to `terraform` to avoid rewriting.

## Terragrunt + OpenTofu together

```bash
# Use OpenTofu as Terragrunt's underlying binary
export TERRAGRUNT_TFPATH=tofu
terragrunt plan
terragrunt run --all apply
```

Works seamlessly — Terragrunt is engine-agnostic.

## Decision Matrix: Which to Adopt?

| Situation | Recommendation |
|---|---|
| 1-3 environments, simple infra | Plain Terraform, separate state per env |
| 5+ environments with shared modules | Terragrunt for DRY |
| License-sensitive (vendor, OSS, gov) | OpenTofu |
| Existing Terraform Cloud / Enterprise | Stay on Terraform |
| Want state encryption | OpenTofu |
| Multi-cloud, multi-region, large org | Terragrunt + OpenTofu |
| Just learning IaC | Plain Terraform first; add layers as you grow |

## Migration paths

- **Terraform → OpenTofu**: drop-in. Re-init.
- **Terraform → Terragrunt**: wrap existing modules in `terragrunt.hcl` with `terraform { source = "..." }`. State files unchanged.
- **Terragrunt + Terraform → Terragrunt + OpenTofu**: set `TERRAGRUNT_TFPATH=tofu`.

## Summary

- **Terragrunt**: DRY layer; adopt when env count makes raw Terraform repetitive.
- **OpenTofu**: license-fork; adopt when BSL is a concern or you want state encryption.
- Both are CLI-compatible; both can coexist with Terraform workflows.
