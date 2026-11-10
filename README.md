# A1 v2.0.1

A lean Go agent harness with two frontends: a bubbletea TUI and a web GUI (`a1 --web`). Claude Code-style tool loop — plan, call tools, gate risk with permissions — with fold-based context management so sessions survive hours of work instead of dying to truncation.

~91k lines of Go. Direct module deps are few on purpose. No runtime bloat.

**Docs:** [doc/](doc/) — [models](doc/models.md) · [extensions](doc/extensions.md) · [MCP](doc/mcp.md) · [Matrix bridge](doc/matrix.md) · [stoat bridge](doc/stoat.md) · [Discord bridge](doc/discord.md) · [TUI](doc/tui.md) · [project layout](doc/project-layout.md) · [context folding math](doc/context-folding-math.md) · [session context](doc/session-context-building.md)

**中文文档：** [README.zh-CN.md](README.zh-CN.md)

## Quick start

```sh
git clone https://github.com/damien1141/a1 && cd a1
make build          # produces ./a1
make install        # build and install into $GOBIN
```

```sh
a1 config           # pick a model (opens the config editor)
a1 run -p "fix the failing test in internal/tools"
a1                  # the TUI
a1 web              # the web GUI on http://127.0.0.1:9001
a1 matrix           # bridge a Matrix room to a chat session (see doc/matrix.md)
a1 stoat            # bridge a stoat.chat channel to a chat session (see doc/stoat.md)
a1 discord          # bridge a Discord channel to a chat session (see doc/discord.md)
```

On first start, a1 creates `~/.a1/{bin,skills,extensions,session,personas,jobs}`. Search binaries (`fd`, `rg`) download into `~/.a1/bin` when missing.

Models are configured in `~/.a1/config.yaml` with an explicit `api` field per entry (`OpenAI` | `OpenAIResponses` | `Anthropic` | `Gemini`). Built-in presets cover current GPT, DeepSeek, Gemini, Kimi, GLM, and Kilo models — name + api_key is enough; base URL, context window, image support, and thinking defaults fill in automatically. See [doc/models.md](doc/models.md).

```yaml
models:
  - name: deepseek-flash
    api_key: sk-...
    default: true
```

## What's in the box

**Agent core** (`internal/agent`) — streaming executor with a 5-gate cognitive loop, sub-agents (`agent_spawn` / `agent_wait`), and a compacted system prompt (~2.8k tokens) that encodes evidence-first, verify-before-done doctrine.

**19 built-in tools** (~3.7k estimated schema tokens total) — `bash`, `read`, `write`, `search`, `fetch`, `browser` (+ atomic `browser_navigate` / `browser_interact` / `browser_read`), `code` (19 code-analysis modes in one tool), `context`, `document`, `docsync`, `vector_search`, `test`, `secret`, `ask_user`, `permission`, `list_skills`. Multi-plane `search` (rg + graph + fd, auto-ranked), deep-research `fetch` (multi-URL, metadata, outlines, anchor-text links), and DDG-Lite query routing with favicon source pills.

**Personas** (`internal/persona`) — personality profiles with per-persona SQLite semantic memory, prompt injection, avatars, and per-persona runtime overrides: model, permission mode, memory on/off. A persona switch retargets the agent's whole stack mid-session.

**Memory** (`internal/memstore`) — per-persona semantic memory with Ollama embeddings: an inlet injects relevant memories each turn, an outlet consolidates new ones in the background. Activation-scored retrieval, never raw cosine.

**Permissions** (`internal/permission`) — APPA-style live mode cycling: `interactive`, `readonly`, `autopilot`, `headless-strict`. The `permission` tool lets the agent inspect its own trust level. Modes switch without restart.

**Context management** — fold-based compression with growth gating: compaction only triggers past a floor fraction of the window AND after real growth, so spikes don't burn tokens. KEEP/DROP doctrine rides in the compression prompts.

**Web GUI** (`internal/webserver` + `web/`) — Preact + Vite frontend embedded in the binary: a widget dashboard as the landing surface (GitHub-style usage heatmap, pinned sessions, and a personal trio — a month-grid .ics calendar with RRULE expansion, a kanban board, and a multi-account mail reader covering local Maildirs and remote IMAP servers, with accounts and app passwords configurable right in the app (add/edit/test, the keyring never the letters) — behind one `personal.enabled` switch with per-model read/write opt-ins), a split-screen file browser with vendored syntax highlighting in preview AND edit (full-system navigation, `A1_FILES` kill-switch), streaming chat with SSE, transcript replay of stored sessions, persona manager with avatar uploads, permission-mode cycling, branch picker, snapshot/rollback overlay, context breakdown, skills and settings modals, config editor, dark mode, and an edge-hover session drawer collapsed by default at every viewport.

**TUI** (`internal/tui`) — bubbletea frontend with the same engine: command palette (Ctrl+K), slash commands, APPA mode cycling (shift-tab), checklist rendering for TODO.md, session resume.

**Skills** — 42 pre-built skills (language pros, framework pros, domain playbooks like rigorous-coding, agent-orchestration, spec-driven-development, harness-config for self-configuring the harness) that load into the system prompt and route by keyword trigger. `list_skills` exposes them to the model.

**Matrix bridge** (`internal/matrix` + `a1 matrix`) — text the harness: one Matrix room bridged onto one a1 session through the same loopback chat API + SSE the GUI uses (no new agent-side surface). Room messages become prompts, replies return as chunked markdown with a typing indicator, permission approvals decide by y/n (unanswered denies), `!stop`/`!new` steer the session, and restarts resume the conversation. Plaintext rooms; the room is the trust boundary. See [doc/matrix.md](doc/matrix.md).

**stoat bridge** (`internal/stoat` + `a1 stoat`) — the same bridge, second protocol: one stoat.chat channel onto one a1 session, through the same loopback chat API + SSE (still no new agent-side surface). Replies chunk at the platform's 2,000-character cap, a typing indicator runs while turns fly, approvals decide by y/n (unanswered denies), `!stop`/`!new` steer the session, and restarts resume. Other bots never get a turn; the channel is the trust boundary. The websocket half is a hand-rolled RFC 6455 client (`internal/ws`) — zero new dependencies. See [doc/stoat.md](doc/stoat.md).

**Discord bridge** (`internal/discord` + `a1 discord`) — the third surface, on the rulings the roadmap locked: bot token, gateway v10 over the same hand-rolled websocket, `!`-prefix commands, replies chunked at 2,000 UTF-16 units, and a bucket-aware rate-limit pacer on the send path (pace before, sleep-retry after, global lockout). Enable the MESSAGE CONTENT intent in the Developer Portal. See [doc/discord.md](doc/discord.md).

**Extensions & MCP** — PXB subprocess extensions (`a1.yaml` under `~/.a1/extensions`), MCP meta-tools (`mcp_list` / `mcp_inspect` / `mcp_call` — never raw schemas on the model).

## Philosophy

Capability first, control second. Most harnesses restrict the tool surface to keep the model from doing harm. a1 gives the model every useful tool, then manages risk through permission policy and observability. Context management is cheap; capability is expensive — with fold-based compression, the marginal cost of a tool is near zero, and a rare tool firing pays for the whole surface.

## Status

Active development. [TODO.md](TODO.md) tracks what's next. [CHANGELOG.md](CHANGELOG.md) tracks releases. Historical work logs (original issue backlog, per-task progress notes) are archived under [doc/history-*.md](doc/).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). AGENTS.md holds the repo's engineering constraints. Format with `make fmt`; tests use testify.
