# WebSocket + Socket.IO — Real-Time Channels, Reconnection, Backpressure

Real-time bidirectional messaging over WebSocket. Socket.IO 4 for browser-facing channels; `ws` or `uWebSockets.js` for raw performance. Scale horizontally with the Redis adapter and sticky sessions.

## When WebSocket (vs SSE, long-poll, REST)

| Need | Pick |
|---|---|
| Bidirectional, low-latency, persistent (chat, presence, multiplayer) | **WebSocket** |
| Server→client one-way stream (notifications, live feed) | **SSE** |
| Infrequent updates from server (every 30s+) | **Long-poll** or REST polling |
| Cross-origin simple push with HTTP/2 | **SSE** (auto-reconnect, no upgrade dance) |
| Binary frames, sub-protocols, custom framing | **Raw WebSocket** (no Socket.IO) |

Default to SSE for one-way push; only reach for WebSocket when the client must also send frequent messages.

## Socket.IO 4 server

```js
import { createServer } from "http";
import { Server } from "socket.io";
import { createAdapter } from "@socket.io/redis-adapter";
import { createClient } from "redis";
import jwt from "jsonwebtoken";

const httpServer = createServer();
const io = new Server(httpServer, {
  cors: { origin: process.env.ALLOWED_ORIGIN, credentials: true },
  pingTimeout: 20000,        // server declares dead if no pong in 20s
  pingInterval: 25000,       // heartbeat cadence
  maxHttpBufferSize: 1e6,    // 1MB max message size — reject oversized payloads
});

// Auth middleware — runs before connection is established
io.use((socket, next) => {
  const token = socket.handshake.auth.token;
  if (!token) return next(new Error("auth required"));
  try { socket.data.user = jwt.verify(token, process.env.JWT_SECRET); next(); }
  catch { next(new Error("invalid token")); }
});

// Redis adapter — fans out emit/broadcast across all server instances
const pub = createClient({ url: process.env.REDIS_URL });
const sub = pub.duplicate();
await Promise.all([pub.connect(), sub.connect()]);
io.adapter(createAdapter(pub, sub));

// Presence: Redis hash maps userId → current socket.id (one entry per user)
io.on("connection", async (socket) => {
  const { userId } = socket.data.user;
  await pub.hSet("presence", userId, socket.id);

  socket.on("join-room", (roomId) => {
    socket.join(roomId);
    socket.to(roomId).emit("user-joined", { userId });
  });

  socket.on("leave-room", (roomId) => {
    socket.leave(roomId);
    socket.to(roomId).emit("user-left", { userId });
  });

  // Ack callback → client knows server received; enables backpressure
  socket.on("message", ({ roomId, text }, ack) => {
    if (typeof text !== "string" || text.length > 2000) {
      return ack({ ok: false, reason: "invalid" });
    }
    io.to(roomId).emit("message", { userId, text, ts: Date.now() });
    ack({ ok: true });
  });

  socket.on("disconnect", async () => {
    await pub.hDel("presence", userId);
  });
});

httpServer.listen(3000);
```

## Client reconnection with backoff and message queue

```js
import { io } from "socket.io-client";

const socket = io("wss://api.example.com", {
  auth: { token: getAuthToken() },
  reconnection: true,
  reconnectionAttempts: 10,
  reconnectionDelay: 1000,         // initial delay
  reconnectionDelayMax: 30000,     // cap
  randomizationFactor: 0.5,        // jitter — prevents thundering herd
});

// Buffer messages while disconnected
let messageQueue = [];
const send = (event, payload) => {
  if (socket.connected) socket.emit(event, payload);
  else messageQueue.push({ event, payload });
};

socket.on("connect", () => {
  messageQueue.forEach(({ event, payload }) => socket.emit(event, payload));
  messageQueue = [];
});

socket.on("disconnect", (reason) => {
  // "io server disconnect" — server explicitly closed; client must reconnect manually
  if (reason === "io server disconnect") socket.connect();
});

socket.on("connect_error", (err) => {
  if (err.message === "invalid token") refreshAuthToken();
});
```

## Rooms, namespaces, broadcasting

- **Rooms** — `socket.join("room:42")` / `io.to("room:42").emit(...)`. Use for chat rooms, document collaboration, presence per entity.
- **Namespaces** — `io.of("/admin")`. Separate auth, separate event space. Use for admin vs user channels.
- **Broadcasting** — `socket.broadcast.emit(...)` (everyone but sender), `io.emit(...)` (everyone), `io.to(room).emit(...)` (room only).
- **Acknowledgments** — pass a callback as the last arg: `socket.emit("ping", () => console.log("ack"))`. Server invokes `ack(...)` to confirm. Use for any state-changing event.

## Backpressure

WebSocket has no built-in flow control. A slow client can buffer unbounded data server-side.

Strategies:
1. **Ack-required sends** — client must ack each message before server sends the next. Drop client if ack is late.
2. **`socket.conn.transport.readyState`** — check before emitting; skip if not `"open"`.
3. **Drop high-frequency events** — server-side rate limit per socket: max 10 messages/sec.
4. **Binary backpressure** — for streams, check `socket.write(data)` return value; if `false`, wait for `"drain"`.
5. **Per-socket buffer limit** — `socket.setMaxListeners()` and a manual buffer-size check; disconnect if exceeded.

```js
// Per-socket rate limit
const rateLimit = new Map();
socket.use((_, next) => {
  const now = Date.now();
  const last = rateLimit.get(socket.id) || [];
  const recent = last.filter((t) => now - t < 1000);
  if (recent.length > 10) return next(new Error("rate limit"));
  rateLimit.set(socket.id, [...recent, now]);
  next();
});
```

## Heartbeat

TCP keepalive is insufficient; proxies/load balancers silently drop idle connections. Socket.IO's `pingInterval`/`pingTimeout` exchanges application-level pings:

- Server sends ping every 25s.
- Client must pong within 20s.
- No pong → server fires `disconnect` and cleans up presence.

For raw `ws`, implement your own ping/pong in `ws.on("ping")`/`ws.on("pong")`.

## Horizontal scaling

WebSocket connections are stateful — the same client must reach the same server instance for the lifetime of the connection.

| Layer | Strategy |
|---|---|
| Load balancer | **Sticky sessions** (cookie or IP hash) — pin client to instance |
| Multi-instance emit | **Redis adapter** — `io.to("room:42").emit(...)` fans out across all instances via pub/sub |
| Presence | **Redis hash** (`presence` → `userId: socketId`) — any instance can look up |
| Session drain | Stop accepting new connections on an instance; let existing connections close naturally |
| Health check | `GET /health` returns 503 during drain so LB stops sending |

Sticky session config (nginx):
```nginx
upstream ws_backend {
  hash $cookie_session_id consistent;
  server ws1:3000;
  server ws2:3000;
}
server {
  location /socket.io/ {
    proxy_pass http://ws_backend;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 600s;
  }
}
```

## Connection limits

Each socket holds a TCP connection + memory. Plan per instance:

- Node.js default `ulimit -n 1024` is too low — raise to 65535+ in production.
- Per-socket memory: ~50KB baseline + buffers. 10K connections ≈ 500MB.
- Use `uWebSockets.js` for > 50K connections per instance (C++ perf, ~10× cheaper than `ws`).
- Always load-test before production: `artillery` or `k6` with WS engine.

## Auth patterns

- **First message** — client sends `{type: "auth", token}` as first frame; server closes if missing.
- **Handshake query / auth field** (Socket.IO) — `io(url, { auth: { token } })`. Cleaner; `io.use()` middleware verifies before connection accepted.
- **Cookie** — session cookie works if same-origin; fails for cross-origin without `credentials: true`.
- **Refresh** — short-lived access token (5-15min); client must reconnect with new token on expiry. Server emits `auth-expired` event before closing.

## Cleanup on disconnect

Always clean up state on `disconnect`:

```js
socket.on("disconnect", async () => {
  await pub.hDel("presence", userId);
  // Cancel any in-flight timers
  if (socket.data.heartbeatTimer) clearTimeout(socket.data.heartbeatTimer);
  // Notify rooms
  for (const room of socket.rooms) {
    if (room !== socket.id) socket.to(room).emit("user-left", { userId });
  }
});
```

Forgetting cleanup → presence leaks, ghost users, memory growth.

## Verification gates

- `npx wscat -c ws://localhost:3000` — basic connection works.
- Auth rejection test: missing/invalid token → connection refused with error.
- Room test: join/leave/broadcast events fire correctly.
- Multi-instance test: open two browser tabs served by different instances, verify message delivery.
- Load test: `artillery run ws-load.yml` — 10K concurrent connections, p99 latency, no presence leaks.
- Reconnect test: kill server, verify client reconnects with jitter, queue flushes on reconnect.
