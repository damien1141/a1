# History Rewriting

Rewriting history = changing commit SHAs. Safe on local-only branches. Dangerous on pushed branches. Catastrophic on shared trunks without coordination.

**Golden rule**: only rewrite commits no one else has. If a branch is pushed AND a teammate might have pulled it, treat it as public.

## `git commit --amend` (the smallest rewrite)

Fix the most recent commit (message, files, or both):

```bash
# Fix the commit message only:
git commit --amend -m "feat(auth): rate-limit login by IP"

# Add a forgotten file:
git add src/forgot.ts
git commit --amend --no-edit             # keep original message

# Change message AND add files:
git add src/forgot.ts
git commit --amend -m "feat(auth): rate-limit login by IP (closes SEC-412)"
```

`--amend` creates a NEW commit with a new SHA. The old commit is orphaned but recoverable via `reflog` for ~90 days.

**If the original commit was pushed**: `git push --force-with-lease` to update. Safe on your own feature branch. Never on `main`.

## `git rebase -i` (the workhorse)

See `references/rebase-and-merge.md` for full interactive-rebase coverage. Capabilities: squash, fixup, reword, reorder, drop, split, edit.

## `git filter-repo` (REPLACES `filter-branch`)

`git filter-branch` is **deprecated and unsafe**. Use `git filter-repo` (install separately, often `pip install git-filter-repo` or `brew install git-filter-repo`).

**filter-repo is destructive to remotes.** Clone fresh, run it, then force-push. The official guidance is: do not run filter-repo on a repo with working collaborators without coordination, and have everyone re-clone afterwards.

### Remove a file from all history
```bash
git clone --mirror <repo> repo-mirror.git
cd repo-mirror.git
git filter-repo --path secrets/prod.env --invert-paths
# Force-push all refs:
git push --force origin --all
git push --force origin --tags
# Every collaborator must re-clone. The old SHAs are gone.
```

### Remove a directory from all history
```bash
git filter-repo --path build-artifacts/ --invert-paths
```

### Replace sensitive text everywhere (e.g. an API key)
```bash
echo "AKIAIOSFODNN7EXAMPLE==>REDACTED_AWS_KEY" > replacements.txt
git filter-repo --replace-text replacements.txt
```

### Move a subdirectory to its own repo (split)
```bash
git filter-repo --subdirectory-filter packages/sdk
# Now `.` contains only the history of packages/sdk, rooted at the new repo root.
```

## BFG Repo-Cleaner (alternative for secret/large-file removal)

Java-based, faster than filter-repo for huge repos, simpler CLI for the common case.

```bash
# Mirror clone (bare):
git clone --mirror <repo>
# Run BFG (deletes files >10MB and files matching "passwords.txt"):
bfg --strip-blobs-bigger-than 10M --delete-files passwords.txt repo-mirror.git
# Or replace text in all files:
bfg --replace-text passwords.txt repo-mirror.git
# Clean up the loose objects BFG marked for deletion:
cd repo-mirror.git
git reflog expire --expire=now --all && git gc --prune=now --aggressive
# Force-push:
git push --force origin --all && git push --force origin --tags
```

BFG does NOT touch the latest commit's HEAD — you must remove the secret from the current tree by hand first (or use filter-repo for that).

## When rewriting history is the WRONG answer

| Situation | Wrong answer | Right answer |
|---|---|---|
| A leaked secret is in a pushed commit | Rewrite history | **Rotate the secret immediately**, then optionally rewrite (rewriting does NOT un-leak) |
| A bad merge is on `main` and merged | Force-push a fix | `git revert -m 1 <merge-sha>` (forward-only) |
| A commit message has a typo on `main` | Amend + force-push | Live with it OR revert + re-commit if critical |
| You committed a large file accidentally | filter-repo on shared repo | Coordinate force-push + re-clone, or use Git LFS going forward |
| A teammate already pulled the bad commit | Force-push without telling them | Tell them, get acknowledgment, then `git fetch && git reset --hard origin/<branch>` for them |

## Rewriting safety checklist

Before force-pushing rewritten history:

1. **Confirm branch visibility.** Is this branch local-only, or pushed-and-shared?
2. **If shared**: coordinate with every teammate. They will need `git fetch && git reset --hard origin/<branch>` after your force-push (losing any local commits on that branch — they should stash/rebase first).
3. **Use `--force-with-lease`**, never `--force`. Lease fails if the remote has commits you didn't expect (e.g. a teammate just pushed).
4. **Update all refs that pointed at old commits**: tags (`git push --force --tags`), other branches that merged the old commits, PRs.
5. **Notify CI/CD**: any deploy keyed on a SHA may misbehave. Update release tags if needed.
6. **Notify anyone who forked**: forks retain old history; their future PRs may resurrect the old commits.

## `git filter-branch` — DO NOT USE

`git filter-branch` is in Git's own man page marked as deprecated: "Please use an alternative history filtering tool such as git-filter-repo." It's slow (single-threaded, walks every commit), fragile (shell-quoting issues), and can leave a repo in a half-filtered state. Use `git filter-repo` or BFG.

If you encounter a repo with a `git filter-branch` invocation in a script, replace it with the `git filter-repo` equivalent and remove the old script.

## Splitting a fat commit post-hoc

If you already committed two unrelated changes in one commit and want to split:

```bash
git rebase -i HEAD~3
# mark the fat commit as "edit"
# at pause:
git reset HEAD^
git add -p                     # stage first logical chunk
git commit -m "feat(x): first half"
git add -A
git commit -m "feat(y): second half"
git rebase --continue
```

If the commit was already pushed to a feature branch: `git push --force-with-lease` after the rebase.

## Anti-patterns

- **Rewriting `main` because "the history is ugly".** History is a record, not a magazine. Live with merge noise; don't rewrite published history.
- **Force-pushing a feature branch your pair is also working on** without telling them. Their next `git pull` will explode. Coordinate.
- **`git filter-branch`** anywhere. Deprecated, unsafe.
- **Rewriting history to remove a secret** without rotating the secret first. The pushed secret is already leaked; rewriting history doesn't claw it back from caches, mirrors, forks, or attacker systems. Rotate first, rewrite second (and only if your threat model demands history scrubbing).
- **Forgetting to force-push tags.** Tags are refs too; `git push --force --tags` after rewriting.
- **Assuming `--force-with-lease` is risk-free.** It only checks the remote's current ref. If a teammate pushed and then force-pushed back to the old SHA (unusual but possible), lease passes and you still overwrite their work.
