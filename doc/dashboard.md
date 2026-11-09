# The Dashboard

The a1 web GUI's landing surface is a **widget grid over the user's own
data** — not a chat scaffold with cards bolted on. The dashboard epic
(chunks 0–8) built it as eight landings, each carrying one ruling that
the next chunks inherited rather than renegotiated.

| Audience | This document |
| --- | --- |
| Users | What the widgets are, what they read, how to turn them on |
| Contributors | The gate rulings, the surface map, the test doctrine |

---

## The surface map

| Widget | Backend | Writes | Reads |
| --- | --- | --- | --- |
| Usage heatmap | `GET /api/usage/daily` | — | vault (sessions, entries, tool runs) |
| Pinned sessions | `/api/pins` + `/api/sessions` | — | vault |
| Calendar (month grid) | `GET /api/personal/calendar[?days\|/range]` | agent tool only | `calendar_dir` (.ics files) |
| Kanban (board) | `GET/POST/PUT/DELETE /api/personal/kanban` | web AND agent | vault |
| Mail (accounts) | `GET /api/personal/mail/*?account=` | NOBODY (mail data) | `mail_dir` + `mail_accounts` (Maildirs and IMAP servers) |
| Mail accounts (configurator) | `GET/PUT /api/personal/mail/accounts`, `POST …/test` | web — the keyring, never the letters | writes `personal.mail_accounts` in config.yaml |
| File browser | `GET /api/files/list\|read`, `PUT /api/files/write` | web (guard()ed) | full system |

The composer stays docked bottom-center of the dashboard; submitting from
it is the old welcome flow verbatim, so no new routing exists in an app
whose Overlay union was already the navigation.

## Chunk 7 — the tightening pass (2026-11-08)

The user's own screenshots drove this one. The session drawer collapsed by
default at EVERY viewport (it had floated at z-50 above the z-40 modals —
the overlap fix is the z-ladder: page < edge zone z-20 < drawer z-30 <
modals z-40 < toasts z-50) and opens from a left-edge hover zone with a
debounced close, so a pointer crossing the gap never flickers it. The
file-browser toggle moved into the composer, left of the send button — the
pane it splits the screen for is the thing the eye is already on. The
second dodec beside the composer is gone (the hero is the one mark). The
heatmap autoselects today, so the day-filter readout opens filled instead
of blank. The calendar became the ask's month grid — every day a button,
the selected day's events listed under the grid, one range fetch per
visible month. The file editor now highlights WHILE editing (the classic
overlay: live-highlighted pre under a transparent-text textarea, scroll-
locked, capped at 256 kB past which it says so and stays plain). And mail
grew multi-account: `mail_accounts` mixes local Maildirs and IMAP servers
(read-only, BODY.PEEK, one connection per call), both transports return
the maildir package's own types so the wire shape cannot drift, and one
dead server renders its outage inline instead of blanking the surface.
The IMAP client is emersion/go-imap v2; its trio runs the library's own
in-process imapserver as the fixture and diffs the wire path against the
disk path over one shared letter set.

---

## Chunk 8 — the mail configurator (2026-11-09)

"Still can't configure email servers/accounts and app passwords" —
chunk 7's multi-account mail was real but config-file driven; this
chunk is the configurator. The mail card's not-configured state now
leads with **Add mail account**, the card header carries a gear, the
reader's rail ends in **Manage accounts**, and the command palette has
the door too — all opening one modal that edits the saved list: rows
with edit/remove, the IMAP-vs-Maildir segmented form (server, port,
username, app password with reveal, implicit-TLS toggle, first
mailbox | root path, optional name), a **Test connection** button whose
verdict is the server's own answer ("Reached it — 3 mailboxes (…)" or
the outage, inline), and one Save that PUTs the whole list.

The credential contract is the chunk's sharpest line: the GET never
returns a password (`hasPassword` instead — an editor that cannot show
a secret cannot leak one), the PUT's blank-field-means-keep resolves by
NAME for named accounts and by type-and-server FINGERPRINT for unnamed
ones (the collision the browser pass caught: two accounts saved without
names both key on the empty string, and a first-match rule handed one
the other's emptiness), landing only on a unique match so edits and
deletions cannot shuffle credentials onto the wrong account (renaming
does not carry the password — the UI says so), and the test dial
resolves keep the same way so a saved account is tested without its app
password ever crossing the wire — while persisting nothing. A successful save reloads the
live config (`onConfigSaved`), so the new account is on the rail — and
in the agent's envelope-only facade — without a restart; the dashboard
refetches behind the still-open modal. Validation is deliberately
stricter than the read path's lenient degrade (the configurator refuses
known-bad shapes with a 400; hand-edited YAML keeps degrading honestly),
the legacy `mail_dir` renders as a pinned read-only row, and passwords
land in config.yaml as plain scalars beside the model api_keys — the
file that is already the project's secret store.

---

## The gate rulings

**One master switch, server-side, always.** `personal.enabled` in
config.yaml gates every `/api/personal/*` route through `personalGate` —
403 before any handler runs. The UI reads the one ungated endpoint
(`/api/personal/status`) and fires **zero gated fetches** from locked
states; a 403 mid-flight (the switch flipped while the tab was open)
folds back to the locked card. No per-widget grants — the anti-goals
rule the block is one surface, on or off.

**Who writes where — the ruling the trio taught, in ascending strictness:**

| Surface | Web writes | Model writes |
| --- | --- | --- |
| Calendar | no | `personal_calendar` create/update, behind `personal_write` |
| Kanban | yes — dragging a card IS the product | `personal_kanban`, behind `personal_write` |
| Mail (data) | NEVER — no handler exists that could | NEVER — `personal_mail`'s Deps has no write hook at all |
| Mail (accounts) | yes — the configurator (chunk 8): `guard()`ed PUT, the keyring not the letters | no — no agent flag reaches it |
| Files | yes — the one `PUT`, `guard()`ed | (not a personal surface) |

The calendar's write face lives in the agent tool because a calendar
write is a commitment the model makes on the user's behalf; the kanban's
does not, because the human clicking their own dashboard needs no
per-model opt-in. Mail goes further than read-only: the model reads the
ENVELOPE, never the letter — `maildir.Summary` (headers, flags, sizes)
is the facade's per-message surface while `Open` (bodies) stays a webui
privilege. The one mail write anywhere is the account-list configurator
(chunk 8), and the line it draws is the chunk's ruling: it writes the
KEYRING (which servers, which credentials — configuration, the same
object `/settings` edits, with the GET never returning a password and
blank-field-means-keep resolved by name server-side), never the LETTERS
(no surface anywhere sends, moves, marks, or deletes a message; the IMAP
path stays BODY.PEEK). Every mutating verb that does exist composes
`guard()` (the CSRF doctrine) — including the files write and the
accounts save.

**Per-model opt-ins.** The agent's personal tools mount only when the
master switch is on AND the active model's `personal_read` (and
`personal_write` for writes) checkboxes are set; absent flags mean
locked. A config save rebinds live web engines (RefreshPersonalTools),
so flipping the switch mounts or unmounts tools on the next turn of an
open chat — both directions.

---

## The honest states

Every personal widget answers the same five questions the same way:
locked (switch off), not-configured (the config key spelled out), empty,
degraded (counted skips — "2 lines unparsable", never silently fewer),
and live. The usage heatmap keys on UTC calendar days via SQLite
`date()` with `json_valid()` guards around every `json_extract` — a
corrupt payload row is skipped, never fatal.

---

## Recurrence — the subset, and the claim not made

RRULE expansion (chunk 6) covers the common personal-calendar shapes:
`FREQ=DAILY/WEEKLY/MONTHLY`, `INTERVAL`, `COUNT`, `UNTIL` (both
spellings), and `BYDAY` — plain weekdays under DAILY (filter) and WEEKLY
(the day set), weekday-plus-ordinal under MONTHLY (`2TU` the second
Tuesday, `-1FR` the last Friday). Everything else — YEARLY, BYSETPOS,
BYMONTH, RDATE, RECURRENCE-ID — degrades to the first occurrence with a
warning in the event's Warnings. **Full RFC 5545 compliance is not
claimed**; `internal/calendar/recur.go`'s doc comment is the scope
statement, and the golden differentials in `recur_test.go` are
hand-computed from the calendar itself so the oracle cannot agree with
a wrong algorithm.

Semantics chosen where the RFC leaves room: occurrences generate from
the rule restricted to instants ≥ DTSTART (a non-matching DTSTART is
not force-included); COUNT counts the whole series from the first
match; UNTIL is inclusive in both spellings; expansion happens in
DTSTART's own zone (09:00 stays 09:00 wall clock across DST); WKST is
fixed at MO.

---

## The split screen and the responsive sweep

The file browser (chunk 5) is the SPA's first split screen: a persistent
right pane, sibling of `main`, not an overlay — it stays up while the
user chats. It is workspace machinery, not personal data (no
personalGate), and deliberately NOT the `/api/code` viewer's project
confinement: the ask was full-system navigation. `A1_FILES` is the env
kill-switch (the A1_MCP precedent). highlight.js is vendored the sparse
way — core plus exactly the languages the server's extension map rules,
one ruling on both sides of the wire — and its token theme derives from
the palette's own channel variables, so light and dark hold with one
rule set.

The pane's open and close are one mechanism at both viewports: it is
KEEP-ALIVE (mounted from app load, `open` is a class flip, never a
mount/unmount — the session drawer's architecture), so the exit can
animate instead of vanishing mid-frame. At `lg` the reveal is a WIDTH
transition — the pane grows from the screen's right edge and the chat
reflows around it, with the inner column at a fixed width and
right-anchored inside the clipping shell, so the content is uncovered by
the edge sweeping left and no glyph ever re-wraps mid-flight; below `lg`
the pane is a full-screen sheet and slides from the right, the way
drawers slide. Both ride the drawer's ease-out-quint curve (`.file-pane`
in styles.css). Two behaviors fall out of keep-alive and are rulings,
not accidents: reopening returns to the folder (and file, and edit
draft) the pane was left in — a pane you tuck away, not a screen you
reboot — and the composer's toggle button, which has never carried the
close button's dirty guard, can no longer silently drop a draft because
closing no longer destroys anything. While closed the shell carries
`inert`, keeping the hidden pane out of the tab order and the
accessibility tree, and the listing fetch is gated on `open` so a pane
nobody opens costs nothing.

Below `lg` the layout is honest about being a phone: the sidebar becomes
an off-canvas drawer (hamburger + scrim + Escape + pick-to-close) instead
of pinching the main column to 135px, the widget grid drops to a single
column, and the split-screen pane covers the screen. Above `lg`
everything is the static multi-column layout; the drawer state is inert.

---

## The test doctrine

Every landing ships its trio: positive (golden fixtures), negative
(hostile inputs, gate-off refusals), differential (endpoint-vs-library,
SQL-vs-Go-scan, or hand-computed goldens). The differentials have earned
their keep — the usage differential caught a UTC day-key mis-bucket, the
files differential caught a backwards sort comparator, and the calendar
windows differential flips its hand-enumerated membership whenever the
overlap semantics change (the chunk-6 all-day fix flipped it again).

| Chunk | Landed | The one-line ruling |
| --- | --- | --- |
| 0 | groundwork | One switch, per-model guardrails, the ungated status surface |
| 1 | shell | The dashboard IS the welcome state; honest placeholders, zero gated fetches |
| 2 | calendar | The web reads; the agent writes (behind `personal_write`) |
| 3 | kanban | The WEB writes — dragging a card IS the product |
| 4 | mail | NOBODY writes; the model reads envelopes, never letters |
| 5 | files | Full-system navigation, one `guard()`ed write, sparse-vendored hljs |
| 6 | polish | RRULE subset, responsive drawer, the docs you are reading |
| 7 | tightening | The drawer collapsed + edge-hover, month-grid calendar, multi-account mail, highlighted editing |
| 8 | configurator | Mail accounts editable in the app — the keyring, never the letters |
