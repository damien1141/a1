# Multiplayer Networking Reference

Client-server models, lag compensation, prediction, reconciliation, and netcode architectures for real-time multiplayer. Read alongside `engine-architecture.md` for the game loop.

## Netcode Models

| Model | Use | Examples |
|---|---|---|
| **Lockstep / deterministic** | RTS, fighting games; small player count | StarCraft, fighting games |
| **Snapshot interpolation** | Most action games; moderate player count | Quake, Halo |
| **Client-side prediction + reconciliation** | FPS, fast-paced action | CS:GO, Valorant |
| **Rollback netcode** | Fighting games; need pixel-perfect sync | GGPO, Mortal Kombat |
| **State replication (authority)** | MMO, large worlds | WoW, Fortnite |

### Choose by Genre

| Genre | Recommended model |
|---|---|
| Fighting (1v1) | Rollback (GGPO) |
| RTS (4-8 players) | Lockstep |
| FPS (5-32 players) | Client prediction + reconciliation |
| MMO (1000+ players) | State replication + interest management |
| Sports | Lockstep or rollback |
| Co-op (2-4 players) | State replication (simple) |

## Client-Server Authority

```
[Client]  → input →  [Server]  → state →  [All Clients]
                       ↑                      ↓
                  authoritative          reconcile to server truth
```

**Server is authoritative.** Clients send inputs; server runs the simulation; server sends state back. Clients never directly tell other clients what happened.

### Listen Server vs Dedicated Server

| | Listen Server | Dedicated Server |
|---|---|---|
| Use case | Casual co-op; prototyping | Competitive; large player count |
| Hosting | One client hosts + plays | Separate process; no rendering |
| Latency | Host has zero ping (advantage) | All clients equal ping |
| Cheating | Host can cheat (full authority) | Server in data center; harder to cheat |
| Cost | Free (player-hosted) | $$/month per instance |

For competitive or large-scale: dedicated server. For co-op with friends: listen server is fine.

## Snapshot Interpolation

Server sends state snapshots 10-20 times/sec; clients interpolate between them at 60 FPS.

```
Server ticks at 20 Hz:
  t=0.00s: send snapshot A (positions, rotations)
  t=0.05s: send snapshot B
  t=0.10s: send snapshot C

Client renders at 60 Hz, but always 100ms behind real time:
  t=0.10s render = interpolate between A and B
  t=0.11s render = interpolate between A and B
  ...
  t=0.15s render = interpolate between B and C
```

The 100ms "delay" is the **interpolation buffer**. Without it, clients would extrapolate (guess) when packets arrive late, causing jitter.

### Entity Interpolation Algorithm

```csharp
// Client-side: render entities between two snapshots
void Update() {
    double renderTime = NetworkTime - InterpolationDelay;  // 100ms behind
    Snapshot prev = FindSnapshotBefore(renderTime);
    Snapshot next = FindSnapshotAfter(renderTime);
    float t = (float)((renderTime - prev.Time) / (next.Time - prev.Time));
    transform.position = Vector3.Lerp(prev.Position, next.Position, t);
}
```

## Client-Side Prediction

To hide latency, clients predict their own movement locally:

```
1. Client presses W → immediately moves locally
2. Client sends input to server
3. Server simulates with input → confirms new position
4. Client receives server confirmation
5. If client's prediction matches server's: smooth
6. If mismatch: reconcile (snap to server truth + replay pending inputs)
```

### Prediction + Reconciliation Code

```csharp
// PlayerController.cs (client)
private Queue<PredictionState> _pendingPredictions = new();
private float _serverLastConfirmedTime = 0;

void Update() {
    if (IsLocalPlayer) {
        // 1. Sample input
        var input = SampleInput();

        // 2. Apply locally (prediction)
        ApplyInput(input);
        _pendingPredictions.Enqueue(new PredictionState {
            Time = NetworkTime,
            Input = input,
            Position = transform.position
        });

        // 3. Send to server
        SendInputToServer(input);
    }
}

// Server sends back confirmed state
void OnServerStateUpdate(ServerState state) {
    _serverLastConfirmedTime = state.Time;

    // Find the prediction we made for this time
    while (_pendingPredictions.Count > 0 &&
           _pendingPredictions.Peek().Time < state.Time) {
        _pendingPredictions.Dequeue();
    }

    if (_pendingPredictions.Count > 0) {
        var prediction = _pendingPredictions.Peek();
        float error = Vector3.Distance(prediction.Position, state.Position);

        if (error > 0.1f) {
            // MISPELL — reconcile
            transform.position = state.Position;  // snap to server truth

            // Replay all pending inputs from this point forward
            foreach (var pending in _pendingPredictions) {
                ApplyInput(pending.Input);
                pending.Position = transform.position;
            }
        }
    }
}
```

## Lag Compensation (Server-Side Rewind)

For hit-scan weapons (instant-hit guns), the server "rewinds" the world to what the shooter saw:

```
1. Client A presses fire at t=5.0s (their view of the world)
2. Network delay: server receives fire command at t=5.1s
3. Server rewinds world to t=5.0s (Client A's view at fire time)
4. Server checks if Client A's shot hit anyone at t=5.0s
5. Server applies damage; unwinds back to t=5.1s
```

The server stores position history for each player for ~200ms and can rewind to validate shots. This is how CS:GO, Valorant, and Overwatch handle "I shot them but they were behind a wall on my screen."

**Trade-off:** the shooter feels good (their shots land as expected) but the victim feels bad ("I was behind the wall!"). Most shooters prioritize the shooter experience.

## Network Models by Engine

### Unity

| Solution | Use |
|---|---|
| **Netcode for GameObjects (NGO)** | Official; GameObject-based; small-to-medium |
| **Mirror** | Open-source; community; similar to old UNet |
| **Fish-Networking** | Modern; performant; growing community |
| **Photon Fusion / Quantum** | Commercial; deterministic physics; battle-tested |
| **Unity Transport** | Low-level; for custom netcode |

### Unreal

- **OnlineSubsystem** — abstraction over platform services (Steam, EOS, custom)
- **Replication system** — built-in; mark `UPROPERTY(Replicated)` and Unreal handles sync
- **Network Prediction** (Unreal 5) — modern prediction + reconciliation
- **EOS (Epic Online Services)** — free; cross-platform; lobbies + matchmaking + relay

### Godot

- **High-level Multiplayer API** — built-in; RPCs + replication
- **ENet wrapper** — reliable UDP; default
- **WebRTC** — for browser-based multiplayer
- **Steamworks integration** — via GodotSteam plugin

```gdscript
# Godot: server-side authority
var peer = ENetMultiplayerPeer.new()
peer.create_server(PORT, MAX_CLIENTS)
multiplayer.multiplayer_peer = peer

# RPC: call function on all clients
@rpc("any_peer", "call_local", "reliable")
func broadcast_message(msg: String):
    $UI/Chat.text += msg + "\n"
```

## RPCs (Remote Procedure Calls)

| RPC type | Direction | Use |
|---|---|---|
| **Server RPC** | Client → Server | "I fired; validate + apply" |
| **Client RPC** | Server → one client | "Show damage indicator" |
| **Multicast RPC** | Server → all clients | "Boss died; play cutscene" |
| **Reliable** | Guarantees delivery | Damage events, purchases |
| **Unreliable** | Best-effort | Position updates (next packet fixes it) |

**Rule:** use reliable for events (must arrive), unreliable for state (newer packet supersedes older).

## Interest Management

Don't send every player's state to every other player. Only send what's relevant:

- **Distance-based** — only sync entities within X meters
- **Line-of-sight** — only sync visible entities
- **Relevance zones** — partition world; only sync current zone
- **Priority** — closer entities update more often

For 100+ players, interest management is mandatory.

## Tick Rate

| Tick rate | Use |
|---|---|
| 10 Hz | Casual co-op; turn-based |
| 20 Hz | Most action games; default |
| 30 Hz | Competitive shooters |
| 60 Hz | Top-tier competitive (CS:GO majors) |
| 120 Hz | LAN-only esports |

Higher tick rate = better hit registration + smoother feel, but more server CPU + bandwidth.

## Bandwidth Budget

Per client, per second:
- Position update (12 bytes pos + 12 bytes rot + 4 bytes state) × 20 Hz × 5 entities = 2.8 KB/s
- Input (4 bytes) × 60 Hz = 240 B/s
- Events (variable)

For 32 players: 32 × 5 KB/s = 160 KB/s total server bandwidth. Mobile data: 1 minute = ~10MB.

## Common Multiplayer Bugs

| Bug | Cause | Fix |
|---|---|---|
| **Rubber-banding** | Prediction mismatch; packet loss | Increase interpolation delay; improve prediction |
| **Desync** | Floating-point differences; non-deterministic | Use fixed-point or server-authoritative sim |
| **Lag spikes** | Variable network conditions | Jitter buffer; smoothing |
| **Shot behind wall** | Lag compensation enabled (expected) | Document the behavior; tune max rewind time |
| **Can't hit moving target** | Tick rate too low; interpolation delay too high | Increase tick rate; reduce interpolation delay |
| **Stuck on loading** | Blocking load on server | Async load |
| **Different physics on each client** | Non-deterministic physics engine | Server-authoritative; or use deterministic engine |
| **Cheating (god mode, etc.)** | Client-authoritative state | Server-authoritative; validate all client inputs |

## Security

- **Never trust the client** — validate all inputs server-side
- **Rate-limit** inputs (prevent spam)
- **Sanitize** chat (anti-PIP, anti-spam)
- **Anti-cheat** (Easy Anti-Cheat, BattlEye, VAC) for competitive games
- **Server-side physics** for anything that affects gameplay
- **Obfuscate** critical state (don't send full map to clients who can't see it)

## Testing

- **Local**: 2 instances on one machine (host + client)
- **LAN**: machines on local network; test real physics sync
- **WAN**: simulate latency with tools (Clumsy, Network Link Conditioner)
- **Stress test**: many bots to test server CPU + bandwidth
- **Long-running**: leave server up for hours; check for memory leaks
- **Network dropouts**: kill network for 5s; verify reconnection logic

## Common Pitfalls

| Pitfall | Symptom | Fix |
|---|---|---|
| Client-authoritative damage | Cheating | Server validates all damage |
| Floating-point across clients | Desync | Use fixed-point or server sim |
| No interpolation buffer | Jittery movement | 100ms buffer |
| Reliable for positions | Bandwidth explosion | Unreliable + sequence numbers |
| Big packets | Packet fragmentation | Fragment or use larger MTU |
| Sync load on server | Frame hitches | Async |
| No rate limit | DDoS from players | Per-client rate limit |
| Sending full state every tick | Bandwidth | Delta compression; only changed fields |
