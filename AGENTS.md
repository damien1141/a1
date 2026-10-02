# Phi

Lean, high-performance Go terminal coding-agent harness. Layout: [doc/project-layout.md](doc/project-layout.md). Humans: [CONTRIBUTING.md](CONTRIBUTING.md).

## Communication Preferences

- Dry, concise, low-key humor. No flattery, no forced memes. Skip preambles and postambles.
- Comments explain "why", not "what". English only — this repo was migrated off Chinese comments.
- Error messages: actionable and specific. No vague "something went wrong".

## Constraints

- **Tool loop is ExtensionPre → Gate/Ask → Run → ExtensionPost.** Don't bypass the permission gate when changing the executor. Don't put MCP server tool schemas on the model — only `mcp_list` / `mcp_inspect` / `mcp_call`.
- **Extensions are PXB subprocesses** (`phi.yaml` + native binary under `~/.a1/extensions` / `.phi/extensions`). Wire: `ext/go/pxb`. Author SDK: Go `ext/go/phi` (module `github.com/pulseaiclub/phi/ext/go`). Host: `internal/extension`. See [doc/extensions.md](doc/extensions.md).
- **Keep hashline `edit`.** Don't replace it with whole-file rewrite. Stale `@file path#TAG` / `LINE#HASH` must fail closed.
- **Sub-agent transcripts stay under `~/.a1/jobs/<id>/`.** Parent context gets the wait/task summary only. Child engines have no `agent_*` tools (no nesting). Default child role is explore (no write/edit; bash allowed except hard denies).
- **UI split:** `internal/components` render; `internal/tui` wires the shell. Non-shell pieces live under `internal/tui/controller` (Engine/Bus/Msg), `internal/tui/transcript` (Mapper); version in `internal/version`. Keep widgets dumb.
- **TUI assembly:** `cmd` constructs `controller.Bus` / `controller.Controller` / App and passes them into `editor.NewEditor(...)`, which builds the commands registry (`commands.NewBuiltinRegistry`). Do not hide `GetDefaultProject` inside `tui` constructors; do not return half-initialized Controllers (`engineErr` zombies). Prefer constructor parameters over `XxxDeps` bags.
- **Stay lean and fast.** Direct module deps are few on purpose. Don't add a dependency without a clear need. Prefer changes that keep startup, idle RSS, and rebuild time small.
- **Format with `make fmt`** (gofumpt / goimports / golines, 120 cols, local prefix `github.com/pulseaiclub/phi`). Don't hand-fight import groups.
- **`testing` / `testify` stay in `*_test.go`.** `depguard` will fail the lint otherwise.
- **Tests use testify** (`assert` / `require`) for assertions — no raw `t.Fatalf` checks.
- After dependency changes: `go mod tidy`. `go.mod` is not generated.

## Contributor Guidelines

- Keep changes focused and reviewable. Add or update tests next to the code.
- Conventional Commits, English, lowercase, imperative, ≤72 chars. One logical change per commit.
- Do not put `@mentions` or `fixes #...` in commit messages (those belong in the PR).
- Do not add `Co-authored-by:`.
- User-visible changes update `CHANGELOG.md` under `## [Unreleased]`. Only release PRs
  move entries under `<!-- Released section -->` (requires `Unlock Released Changelog`).
  Skip with `Skip Changelog` / `dependencies` / `[chore]` in the PR title when no entry is needed.

## Commands

```
make help
make test                      # go test ./...
go test ./internal/extension -v # one package
make fmt                       # apply formatters
make fmt-check                 # CI formatting gate
make lint                      # golangci-lint
make deadcode                  # unreachable funcs vs baseline
make check                     # fmt-check + lint + deadcode (CI)
make build                     # ./a1
```

## Style

- Packages: lowercase, single word, match the directory (`writetool`, not `write_tool`).
- Prefer small packages under `internal/`; keep the exported surface small.
- Tests live beside the code they cover.

## Releases & Pushing

Release procedure (the gates that must all be green before a tag goes out):

1. **Version.** Bump `internal/version/version.go` (`var Version = "vX.Y.Z"`). It
   is shown on the splash screen next to the sphere and used by `a1 update`.
2. **Changelog.** Add a `## [X.Y.Z] - YYYY-MM-DD` section to `CHANGELOG.md`
   (Added / Changed / Deprecated / Removed / Fixed / Security) and add the
   `[X.Y.Z]: https://github.com/pulseaiclub/phi/compare/vX.Y.Z-1...vX.Y.Z`
   and `.../releases/tag/vX.Y.Z` link references at the bottom. Never edit
   text under `<!-- Released section -->` except in a release PR.
3. **Readmes.** Bump the version in the `# A1 vX.Y.Z` header of both
   `README.md` and `README.zh-CN.md`.
4. **CI gates — run these locally before pushing:**
   - `make test` (and `go test ./...` from `ext/go`). The `internal/util/diffreview`
     git tests must be hermetic: neutralize `commit.gpgsign` /
     `user.signingkey` via `GIT_CONFIG_GLOBAL=/dev/null` + `commit.gpgsign=false`,
     like `runGit` already does for author identity.
   - `make check` = `make fmt-check` + `make lint` + `make deadcode`.
     `make fmt` must be run before committing — the repo has formatting drift,
     so `fmt-check` is red until it is applied. `scripts/deadcode.baseline`
     must stay in sync: every new unreachable exported function gets added
     with a one-line rationale (exported public API is the normal excuse).
   - Markdown lint: `markdownlint-cli2 --config .markdownlint.yaml "**/*.md"`.
     Drop orphaned link-reference definitions (MD053) — they fail the gate.
     The gate excludes AI-authored skill content and working notes via negation
     globs in `.github/workflows/markdown.yml` (`!skills/**/*.md`,
     `!internal/llm/skills/**/*.md`, `!todo.md`, `!**/.temp/**/*.md`); the
     pinned CI image ignores `.markdownlintignore`, so the exclusions live
     in the workflow args. Add a new exclusion here, not by mass-fixing the
     skill library.
5. **Commit & push.** Conventional commit, lowercase, imperative, ≤72 chars.
   Push `main` first, then the tag. Only commit files this session changed —
   leave pre-existing unmodified working-tree files out unless asked.
6. **Tag & release.** Annotated tag `vX.Y.Z` on the release commit, push it,
   then `gh release create vX.Y.Z --title "vX.Y.Z" --notes-file CHANGELOG.md
   --latest`. Release notes come from `scripts/changelog-extract.sh vX.Y.Z`
   — that script must exit 0 with a non-empty body or GoReleaser fails.
