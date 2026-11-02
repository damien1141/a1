# Recovery and Reflog

Git rarely loses anything. Almost every "lost" commit is recoverable for ~90 days via the reflog, and even longer via `fsck --lost-found`. The trick is knowing where to look.

## The mental model

- Every HEAD movement (commit, checkout, reset, rebase step, merge) appends an entry to the **reflog** — a per-ref journal of where HEAD/branches have pointed.
- Reflog entries expire after ~90 days (or ~30 days if they're "unreachable"; configurable via `gc.reflogExpire`/`gc.reflogExpireUnreachable`).
- `git gc` prunes unreachable objects after `gc.pruneExpire` (default 2 weeks).
- Until pruned, orphaned commits are findable.

## `git reflog`

```bash
git reflog                      # HEAD's reflog (most recent on top)
# 7e3a1cb HEAD@{0}: commit: feat(auth): rate-limit login
# 2f8d901 HEAD@{1}: rebase -i (finish)
# 9c8b7a2 HEAD@{2}: reset: moving to HEAD~3
# a1b2c3d HEAD@{3}: commit: WIP before reset    ← this is the "lost" commit

git reflog show feature/x       # per-branch reflog
git reflog show --date=iso      # with timestamps
```

Each line: `<sha> <ref>@{<n>}: <op>: <message>`. To recover, just check out the SHA:

```bash
git checkout -b recovered a1b2c3d
# Or reset an existing branch tip back to it:
git checkout feature/x
git reset --hard a1b2c3d
```

## `git reset` modes

| Mode | HEAD moves | Index | Working tree | Use case |
|---|---|---|---|---|
| `--soft` | ✅ to target | unchanged | unchanged | "I want to re-commit these staged changes differently" |
| `--mixed` (default) | ✅ to target | reset to target | unchanged | "I want to unstage and re-pick what to commit" |
| `--hard` | ✅ to target | reset to target | reset to target | "Throw it all away, go back to that state" |

`--hard` is the dangerous one — it discards uncommitted work. Reflog still recovers the prior HEAD if you commit before reset, but uncommitted changes are gone.

### Patterns

```bash
# "I just did a bad commit, undo it but keep changes":
git reset --soft HEAD~1          # changes are now staged; re-commit differently

# "I just did a bad commit, undo and unstage":
git reset HEAD~1                 # --mixed is default; changes are in working tree, unstaged

# "I just did 5 bad commits, throw them away":
git reset --hard HEAD~5          # DANGER: uncommitted work in those files is also lost
# Safer: stash first, then reset, then inspect
git stash --include-untracked
git reset --hard HEAD~5
git stash pop                    # if you want the WIP back

# "I accidentally merged main into my feature branch, undo the merge":
git reset --hard ORIG_HEAD       # ORIG_HEAD is set by merge/rebase/reset to the previous HEAD
```

`ORIG_HEAD` is a convenient pointer set by `merge`, `rebase`, `reset`, and other "dangerous" ops to the previous HEAD. Use it before the next dangerous op overwrites it.

## `git restore` (the modern alternative for partial resets)

Git 2.23+ split `checkout` into `switch` (branches) and `restore` (files). For file-level recovery:

```bash
git restore --staged src/file.ts          # unstage (like `git reset src/file.ts`)
git restore --worktree src/file.ts        # discard working-tree changes (DANGER: unrecoverable if uncommitted)
git restore --source=HEAD~3 src/file.ts   # restore file content from 3 commits ago
git restore --source=feature/x src/file.ts # restore from another branch
git restore --staged --worktree src/file.ts  # full reset of file to HEAD
```

## `git switch` (modern branch ops)

```bash
git switch feature/x           # checkout existing branch
git switch -c feature/y        # create and checkout
git switch -c feature/y origin/main   # create from origin/main
git switch -                    # previous branch (like cd -)
git switch --detach a1b2c3d     # detached HEAD at a SHA (for inspection)
```

## Recovering a deleted branch

```bash
git branch -D feature/x         # oops, deleted
git reflog show feature/x       # find the SHA where it was
# c3d4e5f feature/x@{0}: commit: last work before delete
git checkout -b feature/x c3d4e5f   # recreate at that SHA
# Or shorter:
git branch feature/x c3d4e5f
```

If the branch reflog is gone (gc'd), use `fsck`.

## `git fsck` (dangling commits)

When reflog entries have expired but objects haven't been pruned yet:

```bash
git fsck --lost-found
# dangling commit a1b2c3d...
# dangling commit b2c3d4e...
# Each "dangling commit" is a commit no ref currently points to.
git show a1b2c3d                # inspect what it contains
git log --oneline a1b2c3d -10   # see its history
git checkout -b recovered a1b2c3d
```

`fsck` is slower than reflog but recovers commits reflog has forgotten. Run it in the `.git` directory if `--lost-found` doesn't surface them; objects may still be in `.git/objects/` until `gc --prune=now` removes them.

## Recovering from specific disasters

### "I did `git reset --hard` and lost uncommitted work"

If the work was never staged/committed, it's gone (Git never tracked it). If it was staged at some point, the blob is in `.git/objects` and `git fsck --lost-found` can find it. Use `git show <sha>` to inspect.

### "I force-pushed the wrong branch and clobbered remote `main`"

```bash
# On remote main, reflog is your friend:
git reflog show origin/main      # if you have it locally
# Find the SHA before your bad force-push
git push --force-with-lease origin <good-sha>:main
# If branch protection blocks force-push to main (it SHOULD), you need an admin override.
# Communicate the incident: anyone who pulled during the bad window has the bad state.
```

### "I did a bad merge and pushed it"

Prefer revert (forward-only, safe):
```bash
git revert -m 1 <merge-sha>     # -m 1 = keep first parent (the trunk)
git push origin main
```
Only force-push if: solo repo, OR you've confirmed no one pulled, AND branch protection allows it.

### "I committed a secret"

1. **Rotate the secret first.** This is non-negotiable. Pushed secrets are leaked.
2. Then optionally scrub history (see `references/history-rewriting.md`).
3. Add the secret pattern to pre-commit secret scanning so it doesn't recur.

### "I accidentally did `git push --force` to a shared branch"

1. Tell the team immediately.
2. Recover the prior tip via reflog on whoever has it (you, if you just force-pushed from your machine).
3. Force-push the good SHA back.
4. Everyone who pulled in between needs `git fetch && git reset --hard origin/<branch>`.

### "My rebase went wrong and I'm 20 commits in with conflicts"

```bash
git rebase --abort              # back to pre-rebase state, no harm
# Then either:
git merge origin/main           # merge instead of rebase
# Or: split the rebase into smaller chunks (HEAD~5 instead of HEAD~20)
```

### "I deleted a tag"

```bash
git reflog show <tag>           # if it was just deleted
# Or recover from any clone that still has it:
git fetch origin <tag>          # if remote still has it
# Or recreate from a known SHA:
git tag v1.2.3 <sha>
git push origin v1.2.3
```

## `git cherry-pick` (selective recovery)

Apply a specific commit from another branch:

```bash
git cherry-pick a1b2c3d                # apply that commit on current branch
git cherry-pick a1b2c3d b2c3d4e c3d4e5f # multiple
git cherry-pick a1b2c3d..c3d4e5f        # range (exclusive start, inclusive end)
# Conflict:
git cherry-pick --continue             # after resolving
git cherry-pick --abort                # bail out
git cherry-pick --skip                 # skip this commit, move to next
```

Use for: backporting a fix to a release branch, recovering a single commit from a deleted branch. **Do not use as a primary integration strategy** — it duplicates commits across branches and `git log` archaeology becomes harder.

## `git stash` (save WIP without committing)

Stash = "save my working tree and index, then reset to HEAD". Use when you need to switch branches but aren't ready to commit.

```bash
git stash                          # stash tracked changes (not untracked)
git stash -u                       # also stash untracked files (--include-untracked)
git stash -a                       # also stash ignored files (--all) — rarely wanted
git stash push -m "WIP: rate-limit bug"  # named stash
git stash --keep-index             # stash working tree but keep staged changes
git stash -- <path>                # stash only specific files

# Inspect:
git stash list                     # stash@{0}, stash@{1}, ...
git stash show -p stash@{0}        # diff of a stash

# Restore:
git stash pop                      # apply stash@{0} AND drop it (conflict keeps the stash)
git stash apply                    # apply stash@{0} but keep it in the list
git stash apply stash@{2}          # apply a specific stash
git stash apply --index            # also restore staged state (not just working tree)

# Remove:
git stash drop stash@{0}           # delete one
git stash clear                    # delete ALL (irreversible)
```

Stashes are recoverable via reflog (`git reflog` shows stash entries). But `git stash clear` + `git gc` will prune them.

**Gotcha**: `git stash pop` with a conflict keeps the stash in the list (so you don't lose it). Resolve the conflict, `git add`, `git stash drop`.

**When NOT to stash**: long-running WIP (>1 day) belongs on a real branch (`git checkout -b wip/rate-limit`) where you can commit, push, and back it up. Stashes live on one machine and are lost if the disk fails.

## `git worktree` (parallel branches without cloning)

A worktree is an additional working directory linked to the same repo. Use to work on two branches simultaneously without stashing.

```bash
git worktree add ../repo-feature-x feature/x
# Now ../repo-feature-x is a working dir on branch feature/x
# Sharing the same .git/ objects as the original repo.

git worktree add -b hotfix/v1.2.4 ../repo-hotfix v1.2.3   # new branch off a tag
git worktree add --detach ../repo-inspect a1b2c3d         # detached HEAD at a SHA

git worktree list                  # all worktrees
# /home/me/repo              a1b2c3d [main]
# /home/me/repo-feature-x   b2c3d4e [feature/x]

# Clean up:
git worktree remove ../repo-feature-x   # delete the worktree dir + link
git worktree prune                       # remove metadata for deleted worktree dirs
```

Use cases:

- Review a teammate's PR while keeping your WIP untouched in the main worktree.
- Run a long test/build on one branch while editing another.
- Keep a `main` worktree clean for builds while developing in a feature worktree.

**Constraints**:

- The same branch cannot be checked out in two worktrees simultaneously (Git prevents it).
- Worktrees share `.git` objects — `git gc` in one affects all.
- Don't delete a worktree dir with `rm -rf`; use `git worktree remove` so Git cleans up metadata.

## Anti-patterns

- **Panicking and running `git gc --prune=now`** to "clean up". This expires the reflog and prunes dangling objects — destroying your recovery path. Never `gc --prune=now` to "fix" a mess.
- **`git reset --hard` without checking `git status` first.** Uncommitted work is gone forever.
- **Deleting `.git/` to "start fresh".** You lose the entire history and the reflog. Use `git reset --hard` and a fresh clone if needed.
- **Relying on `ORIG_HEAD` more than one op back.** It's overwritten by each dangerous op. Use `reflog` for multi-step recovery.
- **Skipping `--force-with-lease`** because `--force` "always worked before". One teammate push later, you've overwritten their work.
- **Stashing for >1 day** instead of committing to a WIP branch. Stashes are local-only; a branch can be pushed and backed up.
- **`rm -rf` on a worktree directory** instead of `git worktree remove`. Leaves stale metadata; fix with `git worktree prune`.
- **`git stash clear`** when confused. List and apply first; clear is irreversible.
