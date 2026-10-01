# Conflict Resolution

A conflict means two branches changed the same lines. Git cannot decide which wins. You must. Slow down, read both sides, write the merged result, verify it builds.

## The conflict markers

```
<<<<<<< HEAD
def login(email, password):
    return auth_service.verify(email, password)
=======
def login(email, password, ip):
    return auth_service.verify(email, password, ip)
>>>>>>> feature/rate-limit
```

- `<<<<<<< HEAD` (or `=======`'s top) — your current branch's version.
- `=======` — separator.
- `>>>>>>> feature/rate-limit` — the incoming branch's version.

**Resolution**: edit the file, delete all three marker lines, write the actual desired code. Then `git add <file>`.

```python
# Resolved:
def login(email, password, ip=None):
    return auth_service.verify(email, password, ip)
```

## Resolution workflow

```bash
git status                      # which files are conflicted
# both modified:   src/auth.ts
# Open each conflicted file in your editor, resolve markers, save
git diff                         # see what you've staged vs HEAD vs working
git add src/auth.ts              # mark as resolved
# Continue whatever operation caused the conflict:
git merge --continue             # or: git rebase --continue / git cherry-pick --continue
# If you can't continue (more conflicts to think through):
git merge --abort                # or: git rebase --abort / git cherry-pick --abort
```

### Mid-merge vs mid-rebase

| Op | Continue | Abort | Skip |
|---|---|---|---|
| `git merge` | `git merge --continue` (or just commit) | `git merge --abort` | n/a |
| `git rebase` | `git rebase --continue` | `git rebase --abort` | `git rebase --skip` (for now-empty commits) |
| `git cherry-pick` | `git cherry-pick --continue` | `git cherry-pick --abort` | `git cherry-pick --skip` |
| `git revert` | `git revert --continue` | `git revert --abort` | n/a |

`--abort` is your friend. It always returns you to the pre-op state. Use it freely when confused.

## `git mergetool`

Launches a 3-way merge tool (vimdiff, meld, kdiff3, VS Code):

```bash
git mergetool                    # opens tool on first conflicted file
# Configure default:
git config --global merge.tool vscode
git config --global mergetool.vscode.cmd 'code --wait $MERGED'
```

For VS Code specifically, you can also resolve conflicts in the editor with the built-in merge editor (`> Merge: Open Merge Editor`).

## Conflict strategies

### Take ours (current branch wins)
```bash
git checkout --ours src/config.ts       # during merge
git checkout --ours src/config.ts       # during rebase, "ours" is the BASE side (confusing!)
# Safer explicit form:
git checkout --ours src/config.ts && git add src/config.ts
```

**Caveat**: during a rebase, `--ours` and `--theirs` are SWAPPED from what you'd expect, because rebase replays your commits on top of the new base. `--ours` is the branch you're rebasing onto; `--theirs` is your commit being replayed. Always `git status` and read the labels carefully.

### Take theirs (incoming wins)
```bash
git checkout --theirs src/config.ts
git add src/config.ts
```

### Strategy options at merge time
```bash
git merge -X ours feature/x          # prefer ours on conflict (still tries to auto-merge non-conflicting)
git merge -X theirs feature/x        # prefer theirs on conflict
git merge -X ignore-space-change     # ignore whitespace changes
git merge -X diff-algorithm=histogram  # better conflict detection
```

`-X ours/theirs` is dangerous: it silently picks a side. Use only when you're sure (e.g. regenerating a lockfile). Prefer manual resolution.

## `rerere` (reuse recorded resolution)

If you keep resolving the SAME conflict (e.g. rebasing the same long-lived feature branch repeatedly), enable `rerere`:

```bash
git config --global rerere.enabled true
```

Git records your resolution. Next time the same conflict appears (e.g. after another rebase), Git auto-applies your previous resolution. You still see the file staged; review and `git add` (or `git rerere diff` to verify).

```bash
git rerere status       # which files have recorded resolutions applied
git rerere diff         # see what rerere did
git rerere forget <file>  # forget a bad resolution
git rerere clear         # clear all recorded resolutions for current conflict
```

## Common conflict patterns

### Both added the same import / constant
Trivial — keep one, delete the duplicate.

### Conflicting refactor + feature on the same function
Hardest case. Don't just pick a side; re-read both intents, write a version that satisfies both. Often one side renamed a variable and the other added a parameter — merge both changes.

### Lockfile conflicts (`package-lock.json`, `yarn.lock`, `Cargo.lock`)
Don't resolve by hand. Run the appropriate regenerate command:
```bash
npm install               # regenerates package-lock.json
yarn install              # regenerates yarn.lock
cargo update -p <crate>   # or just `cargo build`
```
Then `git add` the regenerated file.

### Generated file conflicts (protobuf, openapi, build artifacts)
Don't commit generated files. If you must, regenerate them after resolving source conflicts.

### Whitespace-only conflicts
```bash
git merge -Xignore-space-change feature/x    # try merge ignoring whitespace
# Or after the fact:
git checkout --ours src/file && git checkout --theirs src/file  # cycle through
```

## Verifying a resolved conflict

Before continuing the merge/rebase:

```bash
git diff --cached              # what will actually be committed
# Build + test:
npm test                       # or: cargo test, pytest, go test ./...
# If green:
git merge --continue           # or git rebase --continue
# If red:
# Fix the broken code (you may have merged two APIs that don't compile together)
git add <fixed files>
git merge --continue
```

**Always build + test after resolving a conflict.** A clean merge (no markers) is not the same as a working merge. Two semantically-conflicting changes can merge syntactically and produce broken code.

## Anti-patterns

- **`git checkout --ours .` to resolve everything.** You just discarded the incoming branch's work. If that's what you wanted, `git merge --abort` and `git merge -X ours` is clearer.
- **Committing with conflict markers still in the file.** `git commit` will refuse, but `git commit -am` after a partial `git add` can slip markers through. Always `git diff --cached` before continuing.
- **Resolving a lockfile by hand.** Use the package manager.
- **Skipping the build/test step after resolution.** Two halves that look fine separately can produce invalid code together (e.g. a renamed function call meets a renamed definition).
- **`git rebase --skip` to "resolve" a conflict.** That drops the commit, not resolves the conflict. Only use `--skip` if the commit's changes are now empty after the rebase (e.g. already applied upstream).
- **Force-resolving without reading both sides.** Use `git log --left-right HEAD...MERGE_HEAD -- <file>` to see what each side intended.
