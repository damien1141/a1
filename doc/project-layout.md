# Project layout

| Path                     | Purpose                                        |
| ------------------------ | ---------------------------------------------- |
| `cmd/`                   | Entry points (`main.go` via pli: `a1 run`, `a1 update`, `a1 sessions`, …) |
| `ext/`                   | Public extension API, PXB protocol (`ext/go/pxb`), author SDK (`ext/go/a1`) — nested Go module `github.com/damien1141/a1/ext/go` |
| `internal/util/update/`  | Self-update check + GitHub Releases install    |
| `internal/agent/`        | Agent engine, executor, jobs                     |
| `internal/agent/prompt/` | System prompt templates + Skills/MCP catalogs    |
| `internal/components/`   | TUI widgets (chat, input, palette, mention, diffview, codeview, …) |
| `internal/llm/`          | LLM clients (OpenAI-compatible + Anthropic + Gemini), streaming, skills |
| `internal/project/`      | Workspace layout and config                    |
| `internal/project/model/` | Built-in model presets + request interceptors |
| `internal/session/`      | Session persistence, load/apply                |
| `internal/job/`          | Sub-agent job manager (spawn/wait/cancel)      |
| `internal/tools/`        | Agent tools (`*tool` packages + `tooldef`)     |
| `internal/toolmanager/`  | External tool discovery/download               |
| `internal/tui/editor/`   | TUI root widget (`Editor`), layout, dispatch, branch watch |
| `internal/tui/transcript/` | Session→widget projection (Mapper, Pane) |
| `internal/tui/composer/` | Chat input, slash/@ pickers, palette |
| `internal/tui/footer/`   | Activity spinner, token labels, update hint |
| `internal/tui/overlays/` | Permission / continue-ask panels |
| `internal/tui/diffpane/` | Git diff review overlay (`/diff`) |
| `internal/tui/codepane/` | Source viewer overlay (`/code`) |
| `internal/tui/submit/`   | Submit, cancel, slash dispatch, bash runner |
| `internal/tui/commands/` | Slash/palette registry, session/extension commands |
| `internal/tui/pathutil/` | Cwd + git branch path labels |
| `internal/tui/controller/` | Engine lifecycle, Bus/Msg, activity |
| `internal/version/`      | Build-time `Version` (splash / `a1 update`) |
| `internal/util/`         | Shared helpers (diff, retry, SSE, file search, …) |
| `internal/util/diffreview/` | Unified-diff parse/render, review comments, git load |
| `internal/permission/`   | Permission policy and ask gate                 |
| `internal/extension/`    | PXB extension discover/spawn/runner            |
| `internal/mcp/`          | MCP config + stdio client + pool (meta-tool route) |

## Design docs

| Path | Purpose |
| ---- | ------- |
| [`extensions.md`](extensions.md) | Extensions: discover, API, events, migration from hooks |
| [`mcp.md`](mcp.md) | MCP: zero schema pollution, meta-tools, config, CLI |
| [`models.md`](models.md) | Supported models, presets, explicit `api`, thinking config |
| [`tui.md`](tui.md) | TUI: package layout, aggregation, interaction flows |
| [`session-context-building.md`](session-context-building.md) | Sessions: tree model, compaction, context building |

## Runtime data layout — the Vault (Bet 08, decided 2026-11)

One SQLite database, `~/.a1/a1.db`, owns every durable byte the harness
writes (package `internal/vault`):

| Table family | Scope | Replaces |
| ------------ | ----- | -------- |
| `personas` | user | `personas/personas.json` |
| `memory_banks` | per bank key | `personas/memory/persona:<id>.jsonl`, `sessions/memory/<sid>.jsonl` |
| `evidence_receipts` | per project session-dir | `session/--<cwd>--/evidence.jsonl` |
| `sessions` + `session_entries` | per project | `session/--<cwd>--/*.jsonl` (tree keyed `(session, seq, parent)`, payload byte-faithful) |
| `mem_p_<id>_*` / `mem_shared_*` | per persona / engine | `personas/memory/semantic/<id>.sqlite`, `agent/memories.db` |

Decisions, made once:

- **One DB, not per-project DBs.** Backup and delete are the operations
  that matter: "copy one file" and "DELETE with FK cascade" beat any
  sharding argument at personal scale. Sessions stay project-*scoped*
  (rows keyed by the project session dir), just not project-*stored*.
- **The synthetic path contract.** Vault sessions report the same
  `<dir>/<ts>_<id>.jsonl` path the file era used, so every path-derived
  behavior — evidence scope, resume-by-path, job session logging, the
  screenshots directory — is unchanged. Files that exist from before the
  migration are stale shadows; resolution is always vault-first.
- **config.yaml, mcp.json, skills, extensions, and avatars stay files** —
  things a user edits by hand or ships as artifacts. Everything the
  harness alone reads and writes is rows.
- **One-shot migration.** `vault.Boot` imports the legacy layout on
  first open and stamps `vault_meta.migrated`; every step is idempotent
  by key, so a crash mid-import simply resumes. Legacy files are left in
  place (delete at will) but never win resolution again.
- **Degradation is a feature.** A vault that fails to open logs and the
  process keeps running on the legacy file layout — the harness never
  refuses to start because its database had a bad morning.
