# Plan/Apply Workflow — Day-to-Day Operations

## Standard Workflow

```bash
# 1. Format check (run before commit)
terraform fmt -recursive
terraform fmt -check -recursive   # CI gate

# 2. Init (download providers, modules, configure backend)
terraform init -input=false

# 3. Validate (syntax + types + provider schema)
terraform validate

# 4. Plan to a file
terraform plan -out=tfplan -input=false

# 5. Convert plan to JSON for policy / cost analysis
terraform show -json tfplan > tfplan.json

# 6. Policy gate (OPA/Checkov/Sentinel)
conftest test tfplan.json --namespace terraform
checkov -f tfplan.json --framework terraform_plan

# 7. Cost estimate (optional)
infracost breakdown --path tfplan --format json --out-file infracost.json

# 8. Present plan summary to user; wait for explicit approval

# 9. Apply the saved plan
terraform apply -input=false tfplan

# 10. Verify (refresh-only plan should be no-op)
terraform plan -refresh-only
```

## Reading a Plan

Plan output:

```
Terraform used the selected providers to generate the following execution plan.
Resource actions are indicated with the following symbols:
  + create
  ~ update in-place
-/+ destroy and then create replacement
  - destroy

Terraform will perform the following actions:

  # aws_instance.web will be created
  + resource "aws_instance" "web" {
      + ami           = "ami-0c55b159cbfafe1f0"
      + id            = (known after apply)
      + instance_type = "t3.micro"
      + tags          = { Name = "web" }
    }

  # aws_db_instance.main will be updated in-place
  ~ resource "aws_db_instance" "main" {
      ~ backup_retention_period = 1  -> 7
        id                      = "db-abc123"
        # (10 unchanged attributes hidden)
    }

  # aws_security_group.legacy will be destroyed
  # (because it is no longer in the configuration)
  - resource "aws_security_group" "legacy" {
      - id          = "sg-abc123" -> null
      - name        = "legacy"    -> null
      - vpc_id      = "vpc-xyz"   -> null
    }

Plan: 1 to add, 1 to change, 1 to destroy.
```

### Risk levels

| Symbol | Risk | When to flag |
|---|---|---|
| `+ create` | Low | Always safe |
| `~ update in-place` | Medium | Usually safe; check changed attributes |
| `+/- create before destroy` | Medium | Brief downtime; check dependencies |
| `-/+ destroy and recreate` | **High** | Stateful resources risk data loss |
| `- destroy` | **High** | Permanent deletion |

**Critical**: `force-replace` (`-/+`) on RDS instances, EBS volumes, S3 buckets = data loss. Always call out explicitly.

## Inspecting the JSON plan

```bash
terraform show -json tfplan > tfplan.json
```

```bash
# Summarize creates / updates / destroys / replacements
jq -r '
  .resource_changes[]
  | . as $rc
  | $rc.change.actions[]
  | if . == "create" then "+ \($rc.address)"
    elif . == "update" then "~ \($rc.address)"
    elif . == "delete" then "- \($rc.address)"
    elif . == "create-delete" then "-/+ \($rc.address)"
    else . end
' tfplan.json

# Find any force-replacements (most dangerous)
jq -r '
  .resource_changes[]
  | select(.change.actions == ["create","delete"])
  | .address
' tfplan.json

# Find all destroyed resources
jq -r '
  .resource_changes[]
  | select(.change.actions == ["delete"])
  | .address
' tfplan.json

# List sensitive values being changed
jq -r '
  .resource_changes[]
  | .change.before_sensitive, .change.after_sensitive
  | select(. != null)
' tfplan.json
```

## Plan with variable files

```bash
# Single tfvars
terraform plan -var-file="production.tfvars" -out=tfplan

# Multiple tfvars (later overrides earlier)
terraform plan \
  -var-file="common.tfvars" \
  -var-file="production.tfvars" \
  -out=tfplan

# Inline vars
terraform plan -var="region=us-east-1" -out=tfplan

# Auto-loaded: terraform.tfvars, *.auto.tfvars
terraform plan    # picks up these automatically
```

## Targeted plans (use with caution)

```bash
# Plan only one resource (debugging; NOT for production applies)
terraform plan -target=aws_vpc.main -out=tfplan

# Multiple targets
terraform plan -target=module.vpc -target=module.security -out=tfplan
```

⚠️ Targeting can cause drift because Terraform ignores the rest of config. Use only for debugging; never commit `-target` to CI.

## Refresh-only plans (drift detection)

```bash
# Refresh state without applying any changes
terraform plan -refresh-only -out=drift.tfplan

# Exit codes (with -detailed-exitcode):
#   0 = no changes
#   1 = error
#   2 = changes (drift exists)
terraform plan -refresh-only -detailed-exitcode
```

Use in scheduled CI to detect drift:

```bash
if ! terraform plan -refresh-only -detailed-exitcode; then
  echo "Drift detected!"
  terraform show -json drift.tfplan > drift.json
  # alert team
fi
```

## Apply options

```bash
# Apply a saved plan (safest; plan is immutable)
terraform apply tfplan

# Apply with auto-approve (CI; only after policy gates)
terraform apply -auto-approve

# Apply with parallelism limit (large states)
terraform apply -parallelism=10 tfplan

# Apply with lock timeout (wait for lock instead of failing)
terraform apply -lock-timeout=5m tfplan

# Destroy plan
terraform plan -destroy -out=destroy.tfplan
terraform apply destroy.tfplan
```

## Apply JSON output parsing

```bash
terraform apply -auto-approve -json tfplan | tee apply.json | jq -r '.type + ": " + .message'

# Count created / updated / destroyed
jq -s '
  group_by(.type)
  | map({type: .[0].type, count: length})
' apply.json
```

## Targeted applies for emergencies

If a single resource needs to be re-created without touching others:

```bash
terraform taint aws_instance.web       # 0.12-1.x deprecated; use apply -replace
terraform apply -replace="aws_instance.web"
```

## Error Recovery Patterns

### State drift

```bash
# Diagnose
terraform plan -refresh-only -out=drift.tfplan
terraform show -json drift.tfplan | jq '.resource_changes[] | select(.change.actions != ["no-op"])'

# If real-world is wrong → terraform apply to revert
terraform apply drift.tfplan

# If Terraform config is wrong → fix config, re-plan, apply
```

### Provider auth errors

```
Error: error configuring Terraform AWS Provider: no valid credential sources
```

Fix:
```bash
# AWS
export AWS_PROFILE=production
aws sts get-caller-identity   # verify
terraform plan

# OIDC (GitHub Actions → AWS)
# Use aws-actions/configure-aws-credentials with role-to-assume

# Azure
az login --service-principal -u $ARM_CLIENT_ID -p $ARM_CLIENT_SECRET --tenant $ARM_TENANT_ID

# GCP
gcloud auth application-default login
```

### Dependency / unknown value errors

```
Error: Cycle: aws_instance.a, aws_instance.b
```

Fix:
- Add explicit `depends_on` to break cycle
- Restructure: outputs should flow forward; never reverse-reference
- Use `terraform graph > graph.dot` to visualize

### Locked state

```
Error: Error acquiring the state lock: ConditionalCheckFailedException
```

Fix:
```bash
# 1. Check no other apply is running (Slack, CI dashboard)
# 2. If safe, force unlock
terraform force-unlock <lock-id>
```

⚠️ **NEVER** `force-unlock` if another apply is actually running — it'll corrupt state.

### Stuck on import

```
Error: Cannot import non-existent remote object
```

Fix:
- Verify the resource ID exists: `aws ec2 describe-vpcs --vpc-ids vpc-12345`
- Verify the right region: `export AWS_REGION=us-east-1`
- For 1.5+ `import` blocks: ensure the `resource` block matches real config

## CI/CD Pipeline (GitHub Actions)

```yaml
name: Terraform
on:
  pull_request:
    paths: ['terraform/**']
  push:
    branches: [main]
    paths: ['terraform/**']

jobs:
  plan:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
      id-token: write    # for OIDC
    defaults: { run: { working-directory: terraform } }
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
        with:
          terraform_version: 1.7.0
          cli_config_credentials_token: ${{ secrets.TF_API_TOKEN }}

      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::123:role/gh-actions-tf
          aws-region: us-east-1

      - run: terraform fmt -check -recursive
      - run: terraform init -input=false
      - run: terraform validate
      - run: tflint --recursive
      - run: terraform plan -out=tfplan -input=false
      - run: terraform show -json tfplan > tfplan.json
      - run: conftest test tfplan.json --namespace terraform
      - run: checkov -f tfplan.json --framework terraform_plan

      - name: Comment plan on PR
        if: github.event_name == 'pull_request'
        uses: actions/github-script@v7
        with:
          script: |
            const plan = require('fs').readFileSync('tfplan.json', 'utf8');
            const summary = /* parse + format */;
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: `## Terraform Plan\n\`\`\`\n${summary}\n\`\`\``
            });

  apply:
    needs: plan
    if: github.ref == 'refs/heads/main' && github.event_name == 'push'
    runs-on: ubuntu-latest
    environment: production    # requires manual approval (GitHub Environments)
    steps:
      - uses: actions/checkout@v4
      - uses: hashicorp/setup-terraform@v3
        with: { terraform_version: 1.7.0 }
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::123:role/gh-actions-tf
          aws-region: us-east-1
      - run: terraform init -input=false
      - run: terraform plan -out=tfplan -input=false
      - run: terraform apply -input=false tfplan
```

## Atlantis / Terraform Cloud / Spacelift

These tools bring the plan/apply workflow into PR comments:

- **Atlantis** (self-hosted): runs `terraform plan` on PR open, comments with output; `atlantis apply` triggers apply on merge
- **Terraform Cloud / Enterprise**: hosted plan/run UI with审批 workflows
- **Spacelift**: similar, with drift detection + policy as code built-in

## Common Pitfalls

1. **`apply -auto-approve` without prior plan saved to file** — different plan might run. Always `apply tfplan`.
2. **Forgetting `-input=false` in CI** — Terraform waits for input forever.
3. **Not pinning provider versions in CI** — `terraform init -upgrade` silently changes providers.
4. **Trusting `terraform plan` in CI alone** — always also gate with OPA / Checkov.
5. **Reviewing only the summary line** — `1 to add, 1 to change, 1 to destroy` hides the actual changes.
6. **Forgetting `prevent_destroy` on critical resources** — accidental `terraform destroy` wipes data.
7. **Mixing `terraform import` (CLI) with `import {}` blocks** — pick one approach per resource.
8. **Targeted applies in CI** — they leave drift; ban them in pipelines.
