# TODO

Working backlog. Everything here is user-reported unless noted. Each item ships
with tests. See [CHANGELOG.md](CHANGELOG.md) for what already landed and
[doc/history-*.md](doc/) for the archived work logs.

## Active: the Dashboard — the new landing page of a new chat (2026-11-05) — PLANNED, CHUNKED, NOT STARTED

User directive: "next we should get to work on a new section of the webui. I call
it the dashboard its the new landing page of a new chat … calendar, kanban, and
emails fall under personal info … we add another checkbox that allows access to
the personal stuff … massive scope I know … we gotta chunk it - so we dont lose
our minds - lets plan it in the todo.md." The dashboard replaces the welcome
screen: the chatbox docks bottom-center with a bigger avatar, a GitHub-commit-
style usage tracker, pinned sessions, calendar + kanban + a mini mail client
(the "personal stuff"), and a split-screen file browser that opens on the
server's current directory with full-system navigation and syntax-highlighted
preview/edit. Personal data is gated by one new checkbox on the config webpage
beside the thinking/image toggles. Supersedes the "Web dashboard" bullet in the
roadmap horizons below (mailboxes now defined: local Maildir, read-only).

### Decisions locked (so a context reset doesn't relitigate them)

- **The dashboard is the SPA welcome state, not a new route.** The app has no
  URL router — the Overlay union (App.tsx:51-63) is the navigation — so the
  welcome branch (App.tsx:692-741) becomes `<Dashboard/>`. Submitting from the
  docked chatbox is today's exact flow: `selectSession('')` + `sendPrompt` and
  the chat view takes over. No routing framework, no second page.
- **One switch governs all personal data:** config.yaml `personal.enabled`
  (nil ⇒ false — locked until the user opts in), with `personal.calendar_dir`
  and `personal.mail_dir` in the same section. Enforced SERVER-side: a
  `personalGate` helper 403s every `/api/personal/*` handler when disabled; the
  checkbox only hides widgets client-side. Never trust the UI.
  `GET /api/personal/status` is the one ungated endpoint (the dashboard needs
  it to render locked states). Agent-side personal tools are RULED IN
  (2026-11-05, the chunk-0 go): each model card gains personal_read /
  personal_write checkboxes in the thinking/image visual language — the
  model's autonomy is opt-in per model, mail stays read-only even for the
  model (writes touch calendar events and kanban cards only), and the
  master switch stays a precondition: tools mount only when
  personal.enabled AND the model's flags are on.
- **Calendar = a local directory of .ics files, read-only.** No online sync, no
  CalDAV — the user ruled "I dont use online sync- so we can forget about it."
  v1 parses single events (DTSTART/DTEND, all-day, TZID); RRULE is chunk 6.
- **Mail = Maildir, read-only v1.** No SMTP send, no writes back to the
  Maildir — flags untouched, mark-as-read stays client-side until a later
  ruling. The mailbox is on disk; the harness only reads it.
- **Kanban = vault-backed** (subsystem-owned table, the job/schedule pattern
  from `internal/job/rows.go`), three fixed columns v1: todo / doing / done.
- **The file browser is deliberately unconfined.** `/api/code` stays
  project-root confined for the agent; the browser gets NEW endpoints
  (list/read/write) that may traverse the whole filesystem — that is the
  feature on a loopback server, and `a1 serve`'s non-loopback token auth covers
  them like every other route. Writes refuse when the live permission mode is
  readonly. Env kill-switch `A1_FILES=off` (the `A1_MCP=off` precedent).
- **Highlighting: vendor highlight.js** through the existing bun build (no
  CDN, offline-first), theme tokens matched to paper/ink. CodeMirror 6 is a
  parked stretch, not a dependency of the epic.

### Ground rules — binding for every chunk

- Trio law: every new Go package ships `trio_test.go` (internal/calendar,
  internal/kanban, internal/maildir, …); new webserver handlers get covered in
  a webserver trio file. Names: `Test<Subject>_<Case>_Positive|_Negative|
  _Differential`; `scripts/test-trio-coverage.sh` must exit 0 at every chunk
  boundary.
- Config law: the personal section follows the BrowserConfig pattern (public
  struct + yaml mirror + parse branch + `*Resolved()` in
  `internal/project/config.go`); configserver `ConfigDoc` gains the section,
  config.html gains the card, carry-forward keeps sections the form doesn't
  own; harness-config's `references/config-reference.md` updated in the same
  commit — unknown keys are silently ignored, so a doc lag is how dead
  settings happen.
- Vault law: kanban owns its DDL + `EnsureKanban` on the shared handle; no
  ALTERs of core tables; a1.db is never hand-edited.
- Build law: dist artifacts are committed (`internal/webserver/dist` via
  go:embed in render.go) — every UI chunk ends with `cd web && bun run build`
  and commits the rebuilt dist.
- Definition of done per chunk: go build, go test on touched packages, vet,
  trio gate green, dist rebuilt when UI moved, one CHANGELOG Unreleased entry.

### Chunk 0 — groundwork: personal config, the gate, the status endpoint (LANDED 2026-11-05)

- [x] `internal/project/config.go`: `PersonalConfig{Enabled *bool;
      CalendarDir, MailDir string}` + mirror struct + parse branch +
      `EnabledResolved()`; paths TrimSpace'd and ~-expanded (`expandHome`,
      a leading ~ only — no ~user lookups, home-dir failure degrades to the
      raw path). Landed: also `personal_read`/`personal_write` on
      modelEntry → `llm.ModelConfig.PersonalRead/PersonalWrite` (the
      ruled-in per-model guardrail).
- [x] configserver: `PersonalDoc` on ConfigDoc; "Personal data" section card
      in config.html after the Web GUI panel (master checkbox + conditional
      calendar_dir/mail_dir fields, EN/ZH strings), and per-model
      personal_read/personal_write checkboxes on every model card in the
      exact thinking/image tri-state touched pattern; round-trip test
      `config_personal_test.go` beside `config_jev_test.go`.
- [x] webserver: `GET /api/personal/status` → `{enabled, calendarDir,
      mailDir}` (ungated, reads the live config — a config save hot-reloads
      it) + the `personalGate` helper (403 personal-data-disabled);
      `internal/webserver/personalapi.go`, chunk 2-4 endpoints compose it.
- [x] Trio: `internal/project/personal_config_test.go` (4 positive incl.
      explicit-off + per-model flags, type-garbage negative,
      expandHome-vs-hand-written-grid differential),
      `internal/configserver/config_personal_test.go` (round-trips,
      type-garbage 400, golden-block untouched-save differential),
      `internal/webserver/personal_test.go` (status + gate pass/block,
      gate-matches-resolver differential over every section spelling).
      harness-config's SKILL.md Map + config-reference.md teach the section
      in the same commit.

### Chunk 1 — dashboard shell + pinned sessions + usage heatmap (LANDED 2026-11-07)

- [x] `Dashboard.tsx` replaces the welcome branch: hero (bigger dodec), widget
      grid on the paper-card tokens, docked bottom InputBar (bigger icon, per
      the ask), quick actions (settings via `window.open('/settings')`, file
      browser hook stub for chunk 5). Landed: the composer dock is the old
      welcome flow verbatim (`selectSession('')` + send — the chat view takes
      over, no new routing); the heatmap scrolls to its newest weeks on mount
      so today is visible without dragging; the personal trio renders honest
      locked/coming states off `/api/personal/status` with zero gated
      fetches.
- [x] Pinned sessions widget: `fetchPins` + `fetchSessions`, pinned-first,
      click resumes via `selectSession(id)`; the star toggle already ships
      (pins.json + `/api/pins`, modeapi.go:164/173) — the widget is pure
      composition, zero backend.
- [x] Usage tracker: `GET /api/usage/daily?days=371` → per-day {sessions,
      messages, tool_calls, tokens} — sessions from `sessions.created_at`,
      tool calls from `tool_runs.at`; messages/tokens from
      `session_entries` payload via `json_extract` where
      kind='EntryMessage' (JSON1 verified in the modernc build). Landed
      with two correctness rulings the plan had left open: every day keys
      on the UTC day via SQLite `date()` — entry timestamps marshal with
      LOCAL offsets, so the planned `substr(at,1,10)` would mis-bucket a
      +01:00 entry onto the wrong day; and every `json_extract` sits
      behind a `CASE WHEN json_valid()` guard — json_extract RAISES on
      malformed JSON, and a corrupt payload row must be skipped, never
      fatal. The naive Go scan ships as the runtime fallback (not just
      the test oracle). 60s in-memory TTL cache. UI: 53×7 seal-tone
      heatmap, hover tooltip + selected-day readout, click filters the
      session list to that day via the row's new `data-mtime` ISO
      attribute (render.go).
- [x] Placeholder cards for calendar/kanban/mail with honest locked/coming
      states — no dead fetches.
- [x] Trio: webserver usage handler — differential: SQL aggregate vs a naive
      Go scan over a fixture vault with known entries (the flagship oracle;
      fixture seeds non-UTC entry timestamps, tool-role rows, runtime
      riders, a corrupt payload, out-of-window rows, a NULL tool timestamp,
      across 7 window sizes); negative: hostile days (0, -5, "abc", absent,
      4001), corrupt payload rows skipped not fatal, empty vault ⇒ zeros,
      degraded (nil) vault ⇒ zeros.

### Chunk 2 — calendar (.ics from a local dir) (LANDED 2026-11-08)

- [x] `internal/calendar`: RFC 5545 subset — line unfolding, VEVENT blocks,
      DTSTART/DTEND (DATE vs DATE-TIME, Z vs TZID → time.LoadLocation,
      floating ⇒ server local), all-day, SUMMARY/LOCATION/DESCRIPTION
      unescaping; `Scan(dir)` with an mtime memo + per-file size cap;
      malformed lines skip-with-count, never fatal. Landed with the write
      side the agent tool needs: Create (one-event files, atomic
      temp+rename) and Update (surgical property-line patching inside the
      matching VEVENT — RRULE/VALARM/X- lines and other events preserved
      byte-for-byte, DTSTAMP bumped, result re-parsed to verify itself).
- [x] Endpoints (gated): `GET /api/personal/calendar?days=N` (agenda asc),
      `/range?from&to` for the month grid. Hostile inputs follow the usage
      endpoint's doctrine (fallback/clamp, never 5xx); both are READ-ONLY —
      the write face is the agent tool's.
- [x] Widget: agenda card (next 14 days, today highlighted, all-day badge,
      location subtitle); month grid as a stretch. Locked / not-configured
      states read off /api/personal/status with zero gated fetches; a mid-
      flight 403 folds back to locked.
- [x] Trio: parser differential over golden testdata .ics (DST crossings,
      unknown TZID ⇒ UTC + warning field, folded lines, all-day boundaries);
      handler trio (gate off, dir missing, dir emptied at runtime, hostile
      dir path) + endpoint-vs-library differential. Plus the calendartool
      trio (call-time flag refusals; facade-vs-library parity) and
      permission tests for the new ActionPersonal class.
- [x] Agent tool `personal_calendar` (chunk 0's flags gate it): agenda /
      range reads when the model has personal_read; create/update .ics
      events when personal_write — the flags are enforced at the tool call,
      not just at mount, so a mis-mounted tool still cannot write. Writes
      clear a new ActionPersonal permission class: consented by the config
      opt-ins (no per-call prompt in interactive mode), denied in readonly.
      A config save rebinds live web engines (RefreshPersonalTools) so the
      switch flips mounts on the next turn of an open chat.

### Chunk 3 — kanban (vault-backed board) (LANDED 2026-11-08)

- [x] `internal/kanban`: `kanban_cards` DDL (id TEXT PK, col TEXT CHECK'd to
      the three columns, position INTEGER, title, notes, timestamps) +
      `EnsureKanban(v.DB())`; List/Add/Move/Edit/Delete; Move renumbers the
      target column in one transaction.
- [x] Endpoints (gated): GET/POST `/api/personal/kanban`, PUT
      `/api/personal/kanban/card` (move + edit), DELETE `…/card?id=`.
- [x] Widget: three columns, HTML5 drag & drop, inline add, edit via the
      existing Modal component, two-step delete confirm (the session-delete
      UX precedent).
- [x] Trio: store — differential: position-invariant oracle (after any op
      sequence, positions are dense 0..n-1 per column, cross-checked against
      a naive re-sort); negative: unknown column, duplicate id, nil vault,
      hostile positions. Plus the kanbantool trio (call-time refusals,
      facade-vs-library parity) and permission/engine mount tests.
- [x] Agent tool `personal_kanban`: list when personal_read; add/move/edit/
      delete cards when personal_write — same enforcement shape as the
      calendar tool.

### Chunk 4 — mail (Maildir, read-only mini client) (LANDED 2026-11-08)

- [x] `internal/maildir`: mailboxes = subdirs with cur/new (tmp ignored);
      `List(box, limit)` = latest-N headers (Maildir `:2,:S` filename flags +
      net/mail for From/Subject/Date — RFC2047 comes free with the stdlib);
      `Open(box, id)` = body, text/plain preferred, text/html stripped to
      text v1 (no remote content rendering, ever — privacy ruling); size
      caps. Landed with the rules the plan left implicit, all pinned by the
      trio: dotfiles in cur/new are mailbox metadata, never messages
      (.keep placeholders); a header block that runs past the 128 KiB cap
      without a blank line is a runaway disk dump — skipped with a count,
      never slurped (and unaddressable by id: what List refuses to show is
      not a message); a file past the 8 MiB whole-file cap answers
      headers-only with Truncated set, never a body cut mid-sentence; a
      missing Date header falls back to the file's mtime so ordering stays
      total; and `Summary(box, id)` is the envelope-only read the agent
      facade is built on.
- [x] Endpoints (gated): `/api/personal/mail/mailboxes`,
      `/mail/messages?box&limit`, `/mail/message?box&id`; per-request scan
      with an mtime memo — no vault persistence until profiling demands it.
      limit follows the usage doctrine (unparsable/non-positive ⇒ 50, clamp
      200); traversal-shaped names are 400s, missing boxes/messages are
      404s, an unconfigured root is configured:false, and a degraded box
      answers 200 with honest stats.
- [x] Widget: mailbox rail + message list + read pane; dashboard card holds
      the latest preview and expands into the overlay for the three-pane
      layout. The card prefers the box named inbox (falling back to the
      rail's first), shows unread badges and the envelope flags; the reader
      overlay is rail | list | read with the degraded note, the truncated
      refusal note, and locked / not-configured states with zero gated
      fetches (a mid-flight 403 folds back to locked).
- [x] Trio: differential vs a golden fixture Maildir tree (flags, colons in
      filenames, missing Date header, encoded subjects); negative: missing
      root/box, path-traversal box names rejected, oversized body refused
      with header-only fallback. The differential side adds hand-computed
      UTC literals + FixedZone constructions for every dated fixture
      message, the limit-prefix law (every limit is a prefix of the full
      latest-first order), and a seeded 20-tree generator whose plan is the
      oracle (boxes, flags, ties, no-Date mtimes, junk, dotfiles, subdirs,
      tmp decoys, new/-only boxes, empty boxes) — model versus
      implementation, field by field. Plus the mailtool and handler trios.
- [x] Agent tool `personal_mail`: mailboxes / messages / headers reads when
      personal_read; NO write surface exists even with personal_write —
      the Maildir is read-only by ruling, for the model exactly as for the
      webui. The ruling is structural: Deps has no write hook at all, and
      one privacy line goes further — the model reads the ENVELOPE, never
      the letter (Summary, not Open; bodies are a webui privilege). All
      three actions extract as ordinary ActionRead (unknown actions ride
      ActionPersonal so readonly still denies them).

### Chunk 5 — file browser + split screen (LANDED 2026-11-08)

- [x] `internal/webserver/filesapi.go`: `GET /api/files/list?path=` (name /
      dir / symlink target / size / mtime; empty path ⇒ server cwd — "current
      directory" per the ask), `GET /api/files/read?path=` (utf-8 sniff,
      NUL-byte binary refusal, 2MB preview cap, language by extension),
      `PUT /api/files/write {path, content}` (guard()ed, symlink-refusing,
      readonly-mode-refusing at write time via /api/mode's mode source; no
      implicit mkdir -p). Landed with the rulings the plan left implicit,
      all pinned by the trio: dir means NAVIGABILITY not dirent type (a
      symlink to a directory lists as a dir AND carries its target — broken
      ones keep the target and list as plain rows); size/mtime are the
      entry's own lstat values (ls -l semantics); the preview cap is
      REFUSED not truncated (exactly 2 MiB passes, one byte over is a 413
      — a half file presented as the file is the one lie a viewer cannot
      tell); the write preserves an existing file's permissions (editing a
      script never strips its execute bit) via atomic temp+rename in the
      target directory; and the kill-switch is A1_FILES (the A1_MCP
      precedent: trimmed, lowercased, "0/false/off/no", 503 on reads too).
- [x] SPA: split-screen state + header button + dashboard quick action; the
      right pane is the FileBrowser: absolute-path address bar (full-system
      navigation per the ask), breadcrumbs, dirs-first listing; click →
      highlight.js preview; Edit → textarea + Save + dirty indicator + error
      toasts. The pane is a sibling of main, not an overlay (it stays up
      while the user chats); below lg it covers the screen. highlight.js is
      vendored the sparse way — core + exactly the languages the server's
      extension map rules (tsx/jsx/toml alias to TS/JS/ini, the honest best
      available), and the token theme is derived from the palette's own
      channel variables so light and dark both hold with one rule set.
      Leaving with unsaved edits is refused by disabled buttons + a toast,
      not a modal.
- [x] Trio: write→read byte-fidelity round-trip and list vs os.ReadDir oracle;
      negative: binary/oversized preview refused, readonly-mode write refused,
      symlink write refused, unreadable dir, `A1_FILES=off`. The differential
      earned its keep on landing day: it caught the handler's sort comparator
      written backwards (files first, dirs last) — the oracle expected dirs
      first and the diff flipped.

### Chunk 6 — polish pass + RRULE + docs sweep (LANDED 2026-11-08)

- [x] `internal/calendar/recur.go`: RRULE common subset (DAILY/WEEKLY/
      MONTHLY, BYDAY, COUNT/UNTIL/INTERVAL) with hand-computed golden
      differentials — full RFC compliance is NOT claimed and the doc says
      so. Landed with the semantics the RFC leaves room for pinned in
      recur.go's doc: occurrences generate from the rule restricted to
      ≥ DTSTART (a non-matching anchor is not force-included), COUNT
      counts the whole series from the first match (a window that opens
      mid-series consumes the earlier occurrences against the budget),
      UNTIL is inclusive in both spellings (DATE-form bounds a timed
      DTSTART at the whole last day), expansion happens in DTSTART's own
      zone (09:00 wall clock survives the DST boundary — pinned), and
      WKST is fixed at MO. BYDAY under DAILY is a filter, under WEEKLY
      the day set, under MONTHLY the ordinal forms (2TU, -1FR, plain
      every-Friday). Anything else (YEARLY, BYSETPOS, BYMONTH, RDATE,
      WKST≠MO, COUNT+UNTIL together) degrades to the first occurrence
      with a Warnings entry — the honest subset boundary. En route the
      sweep found and fixed a real overlap bug: a zero-End all-day event
      compared end==start==from and FAILED end.After(from), so an
      all-day event on the window's FIRST day vanished from its own
      agenda — the filter now spans [its day, next midnight) as the doc
      always claimed (master and occurrences share the one helper).
      Trio: recur_test.go — the hand-computed golden table (every
      expectation worked out from the 2026 calendar: the second
      Tuesdays, the last Fridays, the Mondays, the 31st skipping short
      months, COUNT-from-DTSTART mid-series windows, UNTIL boundary
      seconds, DST wall-clock preservation across America/New_York's
      fall-back), ParseRule refusals (21 hostile rules), Agenda
      degradation negatives, the step cap (a daily series anchored in
      the year 1300 yields nothing, bounded time), and the memo
      stability pin. Downstream pins flipped with the behavior: the
      calendar trio's agenda membership (the weekly golden event now
      expands 4×/2× across its windows) and the webserver fixture count
      (the 14-day window sees the recur anchor plus its +7d occurrence;
      the range window's midnight to-bound excludes the second).
- [x] Locked-state sweep across all personal widgets (status.enabled=false
      renders locked cards, fires zero gated fetches); responsive (widgets
      single-column on mobile; split screen becomes a full overlay); light +
      dark themes verified. Browser-verified on all axes: personal off
      renders three locked cards with exactly ONE /api/personal/* request
      on the wire (the ungated status — zero gated fetches, server-side
      403s confirmed by direct curl); 375px shows the new off-canvas
      session drawer (hamburger bar, scrim, Escape, pick-to-close — main
      is full-width 375px, no more 135px pinch), a single-column widget
      grid, and the file browser covering the whole screen; dark and
      light round-trip on body palette AND hljs token colors (one rule
      set, channel-variable derived); RRULE verified through the real
      widget (Weekly sync ×2, all-day Monday standup ×2, Sprint block
      DAILY;COUNT=4 ×4, the unsupported YEARLY degraded ×1, nine Repeat
      icons — every occurrence marked). The drawer is chunk 6's one new
      frontend surface: below lg the sidebar slides over main (fixed,
      z-50, shadow, translate-x transition), above lg it is the static
      column and the state is inert.
- [x] doc/dashboard.md design note; README feature list; CHANGELOG
      per-chunk entries reconciled; config-reference.md re-verified against
      config.go. dashboard.md documents the epic's rulings (the surface
      map, the write-permission ladder calendar/kanban/mail/files, the
      honest states, the RRULE subset claim, the responsive sweep, the
      chunk ledger). README's Web GUI paragraph (en + zh-CN) now carries
      the dashboard, the personal trio, the split screen, and the
      responsive layout. CHANGELOG gained the chunk 3 (kanban) and
      chunk 4 (mail) entries the sweep found missing. config-reference
      cross-checked key-by-key against config.go's fileConfig — all 14
      top-level blocks documented, no drift, no edit needed.
- [x] Full gate: go test ./..., -race, vet, trio gate, bun build + dist
      commit. 145 ok / 0 fail, -race 145 ok / 0 races, vet clean, trio
      gate 146/146, dist rebuilt and committed, browser-verified end to
      end (zero console errors across every phase).

### Chunk 7 — the webUI tightening pass (LANDED 2026-11-08)

The user's own screenshots drove this one, item by item:

- **The sidebar overlap** — the drawer floated at z-50, above the z-40
  modal layer, so open dialogs ran underneath it. The z-ladder is now
  page < edge zone (z-20) < drawer (z-30) < modals (z-40) < toasts
  (z-50), and the drawer is collapsed by default at EVERY viewport —
  it opens from a left-edge hover zone (a quiet pill brightens as the
  pointer lands), closes on pick / X / Escape / pointer-leave with a
  300ms grace so crossing the gap never flickers, and slides on an
  ease-out-quint 260ms. Below lg the hamburger + scrim remain the
  touch-friendly exits.
- **The second dodecahedron** beside the composer is gone — the hero is
  the one brand mark, and the docked composer takes its full width.
- **The file-browser toggle** moved from the (now-collapsed) sidebar
  header into the composer, left of the send button — pressed-state
  styling and aria-pressed intact.
- **The heatmap autoselects today** — the newest cell wears the cinnabar
  ring on load and the day-filter readout opens filled with the day's
  sessions instead of leaving the card floor blank.
- **The calendar is the ask's month grid** — ‹ month ›  navigation with
  a today jump, every day a button (event dots, +n overflow), the
  selected day's events listed under the grid, one
  `/api/personal/calendar/range` fetch per visible month, today
  autoselected on the current month. Locked / not-configured / failed /
  degraded states all carry over unchanged.
- **Highlighted editing** — the file browser's editor now colors while
  you type (the classic overlay: a live-highlighted pre under a
  transparent-text textarea, identical metrics, scroll-locked,
  pointer-events-none), capped at 256 kB past which it says so honestly
  and edits plain.
- **Multi-account mail** — `personal.mail_accounts` mixes local Maildirs
  and remote IMAP servers (name, type, root | host/port/username/
  password/tls/mailbox); the legacy `mail_dir` resolves as the "local"
  account so existing configs do not move. New `internal/imapmail`
  (emersion/go-imap v2) is read-only to the bone: read-only SELECTs,
  BODY.PEEK fetches, one stateless connection per call, 10s/30s
  timeouts, UID as the message id. Both transports return the maildir
  package's own types, so the web handlers and the widget carry ONE
  wire shape and the semantics cannot drift. The rail answers 200 with
  a per-account error (one dead server renders its outage inline, the
  others stay clickable); list/read answer 502 for a dead or rejecting
  server, 404 for a missing box/message, 400 for unknown accounts and
  malformed ids. The agent facade is unchanged (envelopes only, legacy
  mail_dir) — the docs say so.

Tests: the imapmail trio (a real in-process emersion imapserver as the
fixture, hand-written golden letters, and the flagship differential —
the SAME letter set through both transports, disk vs wire, entry-by-
entry incl. bodies), the account-resolution trio in project (defaults,
de-dup, the legacy-vs-modern differential), and the webserver
multi-account suite (default rail, per-account selection, the outage
that renders instead of blanking, unknown-account 400s, dead-server
502s). Full gates: build PASS, vet PASS, go test ./... all green
(146 ok / 0 fail), -race all green, trio gate 147/147 (+imapmail),
dist rebuilt and committed, browser-verified end to end on the user's
own list (single dodec, today selected on the heatmap, month-grid day
clicks listing their events, three account pills with the dead-server
outage inline and per-account rails in the reader, the composer's pane
toggle open/close, the highlighted editor screenshot-verified, drawer
open/close on edge hover, drawer z=30 under modal z=40, zero console
errors).

### Anti-goals (permanent for this epic)

- No online WRITES to personal data and no sync: no CalDAV, no SMTP, no
  flags pushed back — reads went online in chunk 7 (IMAP, by the user's
  ask) and stay read-only to the bone; the calendar is a directory of
  .ics files, mail accounts are read-only Maildirs and IMAP boxes.
- No HTML email with remote content; no sending mail in v1.
- No per-widget permission grants; the one switch governs all of it.
- No agent autonomy without opt-in: personal tools mount only when the
  master switch is on AND the per-model personal_read (and personal_write
  for writes) checkboxes are checked; absent flags mean locked. Mail DATA
  (messages, flags) has no write surface at any flag setting — the one
  mail write anywhere is the web's account-list configurator (chunk 8),
  which no agent flag can reach.
- No new JS frameworks; Preact + vendored highlight.js is the ceiling.

### Chunk 8 — the mail configurator (LANDED 2026-11-09)

User directive: "still cant configure email servers/accounts and app
passwords- but that should be easily fixable." Chunk 7's multi-account
mail was config-file driven; this chunk is the configurator itself.

- [x] **The ruling: the keyring, not the letters.** Chunk 4's "nobody
      writes here" governs MAIL — messages, flags — and stands untouched
      (BODY.PEEK, read-only SELECTs, no handler that could send, move, or
      mark). The account LIST is configuration, the same object the
      /settings editor edits; the configurator writes it so the user
      stops hand-editing YAML for it. Write ladder for mail: config yes
      (web only), data never (anywhere, any flag).
- [x] **The credential contract — the wire never carries a password it
      did not have to.** GET /api/personal/mail/accounts returns
      hasPassword, never the password (an editor that cannot show a
      secret cannot leak one). PUT entries carry password (set) or
      keepPassword (preserve), resolved by name for named accounts and
      by type-and-server fingerprint for unnamed ones, landing only on a
      UNIQUE match — row edits and deletions cannot shuffle credentials
      onto the wrong account the way an index-anchored keep could; the
      cost is stated in the UI (renaming does not carry the password).
      The browser pass caught the unnamed collision for real: two
      accounts saved without names both key on "", and the first-match
      rule handed the IMAP account the maildir's empty password — the
      fingerprint rule and its ambiguity-refusal are pinned by
      TestMailAccountsUnnamedKeepFingerprint_Negative. The test dial takes
      the same single-spec body with the same keep resolution, because
      "test the saved account without retyping its app password" is the
      honest test — and it persists NOTHING.
- [x] **Endpoints.** GET accounts (the read model: entries sans
      passwords, the legacy mail_dir as a pinned read-only row, the
      resolved pills the rail serves), PUT accounts (whole-list
      replacement, personalGate(guard()) — the kanban's composition; on
      success onConfigSaved() reloads the project config and rebinds live
      engines' personal tools, so the new account is on the rail and in
      the agent's facade without a restart), POST test (one spec, 10s
      timeout, dead server = 200 ok:false — the answer the button exists
      to fetch). Validation is deliberately stricter than the read
      path's lenient degrade: the configurator refuses known-bad shapes
      (unknown type, imap sans host, maildir sans root, port out of
      range, >32 accounts) with a 400 — hand-edited YAML keeps its
      lenient degrade; this surface saves only what it knows is good.
- [x] **project.SetMailAccounts** — the surgical YAML write
      (modifyConfigYAML): sparse keys, wholesale sequence replacement,
      empty save prunes the sequence and the personal block only when it
      empties (SetMaxToolCalls' discipline); enabled/calendar_dir/
      mail_dir are not this call's to move and survive untouched.
      Passwords land as plain scalars beside the model api_keys — the
      config file is already the project's secret store. expandHome was
      exported (one "~ means home" rule on both sides of the wire).
- [x] **The editor** (web/src/components/MailAccountsEditor.tsx): rows
      with edit/remove, Add account (IMAP server | Local Maildir
      segmented), the app-password field (type password + reveal, blank
      + saved = keep), the Test connection button with inline verdict
      ("Reached it — 3 mailboxes (…)" / the server's own error), Save =
      one PUT of the whole list, saved-flash + the dashboard refetches
      behind the modal (mailVersion). Doors: the card's not-configured
      state (primary action is now "Add mail account"), the card header
      gear, "Manage accounts" at the rail's foot inside the reader, and
      the command palette. The legacy mail_dir renders as a pinned
      read-only row.

Tests: the project trio (SetMailAccounts write→parse round-trip
differential against loadConfig, wholesale replacement, empty-save
pruning with siblings kept), the webserver trio (list+save+keep positive
with the live-reload proof through the rail, the test-dial positive with
a fake IMAP listener that records the LOGIN line — the keep rule's
server-side merge proven ON THE WIRE, the gate/guard/validation
negatives, and the endpoint-vs-file differential: the test's own YAML
parse of config.yaml compared entry-by-entry with the GET, plus the
resolved pills vs the rail's account list — two endpoints, one truth).
Full gates: build PASS, vet PASS, go test ./... all green (146 ok /
0 fail), -race all green (146 ok / 0 races), trio gate 147/147, dist
rebuilt and committed, browser-verified end to end (add-account form,
maildir save landing on disk and the card refetching to the new account
live, the dead-server test verdict, the keep-password flow through a
saved account, zero console errors).

## Active: bug-kill — every find from the test-trio audit fixed (2026-11-05) — LANDED, AWAITING USER VERIFY

User directive: "alright lets kill those bugs we found." All seventeen
found-along-the-way bugs from the W1–W4 trio waves, plus four same-class
siblings found while fixing, are dead: production code fixed, the pins that
documented each bug flipped to assert the fixed behavior, full gate re-run
(go test ./... = 139 ok / 0 fail; -race = 139 ok / 0 races; vet clean;
trio-coverage gate exits 0 — the trio survived every pin flip). Details per
fix in CHANGELOG Unreleased → Fixed; the wave notes below now carry
"FIXED 2026-11-05" markers.

- [x] **engineaccess nil clear + agent typed-nil forwarding.** Registry
      stores through an engineBox (nil representable); agent.SetActiveEngine
      forwards a literal nil on the clear path. Pins flipped: "nil store
      panics" → "nil clears"; differential extended to oracle-check the
      literal-nil clear. Collateral: permissiontool trio comments updated,
      its cleanup clears properly now.
- [x] **graphtool imported_by + overview.** reverseKeys expands a target
      into every reverse-index key spelling (file, package dir,
      module-prefixed); both directions union/retry accordingly.
      renderOverview counts through the same expansion (was always zero on
      Go modules). Differential extended with file-path and bare-dir
      aliases; overview pin flipped to expect "base/base.go (2 importers)".
- [x] **project.TryDefaultProject.** Non-panicking lazy discovery;
      snapshottool, graphtool, and batchtool (same unreachable guard found
      in the sweep) use it. UnusableHome pins flipped from Panics to the
      tool errors. GetDefaultProject keeps the boot-surface contract.
- [x] **chat cursor clamps.** LineStart/LineEnd clamp negative offsets;
      ReplaceRange clamps both bounds before the swap. Oracles extended to
      negative offsets; both-negative / negative-end / start-past-end cases
      pinned. Composer's "currently panics" caveat unpinned — hostile
      carets (999, -9) pinned from the composer side.
- [x] **status negative Frame.** modNonNeg at Draw, Glyph, and
      ForEachFlowCell's head; -1/-11 pins wrap to the last frame.
- [x] **transcript nil entries.** Skipped at every dereference site (Draw
      viewport loop, both Handle loops); copy sites verified nil-safe.
      NotPanics pins.
- [x] **splash display columns.** xui.StringWidth(WidthUnicode); the
      differential's width assertion switched to columns plus a
      greedy-packing check (a break is only legal when the next word
      doesn't fit by columns).
- [x] **depstool grouped imports.** go/parser (ImportsOnly) with the regex
      as malformed-file fallback; aliased/blank/dot imports seen. The
      deps_test dot-import pin that only passed by regex blindness
      re-anchored on a true relative import.
- [x] **deadcode + doctool + apitool parseExports → AST.** Top-level
      FuncDecl/GenDecl names, grouped var/const members individually, kind
      from the group keyword, regex fallback. Raw-string decoys stay
      strings. apitool was a sibling find (same code shape) — fixed in the
      adjudication pass with its own fixture pins.
- [x] **apitool comment mentions.** util.CountGoReferences strips comments
      via go/scanner before matching (strings keep their contents;
      per-line count-once semantics unchanged); scanner failure falls back
      to raw matching.
- [x] **nplusone db.* shapes.** db.Find/First/Where/Take/Last/Raw/Create/
      Save/Update/Delete added to the alternation; db.Find classifies
      "gorm".
- [x] **ranktool double-star globs.** Single-segment patterns keep exact old
      base-name behavior; path-shaped globs match the root-relative slash
      path with ** = zero-or-more segments. Golden differential cases for
      **/*.go, cmd/*.go, sub/**; doc example is now true.
- [x] **readtool progress guarantee.** First-line-over-cap hard-splits
      (prefix + marker, Next offset advances past the line); mid-stream
      over-long lines keep the old notice (the re-read hard-splits). Oracle
      mirrored; 60 KiB first-line differential case added.
- [x] **openai typed Error events.** Both providers (chat completions +
      responses) yield the typed event on mid-stream parse failures,
      matching anthropic/gemini; pre-stream failures stay zero-value
      (all providers' shape). The responses provider's own "error" /
      "response.failed" events had the same bug — fixed in the adjudication
      pass with a three-case negative test.
- [x] **anthropic whitespace-only system skip.** Whitespace-only
      RoleSystem messages skipped; block gated on trimmed content; real
      content byte-identical (goldens + new equality subtests).
- [x] **memstore mood override costs the weakest slot.** Replacement scans
      from the end; bundle keeps descending-activation order, top scorer
      retained. Reference oracle flipped; differential re-pinned.
- [x] **tokentool determinism.** Base weights declared as an ordered
      slice (extras name-sorted); sort tie-breaks on tool name. Tie
      caveats became golden tables + a 5-call determinism pin.
- [x] Verification: whole repo builds; vet clean; 139 ok / 0 fail;
      -race 139 ok / 0 races; trio-coverage gate green. ~48 files across
      21 packages; the only formatting-only diffs are gofmt reindents of
      files that were space-indented at baseline.

User gates: `make test` on the real tree; live spot-checks worth an eye —
(1) `graph imported_by internal/auth/auth.go` in a session returns
importers; (2) reading a file with a >50 KiB line advances on the hint;
(3) the splash hint with CJK wraps at the visual margin; (4) an OpenAI
stream drop surfaces as a typed error in the transcript.

## LANDED: test trio — every package gets positive + negative + differential (2026-11-03 → 2026-11-05)

Goal ("this should catch 99% of bugs"): every package in the module carries at
least one test of each category, so the suite exercises happy paths, failure
paths, and implementation-vs-oracle agreement. Audit of all 140 packages:

- 22 already pass the bar.
- 11 have zero tests (need the full trio).
- 36 need negative + differential.
- 7 need negative only.
- 64 need differential only.
- Total ≈ 176 new test functions.

### Definitions (binding for this effort)

- **Positive** — valid input, expected behavior asserted. Existing happy-path
  tests count; new `_Positive` functions only for untested packages.
- **Negative** — invalid, hostile, or boundary input, and the package's
  documented failure mode is asserted: returned `error`, tool result carrying
  an error message, rejected write, graceful fallback. No panic, no silent
  partial state.
- **Differential** — the implementation is checked against an *independent*
  oracle, named in a `// oracle:` comment on the test:
  - (a) naive reference implementation written in the test (O(n²) loop, string
    building) compared on randomized + edge-case inputs;
  - (b) stdlib/known-good equivalence (`encoding/json`, `filepath.Walk`,
    `regexp`, `strings`…) on the same inputs;
  - (c) round-trip through genuinely distinct code paths (our encode vs
    `encoding/json` decode);
  - (d) golden literal — expected output hand-written in the test, not
    regenerated by the code under test (rendered `View()` strings for widgets).
- Banned as "differential": `f(x)` compared to `f(x)` (tautology), expected
  values computed by calling the same function under test.

### Conventions

- New tests live in one file per package: `trio_test.go` (only missing
  categories are added). Names end `_Positive` / `_Negative` / `_Differential`.
- testify (`assert`/`require`) only, per AGENTS.md. No new module deps.
- `scripts/test-trio-coverage.sh` reports per-package P/N/D status and exits 1
  on a gap — the gate that proves this item done.
- Exemptions require a one-line rationale in the coverage report (e.g.
  `cmd/jev` if it stays an empty placeholder package; `version` negative —
  data-only constant, no failure surface — its README-parity check is the
  differential).

### Waves (disjoint package sets, parallelizable)

- [x] **W1 — zero-test packages, full trios (11)**: cmd/jev*, cmd/toolaudit,
      internal/components/layout, internal/debuglog, internal/engineaccess,
      internal/tools/{batchtool,graphtool,memtool,permissiontool,vectortool},
      internal/version* — DONE 2026-11-03, all green. Found along the way
      (both FIXED 2026-11-05 — see the bug-kill section):
      `engineaccess.SetActiveEngine(nil)` panics though its doc says "pass nil
      to clear" (production code calls it with a typed nil instead);
      graphtool `imported_by` is keyed by raw import path, so file-path
      queries never match on Go modules.
- [x] **W2 — components/ + tui/ gaps (27)**: N+D: components{,/app,/branchlist,
      /chat,/chrome,/codeview,/diffview,/listpicker,/mention,/palette,
      /sessionlist,/splash,/status,/toast,/transcript,/tree}, tui/{codepane,
      diffpane,editor,footer,overlays,pathutil,transcript}; N-only:
      tui/{composer,submit}; D-only: components/{block,text} — DONE
      2026-11-03, all green. Notable pins (all four FIXED 2026-11-05 — see
      the bug-kill section): chat.LineStart/LineEnd and
      ReplaceRange can panic on out-of-range cursors (invariant maintained
      only by callers); status.Spinner panics on negative Frame;
      transcript panics on a nil Widget intersecting the viewport;
      splash.wrapHint measures bytes not display columns (CJK wraps early).
- [x] **W3 — tools/ gaps (48)**: N+D: runtimetool, skilltool, snapshottool;
      N-only: tooldef; D-only: the other 44 tool packages — DONE 2026-11-03,
      all green (full ./internal/tools/... tree ok). Notable finds:
      depstool's import regex misses grouped `import (...)` blocks entirely;
      deadcode/doctool regex parsing sees declarations inside raw strings and
      misses grouped var/const; nplusonetool misses idiomatic `db.Find(&x)`;
      apitool counts comment mentions as references; ranktool's documented
      `**/*.go` glob example can never match (base-name matching);
      readtool's byte-cap hint can loop forever if one line exceeds 50 KiB;
      snapshottool's "project config not loaded" guard is unreachable
      (GetDefaultProject panics instead). All FIXED 2026-11-05 — see the
      bug-kill section.
- [x] **W4 — llm/ + top-level internal + util/ (rest)**: N+D: llm/{anthropic,
      openai}, memfsck, memstore, project/model, toolguard, contextbreakdown,
      graph, util/{githubrelease,update}; N-only: configserver, skillstats,
      telemetry, vault; D-only: agent/prompt, bw, evidence, llm, llm/{gemini,
      jev,openai/responses,skills}, mcp, snapshot, toolmanager, tools,
      util/{archive,clipboard,diffreview,filesearch,gitx,image} — DONE
      2026-11-05, all green (one agent was killed by a sandbox restart
      mid-wave; memfsck/memstore recovered from its files, project/model and
      toolguard re-run). Notable pins/finds (all four FIXED 2026-11-05 —
      see the bug-kill section): anthropic vs openai mid-stream
      failure shapes diverge (zero-value event + iter error vs a typed Error
      event — consumers branching on ev.Type miss openai deaths); provider
      system-message join/skip semantics differ (anthropic keeps
      whitespace-only system messages, which can pin a cache breakpoint);
      memstore's mood-congruency override replaces the FIRST non-positive
      slot positionally (the raw top scorer is displaced, the injected trace
      renders at rank 0); tokentool allocations are nondeterministic under
      tied weights (map-range order + unstable sort — differential is
      tie-aware; a name secondary key would make it reproducible).
- [x] **W5 — verification**: `go test ./...` = 139 ok / 0 fail (cmd/jev
      exempt, no files); `go test -race ./...` = 139 ok / 0 races;
      `go vet ./...` clean; every new trio_test.go gofmt-clean;
      `scripts/test-trio-coverage.sh` exits 0 ("all packages carry the test
      trio"). golangci-lint/gofumpt are not installed in this sandbox —
      `make lint` / `make fmt-check` not run (fmt-check is red at baseline
      per AGENTS.md anyway).

**DONE 2026-11-05.** Final state: 140 packages, 2 exemptions on file
(cmd/jev — empty placeholder; internal/version negative — data-only
constant), ~190 new test functions across ~115 trio_test.go files plus
additions to judgetool_test.go and tokentool's suite. The gate
(`scripts/test-trio-coverage.sh`) enforces the trio going forward.

### Differential oracles per area (guidance, not straitjacket)

- Tool packages: run the tool, compare against direct stdlib/filesystem
  equivalents computed in the test (e.g. findtool vs `filepath.Walk`+pattern
  match, greptool vs `regexp` over the same corpus, tokentool vs a char/word
  count reference).
- LLM providers: request-building vs hand-written known-good JSON literals;
  stream decoding vs `encoding/json` decode of the same events.
- Widgets/components: `View()` output for a fixed state vs golden literal.
- Algorithm packages (memstore, graph, bw): optimized path vs naive reference
  on randomized inputs, property-style.

## Active: JEV extensions — file-ops scale, compaction boundary, bash risk guard (2026-11-02) — LANDED, AWAITING USER VERIFY

User question: which of the sketched JEV extension concepts were worth
building (b risk gating, c routing + context management, d scale file
operations). Plan approved as scoped ("I really like your suggestions on
all of them — lets do it!"): d in full, c's compaction half only
(intent→cheapest-model routing dropped — wrong tool for a classifier),
b last under a fail-closed contract. All three landed:

- **(d) judgetool files mode** — `judge` with a `files` glob classifies
      per file without loading them into context: glob expansion
      (limit 20 default / 100 cap), per-file head truncation (24KB),
      binary detection (NUL in the first 8KB → skipped with a note),
      concurrency 4, one verdict with confidence per file in a single
      result. The system prompt routes classification questions over
      files to it (answers reported as given; grep/read fallback when
      no decision model is configured); codetool's judge-mode docs no
      longer claim Ollama-only — it routes through the configured jev
      provider (api or ollama). Stale judgetool tests rewritten against
      the runner-injected asker: state mode (structured + plain),
      quoted-JSON questions, model override, files mode (binary skip,
      limit cap, no-match), error paths, parseQuestions.
- **(c, compaction half) semantic boundary refinement** — when a
      decision model is configured, compaction asks it which candidate
      cut points near the positional cut are clean fold points: one
      round trip, one self-contained noul question per candidate (the
      excerpts and roles travel with the question). The cut moves to
      the nearest clean boundary within ±3 candidates, subject to the
      kept-zone constraints (INV5 ceiling for older moves, half-recent
      floor for newer). Fail-open by construction: unconfigured,
      error, or nothing classified clean all leave the positional cut
      untouched. A clean boundary also kills the mid-turn turn-prefix
      summary — one summarizer call instead of two.
- **(b) bash risk guard** — `permissions.bash_risk_guard` (off by
      default) runs the decision model on bash commands the regex
      lists miss and containment doesn't answer (the ambiguity zone).
      One round trip, two noul questions (destructive? cheaply
      reversible?); a ≥0.70-confidence destructive+irreversible
      verdict tightens to Ask (interactive, verdict annotated in the
      prompt) or Deny (autonomous modes — the sub-agent hole where
      ChildPolicy's BashDefault=Allow ran novel destructive commands
      like `git push --force` silently). Nothing maps to Allow; every
      miss (unset, unconfigured, errored, timed out, low confidence,
      half-answered) is exactly today's behavior; yolo skips the
      classifier entirely; forked child gates inherit it.
- **Audit round** — all seven jev call sites re-audited; the five
      pre-existing ones (fetch classify, evidence verdict, judgetool
      both modes, engine compaction wiring) already follow the
      discipline (nil when unconfigured, error → fall through +
      debuglog). The risk guard's two gaps closed: its error path now
      debuglogs the miss (same as fetch/evidence), and
      bash_risk_guard set without a provider debuglogs that the guard
      is inactive instead of looking like a classifier that never
      misses. Mid-session deconfiguration pinned by tests at both new
      sites: the asker re-resolves the provider per call, so removing
      the jev: section surfaces ErrNotConfigured on the next
      classification — a miss folded into the fallback, never a stale
      verdict from the dead provider.

- [x] d: judgetool files mode + rewritten tests + prompt/codetool
      routing (commit 021f7be).
- [x] c: boundary.go + boundary_jev.go, refine tests (move, error
      fallback, no-clean fallback, unknown index, nearest, tie prefers
      older, ceiling filter, no-classifier), classifier tests
      (confidence floor, error, self-contained questions, no-candidates
      no-call, mid-session deconfiguration) (commit 6f641a1).
- [x] b: risk.go + risk_jev.go, gate mapping tests (interactive Ask,
      autonomous Deny, yolo skip, miss fall-through table, ambiguity
      zone only, containment first, fork inheritance, empty mode),
      verdict mapping tests, wiring matrix (flag w/o provider, flag +
      provider, no flag), mid-session deconfiguration (commit c4bf28a
      + audit fix).
- [x] audit: debuglog on the risk miss path, inactive-guard note,
      deconfiguration tests at both new sites, missing debuglog import
      fixed (the interrupted commit didn't compile).
- [x] full suite green (127 packages ok, zero failures), go vet clean
      on every touched package.
- [x] CHANGELOG entries.

User gates: `make test` on the real tree. Live checks — files mode:
with jev configured, ask "which of the Go files under internal/tools
contain a TODO?" and watch it route through judge with a files glob;
compaction: a long tool-heavy session with jev on, debuglog should
show boundary refinement moving cuts (and only genuine misses log
"boundary classifier unavailable, positional cut stands"); risk guard:
`permissions.bash_risk_guard: true` with jev on, ask for a novel
destructive command in autopilot → Deny with "risk model:" in the
reason; with jev off → debuglog "the guard is inactive" and unchanged
behavior.

## Active: sandboxed bash dies on machines missing a cache dir (2026-11-02) — LANDED, AWAITING USER VERIFY

User report: "bash commands arent executing again- I think theres somehting
wrong with the sandbox". Same symptom class as the EBADFD round (every bash
call dies at sandbox start), new root cause — and a verification-env lesson:

- **Root cause: `sandbox.Compile` listed RW anchors that don't exist.** A
      Landlock path-beneath rule anchors on an `O_PATH` open of its
      directory; opening a missing path fails, and ONE failure killed the
      whole `applyLandlock` — so a single absent candidate (~/.cargo on a
      Rust-free machine, ~/.a1/browser-profiles before the first browser
      spawn, ~/.bun/install/cache without bun) made EVERY sandboxed bash
      start fail with ENOENT. The permission gate had already auto-allowed
      the call (`sandbox.Supported()` is true on real hardware), so the
      visible symptom was exactly "commands never execute". The sandbox
      container here has no Landlock (ENOSYS probe) and the enforcement
      test honestly skips — the real-hardware path only runs on the user's
      machine. Protocol audit against kernel source ruled the syscall
      layer clean (5.13+ `copy_min_struct_from_user` tolerates the 48-byte
      x/sys v0.49.0 `LandlockRulesetAttr` — zero tail; the packed
      path-beneath layout matches field-for-field), which pointed at the
      environment-dependent anchor opens.
- **Fix, three layers:** (1) `Compile` drops candidates that are not
      existing directories — portable, so seatbelt also renders only real
      subpaths; the workspace and temp dir still always make the manifest.
      (2) `applyLandlock` skips an RW anchor that vanishes between Compile
      and Start: ENOENT/ENOTDIR are skippable (a missing directory grants
      nothing), while EACCES/EPERM/EBADF stay fatal — a misbehaving
      sandbox must fail loud, never silently contained. (3) Rule failures
      name their anchor ("open rule anchor /home/x/.cargo: no such file
      or directory") instead of a bare errno, and bashtool stopped
      double-prefixing startLocked's errors ("sandbox: sandbox: …").
- Tests: `TestCompileDropsMissingCandidates` (every surviving RW anchor
      exists on disk; missing .cargo / ~/.a1 subdirs drop),
      `TestCompileKeepsWorkspaceWhenCachesMissing` (empty home still
      yields workspace+temp, len 2), `TestAnchorMissing` (linux-tagged:
      ENOENT/ENOTDIR + wrapped PathError variants skippable; EACCES,
      EPERM, EBADF, non-errno fatal). Suites green in the verify tree:
      sandbox, bashtool, permission, agent, webserver; darwin + windows
      cross-compile of sandbox/bashtool; whole repo builds (prompt stub +
      template as in prior sessions — the two agent persona-prompt tests
      assert "You are A1" and need the stub's marker string).

User gates: `make test` on the real tree — note the containment round-trip
(`TestStartSandboxedContainsWritesOutsideRW`) actually RUNS on Landlock
hardware (it skips in containers); live check — start a TUI, ask for a
plain `ls`, bash should execute sandboxed. If a start still fails the error
now names the exact anchor path and errno; `permissions.sandbox: off` in
config.yaml remains the pressure valve.

## Active: smart titles + persona defaults + live TUI browser (2026-11-02) — LANDED, AWAITING USER VERIFY

User directive: "smart titles in web gui dont get generated by the memory ops
model / persona settings arent the default- it usually falls back to the start
up model- persona should be the default selection for model, permission level,
memory ops model, and the memory toggle / the browser tool in the tui doesnt
launch a live web browser". Three root-cause fixes plus two found-along-the-way
bugs:

- [x] **Smart titles on the memory-ops model.** Root cause:
      `memoryConsolidationClient` read the persona through
      `session.activePersona()`, which reports false for the DEFAULT persona —
      so a MemoryModel configured on the default persona (the one the editor
      has open by default) never routed, and titles + consolidation ran on the
      main model. Now reads the store directly (every persona is a persona),
      and resolves MemoryModel through the model catalog first so a memory
      model on ANOTHER provider (local 8B vs hosted main) gets that provider's
      base URL/key/hooks instead of a name-swap into the main model's config.
      Catalog miss falls back to the legacy name-swap.
- [x] **Persona settings are the defaults.** Three layers were skipping them:
      (1) `applyPersonaSettings` had an `isDefault` guard — the default
      persona's Model/PermissionMode never applied anywhere; removed (empty
      fields still mean "inherit the base", the revert path restores the
      engine's construction-time base incl. hooks — a persona model on another
      provider now carries that provider's request hooks). (2) `liveFor`
      skipped `SetPersona` for the default persona — fresh sessions fell back
      to the startup model; now every fresh engine adopts the active persona.
      (3) The web UI resolved model/mode from the raw knobs — an untouched
      model knob showed "default" and built engines on the startup model.
      Now: `resolveModel` folds the active persona's Model under the manual
      knob (knob → persona → project default); stateView / handleGetSettings /
      modeView all show the EFFECTIVE values (persona override folded in,
      mirroring what engines run); the settings-panel model picker and the
      mode switcher WRITE the persona when it owns the setting (same
      ownership rule the memory toggle already had); a persona switch (or an
      explicit Model edit on the active persona, or create/delete that
      changes the active id) clears the stale manual knob so the new persona
      owns the defaults. `updateEngineBaseModes` no-ops on persona-owned
      picks. Persona editor's memory toggle is now TRI-STATE (Inherit / On /
      Off) via `clearMemoryEnabled` + `persona.Store.ClearMemoryEnabled` —
      the old binary form pinned `false` on every save, silently turning
      memory off for personas set to inherit.
- [x] **TUI browser launches a live browser.** Four stacked causes: (1) The
      fetch tool's BetterWright search fallback cold-spawned the SHARED
      default-profile daemon headless (`bw.Run(ctx, "", false, …)`) — a warm
      daemon ignores `--headed`, so every later `/browser open` landed in an
      invisible window. The engine now rides a dedicated background lane on a
      dedicated profile (`a1-bg`): its own daemon, headless by design, never
      poisoning the interactive one (and background scraping no longer rides
      the user's logged-in identity). (2) `browserHeadless` had no config
      layer: DISPLAY-less TUI environments (tmux/SSH) silently downgraded to
      headless. Resolution order is now A1_BROWSER_HEADLESS → `browser.headless`
      in config.yaml → display probe. (3) kamitool's PDF render still spawns
      the shared profile headless (PDFs may need the user's logins); its
      headless spawn is now TRACKED (`bw.LastSpawnHeaded`), and `ensureBrowser`
      recovers: stop-all + cold headed respawn when it wants a visible window
      and this process froze the shared daemon headless. (4) The TUI `/browser`
      command discarded both result and error (DispatchSlash swallows them) —
      success and failure were both invisible. It now toasts the outcome, and
      `/browser open` says which window mode landed ("visible window" /
      "headless — set browser.headless: false …") in Content, Output, and the
      toast.
- [x] **Found along the way: DELETE /api/session never removed sessions whose
      id contains an underscore.** `deleteSessionFiles` matched on the LAST
      underscore while the canonical `sessionIDFromFilename` uses the FIRST —
      "successful" 200 deletes left the file on disk and the sidebar row
      alive. One-word fix (LastIndexByte → IndexByte).
- [x] **Found along the way: TestRegenerateRefusesWhenTurnInFlight deadlocked
      the ENTIRE webserver suite.** The hanging handler parked with the
      request body unread — net/http only starts the background read (the
      machinery that cancels r.Context() on client disconnect) after the body
      is consumed — so `hanging.Close()` blocked forever and the whole
      package timed out ("webserver e2e too heavy for the sandbox" was this
      one test all along). Fix: drain the body before parking, cancel the
      turn via /api/cancel before teardown, force-drop connections via
      CloseClientConnections. **The full webserver suite now runs in ~8s in
      the sandbox** — the old "too heavy, compile-verified only" caveat is
      retired.
- [x] Tests: agent (engine_persona_defaults_test.go — default-persona
      overrides, hooks-follow-model, OpsClient routing for default + custom
      personas, catalog resolution + name-swap fallback), webserver
      (persona_defaults_test.go — effective model/mode resolution, knob
      precedence + reset on switch/edit, persona-owned model/mode writes,
      clearMemoryEnabled round-trip, fresh-engine default-persona adoption),
      bw (lane plumbing + spawn tracking + close-all reset), browsertool
      (headless resolution order, headed recovery + no-recovery-when-headed,
      legible window mode), fetchtool (search rides background lane+profile),
      persona (ClearMemoryEnabled persistence), tui/commands (browser
      toasts success + failure). Web dist rebuilt (vite clean).
- [x] Sandbox verification — whole repo builds; go vet clean on every touched
      package; full suites green: agent, webserver (FULL suite, unfiltered,
      8s), persona, bw, browsertool, fetchtool, kamitool, project,
      tui/commands, tui/controller, session, llm/*.

User gates: `make test` on the real tree (now includes the previously-hanging
webserver suite); live checks — (1) set a memory ops model on the default
persona in the web editor, start a new chat, watch the title appear from the
ops model (telemetry/usage of the second model, or debuglog); (2) configure a
persona's model/permission/memory and open a NEW chat — footer chip, settings
panel, and the engine should all show the persona's values; (3) `/browser open
<url>` in the TUI should raise a visible window (with DISPLAY) and toast the
outcome; headless landings now say so and name the override.

## Active: JEV configurable in the config web server (2026-11-02) — LANDED, AWAITING USER VERIFY

User directive: "make jev configurable in the config web server — either
using an api or ollama". The two wired call sites (fetch classify, evidence
verdict) currently gate on the TYPESAFE_API_KEY env var alone; this pass
adds a real `jev:` config.yaml section + editor panel with two backends:

- **Provider "api"** — TypeSafe System One (api_key / base_url / model,
  falling back to the existing TYPESAFE_* env vars).
- **Provider "ollama"** — a local Ollama chat model interrogated with the
  same noul/choice/score questions under a strict JSON contract
  (`format: "json"`, temperature 0, thinking off, tiny num_predict).
  Same validation spine as the TypeSafe wire: every question answered,
  choice ∈ criteria, score in rubric range, confidence required — any
  miss is an error and the call site falls back exactly as before.

Plan: jev.Settings + Configure/resolve in internal/llm/jev (pushed by
project.loadConfig so the web GUI's save→reload→next-turn flow applies
live); Asker interface unifying the two call sites' duplicated jevAsker
seams; provider-aware timeout hints (hosted API keeps 3s/4s, Ollama
raises the ceiling, `timeout_sec` overrides); configserver JevDoc
round-trip; editor panel replaces the stale "JEV placeholder" (that panel
actually edits `judge:`, which feeds `code mode=judge` — retitled, and
the real JEV panel added); tests for the Ollama contract, settings
resolution, config parse, and editor round-trip.

- [x] jev settings + resolver + Asker (settings.go) — Configure/
      CurrentSettings pushed by project.loadConfig; NewConfiguredAsker
      resolves per decision (live provider switching); EffectiveTimeout
      (site default → provider hint → timeout_sec override).
- [x] Ollama backend (ollama.go) — NewOllamaClient over the official
      ollama/api client; strict JSON decision contract; TypeSafe-parity
      validation; OLLAMA_HOST/base-URL normalization mirroring
      envconfig.Host.
- [x] call-site rewire (fetchtool/jev_classify.go, agent/jev_evidence.go)
      — local jevAsker interfaces alias jev.Asker; constructors gate on
      jev.Configured() (settings OR env — the nil-without-key tests still
      pin the env case); timeouts via jev.EffectiveTimeout.
- [x] project config `jev:` section + Configure push — jevConfig mirror
      struct, provider lowercased, fields trimmed; loadConfig pushes on
      every load (web GUI save → reload → next turn).
- [x] configserver round-trip (JevDoc + save-time provider/timeout
      validation) + editor panel (provider select with conditional
      fields, password api key, en+zh i18n; stale "JEV placeholder" panel
      retitled Judge — it edits judge:, which feeds code mode=judge).
- [x] tests — ollama_test.go (all three question types happy path, wire
      contract assertions, every rejection path, fence backstop, base-URL
      resolution table), settings_test.go (resolution matrix, timeout
      math, per-call re-resolution), project jev_config_test.go (parse +
      push), configserver config_jev_test.go (round-trip, validation,
      editor carry).
- [x] sandbox verification — whole repo builds in ~/scratch/a1-verify
      (prompt stub); tests green: jev (20 focused), agent (full suite,
      9s), project, configserver, fetchtool, evidence, llm/*, tui/
      commands; go vet clean on every touched package; inline editor JS
      node --check clean; LIVE BROWSER PASS via agent-browser against a
      real `a1 config` server: panel renders aligned (VLM-checked),
      provider toggle swaps field groups, ollama→save→reload→paint,
      api→save→reload→paint, Off drops the provider key but keeps the
      rest, negative timeout blocked client-side with a message, zh
      toggle applies, backup written, zero browser console errors.
- [x] CHANGELOG entry.

User gates: `make test` on the real tree; optional live check — set
`jev: {provider: ollama, model: <tag>}` in the editor, `ollama pull`
the tag, then grep the debuglog for "jev routed" / "jev verdict" on the
next fetch or unclassified check command.

## Active: Small-LLM context pass + JEV wiring (2026-11-02) — LANDED, AWAITING USER VERIFY

User directive: "improving the usage experience for smaller llm's — better
context management and compacting; on a 64k session sometimes auto
compaction works or doesnt" plus "wire up jev in the fetch classify and
evidence verdict gate from the agent loop (those are the only two places I
think jev should be used) (and should have fallbacks incase jev isnt
configured)".

- [x] **Compaction robustness, five fixes.** (1) Kept-zone ceiling
  (INV5): SettingsFor derives maxKeptTokens = W−1.8T (clamped ≥ K);
  findCutPoint moves the cut NEWER until the kept zone fits, so a giant
  message inside the kept window no longer survives every compaction
  (the post-compaction context could still overflow → streak death). A
  huge entry after every cut point is cut-unreachable by design; the
  recovery window halving owns it. (2) Summarizer input cap:
  CompactionPreparation.ContextWindow + SerializeConversationCapped
  bound the serialized conversation to window−output−1/8 — below cap
  byte-identical, over cap front-trim with a marker (newest kept). The
  summary call can no longer overflow the window that triggered it.
  (3) Safety valve falls back to the bytes/4 estimate when the provider
  reports usage 0 (blind valve = hard overflow with compaction never
  having fired). (4) Ollama-style overflow errors ("input length (N)
  exceeds context length (M)") now match IsContextOverflow and
  ParseContextLimit, so recovery triggers and learns the window. (5)
  compactMu serializes runCompact against ForceCompact from the web/TUI
  surfaces (double-compaction race).
- [x] **JEV in fetch classify (Phase 0).** jevClassifier (nil without
  TYPESAFE_API_KEY) asks one Choice question over 11 engine criteria;
  the pick dispatches before the predicate order via engineByName, and
  any miss (empty result, error, unknown choice, timeout ≤3s) falls
  through to the untouched predicates. Merged is choosable;
  BetterWright deliberately is not. Decisions debuglog-traced.
- [x] **JEV in evidence admission.** jevEvidenceAdvisor (nil without
  key, 4s timeout) is consulted only when ObserveExit produced zero
  records: one Score question (0 not-a-check / 1 failed / 2 unclear /
  3 likely-passed / 4 clearly-passed), confidence ≥ 0.6 required, a
  check-word pre-filter gates the round-trip, and admitted records are
  `jev:<command-slug>` ids — ValidCheckID-legal (declarable in
  contracts), ratcheting like deterministic ones. Fail details carry
  the first error line + `[jev verdict, confidence N.NN]` provenance.
  New ledger surface: ObserveJev + JevCheckID (internal/evidence/jev.go).
  Executor: SetJevAdvisor; engine: WithJevEvidenceAdvisor (default =
  env-gated constructor).
- [x] **In-sandbox verification.** Whole repo `go build` clean; tests
  green: agent (full suite), session, session/compaction, llm+all
  subpackages (incl. jev), evidence, tools/... (incl. fetchtool),
  tui/commands, contextbreakdown; go vet clean on touched packages.
  WEBSERVER E2E ran compile-verified only at the time — RETIRED CAVEAT:
  the "9-minute timeout" was TestRegenerateRefusesWhenTurnInFlight
  deadlocking the suite (see the 2026-11-02 smart-titles/persona/browser
  item above); the full webserver suite now runs unfiltered in ~8s. The
  agent tests ran against a
  SANDBOX-ONLY prompt stub at ~/scratch/a1-verify/internal/agent/prompt
  (the zip lacks the real internal/agent/prompt — present on the user's
  tree; the stub is NOT in the extract and must never be synced). Two
  agent-test failures observed with a short stub were stub artifacts
  (system-prompt text assertions), green with a realistic-length stub.
- [ ] **User verification.** `make test` on the real tree (the true gate
  — internal/agent/prompt exists there), then a 64K-session soak: long
  tool-heavy session, confirm auto-compaction fires reliably near the
  threshold and overflow recovery survives giant results. Optional JEV
  live check: export TYPESAFE_API_KEY, watch debuglog for "jev routed"
  / "jev verdict" lines, and confirm fetch/evidence behave identically
  with the key unset.

## Active: Tooling pass (2026-11-02) — ALL THREE LANDED, SIGNED OFF

User directive after the webui was declared finished: "all we need to do
now is improve the tooling" — more fetch sources, a search-tool overhaul,
betterwright in the Makefile.

- [x] **fetchtool: five new engines + a merged web fallback.** Domain
  engines added: MDN (web-platform docs, before the broad programming
  predicates so "css flexbox" lands on docs), npm and crates.io
  (language-specific package registries before GitHub's language-agnostic
  repo search), Hacker News and Reddit (community opinion — registered
  BEFORE Stack Exchange on purpose: SE's predicate matches any query
  that names a language, and "what do people think of bun" must reach
  the forums; Reddit edges out HN when the query names it). The general
  fallback is no longer DDG Lite alone: a merged engine runs DDG Lite +
  HN + Reddit in parallel and interleaves results 2:1:1 (web index
  dominant, community hosts diversifying the source pills). The
  merge propagates the primary's error — a captcha-walled DDG hands off
  to the BetterWright browser fallback instead of masking it with
  community-only results — and fetches page bodies exactly once, on the
  merged survivors. Reddit hard-blocks datacenter IPs; that error is
  swallowed and the merge proceeds. All engines are keyless JSON APIs
  with parse functions split out for fixture tests; classify tests
  extended (general queries now classify as "web"). Engines: mdn,
  hackernews, npm, crates, reddit + merged (ddglite+hackernews+reddit).
- [x] **searchtool overhaul: glob, literal, context, listing, errors.**
  The grep+ls fusion tool got the knobs the model kept wishing it had:
  `pattern` accepted as an alias for `query` (grep/find habit);
  `glob` filtering every plane (rg --glob for text with full ** /
  negation power, simple matcher for symbol/file); `literal:true` for
  plain-text queries; `context=N` rendering surrounding lines with a ·
  marker (matches keep |); empty query + explicit path = a listing mode
  (dirs first, size + mtime per entry, truncation notice) so search is
  the one-stop discovery call. ripgrep failures now surface — an
  invalid regex used to look like "no results", now the stderr comes
  back with a "pass literal:true" hint. Textless hits show size + mtime;
  ranking adds a basename-match boost and shallower-path tiebreak; the
  header reports "N of T files" when trimmed; the no-results path
  suggests next steps. Verified against the repo itself (context
  rendering, listing with fresh mtimes, regex queries with the trim
  notice).
- [x] **`make betterwright-install`.** Standalone target installing the
  pinned CLI (`BETTERWRIGHT_VERSION ?= 2.9.0`, kept in sync with
  internal/toolmanager's BunTools pin) and running the explicit
  `betterwright setup` browser download (~150 MiB — never an `install`
  side effect). Bun-guarded like web-install; falls back to a
  "open a new shell and run betterwright setup" hint when the fresh
  binary isn't on PATH yet. The bw client's not-installed error now
  points at the target first. NOTE: this extract's Makefile had
  space-indented recipes (GNU make "missing separator") — the whole
  file was converted to tabs, so `make anything` works again.
- [x] **User sign-off.** The user reviewed the fetch surface and
  declared it "perfect now in my opinion." A news function (worldwide
  RSS consolidated into bias-balanced reports) was proposed and
  DECLINED by the user — "most news is stupid" and they don't read
  news anyway; do not resurrect it. The mechanical gate (`make test`
  on the user's tree, where internal/agent/prompt exists) still runs
  whenever the extract syncs back.

## Roadmap: next horizons (2026-11-02) — NOT STARTED

User brainstorm after declaring the tooling pass done ("eventually I
want to add…"). Recorded so the ideas survive context resets; nothing
below is built. Pick these up only on an explicit go.

- **Matrix integration — text the harness.** (DONE 2026-11-09) Built as
  `a1 matrix` + `internal/matrix`, exactly per this note's locked design:
  the webserver runs in-process on an EPHEMERAL loopback port and the
  bridge is a loopback client of the same chat API + SSE the GUI uses
  (POST /api/prompt, GET /api/events, POST /api/ask), so the chat surface
  stays loopback-only. Permission approvals post into the room and a
  plain y/n inside the window decides; an unanswered approval DENIES (the
  bridge's window sits 30s under the webserver's 10-minute ask timeout).
  Room-side commands: `!stop` cancels the running turn, `!new` starts a
  fresh session; the bridged session id + login token persist in
  `~/.a1/matrix/state.json` so restarts resume. Replies arrive as chunked
  markdown (plain body + goldmark HTML, raw HTML dropped) with a typing
  indicator while turns run. Stated v1 limits (see doc/matrix.md):
  plaintext rooms only (megolm out of scope), no history replay, no
  sender allowlist (the room is the trust boundary), no streaming
  replies. What is deliberately LEFT OPEN for later chunks: encrypted
  rooms, `allowed_senders`, message-edit streaming, and cross-process
  session locking (driving the same session from the GUI and the bridge
  at once can interleave turns — documented as a sharp edge).
- **stoat.chat bridge — same loopback-client shape, second protocol.**
  (DONE 2026-11-09) Built as `a1 stoat` + `internal/stoat`, per this
  note's locked shape: the Matrix architecture carried over ruling for
  ruling (ephemeral in-process webserver, the bridge a loopback client
  of the same chat API + SSE, approvals over chat with deny-on-timeout,
  session persistence in a state file, chunked markdown replies, the
  channel as trust boundary). The wire was scoped against the
  platform's own protocol docs + OpenAPI before a line was written:
  stoat is the Revolt lineage — `X-Bot-Token` REST at api.stoat.chat,
  event websocket v1 JSON at wss://stoat.chat/events. The dependency
  budget held at zero: the websocket half forced one new foundation,
  `internal/ws` (hand-rolled RFC 6455 client: masked sends, text
  frames only, fragmentation assembled, auto-pong, CloseError, RSV
  refused, 90s idle deadline), which the Discord bridge shares.
  Stoat-specific rulings: message chunking counts RUNES (the backend
  counts Unicode scalar values — 2,000 CJK chars is one legal
  message), typing rides the event socket (no REST route exists),
  refresh 3s with an immediate end when the last turn finishes, sends
  are paced before the request from X-RateLimit headers (fixed window,
  10/10s per channel) with one retry_after sleep on a 429, other bots
  never get a turn (embedded bot information is the skip marker), and
  the bot token is never cached to disk (a stable pasted credential is
  config's business, unlike Matrix's minted login tokens). LEFT OPEN:
  attachments/embeds, masquerade for bridge lines, sender
  allowlisting, streaming replies.
- **Discord bridge — the third surface, gated on gateway v10 shape.**
  (DONE 2026-11-09) Built as `a1 discord` + `internal/discord`, with the
  four rulings this note demanded settled before chunk 1: BOT TOKENS,
  not OAuth (a bridge is a bot); the `!`-prefix convention the Matrix
  bridge set, over slash-commands' application registry; chunking at
  2,000 — counted in UTF-16 CODE UNITS, the unit every Discord client
  counts (an astral emoji is two units; a rune counter would emit
  "2,000 characters" of emoji and watch Discord reject it as 4,000);
  and the bucket-aware pacer: waits before requests from
  X-RateLimit-Remaining / Reset-After (SECONDS, floating — the unit
  trap stoat's milliseconds do not have), sleeps out a 429's
  retry_after exactly once with every 429 teaching the limiter even
  when it ends the call, and a global 429 takes a process-wide lockout
  that spans even the typing route. The gateway speaks v10 over the
  stoat bridge's hand-rolled `internal/ws` unchanged (HELLO's
  heartbeat interval with a jittered first beat carrying the dispatch
  sequence, IDENTIFY with guild-messages/DMs/message-content intents,
  READY pins the self user, MESSAGE_CREATE the only consumed dispatch).
  No RESUME in v1 — a reconnected bridge identifies fresh and drops the
  gap, the same "history is context" ruling as its siblings; the
  sessions the roadmap left open. LEFT OPEN: RESUME/seq replay,
  slash-commands, embeds/attachments, threads, sender allowlisting,
  streaming replies. The MESSAGE CONTENT intent prerequisite (Developer
  Portal, privileged) is documented loudly in doc/discord.md — the
  missing intent is the silent failure a doc paragraph is the only
  defense against.
- **Web dashboard — mailboxes, recent documents, pinned sessions,
  usage heatmap.** Much of the foundation already exists: `/stats`
  (server-rendered) and `/api/telemetry` (JSON) are live, pins.json
  exists, and the telemetry SQLite indexes `turns.at` and
  `tool_runs.at` — the GitHub-style heatmap is one
  `GROUP BY substr(at,1,10)` query over turns. Recent documents:
  write-tool runs in the transcript JSONL carry file paths, and
  snapshots know what a turn created. "Mailboxes" has NO existing
  data source — closest is the schedule/job system (async fires
  landing results for review); **PLANNED 2026-11-05** — the full chunked
  build now lives as the Dashboard epic at the top of this file: mailboxes
  = a local Maildir (read-only), recent documents folded into the
  split-screen file browser, pins and the heatmap chunk 1.
- **JEV integration — DONE, scope locked by the user.** Wired into
  exactly TWO places (fetch classify + evidence admission, this pass);
  the user said "those are the only two places I think jev should be
  used." Do NOT propose additional homes (loop continuation was
  considered and is out of scope by this ruling). `cmd/jev` stays an
  empty stub by the same decision.

## Active: VeriLoop-Grade Evidence Loop (2026-10) — ALL PHASES LANDED

Port of the VeriLoop-Governed Recurrence (VGR) functional contract onto a1:
the model proposes, the harness admits evidence, commits, and rolls back.
Doctrine currently enforced by the 5-Gate prompt moves into the loop itself.
Design notes: chat analysis 2026-10-29 (authority-split mapping table).
Phases 0-5 and the chat-mode follow-up are done; nothing is open from this
effort.

### Phase 0 — Snapshot integrity fix (DONE)

- [x] **Snapshot captures the full working tree, not just HEAD.**
  `internal/snapshot` rewritten: Create builds a temp-index tree
  (`git add -A` respecting .gitignore) and stores it as
  `refs/a1/snapshots/<name>` — tracked modifications, staged changes, and
  untracked non-ignored files are all captured. No branch is created.
- [x] **Rollback restores without switching branches.** Rollback uses
  `git read-tree --reset -u` + untracked-cleanup (files created after the
  snapshot are removed; files that existed before it are restored byte-exact
  and become staged). HEAD never moves; the old `checkout -f` can no longer
  destroy the user's uncommitted WIP.
- [x] **List/Delete migrated to the ref namespace** (legacy
  `harness-snapshot-*` branches still listed/resolvable for continuity).
- [x] Tests: uncommitted-WIP capture, post-snapshot mutation revert,
  post-snapshot file cleanup, no-branch-switch invariant, legacy branch
  resolution.

### Phase 1 — Evidence ledger (protected ratchet) (DONE)

- [x] **`internal/evidence` package.** `Ledger` keeps last-known
  pass/fail per stable check id (`go-build`, `go-vet`, `go-test:<pkg>`,
  `go-test#<Test>`, `pytest:<nodeid>`, `cargo-test:<name>`,
  `cargo-build`). Checks that have ever passed are **protected**; a later
  fail on a protected check is a regression. Ratchet only grows.
- [x] **Deterministic harness-side parsing.** `classify` recognizes go
  build/vet/test, pytest, cargo test commands, splits compound commands on
  `&&`/`||`/`;`/`|` and parses per-package / per-test results from output.
  Conservative: ambiguous compound failures record nothing; canceled/timed
  out bash calls are never recorded (incomplete output must not poison the
  ledger).
- [x] **JSONL receipts.** Every record appends to
  `<session dir>/evidence.jsonl`; a fresh `Ledger.Load()` replays receipts,
  so the ratchet survives session resume. Harness-written = unfakeable by
  the model.
- [x] **Executor hook (advisory verdict gate).** After every `bash` run the
  executor feeds command + output to the ledger. On protected regression the
  tool result gains a `[evidence]` system note naming the regressed checks
  and the fix-first rule (no deleting/weakening tests to go green) — the
  model sees it next round, in every permission mode, zero rollback risk.
- [x] **Wiring.** `agent.WithEvidenceLedger` option; engines default to a
  session-persisted ledger (memory-only when the session is in-memory).
- [x] Tests: parsers (go/pytest/cargo/ambiguous), ratchet verdict matrix,
  persistence replay, executor hook, regression-note formatting.

### Phase 2 — Verdict gate in the loop (APPA mode-dependent) (DONE)

- [x] **Pre-candidate snapshot.** `internal/agent/evidence_gate.go`
  (`turnEvidenceGate`, one per turn): before a mutating batch (any
  non-readable tool — bash always qualifies) with protected checks in the
  ledger, `snapshot.Create` pins a baseline ref. Baselines are commit
  points: a clean candidate (no regression, no residual protected failure)
  ADVANCES the baseline, so a rollback restores the latest verified state,
  not the turn start — verified progress is never thrown away. At most one
  engine-owned ref is live (superseded + previous-turn baselines are
  deleted), so refs/a1/snapshots never accumulates harness garbage.
- [x] **Enforced rollback in autopilot/headless-strict.** Candidates are
  judged by pre/post ledger diff (VeriLoop's Compare): Regressed = any
  protected check flipped pass→fail. In enforced modes a regressed mutating
  candidate is rolled back to the baseline, the ledger re-admits the
  regressed checks at their verified status (`Ledger.RevertTo`, harness-
  written `harness:candidate-rollback` receipts survive session resume), and
  a system note names what was rejected. Re-applying the same broken edit
  reads as a FRESH regression, not fail→fail neutral — the ratchet stays
  blind-proof. Interactive/readonly/yolo and unknown gates stay advisory
  (Phase 1 note); the mode is read live at verdict time, so mid-turn mode
  flips take effect on the next candidate. Rollback failure or a non-git
  workspace degrades to an advisory note, never breaks the loop. A
  regression on a non-mutating round is surfaced as drift, never rolled
  back.
- [x] **Anti-thrash policy.** `stallLimit` (default 3, `WithStallLimit` /
  `SetStallLimit`, 0 = off) consecutive non-improving verification rounds —
  regressions or no-new-evidence equals — stop the turn with the residual
  failure set instead of burning budget. StrictlyBetter (no regression +
  genuine improvement) resets the counter; the mandated post-rollback
  re-verification round is free (amnesty).
- [x] Tests: enforced rollback (autopilot + headless-strict), advisory
  passthrough (interactive/readonly/yolo/unknown), repeat-offense freshness,
  baseline lifecycle (stale cleanup, advance, latest-restore), stall
  counters + residual surfacing, post-rollback amnesty, snapshot-failure /
  rollback-failure / drift / not-a-candidate degradations, executor
  BatchMutates + verdict drain, engine wiring (live mode flip, stall-limit
  config), and evidence-side RevertTo guardrails + receipt replay.

### Phase 3 — Contract compilation + zero-rank stop (DONE)

- [x] **Machine-checkable DONE_CRITERIA.** The `contract` tool
  (`internal/agent/contract.go`, registered on every engine): the model
  PROPOSES its done-criteria as check ids (`go-build`, `go-vet`,
  `go-test:<pkg>`, `go-test:<prefix>/...`, `go-test:...`, `go-test#<Test>`,
  `pytest:<nodeid>`, `cargo-test:<name>`), the harness HOLDS them
  (`turnContractState`, mutex-guarded, ratcheted — checks can be added,
  never removed, so the model cannot weaken its own finish line mid-task;
  a new user prompt starts a fresh contract). The declaration reply shows
  each check's live ledger status (PASSING / FAILING / NO EVIDENCE), and
  the prompt-side Gate 1 DONE_CRITERIA / Gate 4 ZERO-RANK DONE doctrine is
  now the human-readable projection of the machine contract.
  Contract math lives in `internal/evidence/contract.go`: strict id
  validation, pattern matching (a pattern needs ≥1 match and all matches
  passing — zero matches is NO EVIDENCE, never a vacuous pass), and
  `Residual()` = the non-passing subset.
- [x] **Zero-rank stop.** When the model stops calling tools with a
  declared contract, `Engine.zeroRankCheck` judges it against last-known
  ledger evidence: residual checks inject the failure set as a system note
  and re-open the turn; a clean contract ends with a
  "turn certified complete" certificate; an exhausted budget stops the
  turn with the residual set surfaced ("NOT certified complete"). Stop =
  failure set empty or budget fires — never just "the model said done".
  Budget: `defaultZeroRankBudget` = 2 extra rounds, tunable via
  `WithZeroRankBudget` / `SetZeroRankBudget` (0 = disabled). No declared
  contract = no enforcement (the harness cannot guess undeclared criteria);
  regressed and never-run checks are both residual ("existing but
  unchecked = not done", made mechanical).
- [x] **Budgeted verification.** Gate 4 doctrine now mandates
  `testimpact` / targeted packages for per-change verification and the
  full suite at commit points (before declaring done, before claiming a
  clean candidate) — the harness admits evidence and refuses to certify
  on stale or absent green; it does not run commands itself (APPA stays
  the sole authority over execution).
- [x] APPA integration: `contract` is read-classed in permission
  (extract/contract/label/toolAction) — no prompt in interactive, allowed
  in readonly, never trips a spurious verdict-gate baseline.
- [x] Tests: id validation + pattern/residual math (evidence), contract
  ratchet + refusal + ledger-status rendering + bad args (tool), loop-level
  zero-rank (block → fix → certify, budget exhaustion, no-contract freedom,
  disabled option, per-turn reset, no-evidence blocking) with a scripted
  SSE model.

### Phase 4 — Evidence-driven recurrence (context) (DONE)

- [x] **Ledger state in compaction summaries.** Every compaction summary
  now ends with a harness-written `<evidence-record>` block (rendered by
  `Engine.renderEvidenceRecord` in `internal/agent/evidence_record.go`,
  injected at the single choke point `runCompact`, so post-turn, overflow
  recovery, and `/compact` all emit it): protected-check count, passing
  ids, protected regressions with failure detail, never-passed failures,
  and the turn contract's per-check standing plus residual count — each
  section capped (30 entries, "+N more") so the record stays bounded on
  large workspaces. Next turn's context carries verified evidence, not a
  transcript-shaped memory of it. The block is appended verbatim after the
  model summary and file ops (`compaction.wrapEvidenceRecord`) — the
  summarizer never writes it, and both summary templates now DROP
  test/build status sections so the model never fabricates a copy. The
  previous summary's record is stripped (`compaction.stripEvidenceRecord`)
  before the update prompt sees it, so stale evidence is never folded
  forward. **Chat-inert by construction:** no observed checks and no
  declared contract renders "" — plain chat compaction is byte-identical
  to pre-evidence output (pinned by test).
- [x] **Receipt pruning.** `evidence.jsonl` growth is capped: every 512
  appends (`defaultPruneEvery`) the file is rewritten with the minimal
  replay-equivalent record set — per check, its last Pass receipt followed
  by its last record (`Ledger.pruneLocked`, temp-file + rename under the
  ledger mutex). A check that passed and later failed still replays as
  protected-and-failing (the pass receipt is what keeps the ratchet
  intact), and `harness:candidate-rollback` receipts survive verbatim.
  `Load` self-heals an overgrown file on resume: replay then rewrite, so
  the next resume replays the minimal set.
- [x] Tests: prune replay-equivalence across interleaved prunes (state,
  protected set, regressions identical after reload), ratchet survival for
  regressed checks, file-size bound (1200 receipts → ≤ 2×checks + interval),
  load-time self-heal, memory-only no-op, RevertTo-receipt survival;
  wrap/strip edge cases (unclosed marker untouched, separator swallowing,
  multiple records), Compact append + section ordering (summary → file ops
  → record), PrepareCompact strip; renderer full/chat-inert/contract-only/
  caps, end-to-end ForceCompact with and without evidence (chat summary
  unchanged).

### Phase 5 — Budget contracting (DONE)

- [x] **The `budget` tool: the model PROPOSES, the harness HOLDS.** Every
  budget a1 had (maxRounds, maxToolCalls, stallLimit, zeroRankBudget) was a
  harness-side static cap; Gate 1's STOPPING RULE doctrine was prompt-only.
  Now `budget(rounds: N)` declares N more tool rounds from the current
  position (the ceiling prices against the live round counter the loop
  publishes), the harness holds it in engine state the model cannot edit,
  and every declaration — including each extension — is a visible tool call
  in the transcript. Pairs with `contract`: budgets bound the spend,
  contracts bound the finish line. Read-classed in permission
  (extract/contract/label) and never triggers a verdict-gate baseline.
- [x] **Firing semantics: binds without trapping.** At the ceiling with work
  still wanted, the round runs on the turn's ONE re-open (grace round) and a
  `[budget]` note lands after the batch with the spend state (rounds used,
  ceiling, extensions, re-opens) and the contract residual when declared.
  Declaration-only batches (`budget`/`contract`) never fire the gate —
  without the exemption the grace note's "extend now" would be a trap, since
  the model only reads the note after the grace round. A mixed batch
  (declaration + work) is NOT exempt, so pairing budget(+1) with every work
  round cannot bypass the ceiling. A second work round without a fresh
  declaration stops the turn terminally: pending calls come back as
  rejected results (new `Executor.RejectBatch`; the session never holds an
  unanswered tool call), the terminal assistant message is persisted, and
  the spend report surfaces the contract residual — an out-of-budget stop is
  never mistaken for a certified one. Wrapping up with a final answer after
  a firing ends the turn through the zero-rank gate as usual.
- [x] **Chat-safe by construction.** No declaration = no gate: turns that
  never call `budget` are byte-identical to pre-Phase-5 behavior (pinned by
  test). Reset at every Loop entry — a spent budget never bleeds into the
  next prompt. Enforcement toggle: `WithBudgetContracts(false)` /
  `SetBudgetContracts` records and echoes declarations but never stops the
  turn (default on — extending is always one visible tool call away, so
  enforcement only ever binds the model that declared).
- [x] Tests: state machine (declare/trip/grace/stillSpent/reset), tool
  surface (declare reply, extension echo, singular/plural, bad args, ledger
  standing, enforcement-off echo, read-classing), gate exemption matrix
  (pure declaration vs mixed batch vs reads), loop-level (inert without
  declaration, fire→grace→extend→finish, fire twice→terminal with rejected
  results + spend report + session consistency, terminal stop surfaces
  contract residual, wrap-up-after-grace certifies via zero-rank,
  enforcement disabled, mid-loop declaration prices against live rounds,
  fresh turn starts fresh budget).

### Follow-up — Chat-mode ergonomics of the evidence surface (DONE)

- [x] **The declaration is the classifier.** No user toggle, no intent
  heuristic, no workspace sniffing (chatting inside a repo is the common
  case and must not govern). A session becomes a coding session when the
  model declares its first real `contract` — the check-id grammar is only
  expressible for build/test work, so a chat task cannot accidentally arm
  governance — and the latch never clears for the session: later turns
  keep recording and rollback protection. Until then the executor's
  evidence hook records nothing (`Executor.SetGovernedFn` bound to
  `Engine.IsGoverned`), so an incidental build command in a chat turn
  neither grows the ledger nor arms regression notes for later casual
  turns in the same session. Empty/all-refused declarations arm nothing;
  `budget` never arms it (spend is not a finish line).
- [x] **Live derivation instead of a persisted flag.** `Engine.IsGoverned`
  reads the latch OR a non-empty ledger, so a resumed session (receipts
  replayed by `Ledger.Load`) is governed without any declaration. One
  thin edge accepted by design: declared-but-never-run resumes
  ungoverned — it fails in the safe direction (under-govern, never
  over-govern).
- [x] **Tools stay visible in every session.** `contract`/`budget` are NOT
  hidden outside coding workspaces — they are the opt-in mechanism, and
  hiding them on a directory guess would reintroduce the toggle by other
  means with a worse failure mode (hidden exactly when needed). The
  ~300 prompt tokens are the price of the declaration being always
  available.
- [x] Tests: ungoverned inertness (no records, no notes, no verdicts, no
  baseline, empty compaction record), chat turn with incidental build
  end-to-end, empty/refused declaration never arms, declaration arms and
  the next run records, sticky across turns (turn 2 without declaration
  still records + notes), non-empty ledger governs without declaration,
  real resume derivation across the process boundary, and the
  declared-never-run thin edge pinned.

## Active: Agentic Harness Roadmap (2026-11) — from the full-source audit

Provenance: external audit of the whole tree (1,542 files; four passes: agent
core, tool surface, context/memory systems, ecosystem); user-reported
additions (2026-11) are marked inline.
Verdict: the verification core above is ahead of commercial harnesses — extend
it, never replace it. The distance to a general agentic harness is three axes
a1 barely touches:

- **Time.** Nothing wakes the agent. No daemon, no cron, no timers; the loop is
  strictly user-prompt-driven. `job.Manager.recoverStale` fails all
  non-terminal jobs on restart — sub-agents are strictly in-process.
- **Senses.** Tool results are text-only. `browser_read` returns a screenshot
  *path*; a vision-capable model cannot see its own browser. Image plumbing
  exists on the way in (`ImageEnabled` in `internal/llm`), not on the way out.
- **Scale.** Sub-agents: depth forced 0 (`MaxDepth = 1`), 4 slots, spawn fails
  instead of queueing (`ErrBusy`), parent handoff is a ≤12KB text summary. A
  scout, not a fleet.

Audit scorecard (0-10, source-derived):

| Dimension                 | Sc | Basis                                                    |
| ------------------------- | -- | -------------------------------------------------------- |
| Verification & governance | 9  | VGR: ratchet, rollback, certified completion             |
| Context economics         | 8  | fold math documented + machine-checked                   |
| Permission & trust        | 7  | APPA formal; containment is still regex policy           |
| Memory & learning         | 6  | activation-scored retrieval; dead loops (Bet 06)         |
| Orchestration & scale     | 4  | depth 1, no queue, restart kills jobs                    |
| Action breadth            | 4  | bash blocking, browser web-only, no git tool             |
| Interface surface         | 4  | loopback GUI; machine interface is `a1 run --jsonl` only |
| Durability & scheduling   | 2  | nothing wakes the agent                                  |
| Learning & evals          | 2  | telemetry in-memory, 3 event kinds, no evals             |

Nine bets, four phases, anti-goals last. Effort: S ≤ a week, M ≤ a month, L
is a season. Every bet names its touch points; all build on machinery that
already exists — nothing here requires a rewrite. User-reported (2026-11):
Bet 08, Bet 09, the memory-model toggle and SOUL wiring in Bet 06, and the
tool-description rewrite in the quick wins below; the rest is audit-derived.

### Phase 1 — Compound the cheap wins (DONE 2026-10-31; search fallback re-pointed to Bet 09)

#### Bet 05 — The Grammar: widen the evidence classifier (S) — DONE 2026-10-31 (screenshot kinds ride with Bet 02)

The loop's teeth are only as good as `classify`, and it knows go build/vet/
test, pytest, cargo — full stop. A JS shop using a1 today runs ungoverned:
verdict gates never fire because nothing classifies as evidence. Smallest
bet, biggest leverage — it makes everything already built cover the polyglot
world.

- [x] **Polyglot parsers** in `internal/evidence/classify.go` (DONE
  2026-10-31). Suite-level checks with the build/vet attribution doctrine:
  `js-test` (npm/pnpm/yarn/bun test incl. `run test:unit`, direct
  vitest/jest, npx/bunx forms; jest/vitest/TAP failure lines as detail),
  `playwright-test`, `mypy` (incl. `python -m` / `uv run`), `ruff check`,
  `eslint`, `cargo clippy`, `dotnet test`. Passes credit every segment in a
  successful compound; failures attribute only when the family is the
  single classifiable segment — and the existing build/vet rules were
  tightened to respect suite segments too (`go build && npm test` failing
  now records nothing, matching the ambiguity doctrine).
- [x] **Generic fallback check `exit:<n>`** (DONE 2026-10-31, as watched
  ids `exit:<code>:<command>`). Any single command's exit code becomes an
  admissible check: declare `exit:0:make lint` in a contract, run the
  command, the harness records the observation against the declared
  expectation. Observation-gated by design — an incidental bash call never
  grows the ledger, so the verdict gate's "a check ran" signal stays
  honest. The executor hook parses the real exit code from bashtool's
  `(exit error: exit status N)` marker (signal deaths degrade to
  failed-ness only). Contract grammar in `internal/evidence/contract.go`
  + watch system in `internal/evidence/watch.go`.
- [x] **Config-defined checks in `config.yaml`** (DONE 2026-10-31).
  `checks: [{name, command, success: "exit:<n>", pattern: <regex>}]` —
  every completed run of the exact command records `check:<name>`;
  `project.Config.EvidenceChecks()` converts, `WithEvidenceChecks`
  installs on the default ledger at all three construction sites (web,
  TUI, run loop). Bad patterns fail loud at startup.
- [x] **New evidence kinds beyond process exit** (DONE 2026-10-31 for
  probes). `http:<status>:<url>` observed from curl status lines
  (incl. `-w '%{http_code}'`), `port:<host>:<port>` from `nc -z` — both
  contract-declared watches, judged against the declared expectation
  (a 500 against a declared 200 records FAIL, detail "got 500").
  `screenshot:<name>` still lands with Bet 02.

Quick wins (each under a day, ordered by pain removed):

- [x] **Background bash.** (DONE 2026-10-31) `mode: start` runs detached
  (survives the tool call; killed via process-tree cancel), `ps` lists,
  `attach` returns the bounded rolling tail (optional `wait` seconds block
  until exit — the "wait for server ready" move), `kill` stops the tree.
  Output capped by the existing 8 MB rolling tail; exited shells stay
  listed (final output + exit code) until pruned at 20. Background modes
  never reach the evidence ledger — a still-running suite must not read as
  passing (the hook skips non-run modes). `internal/tools/bashtool/
  background.go` + mode dispatch in `bash.go`.
- [x] **Rewrite tool descriptions for comprehension (user-reported
  2026-11).** (DONE 2026-10-31) All seven remaining targets rewritten
  (fetch/ask_user/search/browser/write + code with its three new modes +
  read with the elision surface) in the bash exemplar style: what it does,
  when to reach for it, mode guidance, gotchas. The rewrite fixed real
  documentation bugs, not just tone: `browser`'s claim that only `open`
  works without a running browser was wrong (every action except `proxy`
  errors); `fetch` hid the routed search (Frankfurter/Stack Exchange/
  GitHub/arxiv/Wikipedia before the DDG-Lite fallback) and misread the
  1 MiB body error as truncation; `search` never documented that the
  grep/find escape hatches take `pattern`, not `query`; `write` never
  spelled out the edit mode's exact-match contract. Token discipline held
  on the rewrites (the five wave-one tools went 1,535 → 1,517), but the
  layer grew with new sanctioned surface: git tool +290, three code modes
  +59, read's refresh param +74 — whole-layer 4,117 → 4,522 (`go run
  ./cmd/toolaudit`), every addition measured.
- [x] **bash stdin + env + cwd params** (DONE 2026-10-31). `stdin` pipes
  text (gpg/ssh/psql/REPL workflows without heredocs), `env` merges
  KEY=VALUE over the inherited environment (later entries win), `cwd` sets
  the working directory (relative resolves against the session cwd).
- [x] **Wire the search fallback — moved to Bet 09 (Phase 2), DONE there
  2026-11-01.** The captcha-bypass catch-all in `fetchtool`'s `searchRouter`
  now exists: ordered fallbacks (DDG Lite → BetterWright browser engine),
  registered when the daemon resolves.
- [x] **A real git/gh tool.** (DONE 2026-10-31) `internal/tools/gittool`:
  one facade, twelve modes (status/diff/log/show/branch/add/restore/
  commit/push/pull/pr/issue), strict argv execution (never a shell
  string), injectable runner, 64 KiB head-keep output cap. APPA mapping
  in `permission/extract.go`: read modes → ActionRead; add/restore/
  branch-create → ActionWrite with resolved paths (missing files → bash
  class so the gate asks instead of hard-denying); commit/push/pull/
  pr-create/issue-create/unknown → ActionBash — "no commit without
  explicit request" is now Ask-by-default in interactive mode and Deny
  under readonly (gate-fold pinned by test). Commit prose never reaches
  the bash-pattern Command field. e2e tests run real `git` hermetically
  (GIT_CONFIG_GLOBAL=/dev/null); gh modes covered by argv tests + the
  missing-binary error branch. Readable=false by design: batched
  add+commit must not race the shared index.
- [x] **Surface the orphaned tools.** (DONE 2026-10-31) `error` (pattern
  match → fix), `stack` (trace → file:line frames + source context), and
  `errpattern` (Go error-handling anti-pattern scan) are now `code`
  modes — 19 total, all read-only so the permission classification is
  unchanged. `code`'s description rewritten around a failure-diagnosis
  block differentiating error/errtrans/stack/runtime with a steering
  rule; a lockstep test pins modes ↔ handlers ↔ description.
- [x] **mtime-keyed read cache (read half landed; search half re-homed to
  Bet 06).** (DONE 2026-10-31 for reads) `internal/agent/readcache.go` +
  executor hook: an exact re-read (same resolved path, mtime, and
  offset/limit) collapses to an "unchanged" note with a taught escape
  hatch (`refresh: true` param on `read`) — but ONLY while the tool
  message carrying the earlier content still rides in the live context
  (PathEntries walk). That liveness check is what the design note
  demanded: post-compaction, the folded-away read re-serves full content,
  so the note can never point at content the model no longer has.
  Elided reads keep the original message as the liveness anchor, so
  elision chains stay honest. Pinned by a full-loop test (read → note →
  refresh → note → changed-file → full) plus compaction and
  conservative-edge tests. The SEARCH half (mtime-keyed incremental index
  so `search`/`graph.OpenGraph` stops re-walking the workspace per
  query) is the Bet 06 "Incremental indexes" item — same machinery,
  memory-scale payoff; re-homed there rather than half-built here.
- [x] **Test-isolation debt (found 2026-10-31, pre-existing).** (DONE
  2026-10-31) The parallel flakes were a real production race, not test
  pollution: memstore's `doWrite` acked before `tx.Commit()`, so a reader
  on another pool connection could miss a just-acked write (WAL readers
  cannot see uncommitted rows) — fixed, and proven by revert (the old
  code fails `-count=25` within 4 runs; the fix survives 200+ including
  `-race`). The agent package leaked 40 JSONL files per run into the
  real `~/.a1` through the global project singleton — TestMain HOME pin;
  the 7 webserver tests now run hermetically on a fixture config (no
  skips, no fresh-machine failure); two more same-pattern leaks fixed in
  `session/memory` and `project` tests. Residual, recorded honestly:
  `internal/tools` package tests still resolve the global project against
  the real home (leak-shaped but currently green) — a same-pattern
  TestMain sweep is a 30-minute follow-up.
- *(Regenerate / edit-to-branch / delete-session are already tracked
  under Web GUI polish below.)*

### Phase 2 — Make autopilot real (DONE 2026-11-01; second-order Bet 02 items deferred to their owning bets)

#### Bet 02 — The Senses: multimodal tool results (M) — DONE 2026-11-01

`browser_read` returns a screenshot *path*; the model is blind to its own
browser. The plumbing for image blocks exists on the way in (`ImageEnabled`
gating for user-attached images) — tool results are text-only on the way out.

- [x] **Content blocks on tool results.** (DONE 2026-11-01) `Images
  []llm.Image` on `tooldef.Result` — the same ride-along shape the
  user-attach path already uses, so no parallel block type was needed; the
  executor passes them on the tool message gated on the active model's
  `ImageEnabled` (rebound on model switch) and strips them for blind
  models. Anthropic tool_result blocks carry text+image content arrays
  natively; other providers keep the text.
- [x] **`browser_read` screenshot → image block** (DONE 2026-11-01).
  Downscale via `internal/util/image.FitForModel`: box-filtered halving,
  ~1 MiB cap, 1600px width, 320px floor. Path stays in the transcript
  content; tools consult `tooldef.ImageEnabledFrom(ctx)` to skip the
  encode work for blind models. Capture rode unchanged across the Bet 09
  engine swap, as promised.
- [x] **Evidence kind `screenshot:<name>`** (DONE 2026-11-01). First
  observation stores a decodability-checked baseline; later captures diff
  (≤1% of pixels may differ by >16/255 per channel; size changes fail).
  Baselines live beside `evidence.jsonl` under `screenshots/`; the ratchet
  and zero-rank gate apply unchanged.
- [ ] **Second-order, deferred to their owning bets:** `code vuln`
  highlights (needs a region-mapping story on the vuln tool), fetch
  og:image previews (cheap once a tool wants it — the ctx image gate
  exists), `document` returning a rendered first page as proof (rides the
  Bet 09 daemon's screenshot artifact). None block the phase.

#### Bet 03 — The Body: OS-level sandbox for bash (M) — DONE 2026-11-01

Bash risk management is a regex allowlist + APPA policy — good governance,
zero containment. "Capability first, control second" is the philosophy, but
control currently means asking nicely. The ultimate harness runs hostile code
on purpose: fuzzed inputs, untrusted repos, agentic experiments.

- [x] **Sandbox profiles.** (DONE 2026-11-01) macOS: `sandbox-exec`
  seatbelt profiles (compiled as a pure string builder, executable only on
  darwin). Linux: Landlock — pure syscalls through the already-present
  x/sys dependency, applied on a locked OS thread immediately before
  fork/exec so the child inherits the ruleset and the harness stays
  unrestricted. bubblewrap dropped (Landlock needs no external binary);
  per-job Docker stays unbuilded for the paranoid tier — a follow-up if
  the walls ever prove porous.
- [x] **Profile compiler: APPA policy → sandbox manifest.** (DONE
  2026-11-01) `sandbox.Compile(workspace, network, sensitiveDeny)` —
  read+execute everywhere, writes confined to the workspace plus
  well-known caches (go/npm/cargo/bun, temp, selected `~/.a1` subdirs),
  sensitive deny prefixes never widened, network egress denied by default
  (`permissions.sandbox_network: allow` opts out).
- [x] **Verdict-gate integration.** (DONE 2026-11-01) `permissions.sandbox:
  auto|on|off` (default auto — on when the host supports it; the config
  server mirror round-trips it). When enforcing, `checkBash` auto-allows
  Ask-eligible commands — the deny list still fires first and readonly
  sessions keep asking (their contract is writes-nothing). Foreground and
  background bash ride the sandbox; the user's own `!cmd` does not.

Quick win:

- [x] **XLSX / PPTX / CSV in `document`.** (DONE 2026-11-01) The unused
  pptx constant finally works; xlsx and csv joined it over gooxml (the
  DOCX dependency — no new module). Pipe tables in content render as real
  grid rows.

#### Bet 09 — The Hands: a real browser via BetterWright (M, user-prioritized) — DONE 2026-11-01

User directive (2026-11): the in-process playwright-go engine is hated —
"can't do my homework reliably." Fair: fresh browser per profile with
zombie-process guards (the struct defends against playwright.Run()
"browser is already running" lingers), raw content dumps, and captcha
death on datacenter IPs.
[BetterWright](https://github.com/BetterWright/betterwright) solves exactly
this: a persistent browser daemon (Bun-managed, checksum-pinned
BetterChromium) that keeps tabs and page state alive between calls, compact
page observations, configurable network controls, trusted credential
filling, and human handoff for steps that need a person. CLI, stdio MCP
server, or JS SDK; the browser runtime needs no model or API key.

- [x] **Adapter, not a rewrite.** (DONE 2026-11-01) `internal/tools/
  browsertool` kept its whole tool surface and the user-opened gate; the
  engine underneath is `internal/bw`, a thin CLI client. The model sees no
  regression (the "browser is not open" contract, per-call
  `/tmp/a1-screenshot-*.png` paths, the 12K content cap, proxy state
  machine — all preserved, all still tested).
- [x] **Transport decision: CLI subprocess.** (DONE 2026-11-01)
  `betterwright run --session a1 --profile <p> -c "<js>"`, one JSON
  envelope back; the MCP stdio path stays available later if a call needs
  mid-run events. playwright-go left go.mod — the lean-deps constraint
  holds; BetterWright is an optional external runtime, not a Go
  dependency.
- [x] **Discovery/download via `internal/toolmanager`.** (DONE 2026-11-01)
  `BunTools`: version-pinned `bun install -g betterwright@2.9.0`
  (`InstallBunTool`), with `betterwright setup` documented as the explicit
  browser-download step — a 150+ MiB fetch is never an install side
  effect.
- [x] **Profiles → sessions whose cookies survive.** (DONE 2026-11-01) a1
  profiles map onto BetterWright profiles (own identity, own cookie jar,
  own daemon, on-disk under `$BETTERWRIGHT_HOME/browser/profiles`); the
  default profile maps to the shared one; `close` keeps logins on disk.
- [x] **Human handoff → APPA ask bridge.** (DONE 2026-11-01) Envelope
  `challenges` become `tooldef.Result.Handoff`; the executor routes it
  through the permission ask flow — approved or declined-with-feedback,
  both as model-facing notes. Headless executors degrade to a
  question-tool hint.
- [x] **Search fallback rides on this** (DONE 2026-11-01). The router has
  ordered fallbacks now: domain engines → DDG Lite → the BetterWright
  browser engine (registered only when the CLI resolves). One wrinkle
  from reading the real daemon's code: BetterWright blocks automation of
  the Google/Bing/DuckDuckGo search UIs on its managed browser, so the
  browser engine (and the in-browser `search` action) drive Mojeek — an
  independent index with no aggressive bot walls. The DDG HTTP scraper
  still runs first on every host.

### Phase 3 — Scale and learn (DONE 2026-11-01, one item deferred)

#### Bet 04 — The Fleet: multi-level orchestration with shared contracts (M)

Depth 1, no queues, prose handoffs. The ultimate harness decomposes "refactor
the payments module" into a fan-out where workers share one finish line — and
the contract machinery is already that finish line waiting to be task-scoped.

- [x] **Depth 2-3.** `ForkChildGate` already composes (child forks parent
  label). Add per-depth step budgets so grandchildren are cheaper than
  children; explore roles keep the no-nesting default.
- [x] **Queue with backpressure instead of `ErrBusy`.** Spawn returns a
  queued job id; slots drain FIFO. Channel-based waiting list, ~50 lines.
- [x] **Task contracts.** `turnContract` resets every `Loop()`; promote to a
  task-scoped contract keyed by task id so a fleet inherits the parent's
  check ids. The add-only ratchet already prevents weakening.
- [x] **Structured handoff beyond 12KB text.** Child result.md + evidence
  receipts + read/modified file lists (compaction already tracks fileops)
  merge into parent context as a structured block, not prose.
- [x] **Warm worker pool.** Persistent children that skip repo re-reading;
  their session JSONL already persists — reuse it.

#### Bet 08 — The Vault: one durable store (M, user-prioritized)

User directive (2026-11): the memory/persona substrate works, but the storage
under it is jank — sessions, memory banks, and evidence receipts are JSONL
files, personas are a JSON registry, and every persona gets its own semantic
SQLite file. Two and a half persistence systems and a .a1 folder that
accumulates files nobody can delete. Everything moves into one SQLite
database: rows instead of files, DELETE that works, one file to back up.
modernc.org/sqlite is already the dep (FTS5 included, pure Go) — this is
consolidation, not new machinery. Independent of phase order; Bet 06's
episodic store and Bet 01's job resume both build on it.

- [x] **One DB, scoped tables.** `~/.a1/a1.db` with namespaced tables
  (sessions project-scoped, personas user-scoped, memory persona-scoped)
  vs per-project DBs — one file wins for backup and delete simplicity;
  decide once and write it down in `doc/project-layout.md`.
- [x] **Sessions: JSONL → rows.** `internal/session` (Manager, load/apply)
  swaps its backing behind the existing interface; entries keyed
  (session, seq, parent) preserve the tree; replay-on-load becomes a
  SELECT, appends become INSERTs in one transaction. `bench_test.go`
  stays the perf gate.
- [x] **Personas → rows.** The `personas.json` registry, `<id>/memory.jsonl`
  banks, and `memory/semantic/<id>.sqlite` (one SQLite file PER persona
  today) fold into the one DB; `internal/persona` keeps its API.
- [x] **Memory unification.** The JSONL `memory.Bank` and the SQLite
  `memstore.Store` are two parallel memory systems — the "two databases"
  jank. One table family, one retrieval path.
- [x] **Evidence receipts → rows.** `evidence.jsonl` keyed by session; the
  ledger's add-only ratchet and replay pruning move from append-only-file
  convention to transactional code in `internal/evidence`.
- [x] **Deletability end-to-end.** Delete session/persona = DELETE with FK
  cascade (entries, receipts, memory rows); no orphaned files, no
  adoption shims — the legacy-bank adoption in `persona.MemoryBank`
  exists because of exactly this class of problem.
- [x] **One-shot migration.** Importer runs on first open: .a1 JSONLs and
  per-persona SQLite files become rows; `config.yaml` and skills stay
  files, everything else is rows.

#### Bet 06 — The Diary: episodic memory + wired-in learning (M)

Semantic facts ≠ experience. "What did I try last time this flaky test bit
me?" has no answer today. Three designed-but-unwired loops (first three
items — each an under-a-day quick win) plus the bigger episodic store.

- [x] **Increment `cool_passes`.** The salience-cooling term
  σ·salience·γ^coolPasses in `memstore/scorer.go` is dead code — nothing
  ever increments the counter. A background decay pass fixes memory
  staleness.
- [x] **Wire session valence.** Hardcoded 0 at inlet and outlet; the
  mood-congruency override in `selectBundle` is unreachable. Compute it in
  the outlet from turn outcomes.
- [x] **Call `memfsck`.** The MEMORY.md reconciler has no production
  caller. Run it at session start, as its doc says.
- [x] **Episodic store.** Index compaction summaries + evidence receipts per
  project as retrievable episodes (both are already written — join them).
  Recall injects "last time this happened" via the existing memory inlet
  splice point. Lands as rows in the Vault (Bet 08), not another file
  format.
- [x] **Dedicated memory-ops model per persona — a persona-editor toggle
  (user-reported 2026-11).** New `MemoryModel` field on `persona.Persona`
  beside the existing `Model` override: unset = main model (default), set =
  consolidation traffic routes to that configured model. Surfaces: toggle +
  model picker in the web persona editor (`internal/webserver/personas.go`
  + frontend) and the TUI persona command. The outlet.go comment already
  promises the engine half ("v2: dedicated client per persona.Model"); an
  8B model doing memory ops frees the main LLM and cuts latency.
- [x] **Incremental indexes.** (deferred to Phase 4 window, DONE there
  2026-11-01: `graph.OpenGraphCached` keys parsed imports by (mtime,
  size) in a lazily-created vault `graph_files` table — parse only
  new/changed, delete vanished, the previously-dead `rescan` input on
  search/graph/batch/testimpact force-rebuilds; the vector store moved to
  a project-scoped SQLite `vec_chunks` family with external-content FTS5
  + triggers, mtime+size+model-keyed incremental re-embedding, and hybrid
  retrieval — dense cosine over normalized BLOBs fused with an FTS5
  keyword leg via reciprocal-rank fusion. FTS5-based, inside the vault,
  no new daemon, no new deps.)
- [x] **SOUL.md lives in the persona prompt — wire it fully (user-reported
  2026-11, supersedes the earlier delete decision).** The persona prompt IS
  the soul: a fenced `soul` block in the persona's system prompt (the §11
  keys — d, tau, sigma, k, …) parses into `memstore.Params` at session
  start. A nostalgic persona (d 0.35) forgets slowly; an eager one
  (tau -1.5) recalls more. The wiring point already exists —
  memory_inlet.go:64 hardcodes `memstore.DefaultParams()` into
  `persona.SemanticStore`; feed it the parsed block instead. Absent block =
  defaults, so every existing persona is unchanged. The persona editor
  grows a temperament hint; golden scorer tests pin the parse. The dead
  config path becomes the feature it always described.

### Phase 4 — Go ambient, stay measured (DONE 2026-11-01)

#### Bet 01 — The Clock: durable daemon + scheduler (L)

An agent that sleeps when your terminal closes is a tool. a1 is closer than
it looks: session JSONL + `evidence.jsonl` are already replay-on-load — the
missing piece is a process model, not a data model. Biggest lift and biggest
identity change; do it last, when everything it schedules is already
governed and measured. Every other bet compounds on top of this one.

- [x] **`a1 serve`.** Headless engine host + HTTP API with token auth.
  Reuse the webserver's `liveFor()` engine cache and per-session turn mutex
  — the multi-session machinery already exists behind the loopback GUI.
  (DONE: `cmd/serve.go` + `webserver.StartServe` — bearer token via header
  or `?token=` for SSE, constant-time compare, non-loopback binds refuse
  to start without a token, Start refactored into shared
  newServer/listenAndServe helpers with zero behavior change.)
- [x] **Durable job state machine.** Replace `recoverStale()`'s
  fail-on-restart with pending → running → terminal, persisted in
  `<job>/meta.json` (post-Bet-08: a jobs table in the Vault). Resume =
  rebuild engine from the session store + replay evidence receipts, then
  continue the prompt.
  (DONE: `RecoverRequeue` parks non-terminal jobs as `pending` with the
  attempt counter bumped; `Manager.Resume` re-runs a job through its own
  prior session (`resumeJobSession` — engine rebuilt from the session
  store, receipts replay through the same ledger scope); `ResumePending`
  is the daemon's boot call; metas mirror into a vault `jobs` table
  (lazy DDL + one-shot importer for pre-vault job dirs); Wait/Cancel
  handle parked jobs explicitly. All agent-glue managers switched to
  Requeue — nested managers keep RecoverIgnore.)
- [x] **`schedule` tool + config grammar** (cron + fixed_rate + one_time).
  Wake-ups inject prompts through the same `EmitUserInput` path extensions
  already use — no new loop entry point.
  (DONE: `internal/schedule` — hand-rolled 5-field cron parser, vault-row
  persistence, `Sync` folds config-declared schedules in (config wins id
  conflicts); ONE `schedule` facade tool with a create/list/cancel action
  enum (anti-goal: no tool-count growth); fires spawn jobs through the
  fleet manager — prompts enter via engine.Loop/EmitUserInput — with
  `schedules:` grammar in config.yaml failing loud on malformed triggers.)
- [x] **Scheduled wake-ups default to headless-strict**; task-scoped
  contracts (Bet 04) give overnight work a finish line that survives.
  (DONE: wake-ups default to role=worker (headless-strict per
  `SpecForRoleAt`); the schedule tool inherits the live task contract at
  create time so the finish line rides into the job; serve boots
  headless-strict for its own turns too.)

#### Bet 07 — The Mirror: eval harness + cost telemetry (M)

You cannot improve what you don't measure. Telemetry is an in-memory recorder
with three event kinds, no persistence, no export. The irony: a1 sits on a
labeled dataset — session trees with evidence receipts are ground truth for
task success. Every completed session is a free eval sample nobody reads.

- [x] **`a1 eval`.** Replay stored sessions (JSONL trees) with mutated
  configs — tool subsets, prompt variants, compaction settings — measuring
  rounds-to-green, rollback rate, stall rate, token spend. The ledger
  already defines "green"; this joins two files a1 already writes.
  (DONE: `internal/eval` + `cmd/eval.go` — offline scorer joins session
  trees + evidence receipts into rounds-to-green / rollback rate / stall
  rate / token spend / compaction counts, `--json` mode; `a1 eval replay`
  re-runs a stored session's prompt under mutated configs (tool subsets,
  round caps) and scores before/after.)
- [x] **Golden-set CI.** Pin ~20 tasks across go/rust/python, run nightly,
  gate PRs on regression. Protects the lean core while the surface grows —
  the guardrail for every other bet.
  (DONE: `internal/eval/golden` pins the suite with inline hermetic
  fixtures; `a1 eval golden` runs it against the configured model and
  records `eval_runs` rows in the vault; no model → clear notice, exit 0;
  nightly `.github/workflows/golden.yml` + `make eval-golden` drive it,
  with `scripts/golden-config.sh` writing the CI config skeleton.)
- [x] **Persist telemetry to SQLite** (modernc.org/sqlite is already a dep):
  per-turn cost by model, per-tool latency/error rates, compaction
  frequency, evidence pass rates. Dashboard panel in the web GUI.
  (DONE: `internal/telemetry/store.go` — buffered single-writer sink; the
  executor records per-tool latency + session id live; turn token totals
  materialize from durable session entries; `GET /api/telemetry` + a
  server-rendered `/stats` dashboard page expose both without a frontend
  rebuild.)
- [x] **Skill routing as a bandit.** Trigger keywords that correlate with
  failed turns get demoted; skills stop being static files and start being
  a bandit problem.
  (DONE: `internal/skillstats` — picks recorded when the executor sees a
  read under the skill path, outcomes land via eval turn materialization,
  Laplace-smoothed win rate reorders the `list_skills` catalog; chronic
  losers sink to the bottom annotated "(recently unhelpful)" instead of
  being silently invisible.)

Quick win:

- [x] **Identity hygiene.** `PHI_*` env vars → `A1_*`, upstream
  pulseaiclub/pli/xui links in released changelogs, version drift between
  the README header and `internal/version`. Fork debt showing through.
  (DONE: `A1_*` env names with a one-cycle `PHI_*` fallback that warns on
  stderr when consumed; the `phi_` release-asset prefix that 404'd every
  `a1 update` is fixed and pinned by test; both readme headers carry
  `# A1 v2.0.1` again; `[Unreleased]` compare link → this repo; Rust SDK
  README URL, Go example's `a1.New`, SDK error prefix, and doc/script
  comments de-phied. Released-changelog lines stay frozen by policy;
  `pli`/`xui` are dependencies, not fork debt; the half-done ext/rust
  crate rename (a1-ext vs phi_ext sources) remains the known follow-up.)

### Anti-goals — what NOT to build (permanent)

"Stay lean and fast" is load-bearing (AGENTS.md). The fastest way to ruin a1
is accepting every feature. These stay out:

- **No extension marketplace.** GitHub owner/repo installs with SHA-256
  pinning are fine at this scale. A registry is a business, not a feature.
- **No multi-user RBAC.** APPA's reader sets are modeled for it, but a1 is a
  personal harness; multi-user turns every design decision into a committee.
- **No plugin interpreter, ever.** Removing yaegi for PXB native binaries was
  the right call. Subprocess isolation with a 16-byte binary protocol beats
  in-process convenience.
- **No web-scale vector DB.** An index that stays inside the existing
  SQLite is plenty at personal scale. FAISS servers and ANN daemons violate
  the lean-deps constraint for zero felt benefit.
- **Don't grow the always-on tool count.** The 45→18 collapse was correct.
  New capability goes behind mode enums on facades or behind MCP meta-tools —
  the model's tool-selection head is the scarcest resource in the system.
- **No enterprise config surface.** `~/.a1` files + config.yaml is the whole
  configuration story.

## Backlog (deferred — revisit later)

User-reported items below are parked; the evidence loop lands first.
(The Web GUI items are low-touch — the GUI is rarely used.)

### Persona correctness (from the persona-override delivery) — ALL LANDED

- [x] **Context-window recalc on persona switch.** DONE. `applyPersonaSettings`
  now re-seeds the runtime window via `resetWindowForModel` in BOTH
  directions — a persona with a model override applies that model's
  configured window on the switch, and switching back to the default persona
  restores the base model's window (the previous persona's window used to
  leak forward). `stateView` prefers the live engine's resolved window and
  falls back to the ACTIVE model's configured window before the first turn
  resolves. Proven at both levels: engine
  (`engine_persona_switch_test.go`) and webserver
  (`persona_switch_proof_test.go` — switch → state payload's
  `contextWindow` equals the persona model's window, switch back restores
  the base window; mutation-tested).

- [x] **Permission-mode application on persona switch.** DONE. The gap was in
  `applyPersonaSettings` (engine) + `engineOptions` (webserver): the mode
  override now applies on every switch, and a persona WITHOUT an override —
  and the default persona — restores the engine's `basePermissionMode`
  captured at construction (`WithBasePermissionMode`). The webserver's
  setter (`s.setPermissionMode`) rebuilds the gate and installs it on every
  live engine, so the flip is behavioral. Proven webserver-level
  (`persona_switch_proof_test.go`): switch to a `readonly` persona → the
  server's gate AND the live engine's own gate deny a write inside the
  workspace; switch back → the base mode is restored and the write is
  allowed again; the state payload's mode (UI pill) follows. Mutation-
  tested (killing the setter call turns the test red).

- [x] **End-to-end memory verification.** DONE — and the suspicion was right.
  Turns ending via verdict-stop, executor-stop, tool-budget-trip, or
  budget-terminal-stop never consolidated (the outlet only lived in the
  no-tool-call branch); all terminal paths now fire `consolidateTurn`. The
  inlet's framing is third-person ("memories about the user"). Proven at
  the loop level (`memory_loop_test.go`): a tool-carrying turn consolidates
  through the real Loop (scripted SSE model), a budget-tripped turn still
  consolidates, and a NEW session's first request carries the injected
  memory as a system message immediately before the user prompt. Direct-
  call e2e in `memory_e2e_test.go` (write → retrieve → inject, persona
  scoping, non-personal skip, no-embedder degradation).

- [x] **Memory toggle follows the persona** (added mid-batch). DONE. The
  persona's `memory_enabled` is now the single effective gate in
  `Engine.MemoryEnabled()`: an explicit persona setting wins over the
  surface's raw toggle in BOTH directions, a persona without one inherits
  the toggle, and "off" silences the auto inlet/outlet AND the
  remember/recall tools together. `applyPersonaToEngines` (web) rebinds the
  shared bank against the effective state on every switch/edit/toggle; the
  TUI's `SetMemoryEnabled` writes the persona when it owns the setting and
  rebinds the live engine either way (it used to only move an atomic
  nobody read back). Matrix pinned engine-level (`memory_loop_test.go`),
  switch behavior pinned webserver-level (`persona_switch_proof_test.go` —
  chip + effective state + tool registration move with the persona in both
  directions; mutation-tested).

- [x] **Fresh engines adopt the active persona** (found while proving the
  above). A brand-new engine built while a persona with overrides was
  already active (switched yesterday, new chat today) came up on the global
  model and mode — only memory was compensated at build time. `liveFor`
  now applies the active persona's settings to a freshly built engine
  after releasing `s.mu` (the mode setter re-enters the lock through
  `applyToEngines`; a deferred func ordered after the unlock keeps the
  application lock-free). Pinned by `TestFreshEngineAdoptsActivePersona`
  (model + window + mode + memory + gate behavior from the first turn).

### Web GUI polish (user-reported)

- [x] **Source pills for `fetch` search results.** Live SSE tool events
  carry `sources`, and the pills now render as one aggregated row at the
  END of the entire turn (not scattered under individual tool rows),
  sorted latest-search-first with per-search rank order preserved, deduped
  by URL, rank-badged + favicon chips with a staggered entrance.
  Reload fix: `SessionMessageEntry` gained a persisted `sources` field —
  the engine stashes each run's Sources by tool_use id and appends tool
  results through `AppendToolResult`, so `ReplaySnapshot` restores them
  and a reloaded session re-renders its pills
  (`internal/session/branch_test.go`:
  `TestToolResultSourcesSurviveReload`). Live smoke PASSED on the user's
  machine: a real arxiv/DDG search turn rendered 10 rank-badged pills
  with favicons in one aggregated end-of-turn row.

- [x] **Copy button on markdown code blocks.** Fenced blocks render
  through a custom marked renderer: a `.md-code` wrapper with a header
  (uppercase language label + copy button). The button is wired by ONE
  delegated click listener on the transcript container (the markdown is
  innerHTML, so per-block Preact handlers can't attach); a successful copy
  swaps the label to "copied" in moss green for a beat.

- [x] **WebUI eats assistant text before tool calls.** FIXED (previous
  wave). msgId per stream round segments multi-round turns into per-round
  bubbles; a session-less tab adopts any stream via `sessionRef`; tool
  rows dedupe by `toolUseId`. Tests:
  `internal/webserver/round_segmentation_test.go` +
  `multiround_sse_test.go`.

- [x] **Copy buttons on LLM and user messages.** Every bubble gets a quiet
  hover action row (copy; regenerate on the turn's last sealed assistant
  bubble; edit on user rows). Touch users see the actions at half opacity
  since group-hover can't fire.

- [x] **Regenerate message button.** `POST /api/regenerate`: the server
  rewinds the session cursor to the last user message's parent
  (`Manager.SetLeaf` — an append-only cursor move; the old response chain
  stays in the tree as an abandoned branch) and re-runs the same prompt as
  a fresh sibling, streaming over the same SSE subscription. 409 when a
  turn is in flight (fail fast, never queue a re-roll behind the stream
  it replaces). Test: `turns_test.go`
  (`TestRegenerateReplacesLastTurnOnPath`,
  `TestRegenerateRefusesWhenTurnInFlight`).

- [x] **Edit user message → new branch.** `POST /api/edit {sessionId,
  userIndex, prompt}`: same rewind primitive, targeting any past user
  message (identified by its 0-based position among user rows — stable
  across live + replayed transcripts, unlike entry ids which live turns
  mint server-side unseen). Everything after the edit falls off-path; the
  turn re-runs from the edited text as if it had been said then. Test:
  `TestEditUserMessageForksBranch`.

- [x] **Delete session button.** `DELETE /api/session?id=` (guarded):
  evicts the live engine (409 if a turn streams), 404s unknown ids,
  unpins, removes the vault rows (FK cascade) AND the jsonl file including
  pre-migration shadows. Sidebar rows get a trash affordance with an
  inline two-step confirm. Tests: `sessions_test.go` (file+pin+engine
  removal, 404, 400).

- [x] **Settings cog → config.** The sidebar header has a cog that opens
  `/settings` (the same editor `a1 config` and the TUI's /config surface).

- [x] **Smart session names via the memory-ops model.** After a session's
  first completed exchange, the server asks the persona's MemoryModel
  (falling back to the main model — `Engine.OpsClient`) for a 2–6 word
  title and stores it in `titles.json` beside the session data (works for
  file and vault backings; corrupt file degrades to no titles). The
  sidebar/topbar overlay the title over the first-message preview. Opt-out:
  `web.auto_name_sessions: false` in config.yaml. One attempt per session
  per server run (failures don't retry every turn). Follow-up fixes
  from the live GUI pass: vault-backed sessions showed their hex id as
  the label (vault rows never filled `Preview`), and the persisted
  attempt-marker let one failed naming call silence naming for that
  session forever — both fixed (see CHANGELOG).

- [x] **Memory ops model in the persona editor.** The chat PersonaManager
  and the /settings persona panel both expose the memory-ops model
  dropdown (model list from `/api/models`), with a mem-ops chip on the
  detail view.

- [x] **Sexy markdown + LaTeX.** KaTeX renders `$…$` / `$$…$$`
  (throwOnError:false, raw-text fallback); code blocks got the header
  treatment; task-list checkboxes are themed; external links open in new
  tabs with noopener.

- [x] **Scrollback survives compaction.** The web transcript now renders
  `Session.AllEntries()` (the full leaf-to-root branch) instead of the
  compaction-cut context path — pre-compaction messages stay scrollable
  above the cut marker; only the model's context stops at the cut.

- [x] **Settings panel refreshes after every change.** The mode buttons
  fire-and-forgot before; every section now reloads the catalog after its
  write.

- [x] **Config page revamp.** JEV retitled with a "placeholder — currently
  does nothing" note; the embedding model moved to its own one-place
  **Embeddings** panel (semantic search keeps only the toggle; the YAML
  keys are unchanged); a **Web GUI** panel (smart session names); a full
  **Personas** section (list/create/edit/delete/activate + memory-ops
  model — the TUI's create-default stub is removed in favor of it); an
  **MCP servers** section (user-file add/remove via `GET/PUT/DELETE
  /api/mcp`; project-defined rows are shown read-only with a pointer at
  the repo file); persona + MCP sections degrade to a hint under
  standalone `a1 config` (no chat API on that server). Bonus fix: editor
  saves can no longer wipe `checks:` / `schedules:` — they round-trip with
  the same carry-over protection `tools:` has.

- [x] **More sexy minimal animations.** Pill stagger-in, hover lift +
  active press on pills and action buttons, code-copy state transition,
  reduced-motion collapse for all of it.

- [x] **Tool-call clusters for same-tool barrages** (second live-session
  find, 2026-11-02, user-reported with a screenshot of 15 stacked fetch
  rows). Consecutive PURE-TOOL rounds (rounds where the model said
  nothing — the heads-down fetching phase) merge into one visual group,
  and consecutive same-name tool runs — inside that group or inside a
  text bubble's list — collapse into a single "fetch ×15" cluster row:
  status icon (spinner while any run is live), seal ×count badge, the
  newest call's detail as a live ticker, chevron expanding a numbered
  sunken list of the individual calls. The streaming status text
  renders under a still-streaming merged group; a turn ending on a
  tool-only round keeps its end-of-turn source row and regenerate
  button. Pure-render change (ChatArea.tsx + styles.css), no protocol
  or server changes; browser-verified against a synthetic heavy turn.

- [x] **Source pills cap at 10 + "+N" sources sidebar** (same session —
  a 103-source turn rendered 103 identical chips). The end-of-turn row
  shows the first ten pills and folds the rest behind a dashed "+N"
  circle badge that opens a right sidebar (Qwen-style) listing every
  source as a card: rank badge, favicon, host, result title, DDG
  snippet — the whole card is the external link. Escape / backdrop /
  X close it; the panel resets on session switch; reduced-motion
  collapses the cascades. Verified in light + dark themes and a 400px
  viewport.

- [x] **Every tool call renders as the dropdown; whitespace rounds join
  the merge** (third live-session find, 2026-11-02, user's screenshot of
  the fetch barrage: "great direction, just off execution"). Two gaps in
  the cluster pass: models that emit whitespace-only text alongside
  tool_use blocks (" \n" + fetch) dodged the pure-tool merge — every
  round rendered its own dropdown, its own usage line, and an orphaned
  copy button under an empty bubble; and single calls still rendered as
  plain bordered rows instead of the dropdown. The merge now judges
  "said nothing" after trimming text AND reasoning; ALL tool runs —
  singles included — render as the ToolCluster dropdown (latest call's
  detail as the preview, no ×1 badge for one call; mixed-name groups
  summarize per-name, "fetch ×2 · read ×2", and keep the name on every
  expanded row); merged rounds sum their usage into ONE readout above
  the dropdown (tok/s dropped — per-round by definition); expanded rows
  wrap their detail instead of truncating (expanding is how you read the
  full query the header clipped). Also fixed the merged group's
  bookkeeping for parallel calls: `last` was `start + toolCount - 1`,
  wrong the moment one round carried two calls — sourcesAt / regenerate
  lookups mis-keyed. Pure-render change (ChatArea.tsx + one styles.css
  comment); browser-verified on a synthetic transcript mirroring the
  screenshot (two 4-fetch barrages split by a text round, a mixed
  fetch/read bubble with an error call, a lone read, 23 sources) in
  light + dark.

- [ ] **Jank hunt against the live GUI.** The known list above is done;
  the remaining sweep is by eye on a real session (needs the user's
  machine: `make test`, `make fmt`, `make build` — web-install now always
  syncs node_modules, so a stale install self-heals instead of failing
  the vite build on missing imports — then a live DDG search turn + a
  reload to see the pills land twice).

  First live-session finds (2026-11-01, all fixed — see CHANGELOG
  Unreleased → Fixed): the settings panel's Agent-memory toggle rendered
  OFF after a flip whenever the active persona owned the setting (the
  catalog read the raw global knob while everything else reads the
  effective one); the same memory-flip pass silently reverted live
  engines to their boot-time model and permission mode (baseModelCfg /
  basePermissionMode never moved with surface changes — footer and
  settings panel disagreed about the model); and toggle knobs were
  near-invisible on dark-mode OFF tracks. Second live-session finds
  (2026-11-02, both fixed — see the two items above): the 15-fetch wall
  → tool clusters; the 103-pill wall → pill cap + sources sidebar.
  Static re-audit of the
  "pills land twice" suspect found no duplication path (SSE dedupes by
  toolUseId, the transcript JSON and the SSE payload share one emitter,
  turnSources dedupes by URL, and the hub has no replay buffer — reload
  re-renders from the transcript alone), so that item stays a
  by-eye check on the user's machine.

### Housekeeping

- [ ] Anything above marked "low priority" can slip; the message-eating bug
  and the persona items have all landed.
