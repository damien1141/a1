# The Stoat Bridge

`a1 stoat` is the second chat surface: one stoat.chat channel bridged onto
one a1 session, built on the Matrix bridge's architecture ruling for
ruling. Stoat is the Revolt lineage (the platform rebranded; the wire did
not) — a REST API speaking `X-Bot-Token` and an event websocket speaking
protocol v1 JSON.

| Audience | This document |
| --- | --- |
| Users | Config reference, the channel-side commands, what the bridge will and will not do |
| Contributors | The loopback-client architecture, the websocket foundation, the test doctrine |

## The architecture ruling: a loopback client, not a new surface

Copied from the Matrix bridge verbatim, because it locked the design: `a1
stoat` boots the webserver **in-process on an ephemeral loopback port** and
drives it through the same chat API + SSE stream the web GUI uses — `POST
/api/prompt`, `GET /api/events` (SSE), `POST /api/ask`. Nothing about the
agent's network posture changes; the only new connections are the bridge's
outbound event websocket to `wss://stoat.chat/events` and its REST calls to
`https://api.stoat.chat`.

What differs from Matrix is the inbound half: where Matrix long-polls
`/sync`, stoat pushes events down a websocket. That forced one new
foundation into the tree — `internal/ws`, a hand-rolled RFC 6455 client
(`gorilla/websocket` is a fine library, but the bridges need one client
role speaking text frames, and the dependency budget is spent on purpose
elsewhere). The subset: client role only (masked sends), text frames only,
fragmentation assembled, ping answered with pong, close surfaced as a
`CloseError`, RSV bits refused (no extension was negotiated), and a hard
idle deadline — no frame from the peer within 90s tears the connection
down, because a silently-dead TCP connection is the one failure a write
can never reveal. Both stoat and the Discord bridge speak through it.

## Config reference

```yaml
stoat:
  token: "…"            # bot token (X-Bot-Token on REST)
  channel: "…"          # the channel id to bridge
  # api: "https://api.stoat.chat"    # optional override (self-hosters)
  # ws: "wss://stoat.chat/events"    # optional override
```

- **Auth is the bot token, full stop.** Create the bot in the client's
  bot settings, copy its token, invite it to the server. No password
  path exists and none is needed; the token is a stable credential the
  user pasted on purpose, which is why the state file caches nothing but
  the session id.
- **The channel fetch is the startup handshake.** One `GET
  /channels/{id}` proves the token is live, the bot can see the channel,
  and the channel is a bridgeable kind (server text channel, group, or
  DM — voice channels and saved-messages channels are refused with the
  reason).
- **Endpoint overrides exist for self-hosters**, not for federation
  gymnastics: `api:` and `ws:` replace the production hosts wholesale.

## The channel-side contract

| You send | The bridge does |
| --- | --- |
| any text | prompts the session |
| `y` / `n` / `yes` / `no` | answers a pending approval — only while one is outstanding; otherwise it is a prompt |
| `!stop` | cancels the in-flight turn (`POST /api/cancel`) |
| `!new` | detaches the channel from its session; the next message starts fresh |

What comes back:

- **Replies** are markdown, sent as the message content — stoat renders
  markdown natively, so there is no HTML pipeline to keep honest. Long
  replies chunk at 2,000 **characters** (the platform's
  `message_length` limit, which the backend counts in Unicode scalar
  values — runes, not bytes: a 2,000-CJK-character answer is one legal
  message) on line boundaries; concatenating the chunks is byte-exact
  the original.
- **Bridge lines** (approvals, errors, `stopped.`) arrive as ordinary
  messages — stoat has no notice msgtype. They carry the `bridge:`
  voice the Matrix bridge's grey notices do, just without the grey.
- **Typing indicator** runs while turns are in flight — `BeginTyping`
  on the event socket (stoat has no REST route for typing), refreshed
  on a 3s tick against the server's few-second hold, and ended the
  moment the last turn finishes.
- **Rate limits are paced, not collected.** Stoat's limiter is a fixed
  window — 10 sends per 10 seconds per channel. The client paces
  before the request from the response's `X-RateLimit-Remaining` /
  `X-RateLimit-Reset-After` headers and sleeps out a 429's
  `retry_after` body (once — a server that only says 429 gets an
  error, not a spin).

## Permissions: y/n over chat

Same ruling as Matrix: the webserver's permission ask (10-minute
timeout) is surfaced into the channel; a plain `y`/`n` inside the window
decides. The bridge's window is 30 seconds shorter (9m30s) so the
channel hears "window closed" just before the server-side denial lands.
An unanswered approval **denies**.

## What v1 will not do

- **History replay.** Messages that arrived while the bridge was
  disconnected are gone — the websocket reconnects to NOW, the same
  "history is context, not prompts" ruling as the Matrix initial sync.
  A reconnect cannot fire a three-hour-old "fix the bug" at the agent.
- **Edits.** `MessageUpdate` events are ignored: an edit of an old
  message must not re-fire its prompt.
- **Other bots.** Messages carrying embedded bot information are
  skipped — a bridge that answers bots is a loop machine.
- **Sender allowlisting.** The channel is the trust boundary: every
  human member can prompt the agent. Keep the channel private.
- **Streaming replies.** The turn's text arrives when it finishes; the
  typing indicator covers the gap.
- **Attachments, embeds, masquerade.** Text in, text out. The bridge
  reads no files and wears no costume.

## Sharp edges, stated plainly

- **Same session, two surfaces.** The bridged session is a normal one —
  the per-session turn mutex only serializes turns *within one
  process*. Driving the same session from `a1 stoat` and a GUI at the
  same time can interleave turns. Don't.
- **The websocket is one connection per bridge, deliberately.** A
  dropped connection costs the messages in flight during the gap (see
  history replay above). The reconnect backoff tops at 30s; a hard-down
  platform is not hammered.
- **Bot tokens are credentials.** They live in `config.yaml` and go
  nowhere else — the state file (`~/.a1/stoat/state.json`) carries only
  the session id.

## Contributor notes

`internal/stoat` is dependency-free beyond `internal/ws` and the module's
existing budget. Four files, four halves:

1. **client.go** — the REST half: channel fetch (the startup
   handshake), message sends with the fixed-window pacer (wait before,
   sleep-retry after, bounded at one retry).
2. **gateway.go** — the event half: dial, `Authenticate` (as a message,
   not a query parameter — tokens in URLs leak into logs), `Ready` (the
   self user is the one whose `relationship` is `"User"`; its absence
   is fatal because the echo filter cannot be built), then `Message`
   events — system messages and other bots never surface. `Bulk` events
   recurse; server `Ping` is answered with `Pong`; the client's own
   `Ping` leaves every 20s (the protocol doc says 10–30s).
3. **bridge.go** — the loopback half, ported from the Matrix bridge
   nearly verbatim: prompt worker (the gateway loop must never block on
   `POST /api/prompt`'s turn-wait — it could not then read the `y` that
   frees it), SSE consumer (`ask`/`done`/failed `status`, filtered by
   the `driven` map), transcript-diff replies, approvals with
   deny-on-timeout, `!stop`/`!new`.
4. **format.go** — the rune-counted chunker: 2,000 runes per message,
   line boundaries preferred, hard cuts land on rune boundaries by
   construction (the budget is counted in runes, so the cut index
   cannot fall mid-rune).

The typing indicator ends the moment the last in-flight turn finishes,
not on the next refresh tick — an instant turn can complete before any
tick observes it, and the indicator must not outlive the turn.

Tests live in `client_test.go` (REST shapes, the pacer's before/after
disciplines against a fake API with header knobs), `bridge_test.go`
(end-to-end against a fake loopback chat API + a fake event gateway
that speaks real websocket frames, plus Connect's validation ladder and
the gateway's own protocol trio), and `format_test.go` (the chunker's
golden table and byte-exact round-trip property). The fake gateway in
`bridge_test.go` is a real server-side websocket implementation —
masked reads, unmasked writes — the same honest-oracle discipline as
`internal/ws`'s tests.
