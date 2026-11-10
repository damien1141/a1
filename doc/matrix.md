# The Matrix Bridge

`a1 matrix` is the **text-the-harness surface**: one Matrix room bridged
onto one a1 session, so the phone in your pocket can drive the agent the
terminal already does. Matrix's own SMS/Signal bridges mean "text it" works
without running a Matrix client daily.

| Audience | This document |
| --- | --- |
| Users | Config reference, the room-side commands, what the bridge will and will not do |
| Contributors | The loopback-client architecture, the concurrency rulings, the test doctrine |

## The architecture ruling: a loopback client, not a new surface

The bridge does not touch the agent's network posture. `a1 matrix` boots
the webserver **in-process on an ephemeral loopback port** and drives it
through the same chat API + SSE stream the web GUI uses:

```text
Matrix room ──╮                                   ┌─ agent engine
              │   sync long-poll   ┌──────────┐   │   (session store,
              ├───────────────────▶│  bridge  │──▶│    vault, tools —
              │   m.room.message   └──────────┘   │    unchanged)
              ◀───────────────────   loopback ──▶ │
              ╰─ m.text/m.notice     HTTP + SSE   └─ webserver on 127.0.0.1:<ephemeral>
```

Consequences that are rulings, not accidents:

- **The web port is ephemeral.** It cannot collide with a running
  `a1 web`, and nothing but the bridge talks to it. The port is printed
  for debugging and serves no other purpose.
- **The bridge is a normal API client.** Prompts are `POST /api/prompt`;
  turn completion arrives on `GET /api/events` (SSE); approvals answer
  `POST /api/ask`. No new exported surface exists for Matrix — the chat
  API is the contract, and the bridge keeps it honest the way a browser
  would.
- **The reply is read from the transcript, not from deltas.** When a turn
  finishes (`done` event), the bridge fetches
  `GET /api/transcript/messages` and diffs it against the transcript
  length it saw at the last turn. Deltas are ignored entirely, so a
  dropped SSE connection mid-turn cannot eat an answer — the transcript
  is the authoritative record.

## Config reference

```yaml
matrix:
  homeserver: "https://matrix.org"   # bare hosts get https:// prefixed
  user: "a1bot"                      # localpart or full @id
  password: "…"                      # m.login.password
  # access_token: syt_…              # …or token auth (wins over password)
  room: "#a1:matrix.org"             # #alias or !roomid:homeserver
```

- **Auth order:** explicit `access_token` → cached token in the state file
  (`~/.a1/matrix/state.json`, whoami-validated) → password login, with
  the granted token cached back so reboots do not mint a fresh device
  session every time (homeservers cap sessions and start evicting).
- **A revoked cached token is not a boot failure:** it falls through to
  the password path. A rejected *config* token with no password is.
- The room is joined at startup (idempotent): aliases resolve, and a
  failure there is fatal — a bridge that cannot reach its room should not
  pretend otherwise.

## The room-side contract

| You send | The bridge does |
| --- | --- |
| any text | prompts the session (reply fallbacks stripped first) |
| `y` / `n` / `yes` / `no` | answers a pending approval — only while one is outstanding; otherwise it is a prompt |
| `!stop` | cancels the in-flight turn (`POST /api/cancel`) |
| `!new` | detaches the room from its session; the next message starts fresh |

What comes back:

- **Replies** are `m.text` with both a plain body and a Matrix HTML
  rendering (markdown → goldmark GFM, raw HTML dropped — model output can
  never inject markup). Long replies chunk at 3,800 bytes on line
  boundaries; concatenating the chunks is byte-exact the original.
- **Bridge lines** (approvals, errors, `stopped.`) are `m.notice` — grey,
  never the agent's voice.
- **Typing indicator** runs while turns are in flight, refreshed on a
  10s tick against a 25s expiry.

## Permissions: y/n over chat

The webserver's permission ask (10-minute timeout) is surfaced into the
room as a notice; a plain `y`/`n` inside the window decides. The bridge's
window is 30 seconds shorter (9m30s) so the room hears "window closed"
from the bridge just before the server-side denial lands. An unanswered
approval **denies** — an unattended prompt cannot stall on an approval
nobody will answer, and it cannot approve itself either.

Both surfaces are live at once: the room approves what the web GUI would
have prompted for, with the same session, the same gate, the same mode.

## What v1 will not do

- **Encrypted rooms.** `m.room.encryption` in the room state is refused at
  startup — loudly, because the alternative is a bridge that silently
  ignores every message (megolm without a crypto dependency is not v1).
- **History replay.** The initial sync takes the cursor and drops the
  events: messages sent while the bridge was down stay unanswered,
  visible in the room, re-askable. A three-hour-old "fix the bug" firing
  on boot would be worse.
- **Edits, notices, emotes, files.** Only `m.text` prompts. An edit
  (`m.replace`) of an old message must not re-fire its prompt.
- **Sender allowlisting.** The room is the trust boundary: every member
  can prompt the agent. Keep it invite-only. (`allowed_senders` is a
  horizon item, not a v1 knob.)
- **Streaming replies.** The turn's text arrives when it finishes. The
  typing indicator covers the gap; message-edit streaming is a horizon
  item.

## Sharp edges, stated plainly

- **Same session, two surfaces.** The bridged session is a normal one —
  it appears in the web GUI's sidebar (when the GUI runs on the same
  project), and the per-session turn mutex only serializes turns *within
  one process*. Driving the same session from `a1 matrix` and a GUI at
  the same time can interleave turns across processes. Don't.
- **The state file is a credential.** `~/.a1/matrix/state.json` carries
  the cached Matrix token; it is written 0600 and should travel with the
  same care as `config.yaml`.

## Contributor notes

`internal/matrix` is deliberately dependency-free beyond the module's
existing budget (net/http + encoding/json + the goldmark already in the
tree): six endpoints, not a framework. The package's three goroutines and
their ordering rules:

1. **The sync loop** (Run's caller) consumes Matrix events and never
   blocks on the agent — `POST /api/prompt` waits out an in-flight turn
   (the webserver serializes turns server-side), and a sync loop stuck in
   that wait could not read the `y` that would free it. Prompts queue
   through a single worker; order is the room's order.
2. **The SSE consumer** handles exactly three events: `ask` (post to
   room, open the window), `done` (transcript diff → reply), and failed
   `status` (error to room). The `driven` map filters events to sessions
   this bridge prompted.
3. **The typing refresher** flips the room's typing flag on in-flight
   count transitions.

The turn-diff anchor (`seen[session]`) is set when a session is adopted
(resume) and advanced after each reply fetch; multi-round turns, system
riders, and queued prompts all fall out as "assistant messages after the
anchor". Tests live in `client_test.go` (wire shapes against a fake
homeserver), `bridge_test.go` (end-to-end against a fake loopback chat
API + fake homeserver, plus Connect's auth ladder), and `format_test.go`
(the fallback stripper's golden fixtures and the chunker's round-trip
property).
