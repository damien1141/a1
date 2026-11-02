## Issues to fix

**Broken — produce wrong output (fix these first)**
1. `coveragetool` regex — parses field 4 (end line) as statement count; Go's coverprofile is `file:startL.col,endL.col numStmt count`, so `numStmt`=field 6, `count`=field 7. Every coverage % is garbage.
2. `watchertool` — `lastCheck` is a package global shared across all paths. Watching dir A then B makes B inherit A's timestamp. Make it per-path (`map[string]time.Time`).
3. `errortrans` — substring `strings.Contains` matching misfires ("attempt to index" etc.). Needs anchored/regex matching.
4. `batchtool` — topological order rides on `graph` regex import parsing (non-treesitter), which misses multi-line `import (` blocks → wrong edit order, and ordering is the tool's whole pitch.
5. `propertytool` — emits non-compiling skeletons (no testify import, `quick.Check` signature mismatch).
6. `secrettool` — counts every pattern hit per line (double-counts); regexes are shallow.
7. `apitool`/`deadcode` `countReferences` — shells to raw `grep -r`, fragile, POSIX-only, no Windows path.

**Dead / unwired**
8. `journaltool` — nothing writes `.journal` anywhere; always returns "No journal entries found." Wire a writer or delete.
9. APPA resolver — `PartialLabel.Resolve`, `CastResolver`, `ResolveCast` are tested but never called from the executor. Unresolved sources accumulate forever. Wire into `admitExecution` or cut.
10. `scratchpadtool` — duplicates the memory bank. Pick one.
11. Five overlapping failure-analysis tools: `error`, `errpattern`, `errtrans`, `stack`, `test`. Combine or collapse into two.

**Design debt**
12. No tokenizer — everything is bytes/4. Compaction cut uses `estimateMessageTokens` but the growth gate reads provider `ContextTokens()`. Two rulers. Pick one.
13. Identity split — module `a1`, dirs `~/.a1`, env `PHI_*`, package `phi`, splash "Phi", temp prefixes `phi-`. Choose one.
14. Config rewrites (`SetDangerouslyAllowAll`, `AppendBashAllow`) round-trip through `map[string]any`→`yaml.Marshal`: strips comments, alphabetizes keys.
15. `extractArchive` shells to system `tar` with no path-traversal guard (the in-Go extractor has one).
16. `/tmp/browser-screenshot.png` is a fixed path — concurrent sessions clobber it.
17. `browser` tool description says "asks the user to open it" but actually just errors.

---

## The ultimate grep tool — combine, don't multiply

You already have three search planes and the infra to merge them. Don't build a fourth grep variant. Build **one tool that fans out to all three and returns ranked, grouped results**, collapsing the model's grep→read→grep loop into a single call.

**The three planes you already own:**
- **Text** — `greptool` (rg): literal/regex, returns `@file` headers + line numbers + context.
- **Structure** — `internal/graph`: `Imports` / `ImportedBy` — semantic "who calls/imports this." Plain rg can't do this.
- **Name** — `findtool` (fd): filename/path-fragment matching.

**Schema (one tool):**
```
grep(query, mode=auto|text|symbol|file, path, limit)
→ JSON grouped by file: { file, matchCount, planes[], topSnippet, rank }
```
`mode=auto` detects: looks like an identifier → also query graph; looks like a path → also query fd; always run rg.

**Plan of action:**
1. **Stabilize the dependencies first** — fix coveragetool, watchertool, batchtool (#1, #2, #4). Grep's ranking will lean on graph + watcher; building on a broken foundation wastes the session. ~1 hr.
2. **Define the combined schema + auto-detect** — one `query` + `mode` param. Return structured JSON grouped by file, not raw lines.
3. **Wire the three planes concurrently** — rg (reuse greptool), `graph.Imports/ImportedBy` (existing), fd (reuse findtool). You already have `readableBatch` parallelism for `Readable` tools — reuse it to fan out.
4. **Rank, don't dump** — score each file by match density + graph distance to the query symbol + recency. Return top-N. This is the actual value-add; unranked grep is what you have now.
5. **Collapse the rendering** — reuse `ReadGroupBlock` so a 30-file search shows as one row ("searched for `X` across 30 files") with expansion, keeping the transcript clean.
