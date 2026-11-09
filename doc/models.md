# Supported models

A1 talks to LLMs through an **explicit** `api` field on each config entry
(`OpenAI` | `OpenAIResponses` | `Anthropic` | `Gemini`). Empty `api` means
OpenAI-compatible `/chat/completions`. There is no name- or URL-based
provider guessing.

Built-in **presets** (exact `name` match) fill `base_url`, `context_window`,
`image_enabled`, `api`, and default thinking when those fields are omitted.
Source of truth: [`internal/project/model`](../internal/project/model/).

## Built-in presets

| Name | API | Base URL (default) | Context | Images | Thinking wire |
| ---- | --- | ------------------ | ------: | :----: | ------------- |
| `gpt-6-astra` | OpenAIResponses | `https://api.openai.com/v1` | 272K | yes | `reasoning.effort` (max) |
| `gpt-5.6-sol` | OpenAIResponses | `https://api.openai.com/v1` | 272K | yes | `reasoning.effort` (high) |
| `gpt-5.6-terra` | OpenAIResponses | `https://api.openai.com/v1` | 272K | yes | `reasoning.effort` (high) |
| `gpt-5.6-luna` | OpenAIResponses | `https://api.openai.com/v1` | 272K | yes | `reasoning.effort` (high) |
| `gpt-5-chat-latest` | OpenAIResponses | `https://api.openai.com/v1` | 128K | yes | — |
| `gpt-5.5` | OpenAIResponses | `https://api.openai.com/v1` | 272K | yes | `reasoning.effort` (high) |
| `gpt-5.5-pro` | OpenAIResponses | `https://api.openai.com/v1` | 1.05M | yes | `reasoning.effort` (high) |
| `deepseek-flash` | OpenAI | `https://api.deepseek.com` | 1M | yes | `extra_body.thinking` + `reasoning_effort` |
| `deepseek-v4-pro` | OpenAI | `https://api.deepseek.com` | 1M | no | same as Flash |
| `gemini-2.5-pro` | Gemini | Google Generative Language | 1M | yes | `thinkingBudget` (token cap) |
| `gemini-2.5-flash` | Gemini | Google Generative Language | 1M | yes | `thinkingBudget` |
| `gemini-3-pro` | Gemini | Google Generative Language | 1M | yes | `thinkingLevel` (off floor `LOW`) |
| `gemini-3-flash` | Gemini | Google Generative Language | 1M | yes | `thinkingLevel` (off floor `MINIMAL`) |
| `kimi-k3` | OpenAI | `https://api.moonshot.cn/v1` | 1M | yes | `reasoning_effort` (max) |
| `kimi-k2.7-code` | OpenAI | `https://api.moonshot.cn/v1` | 10M | yes | — |
| `glm-5.3` | OpenAI | `https://api.z.ai/api/coding/paas/v4` | 1M | no | `extra_body.thinking` + `reasoning_effort` |
| `glm-5.3-flash` | OpenAI | `https://api.z.ai/api/coding/paas/v4` | 1M | yes | same as `glm-5.3` |

Minimal config for a preset (api key only):

```yaml
models:
  - name: deepseek-flash
    api_key: sk-...
    default: true
```

## Other / custom models

Any other `name` works as a normal entry. Set fields yourself:

```yaml
models:
  - name: gpt-5
    api: OpenAIResponses        # /v1/responses (not chat-completions)
    api_key: sk-...
    base_url: https://api.openai.com/v1
    context_window: 272000
    default: true

  - name: gpt-4o
    api: OpenAI                 # optional; default
    api_key: sk-...
    base_url: https://api.openai.com/v1
    context_window: 128000

  - name: claude-sonnet-4-20250514
    api: Anthropic              # required for Anthropic Messages API
    api_key: sk-ant-...
    base_url: https://api.anthropic.com
    context_window: 200000

  - name: my-proxy-model
    api: OpenAI
    api_key: ...
    base_url: https://proxy.example/v1
```

`OpenAIResponses` uses the Responses wire format (`input` items, typed SSE
events, flat function tools). Tool call IDs are stored as `call_id|item_id`
when the stream provides both, matching round-trip needs for
`function_call_output`.
Gemini without a preset name still needs `api: Gemini` and a valid base URL;
thinking then uses the budget style by default.

## Credential pool

A model can carry extra API keys beyond its single `api_key`. The pool
rotates them under provider rate limits (429) and auth failures (401/403):
a key that answers badly cools down and the next request moves to another
key — invisibly when nothing has streamed yet, so a turn survives a
rate-limited key. Cooldowns take the longer of the provider's `Retry-After`
(when sent) and a backoff ladder (1m doubling, capped at 15m); auth
failures quarantine a key for 10 minutes. Pool state lives on the engine
and survives model switches and tool rebinds; configs naming the same keys
in different orders share one pool.

```yaml
models:
  - name: gpt-4o
    api_key: sk-primary      # required; the pool's first key
    api_keys:                # extras, rotated in when the primary cools
      - sk-backup-1
      - sk-backup-2
    default: true
```

`A1_API_KEYS` (comma-separated) extends the default model's pool the same
way. The transport's own retry budget runs first on each key, then the
pool rotates — additive patience, never a replacement.

## Prompt-cache scope

Every session carries a logical prompt-cache scope — the bucket its
OpenAI-wire requests route under via the provider's `prompt_cache_key`.
The scope is derived, not configured: a plain session scopes to its own
id; a scheduler fire (`ParentSession: "schedule:<id>"`) scopes to the
schedule, so recurring fires of one job re-read one warm bucket instead of
each minting a cold one; and a fork pins its parent's resolved scope on
the session header, so a branch's first request — it replays the parent's
entries — re-reads the parent's cached prefix instead of starting cold.
Real parent sessions are deliberately not walked: sibling sub-agents share
a parent id, and collapsing them onto one bucket would let their divergent
request streams evict each other's cached prefixes.

The wire key is content-addressed (`pck_` + a sha256 prefix) over the
scope, the system prompt, and the name-sorted tool surface, so the
chat-completions and Responses routes of one model derive the same key and
share the bucket across modes. It is sent only to `api.openai.com` — every
other endpoint (strict compat servers, aggregators, local models) keeps
the exact pre-scope request bytes — and only on streaming turns:
compaction and memory-consolidation requests are one-shot (nothing ever
reads their cache write back), so they carry no key. Anthropic and Gemini
are untouched; their caching is breakpoint-driven, and a1 already pins
those breakpoints (system + last user block + last tool). The Anthropic
markers carry ttl=1h, and the request sends the `extended-cache-ttl`
beta header whenever a marker does — the field is beta-gated on native
Anthropic and documented by Anthropic-compatible gateways, so the header
follows the TTL wherever it lands (a TTL-free request, like compact's,
sends nothing). The scope rides the session header and survives
persist/reload in both the file and vault
backings, and every client rebuild a rebind triggers re-pins it — the
bucket outlives the rebuild, the same contract the credential pool has.

## Turn recovery and the guardrail stack

The second hermes steal batch closed the loop between what a model can do
to a turn and what the harness does about it. Failures classify
(`llm.Classify`): one pipeline turns every error into a recovery verdict,
so the engine retries exactly what the HTTP layer cannot see — 408s,
mid-stream drops, wrapped transport errors — and never what it already
spent patience on (429/5xx are DoWithRetry's), never auth, never a turn
whose text was already delivered (the partial persists as the reply body).
Empty completions walk a budgeted ladder that fails fast on usage-proven
deterministic emptiness and persists a sentinel row instead of a blank
one; repetition loops are cut mid-stream at 16k scale with a placeholder
persisted, never the looped bytes; and an overflow whose provider-stated
limit the recovery compaction cannot shrink 5% in request bytes surfaces
instead of re-sending almost the same payload.

On the executor side, the toolguard controller watches call patterns per
turn: identical failures warn then block, runaway same-tool streaks halt
unattended engines, byte-identical large results become reference stubs,
and fetch/spawn spending is capped outright. The verify-on-stop gate
covers the undeclared finish — code edited, no check passed since — by
holding the answer as an interim row and re-opening the turn, twice, then
letting it stand. The learning loop ticks opportunity counters (turns
without memory use, rounds without skill use) and fires a cache-parity
review fork: byte-identical request prefix, one review instruction, and a
dispatch whitelist whose denials name their substitutes. And the script
tool collapses tool chains: one Python script calling real tools as
functions over a loopback RPC, every in-script call through the same
permission pipeline the model's own calls face.

## Thinking level

Provider-agnostic knobs on each model (and session UI):

| Key / env | Meaning |
| --------- | ------- |
| `think_enabled` | bool; enable reasoning payload |
| `think_level` | `off` \| `minimal` \| `low` \| `medium` \| `high` \| `xhigh` \| `max` |
| `A1_THINK_LEVEL` | env override on the default model (`off` disables) |

How that maps on the wire depends on the provider / preset interceptor
(OpenAI `reasoning_effort`, Anthropic thinking budget, Gemini budget or level).

```yaml
models:
  - name: gemini-2.5-flash
    api_key: ...
    think_level: medium
```

## Context window resolution

Context folding (compaction) scales to the model's real window. At runtime the
engine resolves the window in this order:

1. `context_window:` on the config entry (explicit wins).
2. The built-in preset for the name (see table above).
3. A **local probe** — llama.cpp `GET /props` (`n_ctx`) or ollama
   `POST /api/show` (`*.context_length`) — for loopback / private base URLs.
4. The **assumed fallback, 32K**, when nothing above yields a value. Folding
   must run even on unknown windows; before this fallback existed, window 0
   disabled both compaction triggers entirely.
5. **Learned**: a provider overflow error that states the limit
   (`maximum context length is 65536 tokens`, …) overrides everything — the
   provider measured it.

Mid-turn overflows force a compaction and retry (up to 3 consecutive
recoveries per turn; the streak resets after any successful round). A
numberless overflow halves the retry window for that turn so the retry fits
windows smaller than anything configured. The math — invariants, the
operating-point table for 16K–1M, and the worked 64K llama.cpp example — is
in [doc/context-folding-math.md](context-folding-math.md), and is asserted by
`internal/session/compaction/setting_test.go` so the doc, code, and tests
cannot drift apart.

For a local llama.cpp run at `-c 65536`, no config is needed: the probe picks
the window up and folding runs at K=9830 / T=8192 / G=19660 / floor=29491
from the first turn. For remote models without a preset, set
`context_window:` explicitly to skip the 32K assumption.

## Local endpoints

A loopback or private-network `base_url` (llama.cpp, Ollama, LM Studio,
vLLM on the LAN) changes the economics of every request: the server keeps
the conversation's prompt cache warm only while nothing else touches it,
and prefill runs at hardware speed — a 15K-token context at ~600 tok/s is
twenty seconds of dead time per cold turn. The harness keys several
behaviors off endpoint locality (`llm.IsLocalBaseURL`):

| Behavior | Remote | Local |
| --- | --- | --- |
| Memory consolidation | every completed turn | every 10th turn (batched — each outlet call is a different-prefix request that would evict the cache and re-prefill the whole context next turn) |
| Web session naming | LLM-generated after the first exchange | skipped — the first-message preview titles the session; a naming call evicts the cache between turns 1 and 2 |
| Fleet concurrency | 4 parallel sub-agents | 1 — interleaved child prefixes thrash a single-slot server's cache into full re-prefills per round, and prefill saturates the box anyway, so serialization costs ~nothing |
| Context-window probe | no (refused for public hosts) | llama.cpp `/props`, ollama `/api/show` |
| `prompt_cache_key` | api.openai.com only | never sent — local servers cache by byte-identical prefix, and strict ones may reject unknown fields |

The request wire is byte-stable on both routes (pinned by
`engine_cache_stability_test.go` for the Anthropic wire and
`engine_cache_stability_openai_test.go` for the OpenAI wire), so a local
server's automatic prefix caching holds across turns — that is the whole
game: keep the prefix untouched and the second turn prefills only its
delta.

Getting per-turn memory freshness back on a local box: route the ops
traffic off the conversation endpoint — set a persona **memory model**
pointed at a second (or remote) server. `LocalOpsEndpoint()` follows that
routing, and consolidation plus LLM naming return to per-turn behavior
immediately.

Server-side advice, briefly: llama.cpp's `--parallel N` gives N isolated
slots (concurrent harness traffic stops evicting each other at N > 1),
Ollama keeps a 5-minute idle cache per model (`OLLAMA_KEEP_ALIVE` to
extend), and LM Studio caches prefixes automatically. None of these
replace the byte-stable wire — they multiply it.

## Related

- Config overview: [README § Configuration](../README.md#configuration)
- Preset code: `internal/project/model/`
