# Remote Operations

The remote is where Git stops being personal. Every remote operation affects teammates. Slow down, check upstream state, prefer `--force-with-lease`, and verify push success.

## The four core commands

| Command | Reads from remote | Writes to remote | Modifies working tree |
|---|---|---|---|
| `git fetch` | ✅ | ❌ | ❌ (only updates remote-tracking refs) |
| `git pull` = fetch + merge/rebase | ✅ | ❌ | ✅ (merge/rebase updates your branch) |
| `git push` | ❌ (it pushes, doesn't fetch) | ✅ | ❌ |
| `git ls-remote` | ✅ (refs only) | ❌ | ❌ |

**Use `git fetch` + explicit `merge`/`rebase` over `git pull`** for transparency. `git pull` hides what's happening; `git fetch && git rebase origin/main` is explicit.

## Upstream tracking

A local branch tracks a remote branch (its "upstream"). Once set, `git push`/`git pull` work without arguments.

```bash
# On first push:
git push -u origin feature/x       # -u sets upstream
# Or after the fact:
git branch --set-upstream-to=origin/feature/x feature/x
# -u is shorthand for --set-upstream-to=origin/<branch>

# Inspect:
git branch -vv
# * feature/x   a1b2c3d [origin/feature/x: ahead 2, behind 1] feat(x): ...
#   main        9c8b7a2 [origin/main] ...

git rev-parse --abbrev-ref --symbolic-full-name @{u}   # print upstream of current branch
```

If `git status` shows "ahead 2, behind 1", you have 2 commits to push and 1 to integrate. **Pull/fetch before pushing** when behind, or your push will be rejected.

## `git fetch`

```bash
git fetch origin                  # all branches
git fetch origin main             # one branch
git fetch --all                   # all remotes
git fetch --prune                 # delete local refs to deleted remote branches
# Recommended config:
git config --global fetch.prune true
```

`fetch --prune` (or `git remote prune origin`) cleans up local references to remote branches that have been deleted (e.g. after a PR merges and the branch is auto-deleted on GitHub).

## `git pull`

```bash
git pull                          # = fetch + merge (or rebase if pull.rebase=true)
git pull --rebase                 # force rebase this once
git pull --no-rebase              # force merge this once
git pull --ff-only                # only fast-forward, fail otherwise (no merge commit)
```

Recommended config:
```bash
git config --global pull.rebase true       # rebase instead of merge on feature branches
git config --global rebase.autoStash true  # stash WIP, rebase, pop
```

## `git push`

```bash
git push                          # current branch → its upstream
git push origin feature/x         # explicit
git push origin local-name:remote-name  # different local/remote names
git push origin --all             # all branches
git push origin --tags            # all tags (tags are NOT pushed by default)
git push origin v1.2.3            # one tag
git push origin :feature/x        # delete remote branch (leading colon)
git push origin --delete feature/x # equivalent, clearer
git push --force-with-lease       # safe force-push
git push --no-verify              # SKIP pre-push hooks (DON'T, without a written reason)
git push --dry-run                # what would be pushed
git push -u origin feature/x      # set upstream on first push
```

### Push tags

```bash
git tag -a v1.2.3 -m "Release 1.2.3"   # annotated tag (recommended over lightweight)
git push origin v1.2.3                  # push one tag
git push origin --tags                  # push all tags
git push origin --follow-tags           # push annotated tags reachable from pushed commits
# Recommended config:
git config --global push.followTags true
```

Annotated tags (`-a`) carry a message, tagger, date, and GPG signature. Lightweight tags are just pointers. **Use annotated tags for releases.**

### Push a signed tag
```bash
git tag -s v1.2.3 -m "Release 1.2.3"   # -s signs with GPG/SSH
# Or:
git tag -a -s v1.2.3 -m "Release 1.2.3"
git push origin v1.2.3
```

## `--force-with-lease` vs `--force`

| Option | Behavior | When |
|---|---|---|
| `git push --force` | Overwrites remote ref unconditionally. Clobbers any commits teammates pushed. | NEVER on shared branches. Use only on a solo repo branch. |
| `git push --force-with-lease` | Push only if remote ref matches your local `origin/<branch>` ref. Fails if remote moved. | After rebase/amend on YOUR feature branch. Default choice. |
| `git push --force-with-lease=feature/x:<expected-sha>` | Even stricter: specify exact SHA expected. | Paranoid mode; useful in CI scripts. |

`--force-with-lease` works by comparing the remote ref to your local remote-tracking ref (`origin/feature/x`). If a teammate pushed since you last `fetch`ed, your local tracking ref is stale and the push is rejected — you must `fetch` and decide.

**If `--force-with-lease` is rejected**: `git fetch`, inspect what changed, rebase or coordinate, then retry. Do NOT fall back to `--force`.

### Recommended config
```bash
git config --global push.default current        # push current branch to same-named remote
git config --global push.autoSetupRemote true   # auto-set upstream on first push (Git 2.37+)
# Now `git push` from a new branch just works, no -u needed.
```

## Deleting remote branches

```bash
git push origin --delete feature/x       # explicit, recommended
git push origin :feature/x               # older syntax, same effect
# Local cleanup:
git fetch --prune                        # remove local ref to deleted remote branch
git branch -d feature/x                  # delete local (only if merged)
git branch -D feature/x                  # delete local forcibly
```

GitHub auto-deletes branches after merge if "Automatically delete head branches" is enabled in repo settings. Recommended.

## Recovering from a bad push

### Bad commit pushed to a feature branch you own
```bash
git reset --hard HEAD~1           # or amend / rebase -i to fix
git push --force-with-lease origin feature/x
```

### Bad commit pushed to `main` (shared)
**Do not force-push.** Use revert:
```bash
git revert <bad-sha>              # creates a new commit that undoes <bad-sha>
git push origin main
```

### Force-push overwrote someone's work
```bash
# On the clobbered teammate's machine:
git fetch origin
git reflog show feature/x         # find their lost commit SHA
git push --force-with-lease origin <their-sha>:feature/x
# Coordinate so both of you aren't force-pushing simultaneously.
```

## Remote branch inspection

```bash
git remote -v                          # list remotes and URLs
git remote show origin                 # detailed: branches, tracking, stale
git ls-remote origin                   # all refs on remote (branches, tags)
git branch -r                          # local view of remote branches
git branch -a                          # all branches (local + remote)
git log origin/main --oneline -10      # log of remote branch without checking out
```

## Adding/renaming/remotes

```bash
git remote add upstream https://github.com/original/repo.git
git remote rename origin github        # rename
git remote set-url origin git@github.com:org/repo.git   # change URL
git remote remove upstream             # remove
```

## Forks and upstream sync

For OSS contributions where you forked a repo:

```bash
git remote add upstream https://github.com/original/repo.git
git fetch upstream
git checkout main
git merge --ff-only upstream/main       # keep your fork's main in sync
git push origin main
# Now branch off main for your contribution:
git checkout -b fix/typo-readme
# ... push to origin (your fork), open PR upstream
```

`--ff-only` ensures you never accidentally create a merge commit on your fork's `main` while syncing.

## SSH vs HTTPS

```bash
# HTTPS (prompts for token / uses credential helper):
git clone https://github.com/org/repo.git
# SSH (uses your SSH key):
git clone git@github.com:org/repo.git

# Switch a repo from HTTPS to SSH:
git remote set-url origin git@github.com:org/repo.git

# Test SSH:
ssh -T git@github.com
```

Recommended: SSH with a hardware-backed key (YubiKey) or `~/.ssh/config` with `IdentitiesOnly yes`. For HTTPS, use a credential helper (`git config --global credential.helper osxkeychain` / `manager` / `store`) with a PAT (GitHub no longer accepts password auth).

## Submodules vs subtrees

For embedding one repo inside another. Each has sharp edges; pick deliberately.

| | Submodules | Subtrees |
|---|---|---|
| Storage | `.gitmodules` + gitlink (commit pointer); nested repo's objects NOT in parent | Nested repo's objects merged INTO parent's history |
| Clone experience | Parent clones fast; `git submodule update --init` fetches nested | Parent clones with full nested history (larger clone) |
| Update nested repo | `cd nested && git checkout <new-sha> && cd .. && git add nested && git commit` | `git subtree pull --prefix=nested nested-repo main --squash` |
| Push back to nested repo | `cd nested && git push` (works as nested repo's remote) | `git subtree push --prefix=nested nested-repo main` (slower) |
| Branch switching | Painful — `git submodule update --init --recursive` after every checkout | Transparent — nested content is part of parent |
| Best for | Third-party libs pinned at a specific commit (rarely edited) | Vendored code you sometimes edit and push back |

### Submodule workflow

```bash
# Add:
git submodule add https://github.com/org/lib.git third-party/lib
git commit -m "chore: vendor lib as submodule"

# Clone a repo with submodules (one-shot):
git clone --recurse-submodules https://github.com/org/repo.git

# After clone (without --recurse-submodules):
git submodule update --init --recursive

# Update all submodules to their recorded commits (after a checkout/pull):
git submodule update --init --recursive

# Update a submodule to its remote's latest main:
cd third-party/lib
git checkout main && git pull
cd ../..
git add third-party/lib
git commit -m "chore: bump lib submodule to latest"

# Remove a submodule:
git submodule deinit -f third-party/lib
git rm -f third-party/lib
# Then delete the entry from .gitmodules and .git/config manually if needed.
```

**Submodule pitfalls**:

- Forgetting `--recurse-submodules` on clone → empty submodule dirs.
- Forgetting `git submodule update --init --recursive` after pulling a parent commit that bumped a submodule → "detached HEAD" at the old submodule commit, work goes to the wrong place.
- Detached HEAD in submodules by default — make a branch before editing: `cd submodule && git checkout -b my-work`.
- Branch switching in parent leaves submodules in inconsistent state without `--recurse-submodules`.

### Subtree workflow

```bash
# Add a subtree (one-time):
git subtree add --prefix=third-party/lib https://github.com/org/lib.git main --squash

# Update (pull latest from nested repo's main):
git subtree pull --prefix=third-party/lib https://github.com/org/lib.git main --squash

# Push changes back to the nested repo:
git subtree push --prefix=third-party/lib https://github.com/org/lib.git my-feature-branch
```

**Subtree pitfalls**:

- Clone size grows (nested history is in parent).
- `subtree push` is slow (it has to synthesize commits to push back).
- Merge commits from subtree updates can be confusing in `git log`.

### Recommendation

- **Third-party library you pin and rarely touch**: submodule. Document `--recurse-submodules` in your README and CI.
- **Code you actively edit and push back**: subtree, OR a separate package manager (npm, cargo, pip, luarocks) and a proper versioned dependency. Treat subtrees as a last resort when package managers don't apply.
- **Neither if you can avoid it**: extract the shared code to a proper package and depend on a versioned release. Submodules and subtrees both create friction that grows with team size.

## Anti-patterns

- **`git push --force` to `main`/`develop`/`release/*`.** Branch protection should block this; if not, enable it.
- **`git push` while behind.** Push will be rejected; `fetch`+`rebase` first.
- **`git pull` (merge) on a feature branch** creating "Merge branch 'main' into feature" noise. Set `pull.rebase=true`.
- **Pushing without `fetch --prune`.** Your local `origin/feature/x` refs accumulate stale deleted branches.
- **Pushing tags separately when `--follow-tags` would do.** Set `push.followTags=true`.
- **`git push --no-verify` past pre-push hooks** without a written reason in the PR description. If the hook is wrong, fix the hook.
- **Using HTTPS with password auth** — GitHub rejected this in 2021. Use a PAT or SSH.
- **Trusting "pushed" = "deployed".** Push only updates the remote ref. CI/CD pipelines run on push events but may fail. Verify via `gh pr checks` or your deploy dashboard.
