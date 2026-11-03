# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Yolo mode turns the bash sandbox off.** Containment exists to
  substitute for human approval, and yolo withdraws approval wholesale —
  walls standing in for prompts nobody is sending just get in the way
  (writes outside the workspace an inspection workflow needs,
  no_new_privs on the child, network egress cut). Three pieces had to
  agree, or one of them would lie about walls the others never raised:
  - **The gate stops claiming containment.** `sandboxEnforcing` returns
    false for a yolo policy, so checkBash's containment branch never
    answers with a "sandboxed: writes confined" note on a run that
    executes untcontained. The decision is unchanged — yolo folded the
    would-be Ask to Allow anyway — what changes is the gate no longer
    misreports the mechanism. Switching out of yolo re-arms containment
    immediately (every mode switch rebuilds the gate).
  - **The bash tool compiles no profile** for foreground and background
    shells alike; without the background half, a detached command
    launched from a yolo turn would have been the one place the sandbox
    kept enforcing.
  - **The live mode reaches the tool.** The executor attaches the session
    gate's mode to the tool context (`tooldef.WithPermissionMode`, a
    string on purpose — tooldef sits below permission in the import
    graph), so a shift-tab cycle or a web dropdown flip is honored
    mid-session without a config rewrite; the config file's
    `permissions.mode` stays the fallback for engine-less invocations.

  What still fires in yolo is the regex deny list (sudo, rm -rf,
  curl|sh) — yolo is deliberately a mode rather than an allow-all flag,
  and the deny rules live in the gate, not the sandbox. The web mode
  switcher's yolo line now says so ("no prompts, no bash sandbox —
  dangerous, denied tools stay denied"), and the persona editor's mode
  picker matches. Pinned by tests on both sides of the agreement:
  `sandboxEnforcing`/checkBash notes in permission, profile suppression
  (context mode, config mode, interactive still compiles, off stays off)
  in bashtool.

### Added

- **`judge` classifies files without loading them — `files` glob mode.**
  `code mode=judge` (and the `judge` tool) accepts a `files` glob plus the
  usual questions: the tool expands the glob (20 files default, 100 cap),
  truncates each file's head to 24KB, skips binaries, and asks the
  decision model per file in one concurrent batch (4 at a time). The
  result is one verdict with confidence per file — a classification
  question like "which of these contain a TODO?" costs one sub-cent
  round trip instead of dozens of reads filling the context. The system
  prompt routes classification questions over files to it (answers
  reported as given; grep/read fallback when no decision model is
  configured), and the judge mode's docs no longer claim Ollama-only —
  it routes through whatever jev provider is configured (api or
  ollama).

- **Compaction cuts at semantic boundaries when a decision model is
  configured.** The positional cut knows token budgets, not tasks — it
  would land between a tool call and its result and let the mid-turn
  turn-prefix summary patch the orphan afterwards (a second summarizer
  call running in parallel with the history summary). Now the candidate
  boundaries nearest the positional cut (±3, subject to the kept-zone
  constraints) are classified — "does the work just before this
  boundary reach a natural completion?" — and the cut moves to the
  closest clean one, nearest first, ties preferring the older boundary
  (more context kept). Fail-open by construction: no classifier, a
  classifier error, or nothing classified clean leaves the positional
  cut exactly as it was — the refinement can never block, delay beyond
  its own timeout, or break compaction. A refined clean boundary also
  eliminates the turn-prefix summary: one summarizer call instead of
  two.

- **`permissions.bash_risk_guard` — the decision model as a second net
  for the bash ambiguity zone.** Off by default. When enabled and a
  decision model is configured, bash commands that match neither the
  allow nor the deny list and that containment does not answer are
  classified before execution (one round trip, two questions:
  destructive? cheaply reversible?). A ≥0.70-confidence
  destructive-and-irreversible verdict tightens the decision — Ask with
  the verdict annotated in interactive mode, Deny in autonomous modes
  (the sub-agent hole: ChildPolicy's BashDefault=Allow used to run
  novel destructive commands — `git push --force`, `find / -delete` —
  silently). The contract is fail-closed toward today's behavior:
  nothing maps to Allow, every miss (unset, unconfigured, errored,
  timed out, low confidence, half-answered) is exactly the decision
  today's policy would have made, yolo skips the classifier entirely,
  and forked child gates inherit the guard. Setting the flag without a
  decision model debuglogs that the guard is inactive; a classifier
  miss debuglogs the miss. Mid-session deconfiguration is pinned by
  tests at both new jev call sites (here and compaction): the asker
  re-resolves its provider per call, so removing the `jev:` section
  surfaces `ErrNotConfigured` on the next classification — a miss
  folded into the fallback, never a stale verdict from the dead
  provider.

- **Persona settings are the defaults everywhere.** The active persona's
  Model, PermissionMode, MemoryModel, and memory toggle now define what a
  new session actually runs and what every selector shows — the "falls back
  to the startup model" gap is closed at every layer:
  - The DEFAULT persona's settings are real settings. `applyPersonaSettings`
    dropped its isDefault skip (the editor writes Model/PermissionMode for
    every persona; they were silently dead on the default one), fresh web
    engines now adopt the active persona even when it is the default
    (`liveFor` no longer skips SetPersona for it), and the memory-ops client
    routes the default persona's MemoryModel too — smart session names and
    consolidation finally run on the configured second model instead of the
    main one.
  - The memory-ops model resolves through the model catalog before the
    legacy name-swap, so a MemoryModel on another provider (a local 8B
    against a hosted main model) gets that provider's base URL, API key, and
    request hooks. Persona model swaps carry the override model's hooks the
    same way, and reverting restores the construction-time pair.
  - The web GUI shows — and writes — what the persona owns. The footer chip,
    the settings panel, and the mode switcher resolve the EFFECTIVE values
    (manual knob → persona override → project default) exactly like the
    engines do; the model picker and the mode switcher persist to the
    persona when it carries the corresponding override (the memory toggle's
    ownership rule, applied consistently); and a persona switch clears the
    stale per-run model knob so the new persona owns the defaults from the
    first render. The persona editor's memory toggle is tri-state
    (Inherit / On / Off, via `clearMemoryEnabled`) — the old binary form
    silently pinned "off" on every save.

- **`browser.headless` in config.yaml** forces the interactive browser's
  window mode past the display probe: `false` gives `/browser open` a
  visible window from DISPLAY-less TUI environments (tmux over SSH), `true`
  keeps servers/CI invisible. `A1_BROWSER_HEADLESS` still wins per
  invocation.

- **JEV is configurable from the config web server — TypeSafe API or a
  local Ollama model.** A new `jev:` section in config.yaml (and a real
  editor panel replacing the stale "JEV placeholder", which actually
  edited `judge:` — that panel is now titled Judge and points at the
  code tool's judge mode) selects the decision-model backend used by the
  two wired call sites (fetch classify, evidence verdict):
  - **`provider: api`** — the hosted TypeSafe System One endpoint
    (`api_key` / `base_url` / `model`, each falling back to the
    TYPESAFE_* environment variables; `model` defaults to `jev-latest`).
  - **`provider: ollama`** — a local Ollama chat model interrogated with
    the same noul/choice/score questions under a strict JSON contract:
    `format: "json"` constrained decoding, temperature 0, thinking off,
    num_predict capped at 512, and the same validation spine as the
    TypeSafe wire (every asked question answered, the choice drawn from
    the question's alternatives, the score inside the rubric range and
    whole, confidence required and in 0..1). Any miss is an error and the
    call site falls back to its deterministic path — a bad local model
    can never do worse than no JEV. Base URL falls back to OLLAMA_HOST
    and then `http://127.0.0.1:11434`; the model tag is required
    (pulling a local model is never a default).
  - **Resolution is live.** `project.loadConfig` pushes the section to
    `jev.Configure` on every load, and the asker the call sites hold
    resolves its backend per decision — so the web GUI's save → reload →
    next-turn flow switches providers without a restart (the fetch
    classifier rebuilds per search anyway). No `jev:` section keeps the
    env-only TYPESAFE_API_KEY behavior byte-for-byte; nothing configured
    means JEV stays off everywhere.
  - **Provider-aware timeouts.** The hosted API keeps the tuned 3s/4s
    call-site ceilings; the Ollama provider raises them to 20s (a local
    model can take seconds); `jev.timeout_sec` overrides both directions
    so fast hardware can lower the ceiling too.
  - **Editor panel.** Provider select (Off / TypeSafe API / Ollama) with
    conditional fields, a password-type API key field, positive-integer
    timeout validation, and en + zh i18n. Save-time validation rejects a
    typo'd provider with a clear message; the runtime resolver stays
    lenient (unknown provider = off, debuglog note) so a hand-edited
    config.yaml never bricks the harness.
  - New surface in `internal/llm/jev`: `Asker` (the backend-agnostic
    seam both call sites' duplicated `jevAsker` interfaces now alias),
    `Settings` + `Configure`/`CurrentSettings`, `Configured`,
    `NewConfiguredAsker`, `TimeoutHint`/`EffectiveTimeout`,
    `NewOllamaClient`/`OllamaConfig`, `ErrNotConfigured`.

- **JEV wired into exactly two places — fetch classify and evidence
  admission — with fallbacks everywhere.** The TypeSafe System One client
  (`internal/llm/jev`) existed complete and tested but nothing imported
  it; `cmd/jev` is still an empty stub by user decision (only these two
  homes). Both wirings share one discipline: **nil without
  TYPESAFE_API_KEY** (byte-for-byte pre-JEV behavior), hard per-call
  timeouts (3s classify / 4s verdict), confidence floors, and
  debuglog-traced decisions.
  - **`fetch` Phase 0 engine routing.** When configured, the decision
    model picks the search engine with one Choice question (11 criteria:
    the ten domain engines plus the merged web engine; BetterWright is
    deliberately not a choice — it is an emergency path). The pick gets
    first dibs but no veto: an empty result or engine error falls through
    to the untouched predicate order, and every failure mode of the call
    itself (no key, timeout, unknown choice) means the predicates run
    exactly as they always did. Observable via debuglog ("jev routed X
    to Y").
  - **Evidence admission for the unclassified tail.** The deterministic
    classifiers cover the major runners; `make check`, `just verify`,
    `./ci.sh` and friends recorded nothing — invisible to the verdict
    gate, forever NO EVIDENCE in contracts. When a completed bash command
    produces no deterministic records, the advisor asks one Score
    question (0 = not a check, 1 = failed, 2 = unclear, 3 = likely
    passed, 4 = clearly passed); a confident (≥ 0.6) 1 fails and 3/4
    passes, admitted as a `jev:<command-slug>` ledger record that
    ratchets like any other (a JEV pass protects; a later fail
    regresses). 0/2/any-doubt records nothing. A cheap pre-filter keeps
    `cat`/`ls`/`git status` from paying the round-trip, the advisor is
    never consulted when a deterministic classifier already spoke, and
    fail details carry the first error line plus `[jev verdict,
    confidence N.NN]` provenance. `jev:` ids are ValidCheckID-legal, so
    contracts can declare them as done-criteria.

### Fixed

- **The compaction scrollback contract is pinned at the session layer.**
  `Manager.FullBranch` returns the full leaf-to-root branch (header
  excluded, oldest first) with no compaction cut applied — the UI
  scrollback view — while `BuildContext` remains the only view that stops
  at the newest compaction's retained tail, so the model context is cut
  but the transcript never is. `Session.AllEntries` (the web transcript's
  replay source) now delegates to it instead of duplicating the walk, so
  the pinned primitive and the production path are one implementation.
  The tool-result source cards ride the entry as `ToolSources`
  (`"tool_sources"` on the JSONL line — renamed from the mismatched
  `"sources"` the acceptance suite pins as the wire contract) and
  `ReplaySnapshot` restores them onto the replayed `ToolRun` together
  with the name carried from the tool_use block.

- **Concurrent job-meta writes could splice into unreadable JSON.**
  `store.writeMeta` wrote every version to the same fixed `meta.json.tmp`
  path; two truncate+write cycles sharing that path interleave into
  valid-JSON-prefix-plus-stale-tail bytes, and `readMeta` fails with
  "invalid character ... after top-level value" — a corrupted job. Each
  write now gets a unique `CreateTemp` name (O_EXCL, same directory, so
  the rename stays atomic): a reader sees one whole version or another,
  never a splice. The requeue test that exposed it also models process
  death properly now — it releases only the NEW manager's runner, so the
  abandoned manager's goroutine stays parked like a real dead process's
  would instead of waking to race the completion persist.

- **Gate and browser tests are deterministic on every host.** Two
  environment leaks made the suite machine-dependent: on a Wayland
  desktop the browser tests' "no display" simulation cleared only
  `DISPLAY`, so the leaked `WAYLAND_DISPLAY` kept the probe headed and
  the expected headless downgrade landed (and was reported) as a visible
  window — both browser tests now pin both display vars; and on a
  Landlock host the real sandbox probe folded every unpinned Ask
  expectation into a sandboxed Allow ("containment substitutes for
  approval"), flipping the webserver's mode-folding and policy-fallback
  contracts to green-light `pip install` — the webserver package now
  pins the probe via `permission.PinSandboxSupportedForTest` in a
  `TestMain`, mirroring the permission package's own pin, with the
  containment path itself still tested where it belongs.

- **Sandboxed bash died on machines missing a single cache directory —
  the "commands aren't executing" regression.** `sandbox.Compile` listed
  RW anchors without checking they exist, and a Landlock path-beneath
  rule anchors on an `O_PATH` open of its directory: one absent candidate
  (`~/.cargo` on a Rust-free box, `~/.a1/browser-profiles` before the
  first browser spawn, `~/.bun/install/cache` without bun) failed the open
  and killed the whole start — every sandboxed bash call errored with
  ENOENT while the permission gate had already auto-allowed it (the host
  supports Landlock), so commands simply never executed. Compile now
  drops candidates that aren't existing directories (seatbelt renders
  only real subpaths too), and `applyLandlock` skips an RW anchor that
  vanishes between Compile and Start — ENOENT/ENOTDIR are skippable
  because a missing directory grants nothing; EACCES/EPERM/EBADF stay
  fatal so a misbehaving sandbox fails loud instead of silently
  contained. Rule failures now name their anchor ("open rule anchor
  /home/x/.cargo: no such file or directory") instead of a bare errno,
  and bashtool no longer double-prefixes them ("sandbox: sandbox: …").

- **Smart titles ran on the main model.** `memoryConsolidationClient`
  consulted `session.activePersona()`, which reports false for the default
  persona — a MemoryModel configured there never routed. It now reads the
  persona store directly and resolves the model through the catalog (see
  Added above).

- **The TUI browser tool never launched a live browser.** Four stacked
  causes: the fetch tool's BetterWright search fallback cold-spawned the
  shared default-profile daemon HEADLESS, and a warm daemon ignores
  `--headed` — every later `/browser open` landed in an invisible window
  (the fallback now rides a dedicated `a1-bg` lane and profile with its own
  daemon, so background search can neither poison the interactive browser
  nor navigate its page); `browserHeadless` had no config override (added,
  see Added above); kamitool's PDF render still spawns the shared profile
  headless, but the spawn is now tracked and `ensureBrowser` recovers a
  visible window by stopping every daemon and respawning cold-headed; and
  the `/browser` command discarded both its result and its error
  (DispatchSlash swallows them), so success and failure were both invisible
  — it now toasts the outcome, and `open` says which window mode landed and
  how to force the other one.

- **DELETE /api/session left underscore-id sessions on disk.**
  `deleteSessionFiles` matched the id after the LAST underscore while the
  canonical filename parser uses the FIRST — a "successful" delete left the
  file and the sidebar row alive for any session id containing an
  underscore.

- **The webserver test suite deadlocked.** One test's hanging LLM handler
  parked with the request body unread; net/http only arms the
  disconnect-detection that cancels `r.Context()` after the body is
  consumed, so the test server's Close blocked forever and the entire
  package timed out (the long-standing "webserver e2e too heavy to run"
  caveat). The handler now drains the body before parking, the test cancels
  its in-flight turn via /api/cancel, and teardown force-drops connections.
  The full suite runs unfiltered in seconds.

- **Auto-compaction robustness on small windows (the 64K "sometimes
  works or doesn't" pass).** Five structural weaknesses, each capable of
  intermittent compaction failure:
  - **Kept-zone ceiling (INV5).** A giant message (big paste, huge tool
    result) landing inside the kept window survived every compaction —
    the post-compaction context still overflowed, overflow recovery
    found nothing new to shrink, and the turn died on streak exhaustion.
    `SettingsFor` now derives `maxKeptTokens = window − 1.8×T` (clamped
    ≥ keepRecent; INV5: ceiling + summary cap ≤ the safety-valve
    threshold) and the cut moves to NEWER boundaries until the kept zone
    fits, dropping the giant into the summarized zone where the
    serialization truncates and the summary compresses. A huge entry
    after every cut point (the very tail) is documented as
    cut-unreachable — the recovery window halving takes over.
  - **Summarizer input cap.** The summary call sent the full serialized
    conversation, so under overflow recovery the compaction call itself
    could overflow the same window and the turn died with the original
    error anyway — the classic "marginal fit" flake. CompactionPreparation
    now carries ContextWindow and `SerializeConversationCapped` bounds
    the input to window − output-cap − 1/8 overhead: below the cap
    byte-identical to before; over it the OLDEST entries drop behind a
    marked omission (the newest stays — it abuts the kept zone the
    summary must remain continuous with).
  - **Safety valve no longer blind when the provider reports no usage.**
    With `msg.Usage.ContextTokens() == 0` the hard-overflow guard never
    fired and only the bytes/4 growth gate remained — which underestimates
    code and CJK, so a 64K session could walk straight into a hard
    overflow with compaction never having fired. The estimate now stands
    in for the safety check when usage is zero (documented ruler caveat:
    it can fire late, but late beats never).
  - **Ollama-style overflow errors recognized.** `IsContextOverflow` and
    `ParseContextLimit` now match ollama's "input length (N) exceeds
    context length (M)" (and the context-size/limit variants), so
    overflow recovery actually triggers and learns the real window
    instead of the turn dying cold.
  - **Compaction serialized.** The loop's post-turn auto path and a
    surface's ForceCompact (web /compact, TUI :compact) run on different
    goroutines; two preparations against the same leaf could append two
    compaction entries. `runCompact` now holds `compactMu` for its
    duration.

- **Tooling pass: five new search engines and a merged web fallback in
  `fetch`.** General queries no longer see DuckDuckGo Lite alone: the
  router gained keyless JSON domain engines — MDN (web-platform docs),
  npm and crates.io (package registries), Hacker News and Reddit
  (community threads) — and the general fallback became a merged engine
  that runs DDG Lite + Hacker News + Reddit in parallel and interleaves
  results 2:1:1, so one search surfaces web links AND practitioner
  discussions (the sources sidebar gains hacker-news/reddit hosts
  without losing web coverage). Ordering is deliberate: MDN before the
  broad programming predicates, registries before GitHub, and
  Reddit/HN before Stack Exchange (its predicate matches any query that
  names a language; "what do people think of bun" must reach the
  forums). The merged engine propagates its primary's error — a
  captcha-walled DDG still hands off to the BetterWright browser
  fallback — and fetches page bodies once, on the merged survivors
  only. Reddit refuses datacenter IPs; the error is swallowed and the
  merge proceeds (works on residential). `classify` tests extended;
  general queries now classify as `web`.

- **`search` tool overhaul: glob, literal, context, listing mode, and
  honest ripgrep errors.** The grep+ls fusion tool gained the knobs
  that were missing: `pattern` as an alias for `query` (grep/find
  habit), `glob` filtering every plane (rg's full glob syntax for the
  text plane), `literal:true` for plain-text queries, `context=N`
  rendering surrounding lines with a `·` marker while matches keep
  `|`, and an empty query + explicit path now LISTS that directory
  (dirs first, size + relative mtime per entry, truncation notice) —
  search is the one-stop discovery call. ripgrep failures surface
  properly: an invalid regex used to silently render as "no results";
  the stderr now comes back with a "pass literal:true" hint. Hits
  without text matches show size + mtime, ranking adds a
  basename-match boost plus a shallower-path tiebreak, the header
  reports "N of T files" when trimmed, and the zero-hit response
  suggests concrete next steps.

- **`make betterwright-install`.** A standalone target that installs
  the BetterWright CLI at the pinned version (`BETTERWRIGHT_VERSION ?=
  2.9.0`, in sync with internal/toolmanager's `BunTools` pin) and runs
  the explicit `betterwright setup` browser download (~150 MiB — kept
  out of `make install` on purpose). Bun-guarded with a clear error
  when bun is missing, and hints to open a new shell when the freshly
  installed binary isn't on PATH yet. `internal/bw`'s not-installed
  error now points at the target. Also repaired the checked-in
  Makefile's space-indented recipes (GNU make "missing separator") —
  the whole file is tab-indented again, so every target parses.

- **Web transcript density: tool-call clusters and a capped source row.**
  Second live-jank pass; both user-reported with screenshots:
  - **Same-tool runs collapse into cluster rows.** A research turn that
    hammered `fetch` fifteen times across five say-nothing rounds used
    to render a wall of full-width bordered rows. Consecutive PURE-TOOL
    assistant rounds (no text, no reasoning — the heads-down fetch/grep
    phase) now merge into one visual group, and consecutive same-name
    runs — within that group or inside a text bubble's tool list —
    collapse into a single `fetch ×15` header: status icon (spinner
    while any run is live, error wins over done), a seal-tinted ×count
    badge, the newest call's detail as a live ticker while the run
    streams, and a chevron that expands a numbered, sunken list of the
    individual calls. The live status line ("searching arxiv…") renders
    under a still-streaming merged group, and a turn that ends on a
    tool-only round (budget trip, verdict stop) keeps its end-of-turn
    source row and regenerate affordance.
  - **Source pills cap at ten behind a "+N" circle that opens a sources
    sidebar.** A 103-source turn rendered 103 identical chips — a wall
    of `arxiv.org` with cyclically repeating rank badges. The
    end-of-turn row now shows the first ten pills and folds the rest
    behind a dashed `+N` badge that opens a right-hand sidebar
    (Qwen-style): every source as a card with rank badge, favicon,
    host, the search-result title, and the DDG snippet as the
    description — the whole card is the external link — scrollable
    under a sticky header. Escape, backdrop click, and the header X
    close it; the panel resets on session switch; reduced-motion
    collapses the entrance cascades. Pure-render change
    (`web/src/components/ChatArea.tsx` + `styles.css`), no protocol or
    server changes. Verified against a synthetic heavy turn (15+1
    fetches across six rounds, 96 sources) in light + dark themes and
    a 400px viewport.

- **Web GUI polish wave: branches, pills, titles, and the config revamp.**
  The browser chat surface gains the conversation-shaping affordances:
  - **Regenerate** (`POST /api/regenerate`) re-rolls the last assistant
    turn: `Manager.SetLeaf` moves the session cursor to the target user
    message's parent — an append-only cursor move, the abandoned response
    chain survives as an off-path branch — and the normal Loop append
    replays the prompt as a fresh sibling. 409s while a turn streams.
  - **Edit user message → branch** (`POST /api/edit`): any past user
    message can be rewritten; the turn re-runs from the edited text as if
    it had been said then. The client identifies the target by its index
    among user rows (stable across live + replayed transcripts).
  - **Delete session** (`DELETE /api/session`, guarded): removes the vault
    rows (FK cascade) and the jsonl file including pre-migration shadows,
    unpins, evicts the live engine, 404s unknown ids, and refuses (409)
    sessions with a turn in flight. The sidebar asks for an inline
    two-step confirm.
  - **Source pills, end-to-end and durable.** `SessionMessageEntry` gained
    a persisted `sources` field: the engine stashes each search-run's
    ranked Sources by tool_use id and appends tool results through the new
    `AppendToolResult`, so a reloaded session re-renders its pills
    (`ReplaySnapshot` restores them). The browser aggregates all of a
    turn's searches into ONE pill row at the end of the turn, latest
    search first, deduped by URL, rank-badged favicon chips with a
    staggered entrance.
  - **Smart session names.** After a session's first completed exchange
    the server asks the memory-ops model (`Engine.OpsClient` — the
    persona's dedicated MemoryModel, falling back to the main one) for a
    2–6 word title, stored in `titles.json` beside the session data (both
    backings, no vault migration) and overlaid on the sidebar/topbar.
    Opt-out: `web.auto_name_sessions: false` (new `web:` config section,
    default on); one attempt per session per server run.
  - **MCP server registry over HTTP** (`GET/PUT/DELETE /api/mcp`): the
    same `~/.a1/mcp.json` (+ project override) the `a1 mcp` CLI manages,
    for the config editor. Writes touch the user file only; project-
    defined servers are listed read-only with a pointer at the repo file.
  - **Config editor revamp** (`/settings`): "Judge" renamed **JEV** with a
    plain note that it currently does nothing; the embedding model moved
    to its own one-place **Embeddings** panel (semantic search keeps only
    the toggle — YAML keys unchanged); a **Web GUI** panel; a full
    **Personas** section (list/create/edit/delete/activate, memory-ops
    model picker — replacing the TUI's create-default-persona stub, which
    is removed); graceful degradation under standalone `a1 config` where
    the chat-only APIs are absent.
  - **Markdown/LaTeX upgrades in chat**: KaTeX for `$…$` / `$$…$$` with
    raw-text fallback, code blocks with a language header + copy button
    (delegated click wiring), themed task-list checkboxes, external links
    open in new tabs with noopener, copy buttons on every message bubble,
    and reduced-motion collapses for all new animation.
- **`Manager.SetLeaf` / `AppendToolResult`** (internal/session): the
  branch primitive (cursor move, append-only — nothing is deleted) and
  the tool-result append that carries UI metadata (Sources) the
  model-facing `llm.Message` has no field for. `Session.AllEntries`
  exposes the full leaf-to-root branch for transcript rendering.

### Fixed

- **Tool-call clusters: whitespace rounds join the merge, every call gets
  the dropdown.** User's live screenshot of the fetch barrage ("great
  direction, just off execution"): each round rendered its own `fetch ×N`
  dropdown with its own usage line and an orphaned copy button under an
  empty bubble, because several models emit whitespace-only text
  alongside tool_use blocks and the pure-tool merge tested raw
  truthiness — " \n" counted as content. "Said nothing" is now judged
  after trimming text and reasoning, so the barrage merges into ONE
  dropdown with the newest call's detail as the preview and ONE summed
  usage readout above it (tok/s is per-round and dropped). Singles
  render as the dropdown too — no ×1 badge — and mixed-name groups
  summarize per-name (`fetch ×2 · read ×2`) with the name kept on every
  expanded row; expanded details wrap instead of truncating. Also fixed
  the merged group's message-index bookkeeping
  (`last = start + toolCount - 1` broke the moment a round carried two
  parallel calls, mis-keying the end-of-turn sources and regenerate
  lookups). Pure-render (`web/src/components/ChatArea.tsx` + a
  styles.css comment); browser-verified on a synthetic transcript
  mirroring the live screenshot, light + dark.

- **Web settings panel: the memory toggle renders its real state.** First
  find of the live jank hunt. `GET /api/settings` returned the RAW global
  memory knob in its state block, but the toggle writes the active persona
  when it carries an explicit `memory_enabled` (any persona-editor save
  pins one), and the footer badge + SSE state report the persona-folded
  EFFECTIVE value. So a flip on a persona-owned setting wrote the persona,
  turned the badge on, and the panel's re-fetch read the still-off global
  knob — the toggle rendered OFF while memory ran ON. The catalog's state
  block now carries `effectiveMemoryEnabled` (the same resolver
  `stateView` uses), so the panel, the footer, and the engine can never
  disagree about memory.

- **Persona-settings passes no longer revert live engines to their
  boot-time model and mode.** Found in the same screenshot: the footer said
  `llama.cpp` while the settings panel had `dots-3-note-preview` picked.
  Flipping the web memory toggle routes through `applyPersonaToEngines →
  SetPersona → applyPersonaSettings`, whose "persona without an override
  restores the base" branch compares against `baseModelCfg` /
  `basePermissionMode` captured at construction — and surface-driven model
  or mode changes never moved those bases, so the pass silently reverted
  the engine to whatever it booted with. `SetModelWithHooks` now re-bases
  `baseModelCfg` (every caller is a surface intent; the persona path sets
  `modelCfg` directly), and `handleSetMode` (direct pick and cycle)
  re-bases every live engine's `basePermissionMode` via the new
  `updateEngineBaseModes`. Override-free personas now restore the user's
  last surface choice, not the engine's birthday.

- **Toggle knobs are visible in dark mode.** The switch knobs sat at
  `paper-card` (a dark brown) on the near-black `paper-sunk`/`hairline`
  OFF tracks — the OFF state read as an empty pill. Knobs now flip to
  `ink-light` in dark mode on the settings toggles and both persona-editor
  switches; the ON state keeps the on-seal convention (dark knob on the
  light-mode mid-tone seal, dark on the dark-mode light seal). The Tailwind
  config gains `darkMode: ['selector', '[data-theme="dark"]']` keyed off
  the same attribute `theme.ts` already drives.

- **Sandbox enforcement restored: Landlock arg order, gate-fold contracts,
  deterministic tests.** Four fixes, one theme — the sandbox must earn its
  authority instead of assuming it:
  - `landlock_add_rule` was invoked with `LANDLOCK_RULE_PATH_BENEATH` in
    the ruleset-fd slot (a 3-arg `Syscall` against a 4-arg syscall): the
    kernel read fd 1, every rule add failed EBADFD, and sandboxed starts
    died with "file descriptor in bad state". Now a `Syscall6` call with
    the correct (fd, type, attr, flags) order — sandboxed bash actually
    enforces on Linux again.
  - The permission gate's containment fold (sandboxed ⇒ Ask-eligible bash
    becomes Allow) no longer fires for headless-strict sessions or when
    `BashDefault=Deny`. Headless-strict's contract is Ask≡Deny — an
    unattended run must not discover an approval authority in the sandbox
    — and an explicit operator default beats containment. Readonly was
    already excluded for the same reason.
  - The permission package's tests default the host-probe seam to
    "unsupported" (`TestMain`), so gate expectations stay deterministic on
    Landlock-capable dev machines instead of silently flipping to
    sandbox-folded Allows. Containment tests opt in via
    `withSandboxSupport` as before.
  - `TestStartSandboxedContainsWritesOutsideRW` builds a hand-made narrow
    profile instead of `Compile(ws, …)`: Compile grants RW on
    `os.TempDir()`, and the test's "outside" probe (a sibling
    `t.TempDir()`) lived inside the RW set — the write succeeded and the
    containment assertion was vacuous. The narrow profile puts the probe
    genuinely outside.

- **`make build` self-heals stale web deps.** `web-install` no longer
  skips when `web/node_modules` exists — it always runs the
  package-manager install (a no-op sync against a satisfied lockfile,
  well under a second with bun). The skip silently broke `web-build`
  with "Rollup failed to resolve import" whenever the tree was refreshed
  from elsewhere and package.json had gained a dependency (katex being
  the first case) that the local node_modules predated.

- **Vault-backed sessions show text in the sidebar, not hex hashes.**
  `session.ListSessions` filled `Preview` only on the file-scan path; the
  vault branch (Bet 08) built rows without one — and `vault.SessionPath`
  is a synthetic identity path, so there was no file to scan either. The
  label now comes from the stored entry lines themselves (`vaultPreview`:
  newest-first scan of the vault's raw JSONL rows with an early exit, so
  cost tracks the conversation tail, not its length). File-backed
  sessions keep the existing scan.

- **Smart session naming survives its own failures.** The one-attempt
  marker was persisted to `titles.json`, so a single failed call (nil
  ops client, one provider 429) permanently suppressed naming for that
  session — across restarts — for a cosmetic label. Worse, the marker
  was written but never read, so failures actually retried every turn.
  The marker is now in-memory per server run and actually consulted (old
  files' `attempted` keys are ignored and self-clean on the next write);
  a missing `MemoryModel` is treated as a config gap, not an attempt,
  and no longer burns the session's one try.

- **Web transcript survives compaction.** `/api/transcript/messages` now
  renders the full branch (`AllEntries`) instead of the compaction-cut
  context path: messages above a compaction marker stay scrollable in the
  browser; only the model's context stops at the cut.
- **Editor saves can no longer wipe `checks:` / `schedules:`.** ConfigDoc
  declares both sections and the save handler carries the file's existing
  values over when the payload omits them — the same protection
  `tools.max_tool_calls` already had. An explicit empty array clears them.
- **Settings overlay mode buttons now refresh.** Switching the permission
  mode in the in-app Settings panel reloads the catalog like every other
  section, instead of leaving the previous mode highlighted until reopen.
- **`web.auto_name_sessions` config plumbing** (project/config): new
  `web:` section parsed and round-tripped through the config editor.

- **Bet 01: the Clock — durable daemon + scheduler.** `a1 serve` runs the
  harness as a headless daemon: the full web API behind bearer-token auth
  (`Authorization: Bearer` or `?token=` for EventSource; constant-time
  compare; non-loopback binds refuse to start without a token),
  headless-strict by default so unattended prompts deny instead of hanging
  (`--no-strict` opts back into a remote-GUI posture). The durable job
  state machine replaces fail-on-restart: leftover non-terminal jobs park
  as `pending` (attempt bumped) instead of being marked failed, `Resume`
  re-runs them through the runner with the job's own prior session —
  rebuilt from the session store, evidence receipts replaying through the
  same ledger scope — and the daemon auto-resumes every pending job at
  boot, so overnight work survives the process. Job metas now mirror into
  a vault `jobs` table (lazy DDL, the memstore-namespace pattern) for
  cross-process queries; the one-shot vault migration imports pre-vault
  job dirs. The `schedule` tool (one facade with a create/list/cancel
  action enum — the always-on tool count stays put) plus a `schedules:`
  config grammar (cron / fixed_rate / one_time, exactly one trigger per
  entry, malformed triggers fail loud) let the model and the user declare
  durable wake-ups; fires spawn jobs through the fleet's manager —
  backpressure and timeouts reused, prompts entering through the same
  `EmitUserInput` path as any user turn — and scheduled jobs default to
  role=worker (headless-strict) so an unattended wake-up never stalls on
  an approval. Schedules persist as vault rows; config-declared ones are
  synced at boot and win id conflicts. Cron is a hand-rolled 5-field
  parser (names, ranges, steps, lists; vixie dom/dow OR rule) — no new
  dependency.

- **Bet 07: the Mirror — eval harness + cost telemetry.** `a1 eval` joins
  the two files a1 already writes: stored session trees and evidence
  receipts become per-session metrics (rounds-to-green, rollback rate,
  stall rate, token spend by model, compaction count) with a `--json`
  machine mode, and `a1 eval replay` re-runs a stored session's original
  prompt under mutated configs (tool subsets, round caps) then scores the
  new session for before/after. `a1 eval golden` runs the pinned ~20-task
  golden set (go/rust/python fixtures) against the configured model and
  records results in a vault `eval_runs` table — no model configured →
  clear notice, exit 0, so CI without secrets stays green; a nightly
  `golden.yml` workflow and `make eval-golden` drive it. Telemetry
  persists to SQLite: per-tool latency and error rows flow live from the
  executor (a buffered single-writer sink), turn token totals materialize
  from the durable session entries, and a `/stats` dashboard page plus
  `GET /api/telemetry` expose both. Skill routing became a bandit:
  reading a skill records a pick, eval materialization records outcomes,
  and the `list_skills` catalog reorders by Laplace-smoothed win rate —
  skills that keep correlating with failed turns sink to the bottom,
  annotated, instead of being silently invisible.

- **Incremental indexes (deferred from Bet 06).** The code graph no longer
  rebuilds per query: `OpenGraphCached` keys parsed imports by
  (mtime, size) in a vault `graph_files` table, parses only new/changed
  files, deletes vanished ones, and the previously-dead `rescan` input on
  `search`/`graph`/`batch`/`testimpact` now force-rebuilds. The semantic
  index leaves the global `~/.a1/semantic_index.json` (rewritten whole on
  every upsert, shared across projects, stale by default) for a
  project-scoped SQLite store: incremental per-file re-embedding keyed by
  mtime+size+model, and hybrid retrieval — dense cosine over normalized
  BLOBs fused with an FTS5 keyword leg via reciprocal-rank fusion — all
  inside the existing database, no new dependencies.

- **Bet 04: the Fleet — orchestration with a shared finish line.**
  Sub-agent spawns no longer fail when the concurrency slots are full:
  they join a bounded FIFO wait queue (`StatusQueued`) and start as
  slots drain — ownership-transfer retirement keeps FIFO order
  race-free, and only a full queue returns `ErrBusy` (queue overflow
  also cleans up its orphaned job dir). Worker children above depth 1
  can nest (default ceiling 3 generations, tighter nested concurrency,
  `RecoverIgnore` so only the top manager owns restart recovery) while
  explore/review keep the no-nesting default; every generation gets
  cheaper confinement step budgets (20/14/10 for workers, clamped).
  Task contracts: every `contract` declaration also enters a
  session-scoped task contract that survives per-turn resets and is
  forwarded to spawns — worker children are held to the parent's
  finish line by the zero-rank stop, explore/review only arm the
  watches. Structured handoff: children write `handoff.json` (files
  read/modified, current evidence check states) beside `result.md`,
  and `agent_wait` merges it into the parent's context as data.
  Warm worker pool: `agent_spawn warm_from_job` resumes a finished
  worker's session so it skips repo re-reading (file copy, or direct
  vault resume post-Bet-08; warmth is never a hard requirement).

- **Bet 08: the Vault — one durable store.** `~/.a1/a1.db` (package
  `internal/vault`, modernc SQLite, WAL, foreign_keys on) replaces the
  two-and-a-half persistence systems: personas are rows (the JSON
  registry is gone), persona memory banks are rows, per-persona
  semantic stores are namespaced table families (`mem_p_<id>_*`), the
  shared engine bank is `mem_shared_*`, evidence receipts are rows
  keyed by the project scope, and session trees are rows keyed
  `(session, seq, parent)` with byte-faithful payloads — replay is the
  old OpenSession walk minus the file IO, and `bench_test.go` still
  gates context building. Vault sessions report the same
  `<dir>/<ts>_<id>.jsonl` identity path, so evidence scope, resume,
  listings, and screenshot baselines are unchanged; resolution is
  vault-first with the legacy files as degraded fallback (a vault that
  fails to boot logs and the process keeps running on the file
  layout). One-shot migration imports personas.json, bank JSONLs
  (both locations), per-persona semantic SQLite files, the shared
  memories.db, per-project evidence.jsonl, and every session JSONL on
  first open — idempotent by key, stamped in `vault_meta`. Deletability
  end-to-end: deleting a persona cascades registry row, bank rows, and
  semantic family in one transaction (plus the avatar dir); deleting a
  session cascades its entries; the legacy-bank adoption shim is
  bypassed in vault mode. Decision written down in
  `doc/project-layout.md`.

- **Bet 06: the Diary — episodic memory and the wired-in loops.**
  `cool_passes` finally increments: a 24h background decay pass cools
  unrehearsed memories, driving the σ·salience·γ^coolPasses term that
  was dead code. Session valence is computed at every turn's end from
  outcomes (-0.2 per protected regression, +0.3 per certified
  contract) and reaches both the inlet's and the outlet's retrieval
  cues — selectBundle's §3.5 mood-congruency override is reachable.
  `memfsck` runs at session start (once per engine, over
  `<cwd>/memory`, silent no-op without it) as its doc always said.
  The episodic store lands as vault rows: every compaction summary —
  which already embeds the evidence record — becomes a project-scoped
  episode, and the memory inlet injects the most recent ones as a
  "last time this happened" block. Per-persona memory-ops model:
  `MemoryModel` on the persona (web editor + `SetMemoryModel` API)
  routes consolidation traffic to a dedicated client while the
  user-facing assistant stays on `Model`. SOUL wiring: a fenced
  ```soul block in a persona's system prompt parses into
  `memstore.Params` at store open (d/tau/sigma/k/gamma/beta/lambda/
  w_kwd/mu/m/vec_top/fts_top/diversity/tref; golden-pinned parse) —
  a nostalgic persona forgets slowly, an eager one recalls more, and
  an absent block keeps every existing persona byte-identical.

- **Bet 03: OS-level sandbox for bash — containment substitutes for
  approval.** `internal/sandbox` compiles the APPA policy into
  enforcement: read+execute everywhere, writes confined to the workspace
  plus well-known caches (go/npm/cargo/bun, temp, selected ~/.a1
  subdirs), sensitive deny prefixes never widened, network egress denied
  by default (`permissions.sandbox_network: allow` opts out). Backends:
  Landlock on Linux (pure syscalls via the existing x/sys dependency —
  applied on a locked OS thread immediately before fork/exec; the child
  inherits the ruleset and the harness stays unrestricted) and
  sandbox-exec seatbelt profiles on macOS. Config: `permissions.sandbox:
  auto|on|off` (default auto — on when the host supports it). When
  enforcing, `checkBash` auto-allows Ask-eligible commands (the deny list
  still fires first; readonly sessions keep asking — their contract is
  writes-nothing), so autopilot stops being scary and "yolo with walls"
  is a real mode. Foreground and background bash both ride the sandbox;
  the user's own `!cmd` stays untouched. Sandboxed runs that hit
  "Permission denied" get a hint explaining the wall.

- **XLSX / PPTX / CSV in `document` (Bet 03 quick win).** The format
  switch grows three outputs over the same markdown content: xlsx (one
  row per heading/paragraph/bullet; pipe tables become grid rows), pptx
  (title slide plus one slide per section), and csv (tables as rows,
  prose as one row per line). gooxml — already the DOCX dependency —
  covers the first two, so no new module.

- **Bet 09: the browser is now a BetterWright daemon, not an embedded
  engine.** `internal/tools/browsertool` keeps its whole tool surface
  (open/navigate/search/click/type/screenshot/content/close/profile/proxy
  plus the atomic trio) and the user-opened gate, but the playwright-go
  engine underneath is gone — `playwright-go` left go.mod, keeping the
  direct-dependency count lean; BetterWright is an optional external
  runtime, not a Go dependency. The daemon keeps tabs and page state alive
  between calls, profiles keep their cookies on disk after close (the
  "logins stop evaporating" fix), bot challenges surface as handoff events
  routed through the permission ask flow, and `internal/bw` is the shared
  thin CLI client (`betterwright run --session a1 -c "<js>"`, JSON
  envelope). PDF rendering (`document` tool) rides the same daemon. a1's
  browser profiles map onto BetterWright profiles (own identity, own
  cookie jar, own daemon); the default profile maps to the shared one.
  Acquisition is first-class in `internal/toolmanager` (BunTools:
  version-pinned `bun install -g betterwright@2.9.0`; `betterwright setup`
  stays an explicit step). In-browser search uses Mojeek — BetterWright
  deliberately blocks automation of the Google/Bing/DuckDuckGo search UIs
  on its managed browser.

- **Search fallback rides on the daemon.** The fetch tool's search router
  now has real fallback ordering: domain engines (Frankfurter, GitHub,
  Stack Exchange, arxiv, Wikipedia) via Matches(), then DDG Lite (cheap
  HTTP), then the BetterWright browser engine (registered only when the
  CLI resolves) — so a captcha-killed scraper falls through to a real
  browser session instead of failing. `WithCatchAllEngine` is the
  documented injection point for custom final fallbacks.

- **Multimodal tool results: the model can see its own browser (Bet 02).**
  `tooldef.Result` gains `Images []llm.Image`; the executor forwards them on
  the tool message when the active model has `image_enabled` (rebound on
  every model switch) and strips them otherwise, so blind models keep the
  path-only text. `browser_read` mode=screenshot now attaches the capture
  itself — downscaled and re-encoded to stay under ~1 MiB / 1600px wide
  (`internal/util/image.FitForModel`, box-filtered halving with a 320px
  floor so the image never becomes an unusable blur). Anthropic tool_result
  blocks carry text+image content arrays natively; other providers ignore
  tool-message images and keep the text. Tools consult
  `tooldef.ImageEnabledFrom(ctx)` to skip image work for blind models.

- **Evidence kind `screenshot:<name>` — screenshot-diff as a machine-checked
  contract id.** Declare it in a contract, then capture with
  `browser_read` mode=screenshot `name:<same>`: the first observation stores
  the capture as a baseline (decodability-checked — garbage never seeds a
  baseline), later captures diff against it (≤1% of pixels may differ by
  more than 16/255 per channel; size changes fail outright). Baselines live
  beside the session's `evidence.jsonl` under `screenshots/`, so receipts
  and artifacts share one lifecycle. The add-only ratchet, regression notes,
  and the turn-end zero-rank gate all apply unchanged.

- **Human handoff → APPA ask bridge.** `tooldef.Result` gains `Handoff`
  (reason + URL); the executor routes it through the permission ask flow —
  the same gate that asks before a risky bash command asks before a human
  takes the wheel. Approval and denial (with feedback) both become
  model-facing notes on the tool result.

- **`git` tool: governed git/gh operations instead of raw bash.** One
  facade, twelve modes (status/diff/log/show/branch/add/restore/commit/
  push/pull/pr/issue) executed as strict argv — never a shell string. APPA
  gating makes "no commit without explicit request" enforceable instead of
  doctrinal: read modes (status/diff/log/show/branch list/pr+issue list/view)
  map to ActionRead (no prompt), add/restore/branch-create map to ActionWrite
  with paths (workspace-checked), and commit/push/pull/pr-create/issue-
  create map to the bash class — Ask by default in interactive mode, Deny
  under readonly, and an unknown mode fails safe to Ask. Commit messages
  never render into the bash-pattern Command field, so prose can't trip the
  deny list. `gh` modes return an install-hint error when the binary is
  missing. Output is capped (64 KiB head-keep — git orders newest-first).

- **Read elision: identical re-reads of unchanged files collapse to a note
  (compaction-aware).** The executor now intercepts `read` results: when the
  exact same read (same resolved path, same mtime, same offset/limit) was
  already served AND the tool message carrying that content still rides in
  the live context, the model receives a short "unchanged" note instead of
  the content — with the escape hatch taught in the note
  (`{"refresh": true}` forces the content; new `refresh` param on `read`).
  The liveness check walks the post-compaction session path, so a folded-
  away read always re-serves full content — the failure mode of the naive
  (path, mtime) cache (a note pointing at content the model no longer has)
  is impossible by construction. Elided reads keep the original message as
  the liveness anchor, so chains of elisions stay honest through compaction.

- **Three orphaned analysis tools surfaced as `code` modes.** `errortool`,
  `stacktool`, and `errpattern` were tested but unreachable from the agent;
  they are now `code` modes `error` (match error text against known patterns
  for a fix), `stack` (parse a pasted trace into file:line frames with
  source context), and `errpattern` (scan Go source for error-handling
  anti-patterns). All three are read-only, so the existing `code` permission
  classification covers them unchanged. The `code` description was rewritten
  around a failure-diagnosis block that differentiates the four overlapping
  diagnosis modes (error/errtrans/stack/runtime) with a steering rule.

- **Polyglot evidence classifier + watched checks + config-defined checks
  (Bet 05).** `classify` now recognizes js-test (npm/pnpm/yarn/bun test,
  vitest/jest direct and npx/bunx forms, TAP detail lines), playwright-test,
  mypy (incl. `python -m` / `uv run`), ruff check, eslint, cargo clippy, and
  dotnet test, with the build/vet attribution doctrine extended to suite
  segments (a failing `go build && npm test` records nothing — ambiguous
  compound failures attribute nothing). Contract-declared watches add three
  check-id families: `exit:<code>:<command>` (any command's real exit code,
  parsed from bashtool's exit-status marker), `http:<status>:<url>`
  (observed from curl status lines incl. `-w '%{http_code}'`), and
  `port:<host>:<port>` (from `nc -z`) — observation-gated so incidental
  bash calls never grow the ledger. `config.yaml` grows a `checks:` list
  (`{name, command, success, pattern}`) whose exact-command runs record
  `check:<name>` evidence; wired at all three engine construction sites,
  bad patterns fail loud at startup.

- **Background bash shells + per-call stdin/env/cwd.** `bash` mode=start
  runs detached (survives the tool call; process-tree cancel on kill), ps
  lists, attach returns the bounded rolling tail with an optional wait
  (the "wait for server ready" move), kill stops the tree; exited shells
  stay listed until pruned at 20. Background modes never reach the evidence
  ledger — a still-running suite must not read as passing. Foreground runs
  gain `stdin` (pipe text — gpg/ssh/psql/REPL workflows without heredocs),
  `env` (merged over the inherited environment), and `cwd` (relative to the
  session cwd).

- **Budget contracting: budgets the model declares become budgets the
  harness holds (evidence loop Phase 5).** New `budget` tool: the model
  declares "N more tool rounds" and the harness holds the ceiling in engine
  state the model cannot edit — Gate 1's STOPPING RULE doctrine is now the
  human-readable projection of a machine-held budget, the same authority
  split as the `contract` tool. At the ceiling with work still wanted, the
  next round runs on the turn's one re-open and a `[budget]` note lands
  with the spend state; declaration-only rounds (budget/contract) never
  fire, so extending is always available and always visible in the
  transcript; a further work round without a fresh declaration stops the
  turn with a spend report — pending calls rejected (never executed past
  the declared budget), the contract residual surfaced so an out-of-budget
  stop is never mistaken for a certified one. Turns that never declare are
  untouched (chat-safe, pinned by test); budgets reset per prompt.
  `WithBudgetContracts(false)` / `SetBudgetContracts` keeps declarations
  recorded but stops enforcement (default on).

- **Evidence-driven recurrence: verified state survives compaction (evidence
  loop Phase 4).** Compaction summaries now end with a harness-written
  `<evidence-record>` block: protected checks, current regressions, never-
  passed failures, and the turn contract's standing with its residual set.
  After a fold the model's context carries what is *verified* rather than a
  transcript-shaped memory of it — the record is rendered from the evidence
  ledger by the harness (never by the summarizer, whose copy could drift),
  re-appended fresh at every compaction, and stripped from the previous
  summary before the update prompt sees it. Both summary templates now
  instruct the model never to restate a test/build status section. Sessions
  with no observed checks and no contract (plain chat) render no record at
  all — their summaries are byte-identical to before.

- **Receipt pruning: evidence.jsonl stays bounded.** The receipt file now
  rewrites itself to the minimal replay-equivalent record set (last pass +
  last record per check) every 512 appends, and `Load` self-heals an
  overgrown file on session resume. The ratchet is preserved exactly: a
  check that passed and later failed still replays as protected-and-failing
  — its pass receipt is what keeps it protected — and harness rollback
  receipts (`harness:candidate-rollback`) survive pruning verbatim.

- **Zero-rank stop: "done" is certified, not claimed (evidence loop Phase
  3).** New `contract` tool lets the model declare its done-criteria as
  machine-checked check ids (`go-build`, `go-vet`, `go-test:<pkg>`,
  `go-test:<prefix>/...`, `pytest:<nodeid>`, ...). The harness holds the
  contract — declarations accumulate for the turn and can never be removed —
  and when the model stops calling tools it judges every declared check
  against the evidence ledger: failing or never-run checks inject the
  residual set and re-open the turn (bounded extra verification rounds,
  default 2, `WithZeroRankBudget` to tune or disable); a fully green
  contract ends the turn with a "certified complete" note; an exhausted
  budget stops with the residual set surfaced as NOT certified. The 5-Gate
  prompt's DONE_CRITERIA gate is now the human-readable projection of this
  machine contract, and Gate 4 gained the budgeted-verification doctrine
  (targeted `testimpact` runs per change, full suite at commit points).

- **Verdict gate: the loop now disposes of candidates (evidence loop Phase
  2).** Every mutating tool batch that runs classified checks is a
  candidate: the harness diffs the protected evidence ledger before and
  after and, in autopilot/headless-strict, rolls a regressing candidate
  back to the last verified baseline (a cheap git ref snapshot taken before
  the batch; baselines advance on clean candidates, so rollback restores
  the latest verified state and never throws away verified progress). The
  ledger re-admits the regressed checks at their verified status, so
  re-applying the same broken edit reads as a fresh regression. Interactive
  family modes stay advisory — the APPA permission mode is the strictness
  dial. An anti-thrash policy (3 consecutive non-improving verification
  rounds by default, `WithStallLimit`) stops spinning turns with the
  residual failure set instead of burning budget.

- **Web GUI dark mode.** The whole palette moved into CSS variables with a
  `[data-theme="dark"]` charcoal counterpart, so every panel, modal, and
  bubble follows the theme without per-component branching. Toggle with the
  moon/sun button in the sidebar header or the new Appearance section in
  Settings (Light / Dark / System). The choice persists in localStorage and a
  pre-paint boot snippet applies it before first render, so reloads never
  flash the wrong theme. Accent buttons now draw their label from a dedicated
  `on-seal` color to keep contrast above the WCAG floor in both themes.

- **Past sessions render like live ones.** A new JSON endpoint
  (`GET /api/transcript/messages`) hands the GUI the raw markdown source,
  reasoning, tool runs, and per-turn token usage instead of the old rendered
  HTML fragment, which the client scraped with `textContent` — flattening
  markdown paragraphs into one unbroken wall and dropping everything else.
  Replayed sessions now go through the same markdown / reasoning / tool-row
  pipeline as live chat, compaction cuts render as quiet centered dividers,
  and assistant turns show their `in / out / tok/s` readout. The HTML-scrape
  loader stays as a fallback for older server binaries.

- **Persona prompts are actually injected.** The "Personality" textarea was
  stored on the persona but never reached the model — only the one-line
  tagline summary did. The full prompt now rides into `BuildContext` as its
  own system message (ahead of the summary), and a mid-session persona switch
  re-injects the new profile's prompt.

- **Personas can replace the default system prompt.** Each persona has a
  "Replace default system prompt" toggle in the persona manager. When on,
  the persona's prompt becomes THE system prompt — a1's built-in behavioral
  template, project context, and skills/MCP catalogs are dropped so the
  persona truly owns the agent's voice instead of riding alongside the
  default. Runtime context (clock, weather, persona memories) still flows
  as separate system messages. Switching personas (or editing the active
  one's toggle/prompt) rebinds live engines immediately — no new-chat wait.
  The toggle is off by default; an override persona with an empty prompt
  keeps the built-in prompt so a blank toggle can never lobotomize the agent.
  The web UI marks override personas with a "system override" badge and warns
  that built-in discipline (tool routing, coding standards) is gone while
  the override is active.

### Fixed

- **memstore read-your-write race (the parallel-test flake root cause).**
  `doWrite` acknowledged the caller from inside the write function before
  `tx.Commit()` ran, so a reader on another pool connection could miss a
  just-acked write (WAL readers cannot see uncommitted rows). Insert-then-
  retrieve could intermittently come back empty under CPU contention —
  exactly the nondeterministic TestDedupeMerge / TestRehearsalRevival /
  TestLoopMemoryInletInjectsIntoModelRequest failures that passed with
  `-p 1`. Writes now ack only after Commit; fire-and-forget rehearsal ticks
  keep their nil-result behavior.

- **Tests no longer write to the real `~/.a1`.** The agent package's tests
  opened memory banks through the global project singleton in the user's
  real home (measured: 40 leaked JSONL files per run); a TestMain HOME pin
  makes the whole package hermetic. Seven webserver tests required a
  models-configured `~/.a1/config.yaml` and failed on any fresh machine —
  they now run against a fixture config in a temp home (no skips). Two more
  same-pattern leaks fixed in `session/memory` and `project` tests (one of
  which wrote a stub config into the developer's real `~/.a1/config.yaml`).

- **Fresh web engines adopt the active persona's settings.** A brand-new
  engine built while a persona with overrides was already active (switched
  to it yesterday, opened a new chat today) came up on the global model,
  window, and permission mode — only memory was compensated at build time.
  `liveFor` now applies the active persona to a freshly built engine via
  the same `SetPersona` path a manual switch takes, ordered after the
  server lock releases (the permission-mode setter re-enters the lock when
  it installs the new gate on live engines). The new session runs the
  persona's model, window, mode, and memory surface from its very first
  turn.

- **The memory toggle now follows the persona.** The persona's
  `memory_enabled` is the single effective gate: an explicit persona
  setting wins over the surface's global toggle in BOTH directions (opting
  in turns memory on while the knob is off; opting out turns it off while
  the knob is on), a persona without an opinion inherits the knob, and
  "off" silences the automatic injection/consolidation AND the
  remember/recall tools together. Switching personas rebinds every live
  web engine's memory bank against the effective state, the footer chip
  reads the same value the engines gate on, and the TUI palette toggle now
  writes the persona when the persona owns the setting — and rebinds the
  live engine either way (it previously only moved a flag nothing read
  back, so flipping it mid-session changed nothing).

- **Memory consolidation now runs on every completed turn, not just the
  no-tool-call wrap-up.** Turns that ended through the verdict gate's stall
  stop, an executor stop, the tool-call budget trip, or a declared-budget
  terminal stop never reached the consolidation pipeline — the outlet only
  lived on one terminal path. Every completed-turn terminal now
  consolidates; turns aborted by errors, cancellation, or a declined
  continue-ask still do not (no exchange worth keeping). The inlet's
  injected block is framed in the third person ("memories about the user")
  — some models read a generic or second-person header as things they
  themselves said and attributed injected facts to their own past turns.

- **Persona switches re-apply the model's context window in both
  directions.** Switching to a persona with a model override re-seeds the
  runtime window to that model's configured window (the footer's
  denominator and the compaction triggers follow the new provider
  immediately); switching back to the default persona restores the base
  model's window instead of leaking the last persona's forward. Proven at
  the webserver level: the state payload's `contextWindow` equals the
  persona model's configured window right after the switch.

- **Persona switches now actually apply the persona's permission mode.**
  The `permission_mode` override is stored and round-trips through the
  editor, but the mode never flipped when the persona was switched — the
  engine never called the surface's mode setter, and a persona without an
  override (or the default persona) never restored the mode the surface ran
  before. The override now applies on every switch and the pre-persona mode
  is restored on the way back; the rebuilt gate is installed on every live
  engine, so a `readonly` persona denies writes from the very next tool
  call, behaviorally, on both the server's gate and each engine's own.

- **Persona memories were saved to a file the UI never read.** The agent's
  write path opened the persona bank under the personas directory while the
  web manager counted and listed a bank under the shared session-memory
  directory — two different files, so everything a persona remembered
  landed somewhere the manager never looked and the count stayed at zero.
  The bank location is now owned by the persona store itself
  (`<personas root>/memory/persona:<id>.jsonl`) and every reader and writer
  resolves it through that one path; banks stranded at the old location by
  earlier builds are adopted into the canonical dir on first open.

- **Persona memory counts.** The executor logs every successful tool run into
  the memory bank, and the persona manager counted all of it — "N memories"
  that was mostly bash invocations. Successful tool runs are now typed as
  traces and excluded from the count, the bank preview, and the context
  injections (they were also crowding real decisions out of the ten-slot
  memory window); legacy banks' tool-shaped `context` entries are recognized
  by their `name(args) => result` marker. The `remember` tool additionally
  mirrors its fact into the (persona-scoped) bank, so remembered facts now
  appear — and count — where the persona manager reads. Delete handles keep
  pointing at raw bank indices so "forget" on the filtered view still drops
  the right line.

- **Command palette auto-scroll.** The scroll-into-view effect indexed the
  list's per-group wrapper divs with the flattened command index, so arrowing
  down targeted whole group blocks and the viewport could jump to the bottom
  of the palette. Rows are tagged with their index and looked up explicitly
  now; the view also follows filter changes back to the top.

### Added

- **Tool rows can start expanded — and do by default.** A new
  `ui.auto_expand_tool_output` config key (editable in the web config editor
  under "Terminal UI") opens every tool row by default, so file diffs and
  command output are visible without pressing Enter. The key defaults to **on**,
  so a config that never mentions it expands anyway; set
  `auto_expand_tool_output: false` to opt out. Collapsing a row still sticks for
  the rest of the session — the user's toggle always beats the config default.

- **The TUI now names what is happening.** The composer status slot used to say
  only "Calling tools…" while a tool ran. It now names the running tool and its
  target while a single call is in flight (`Running edit (internal/permission/gate.go)…`),
  falls back to a count when several run concurrently, and still ignores
  user-initiated `!cmd` runs. Read-only and other tool rows also report how much
  came back (`✓ grep EvaluateProspective (2 lines)`), so a finished row states its
  outcome without expanding.

- **File edits and reads render as cards.** `write` / `edit` tool rows render as
  a Claude-Code-style card — `✓ internal/path.go +12 -3` on the header, with the
  unified diff body carrying old/new line numbers, `+`/`-` colored against the
  theme's success / destructive roles, and a dim separator between hunks.
  `read` / `grep` rows use the same card shape with a line-number gutter instead
  of a diff: `✓ read haiku.txt (3 lines)`, `✓ grep "cursor" in . (1 lines)`.
  Added and removed lines are counted from the diff itself, so the edit stat is
  never a guess; a failed edit shows its error immediately instead of hiding
  behind the expand arrow.

- **Sub-agent branches fork from the parent trajectory.** `agent_spawn` now
  seeds the child gate from the parent's live label and shares its effect log
  (`L0c := Lp`), instead of building a fresh gate from scratch. A child can no
  longer start from an unconstrained top label, which was a laundering path for
  data the parent had already ingested. Child acceptances and rulings stay
  scope-bound, so they never cross the fork boundary.

### Fixed

- **`/config` works outside the source tree.** The editor page is embedded in
  the binary, so `a1 config` no longer fails with "config.html not found" when
  run from any directory other than the phi checkout. The slash handler also
  resolves the config path through the session controller and keeps the editor
  server alive after the command returns, instead of tying it to a background
  context that never cancels.

- **APPA prospective evaluation is no longer conditional.** `Gate.Check`
  previously ran the prospective `cτ(L)` computation only after the legacy
  regex/path check had already returned `Ask`, so any call that passed those
  checks with `Allow` skipped prospective evaluation entirely. Release-side
  requirements (trust floors, recipient covers) are now evaluated
  unconditionally, which is what blocks composite read-and-release tools.

- **Effect tokens commit only on reported success.** `Gate.Admit` committed
  `Kτ` unconditionally; failed and indeterminate outcomes now contribute
  nothing, per the paper's effect-log semantics.

- **Unannotated output no longer adopts a declared label.** An output with no
  declared label is recorded as an unresolved source in the partial label
  rather than folding the tool's contribution as if it had been annotated.

- **`Unknown` is no longer on the trust chain.** The zero `Trust` value is now
  off-chain (`Unspecified`), so an unset `Label` can no longer read as
  "trusted", and the trust floor precondition no longer accepts an
  unestablished label. `Label` also lost its `EffectTokens` field, which
  conflated data properties with the effect log.

- **Narrowing requires acceptance.** `IsNarrower` previously used an
  effect-token count comparison as a tie-break, which produced false
  narrowings; narrowing is now decided by the trust rank and reader set alone,
  and acceptances are recorded per call hash instead of resetting on every
  admission.

- **Gate workspace resolves from the session cwd.** Both the TUI controller and
  `a1 run` re-derived the gate's workspace from the process working directory,
  so a session opened in a subdirectory (or from a parent directory) gated
  paths against the wrong root. `NewGateAt` / `WorkspaceRootAt` resolve it from
  the session cwd.

- **Authority rulings name their gap.** `ConsultAuthority` recorded a
  hardcoded `"consult"` pseudo-gap, and the policy's `Authority any` field was
  type-asserted without a comma-ok. A ruling now records the concrete gaps it
  covers and is skipped entirely when it has no target gap, since an unbounded
  override is not a ruling the mandate model permits.

- **Recovery search uses the graph's own path budget** instead of a hardcoded
  `3` that silently overrode `RecoveryGraph.MaxDepth`, and blocked calls now
  report which gaps blocked them.

- **Pre-existing data race fixed** in
  `TestHumanReviewResolver_AsyncCallback`: the test read callback state
  written on the resolver's goroutine with no synchronization, relying on a
  `time.Sleep`. It now joins through a channel.

- **`write` no longer dumps raw content or a stale hash header.** Creating a
  file said only "Created new file." and showed nothing, while overwriting one
  printed an `@file path#TAG` line left over from the hashline-anchor system
  that was removed in 975ca05. A new file now renders its content as `+` lines
  with a line count, and an overwrite shows a real unified diff. The whole
  `internal/util/hash.go` module — `ComputeFileHash`, `FormatFileHeader`,
  `ComputeLineHash`, `ValidateHash`, `NormalizeFileHashText` — was dead once
  write/edit stopped emitting it and is deleted.

- **`grep` no longer overwrites its own transcript identity.** The tool set its
  `Detail` to the match count ("3 matches") on completion, erasing the
  arg-derived phrase ("grep \"cursor\" in .") that identifies the search. The
  count is now only the row's stat.

- **Removed the last traces of the hash-anchor system.** `internal/util/sanitized.go`
  (dead hashline prefix strippers for pasted blocks — zero callers after
  `edit` moved to `old_str` / `new_str`), the `hashline-compatible line hashing`
  line in `internal/util/doc.go`, and the AGENTS.md rule that still said "keep
  hashline `edit`". The system prompt was the live one: it told the model to set
  `edit.hash` to "the 4 hex chars after `#` in `@file path#TAG`", a field that no
  longer exists — so any model following it would call a tool with a nonexistent
  parameter. It now names `old_str` / `new_str`, with a test guarding against
  the regression. Two stale comments in `RelToCwd` / `DiffBlock.displayPath`
  also dropped their hash references.

- **The web server no longer fails open without config.** `Server.policy()`
  fell back to a zero `permission.Policy{}`, whose `BashDefault` is the zero
  value `Allow` — so a Server built without a loaded config allowed every bash
  command. It now falls back to `DefaultPolicy()`, which asks.

- **`watcher` no longer swallows same-scan writes.** The tool recorded
  `lastCheck` *after* walking the tree, so a file written while the walk ran had
  an mtime past the recorded instant and was silently lost until a later scan.
  The instant is now captured before the walk, and a test pins that ordering.
  `TestRunWatcher_WithGlob` also passed an explicit `since` so it no longer
  inherits the package-global `lastCheck` from sibling tests.

- **Settings in the web chat GUI**: `a1 --web` now serves the config editor from
  the same port and origin. A gear icon in the chat topbar opens `/settings`,
  which loads the same editor `a1 config` uses, and a "Back to chat" link
  returns to the chat. The settings page now shares the chat's palette tokens,
  button sizing, and accent-coloured save action. Related fix: `config.html` is
  embedded in the binary, so `a1 config` works from any directory instead of
  failing with "config.html not found" outside a source tree.

- **Permission mode switcher and session pins in the web chat**: the chat
  topbar carries a permission-mode pill (ask / read-only / autopilot / strict)
  that closes over the gate of every running session, so a mode change takes
  effect on the next tool call without restarting anything. Sessions can be
  pinned from the sidebar or from the composer, and pinned chats sort to the
  top of the sidebar. The pin list is stored per project under the session
  directory (`pins.json`), so it never touches the shared config file.

- **Web chat GUI** (`a1 --web` or `a1 web`): a browser chat for the same agent
  engine and session store as the TUI. Serves on http://127.0.0.1:9001 with a
  ChatGPT-style start page and a collapsible sidebar that lists, resumes, and
  starts sessions. Prompts stream over SSE; assistant text, thinking, tool
  calls, and compaction all render live. Permission prompts raised mid-turn
  appear as cards in the transcript with Allow / Deny buttons. Built with htmx
  and Tailwind in a single HTML document embedded in the binary, so there is no
  build step or node toolchain to install.

### Changed

- **Web chat no longer depends on htmx.** The sidebar fragment swap, the prompt
  and permission round-trips, and mode switching all go through `fetch` now.
  htmx's `ajax()` resolves to `undefined` with `swap:"none"` and to an XHR
  otherwise, so it could never return a parsed JSON body — which is why the
  stop button used to 404 and lock the composer until the turn ran to
  completion. htmx is no longer loaded from a CDN, and the live stream was
  already a hand-rolled `EventSource`.

### Fixed

- **`/api/cancel` now stops the turn.** The browser never learned the session
  id the server assigns on the first prompt, so every cancel request carried an
  empty id and returned "session is not running". The id is adopted from the
  `/api/prompt` response, which also fixes the topbar showing a raw session id
  instead of the conversation's name.

- **The web chat UI no longer wedges on a slow or unresponsive URL.** Two
  separate faults caused it: a handler that re-locked the server mutex while
  already holding it (deadlocking every route at once, recoverable only by a
  restart), and a permission-prompt card that never resolved, leaving the
  composer locked and the status pill stuck on "thinking…" for the whole turn.
  Both are fixed and covered by regression tests.


### Removed

- Removed hash-based edit anchors (`@file path#TAG` and `LINE#HASH`) from `edit`, `read`, `grep`, `secret`, `stack`, `test`, and `vector_search`. The new `str_replace`-style `edit` tool uses `old_str` / `new_str` instead.

### Added

- **Curated skill library:** 41 pre-built skills ship embedded and install to
  `~/.a1/skills/` on first run. Each is a `SKILL.md` with YAML frontmatter, a
  structured operating loop, and a `references/` directory. The harness loads
  them into the system prompt and routes by keyword trigger. Covers keel skills
  (`rigorous-coding`, `agent-orchestration`, `spec-driven-development`,
  `debugging-wizard`), language skills (Go, Rust, Python, TypeScript, C++, .NET,
  Swift, JVM, PHP, Ruby, Lua), framework skills (React, Astro, Vue, Angular,
  HTMX, Alpine), and domain skills (system architecture, API design, cloud-native,
  SRE, data engineering, LLM engineering, testing, code review).
- **Skill routing guide** in the system prompt: keel → domain → language →
  framework loading order, with rules for multi-skill stacking.

### Changed

- **Current date and time in every turn's context.** Each LLM request now leads
  with a system message carrying the full date, weekday, clock time, ISO week,
  and timezone zone (e.g. `Friday, 9 October 2026 (20:56:43), 2026-10-09 (ISO
  week 2026-W41, zone CDT)`). The stamp is regenerated per turn, so long-running
  sessions no longer drift from the wall clock, and relative phrasing
  ("yesterday", "next sprint") resolves against a real date instead of the
  model's training cutoff. Applies to the TUI, `a1 run`, and sub-agents.

### Deprecated

- **Post-fork documentation drift is now fixed across the tree.** `AGENTS.md`,
  `doc/*.md`, `CONTRIBUTING.md`, `SECURITY.md`, `.goreleaser.yaml`, and ~50 code
  comments still described the upstream project: module path
  `github.com/pulseaiclub/phi`, clone URLs, `~/.phi/` paths, and `phi update`.
  They now say `github.com/damien1141/a1`, `.a1/`, and `a1 update`, matching the
  binary that actually ships.

- **`auto_expand_tool_output` no longer opens every row.** It now opens
  file-editing rows only. `read` / `grep` rows render as a one-line summary and
  `bash` rows render a tail preview of their output, regardless of the setting.
  The opt-out key keeps its old meaning (`false` = fully collapsed).

### Removed

### Fixed

- **Identity hygiene: the fork's old name stops leaking.** The model env
  overrides are `A1_*` (`A1_MODEL`, `A1_API_KEY`, `A1_BASE_URL`,
  `A1_SKILL_PATH`, `A1_THINK_LEVEL`, `A1_THINK_ENABLED`), with the
  pre-rename `PHI_*` names kept as a one-cycle fallback that warns on
  stderr when it is the one actually consumed — a hard switch would
  silently ignore exported vars in users' shells and CI. `a1 update`
  downloaded assets stamped `phi_<ver>_<os>_<arch>` while GoReleaser
  publishes `a1_…`, so every self-update 404'd; the prefix is fixed and
  pinned by test. Both readme headers carry `# A1 v2.0.1` again (matching
  `version.go`, per the release procedure), the `[Unreleased]` compare
  link points at this repo, the Rust SDK README's dependency URL is
  `damien1141/a1`, the Go extension example calls `a1.New` (it imported
  the `a1` package but called `phi.New`), the extension SDK's
  queue-full error prefix reads `a1:`, and the doc/script comments that
  still said "phi" now say "a1". The released-changelog history and the
  `pli`/`xui` dependency imports are untouched — the first is frozen by
  policy, the second are dependencies, not fork debt.

- **Rate limits no longer end the turn after two seconds of waiting.** The HTTP
  retry policy treated a `429` like a transient `5xx` gateway blip: three
  attempts with delays of 0s / 1s / 2s, then the error surfaced to the user. A
  rate limit is temporary by definition — the provider states when the window
  frees — so waiting nearly always beats failing a turn the user is watching.
  `429` now gets its own budget: five attempts, `Retry-After` honored (capped at
  60s so an absurd header cannot park the loop), exponential backoff with jitter
  when the header is absent, and a 90s ceiling on the whole sequence. Transient
  `5xx` and stale connections keep the tighter three-attempt schedule. Verified
  against a scripted provider: `Retry-After: 1` now waits 1s per attempt (4s
  total across five attempts, against the old 3s), and an absent header backs
  off 1.3s → 2.2s → 4.3s → 8.4s.

- **`scripts/install.sh` and `scripts/install.ps1` could never install a1.** Both
  resolved the fork's default repo (`pulseaiclub/phi`) and looked for release
  assets named `phi_<ver>_<os>_<arch>.tar.gz` — but GoReleaser builds and names
  them `a1_<ver>_<os>_<arch>.tar.gz`. Every download 404'd, and the checksum
  check then aborted. Asset names, repo default, install destinations, PATH
  hints, and log/temp file names now all match the released artifacts. Env
  overrides are renamed to `A1_REPO` / `A1_VERSION` / `A1_INSTALL_DIR`.
  (The model env vars carry `A1_*` names now too — see the identity-hygiene
  entry below; at the time of this fix the loader still read `PHI_*`.)

- **`a1 update` checked the wrong repository.** `internal/util/update` compared
  the running version against `pulseaiclub/phi`'s latest release, so a fork user
  was told to "update" to an upstream version that does not exist on their
  install. It now queries `damien1141/a1`.

- **README header and `internal/version` disagreed.** The readmes advertised
  `# A1 v1.5.1` while the shipped binary reports `v2.0.1`. Both readmes now
  track `version.go`, and the release procedure in `AGENTS.md` states that
  `version.go` is the source of truth.

- **README claimed model env vars are named `A1_*`.** At the time the
  config loader read the pre-rename `PHI_MODEL`, `PHI_API_KEY`,
  `PHI_BASE_URL`, `PHI_THINK_LEVEL`, and `PHI_SKILL_PATH`, so the readme
  was corrected to list those. The identity-hygiene pass has since renamed
  the loader to the `A1_*` names with `PHI_*` kept as a one-cycle
  deprecation fallback — the readme's original claim is true again.

### Added

- **Skills route on their trigger keywords instead of their prose.** Every
  shipped skill has always carried a `metadata.triggers` list in its frontmatter,
  and the system prompt's routing section told the model to match against it —
  but the parser skipped the nested `metadata:` map, so the catalog shipped the
  full `description` instead and the prompt contradicted the data it was handing
  the model. The parser now reads `metadata.triggers` (quoted list, block scalar,
  or comma list) and the catalog renders it.

  The catalog shrink is the point: **~4,800 → ~2,350 tokens of every turn**, with
  no skill lost and routing *more* accurate, because a keyword list is faster to
  match than a sentence. The system prompt as a whole drops ~12.5k → ~10.1k
  tokens per turn. The `**Location:**` line is gone too — the model can derive
  `~/.a1/skills/<name>/SKILL.md` from the name, and it cost ~1,000 tokens to
  print 41 paths. A third-party skill with no triggers falls back to its
  description, so it is never silently invisible.

- **Read and bash rows are collapsed by default.** A `read` / `grep` row now
  renders as a one-line summary — `✓ read internal/permission/gate.go (3 lines)`
  — instead of a card of file content, and a `bash` row keeps a tail preview
  instead of expanding to the full retained log:

  ```
  $ go test ./... ▶
    === RUN   TestHandler193
    --- PASS: TestHandler193 (0.01s)
    … +392 more lines
  ```

  The preview is capped at the last 8 output lines with a dim
  `… +N more lines` trailer, measured on wrapped rows so one 400-column log line
  cannot blow the budget. File edits still open automatically — a diff is the
  one thing that must not be missed — and a user's own expand/collapse choice on
  any row still wins for the rest of the session.

### Removed

- **The legacy `judge:` config section and its web-panel twin.** The
  config page carried two panels for one idea: "Judge" edited the pre-JEV
  `judge:` section — an Ollama-only fallback that only ever fed the judge
  tool — while "JEV decision model" edits the `jev:` section every
  decision consumer reads: fetch routing, evidence verdicts, compaction
  boundaries, the bash risk guard, and the judge tool itself. The
  duplicate is gone — panel, YAML section, and the tool's fallback path.
  Configure the decision model in one place (`jev:`, or the
  TYPESAFE_API_KEY environment fallback). A leftover `judge:` section in
  config.yaml is ignored, and the next save from the web editor drops it;
  the judge tool answers with the configuration error naming `jev:` when
  nothing resolves.

### Security

<!-- Released section -->
<!-- Everything from here down is frozen history. Only a release PR carrying
the "Unlock Released Changelog" label may edit it, and
verify_released_changelog.sh diffs exactly these lines on every PR. Do not
remove the marker above — without it the script compares an empty region and
the gate silently passes on everything. -->

## [1.5.0] - 2026-10-06

### Added
- Fish-style slash command autocomplete with arg-level completion.
- `/permission` slash command with arg completion for `trajectory`, `recovery`, `authority`.
- `ArgCompleter` support on `Command` with `FilterSlashArgs` in the registry.
- `ActiveSlashArgs` cursor detection in `internal/components/chat/slash.go`.
- Chained tab completion: completing a command with args immediately reopens the arg picker.
- Permission prompt now offers three options: **Allow once**, **Allow forever** (persists to `~/.a1/config.yaml` allowlist), **Reject**.
- `StaticGate.AppendBashAllow` and `project.AppendBashAllow` for runtime allowlist mutation.

### Changed
- `/permission` command executes directly instead of inserting text.

### Fixed
- APPA end-to-end behavior verified; autopilot mode still bypasses checks for allowlist matches.

### Security

[1.5.0]: https://github.com/pulseaiclub/phi/compare/v1.2.1...v1.5.0
[1.5.0]: https://github.com/pulseaiclub/phi/releases/tag/v1.5.0

## [1.2.1] - 2026-10-05

### Added

### Changed

### Deprecated

### Removed

### Fixed

- **Browser tool:** the LLM-facing tool path no longer auto-launches Firefox.
  `runBrowser` only controls an already-open browser, so the model cannot
  spawn the browser on its own. If the browser is closed, the tool returns a
  clear message asking the user to open it with `/browser open <url>` or to
  use `fetch` instead. The slash command still auto-launches the browser as
  before.

### Security

## [1.2.0] - 2026-10-05

### Added

### Changed

- **Version bump to v1.2.0.**

### Deprecated

### Removed

- Removed hash-based edit anchors (`@file path#TAG` and `LINE#HASH`) from `edit`, `read`, `grep`, `secret`, `stack`, `test`, and `vector_search`. The new `str_replace`-style `edit` tool uses `old_str` / `new_str` instead, which is what LLMs are actually good at.

### Fixed

### Security

## [1.1.0] - 2026-10-02

### Added

- **Curated skill library:** 41 pre-built skills ship embedded and install to
  `~/.a1/skills/` on first run. Each is a `SKILL.md` with YAML frontmatter, a
  structured operating loop, and a `references/` directory. The harness loads
  them into the system prompt and routes by keyword trigger. Covers keel skills
  (`rigorous-coding`, `agent-orchestration`, `spec-driven-development`,
  `debugging-wizard`), language skills (Go, Rust, Python, TypeScript, C++, .NET,
  Swift, JVM, PHP, Ruby, Lua), framework skills (React, Astro, Vue, Angular,
  HTMX, Alpine), and domain skills (system architecture, API design, cloud-native,
  SRE, data engineering, LLM engineering, testing, code review).
- **Skill routing guide** in the system prompt: keel → domain → language →
  framework loading order, with rules for multi-skill stacking.

### Changed

- Bump version to v1.1.0.
- `browser.search` now defaults to DuckDuckGo instead of Google.

### Deprecated

### Removed

### Fixed

- **Browser tool:** the Firefox switch left the old `browser playwright.Browser`
  global declared but never assigned (`LaunchPersistentContext` returns a
  context, not a browser). `browserClose` called `browser.Close()` on a nil
  browser, so every `/browser close` (and the close on exit) panicked with a
  nil-pointer dereference. Removed the dead global; close now stops the
  Playwright driver and closes the persistent contexts.
- **Thinking mode switcher:** the on-the-fly Tab cycle and the
  `settings → think` palette only offered `off / minimal / low / medium /
  high / xhigh / max`. The provider-adaptive "default" was missing — it is
  what `buildThinkingConfig` maps to Anthropic's `thinking.type=adaptive`, and
  what the config UI already exposes as `(default)`. Added a `default`
  `ThinkMode` sentinel plus `AllThinkModes()`; the cycle and palette now
  include it, and the controller resolves it to the active model preset's
  thinking config before any request is sent, so providers never see the
  sentinel.
- **Auto-compaction on context overflow:** `runCompact(force=true)` skipped
  both the safety and growth triggers by design, but the guard that no-ops
  when neither trigger fired also applied to the force path — so the
  overflow-recovery branch never compacted and burned its single retry on a
  second overflow. The guard now requires `!force`, so force-compaction runs
  whenever there is history to summarize. Added `TestRunCompact_ForceCompactsWhenHistoryExists`.
- **Default skills directory:** the directory wiring was already correct —
  `Discover` installs the embedded skill library into `~/.a1/skills` and
  `loadConfig` defaults `skill_path` to that dir — but every installed skill
  failed to parse. `parseFrontmatter` rejected any indented frontmatter line
  with `ErrInvalidYAML`, and every shipped skill carries a nested `metadata:`
  map block, so `LoadSkills` silently returned an empty list and the skills
  palette showed "No skills found". Indented continuation lines are now
  skipped (the parser only needs the flat top-level keys) instead of erroring.

### Security

## [0.27.5] - 2026-09-22

### Added

- `/code <path>[:line]`: full-screen source viewer with syntax highlighting, a
  CJK-aware caret (`j`/`k`, `g`/`G`, `h`/`l`), and `v` to select lines then `a`
  to hand them to the chat input. The pane reads files itself, so it answers to
  the same deny list as the tool gate.

### Changed

### Deprecated

### Removed

### Fixed

- Horizontal scrolling no longer drops characters from the middle of a
  wide-glyph (CJK) line in the diff and file views.

### Security

## [0.27.4] - 2026-09-20

### Added

- `/branch`: pick a working branch from the composer — name and last commit
  only, `●` for the current branch, Enter runs `git switch` off the UI goroutine
  and the footer branch label refreshes immediately.
- `/branch <name>`: switch to that branch, or create it from HEAD when nothing
  carries the name yet. Remote rows check out the local branch that tracks them.
- TypeSafe System One client support via `internal/llm/jev`.

### Changed

- `/branch` completes with a trailing space, so typed arguments follow the command
  name instead of gluing onto it.

### Deprecated

### Removed

### Fixed

### Security

## [0.27.3] - 2026-09-17

### Added

### Changed

### Deprecated

### Removed

### Fixed

- Token usage is four disjoint buckets end to end: `prompt` is the input that
  missed the cache, cache reads and writes are counted separately, and the
  context fill comes from the provider's total (or the bucket sum). Earlier the
  context fill was sized from the uncached input alone, so a cache-heavy turn sat
  at 0% of a 1M window, and Gemini, OpenAI Responses and OpenAI-compatible chat
  each split the cache out differently. Compaction now reads the same number the
  composer shows, and `phi run` emits the cache counts alongside `prompt`.

### Security

## [0.27.2] - 2026-09-16

### Added

- Add built-in `gpt-5.5` and `gpt-5.5-pro` presets using the OpenAI Responses API.

### Changed

### Deprecated

### Removed

### Fixed

- Extension RPCs distinguish host requests from replies, bound blocked writes and
  shutdown, and terminate the plugin on in-flight cancellation or timeout.
- Go and Rust SDKs preserve requests received during confirmation dialogs and
  report oversized tool results explicitly. Rust async tools support network IO
  and timers on the SDK runtime.
- Plugin updates use the installed directory independently of the manifest name,
  prepare replacements before moving the working version, and honor explicit
  version pin changes and downgrades.

### Security

## [0.27.1] - 2026-09-15

### Added

### Changed

- Composer pickers: `Tab` completes the highlighted row into the input
  (`/diff`, `@path`, each with a trailing space) instead of moving down, and never runs a command —
  `/clear`-style no-arg commands still run on `Enter` only. The session and
  diff file pickers accept on `Tab` as well. `Shift+Tab` no longer steps back;
  use `Up` / `Ctrl+P`.

### Deprecated

### Removed

### Fixed

- Compaction summaries are capped at a fraction of the headroom they free
  (0.8x `reserveTokens` for history, 0.5x for a turn prefix), and a summary the
  provider stopped at that cap is rejected instead of persisted. A truncated
  summary used to overwrite the session checkpoint and drop every message it
  stood in for, with no way back.
- Compaction now measures the cut budget per message instead of summing the
  provider's reported usage. Each assistant message reports the size of the
  whole conversation up to that turn, so the budget overflowed on the newest
  message: the recent window was never kept, the cut was always mid-turn, and a
  previous summary could be replaced with `No prior history.`
- Compaction no-ops instead of persisting a summary when nothing falls outside
  the recent window.
- Cutting mid-turn (compaction lands inside a turn) now sends a turn-prefix
  summarization prompt with the prefix. The request carried the conversation
  dump and no instruction at all, so the model's continuation — not a summary —
  was what got persisted as the session summary.
- The transcript compaction marker now shows the pre-cut context size
  (`Compacted from 15k tokens`). The count was persisted on every compaction
  entry but nothing ever read it: the marker text was hardcoded in three
  places, and a resumed session dropped the value entirely.
- Resuming a session (`/sessions`) now refreshes the composer token readout
  from the resumed session instead of keeping the previous session's counts;
  sessions without reported usage clear the label.
- Compaction now reads token usage from the persisted entry instead of the
  in-memory message field, which is dropped on load. Resumed sessions were
  seen as having spent zero tokens, so the first auto-compaction summarized
  nothing and context-overflow recovery did not compact at all.

### Security

## [0.27.0] - 2026-09-15

### Added

- OpenAI Responses API route (`api: OpenAIResponses`) via
  `/v1/responses`, for models that no longer speak chat-completions
  (e.g. GPT-5 tool calling).
- Built-in DeepSeek model presets: config entries named `deepseek-flash`
  or `deepseek-v4-pro` auto-fill base_url / context_window /
  image_enabled from the catalog, so only name + api_key is required.
- Built-in Gemini model presets (`gemini-2.5-pro`, `gemini-2.5-flash`,
  `gemini-3-pro`, `gemini-3-flash`) with provider-native thinking
  config (budget for 2.x, level for 3.x).
- Built-in Kimi model presets (`kimi-k3`, `kimi-k2.7-code`) with Moonshot API defaults.
- Built-in GLM presets (`glm-5.3`, `glm-5.3-flash`) on the z.ai coding
  plan, with Preserved Thinking.
- Per-model `think_enabled` / `think_level` config keys and
  `PHI_THINK_LEVEL` env var override.
- Rust SDK (`ext/rust`, crate `phi-ext`): `Context` gains `cwd()`,
  `session_id()`, and `has_ui()` accessors.
- Docs: [Supported models](doc/models.md).

### Changed

- Provider routing uses explicit `models[].api` (`OpenAI` /
  `OpenAIResponses` / `Anthropic` / `Gemini`) instead of guessing from
  model name or base URL.
- Vendor thinking wire shape (DeepSeek `extra_body`, Gemini budget/level)
  lives on model preset request interceptors, not client name matching.
- Rust SDK: PXB messages are built by the declarative `pxb_message!` macro —
  `pxb::Hello::encode()` / `decode()` replace the free `pxb::encode_hello` /
  `decode_hello` functions (and likewise per message type). Wire format
  unchanged.
- `/diff`: an empty overlay names the comparison (`No changes vs <rev>`) and
  reminds that untracked files never show up; git failures collapse to one
  line with the next step (`fetch first`, `open inside the repo`).

### Deprecated

### Removed

- `/resume` slash command — use `/sessions` picker or `phi run --session <id>` instead.

### Fixed

- Windows: `fd` / `rg` installed in `~/.phi/bin` as `fd.exe` / `rg.exe` are
  found again — the bin dir probe now tries the `.exe` suffix, so `find` and
  `grep` no longer report "fd is not available".
- Windows: Git Bash is found for per-user installs
  (`%LocalAppData%\Programs\Git`) and for custom roots reached through the
  Git on PATH (`D:\Git`), instead of falling through to WSL's legacy
  `bash.exe` shim and failing with `execvpe(/bin/bash): No such file or
  directory`.

### Security

## [0.26.0] - 2026-09-10

### Added

- Extensions: `ToolResult.Expanded` opens the TUI tool row by default (e.g. plan
  body). User toggle still wins after the first interaction.
- CI / `make lint-markdown`: markdownlint on `**/*.md`.
- TUI `/diff`: full-screen git diff review (working tree / staged / HEAD),
  line notes in `.phi/review.json`, `a` sends notes to the agent.

### Changed

### Deprecated

### Removed

### Fixed

- Compaction file-op details are a typed `session.CompactionDetails` (not `any`),
  so persist/reload and later compact rounds keep prior read/modified files.
- `/diff`: cursor movement no longer leaves a blue trail on swept rows; help
  overlay no longer fills with selection background. Notes autosave; drop
  unused GitHub-shaped comment fields and the redundant `w` save key.

### Security

## [0.25.1] - 2026-09-10

### Added

### Changed

### Deprecated

### Removed

### Fixed

- MCP HTTP transport: SSE bodies that interleave server notifications
  (e.g. `notifications/message` log frames, no `id`) before the JSON-RPC
  response frame now resolve to the response frame instead of the first
  parseable frame, which previously made `tools/call` return an empty
  result when a server streams log frames before answering.
- Preserve Unicode when copying TUI text to the Windows clipboard.

### Security

## [0.25.0] - 2026-09-09

### Added

- Sub-agents: optional per-role model defaults in `agents.models` (`explore` /
  `review` / `worker`). Palette: settings → agents → models → role → model
  (session-only; `(inherit parent)` clears). Omitted roles inherit the parent
  model. (`project`, `agent`, `tui`)

### Changed

- Sub-agents: child gates use `BashDefault=Allow` (hard deny list still applies), so explore/review/worker can run non-allowlisted shell (`make`, pipelines, tests) without Ask→Deny folding. Role hints and parent spawn guidance emphasize task contracts (recon / review report / scoped implement) rather than allowlisted bash. Hard deny now also blocks piping into `sh`/`bash`.

### Deprecated

### Removed

### Fixed

- Agent: mid-loop context-window overflow (`prompt is too long` and similar
  provider errors) now force-compacts once and retries the stream instead of
  failing the turn cold. A second overflow still fails closed. (`agent`, `llm`)
- Config: `agents.enabled` omitted under an `agents:` block (e.g. only
  `agents.models` set) no longer decodes as false; default stays on. (`project`)

### Security

## [0.24.0] - 2026-09-08

### Added

- Extensions: tools can be marked `Readable` (side-effect-free) via the Go
  SDK `Tool.Readable` or the Rust SDK `Tool::readable()`; the host surfaces
  it as `Definition.Readable`, so a batch of all-readable calls — including
  extension tools — runs concurrently.

### Changed

- Agent: when every tool call in a turn targets a read-only tool
  (`Definition.Readable` — read/grep/ls/find), the calls now execute
  concurrently; results keep call order, and any write-capable call in the
  batch falls back to sequential execution.
- Rust SDK (`ext/rust`): tool `execute` handlers can now be async
  (`Tool::new_async`) — the SDK drives them to completion on a
  single-threaded tokio runtime, so network / IO calls work without blocking
  tricks; sync `Tool::new` handlers are unchanged.
- LLM provider errors now read as one compact line — e.g. `anthropic API error
  (400): prompt is too long` — using the provider's own message instead of a
  raw JSON body, which can no longer flood the terminal or session history.
- TUI: assistant blocks that end in error state render their body in red under
  an `Error:` label, so failures read like tool errors.

### Deprecated

### Removed

### Fixed

### Security

## [0.23.0] - 2026-09-07

### Added

- Gemini endpoints (Google AI Studio / Vertex AI): streaming chat and compaction with tool calling, image input, system prompts, and model thinking surfaced as reasoning. (`llm`)
- `phi plugin list` / `phi plugin update [repo[@ref]] [--check]` / `phi plugin remove <repo>` (alias `rm`): audit and update extensions without manual removal. Install records the GitHub source in `~/.phi/extensions/<repo>/.phi-install.json`; `update` re-resolves that source and swaps the directory atomically (release archive preferred, git clone fallback, pinned tags stay pinned unless overridden). Extensions not installed via `phi plugin install` are left untouched.

### Changed

- Rust SDK (`ext/rust`): tool-schema and confirm-dialog JSON now serializes with `serde`/`serde_json` instead of hand-rolled writers (wire output unchanged; `preserve_order` keeps key order).

### Deprecated

### Removed

### Fixed

- Fix PowerShell installer parsing of checksum mismatch errors.
- Extension footer status (e.g. plan-mode hints) is reset when extensions are reloaded or the model is switched, so stale text no longer outlives the extension subprocess.

### Security

## [0.22.0] - 2026-09-04

### Added

### Changed

- TUI: composer bottom-left status slot now toggles between activity/spinner (busy) and token stats (idle); busy state animates a highlight through the activity text (no block-glyph scan bar); the bottom footer row is reserved for extension status, live jobs, and update hints.
- TUI: chat-input border chrome speaks one ambient voice (path, idle tokens, activity); model Identity remains the sole frame accent; context % escalates to Warning/Destructive only under pressure.
- TUI color dialect: Identity ≠ Success; Title for panel/md structure; ToolName is the sole cool action accent (tools, keybinds, paths, code); Warning/Destructive reserved for risk; default theme is Dark (Terminal stays a compatibility option).

### Deprecated

### Removed

### Fixed

### Security

## [0.21.1] - 2026-09-03

### Added

- Slash commands can declare `NeedsArgs` (Go) / `needs_args` (Rust). Picker accept or bare submit of `/name` leaves `/name` plus a trailing space in the composer so the user can type arguments instead of auto-running and toasting usage. Built-in `/resume` uses this.

### Changed

- Go author SDK nested module now publishes under `github.com/pulseaiclub/phi/ext/go` (imports `…/ext/go/phi`, `…/ext/go/pxb`; tags `ext/go/vX.Y.Z`), matching its repo location `ext/go/` so proxies can resolve it.

### Deprecated

### Removed

### Fixed

- Publish Rust crate workflow: write `exists` to `$GITHUB_OUTPUT` so `cargo publish` actually runs when the version is new (previously the step was always skipped).

### Security

## [0.21.0] - 2026-09-03

### Added

- Extension tools can set `DetailFromArgs` (Go) / `detail_from_args` (Rust) so the TUI shows a one-line summary instead of raw JSON args before execution.
- `phi plugin install` prefers a GitHub Release archive for the current OS/arch (`{repo}_{version}_{goos}_{goarch}.tar.gz`, same layout as `phi update`), with optional checksum verification; falls back to shallow git clone when no matching asset exists.

### Changed

### Deprecated

### Removed

### Fixed

### Security

## [0.20.0] - 2026-09-02

### Added

- Extension tools can set `TimeoutSec` (Go SDK) / `timeout_sec` (Rust SDK) so the host waits longer than the default 30s RPC budget for slow tools (e.g. HTTP fetch). Clamped to 1–3600s.
- Rust SDK: typed `phi::Schema` builder for tool parameters (replaces raw `Vec<u8>`), staying zero-dep instead of Codex-style `schemars`.

### Changed

- TUI chrome consistency: shared glyph/hint dialect (`internal/components/chrome`), unified ask/continue/confirm option rows, `Theme.Identity` for model/user/bash chrome (separate from Success status), and aligned transcript expand indent / status suffixes. Permission/continue/confirm overlays navigate with arrows only (no Alt+N accelerators). Sessions list picker uses a wider panel (~90% / up to 112 cols) so previews truncate less.

### Deprecated

### Removed

- Palette command `clipboard copy last message` (`Ctrl+Shift+C`); the key chord still copies selection / last message via the transcript handler.

### Fixed

### Security

## [0.19.2] - 2026-09-02

### Added

- Rust author SDK: zero-dependency `ext/rust` crate (`phi-ext`) — PXB wire protocol (`pxb`) + `Extension` API (`phi`), byte-compatible with the Go SDK via golden fixtures, plus fake-host end-to-end tests. See [doc/extensions.md](doc/extensions.md).

### Changed

### Deprecated

### Removed

### Fixed

### Security

## [0.19.1] - 2026-09-01

### Added

- Publishable Go author SDK: nested module `github.com/pulseaiclub/phi/ext` (`go get …@vX.Y.Z` via `ext/vX.Y.Z` tags). See [doc/extensions.md](doc/extensions.md).

### Changed

### Deprecated

### Removed

### Fixed

- Extension discovery follows symlinked directories (e.g. `.phi/extensions/foo` → `examples/foo`), so linked installs are no longer skipped.

### Security

## [0.19.0] - 2026-09-01

### Added

- **PXB extensions:** native binary extensions speaking a length-prefixed binary protocol (`ext/pxb`) over stdin/stdout. Author SDK `ext/phi`. Layout: `~/.phi/extensions/<name>/phi.yaml` + `exec`. See [doc/extensions.md](doc/extensions.md). Palette: **extensions → list/reload**. Disable with `PHI_EXTENSIONS=off`.
- `phi plugin install <github-repo[@tag]>`: shallow-clone a GitHub repo into `~/.phi/extensions/<repo>/` (requires `phi.yaml` + compiled binary).
- Extension full chain: `user_input`, `turn_stopping`, `session_compact` intercepts/events; `SubscribeEvent` with payload; `SendUserMessage` host request.
- Extension **Confirm** dialog (blocking RPC).
- `/sessions` opens an opaque filterable session picker (Enter resumes); no longer prints into the transcript.

### Changed

- TUI: `agent_spawn` / `agent_wait` tool rows show role in the detail line (`explore · …`).
- **Breaking:** Shell `plugin.json` hooks (`internal/hooks`, `PHI_HOOKS`, `.phi/hooks`) are removed. Rewrite policy as extensions (migration table in doc/extensions.md).
- **Breaking:** Yaegi-interpreted `.go` extensions are no longer loaded. Migrate to PXB binaries + `phi plugin install`.

### Deprecated

### Removed

- Extension **ShowPane** / **UpdatePane** / **ClosePane** / `OnPaneAction` (and `internal/tui/extpane`). Prefer Ctrl+K-style overlays for list UIs.
- Shell hook plugins (`plugin.json` + `type: "command"`), `doc/hooks.md`, and related TUI **hooks →** commands.
- Yaegi extension loader (`github.com/pulseaiclub/yaegi` dependency).

### Fixed

- `/resume` closes the previous extension runner and rebinds host UI (was leaking subprocesses and dropping Notify/Confirm).
- Controller `Close` and headless `phi run` shut down extension subprocesses; TUI defers `ctrl.Close()` on exit.
- Extension `Notify` / status frames now reach the TUI (`Proc.onNotify` wired in `Runner.Bind`).
- Slash-command `Submit` from PXB `CommandResponse` is delivered to the composer.
- Extension handshake registration respects the handshake timeout (no longer only checked between blocking reads).
- `session_before_switch` toast without cancel is published (previously only on deny).
- SDK command handlers receive a usable `ctx.UI` (maps to `Notify` / `SetStatus`).
- `OnToolResult.Stop` now ends the agent loop (was discarded in the executor).
- Duplicate `turn_end` from the TUI controller removed (engine owns turn indices).
- Session ID / previous / target fields ride on lifecycle `EventNotify`; host pushes `SessionMeta` after `/new` / `/resume`.

### Security

## [0.18.1] - 2026-08-28

### Added

### Changed

- **Breaking:** `plugin.json` hooks use an event-map shape (`PreToolUse`, `PostToolUse`, … + `type: "command"` shell commands). Legacy flat `pre_tool` / `run` manifests are no longer accepted. Phi extensions: `SessionShutdown` (leaving the active session; `SessionEnd` is a deprecated alias), `SessionBeforeSwitch`, `PostTurn`, and `Command` slash hooks. See [doc/hooks.md](doc/hooks.md).
- Hooks: drop unimplemented `plugin.json` fields (`if`, `once`, `statusMessage`, `asyncRewake`) until needed.

### Deprecated

### Removed

### Fixed

- `/resume` and `/clear` now replay tool executions (with their styled rows) instead of dropping them.

### Security

## [0.18.0] - 2026-08-27

### Added

- Composer shortcut help: type `?` at the start of the input (like `/` commands); Esc closes the picker.
- `Ctrl+A` / `Ctrl+E` move the composer cursor to the start/end of the current line.
- `Ctrl+U` clears the composer input, pending images, and pending skills.
- Per-model `image_enabled` config key; the composer blocks clipboard and `@` image attach and shows a warning when the active model does not support images.
- `make check` runs `deadcode -test` against a baseline so new unreachable functions fail CI without blocking on known legacy dead code.

### Changed

- Splash hint now shows `?` for shortcut help instead of the removed `!` shell-command shortcut.

### Deprecated

### Removed

- Dead code: unused layout widgets, `StatusBlock`, `input` package, orphan exported helpers, and unused clipboard read-text path.

### Fixed

- `@` file picker: cancel in-flight `fd` searches on each keystroke / close, surface timeouts as actionable hints, and note when the match list is truncated.

### Security

## [0.17.0] - 2026-08-25

### Added

- `phi run --yolo`: skip all permission checks for one headless run (benchmarks / CI).
- `phi run --tools`: limit a headless run to selected built-in tools.
- Hooks: session lifecycle events now include `usage` — token counts of the latest completed assistant turn.
- Hooks: `post_turn` event fires after each completed assistant stream with per-round `usage` (for audit metrics such as cache hit ratio).
- `util/clipboard` reads and writes the system clipboard (text; images via wl-paste/xclip, osascript/pngpaste, or PowerShell).
- Composer image attachments: Ctrl+V clipboard, `@` image files, pending queue, and LLM `Images` on submit.
- `util/image.Load` reads an image file (raw bytes + content-sniffed MIME type; png/jpeg/gif/webp, up to 10 MiB).

### Changed

- System prompt and `agent_spawn` guidance now state the sub-agent concurrency cap (default 4; spawn beyond it fails, no queue).

### Deprecated

### Removed

### Fixed

- Tool errors no longer duplicate the error text in the TUI (Error and Output shown the same message twice).

### Security

## [0.16.0] - 2026-08-22

### Added

- Hooks: `command` UI intents — `status` (footer), `list` (palette page).
- Hooks: session lifecycle events `session_start`, `session_shutdown`, `session_before_switch`.

### Changed

### Deprecated

### Removed

### Fixed

### Security

## [0.15.0] - 2026-08-20

### Added

### Changed

### Deprecated

### Removed

- `agent_list` `status` filter parameter (always returns the full list; each row still includes `status`).

### Fixed

### Security

## [0.14.0] - 2026-08-20

### Added

- Hook event `command`: `plugin.json` entries register TUI slash commands (`/name` runs `run`). stdout may `submit` a user message or `toast`.

### Changed

- `write` creates or overwrites files (no longer create-only). Use `edit` for surgical changes.
- File tools resolve relative paths against the session cwd and print cwd-relative results (`find`/`ls`/`grep`/`read`/`write`/`edit`). Absolute paths are used internally (including rg/fd) and returned only when the file is outside cwd.
- `find` (formerly `glob`) uses `fd` from `~/.phi/bin` (same as `rg`): respects `.gitignore`, early-stops at limit, optional `limit` arg.
- Renamed directory listing tool `list` → `ls`.

### Deprecated

### Removed

- Built-in `fetch` tool (and `permissions.fetch` config). Use MCP if you still need URL fetching.
- `agent_log` tool (parent agents only get `agent_wait` summaries; job logs remain on disk under `~/.phi/jobs/`).

### Fixed

- `phi update` on Windows: stage the download next to the installed binary (same volume) and fall back to copy when rename still cannot cross drives.
- Assistant fenced code blocks drop the box/`-----` chrome; a muted language caption sits above the highlighted code so mouse selection stays copy-clean.

### Security

## [0.13.0] - 2026-08-18

### Added

- TUI hot-reloads the git branch in the path label: switching branches outside the app (another terminal, an editor) refreshes the label automatically.

### Changed

- TUI activity: tool rows keep a 1-cell braille spinner; the footer uses an
  Knight-Rider scan bar so the two don't share the same glyph.
- Tool routing: bash is no longer described as an inspection tool; grep/glob no
  longer nudge `agent_spawn`; `edit.hash` is the 4 hex chars after `#` in
  `@file path#TAG` (leading `#` / full header copy-paste is accepted).
- **Breaking:** hooks are declared in `plugin.json` (one file, many hooks) instead of
  per-directory `hook.json`. Load `~/.phi/hooks/plugin.json` and
  `~/.phi/hooks/<plugin>/plugin.json` (same under the project `.phi/hooks/`).

### Deprecated

### Removed

- Per-hook `hook.json` directories. Use `plugin.json` instead.

### Fixed

### Security

## [0.12.0] - 2026-08-17

### Added

- Changelog gate: PRs must update `CHANGELOG.md` (with skip labels / `[chore]`), released sections are protected, and GitHub Release notes are taken from this file.

### Changed

- Hashline `edit` now requires a whole-file `@file path#TAG` (`hash` field) from `read`/`grep`; after a successful edit, re-read before another `edit` on that path. Per-line hashes are 3 letters (a-z) and no longer use digits.

### Removed

- Remove the redundant `agent_task` tool; compose `agent_spawn` + `agent_wait` instead.

## [0.11.0] - 2026-08-16

Baseline release when this changelog became the source of truth for user-visible changes.
Earlier releases are available from GitHub tags only.

<!-- Released section ended -->

[Unreleased]: https://github.com/damien1141/a1/compare/v1.5.0...HEAD
[1.2.1]: https://github.com/pulseaiclub/phi/compare/v1.2.0...v1.2.1
[1.2.0]: https://github.com/pulseaiclub/phi/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/pulseaiclub/phi/releases/tag/v1.1.0
[0.27.5]: https://github.com/pulseaiclub/phi/compare/v0.27.4...v0.27.5
[0.27.4]: https://github.com/pulseaiclub/phi/releases/tag/v0.27.4
[0.27.3]: https://github.com/pulseaiclub/phi/releases/tag/v0.27.3
[0.27.2]: https://github.com/pulseaiclub/phi/releases/tag/v0.27.2
[0.27.1]: https://github.com/pulseaiclub/phi/releases/tag/v0.27.1
[0.27.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.27.0
[0.26.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.26.0
[0.25.1]: https://github.com/pulseaiclub/phi/releases/tag/v0.25.1
[0.25.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.25.0
[0.24.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.24.0
[0.23.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.23.0
[0.22.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.22.0
[0.21.1]: https://github.com/pulseaiclub/phi/releases/tag/v0.21.1
[0.21.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.21.0
[0.20.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.20.0
[0.19.2]: https://github.com/pulseaiclub/phi/releases/tag/v0.19.2
[0.19.1]: https://github.com/pulseaiclub/phi/releases/tag/v0.19.1
[0.19.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.19.0
[0.18.1]: https://github.com/pulseaiclub/phi/releases/tag/v0.18.1
[0.18.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.18.0
[0.17.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.17.0
[0.16.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.16.0
[0.15.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.15.0
[0.14.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.14.0
[0.13.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.13.0
[0.12.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.12.0
[0.11.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.11.0
