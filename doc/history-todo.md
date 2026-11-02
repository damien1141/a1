# TODO — issues.md work

Scope: every issue in `issues.md` (broken #1-7, dead/unwired #8-11, design debt #12-17) plus the combined grep tool. Working in `/home/z/my-project/upload/a1_extracted`.

## Phase 1 — Stabilize broken tools (issues.md priority order)

- [x] #1 coveragetool — use numStmt (m[6]) not endLine (m[4]) for block weight
- [x] #2 watchertool — per-path `map[string]time.Time` instead of single global
- [x] #4 batchtool — PathResolver must strip the go.mod module prefix before resolving Go import paths
- [x] #3 errortrans — anchored matching (word-boundary / start-of-line / regex) so substrings like "no field" don't fire mid-sentence
- [x] #5 propertytool — emit compiling skeletons: add testify import, fix quick.Check signature
- [x] #6 secrettool — dedupe hits per (line, pattern); don't double-count
- [x] #7 apitool/deadcode — replace `grep -r` shell with Go-native recursive walk + scanner

## Phase 2 — Dead / unwired

- [x] #8 journaltool — added a `record` action (action+file+line+outcome) so the model can write `.journal` entries; reader kept for read mode
- [x] #9 APPA resolver — wired `RegisterCast` + `ResolveUnresolved` onto StaticGate; `admitExecution` calls `ResolveUnresolved` after Admit returns Allow (via the new `UnresolvedSourceResolver` interface). Fixed the latent cast-ceiling bug (old `IsNarrower` check required *strict* narrowing, so a cast that resolved to exactly its ceiling was rejected — paper says `λ ≤ may_cast`, equal is allowed).
- [x] #10 scratchpadtool — removed from registry; memory bank (`session.MemoryBank()`) is the single note store, wired into the executor
- [x] #11 collapse 5 failure-analysis tools into 2 — `errortool`, `errpattern`, `stacktool` removed from registry; `errortrans` (text → diagnosis) + `testtool` (run + parse) survive
- [x] #17 browser tool description — copy now says "errors directing the model to call open" instead of "asks the user to open"

## Phase 3 — Design debt

- [ ] #12 tokenizer — pick one ruler (estimateMessageTokens or provider ContextTokens)
- [x] #13 identity split — env vars `PHI_*` → `A1_*`, package `ext/go/phi` → `ext/go/a1`, manifest `phi.yaml` → `a1.yaml`, temp prefixes `phi-` → `a1-`, `.phi/` → `.a1/` in tests, splash "A1" (already), README/AGENTS.md/docs updated
- [ ] #14 config rewrites — stop round-tripping through map[string]any→yaml.Marshal
- [ ] #15 extractArchive — add path-traversal guard around system `tar`
- [x] #16 /tmp/browser-screenshot.png — per-call `os.CreateTemp("", "a1-screenshot-*.png")`; concurrent sessions and repeated screenshots no longer clobber

## Phase 4 — Combined grep tool

- [x] New `searchtool` — one tool, three planes (rg + graph + fd), ranked, grouped by file
- [x] auto-detect mode: identifier → +graph; path fragment → +fd; always rg
- [x] reuse `ReadGroupBlock` for collapsed transcript rendering (inherited — the tool returns Readable=true, the UI layer already folds consecutive read-only calls)
- [x] wire into tool registry (`DefaultTools()` and `ReadonlyTools()`)

## Verification

- [x] `go build ./...` clean
- [x] `go test ./internal/tools/... ./internal/graph/... ./internal/util/... ./internal/agent/...` green
- [x] new tests added where bugs weren't covered by existing tests
