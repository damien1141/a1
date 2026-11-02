# Testing & Policy as Code

## Testing Layers

| Layer | Tool | What | Speed |
|---|---|---|---|
| Static (format, validate) | `terraform fmt`, `terraform validate` | Syntax, types | Seconds |
| Lint | `tflint`, `tfsec` | Style, provider-specific rules, security | Seconds |
| Unit (config logic) | `terraform test` (built-in, 1.6+) | Module outputs match expectations | Seconds-minutes |
| Integration | Terratest (Go), pytest-terraform (Python) | Apply + verify real cloud resources | Minutes |
| Policy | OPA/Conftest, Checkov, Sentinel | Security, compliance, tagging rules | Seconds |
| Cost | Infracost | $ impact of changes | Seconds |
| Drift | Scheduled `plan -refresh-only` | Real-world vs state | Minutes |

## `terraform test` (1.6+)

Built-in test framework — runs `plan` or `apply` and asserts on outputs.

### File structure

```
modules/vpc/
├── main.tf
├── variables.tf
├── outputs.tf
└── tests/
    └── vpc_test.tftest.hcl
```

### Unit test (plan only)

```hcl
# tests/vpc_test.tftest.hcl
run "validate_vpc_cidr" {
  command = plan

  variables {
    name       = "test-vpc"
    cidr_block = "10.0.0.0/16"
  }

  assert {
    condition     = aws_vpc.this.cidr_block == "10.0.0.0/16"
    error_message = "VPC CIDR block did not match"
  }

  assert {
    condition     = aws_vpc.this.enable_dns_hostnames == true
    error_message = "DNS hostnames should be enabled by default"
  }
}

run "validate_tags" {
  command = plan

  variables {
    name       = "test-vpc"
    cidr_block = "10.0.0.0/16"
    tags       = { Environment = "test" }
  }

  assert {
    condition     = aws_vpc.this.tags["Environment"] == "test"
    error_message = "Environment tag not set"
  }
}

run "reject_invalid_cidr" {
  command = plan

  variables {
    name       = "test-vpc"
    cidr_block = "not-a-cidr"
  }

  # Expect failure
  expect_failures = [var.cidr_block]
}
```

### Integration test (apply)

```hcl
# tests/integration_test.tftest.hcl
run "create_full_stack" {
  command = apply

  variables {
    name       = "integration-test"
    cidr_block = "10.0.0.0/16"
    private_subnets = {
      a = { cidr_block = "10.0.1.0/24", az = "us-east-1a" }
    }
  }

  assert {
    condition     = length(aws_subnet.private) == 1
    error_message = "Should create one private subnet"
  }

  assert {
    condition     = output.vpc_id != ""
    error_message = "VPC ID should not be empty"
  }
}
```

### Run

```bash
terraform test                                    # all tests
terraform test tests/vpc_test.tftest.hcl          # specific file
terraform test -verbose                           # detailed output
terraform test -no-cleanup                        # keep resources for debugging
```

## Terratest (Go-based integration testing)

For real cloud resources + assertions via cloud SDKs.

### Setup

```
modules/vpc/
├── examples/complete/
│   └── main.tf           # uses the module
└── tests/
    ├── go.mod
    └── vpc_test.go
```

```go
// tests/go.mod
module github.com/org/tf-modules/tests

go 1.22

require (
    github.com/gruntwork-io/terratest v0.46.0
    github.com/stretchr/testify v1.9.0
)
```

### Basic test

```go
// tests/vpc_test.go
package test

import (
    "testing"
    "time"

    "github.com/gruntwork-io/terratest/modules/terraform"
    "github.com/stretchr/testify/assert"
)

func TestVPCCreation(t *testing.T) {
    t.Parallel()

    opts := &terraform.Options{
        TerraformDir: "../examples/complete",
        Vars: map[string]interface{}{
            "name":       "terratest-vpc",
            "cidr_block": "10.0.0.0/16",
        },
        EnvVars: map[string]string{
            "AWS_DEFAULT_REGION": "us-east-1",
        },
        MaxRetries:         3,
        TimeBetweenRetries: 5 * time.Second,
    }

    // Clean up after test
    defer terraform.Destroy(t, opts)

    // Apply
    terraform.InitAndApply(t, opts)

    // Assert
    vpcID := terraform.Output(t, opts, "vpc_id")
    assert.NotEmpty(t, vpcID)

    vpcCIDR := terraform.Output(t, opts, "vpc_cidr_block")
    assert.Equal(t, "10.0.0.0/16", vpcCIDR)
}
```

### Advanced test with AWS SDK verification

```go
func TestVPCConfiguration(t *testing.T) {
    t.Parallel()
    awsRegion := "us-east-1"

    opts := &terraform.Options{
        TerraformDir: "../examples/complete",
        Vars: map[string]interface{}{
            "name":       "terratest-vpc",
            "cidr_block": "10.0.0.0/16",
        },
    }
    defer terraform.Destroy(t, opts)
    terraform.InitAndApply(t, opts)

    vpcID := terraform.Output(t, opts, "vpc_id")

    // Verify via AWS SDK
    vpc := aws.GetVpcById(t, vpcID, awsRegion)
    assert.Equal(t, "10.0.0.0/16", *vpc.CidrBlock)
    assert.True(t, *vpc.EnableDnsSupport)
    assert.True(t, *vpc.EnableDnsHostnames)

    // Verify tags
    tags := aws.GetTagsForVpc(t, vpcID, awsRegion)
    assert.Equal(t, "terratest-vpc", tags["Name"])
    assert.Equal(t, "Terraform", tags["ManagedBy"])
}
```

### Run

```bash
cd tests
go mod download
go test -v -timeout 30m -run TestVPC
```

## Policy as Code

### OPA / Conftest

Write policies in Rego; test against JSON plan.

**`policy/tagging.rego`**
```rego
package terraform

# Deny if any created resource is missing required tags
deny[msg] {
    r := input.resource_changes[_]
    r.change.actions[_] == "create"
    required := ["Environment", "ManagedBy", "Owner"]
    missing := required[_]
    not r.change.after.tags[missing]
    msg := sprintf("resource %s missing required tag: %s", [r.address, missing])
}

# Deny if S3 bucket is created without encryption
deny[msg] {
    r := input.resource_changes[_]
    r.type == "aws_s3_bucket"
    r.change.actions[_] == "create"
    not r.change.after.server_side_encryption_configuration
    msg := sprintf("S3 bucket %s must have encryption enabled", [r.address])
}

# Deny if RDS is public
deny[msg] {
    r := input.resource_changes[_]
    r.type == "aws_db_instance"
    r.change.after.publicly_accessible
    msg := sprintf("RDS instance %s must not be publicly accessible", [r.address])
}

# Deny if Security Group opens 0.0.0.0/0 on sensitive ports
deny[msg] {
    r := input.resource_changes[_]
    r.type == "aws_security_group"
    ingress := r.change.after.ingress[_]
    ingress.cidr_blocks[_] == "0.0.0.0/0"
    sensitive_ports := [22, 3306, 5432, 6379, 27017]
    ingress.from_port in sensitive_ports
    msg := sprintf("SG %s opens sensitive port %d to 0.0.0.0/0", [r.address, ingress.from_port])
}

# Warn if no tags at all
warn[msg] {
    r := input.resource_changes[_]
    r.change.actions[_] == "create"
    not r.change.after.tags
    msg := sprintf("resource %s has no tags", [r.address])
}
```

**Run:**
```bash
# Convert plan to JSON
terraform plan -out=tfplan
terraform show -json tfplan > tfplan.json

# Test against policies
conftest test tfplan.json --namespace terraform --policy policy/

# Output as JSON
conftest test tfplan.json --policy policy/ --output json
```

### Checkov

Built-in policies for AWS/Azure/GCP. 1000+ checks out of the box.

```bash
# Scan plan
checkov -f tfplan.json --framework terraform_plan

# Scan source code (no plan needed)
checkov -d . --framework terraform

# Skip specific checks (with justification in comments)
checkov -d . --skip-check CKV_AWS_18,CKV_AWS_138

# Output as JSON
checkov -d . --output json > checkov.json
```

Common Checkov IDs:
| ID | Check |
|---|---|
| CKV_AWS_18 | S3 bucket has access logging |
| CKV_AWS_19 | S3 bucket encryption enabled |
| CKV_AWS_338 | S3 bucket default encryption AES256 |
| CKV_AWS_138 | RDS backup retention ≥ 7 |
| CKV_AWS_17 | RDS not publicly accessible |
| CKV_AWS_23 | Security group no 22 to 0.0.0.0/0 |
| CKV_AWS_57 | S3 bucket has versioning |
| CKV_AWS_158 | CloudWatch log group retention |
| CKV_AWS_300 | IAM policy not too permissive |

Skip inline:
```hcl
#checkov:skip=CKV_AWS_18:Access logging intentionally disabled for ephemeral test bucket
resource "aws_s3_bucket" "test" {
  bucket = "test-bucket"
}
```

### Sentinel (Terraform Cloud / Enterprise)

HCL-like policy language. Runs in Terraform Cloud.

```sentinel
# require-tags.sentinel
import "tfplan/v2" as tfplan

required_tags = ["Environment", "ManagedBy", "Owner"]

taggable_resources = [
    "aws_instance",
    "aws_s3_bucket",
    "aws_db_instance",
    "aws_vpc",
    "aws_subnet",
]

main = rule {
    all tfplan.resource_changes as _, rc {
        rc.type in taggable_resources implies
        all required_tags as _, tag {
            rc.change.after.tags contains tag
        }
    }
}
```

Enforced in Terraform Cloud workspace policy sets.

## TFLint

```bash
brew install tflint
tflint --init
```

**`.tflint.hcl`**
```hcl
plugin "terraform" {
  enabled = true
  preset  = "recommended"
}

plugin "aws" {
  enabled = true
  version = "0.29.0"
  source  = "github.com/terraform-linters/tflint-ruleset-aws"
}

rule "terraform_naming_convention" {
  enabled = true
  format  = "snake_case"
}

rule "terraform_required_version"     { enabled = true }
rule "terraform_required_providers"   { enabled = true }
rule "terraform_deprecated_interpolation" { enabled = true }
rule "terraform_deprecated_lookup"    { enabled = true }
rule "aws_instance_invalid_type"      { enabled = true }
rule "aws_s3_bucket_encryption"       { enabled = true }
```

```bash
tflint --recursive
tflint --format=json
```

## tfsec (also Snyk IaC)

Static security scanner — faster than Checkov, fewer features.

```bash
brew install tfsec
tfsec .
tfsec . --format json
tfsec . --exclude AWS058,AWS079
```

## Pre-commit Hooks

**`.pre-commit-config.yaml`**
```yaml
repos:
  - repo: https://github.com/antonbabenko/pre-commit-terraform
    rev: v1.89.0
    hooks:
      - id: terraform_fmt
      - id: terraform_validate
      - id: terraform_tflint
        args: [--args=--config=__GIT_WORKING_DIR__/.tflint.hcl]
      - id: terraform_checkov
        args: [--args=--quiet, --args=--skip-check, CKV_AWS_*]
      - id: terraform_docs
        args: [--args=--path-to-file=README.md]
      - id: terraform_tfsec
```

```bash
pip install pre-commit
pre-commit install
pre-commit run -a
```

## CI Pipeline (complete)

```yaml
# .github/workflows/terraform.yml
name: Terraform CI
on: { pull_request: { paths: ['terraform/**'] } }

jobs:
  validate:
    runs-on: ubuntu-latest
    defaults: { run: { working-directory: terraform } }
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
        with: { terraform_version: 1.7.0 }
      - run: terraform fmt -check -recursive
      - run: terraform init -input=false
      - run: terraform validate
      - run: tflint --recursive
      - run: terraform test
      - run: terraform plan -out=tfplan -input=false
      - run: terraform show -json tfplan > tfplan.json
      - run: conftest test tfplan.json --policy policy/
      - run: checkov -f tfplan.json --framework terraform_plan
      - uses: infracost/infracost-action@v3
        with:
          path: tfplan
          terraform_plan_terraform_cli: terraform
        env:
          INFRACOST_API_KEY: ${{ secrets.INFRACOST_API_KEY }}
```

## Cost Estimation (Infracost)

```bash
brew install infracost
infracost auth login

# Estimate cost of a plan
terraform plan -out=tfplan
infracost breakdown --path tfplan --format json --out-file infracost.json

# Show diff (PR comment friendly)
infracost diff --path tfplan --compare-to main.tfplan
```

Outputs monthly cost impact — useful for PR review.

## Best Practices

1. **Run `terraform validate` and `tflint` on every commit** (pre-commit hook).
2. **Run `terraform test` in CI** for module logic.
3. **Run Terratest** for critical modules (VPC, EKS, RDS).
4. **Policy-gate every plan** with OPA/Checkov/Sentinel.
5. **Cost-estimate every PR** with Infracost.
6. **Drift-check nightly** with scheduled `plan -refresh-only`.
7. **Block merges on `deny`** from policy engines.
8. **Document skipped checks** inline with justification comments.
9. **Tag everything** — enforced by policy.
10. **Pin policy versions** in CI for reproducibility.
