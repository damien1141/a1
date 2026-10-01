---
name: git-pro
description: "Use when committing, branching, rebasing, recovering history, or pushing to shared remotes in Git 2.40+. Produces atomic conventional commits, safe rebases (never on public branches), force-with-lease over force, signed commits, and verifies with git status clean + pre-commit + gh pr checks before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: vcs
  triggers: "git,commit,branch,rebase,merge,conflict,reflog,stash,worktree,submodule,force-push,conventional commits,pull request,cherry-pick,filter-repo,signing,GPG,SSH,hooks"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "rigorous-coding,spec-driven-development,code-reviewer"
---

# Git Pro

Git 2.40+ workflow discipline. Atomic conventional commits, safe rebase (never on public branches), `--force-with-lease` over `--force`, `git filter-repo` over `filter-branch`, signed commits, reflog recovery. The model never claims a push is safe without checking the upstream state. Honest exit separates **VERIFIED** (ran a command, saw clean output) from **ASSUMED** (could not run).

## When to Use

- Writing, splitting, or rewriting commits before they ship
- Choosing a branching strategy for a team or a release
- Rebasing a PR on `main`, resolving conflicts, or recovering a broken rebase
- Force-pushing to a feature branch (NEVER to `main`/`master`/shared release branches)
- Recovering a deleted branch, dropped commit, or bad merge via `reflog`
- Cleaning secrets from history (BFG or `git filter-repo`, NEVER `filter-branch`)
- Setting up hooks (husky, pre-commit, lint-staged), signing commits, or hardening CI

Skip for: trivial one-line edits on a personal branch with no remote, or pure-read operations like `git log` for exploration.

## Operating Loop

1. **Scope** — Name the change and the **branch visibility** (local-only / pushed-to-feature / on-shared-trunk). Branch visibility decides whether rebase and `--force` are even allowed. State the load-bearing unknown: "is `main` here a shared trunk or a release branch?"
2. **Recon** — `git status` (clean tree before structural ops), `git log --oneline -20`, `git branch -vv` (upstream), `git remote -v`, `git stash list`. Confirm upstream branch and whether it is protected. Read `.pre-commit-config.yaml` / `.husky/` / branch protection rules.
3. **Plan the commit graph** — Decide: one commit or many? Each commit must be atomic (one logical change, builds clean, tests pass at HEAD). Pick the branching strategy from `references/branching-strategies.md`. Decide rebase-vs-merge up front; do not flip mid-flight.
4. **Implement** — Stage with intent (`git add -p` for hunks), write the conventional-commit subject (≤50 chars, imperative mood) and a body that explains **why**, not what. Run `pre-commit run --all-files` BEFORE the first commit so hook fixes land in the same commit.
5. **Verify (gate)** — In order, until clean:
   - `git status` → working tree clean
   - `git diff --cached` reviewed before every commit (no stray debug, no secrets)
   - `git log --oneline -10` → history reads as a story
   - `pre-commit run --all-files` → all hooks pass
   - `git push --force-with-lease` (if rebased feature branch) → no rejection
   - `gh pr checks` (if PR open) → all required checks green; `gh pr view --json reviewDecision` → approved before merge
   - If any step fails: fix the cause. No `--no-verify` past a hook without a written reason.
6. **Exit** — Report. **VERIFIED**: list each command + result. **ASSUMED**: list what you believe but did not check (e.g. "teammates have not pulled since my last force-push"). Flag lingering risks (force-push to a shared branch, an unmerged recovery commit).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Branching strategies (trunk-based, GitHub Flow, Git Flow, release branches) + decision table | `references/branching-strategies.md` | Choosing a workflow for a team/release, deciding merge vs rebase policy |
| Rebase vs merge, interactive rebase (squash/reword/reorder), `pull --rebase`, rebase safety | `references/rebase-and-merge.md` | Cleaning up a feature branch before merge, rebasing a PR on main |
| Conflict resolution workflow, `mergetool`, abort/continue, `rerere`, conflict markers | `references/conflict-resolution.md` | Stuck in a merge or rebase with conflicts, repeated identical conflicts |
| History rewriting: `commit --amend`, `rebase -i`, `filter-repo`, BFG | `references/history-rewriting.md` | Squashing old commits, removing secrets, splitting a fat commit |
| Recovery: `reflog`, `reset` (soft/mixed/hard), `restore`, `fsck`, deleted branches | `references/recovery-and-reflog.md` | Recovering a dropped commit, undoing a bad merge/reset, dangling commits |
| Remote ops: push/pull/fetch, upstream tracking, force-push safety, tags, remote branch deletion | `references/remote-operations.md` | Pushing a rebased branch, deleting remote branches, tagging a release, bad-push recovery |
| Security: signed commits (GPG/SSH), `gh auth`, secret scanning, `.gitignore`, never commit secrets | `references/security-and-signing.md` | Setting up commit signing, scanning for secrets, hardening a repo |
| PR/MR workflow: small PRs, reviewable diffs, squash vs merge-commit vs rebase-merge, rebasing on main | `references/pr-workflow.md` | Opening a PR, choosing merge strategy, addressing review with rebase |

## Constraints

### MUST DO
- **Atomic commits**: one logical change per commit. If a commit does two things, split it (`git rebase -i` or reset and re-stage).
- **Conventional commits**: `type(scope): subject` — `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `perf`, `build`, `ci`, `revert`. Subject ≤50 chars, imperative mood, no trailing period. Body explains **why**, wrapped at 72.
- **Review `git diff --cached` before every commit.** No stray `console.log`, no commented-out code, no secrets, no IDE config drift.
- **`--force-with-lease` over `--force`.** Always. Lease checks the remote hasn't moved; bare `--force` clobbers teammates' work silently.
- **Never rebase commits that have been pushed to a shared branch.** Rebase rewrites history; anyone who pulled the old commits gets a divergent tree and a merge nightmare. Rebase only your own feature branch.
- **Never `--force` to `main`/`master`/`release/*`/`develop`.** Branch protection should enforce this; treat it as a hard rule regardless.
- **`git filter-repo` over `git filter-branch`.** `filter-branch` is deprecated, slow, and dangerous. BFG is acceptable for secret removal on a bare clone.
- **`.gitignore` discipline**: ignore build artifacts, editor configs, OS files, and secrets at repo root. Use global gitignore for personal editor/OS files.
- **Recover with `reflog` before panicking.** Almost nothing in Git is truly lost for ~90 days. `git reflog`, find the SHA, `git reset --hard <sha>` (only on a branch you control).
- **Sign commits** on shared/oss repos (GPG or SSH key). Configure once: `git config commit.gpgsign true`.
- **Run `pre-commit run --all-files` before the first commit** so hook fixes (formatters, trailing whitespace, secret scans) land in the commit, not after.
- **Small PRs**: target <300 lines diff, <10 files, one reviewable concern. Split big PRs into a stack.
- **Honest exit**: separate VERIFIED (ran it, saw green) from ASSUMED (believe it, did not check).

### MUST NOT DO
- **No `git push --force` to shared branches** (`main`/`master`/`develop`/`release/*`). Use `--force-with-lease` AND only on your own feature branch. If branch protection blocks you, that's correct — open a PR instead.
- **No `git filter-branch`.** Deprecated and unsafe. Use `git filter-repo` or BFG.
- **No `--no-verify` past a pre-commit/pre-push hook** without a written reason in the commit body. If the hook is wrong, fix the hook.
- **No committing secrets.** API keys, `.env`, private keys, `id_rsa`. If you did: rotate immediately, then `filter-repo`/BFG to scrub history (rewriting history does NOT un-leak a pushed secret — rotate first).
- **No `git reset --hard` on a branch you don't own** without checking `git status` and `git stash list` first. Hard reset discards uncommitted work irreversibly (until reflog, which expires).
- **No `git pull` (merge) on a feature branch.** Set `pull.rebase=true` to avoid "Merge branch 'main' into feature" noise.
- **No `git commit -am "stuff"`.** The message is the contract with your future self and reviewers. Write a real subject + body.
- **No rebasing across a merge commit** without `--rebase-merges` (or you flatten merge structure and lose context).
- **No claiming "pushed and verified" without checking `gh pr checks`** (or the equivalent CI status). Push is not the same as green CI.

## Code Examples

### Atomic commit with conventional message
```bash
git status                              # clean tree before structural ops
git add -p                              # stage hunks intentionally, not whole files
git diff --cached                       # REVIEW before commit — no debug, no secrets
pre-commit run --all-files              # hook fixes land in THIS commit
git commit -m "feat(auth): rate-limit login by IP" -m \
  "Prevents brute-force attacks from a single IP. Uses token bucket" \
  "per IP with 5 req/min burst. Existing users behind NAT may need to" \
  "whitelisted via ALLOWLIST_IPS env. Refs SEC-412."
git log --oneline -3                    # verify history reads as a story
```

### Safe rebase of a local feature branch on latest main
```bash
git fetch origin
git checkout feature/rls-login
git rebase origin/main                  # NEVER rebase commits others have pulled
# If conflicts:
#   edit file, remove <<< === >>> markers
#   git add <file>
#   git rebase --continue
# If overwhelmed:
#   git rebase --abort    (back to pre-rebase state, no harm done)
git push --force-with-lease             # NEVER --force; lease fails if remote moved
```

### Interactive rebase to squash the last 4 commits into one
```bash
git rebase -i HEAD~4
# In editor: leave first as "pick", change others to "squash" (or "fixup" to drop msg)
# Save → second editor opens to write the combined commit message
# If branch is pushed: git push --force-with-lease
# NEVER do this on a branch teammates have already pulled
```

### Recover a deleted branch via reflog
```bash
git reflog                              # find the SHA where the branch tip was
git checkout -b recovered-feature <sha> # recreate branch at that SHA
# If reflog expired (rare): git fsck --lost-found  (dangling commits)
```

## Output Template

When delivering a Git change, provide in this order:

1. **Branch & visibility** — name, upstream tracking, shared or local-only
2. **Commit graph summary** — `git log --oneline` of the affected range
3. **Verification block**:
   ```
   $ git status
   nothing to commit, working tree clean
   $ git log --oneline -5
   7e3a1cb feat(auth): rate-limit login by IP
   2f8d901 fix(auth): trim whitespace in email
   ...
   $ pre-commit run --all-files
   Trim Trailing Whitespace.........................................Passed
   detect-private-key................................................Passed
   $ git push --force-with-lease origin feature/rls-login
   + 7e3a1cb...7e3a1cb feature/rls-login -> feature/rls-login (forced update)
   $ gh pr checks
   Some checks were not successful
   1 failing, 2 successful, 0 skipped, 0 cancelled
   ```
4. **Exit report** — VERIFIED / ASSUMED / lingering risks (e.g. "force-pushed; teammates who pulled the old `feature/rls-login` tip will need `git reset --hard origin/feature/rls-login`")

## Knowledge Reference

Git 2.40+ · atomic commits · conventional commits · `git add -p` · `git diff --cached` · trunk-based · GitHub Flow · Git Flow · release branches · `git rebase -i` (pick/squash/fixup/reword/drop) · `git pull --rebase` · `--rebase-merges` · `git merge --no-ff` · `git mergetool` · `rerere` · `git stash` · `git worktree` · `git reflog` · `git reset` (`--soft`/`--mixed`/`--hard`) · `git restore` · `git fsck --lost-found` · `git cherry-pick` · `git revert -m` · `git filter-repo` · BFG · `git push --force-with-lease` · upstream tracking · `gh pr checks` · branch protection · pre-commit · husky · lint-staged · GPG/SSH signing · `gh auth` · secret scanning · Git LFS · submodules · subtrees
