# Phi overhaul — state, findings, plan

> Session history below is reconstructed from the earlier turn log; it predates
> the claudification work and is kept because the findings are still load-bearing.

## Archive — earlier rounds

### Baseline (verified 2026-10-11)
- `go test ./...` → all packages ok, exit 0.
- `go vet ./...` → clean.
- `go test -race -count=1 ./internal/permission` → **FAIL** (pre-existing data race in
  `TestHumanReviewResolver_AsyncCallback`: test wrote `called`/`gotHash` from the
  resolver goroutine with no synchronization, resolver_test.go:44 / :59).
- `go build ./cmd` in /tmp/cfg3 → `/config` served 200 (embed already in worktree).
- `/usr/sbin/a1 config` from /tmp/other → **FAIL**: "config.html not found from /tmp/other".

### Issue 1 — `/config` only works in the phi source tree — FIXED
Root cause: `configserver.Start` read the HTML from disk via `locateConfigHTML()`
(walks up from cwd). Installed binary → no source tree → error.
Verified fix: freshly built binary serves the page (HTTP 200, 79098 bytes) from
/tmp/embedtest; `/api/config` returns 200.
Additional fix: the slash handler resolved the config path through the cwd-seeded
`project.GetDefaultProject()` singleton instead of the session controller, and
tied the server lifetime to a background context that never cancels.
Files: `internal/configserver/server.go` (pre-existing embed), `internal/tui/commands/config.go`,
`internal/tui/controller/controller.go` (`ConfigPath()`).

### Issue 2 — APPA implementation bugs (paper: 2607.24625v2) — FIXED
Deviations found (file:line → paper ref) and their fixes:

1. `gate.go:277` prospective evaluation gated behind `dec == Ask` → now unconditional
   (§4 / Prop. 4). New `EvaluateProspective` is exported so the executor can name the
   gaps an authority ruling covers.
2. `gate.go:347` `Admit` committed effect tokens unconditionally → now gated on
   `req.Succeeded` (§3: "failed or indeterminate outcomes contribute none").
   Added `Request.Succeeded`.
3. `gate.go:356-367` unannotated return folded the *declared* contribution → now
   recorded as an unresolved source (`out:<tool>` in `Usrc`, §4).
4. Gate 2 never verified the realized return → `Policy.RequireLabeledOutput` opt-in;
   `NoUntrustedOutput` still denies up front.
5. `gate.go:244` `Policy.Authority.(Authority)` without comma-ok + `GapsCovered: []string{"consult"}`
   → ruling records the real gaps; a ruling with no target gap is refused.
6. `gate.go:196` + `controller.go:248` workspace re-derived from process cwd →
   `NewGateAt` / `WorkspaceRootAt` resolve from the session cwd (TUI + `a1 run`).
7. `label.go` `TrustOrder` put `Unknown` on the chain → zero value is now
   `Unspecified` (off-chain). `TrustOrder` returns -1 for it.
8. `label.go:269` `IsNarrower` used an effect-token count tie-break → removed;
   narrowing is trust-rank + reader-set only.
9. `label.go:64` `Label.EffectTokens` conflated L and E → field removed.
10. `gate.go` `AcceptedNarrowing bool` reset on every Admit → now a
    `map[string]bool` keyed by call hash, recorded at acceptance time.
11. `gate.go:332` `Search(..., 3)` overrode `RecoveryGraph.MaxDepth` →
    `g.recovery.MaxPaths()` (new accessor, default 3).
12. `confinement.go` `ChildTrajectory`/`Merge`/`AttestSchema` unreferenced →
    now wired: `EngineRunner.ParentGateFn` → `permission.ForkChildGate` implements
    `L0c := Lp`, shared effect log, scope-bound acceptances/rulings (§6, Thm 8).
13. `resolver.go:80` fire-and-forget goroutine (documented "Timeout is ignored") →
    not changed; the fix is that the async resolver is now unreachable in the
    authority path (see 5) and the race in its test is fixed.
14. `executor.go:451` `consultAuthority` → uses `StaticGateOf` (unwraps
    SessionAllowGate/ConfinementGate) and passes the real gaps.
15. `executor.go:384` admission Request had no realized label / outcome →
    now sets `OutputLabel` and `Succeeded`, routes an Ask through
    `ApproveAdmit`.
16. `gate.go` initial trajectory seeded at `{Trusted, []}` (lattice bottom) →
    now `{User, TopReaderSet}`; `TopReaderSet` is the reader universe.
17. `contract.go` read/write contracts declared `ReaderSet: ["user"]` and bash
    `[]` → all use `TopReaderSet` so a read/write is not a narrowing.
18. `contract.go` `PreconditionTrustFloor` was inverted
    (`TrustOrder(a) < TrustOrder(b)` rejects correct data) → direction fixed and
    off-chain trust now fails closed.
19. `label.go` `MeetTrust`/`JoinTrust` returned `Trusted`/`Untrusted` wrongly for
    mixed trust → both now correct GLB/LUB (meet = more restrictive wins).
20. `recovery.go:311` `contract.Contribution.Trust != Unknown` → `IsKnownTrust`.
21. Pre-existing data race in `TestHumanReviewResolver_AsyncCallback` → joined
    through a channel + `sync.Once`.

### Issue 3 — TUI doesn't show what's happening — FIXED
1. `write`/`edit` rows render as a `DiffBlock` card —
   `✓ internal/path.go +12 -3`, unified-diff body with old/new line numbers,
   +/- colored per theme roles, dim separator between hunks, file headers dropped,
   failed edits show the error immediately. Routes in `Mapper.toolWidget` /
   `patchTool`; `isEditTool` is the single predicate.
2. The composer status slot now names the running tool and its target
   (`Running edit (internal/permission/gate.go)…`), falls back to a count when
   several run concurrently, and still ignores user `!cmd` runs. New
   `session.RunningToolCall` picks in-progress over queued deliberately rather
   than by map iteration order.
3. Generic tool rows report result volume (`✓ grep Patterns (2 lines)`) via
   `ToolBlock.resultStat`, shown only on a successful non-empty result.

Verified by rendering a full turn end-to-end (assistant text + grep + edit +
bash) through the Mapper and inspecting the surface text.

### Bonus (pre-existing, unrelated to the 3 issues)
- `internal/tools/watchertool/watcher.go` `lastCheck = time.Now()` was recorded
  *after* the filesystem walk, so a file written during the walk was lost until
  the next scan. Now `scannedAt` is captured before the walk.
- `watcher_test.go` `TestRunWatcher_WithGlob` leaked the package-global
  `lastCheck` from sibling tests → now passes an explicit zero `since`.

### Verification (earlier rounds)
- `make fmt` / `make fmt-check` → clean.
- `go vet ./...` → clean.
- `go test -race -count=1 ./...` → all ok, two consecutive passes.
- `go test ./...` (no race) → all ok.
- `make deadcode` → only pre-existing unreachable symbols remain (baseline was
  already red: `countIndent`, `SetDangerouslyAllowAll`, `ValidateHash`, the
  `sanitized.go` helpers, …). My additions add nothing.
- `make lint` → red, baseline red. Verified by stashing: 32 findings in
  `internal/permission` alone predate this work. My files contribute style-only
  findings (`perfsprint` on pre-existing `String()` builders, `revive`
  unused-receiver). Not mass-reformatted: that would be an unrequested refactor.
- Pre-existing test failures (identical on `git stash -u`):
  `TestTryAttachClipboardImageFallsThroughWhenSupported` and
  `TestTryAttachClipboardImageAllowedWithoutModelInfo` in
  `internal/tui/composer` (clipboard detection, environment-dependent).

### Round 3 — user feedback (all addressed)
- **Edit card was good, read is fine, write needed work.** `write` on a new file
  said only "Created new file." and showed nothing; on an overwrite it printed an
  `@file path#TAG` header left over from the hashline system removed in 975ca05
  (that commit stripped the *anchors* from tool schemas but the tools kept
  *emitting* the header). New file now renders its content as `+` lines with a
  line count; an overwrite shows a real unified diff. `internal/util/hash.go` was
  fully dead afterwards (grep-verified, including read/grep which 975ca05 had
  already cleaned) and is deleted with its tests.
- **All tool rows now expand by default**, toggleable via a new
  `ui.auto_expand_tool_output` config key, surfaced in the web config editor
  under a "Terminal UI" panel (EN + ZH i18n). Wiring: `project.UIConfig` →
  `cmd/tui.go` → `editor.NewEditor` → `TranscriptPane.SetAutoExpandToolOutput` →
  `Mapper.AutoExpand` → `Mapper.autoExpandFor`. A user toggle on a row always
  wins, because `Mapper.autoExpandFor` consults `m.expanded` with the `ok` idiom
  before falling back to the config default. Verified round-trip: POST the flag
  through the live config server → `ui.auto_expand_tool_output: true` in
  ~/.a1/config.yaml → GET returns it. (The user's real config was restored from
  the .bak the server writes.)

### Round 4 — read/grep styling + auto-expand default ON
- **read/grep now render as cards.** New `block.ReadBlock` gives them the same
  shape as DiffBlock but with a line-number gutter: `✓ read haiku.txt (3 lines)`
  / `✓ grep "cursor" in . (1 lines)`. The `@file path` header line the tools
  emit is stripped (already in the card header), grep's `path:>>  12|` prefix is
  normalized to the same gutter read uses, and the header prints one verb plus
  the target (not two).
- **auto_expand_tool_output defaults to ON.** The config field is now a
  `*bool`, so an absent key stays nil and resolves to true via
  `AutoExpandToolOutputResolved()`; verified against the real
  `~/.a1/config.yaml` which has no `ui` block. The editor treats absent as
  checked so the box reflects the effective setting.
- **Two pre-existing bugs surfaced while wiring this, both fixed:**
  1. `grep` overwrote its own `Detail` with the match count on completion,
     erasing the arg-derived phrase that identifies the search in the
     transcript.
  2. `webserver.Server.policy()` fell back to a zero `permission.Policy{}`,
     whose `BashDefault` is the zero value `Allow` — a Server built without a
     loaded config allowed every bash command. Now falls back to
     `DefaultPolicy()`, which asks. Regression test added.
- **Hash-lineage cleanup** (after the user spotted a trace): dead
  `internal/util/sanitized.go` removed (zero callers), the `hashline-compatible
  line hashing` line in `internal/util/doc.go` dropped, and the AGENTS.md rule
  "keep hashline `edit`" replaced. The live one was
  `internal/agent/prompt/system-prompt.tmpl`, which told the model to set
  `edit.hash` to "the 4 hex chars after `#` in `@file path#TAG`" — a field that
  no longer exists — replaced with `old_str` / `new_str` naming, with a test
  guard in `prompt_test.go`. Verified the tools' real output emits no `#TAG` /
  `LINE#HASH`.

### Open items carried forward
1. `ChildTrajectory.Return` / `Merge` are still only reached through the fork
   path in tests — the sub-agent return channel is not yet shaped by an
   attest-schema in production.
2. `resolver.go` `HumanReviewResolver`'s undocumented "Timeout is ignored" field.
3. Consider `Policy.RequireLabeledOutput` in the config editor.


# Claudification — implementation log

Goal: land the ports from `claudification.md` in phases. **Memory (item 6) is
explicitly deferred** — the user wants to get into the weeds on that one
separately.

## Decisions locked this session

- **Item 1 is a renderer, not a tool.** `TODO.md` at the project root is the
  agreed file. No `todoread`/`todowrite`, no schema, no persistence, no snapshot
  fields. The mapper synthesizes the row from the file's mtime + content hash,
  so the row exists whether or not the model cooperates.
- **Plan mode is dropped**, folded into item 1's reasoning. `ExitPlanMode` reads
  its plan from a file, so the tools are machinery around a text file.
- **Item 7a (`AskUserQuestion`) leads the queue** — phi already has the whole
  transport in the permission overlay.
- **Item 7c (collapse read/search runs) is mandatory**, not optional: the
  read/grep cards added earlier made each row bigger, so a 15-file exploration
  is now 15 large rows.

## Phases

| Phase | Contents | Status |
|-------|----------|--------|
| 1 | `Byline` + sub-agent current tool + interrupt CTA (small, independent) | **done** |
| 2 | Checklist rendering (`TODO.md`) | **done** |
| 3 | AskUserQuestion tool + overlay | **done** |
| 4 | Collapse read/search runs | **done** |
| 5 | Transcript cursor: copy + edit past message | **done** (copy only) |
| 6 | `/context` token breakdown | **done** |
| 7 | Tool presentation: fetch/browser render modules | **done** |
| 8 | Memory | **deferred** |

## Log

### Phase 1 — small independent ports — DONE
Gates: `make fmt-check` clean, `go vet ./...` clean, `go test -count=1 ./...` all
ok (no failures at all this round — the clipboard pair is environment-dependent
and did not trip), `make deadcode` adds nothing.

1. **`internal/components/byline.go`** (new). `Byline(items ...string) string`
   joins non-empty items with a dim ` · `; `BylineSpans([]BylineItem, sep)` is
   the styled form for span-based call sites, where each item keeps its own
   style and only the separator takes `sep`. Both drop empty items, which is the
   point — callers assemble candidates and get whatever existed, with no
   `"a · · b"`. Ported from their React `Byline.tsx`.
2. **Composer status slot** (`internal/tui/controller/activity.go`): the
   running-tool label went from string-concatenated `Running edit (path)…` to
   `Running edit · path…` via Byline. Test updated to the new shape.
3. **Interrupt CTA** (`activity.go`): `ActivityCancelled` now renders
   `Stopped · What should I do instead?` instead of a bare `Stopped`. The
   `...OtherActivitiesUnchanged` case list dropped `ActivityCancelled` (it is
   exactly the thing that changed) and a dedicated test guards the new text.
4. **Sub-agent current tool** (`block/agent_block.go`): new `Activity` field,
   shown in the header as `→ grep · cursor in .` only while the row is running.
   New `block.LastRunningChild(children)` picks it — scans rather than trusting
   order, because `SubagentStore` appends in completion order. Falls back to the
   most recent child so a resolved row still shows what it last did.
   `mapper.go` `fillAgentBlock` populates it; `patchTool`'s `agentHeightSnap`
   gained `Activity` and the dirty check includes it, otherwise an
   already-mounted row would keep showing a stale tool.

Verified by render: `⋯ agent_spawn find the writer → grep · cursor in . ▼` over
`├── ✓ read haiku.txt` / `╰── ⋯ grep cursor in .`.

### Phase 2 — checklist rendering (`TODO.md`) — DONE
Gates: `make fmt-check` clean, `go vet ./...` clean, `go test -count=1 ./...` all
ok, `go test -race` clean on the touched packages, `make deadcode` adds nothing
(an unused `ChecklistSource.entID` was added by accident and removed).

**`internal/todo/` (new package)** — parser for the agent's markdown task file.
`Parse(path)` / `ParseText(text)` return `[]Item` where Item is
`{Indent, Text, Done, InProgress, Detail}`. Three item kinds, and the third is
the one that matters:

- **tasks** carry a box (`- [ ]`, `- [x]`, `- [~]`) and are the only things the
  counts include.
- **details** are indented unboxed bullets — sub-notes under a task. They render
  indented and dim and are never counted, because nothing has been claimed about
  their state.
- everything else (prose, headings, tables, quotes, ordered lists) is ignored.

Bugs hit and fixed while building it, all found by tests:
1. **Indent used one counter for bytes and columns.** A tab is 1 byte / 4
   columns, so `indent += 4` skipped past the bullet and dropped the line. Now
   `i` walks bytes and `cols` walks display width.
2. **`Parse` returned nil for a directory** — `os.Stat` succeeds on a dir, so
   the `err != nil || info.IsDir()` short-circuit returned a nil error and the
   caller saw an empty file. Now an explicit `os.ErrInvalid`.
3. **Code fences weren't tracked**, so `- [ ]` samples inside an example block
   were counted as tasks. Tracked with the open marker so a ~~~ block can't be
   closed by ```.

**`internal/components/block/checklist_block.go` (new)** — the widget.
`☑ TODO.md 1 done · 2 open ▶` header, then when expanded `▸` in-progress, `◦`
open, `✓` done, with dim unboxed details beneath their parent. Ordering puts
in-progress first, then open, then done — work still ahead of you outranks
history. Row cap 10 (or `MaxRows`), remainder reported as `… +N more` rather
than silently dropped. `StaleFor > 0` renders
`⚠ N file changes since this list moved`, which is the mitigation for the
compliance risk: a list the agent stopped updating is called out instead of
trusted.

`CopyText` preserves each line's original shape — a detail copies back as an
unboxed bullet, because writing `- [ ]` over it would turn a note into a task
nobody claimed.

**Mapper** — the row is *synthesized*, not projected. `Session.ItemKind` is
untouched, because a checklist is workspace state rather than conversation
content: it has to exist before the first message and survive a replay. New
`ChecklistSource` stats the file (mtime+size) and re-parses only on change, so
an idle frame costs one `stat` and no relayout. `StaleFor` is computed from
succeeded edit/write rows via `countEditTools`.

Three bugs here, again caught by tests:
1. **The checklist row was never marked dirty.** `Sync` appends it but the
   `dirty` list only covered projected rows, so its height never invalidated on
   a file change. Now compared against the previous widget via
   `checklistEqual` and dirtied only when content moved.
2. **`refresh()` ran after the staleness count was computed**, so on the very
   first sync (file already present) `loaded` was false and `StaleFor` stayed 0.
3. **`byID[ChecklistSourceEntryID]` was assumed to be index 0.** The dirty check
   indexed `entries[0]` directly, which is only right when the checklist is the
   first entry. Now resolves the previous index from `byID`.

**Wiring:** `editor.NewEditor` sets
`transcript.SetChecklistFile(filepath.Join(cwd, "TODO.md"))`; the pane hands the
Mapper a `NewChecklistSource`. Absent file → no row, so this is safe before
anything is written, and creating the file mid-session starts the row on the
next sync.

**System prompt** — new `# Tasks` section (guarded by
`TestBuildIncludesTaskInstructions`, since with no tool holding the state the
prompt rules *are* the mechanism). It specifies the three markers, says exactly
one `- [~]`, requires unboxed second level for notes, bans staleness
("must not go stale … complete or deleted, never left sitting there"), and
scopes it to 3+ steps so small work doesn't get ceremony.

Also removed a duplicated `SetModelLabel` seed block in `editor.go` that I hit
while editing nearby — exact duplicate comment+statement, no behavior change.

### Phase 3 — AskUserQuestion — DONE
Gates: `make fmt-check` clean, `go vet ./...` clean, `go test -count=1 ./...` all
ok, `-race` clean on touched packages, `make deadcode` adds nothing.

An `ask_user` tool: the agent asks a structured multiple-choice question
mid-turn and gets the answer back as the tool result. Nothing is approved or
denied — an answer is collected.

**Types, deliberately in two places.** `tooldef.Question` /
`tooldef.QuestionOption` / `tooldef.QuestionReply` live in the tool layer so the
tool never imports the TUI. `controller.Question` /
`controller.QuestionOption` / `controller.QuestionReply` mirror them in the
controller message layer. `EngineController.askUserQuestion` is the only
translation point — duplicating four small structs is cheaper than a
tools→controller import cycle, and it keeps the UI free to add fields the model
never sees.

**Transport reuses the permission modal**, which is why this was a small change
rather than a new subsystem: `OverlayQuestionAsk` / `OverlayQuestionDismiss`,
`OverlayMsg.Question` / `QuestReply`, `EngineController.AskQuestion` (same
`waitReply` + timeout + dismiss as `askPermission`), and a `questionAskState`
that renders and handles keys the way `permAskState` does. The composer's
`Wire` gained one `handleQuestionKey` slot; the editor passes
`o.overlays.HandleQuestionKey`.

**Schema is constrained on purpose**: 2-4 options, both required, header capped
at 12 runes (the reference's chip width). Outside that range the tool refuses
with a message saying *"a question outside that range is a decision you should
make yourself"* — a model that offers one option is asking a leading question,
and one that offers eight is asking the user to do its triage.

**"Other" is always present** and is not one of the agent's options. A question
with no free-text path would trap the user inside the agent's framing, so the
row is appended unconditionally plus an inline text field.

**Fails closed.** No asker on the context → `ask_user is unavailable: no
question handler attached to this session`. A hang there would look like a hung
model; this looks like a wiring bug.

Rendered:
```
│  Approach                                                            │
│ How should the parser handle quoted commas?                          │
│ ▸● csv-aware (Recommended)                                           │
│     Use a real CSV reader; handles embedded quotes and newlines.     │
│  ○ split on comma                                                    │
│  ○ Other                                                             │
│ ↑↓ move · Enter select · Esc skip                                    │
```
Multi-select adds a checkbox on the option's own row (`▸● [ ] a`) — the checkbox
was first emitted as a separate wrapped span, which put it on its own line and
was caught by rendering rather than by a test.

**Bug found while reviewing my own diff:** `chrome.AskHint` already prefixes
`Enter ` / `Esc `, so my first hint strings produced `Enter select · Esc esc to
skip`. The helper owns the key names; the caller passes bare verbs.

**Prompt** — new `# Questions` section, guarded by
`TestBuildIncludesQuestionInstructions`. The rule that matters most and decays
first: *not* a confirmation dialog. Plus "ask one question per call", "do not ask
what the code can tell you", and "after an answer, act on it — do not re-ask the
same thing later in the turn".

### Phase 4 — collapse read/search runs — DONE
Gates: `make fmt-check` clean, `go vet ./...` clean, `go test -count=1 ./...` all
ok, `-race` clean on touched packages, `make deadcode` adds nothing.

```
Read 2 files, searched for 1 pattern
  ├── ✓ read haiku.txt
  ├── ✓ grep "cursor" in .
  ╰── ✓ read main.go
```

This is the regression fix, not a new feature: the read/grep cards added earlier
made every row large, so a 15-file exploration became 15 large cards.

**`session.CollapseGroups` + `collapseRun`** (`internal/session/project.go`) —
folds a run of read-only tool_use blocks (`read`, `grep`, `ls`, `glob`, `find`)
into one `Item` whose `ToolRun.Group` (`Members []string`, `Reads`/`Searches`/
`Lists`, `Hint`) carries the counts. `ToolRun.Group` is nil for every ordinary
row, so nothing else had to change.

Three rules that make it a fold rather than a lossy one:

1. **Text, a non-collapsible tool, or an error breaks the run.** An error stays
   its own row because folded into a count it is unactionable — test-enforced in
   both directions.
2. **A single call is not a group.** One read keeps its own card; collapsing it
   would hide one card behind an expand arrow for no gain.
3. **An unreported member keeps the group in-progress.** `collapseRun` routes each
   member through `mergeToolRun` before reading its status, so a member the
   executor has not reported on yet (the common state mid-run) does not let the
   group claim completion. This was a real bug: reading `tools[id]` directly
   returned the `ToolQueued` zero value for absent members.

**`block.ReadGroupBlock`** (`internal/components/block/readgroup_block.go`) —
summary header with the reference's tense flip (`Reading 2 files` while in
flight, `Read 2 files` resolved), tree-gated member rows on expand, and the
member detail resolved at *draw* time through a closure so live results arriving
after the row was built still show. The current target appears as a hint only
while running; a resolved row's last target is history, and the expansion carries
every path anyway.

**Mapper** — `isReadGroupTool`, a branch in both `widgetFor` and `patchTool`, the
`expanded` capture case, and `groupMembersFrom` which resolves members against
the mapper's snapshot via `findGroup`. The closure captures the *item id*, not
the `run` — capturing the run froze the member set at creation time, which took
two rounds to diagnose.

`stripLeadingVerb` drops the duplicated verb in member rows: the arg-derived
phrase is already `grep "cursor" in .`, so naively printing `name + detail` gave
`grep grep "cursor" in .`. Word-boundary aware, so `grep` does not eat
`grepfile`.

**Bug I introduced and fixed mid-phase:** I first appended `CollapseGroups` after
`projectAssistant`'s own block loop, which (a) emitted thinking/text blocks a
second time and (b) moved a trailing text block to the end of the item list
instead of keeping it between the tool calls. `TestProjectOrder` caught both —
the text-after-tool ordering one is exactly what a test written for the old shape
protects.

### Phase 5 — transcript cursor (copy) — DONE
Gates: `make fmt-check` clean, `go vet ./...` clean, `go test -count=1 ./...` all
ok, `-race` clean on touched packages, `make deadcode` adds nothing.

A keyboard cursor over the transcript. Entered with **shift+up**, then `j`/`k` or
arrows to move, `c` to copy the block under the cursor, Esc to leave. The
current position and the keys show in the composer status slot
(`↑↓ move · c copy · Esc close · 2/3`), the same slot that names a running tool.

**Why this exists:** every block already computed `CopyText` — `DiffBlock`,
`ReadBlock`, `AgentBlock`, and now the group and checklist rows — but there was
no keyboard path to it. Only a mouse selection, which does not work over ssh.

**Deliberate scope cut: Enter-to-edit a past prompt is not implemented.** The
cursor exposes `ItemID()` for it and `UserBlock` now carries its session item id,
so the seam is real, but re-running a turn from a message index means session
history surgery: truncate-and-replay semantics, gate/label consequences under
APPA, and a decision about whether the forked branch keeps the old timeline. That
is its own design, not a port, so I stopped at the half that is unambiguous.

**`MessageList.ScrollToEntry`** (`internal/components/transcript/`) — scrolls an
entry into view by walking the cached heights above it. Needed because the cursor
has to reach entries far above the viewport, which paging never had to.

**Keybinding choice:** `shift+Up` (Ctrl-free) is the *movement* key, not Up —
so it can never move the caret or trigger history. Ctrl+key falls through to the
composer always, which is what keeps Ctrl+C as the interrupt chord rather than a
copy.

**Modifier discipline is test-enforced:** while the mode is active, any
Ctrl/Alt-modified rune returns false so the composer keeps it. Without that,
entering the mode would silently break every shortcut.

**Cursor survives a no-op sync, exits on a rebuild.** `TranscriptPane.Sync`
compares the old and new id lists; identical means the cursor keeps its exact
index, otherwise it exits rather than silently retargeting onto a different
block. Moving-by-index across a rebuild is how a cursor ends up copying the wrong
thing.

**Two real bugs found while verifying, both caught by looking rather than
assuming:**

1. My first highlight probe read `root.Buffer` and found zero background cells,
   which looked like the selection highlight was dead. It was not — the
   highlights live on the entry sub-surfaces in `root.Children`. The regression
   test I added as a result inspects `Children` and asserts exactly *one* child
   carries a fill, so a future bug in either direction (no highlight, or a fill
   bleeding across every row) fails.
2. `UserBlock` had no `ItemID`, so `MessageActions.ItemID()` returned empty and
   the edit seam was decorative. `mapper.go` now populates it from the projected
   item in both `widgetFor` and `patchItem`, so it is real data rather than a
   placeholder.

**Files:** `internal/tui/transcript/message_actions.go` (new, the cursor),
`message_actions_test.go` (new), `internal/components/transcript/message_list.go`
(`ScrollToEntry`), `selected_highlight_test.go` (new),
`internal/components/block/user_block.go` (`ItemID`), plus wiring in
`pane.go` / `editor.go` / `composer/pane.go` / `footer.go`.

### Phase 6 — `/context` token breakdown — DONE
Gates: `make fmt-check` clean, `go vet ./...` clean, `go test -count=1 ./...` all
ok, `-race` clean on touched packages, `make deadcode` adds nothing.

```
Context estimate (~4 bytes/token, provider counts will differ)

System prompt       7,037
Tools               2,350  34 registered
Messages               10
Thinking                9
Tool output            27

Total 7,083 / 200,000 (3% used)
```

**`internal/contextbreakdown/`** (new) — `Build(snap, contextWindow, toolBytes,
toolCount) Breakdown` with categories `System prompt`, `Tools`, `Messages`,
`Thinking`, `Tool output`.

**Honesty about precision, which is the whole design constraint here.** phi has
no tokenizer dependency, and adding one (`tiktoken` / `go-tiktoken`) is a new
direct dep, which AGENTS.md forbids without a clear need. So the counts are bytes
÷ 4 — the standard English-text heuristic — and the output states the ratio in
its header: *"~4 bytes/token, provider counts will differ."* Claude Code renders
a grid of squares sized by share; a grid implies precision this estimate does not
have, so phi prints a table instead.

**Provider usage counters are deliberately not used.** `session.TokenUsage`
reports what one call cost, not what the current context holds. Summing those
would produce a readout that disagrees with the provider's own billing — the
exact failure mode of a context meter that lies.

**`Engine.ToolSchemaBytes()`** (`internal/agent/engine.go`) — sizes every
registered tool definition and counts them. Schemas are context paid before the
conversation starts and they grow as extensions and MCP servers register, so the
`Tools` row is the one that surprises people. It notes the count beside the
estimate (`34 registered`).

**`Category.Note` marks informational entries**, and `Total()` excludes them —
a future "deferred tools" category can be added without inflating the total.

**Two real bugs caught while writing it:**

1. **Tool outputs were multiplied.** My first loop summed `snap.Tools` inside the
   per-message loop, so every tool result counted once per message. In a 10-message
   turn the "Tool output" row was 10× too large. `TestToolOutputCountedOnce` pins
   it: 400 bytes of output through two messages estimates 100 tokens, not 200.
2. **An unknown context window read as "0% used"** — indistinguishable from
   "plenty of room". Now reports `window unknown for this model`, because a model
   whose window phi does not know should not look like an empty context.

**Why it lands in the transcript rather than a toast:** a toast is one line and
this is a table. `/context` goes through
`SessionEventMsg{AssistantMessageUpdate}` the same way a loop error does, so it
reads as a normal assistant block and scrolls with the conversation.

**Accessors added:** `EngineController.ToolSchemaBytes()` and
`FooterChrome.ContextWindow()`. Both read state their owners already had; no new
plumbing through the engine.

### Phase 7 — fetch / browser presentation — DONE
Gates: `make fmt-check` clean, `go vet ./...` clean, `go test -count=1 ./...` all
ok, `-race` clean on touched packages, `make deadcode` adds nothing.

The design-cue port from Claude Code's `WebSearchTool/UI.tsx`: a per-tool render
module separate from execution. Before:

```
✓ fetch fetch https://example.com/docs 200 OK ▶
Fetched GET https://example.com/docs
============================================================

Status: 200 OK
Content-Type: text/html

hello
```

After:

```
✓ fetch https://example.com/docs 200 OK · 42.5 KB · 1.2s · example.com ▶
✓ browser content Read · example.com · 7 results ▶
✓ browser screenshot saved browser-screenshot.png ▶
✓ browser https://example.com/pricing Navigated · example.com ▶
```

**The seam was already there, just unused.** `tooldef.Result` has `Detail` for a
one-line row label and `Output` for the display body — but fetch and browser both
built their *display chrome* into `Content`, which is also what the model reads.
So a `====` rule and a `Status:` / `Content-Type:` preamble were paid for twice:
once in the transcript as noise, and once in tokens as something the model does
not need.

**`tooldef.Result.Summary` (new field)** — the one-line *outcome*, kept distinct
from `Detail`, the call's *identity* for the whole run. That distinction is the
whole fix: the bug was tools overwriting `Detail` at completion, so a row that
started as `fetch https://…` ended as `200 OK`, and `browser content` ended as
whatever the page title happened to be. `Detail` now never changes after
`DetailFromArgs`; `Summary` is where a completion stat belongs. Threaded through
`ToolRun.Summary` → `Executor.run` → `Mapper` → `ToolBlock`, where a real summary
wins over the generic `(N lines)` count and the fallback stays for tools that set
none.

**`fetchtool/ui.go` and `browsertool/ui.go` (new)** — per-tool render helpers.
`Summary(...)` composes the outcome; `HostOf` reduces a url to its host;
`Running(...)` is the in-flight line. Both tools' chrome moved out of `Content`
and into these, with the status line kept in `Content` because the model
genuinely needs it.

**Three fixes the port forced, each caught by rendering rather than by a test:**

1. **`fetch fetch https://…`** — the row prints the tool name and `DetailFromArgs`
   also prefixed one. Same duplicate-verb problem as the read group earlier.
   `DetailFromArgs` now returns the bare url.
2. **Browser rows printed whole urls.** `Summary` now reduces a url target to its
   host, leaving selectors and queries intact. The full url stays in `Content`
   and in the expanded body where it can be copied.
3. **`browser screenshot`'s identity was its file path**, so the row was
   `/tmp/browser-screenshot.png`. Identity is now `screenshot`; the summary is
   `saved browser-screenshot.png`; the full path is in the body.

**Not done, deliberately:** progress rows. `Running(...)` exists on both tools and
`fetchtool` records `startedAt`, but emitting a mid-flight row needs a session
event the executor does not publish during a blocking call — the row only appears
once the tool returns. That is a real change to the executor's streaming path, and
a fetch that shows nothing for two seconds is a cosmetic gap, not a correctness
one. The helpers are in place so it is a small addition later.

**Bugs caught by tests, not by eye:** the two `hostOf` implementations started
inconsistent — fetch stripped a port, browser did not. Caught by a test asserting
the same input against both.

### Post-phase fix — fetch / browser now collapse too
Found by the user ("fetchs and reads arent collapsed"), verified by running the
projection directly: `read` folded, `fetch` and `browser` did not.

`collapsibleReadTools` gained `fetch` and `browser`, and `collapseRun` buckets
them into a new `Fetched` count rather than letting them fall through to "Read"
(a fetch is not a file). Summary now reads
`Read 1 file, searched for 1 pattern, fetched 3 pages`.

Two follow-ons the fix forced:

1. **`ChildTool.Summary` (new).** A fetch member's status and size are the answer
   and its url is provenance — the opposite priority to a read, where the path
   *is* the answer. `memberSpans` shows Summary ahead of Detail, so a member row
   reads `fetch 200 OK · 12 KB · 0.4s · a.test` instead of a bare url running to
   the terminal edge.
2. **Tests for the case that already existed now cover it.** Failed fetches stay
   their own row (a dead url is actionable, a count is not), and `Fetch` /
   `Browser` with model casing fold too — the replay path restores casing, which
   is the same trap that bit `Read`/`Grep` earlier.

`ToolGroup.Fetched` added so the group carries the count rather than the UI
recounting from member names.

### Bug report: "the browser tool got broken somehow" — FIXED (post-phase-7)

Two real bugs, both introduced by me and neither caught by tests that existed
when I shipped Phase 7. One was visible in the code the whole time.

**1. `browsertool` was marked `Readable: true`, so a batch of browser calls ran
concurrently against one shared page.**

`Executor.readableBatch()` runs any batch whose calls all target a `Readable`
tool concurrently — sound for `read`/`grep`, which have no side effects. But
every browser action mutates one shared page: `open`, `navigate`, `click`,
`type`. Two calls interleaved produce nonsense — a click landing before its
navigate. `browserMu` makes it *safe*, not *correct*, which is exactly the kind
of thing that passes a race detector and is still broken.

`Readable` is now false, with a comment saying why. `TestBrowserTool_Definition`
had been asserting `Readable: true` — i.e. pinning the bug — and now asserts
false. A second test, `TestBrowserToolIsNotReadable`, names the failure mode
directly.

**2. `browser` was in `collapsibleReadTools`, so a sequential browser chain
collapsed into one summary row.**

`open → click → type` rendered as a single `✓ fetched 3 pages ▶` row. For reads
that folding is right; for the browser it destroys the order and hides what each
step did — and the count was wrong in kind, since those were actions, not pages.
`browser` is removed from the set; `fetch` stays, since a batch of independent
GETs genuinely folds. Tests rewritten to pin the correct behavior:
`TestCollapseGroupsFoldsFetchButNotBrowser`,
`TestCollapseGroupsDoesNotFoldBrowser` (asserts the three steps stay in order),
and `TestCollapseGroupsBrowserBreaksFetchRun`.

**Also fixed while here: `Result.Detail` was clobbering the row's identity.**
 
`internal/agent/executor.go` overwrote the arg-derived `Detail` with the tool's
returned `Detail` at completion. So `browser navigate https://x` rendered as
whatever the page title was, and `fetch https://x` as `200 OK`. This was the
exact bug Phase 7 was supposed to fix, and I had added `Summary` for the outcome
without removing the `Detail` clobber — half the fix. `Detail` is now frozen at
`DetailFromArgs`; only a tool with no `DetailFromArgs` (identity still the raw
args JSON) yields to `result.Detail`, tracked by a `detailIsArgs` flag.
 
Guarded by `TestExecutorKeepsArgDerivedDetailOverResultDetail` and
`TestExecutorUsesResultDetailWhenArgsGiveNone`.
 
**Verified by rendering** — the five-call turn now produces
 
```
✓ fetched 2 pages ▶
✓ browser browser open Opened · a.test
✓ browser browser click Clicked · #next
✓ browser browser type Typed into · #search
```
 
**On the `browser browser open` duplication:** `DetailFromArgs` returns
`browser <action>` and the row prints the tool name, so the verb appears twice.
Not fixed yet — `DetailFromArgs`'s phrase is what the identity is, and dropping
the prefix means changing it for every action including the ones whose identity
is a url rather than a verb. Flagging rather than guessing, since the same
question applies to `fetch` (already fixed there by returning the bare url).
 
**Gates:** `make fmt-check` clean, `go vet ./...` clean,
`go test -count=1 ./...` all ok, `-race` clean on agent/session/tools,
`make deadcode` clean.
 

