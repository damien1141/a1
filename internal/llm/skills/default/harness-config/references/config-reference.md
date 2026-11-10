# a1 config reference

Everything the harness reads at boot, annotated. Source of truth in the checkout:
`internal/project/config.go` (schema), `internal/mcp/config.go` (mcp.json),
`internal/llm/skills/skills.go` (SKILL.md format). When this file and the code
disagree, the code wins — go read it.

---

## ~/.a1/config.yaml — full schema

```yaml
# ── models ─────────────────────────────────────────────────────────
# Flat list; one entry is the default. The entry carrying `default: true`
# wins; with no flag anywhere, the FIRST entry is the default.
# There is no top-level default_model: key.
models:
  - name: main                    # required, unique — referenced by agents.models, a1 eval --model
    api_key: sk-…                 # single key
    api_keys:                     # OR: pool — rotates under 429 / 401 / 403 with cooldown ladders
      - sk-one
      - sk-two
    base_url: https://api.openai.com/v1   # OpenAI-compatible endpoint
    context_window: 200000        # int — fold math uses this
    image_enabled: true           # multimodal in / out
    api: openai                   # explicit router when the URL doesn't betray it
                                  #   (openai | anthropic | gemini | … see internal/project/model)
    default: true                 # ← the default-model flag
    think_enabled: true           # extended thinking
    think_level: high             # provider-shaped level
    personal_read: true           # bool, optional, default false — opts the model into
                                  # agent-side personal-data reads (calendar / kanban / mail)
    personal_write: true          # bool, optional, default false — opts the model into
                                  # personal-data writes (calendar events, kanban cards);
                                  # mail is read-only even with write
    headers:                      # map, optional — extra HTTP headers sent with every
      X-Provider: munito          # request to this endpoint (routing keys, tenant tags);
                                  # a configured header can also override a client default

# ── skill_path ─────────────────────────────────────────────────────
# Where SKILL.md dirs are loaded from. Default: ~/.a1/skills.
# skill_path: /abs/path/to/skills

# ── permissions ────────────────────────────────────────────────────
permissions:
  mode: interactive        # interactive | readonly | autopilot | headless-strict | yolo
  workspace_only_writes: true   # writes confined to the workspace
  ask_timeout_sec: 120          # how long an Ask may hang before it resolves
  dangerously_allow_all: false  # never set silently — full bypass
  bash_risk_guard: true         # deny-regex risk screen on bash
  sandbox: landlock             # OS sandbox (Linux: landlock, macOS: seatbelt)
  sandbox_network: ""           # sandbox network policy
  bash:
    default: allow              # default verdict for unrated bash commands
    allow:                      # allowlist (regex strings)
      - "^go (build|test)"
    deny:                       # denylist wins over allow
      - "rm -rf"

# Mode semantics (mode strings are literal, internal/permission/policy.go):
#   interactive    — Ask gate live; the normal mode
#   readonly       — no mutating calls at all
#   autopilot      — reads and writes flow; risky ops still ask
#   headless-strict— unattended default (a1 serve, wake-ups): Ask → Deny
#   yolo           — no prompts, no caps, no sandbox walls. Benchmarks/CI only.
# Note: yolo reaches the engine through four gate shapes (flag, config key,
# TUI override, bare allow-all) — all are honored as "unrestricted".

# ── agents (subagent fleet) ─────────────────────────────────────────
agents:
  enabled: true             # false removes agent_spawn/wait/… tool schemas
  max_tool_calls: 200       # per subagent turn budget
  models:                   # per-role model overrides; empty → inherit parent
    explore: fast-cheap
    review: main
    worker: main

# ── semantic_search (vector recall) ─────────────────────────────────
semantic_search:
  enabled: true
  ollama_base_url: http://127.0.0.1:11434
  embedding_model: nomic-embed-text

# ── jev (the decision/judgment model — one config, no legacy judge:) ─
jev:
  provider: openai          # provider family
  api_key: sk-…
  base_url: https://api.openai.com/v1
  model: gpt-5-mini
  ollama_base_url: http://127.0.0.1:11434   # native /v1/systemone wire; chat emulation is the pre-0.35 fallback
  timeout_sec: 60

# ── browser (BetterWright daemon) ───────────────────────────────────
browser:
  profiles: [work, personal]     # named on-disk profiles with cookie survival
  proxy: http://127.0.0.1:8885   # applied at launch → browser must reopen to change
  headless: true                 # nil = detect (headed when a graphical session exists)

# ── personal (dashboard personal-data widgets) ────────────
# The master switch gates the dashboard's calendar / kanban / mail widgets
# AND is a precondition for the per-model personal_read / personal_write
# tools above. Calendar: local .ics files, no CalDAV, no sync. Mail: local
# Maildirs and remote IMAP servers (IMAP reads landed with chunk 7), every
# account read-only on every surface — no SMTP, no flags written back.
personal:
  enabled: true                  # bool, optional, default false (locked) — master switch
  calendar_dir: ~/.a1/calendar   # local dir of .ics files (~ expanded at parse); read by the
                                 # dashboard's agenda and, with personal_write, the one personal
                                 # surface the agent may write (new one-event .ics files, surgical
                                 # in-place updates — the web UI itself is read-only)
  mail_dir: ~/.a1/mail           # local Maildir root (~ expanded at parse); read-only by
                                 # ruling on EVERY surface — the web reads full messages,
                                 # the agent's personal_mail reads envelopes only (headers,
                                 # flags, sizes — never bodies), and nothing can send, move,
                                 # or mark: flags come from filenames, never written back.
                                 # Legacy single-root spelling: feeds the account list as
                                 # one account named "local"
  mail_accounts:                 # multi-account mail (any mix of local Maildirs and
                                 # remote IMAP servers; every account read-only on every
                                 # surface, exactly the mail_dir ruling carried to the wire).
                                 # Editable from the dashboard (chunk 8: the mail card's
                                 # "Add mail account" / gear / the reader's "Manage accounts")
                                 # — the configurator writes THIS key through the same
                                 # surgical YAML edit the settings page uses; hand-editing
                                 # stays welcome (validation is strict in the UI, lenient
                                 # here: a malformed account degrades to an honest
                                 # per-account error, never a boot failure)
    - name: work                 # display label + the ?account= key on the mail
                                 # endpoints; empty = root's basename (maildir) or
                                 # host (imap); duplicates get -2/-3 suffixes
      type: imap                 # "maildir" (default) | "imap"
      host: imap.work.dev        # imap: server host (required)
      port: 993                  # imap: 0 = 993 with tls on, 143 with it off
      username: me@work.dev
      password: <secret>         # app password for hosted providers (Gmail & co);
                                 # sits beside the model api_keys — config.yaml is
                                 # this project's secret store (the dashboard's
                                 # account editor never reads it back: a blank
                                 # field keeps the saved one)
      tls: true                  # implicit TLS (default); false = plaintext
      mailbox: INBOX             # the box the widget opens first (default INBOX)
    - name: local-archive
      root: ~/.a1/mail-archive   # maildir: the Maildir root (~ expanded)

# ── ui / web / personas / tools ────────────────────────────────────
ui:
  auto_expand_tool_output: true  # file-edit rows start expanded
web:
  auto_name_sessions: true       # memory-ops model titles sessions
  dark_mode: true                # web GUI theme: true=dark, false=light,
                                 # absent=follow the OS. Written by the GUI's
                                 # theme button (PUT /api/theme) so the choice
                                 # reaches every browser and the /settings
                                 # config screen, which reads it server-side.
personas:
  active: <persona-id>           # registry itself lives in ~/.a1/personas/
tools:
  max_tool_calls: 40             # cap per user turn; 0/absent = unlimited; yolo ignores

# ── checks (config-defined evidence) ───────────────────────────────
# Each named command records `check:<name>` evidence in the ledger when it
# runs. Declare `check:<name>` via the contract tool and the harness judges
# it like any built-in check — five lines of config teach the ledger a
# custom stack.
checks:
  - name: lint
    command: golangci-lint run ./...
    success: exit_0        # or a regex over combined output
    pattern: ""            # regex alternative to exit-code judging

# ── schedules (config-declared, boot-synced) ───────────────────────
# Prefer the live `schedule` tool for creating wake-ups (vault + scheduler
# immediately). Hand-written entries here sync at boot — the declarative
# path for humans. Exactly ONE trigger per schedule.
schedules:
  - id: nightly-lint
    prompt: "Run the lint check; report failures with file:line."
    description: nightly lint pass
    cron: "30 3 * * *"           # 5-field, minute granularity — OR:
    # fixed_rate: 15m             # duration, repeats — OR:
    # one_time: 2026-12-01T09:00:00Z   # fires once, then disables
    workdir: /path/to/repo        # where the wake-up session runs
    role: worker                  # fleet role (default worker: headless-strict)
    contract: ["check:lint"]      # inherited finish line
    timeout_sec: 900
    enabled: true
```

---

## mcp.json

User level `~/.a1/mcp.json`; project level `<cwd>/.a1/mcp.json` — same-named
project servers override user ones.

```json
{
  "servers": {
    "fetch": {
      "transport": "stdio",
      "command": ["npx"],
      "args": ["-y", "@modelcontextprotocol/server-fetch"]
    },
    "remote": {
      "transport": "http",
      "url": "http://127.0.0.1:3001/mcp",
      "headers": { "Authorization": "Bearer …" }
    }
  }
}
```

Claude/Cursor migration: `mcpServers` → `servers`; string `"command": "npx"`
becomes the array `["npx"]`.

- CLI: `a1 mcp add <name> -- <cmd> [args…]` (stdio), `remove`, `list`,
  `call <server> <tool> '<json>'`, `doctor`.
- Model side: `mcp_list` / `mcp_inspect` / `mcp_call` meta-tools; server names
  ride the system prompt; schemas never enter context.
- Restart a1 after adding — the pool loads at startup.
- Logs: `~/.a1/logs/mcp/<name>.log` (move with `A1_MCP_LOG_DIR`).
- Kill switch: `A1_MCP=off`.

---

## Environment variables (precedence over config.yaml)

| Var | Overrides | Notes |
| --- | --- | --- |
| `A1_MODEL` | default model name | also names it |
| `A1_API_KEY` | default entry's api_key | |
| `A1_API_KEYS` | default entry's pool | rotation on 429/401/403 |
| `A1_BASE_URL` | default entry's base_url | |
| `A1_THINK_ENABLED` | think toggle | |
| `A1_THINK_LEVEL` | think level | `off` disables |
| `A1_SKILL_PATH` | skill_path | |
| `A1_MCP` | MCP subsystem | `off` disables |
| `A1_MCP_LOG_DIR` | MCP log dir | |
| `A1_EXTENSIONS` | extensions | `off` disables |
| `A1_BROWSER_HEADLESS` | browser headless | wins over config (per-invocation) |
| `A1_OFFLINE` | network features | |
| `A1_SKIP_VERSION_CHECK` | update check | |
| `A1_DEBUG` / `A1_DEBUG_FILE` | debug logging | |

PHI_* spellings are honored with a deprecation notice.

---

## SKILL.md format

Directory per skill under the skill path (`~/.a1/skills` default):

```
<skill-name>/
  SKILL.md            # required — frontmatter + body
  references/*.md     # optional — load-on-demand depth
```

Frontmatter (parsed by `internal/llm/skills/skills.go`):

```yaml
---
name: my-skill
description: One routing-relevant paragraph — this is what the catalog shows.
license: MIT
metadata:
  author: …
  version: "1.0.0"
  domain: meta
  triggers: comma, separated, routing, keywords
  role: specialist
  scope: implementation
  output-format: code
  related-skills: …
---
```

- `name` + `description` are the catalog surface (`list_skills`).
- `metadata.triggers` feed routing.
- User skills under the skill path **take precedence** over embedded defaults
  and are never overwritten by them.
- `list_skills` re-reads disk every call — a new SKILL.md is discoverable in
  the same session, no restart.

## CLI surface map

| Command | Purpose |
| --- | --- |
| `a1 mcp list/add/remove/call/doctor` | MCP config + health |
| `a1 config` | HTML config editor (human-facing; loopback) |
| `a1 sessions list` | persisted sessions for this directory |
| `a1 run -p … [--jsonl] [--yolo] [--tools LIST] [--max-rounds N] [--session ID \| --continue-last] [--session-dir DIR]` | headless run; model selection is config/env (`A1_MODEL`), not a flag |
| `a1 serve` | token-authed headless daemon (schedules fire, jobs resume) |
| `a1 eval score/replay/golden` | session scoring, golden set |

## Local LLM endpoints

A loopback or private-network `base_url` changes harness behavior
automatically (`llm.IsLocalBaseURL`): memory consolidation batches to every
10th turn, web session naming skips its LLM call (the preview title serves),
fleet concurrency collapses to 1, and the context-window probe runs. All of
it exists for one reason — on a local server, any different-prefix request
evicts the conversation's prompt cache, and the next turn re-prefills the
whole context at hardware prefill speed.

To tune for local:

- **Set `context_window:`** on the model entry when the probe can't see it
  (LM Studio answers 404 to both probes) — the 32K assumption makes
  compaction fire too early on bigger windows.
- **Route ops traffic off the conversation endpoint**: a persona memory
  model on a second server (or a remote one) restores per-turn
  consolidation and LLM session naming at zero cache cost.
- **`web.auto_name_sessions: false`** kills the naming call outright if you
  never want it.
- Keep the model's config stable mid-session: a persona model switch or a
  tools change legitimately rewrites the prefix (one re-prefill, then warm
  again).
- Server side: llama.cpp `--parallel N` isolates N slots; Ollama's idle
  cache is 5 minutes (`OLLAMA_KEEP_ALIVE`); LM Studio caches prefixes
  automatically.

## Extension layout

- User: `~/.a1/extensions/`; project: `<cwd>/.a1/extensions/`.
- Go plugins speaking PXB; author SDK in `ext/go/a1`, protocol in `ext/go/pxb`.
- Authoring and lifecycle: `doc/extensions.md`. Disable: `A1_EXTENSIONS=off`.
