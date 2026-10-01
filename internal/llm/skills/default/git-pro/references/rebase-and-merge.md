# Rebase and Merge

Rebase rewrites history. Use it to clean up your own branch before merging. Never use it on commits others have pulled.

## When to rebase vs merge

| Situation | Rebase | Merge | Notes |
|---|---|---|---|
| Updating your local feature branch with latest `main` | ✅ `git pull --rebase` | ⚠️ | Rebase keeps linear history; merge creates a noise commit |
| Integrating a finished feature branch into `main` | ✅ Squash-merge or rebase-merge | ✅ Merge-commit | Team policy decision |
| Syncing with `main` after others have pushed | ✅ `git pull --rebase` | ⚠️ | Merge OK but creates a "Merge origin/main into feature" commit |
| Updating commits others have already pulled | ❌ NEVER | ✅ | Rebase = rewrite = divergence for teammates |
| Combining your WIP commits before a PR | ✅ `rebase -i` | ❌ | Merge can't squash |
| A feature branch with merges from `main` already in it | ⚠️ Use `--rebase-merges` | ✅ | Plain rebase flattens and loses the merge structure |
| Public `main`/`develop`/`release/*` | ❌ NEVER | ✅ | Hard rule |

## The golden rule

**Never rebase commits that exist outside your local machine.** Once pushed to a branch anyone else might pull, those commits are public. Rewriting them creates duplicate commits (old + new SHA) and a merge nightmare for everyone downstream.

If you must rewrite public history (rare, e.g. removing a leaked secret): coordinate with all teammates, force-push, and have everyone `git fetch && git reset --hard origin/<branch>` after confirming they have no uncommitted work.

## `git pull --rebase` is the default for feature branches

Set globally or per-branch:

```bash
git config --global pull.rebase true
git config --global rebase.autoStash true           # stash before, pop after
# Or per branch:
git checkout feature/x
git branch --set-upstream-to=origin/feature/x
git config branch.feature/x.rebase true
```

After `pull.rebase=true`, `git pull` becomes `git pull --rebase` automatically. `rebase.autoStash=true` is the single biggest quality-of-life Git config.

## Interactive rebase

The tool for rewriting your own branch's history: squash, fixup, reword, reorder, drop, split.

```bash
git rebase -i HEAD~5           # last 5 commits
# Or against a base:
git rebase -i origin/main      # everything since forked from main
```

Editor opens with one line per commit:

```
pick   a1b2c3d feat(auth): rate-limit login
pick   b2c3d4e fix(auth): typo in error msg
pick   c3d4e5f docs: update README
pick   d4e5f6g WIP
pick   e5f6g7h feat(auth): add IP allowlist
```

Commands:

| Command | Effect |
|---|---|
| `pick` / `p` | keep as-is |
| `reword` / `r` | keep, but edit commit message |
| `edit` / `e` | pause to amend the commit (split, modify) |
| `squash` / `s` | combine with previous; opens editor to merge messages |
| `fixup` / `f` | combine with previous; discard this commit's message |
| `drop` / `d` | remove the commit |
| reorder lines | reorder commits |
| `exec` / `x` | run shell command after the commit |

### Squash the 5 WIP commits into 1 clean commit
```
pick   a1b2c3d feat(auth): rate-limit login
fixup  b2c3d4e fix(auth): typo in error msg
fixup  c3d4e5f docs: update README
drop   d4e5f6g WIP
fixup  e5f6g7h feat(auth): add IP allowlist
```
Save → one commit `a1b2c3d feat(auth): rate-limit login` with all changes combined.

### Split a commit
```
pick   a1b2c3d feat: too much in one commit
# change "pick" to "edit":
edit   a1b2c3d feat: too much in one commit
```
Save. Git pauses at that commit.
```bash
git reset HEAD^                  # unstage, keep changes in working tree
git add -p                       # stage first logical chunk
git commit -m "feat(x): first half"
git add -A
git commit -m "feat(y): second half"
git rebase --continue
```

### Reorder to put a fixup next to its target
```
pick   a1b2c3d feat(x): first
pick   c3d4e5f feat(y): second
pick   b2c3d4e fixup! feat(x): first     # move this ABOVE its target
```
becomes:
```
pick   a1b2c3d feat(x): first
fixup  b2c3d4e fixup! feat(x): first
pick   c3d4e5f feat(y): second
```
Git has an autocore for this: `git config rebase.autoSquash true` then `git rebase -i --autosquash origin/main` will auto-order commits named `fixup! <subject>` or `squash! <subject>`.

## Conflict during rebase

```bash
git rebase origin/main
# CONFLICT in src/auth.ts
# Edit file, remove <<< === >>> markers, keep the merged result
git add src/auth.ts
git rebase --continue
# Repeat per conflicting commit
# Bail out entirely:
git rebase --abort            # back to pre-rebase state, nothing lost
# Skip a commit that's now empty after rebase:
git rebase --skip
```

`git status` tells you exactly where you are mid-rebase. Read it.

If you find yourself in a rebase hell with 20 commits and repeated conflicts: `git rebase --abort`, then `git merge origin/main` instead. Sometimes a merge is the honest answer.

## `--rebase-merges` (preserve merge structure)

Plain `git rebase` flattens merge commits. If your feature branch contains merges (e.g. you merged `main` into it), `git rebase -i origin/main` will collapse them. Use `--rebase-merges` to preserve merge topology:

```bash
git rebase -i --rebase-merges origin/main
```

## Merge strategies

| Merge type | Command | When |
|---|---|---|
| Fast-forward (default, no merge commit) | `git merge feature/x` (if possible) | Linear history; only works if base hasn't moved |
| Merge-commit (`--no-ff`) | `git merge --no-ff feature/x` | Preserve feature-branch topology on `develop` (Git Flow) |
| Squash-merge | `git merge --squash feature/x && git commit` | One commit per PR; clean main history |
| Rebase-merge | GitHub PR button "Rebase and merge" | Linear history without squashing (preserves PR commits) |

### Squash-merge is the modern default for trunk-based

- One commit per PR on `main` — `git log` reads as a story.
- The PR conversation (review, CI runs) is preserved in GitHub even though the commits are squashed.
- Loses granular commit history within the PR. Acceptable trade-off for short PRs.

### Merge-commit is for long-lived feature branches (Git Flow)

- Preserves "this was a feature branch that got merged" topology.
- Useful for archaeology: `git log --merges --oneline` shows every feature integration.
- Noisier main history.

### Rebase-merge is a compromise

- Linear main history (no merge commits).
- Individual PR commits preserved.
- Each commit must already be clean (CI must pass at every commit, not just the final one).

## `git pull --rebase` vs `git rebase origin/main`

Equivalent in effect, but:

- `git pull --rebase` = `git fetch` + `git rebase origin/<upstream>`. Use when you have an upstream set.
- `git fetch origin && git rebase origin/main` = explicit. Use when rebasing against a non-upstream branch (e.g. `feature/x` against `main` while upstream is `origin/feature/x`).

## Anti-patterns

- **Rebasing `main` because "it's cleaner".** No. `main` is public; you've just rewritten history for everyone.
- **`git pull` (merge) on a feature branch** creating "Merge branch 'main' into feature/x" noise. Set `pull.rebase=true`.
- **Squash-merging a 50-commit PR.** The 50 commits are gone; reviewers couldn't have reviewed them anyway. Split the PR.
- **`git rebase --autostash` without `autoStash=true`** — surprising stash pops on collaborators. Configure per-user, not per-repo.
- **Rebasing across an already-merged branch's commits** — you're rewriting commits that exist on `main` via the squash. Coordinate or revert instead.
