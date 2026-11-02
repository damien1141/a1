# PROGRESS — issues.md work

## Goal
Fix every issue in `issues.md` and build the combined grep tool. Working in `/home/z/my-project/upload/a1_extracted` (Go 1.26.3 toolchain at `/home/z/my-project/go/bin/go`).

## Decisions / constraints
- Bugfix rule: fix minimally, no refactors while fixing.
- Tests use testify. `testing`/`testify` stay in `*_test.go`.
- Existing tests for several broken tools encoded the bug — they get rewritten to assert correct behavior.
- For #4 batchtool: the actual root cause is `PathResolver` not knowing the go.mod module prefix (so Go imports like `github.com/damien1141/a1/internal/graph` never resolve). The issue text claims regex parsing misses multi-line `import (` blocks, but `parseGoImports` already uses `go/parser` which handles those correctly — issue text was outdated on that point. Fix is to make the resolver module-aware.
- For #9 APPA: wire-in is high-risk (touches executor admission). I left it for the user — wiring into the executor's admit path is a behavior change; cut removes tested code. Default = leave for user.
- For #11 collapse: kept `errortrans` (text → diagnosis) + `testtool` (run + parse); removed `errortool`, `errpattern`, `stacktool` from the registry. Source packages stay in place as dead code for now (the bugfix rule says preserve by default; deleting substantive content needs explicit approval). Future cleanup can delete them.

## Completed (with evidence)

### #1 coveragetool
- Bug: `lengthStr := m[4]` was the end-line, not numStmt (m[6]).
- Fix: `internal/tools/coveragetool/coverage.go` — use m[6] for numStmt.
- Tests: rewrote `TestParseCoverageProfile_Basic` and `_Sorting` (assert 50%/100% instead of 16.67%/40%). Added `TestParseCoverageProfile_WeightByNumStmt` to lock in the specific bug (50-line block with 1 stmt vs 2-line block with 10 stmts).
- `go test ./internal/tools/coveragetool/...` → ok.

### #2 watchertool
- Bug: package-global `lastCheck time.Time` shared across all paths.
- Fix: `internal/tools/watchertool/watcher.go` — `lastCheck map[string]time.Time` keyed by cleaned absolute path. Added `lastCheckFor`/`recordLastCheck` helpers.
- Tests: updated `TestRunWatcher_LastCheckSeesEveryReportedFile` and `_ExplicitSinceUnaffectedByLastCheck` to take a path argument. Added `TestRunWatcher_LastCheckIsPerPath` (scan A, then first scan of B must see B's pre-existing file).
- `go test ./internal/tools/watchertool/...` → ok.

### #4 batchtool (graph resolver)
- Bug: `PathResolver` didn't strip the go.mod module prefix; `TopologicalSortWithResolver` degenerated to reverse-input order for any real Go codebase.
- Fix: `internal/graph/resolver.go` — `readGoModModule` parses the `module <path>` line; `Resolve` strips the module prefix before file lookup.
- Tests: added `TestPathResolver_ModulePrefix` and `TestTopologicalSortModulePrefixedImports`.
- `go test ./internal/graph/...` → ok.

### #3 errortrans
- Bug: `strings.Contains` matching misfired (e.g. "no field" matched mid-word).
- Fix: `internal/tools/errortrans/errtrans.go` — `pattern.matches` now uses `(?i)\b<substring>\b` (or `\b` only at start when substring ends in a non-word char like "undefined:"). Added optional `Regex` field for tighter patterns.
- Tests: existing tests still pass. Added `TestRunErrtrans_AnchoredMatching` with 6 cases of substrings embedded in larger words ("no field" inside "no fielded", etc.).
- `go test ./internal/tools/errortrans/...` → ok.

### #5 propertytool
- Bug: skeleton emitted `assert := assert.New(t)` with no testify import and unused value; no `package` clause, no `testing/quick` import. Pasting into an empty `_test.go` failed to compile.
- Fix: `internal/tools/propertytool/property.go` — skeleton now includes `package <pkg>` + imports `testing`/`testing/quick` + drops the unused assert line.
- Tests: added `TestRunProperty_SkeletonCompiles` (writes the skeleton to disk and runs `go vet` on it; must type-check clean) and `TestRunProperty_SkeletonHasNoUnusedAssert`.
- `go test ./internal/tools/propertytool/...` → ok.

### #6 secrettool
- Bug: per-line loop appended every pattern match, so a line matching both API key + Generic secret got two entries; headline count was inflated. Regexes also shallow (pure-hex hashes, placeholder passwords).
- Fix: `internal/tools/secrettool/secret.go` — patterns reordered most-specific-first; per-line loop breaks on first match. Added per-pattern `ok` post-filter: Password rejects placeholder values ("password", "changeme", etc.); Generic secret rejects pure-hex values of MD5/SHA-1/SHA-256/SHA-512 lengths via `isHexHash`.
- Tests: existing tests still pass. Added `TestRunSecret_NoDoubleCount` (single line, single entry, [API key] beats [Generic secret]), `TestRunSecret_HexHashNotGenericSecret` (md5/sha1/sha256), `TestRunSecret_PasswordKeywordRejected`.
- `go test ./internal/tools/secrettool/...` → ok.

### #7 apitool/deadcode countReferences
- Bug: shelled out to `grep -r`; fragile, POSIX-only, no Windows path.
- Fix: new `internal/util/greprefs.go` with `CountGoReferences(root, defFile, symbol)`. Walks the tree Go-natively, skips `vendor/`, `.git/`, etc., skips the defining file and `_test.go` files. Uses `\b<symbol>\b` regex so `Foo` doesn't match `FooBar` (substring grep did and inflated counts). Both `apitool/api.go` and `deadcode/deadcode.go` now call this helper.
- Tests: added 7 `TestCountGoReferences_*` cases in `internal/util/grep_test.go` (basic count, skips defining file, skips test files, identifier boundary, skips vendor/.git, empty symbol, no matches).
- `go test ./internal/util/... ./internal/tools/apitool/... ./internal/tools/deadcode/...` → ok.

### #8 journaltool
- Bug: nothing in the codebase wrote `.journal`; the read always returned "No journal entries found."
- Fix: `internal/tools/journaltool/journal.go` — added `action`/`file`/`line`/`outcome` params. When `action` is non-empty, `writeJournalEntry` appends a `timestamp|action|file|line|outcome` line to `<path>/.journal` (creating it if missing). Read mode preserved.
- Tests: added `TestRunJournal_RecordAndRead` and `TestRunJournal_RecordAppendsNotOverwrites`.
- `go test ./internal/tools/journaltool/...` → ok.

### #17 browser tool description
- Bug: description said "this tool will ask the user to open it first" but the tool just returns an error.
- Fix: `internal/tools/browsertool/browsertool.go` — description now says "actions other than 'open' return an error directing the model to call 'open' first (or use the fetch tool)."
- `go test ./internal/tools/browsertool/...` → ok.

### #10 scratchpadtool
- Bug: duplicates `internal/session/memory.Bank` (the session-scoped JSONL store wired into the executor).
- Fix: removed `scratchpadtool.ScratchpadTool()` from both `DefaultTools()` and `ReadonlyTools()` registries in `internal/tools/tools.go`. Removed the `scratchpadtool` import. Deleted the package directory (`internal/tools/scratchpadtool/`). Memory bank is the single note store.
- `go build ./...` → clean.

### #11 collapse 5 failure-analysis tools → 2
- Bug: `error`, `errpattern`, `errtrans`, `stack`, `test` overlapped.
- Fix: removed `errortool.ErrorTool()`, `errpattern.ErrPatternTool()`, `stacktool.StackTool()` from both registries. Removed their imports. Survivors: `errortrans.ErrtransTool()` (text → diagnosis, multi-language) and `testtool.TestTool()` (executable test runner + parser). Source packages stay as dead code (preserve-by-default rule); future cleanup can delete them.
- `go build ./...` and full test sweep → green.

### Phase 4 — searchtool (combined grep)
- New package: `internal/tools/searchtool/`. Tool name: `search`.
- Schema: `query` (required), `mode` (auto|text|symbol|file, default auto), `path`, `limit`, `ignoreCase`.
- Auto-mode detection: identifier → also query graph; path fragment → also query fd; always run rg.
- Planes run concurrently (sync.WaitGroup + sync.Mutex on shared `map[string]*fileHit`).
- Rank: (planes hit desc, text match count desc, rel path asc).
- Render: per-file `@file` block with plane badges `[text:N symbol file]`, top 5 text matches, symbol-plane label.
- Registered in both `DefaultTools()` and `ReadonlyTools()`.
- Tests: 9 cases (definition, auto find, mode text only, limit cap, ignore case, invalid mode, empty query, path not found, heuristic coverage, shortPath).
- `go test ./internal/tools/searchtool/...` → ok.

## Open / not done

### #9 APPA resolver
- Needs user decision: wire `PartialLabel.Resolve` / `CastResolver` into `admitExecution`, or cut the code entirely.
- Default = leave for user. Wiring into the executor's admit path is a behavior change; cutting removes tested code. Both are bigger changes than the rest of phase 2.

### Phase 3 — Design debt (#12–#16)
- Not started. Each is a substantive refactor:
  - #12 tokenizer: pick one ruler between `estimateMessageTokens` and provider `ContextTokens()`.
  - #13 identity split: module `a1`, dirs `~/.a1`, env `PHI_*`, package `phi`, splash "Phi", temp prefixes `phi-` → pick one and rename across the codebase.
  - #14 config rewrites: `SetDangerouslyAllowAll`, `AppendBashAllow` round-trip through `map[string]any` → `yaml.Marshal`, which strips comments and alphabetizes keys.
  - #15 extractArchive: shells to system `tar` with no path-traversal guard (the in-Go extractor has one).
  - #16 `/tmp/browser-screenshot.png`: fixed path; concurrent sessions clobber it.
- These can be follow-up sessions. Each touches more files than the bugfixes above and warrants its own scoped PR.

## Verification summary
- `go build ./...` — clean.
- `go test ./internal/tools/... ./internal/graph/... ./internal/util/... ./internal/agent/...` — all green.
- New tests added where bugs weren't covered by existing tests.
