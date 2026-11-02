# Security and Signing

Git security is layered: don't commit secrets, scan for them pre-push, sign commits so they're attributable, harden `.gitignore`, and lock down the remote with branch protection + 2FA.

## Never commit secrets

**The rule**: secrets never enter the repo. Not in `.env`, not in `config.js`, not in a comment, not in a test fixture, not in a CI workflow file. Once pushed, treat the secret as leaked regardless of whether you later scrub history.

### Pre-commit secret scanning

Use [pre-commit](https://pre-commit.com/) with secret-detection hooks:

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/pre-commit/pre-commit-hooks
    rev: v4.6.0
    hooks:
      - id: detect-private-key
      - id: check-merge-conflict
      - id: check-added-large-files
        args: ['--maxkb=500']
  - repo: https://github.com/Yelp/detect-secrets
    rev: v1.5.0
    hooks:
      - id: detect-secrets
        args: ['--baseline', '.secrets.baseline']
  - repo: https://github.com/gitleaks/gitleaks
    rev: v8.18.1
    hooks:
      - id: gitleaks
```

Install: `pre-commit install` (sets up `.git/hooks/pre-commit`). Run on all files: `pre-commit run --all-files`.

`detect-secrets` and `gitleaks` are complementary — detect-secrets is good at AWS/API keys, gitleaks covers a broader regex set. Using both is reasonable.

### Baseline for existing secrets

If your repo already has known-false-positives (e.g. test fixtures with fake keys), generate a baseline so the scanner doesn't block on them:

```bash
detect-secrets scan > .secrets.baseline
# Audit and confirm each entry is a false positive:
detect-secrets audit .secrets.baseline
# Commit the baseline. New secrets trip the hook; baseline entries don't.
```

### If a secret was committed

1. **Rotate immediately.** Generate a new key, revoke the old one, update consumers. This is non-negotiable — history scrubbing does NOT un-leak a pushed secret.
2. Then optionally scrub history with `git filter-repo` or BFG (see `references/history-rewriting.md`).
3. Add the secret pattern to pre-commit scanning.
4. Force-push the cleaned history; have teammates re-clone.
5. Notify anyone with a fork or local clone.

## `.gitignore` discipline

```gitignore
# .gitignore at repo root — committed, shared with team

# Secrets
.env
.env.*
!.env.example
*.pem
*.key
id_rsa*
secrets/

# Build artifacts
/dist/
/build/
/target/
node_modules/

# Editor / IDE
.vscode/
!.vscode/extensions.json
!.vscode/settings.json
.idea/

# OS
.DS_Store
Thumbs.db

# Logs / caches
*.log
.cache/
```

Personal editor/OS files belong in **global gitignore**, not the repo:

```bash
git config --global core.excludesfile ~/.gitignore_global
# ~/.gitignore_global:
# .DS_Store
# .idea/
# .vscode/
# *.swp
```

### `.gitignore` gotchas

- **`.gitignore` only affects untracked files.** If a file is already tracked, adding it to `.gitignore` does NOT stop tracking it. Run `git rm --cached <file>` first.
- **Negation `!` only works if the parent directory isn't ignored.** `node_modules/` then `!node_modules/my-pkg/` won't work; use `node_modules/*\n!node_modules/my-pkg/` instead.
- **Don't ignore `.env.example`** — it documents required vars for teammates.
- **Don't commit actual `.env` files.** Use a `.env.example` template and a per-developer `.env` (gitignored).

### Removing a file that should have been ignored

```bash
git rm --cached .env                # remove from index, keep local file
echo ".env" >> .gitignore
git commit -m "chore: stop tracking .env (was already gitignored locally)"
# If the file is already in history with secrets: ROTATE + filter-repo.
```

## Signed commits

### Why sign?

- **Attribution**: a signed commit proves it came from the holder of the key. Without signing, anyone with push access can commit as anyone (configurable per-repo).
- **Supply chain**: GitHub shows "Verified" badge on signed commits. Required for OSS / regulated industries.
- **Audit trail**: signed tags prove a release was made by the claimed maintainer.

### GPG signing

```bash
# 1. Generate a GPG key (if you don't have one):
gpg --full-generate-key
# Choose: RSA and RSA, 4096 bits, no expiry (or 2y).
# Use the SAME email as your git config / GitHub account.

# 2. List keys to get the key ID:
gpg --list-secret-keys --keyid-format=long
# sec   rsa4096/ABCD1234EFGH5678 2024-01-01 [SC]
#                          ^^^^^^^^^^^^^^^^ this part

# 3. Configure Git:
git config --global user.signingkey ABCD1234EFGH5678
git config --global commit.gpgsign true       # sign all commits
git config --global tag.gpgsign true          # sign all tags

# 4. Add the public key to GitHub:
gpg --armor --export ABCD1234EFGH5678 | pbcopy  # or xclip / wl-copy
# Paste at https://github.com/settings/gpg/new

# 5. Sign a commit explicitly (if not enabled globally):
git commit -S -m "feat(x): ..."
# Sign a tag:
git tag -s v1.2.3 -m "Release 1.2.3"
```

### SSH signing (simpler, modern)

Git 2.34+ supports SSH keys for commit signing. Reuse your existing SSH key — no GPG setup.

```bash
# 1. Use an existing SSH key (or generate one):
ssh-keygen -t ed25519 -C "your_email@example.com" -f ~/.ssh/git_signing_key

# 2. Configure Git:
git config --global gpg.format ssh
git config --global user.signingkey ~/.ssh/git_signing_key.pub
git config --global commit.gpgsign true
git config --global tag.gpgsign true

# 3. Add the PUBLIC key to GitHub as a "Signing Key" (NOT auth key):
cat ~/.ssh/git_signing_key.pub | pbcopy
# https://github.com/settings/ssh/new — Key type: "Signing Key"
```

SSH signing is now GitHub's recommended approach — simpler setup, no GPG agent, hardware-backed (YubiKey) supported via `ssh-keygen -t ed25519-sk`.

### Verifying signatures

```bash
git log --show-signature -1                 # show signature on a commit
git verify-commit <sha>                      # verify a commit's signature
git tag -v v1.2.3                            # verify a tag's signature
git log --pretty="format:%h %G? %s" -10      # %G?: G=good, B=bad, U=unknown, N=none
```

On GitHub: "Verified" badge on commits and tags. Branch protection can require signed commits.

## `gh auth` and GitHub CLI

```bash
# Interactive auth (opens browser):
gh auth login
# Choose: GitHub.com → HTTPS or SSH → authenticate via browser → sign commits? Y

# Verify:
gh auth status

# Use a token in CI (set GH_TOKEN env var):
export GH_TOKEN=ghp_xxxxxxxxxxxx
gh auth status          # reads from env

# Manage scopes:
gh auth refresh -s write:packages,read:org

# Switch accounts:
gh auth switch
```

`gh auth login` will configure Git credentials for you (HTTPS credential helper or SSH key upload). For commit signing, it asks separately.

## Branch protection (the real security boundary)

A signed commit is meaningless if anyone can push to `main`. Set up branch protection:

```bash
gh api repos/:owner/:repo/branches/main/protection -X PUT \
  --input - <<'EOF'
{
  "required_status_checks": {
    "strict": true,
    "contexts": ["CI / build", "CI / test"]
  },
  "enforce_admins": true,
  "required_pull_request_reviews": {
    "required_approving_review_count": 1,
    "dismiss_stale_reviews": true,
    "require_code_owner_reviews": true
  },
  "restrictions": null,
  "required_linear_history": true,
  "allow_force_pushes": false,
  "allow_deletions": false,
  "block_creations": false,
  "required_signatures": true
}
EOF
```

Key fields:

- `enforce_admins: true` — admins follow the rules too (otherwise protection is theater).
- `required_status_checks.strict: true` — branches must be up to date with base before merge.
- `required_signatures: true` — commits to this branch must be signed.
- `allow_force_pushes: false` — no `--force` to this branch.
- `required_linear_history: true` — squash-merge or rebase-merge only, no merge commits.

For GitHub Teams/Orgs, also require SSO and 2FA for members.

## Hooks: pre-commit, pre-push, husky, lint-staged

### Native Git hooks

Live in `.git/hooks/` (NOT committed by default). To commit hooks with the repo, use husky or core.hooksPath.

```bash
git config --global core.hooksPath .githooks    # use a committed dir
chmod +x .githooks/pre-commit
```

### husky (npm)

```bash
npm install --save-dev husky lint-staged
npx husky init                                # creates .husky/pre-commit
# .husky/pre-commit:
#!/usr/bin/env sh
npx lint-staged
```

```json
// package.json
{
  "lint-staged": {
    "*.{ts,tsx}": ["eslint --fix", "prettier --write"],
    "*.{json,md,yml}": ["prettier --write"]
  }
}
```

### pre-commit framework (Python, language-agnostic)

`.pre-commit-config.yaml` (see secret scanning above) committed to repo. Each developer runs `pre-commit install` once to set up `.git/hooks/pre-commit`.

### Pre-push hook

```bash
# .githooks/pre-push (or .husky/pre-push):
#!/usr/bin/env sh
npm run test || exit 1
# Or for pre-push secret scanning:
gitleaks protect --staged --redact || exit 1
```

## Anti-patterns

- **`git push --no-verify`** to bypass a failing pre-push hook without a written reason. If the hook has a false positive, fix the hook (or use `git commit --no-verify` ONCE with a comment explaining why).
- **Committing `.env` "just for testing" then deleting.** The blob is in history forever. Rotate.
- **Sharing a GPG/SSH signing key across machines.** One key per machine; revoke lost ones.
- **No expiry on signing keys.** Set 2-year expiry and rotate.
- **Trusting "Verified" badge = "secure".** It only proves the commit was signed by a key associated with the GitHub account. It says nothing about code quality.
- **No branch protection because "we're a small team".** Set it up on day one; retrofitting is painful after a bad push.
- **`git config --global commit.gpgsign false`** to "speed up" commits on a repo that requires signing. Configure per-repo overrides instead.
- **Hardcoding tokens in CI workflow YAML.** Use repo/org secrets, not plaintext.
