# Terraform Module Patterns

## Module Anatomy

```
modules/vpc/
├── main.tf           # resource definitions
├── variables.tf      # input declarations (with validation)
├── outputs.tf        # outputs (with descriptions)
├── versions.tf       # required_version + required_providers
├── README.md         # usage docs
├── examples/
│   └── complete/
│       └── main.tf   # tests + docs
└── tests/
    └── vpc_test.tftest.hcl
```

## Composing Modules

```hcl
module "network" {
  source = "./modules/vpc"

  name       = "production"
  cidr_block = "10.0.0.0/16"
  private_subnets = {
    a = { cidr_block = "10.0.1.0/24", az = "us-east-1a" }
    b = { cidr_block = "10.0.2.0/24", az = "us-east-1b" }
  }
  tags = local.common_tags
}

module "security" {
  source = "./modules/security-groups"

  vpc_id = module.network.vpc_id       # output flows forward, no cycle

  security_groups = {
    web = {
      ingress = [{ from_port = 443, to_port = 443, protocol = "tcp", cidr_blocks = ["0.0.0.0/0"] }]
    }
  }
}

module "app" {
  source = "./modules/app"

  subnet_ids        = module.network.private_subnet_ids
  security_group_id = module.security.group_ids["web"]
}
```

## `for_each` vs `count`

### `for_each` for map inputs (preferred)

```hcl
variable "buckets" {
  type = map(object({
    retention = number
    versioned = bool
  }))
  default = {
    logs  = { retention = 90,  versioned = true }
    data  = { retention = 365, versioned = true }
  }
}

resource "aws_s3_bucket" "this" {
  for_each = var.buckets
  bucket   = "${var.name}-${each.key}"
}

# Reference: aws_s3_bucket.this["logs"]
```

Removing `logs` from the map removes only that bucket — others unaffected.

### `count` for boolean / list (use carefully)

```hcl
resource "aws_nat_gateway" "this" {
  count = var.enable_nat ? 1 : 0
  # ...
}
```

⚠️ Pitfall: `count` resources are indexed (`aws_nat_gateway.this[0]`). If you remove the middle item from a list, all subsequent items shift. Use `for_each` for mutable collections.

### When `count` is OK

- Boolean enable/disable (one resource)
- Immutable list of N identical resources (e.g. one IAM user per AZ, never re-ordered)

## `dynamic` blocks — repeat nested blocks

```hcl
variable "ingress_rules" {
  type = list(object({
    from_port   = number
    to_port     = number
    protocol    = string
    cidr_blocks = list(string)
    description = string
  }))
  default = []
}

resource "aws_security_group" "this" {
  name   = var.name
  vpc_id = var.vpc_id

  dynamic "ingress" {
    for_each = var.ingress_rules
    content {
      from_port   = ingress.value.from_port
      to_port     = ingress.value.to_port
      protocol    = ingress.value.protocol
      cidr_blocks = ingress.value.cidr_blocks
      description = ingress.value.description
    }
  }

  dynamic "egress" {
    for_each = var.egress_rules
    content {
      from_port   = egress.value.from_port
      to_port     = egress.value.to_port
      protocol    = egress.value.protocol
      cidr_blocks = egress.value.cidr_blocks
    }
  }
}
```

## Conditional resources

```hcl
# Create NAT gateway only if enabled (count = 0 or 1)
resource "aws_nat_gateway" "this" {
  count           = var.enable_nat_gateway ? 1 : 0
  allocation_id   = aws_eip.nat[0].id
  subnet_id       = aws_subnet.public[0].id
  depends_on      = [aws_internet_gateway.this]
}

# for_each with empty map when disabled (cleaner pattern)
resource "aws_route53_zone" "private" {
  for_each = var.create_private_zone ? { main = var.domain_name } : {}
  name     = each.value
  vpc { vpc_id = aws_vpc.this.id }
}
```

The `for_each` with `{}` is cleaner than `count = 0` because there are no index-shift bugs.

## `lifecycle` meta-arg

```hcl
resource "aws_db_instance" "main" {
  # ...

  lifecycle {
    # Recreate the resource before destroying (zero-downtime)
    create_before_destroy = true

    # Refuse to destroy (protect production data)
    prevent_destroy = true

    # Ignore changes from external tools (e.g. autoscaler changing tags)
    ignore_changes = [
      tags["LastModified"],
      tags["ModifiedBy"],
    ]

    # Replace resource when this triggers
    replace_triggered_by = [
      aws_iam_role.app.arn
    ]

    # Don't trigger replacement for these changes (1.9+)
    precondition {
      condition     = var.environment == "production"
      error_message = "Production DB must have Multi-AZ enabled."
    }
  }
}
```

### When to use each

| Setting | Use when |
|---|---|
| `prevent_destroy = true` | Critical stateful resources (DB, state bucket, KMS key) |
| `create_before_destroy = true` | Resources with strict naming requirements (security groups in ASG) |
| `ignore_changes` | External tools mutate attributes (k8s controller, autoscaler) |
| `replace_triggered_by` | Cascade replacement when a dependency changes |
| `precondition` | Validate complex invariants before apply |
| `postcondition` | Verify outputs after apply (1.7+) |

## Validation blocks

```hcl
variable "environment" {
  type = string
  validation {
    condition     = contains(["dev", "staging", "production"], var.environment)
    error_message = "environment must be one of: dev, staging, production."
  }
}

variable "instance_count" {
  type = number
  validation {
    condition     = var.instance_count > 0 && var.instance_count <= 100
    error_message = "instance_count must be between 1 and 100."
  }
}

variable "cidr_block" {
  type = string
  validation {
    condition     = can(cidrhost(var.cidr_block, 0))
    error_message = "Must be a valid IPv4 CIDR (e.g. 10.0.0.0/16)."
  }
}

variable "domain_name" {
  type = string
  validation {
    condition     = can(regex("^[a-z0-9-]+(\\.[a-z0-9-]+)+$", var.domain_name))
    error_message = "Must be a valid domain name."
  }
}
```

## Locals — DRY without modules

```hcl
locals {
  name_prefix = "${var.project_name}-${var.environment}"

  common_tags = {
    Environment = var.environment
    ManagedBy   = "Terraform"
    Project     = var.project_name
    CostCenter  = var.cost_center
    Owner       = var.owner_email
  }

  # Environment-specific sizing
  instance_type = {
    production  = "t3.large"
    staging     = "t3.medium"
    development = "t3.micro"
  }

  selected_instance_type = local.instance_type[var.environment]
  enable_multi_az        = var.environment == "production"
}

resource "aws_instance" "app" {
  instance_type = local.selected_instance_type
  tags          = merge(local.common_tags, { Name = "${local.name_prefix}-app" })
}
```

## Data sources — don't hardcode

```hcl
# Bad: hardcoded AMI
resource "aws_instance" "web" {
  ami = "ami-0c55b159cbfafe1f0"  # will go stale
}

# Good: dynamic lookup
data "aws_ami" "amazon_linux_2023" {
  most_recent = true
  owners      = ["amazon"]
  filter { name = "name"; values = ["al2023-ami-*-x86_64"] }
}

resource "aws_instance" "web" {
  ami           = data.aws_ami.amazon_linux_2023.id
  instance_type = "t3.micro"
}
```

## Provider `default_tags` — apply once, tag everything

```hcl
provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Environment = var.environment
      ManagedBy   = "Terraform"
      Project     = var.project_name
      CostCenter  = var.cost_center
    }
  }
}

# Now every aws_* resource inherits these tags automatically.
# Per-resource tags are merged in.
resource "aws_s3_bucket" "this" {
  bucket = var.name
  tags   = { Name = var.name }   # + Environment, ManagedBy, Project, CostCenter
}
```

## Module versioning & sourcing

```hcl
# Public Registry
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.5.0"           # pin to specific version
}

# Major-only constraint (auto-updates minors)
module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 20.0"         # >= 20.0, < 21.0
}

# Git tag
module "custom" {
  source = "git::https://github.com/org/tf-modules.git//vpc?ref=v1.2.3"
}

# Git branch (avoid in production)
module "custom" {
  source = "git::https://github.com/org/tf-modules.git//vpc?ref=main"
}

# Local path
module "vpc" {
  source = "./modules/vpc"
}

# OCI registry (1.x)
module "vpc" {
  source = "oci://registry.example.com/modules/vpc"
  version = "1.2.0"
}
```

**Production**: always pin to specific version (e.g. `5.5.0`) or `~>` constraint. Dependabot/Renovate can open PRs to bump.

## Module outputs

```hcl
output "vpc_id" {
  description = "ID of the VPC"
  value       = aws_vpc.this.id
}

output "private_subnet_ids" {
  description = "Map of private subnet IDs by key"
  value       = { for k, v in aws_subnet.private : k => v.id }
}

output "db_password" {
  description = "Database password (sensitive)"
  value       = aws_db_instance.main.password
  sensitive   = true
}

output "connection_string" {
  description = "DB connection string"
  value       = "postgres://${var.db_user}:${aws_db_instance.main.password}@${aws_db_instance.main.endpoint}/mydb"
  sensitive   = true
  # don't forget to mark sensitive if it contains a secret!
}
```

## Anti-patterns to avoid

1. **God modules** — module that creates VPC + EKS + RDS + K8s manifests. Split.
2. **`count.index` in resource names** — index-shift bug on delete. Use `for_each` with stable keys.
3. **Hardcoded AMIs / regions / account IDs** — use data sources and variables.
4. **`depends_on` on modules** — modules already depend via their inputs/outputs.
5. **Variables without `description`** — your team will hate you.
6. **Variables without `validation`** — garbage-in, garbage-out.
7. **Resources without tags** — invisible to FinOps.
8. **Secrets in `default` values** — secrets must come from secret managers, not tfvars.
9. **`terraform apply -auto-approve`** in CI without human-approved plan.
10. **Using `count = 0` to disable a resource when you should use `for_each = {}`**.

## Example: well-structured module (vpc)

```hcl
# modules/vpc/main.tf
resource "aws_vpc" "this" {
  cidr_block           = var.cidr_block
  enable_dns_hostnames = var.enable_dns_hostnames
  enable_dns_support   = var.enable_dns_support
  tags                 = merge(var.tags, { Name = var.name })
}

resource "aws_subnet" "private" {
  for_each = var.private_subnets
  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value.cidr_block
  availability_zone = each.value.az
  tags = merge(var.tags, {
    Name = "${var.name}-private-${each.key}"
    Tier = "private"
  })
}

resource "aws_subnet" "public" {
  for_each = var.public_subnets
  vpc_id            = aws_vpc.this.id
  cidr_block        = each.value.cidr_block
  availability_zone = each.value.az
  map_public_ip_on_launch = true
  tags = merge(var.tags, {
    Name = "${var.name}-public-${each.key}"
    Tier = "public"
  })
}

resource "aws_internet_gateway" "this" {
  count  = var.enable_internet_gateway ? 1 : 0
  vpc_id = aws_vpc.this.id
  tags   = merge(var.tags, { Name = "${var.name}-igw" })
}

resource "aws_nat_gateway" "this" {
  for_each = var.enable_nat_gateway ? var.public_subnets : {}
  allocation_id = aws_eip.nat[each.key].id
  subnet_id     = aws_subnet.public[each.key].id
  depends_on    = [aws_internet_gateway.this]
  tags          = merge(var.tags, { Name = "${var.name}-nat-${each.key}" })
}

resource "aws_eip" "nat" {
  for_each = var.enable_nat_gateway ? var.public_subnets : {}
  domain   = "vpc"
  tags     = merge(var.tags, { Name = "${var.name}-nat-eip-${each.key}" })
}
```

```hcl
# modules/vpc/outputs.tf
output "vpc_id" {
  description = "ID of the VPC"
  value       = aws_vpc.this.id
}

output "private_subnet_ids" {
  description = "Map of private subnet IDs by key"
  value       = { for k, v in aws_subnet.private : k => v.id }
}

output "public_subnet_ids" {
  description = "Map of public subnet IDs by key"
  value       = { for k, v in aws_subnet.public : k => v.id }
}

output "vpc_cidr_block" {
  description = "CIDR block of the VPC"
  value       = aws_vpc.this.cidr_block
}
```

```hcl
# modules/vpc/versions.tf
terraform {
  required_version = ">= 1.7.0"
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.40" }
  }
}
```
