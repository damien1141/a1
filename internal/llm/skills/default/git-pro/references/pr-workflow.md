# PR / MR Workflow

A PR is a code review artifact, not just a merge mechanism. Small, reviewable PRs with clean diffs merge faster, find more bugs, and produce better history than large "do everything" PRs.

## Small PRs

Target:

- **<300 lines diff** (excl. generated/lockfile). Hard cap ~1000 lines.
- **<10 files**. Caps the reviewer's cognitive load.
- **One reviewable concern**. One feature, one fix, one refactor — not all three.
- **Builds + tests pass at every commit** (for rebase-merge) OR at the final commit (for squash-merge).

If a PR is too big, split it. Patterns:

- **Stacked PRs**: PR #2 depends on PR #1. Branch `feature/b` is based on `feature/a`. Merge `a` first, rebase `b` on `main`, then merge `b`. Use [Graphite](https://graphite.dev/) or `gh` to manage stacks.
- **Feature flags**: ship the scaffolding behind a flag (off), then enable in a follow-up PR.
- **Refactor-then-feature**: PR #1 extracts the interface, PR #2 adds the new implementation.

## Writing the PR description

```markdown
## What
One-paragraph summary of what this PR changes.

## Why
The problem this solves. Link to issue/ticket: `Closes #412`.

## How
Key design decisions. "Used token bucket per IP because..."
Mention anything a reviewer should pay attention to:
- "The migration in 0042_add_ip_column.rb is reversible."
- "I deliberately did NOT change the rate-limit config format — that's a follow-up."

## Testing
- `pytest tests/test_rate_limit.py -q` → 14 passed
- Manual: `curl -X POST localhost:3000/login` 10× → 9× 200, 1× 429
- `gh pr checks` all green

## Risk
- Existing users behind NAT may hit the limit. Mitigation: ALLOWLIST_IPS env.
- Performance: bucket lookup adds ~0.1ms per request (measured).

## Rollback
Revert this commit. The IP column is harmless if unused.
```

## Reviewable diffs

Help reviewers by structuring the diff:

- **Pure refactors in their own commit** (no behavior change) — easier to skim.
- **Test additions before behavior changes** — reviewer sees the test fail then pass.
- **Avoid unrelated reformatting** in a behavior-change PR. Run the formatter separately.
- **Split generated files** (lockfiles, protobuf, openapi) into their own commit so reviewers can skip them.

## Merge strategies on GitHub

| Strategy | Effect on `main` history | Use when |
|---|---|---|
| **Create a merge commit** (`--no-ff`) | Adds a merge commit; preserves all PR commits + merge topology | Git Flow-style; you want "this was a PR" topology in `git log` |
| **Squash and merge** | One commit on `main`, message = PR title + description | Trunk-based default; clean linear history; one commit per PR |
| **Rebase and merge** | PR commits rebased onto `main` (no merge commit); each commit preserved | You want linear history AND individual PR commits preserved; commits must be clean |

### Modern default: squash-merge

- `git log --oneline` on `main` reads as a story: one line per PR.
- Each commit message = the PR title (which you control, can be conventional commit format).
- Loses in-PR commit granularity, but PR conversation is preserved on GitHub.
- Set `required_linear_history: true` in branch protection to enforce linear `main`.

### When to use merge-commit instead

- **Git Flow** — `develop` integrates feature branches with `--no-ff` to preserve topology.
- **Audit-heavy industries** — the merge commit's "Merged PR #412" message and metadata are valuable.
- **PRs with multiple meaningful commits** that you want preserved (rare; usually squash is better).

### When to use rebase-merge

- **PRs where each commit is already clean** and meaningful on its own (e.g. a series of refactors then a feature).
- Reviewers should be able to `git log` the PR commits and see a coherent story.
- **Do NOT use** if the PR has "WIP" / "fix typo" / "address review" commits — squash instead.

## Rebasing a PR on `main`

When `main` has moved since you opened the PR:

```bash
# On the PR branch:
git fetch origin
git rebase origin/main
# Resolve conflicts (see references/conflict-resolution.md)
git push --force-with-lease

# Or via GitHub UI: "Update branch" → if there are no conflicts, GitHub does a merge commit.
# Prefer rebase locally to keep history linear.
```

**Communicate force-pushes to active reviewers.** A force-push invalidates their pending review comments (GitHub shows them as "outdated"). Tell them "rebasing, will re-request review when done".

## Addressing review feedback

Two valid approaches:

### A. Add fixup commits, then squash before merge
```bash
git add <fixed files>
git commit --fixup <original-sha>          # or: -m "fixup! <original subject>"
git push
# Before merge, autosquash:
git rebase -i --autosquash origin/main
git push --force-with-lease
# Then squash-merge the PR — clean history.
```

### B. Amend the original commit (if PR has only one commit)
```bash
git add <fixed files>
git commit --amend --no-edit
git push --force-with-lease
```

Avoid adding "Address review feedback" as a separate commit if you'll squash-merge anyway — it's noise that gets squashed out, but it makes the diff harder to review mid-flight.

## `gh` CLI for PR workflow

```bash
# Open a PR from current branch:
gh pr create --base main --title "feat(auth): rate-limit login by IP" --body-file pr-description.md
# With labels, reviewers, assignees:
gh pr create --base main --fill --reviewer alice,bob --label security --assignee @me

# Check status:
gh pr status
gh pr view --web                    # open in browser
gh pr checks                        # CI status
gh pr view --json reviewDecision,state,mergeable

# Re-request review after changes:
gh pr comment <num> --body "Rebased, ready for re-review"
gh pr ready <num>                   # mark draft as ready

# Merge:
gh pr merge <num> --squash --delete-branch --subject "feat(auth): rate-limit login by IP (#412)"
gh pr merge <num> --merge            # merge-commit
gh pr merge <num> --rebase           # rebase-merge

# Close without merging:
gh pr close <num> --delete-branch
```

## Draft PRs

Open as draft to signal "not ready for review, but CI is running":

```bash
gh pr create --draft ...
# Mark ready:
gh pr ready <num>
```

Use drafts for: early CI signal, design feedback on direction, "I'm 80% done, want eyes on approach". Don't leave drafts open for weeks — they accumulate merge debt.

## Code owners

`.github/CODEOWNERS` routes review requests automatically:

```
# Default owners:
*                       @alice @bob

# Per-path:
/src/auth/              @security-team
/src/payments/          @payments-team @finance-reviewer
/docs/                  @docs-team
/package.json           @release-managers
```

Combine with branch protection's `require_code_owner_reviews: true` so changes to `auth/` cannot merge without a security-team approval.

## Stacked PRs (advanced)

For a multi-step feature where each step depends on the prior:

```bash
git checkout -b feature/scaffold main
# ... commit, push, open PR #1
git checkout -b feature/impl feature/scaffold
# ... commit, push, open PR #2 (base: feature/scaffold)
git checkout -b feature/polish feature/impl
# ... commit, push, open PR #3 (base: feature/impl)
```

After PR #1 merges:
```bash
git checkout feature/impl
git fetch origin
git rebase origin/main
git push --force-with-lease
gh pr edit <pr-2-num> --base main   # retarget PR #2 to main
# Repeat for PR #3 after #2 merges.
```

Tools like Graphite, `gh-stack`, or `spr` automate this dance. Otherwise it's manual but tractable for 2-3 PR stacks.

## CI on PRs

- **Required status checks** in branch protection: at minimum `build`, `test`, `lint`. Add `security-scan`, `code-coverage`, `e2e` as relevant.
- **`strict: true`** (require branches up to date) — forces a rebase before merge if `main` moved. Reduces "passed CI 3 days ago, but main has 50 new commits" risk.
- **Auto-cancel stale PR runs** — GitHub does this by default; saves CI minutes.

```bash
gh pr checks --watch            # live-watch current PR's checks
gh pr checks <num> --required   # only required checks
```

## Anti-patterns

- **"Big bang" PRs** with 50 files and 3000 lines. Split it.
- **PR descriptions that say "see commit messages"** — reviewer now has to read N commit messages. Summarize in the PR body.
- **Force-pushing during active review** without telling reviewers. Their pending comments become "outdated" and may be lost.
- **Squash-merging a PR with WIP/typo commits** without rewriting the squashed message. The squashed commit message becomes garbage. Always edit the squash message before confirming.
- **Marking a PR "approved" on your own PR** — many repos forbid self-approval. Use draft + comment instead.
- **Skipping CI by merging via admin override** when checks fail. If CI is flaky, fix the flakiness; don't override.
- **Letting "mergeable: false" PRs sit** — rebase them. Stale PRs diverge further from main and become harder to merge.
- **Rebase-merging a PR with fixup commits** — the fixup commits hit `main` as garbage. Use squash-merge instead, or autosquash first.
