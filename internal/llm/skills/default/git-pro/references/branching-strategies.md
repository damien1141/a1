# Branching Strategies

Choose once, document in `CONTRIBUTING.md`, enforce via branch protection. Flipping strategies mid-project causes merge pain.

## Decision Table

| Strategy | When to use | Trunk | Lifespan of feature branch | Merge to trunk | Release model |
|---|---|---|---|---|---|
| **Trunk-based** | Continuous deployment, small team, strong CI, feature flags | `main` | Hours to ~2 days | Squash-merge or rebase-merge | Deploy from `main` directly |
| **GitHub Flow** | Web product, continuous deploy, no long-lived releases | `main` | Days to ~1 week | Squash-merge or merge-commit | Deploy from `main` after PR merge |
| **Git Flow** | Versioned product shipped to customers, multiple supported versions | `develop` (integration); `main` (released) | Weeks | `--no-ff` merge-commit to `develop`; `develop` → `release/*` → `main` | Tags on `main`; hotfix branches off `main` back-merge to `develop` |
| **Release branches** (Git Flow-lite) | Synchronized releases every 2-6 weeks, no per-customer forks | `main` | 2-6 weeks | Squash to `main` at release cut | Tag `main`; cherry-pick critical fixes to `release/*` |

### Default recommendation

**Trunk-based with feature flags and squash-merge** is the modern default for SaaS/web. Use Git Flow only if you ship on-prem/versioned artifacts with multiple supported versions. Use release branches when releases are synchronized and you need a "code freeze" period.

## Trunk-based (recommended default)

- `main` is always deployable. CI must be fast (<10 min) and trusted.
- Feature branches live <2 days. Longer work goes behind a feature flag.
- **Trunk-based ≠ no branches.** It means branches are short and merge via squash or rebase.
- Feature flags (LaunchDarkly, Unleash, in-house) decouple deploy from release.
- Pair programming or mob review replaces long PR review cycles.

```bash
git checkout -b feature/x
# <short work, hours>
git fetch origin
git rebase origin/main          # keep linear
git push --force-with-lease
gh pr create --base main
# After approval + green CI: squash-merge via GitHub UI, or:
git checkout main && git pull
git merge --squash feature/x && git commit -m "feat(x): ..."
git push origin main
git branch -D feature/x && git push origin :feature/x
```

## GitHub Flow

- `main` is always deployable (same as trunk-based).
- Feature branches longer (up to ~1 week) but still small PRs.
- Merge-commit OR squash-merge both acceptable.
- Deploy from `main` after merge (or from the merge commit on a deploy bot).
- No `develop`, no `release/*`.

## Git Flow

Use ONLY for versioned products with multiple supported versions. Otherwise it's overhead.

- `main` — production-released. Each merge → tag `vX.Y.Z`.
- `develop` — integration branch. Nightly/CI builds.
- `feature/*` — branch off `develop`, merge back to `develop` with `--no-ff`.
- `release/*` — branch off `develop` when ready to release. Bug fixes only. Merge to `main` (tag) AND back to `develop`.
- `hotfix/*` — branch off `main` for urgent prod fixes. Merge to `main` (tag) AND `develop`.

```bash
# Start a feature
git checkout develop && git pull
git checkout -b feature/oauth-login

# Finish a feature
git checkout develop && git pull
git merge --no-ff feature/oauth-login
git push origin develop
git branch -d feature/oauth-login

# Cut a release
git checkout develop
git checkout -b release/2.4.0
# bugfixes only on release/2.4.0
git checkout main && git pull
git merge --no-ff release/2.4.0
git tag -a v2.4.0 -m "Release 2.4.0"
git push origin main --tags
git checkout develop && git merge --no-ff release/2.4.0
git branch -d release/2.4.0
```

## Release branches (Git Flow-lite)

- `main` is the trunk (deployable).
- `release/X.Y` branches off `main` at code-freeze. Only critical fixes.
- Fixes cherry-picked from `main` to `release/X.Y` (or vice versa with care).
- Use when: synchronized team releases every N weeks; no per-customer long-lived forks.

## Branch protection (enforce, don't trust)

Required for any team repo:

- `main` (and `release/*`) → require PR, ≥1 review, status checks must pass, no direct push, no force-push, dismiss stale reviews on push, include administrators.
- Linear history on `main` (squash or rebase-merge only — block merge-commits if you want linear).
- Signed commits required for OSS / regulated industries.
- `gh api repos/:owner/:repo/branches/main/protection` to inspect; `gh api -X PUT ...` to set.

## Naming conventions

- `feat/<short-kebab>` — new feature
- `fix/<short-kebab>` — bug fix
- `chore/<x>` — tooling, deps
- `docs/<x>` — docs only
- `release/X.Y` — release cut
- `hotfix/<X.Y.Z>` — production hotfix
- Tie to issue: `feat/SEC-412-rate-limit-login` (Jira) or `feat/issue-412-rate-limit`.

## Anti-patterns

- **Long-lived feature branches** (>1 week on trunk-based). Rebase pain, merge hell. Split into a stack of small PRs or use a feature flag.
- **`develop` branch that no one merges to `main`.** The release becomes a giant integration event. Either release continuously from `main` or commit to Git Flow properly.
- **Branches named `wip`, `temp`, `asdf`.** Unreviewable, unmergeable, untraceable.
- **Cherry-picking as a primary integration strategy.** Use it for release backports only; otherwise it duplicates commits and breaks `git log` archaeology.
- **Force-pushing to `develop`/`release/*`/`main`.** Always block via branch protection.
