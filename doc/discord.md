# The Discord Bridge

`a1 discord` is the third chat surface: one Discord channel bridged onto
one a1 session, built on the same loopback-client ruling the Matrix and
stoat bridges locked. Where stoat reuses `internal/ws` unchanged, Discord
adds the rulings its platform demands — the roadmap note locked them
before any code: bot tokens (not OAuth), the 2,000-unit message cap,
`!`-prefix commands (the convention the Matrix bridge set), and a
bucket-aware pacer on the send path.

| Audience | This document |
| --- | --- |
| Users | Setup (including the intent no code can enable), config reference, the channel-side commands |
| Contributors | The gateway v10 shape, the rate-limit discipline, the test doctrine |

## The prerequisite no code can supply

Two things must be true in Discord before the bridge can work, and both
live in the [Developer Portal](https://discord.com/developers/applications):

1. **The bot is in the server** (invite it like any user, with VIEW_CHANNEL
   on the bridged channel). Connect validates visibility at startup with
   one channel fetch — a 403 there is fatal with the reason.
2. **The MESSAGE CONTENT intent is enabled** (Bot → Privileged Gateway
   Intents). It is a privileged intent: without it the gateway either
   refuses the connection outright (close code 4014, "Disallowed
   intent(s)" — the error text surfaces in the bridge's log) or, worse,
   connects and delivers MESSAGE_CREATE events with **empty content**.
   The bridge cannot tell that apart from a quiet channel; the doc is
   the defense.

## The architecture ruling: a loopback client, not a new surface

Same as the other two bridges: `a1 discord` boots the webserver
**in-process on an ephemeral loopback port** and drives it through the
same chat API + SSE stream the web GUI uses — `POST /api/prompt`,
`GET /api/events`, `POST /api/ask`. Nothing about the agent's network
posture changes; the only new connections are the bridge's outbound
gateway websocket (`wss://gateway.discord.gg/?v=10&encoding=json`) and
REST calls (`https://discord.com/api/v10`).

## Config reference

```yaml
discord:
  token: "…"            # bot token (Developer Portal → Bot → Reset Token)
  channel: "…"          # the channel id to bridge
  # api: "https://discord.com/api/v10"    # optional override (proxies)
  # gateway: "wss://gateway.discord.gg"   # optional override
```

- **Auth is the bot token, full stop** — the roadmap's locked ruling
  (OAuth is for user-facing apps; a bridge is a bot). The token rides
  `Authorization: Bot` on REST and the IDENTIFY payload on the gateway,
  and is cached nowhere: `~/.a1/discord/state.json` carries only the
  session id.
- **Startup validation is two REST calls:** `/users/@me` (the token is
  live, and the bot's own id — the echo filter's other half) and `GET
  /channels/{id}` (the channel is visible and a bridgeable kind: guild
  text, announcement, or DM — voice, categories, forums, and threads are
  refused with the reason).

## The channel-side contract

| You send | The bridge does |
| --- | --- |
| any text | prompts the session |
| `y` / `n` / `yes` / `no` | answers a pending approval — only while one is outstanding; otherwise it is a prompt |
| `!stop` | cancels the in-flight turn (`POST /api/cancel`) |
| `!new` | detaches the channel from its session; the next message starts fresh |

The `!`-prefix is the convention the Matrix bridge set (the roadmap's
locked ruling over slash-commands: slash commands want an application
command registry and latency; a text channel already speaks text).

What comes back:

- **Replies** are markdown, sent as message content — Discord renders
  markdown natively. Long replies chunk at 2,000 **UTF-16 code units**,
  the unit every Discord client counts (`content.length` in the
  JavaScript of every official client and library): an astral emoji is
  two units, a CJK character one. A rune-counting splitter would emit
  "2,000 characters" of astral emoji and watch Discord reject it as
  4,000. Cuts prefer line boundaries; concatenating the chunks is
  byte-exact the original.
- **Bridge lines** (approvals, errors, `stopped.`) arrive as ordinary
  messages — no notice msgtype exists. They carry the `bridge:` voice.
- **Typing indicator:** `POST /channels/{id}/typing` on prompt and every
  7s while turns run. Discord's indicator lives ~10s and has NO
  end-typing route — it simply expires, so when the beats stop, it dies
  on its own.

## Rate limits: pace, don't collect

Discord's limiters are per-route buckets plus a global bucket, and the
roadmap locked the discipline before the code: **the send path paces.**
The client waits before a request when the previous response said the
bucket was empty (`X-RateLimit-Remaining` / `X-RateLimit-Reset-After` —
Reset-After in **seconds**, floating, the one unit difference from
stoat's milliseconds), and a 429 in spite of pacing is slept out per
its `retry_after` body exactly once — every 429 teaches the limiter,
even the one that ends the call. A **global** 429 takes a process-wide
lockout: it says stop everything, and even the typing beat waits.

Buckets are keyed by route (the `X-RateLimit-Bucket` header is only
known after a response, while pacing must decide before the next one;
the routes that share a bucket — send/edit/delete on one channel — are
exactly the verbs this bridge never mixes).

## Permissions: y/n over chat

Same ruling as the other bridges: the webserver's permission ask
(10-minute timeout) is surfaced into the channel; a plain `y`/`n`
inside the window decides. The bridge's window is 30 seconds shorter
(9m30s) so the channel hears "window closed" just before the
server-side denial lands. An unanswered approval **denies**.

## What v1 will not do

- **RESUME.** A reconnected bridge identifies fresh and the messages
  that arrived during the gap are gone — history is context, not
  prompts, the same ruling as the other bridges. (Discord offers
  RESUME/seq replay; a bridge that re-fires prompts from its downtime
  would be worse than one that drops them.)
- **Slash commands.** `!`-prefix only (see above).
- **Other bots.** MESSAGE_CREATE's `author.bot` marker is the skip.
- **Sender allowlisting.** The channel is the trust boundary: every
  human member can prompt the agent. Keep the channel gated.
- **Streaming replies, embeds, attachments, threads.** Text in, text
  out, one channel.

## Sharp edges, stated plainly

- **Same session, two surfaces.** The per-session turn mutex only
  serializes turns *within one process*. Driving the same session from
  `a1 discord` and a GUI at the same time can interleave. Don't.
- **The missing intent is silent.** See the prerequisite above — a
  connected bridge that sees no messages is probably missing MESSAGE
  CONTENT.
- **Bot tokens are credentials.** They live in `config.yaml` and go
  nowhere else.

## Contributor notes

`internal/discord` reuses `internal/ws` (the stoat bridge's foundation)
unchanged. Four files, four halves:

1. **client.go** — the REST half: `/users/@me` + channel fetch (the
   startup validations), message sends, typing beats, and the
   bucket-aware pacer (before-request waiting, one bounded
   retry_after sleep, global lockout spanning routes).
2. **gateway.go** — the event half: HELLO opens the heartbeat contract
   (first beat after a jittered fraction of the interval — the spec's
   anti-thundering-herd guidance — then one per interval, carrying the
   last dispatch sequence); IDENTIFY carries the token, the intents
   (guild messages | direct messages | the privileged message content),
   and honest connection properties; READY pins the self user (absence
   is fatal — the echo filter cannot be built); MESSAGE_CREATE is the
   only dispatch consumed. op-1 requests get an immediate beat; op-9
   and op-7 end the connection for the bridge's reconnect loop.
3. **bridge.go** — the loopback half, ported from the stoat bridge
   nearly verbatim (which was ported from the Matrix bridge): prompt
   worker, SSE consumer, transcript-diff replies, approvals with
   deny-on-timeout, `!stop`/`!new`, stored-session resume.
4. **format.go** — the UTF-16-counted chunker: 2,000 units per message,
   line boundaries preferred, hard cuts never split a rune (the unit
   walk stops between runes even when a rune is worth two units).

Tests live in `client_test.go` (REST shapes, the pacer's
before/after disciplines in Discord's own units — seconds — against a
fake API with header knobs, the global lockout spanning routes, error
messages surfaced), `bridge_test.go` (end-to-end against a fake loopback
chat API + a fake gateway v10 speaking real websocket frames: HELLO →
IDENTIFY shape (token, intents, properties) → READY → MESSAGE_CREATE,
heartbeats on the HELLO interval carrying the sequence, op-1 immediate
beats, op-9/op-7 fatal, no-self-READY fatal, pre-READY drops, plus the
bridge battery: prompt→reply, chunking, approvals y/n/expiry,
`!stop`/`!new`, echo/other-channel/other-bot filtering, stored-session
resume with the diff anchor, unavailable sessions starting fresh, the
typing lifecycle, undriven sessions ignored, Connect's validation
ladder), and `format_test.go` (the golden table with hand-computed
counts from the 2,000-unit budget — the astral-emoji differential that
separates a unit counter from a rune counter — byte-exact round trips,
line-boundary preference, rune safety, and the standard library's own
utf16.Encode as the independent counting oracle).
