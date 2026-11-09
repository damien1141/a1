---
name: harness-config
description: Configure the a1 harness itself — register MCP servers (a1 mcp add, ~/.a1/mcp.json), edit ~/.a1/config.yaml (models, permissions, agents, checks, schedules, browser, semantic search), install skills and extensions, and know what applies live vs. needs a restart. The harness's config is files, its state is the SQLite vault; never confuse the two.
license: MIT
metadata:
  author: super-skills
  version: "1.0.0"
  domain: meta
  triggers: configure harness, harness config, config.yaml, a1 config, mcp server, add mcp, mcp.json, model context protocol, register server, api key, provider setup, default model, permission mode, autopilot, yolo, sandbox config, subagent models, agents config, evidence check, custom check, schedules config, cron config, install skill, skill format, extension install, self-configure, set up tools, restart needed, what needs restart
  role: specialist
  scope: implementation
  output-format: code
  related-skills: agent-orchestration, cli-developer, debugging-wizard
---

# Harness Config

The a1 harness is configured by **files you can read and edit**; its runtime *state* (sessions, memory, evidence, jobs, telemetry) lives in the SQLite vault `~/.a1/a1.db` and is never hand-edited. This skill is the map to those files, the tools that change them safely, and the line between what applies **live** and what needs a **restart**.

The design guarantees you lean on: a malformed config never bricks the harness — it degrades, logs, and keeps running; the HTML editor auto-backs-up to `config.yaml.bak`; the `write` tool enforces read-before-write. Your job is to edit precisely, validate before declaring done, and tell the user honestly what restarts.

## The Map

| Path | Owns | Change via |
|------|------|-----------|
| `~/.a1/config.yaml` | models + api keys, permissions policy, agents, jev, browser, personal, ui/web, personas.active, tools caps, `checks:`, `schedules:` | `write` tool (edit mode), then validate |
| `~/.a1/mcp.json` | user-level MCP servers | `a1 mcp add` / `remove` (preferred) or write tool |
| `<cwd>/.a1/mcp.json` | project-level MCP servers — same-named servers **override** user config | write tool (watch secrets: this one can be committed) |
| `~/.a1/skills/<name>/SKILL.md` | user skills — take precedence over the built-in defaults, never overwritten | `write` tool; discoverable immediately |
| `~/.a1/extensions/`, `<cwd>/.a1/extensions/` | Go extension plugins (PXB protocol) | build + drop per `doc/extensions.md` |
| `~/.a1/personas/` | persona registry, avatars, per-persona memory banks | persona tools / web GUI |
| `~/.a1/a1.db` | **the vault** — sessions, memory, evidence, jobs, schedules mirror, telemetry, eval runs | **never hand-edit**; tools and CLI only |
| `~/.a1/logs/mcp/<name>.log` | per-server MCP logs | read when a server misbehaves |

## When to Use

- "Add / set up an MCP server" (fetch, browser, github, a private one) → the MCP recipe below
- "Use a different model / provider / api key" → models block in config.yaml
- "Make this permission setup permanent" / persistent autopilot or sandbox tuning → permissions block
- "Add a lint/test gate the harness tracks as evidence" → `checks:` block
- "Give subagents their own models" or cap tool calls → agents / tools blocks
- "Install a skill" → skills dir (drop-in, live discovery)
- Any question of the shape "does this need a restart?" → the matrix below

Skip for: one-shot runtime permission flips (use the `permission` tool — session-live), one-shot wake-ups (use the `schedule` tool — vault-durable), anything about past sessions (session_search).

## Live vs. Restart

**Applies immediately, no restart:**

- `list_skills` re-reads the skills dir **on every call** — a SKILL.md you just wrote is discoverable this turn
- `permission` tool actions (status, toggle_allow_all, …) — session-scoped
- `budget` tool declarations, `contract` declarations — per-turn
- `schedule` tool create/list/cancel — writes the vault + live scheduler; survives restarts
- Calls to **already-configured** MCP servers (lazy start per call)

**Needs a restart to apply:**

- Newly **added** MCP servers — the pool loads at startup (`a1 mcp doctor` sees them; the running session's meta-tools do not until restart)
- Every config.yaml key: models, permissions policy, agents, jev, browser, personal, semantic_search, ui, web, tools, `checks:`, config-declared `schedules:` (synced at boot)

Sessions survive restarts — say so: the user resumes via `a1 sessions list` / `--continue-last` (CLI) or the session picker (TUI/GUI). Jobs park as pending and resume.

## Operating Loop

1. **Read before writing.** `read` the current config.yaml (or mcp.json) in full. The `write` tool enforces read-before-write; editing blind forfeits unknown keys you'd otherwise drop.
2. **Edit in place, never regenerate.** Use the write tool's edit mode on the specific key. A config.yaml rewritten from memory silently deletes keys you forgot — future versions' keys, comments, and the user's tuning.
3. **Validate.** Run the config validator (`config_validate` — also reachable as `context` tool with `mode: "config"`) against the file you just wrote. It reports missing required fields, invalid values, and env conflicts. Fix and re-run until clean.
4. **Verify the external side.** MCP changes: `a1 mcp doctor` (config + connectivity, exit code tells the truth); on failure read `~/.a1/logs/mcp/<name>.log`. Model/provider changes: nothing to verify pre-restart beyond validation — the harness degrades rather than bricks.
5. **Report the restart line honestly.** State exactly what changed, what is live now, what the restart applies, and how sessions resume. "Done" without the restart caveat is a false claim when config.yaml moved.

## MCP Recipe

```sh
a1 mcp add fetch -- npx -y @modelcontextprotocol/server-fetch
a1 mcp list
a1 mcp doctor
```

- Config lands in `~/.a1/mcp.json` as `{"servers": {"fetch": {"transport": "stdio", "command": ["npx"], "args": ["-y", "@modelcontextprotocol/server-fetch"]}}}`; http servers use `"transport": "http", "url": …, "headers": {…}`.
- Migrating Claude/Cursor config: `mcpServers` → `servers`, string `command` becomes the first element of the `command` array.
- Project override: `<cwd>/.a1/mcp.json` beats user config for same-named servers — and may be committed, so no secrets there.
- The agent side stays context-cheap by design: schemas never enter the model context; the running model gets `mcp_list` / `mcp_inspect` / `mcp_call` meta-tools and server names in its prompt. One `a1 mcp call <server> <tool> '<json>'` proves the server works end-to-end.
- `A1_MCP=off` disables the whole subsystem; `A1_MCP_LOG_DIR` moves the logs.
- **Restart required** for a newly added server to appear in a running session's meta-tools.

## Reference Guide

| Topic | Reference | Load When |
|-------|-----------|-----------|
| Full annotated config.yaml schema — every key, every permission mode, model entries, checks, schedules | `references/config-reference.md` | About to write config.yaml |
| All `A1_*` env vars and what they override | `references/config-reference.md` | Env vs. file precedence questions |
| mcp.json schema, project overrides, log locations | `references/config-reference.md` | MCP setup beyond the basic recipe |
| SKILL.md format, precedence, `references/` convention | `references/config-reference.md` | Installing or revising skills |
| CLI surface map (`a1 mcp`, `a1 config`, `a1 sessions`, `a1 run` flags) | `references/config-reference.md` | Choosing between file edit and CLI |

## Constraints

### MUST DO

- **Read the file before editing it, and edit in place.** Never regenerate config.yaml or mcp.json from memory — unknown keys, comments, and future-version keys must survive your edit.
- **Validate before declaring done.** `config_validate` the file you wrote; for MCP also `a1 mcp doctor`. A config change is VERIFIED only when the validator (and doctor, where applicable) ran clean after the edit.
- **Prefer `a1 mcp add` / `remove` over hand-writing mcp.json** — the CLI owns the file's shape; hand edits risk JSON syntax errors the CLI never makes.
- **Keep secrets at user level.** api keys belong in `~/.a1/config.yaml` / `~/.a1/mcp.json` (0600-ish, never committed), not in `<cwd>/.a1/` files a repo might track.
- **State the restart boundary.** Every config.yaml or mcp.json change gets an explicit "restart applies this; sessions resume via …" sentence in your answer.
- **Use `default: true` on a models entry to pick the default model.** There is no `default_model:` top-level key; absent the flag, the first entry wins.
- **Create live schedules with the `schedule` tool**, not by hand-editing the `schedules:` block — the tool path writes the vault and arms the scheduler now; config-declared schedules only sync at boot.

### MUST NOT DO

- **Never read, write, or "fix" `~/.a1/a1.db`.** The vault is rows, not files; every legitimate mutation has a tool or CLI path. Hand-edits corrupt exactly the data that survives restarts.
- **Never edit `config.yaml.bak`** — it is the rollback path, not a second config.
- **Never claim a restart-required change is live.** A newly added MCP server is not callable by the running session no matter what `a1 mcp doctor` says.
- **Never widen permissions silently.** Setting `mode: yolo`, `dangerously_allow_all: true`, or sandbox off in config.yaml is a trust-boundary change — say it plainly, note the blast radius (yolo = uncapped calls *and* no sandbox walls), and let the user choose.
- **Never invent config keys.** If a key isn't in `references/config-reference.md`, it doesn't exist; the YAML parser ignores unknown keys, so a typo'd key is a silently dead setting. Validate, and when unsure, read `internal/project/config.go` in the checkout — it is the schema's source of truth.
