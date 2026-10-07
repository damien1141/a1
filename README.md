# A1 v1.5.1

A terminal coding-agent harness for people who want an agent that can actually reason over long sessions, inspect its own permissions, and browse the web without leaving the shell.

Fork of [phi](https://github.com/pulseaiclub/phi) with fold-based context management, live APPA permission control, and Playwright-driven browser automation baked in. ~85k LOC of Go, no runtime bloat.

**Docs:** [pulseaiclub.github.io](https://pulseaiclub.github.io/)

**Recommended local models:** for local LLM inference, [GRM-3.2-Sky-ONYX-GGUF](https://huggingface.co/el4/GRM-3.2-Sky-ONYX-GGUF); for embeddings, [nomic-ai/nomic-embed-text-v2-moe-GGUF](https://huggingface.co/nomic-ai/nomic-embed-text-v2-moe-GGUF).

## Why use this

phi is already the leanest practical terminal coding-agent harness available. This fork keeps that core and adds three things that change how you actually work:

1. **Context management that scales** - `context` treats the session as something foldable, not truncatable. Fold-based compression with a growth gate so noisy overshoots do not trigger costly summarization. KEEP/DROP doctrine embedded in prompts; the model learns what survives compaction.
2. **Live APPA permissions** - shift-tab cycles `interactive`, `readonly`, `autopilot`, and `headless-strict` without restarting. The `permission` tool exposes mode, allow-all state, pre-check/admit decisions, and admission checks so the agent can inspect its own trust level. Not a black box - a legible control plane.
3. **Live browser in the terminal** - `browser` spawns a visible Chromium via Playwright. Navigate, click, type, screenshot, and read page content without leaving the session. Isolated profiles, proxy support, and trimmed output so one page never blows the context window.

On top of that, 30+ analysis tools, semantic code search, dependency graphs, batch edits ordered by import topology, test-impact selection, snapshot/rollback, multi-language error normalization, build-system awareness, and a system doctrine that biases toward evidence over narration.

## Quick start

```sh
curl -fsSL https://raw.githubusercontent.com/damien1141/a1/main/scripts/install.sh | bash
```

```sh
a1 config
a1 run -p "fix the failing test in internal/tools"
```

Build from source:

```sh
make build          # produces ./a1
make install        # build and install into $GOBIN
```

On first start, A1 creates `~/.a1/{bin,skills,hooks,session}`. Search tools (`fd`, `rg`) download into `~/.a1/bin` when missing.

## Philosophy

**Capability first, control second.**

Most agent harnesses restrict the tool surface to keep the model from doing harm. This project takes the opposite stance: give the model every useful tool, then manage risk through permission policy and observability.

That is why 30+ tools feel light instead of heavy. Context management is cheap; capability is expensive. With fold-based compression and growth-gated compaction, the marginal cost of adding a tool is near zero. When a rare tool does fire, it pays for the entire surface.

The same philosophy applies to permissions. The APPA integration does not disable tools - it labels trust, logs effects, enforces admission checks, and makes the entire state inspectable. You do not get safety by removing power; you get it by making power legible and auditable.

Design goals:

- **Breadth over restriction:** tools and features should accumulate, not collapse.
- **Observability over prevention:** see what is happening, then decide.
- **Model-in-the-loop permissions:** the agent can inspect its own permission state via the `permission` tool.
- **Low-friction switching:** shift-tab cycles permission modes live, without restarting.

## Standout features

### Fold-based context management

Long sessions do not have to die to truncation. `context` reports protected zones, recent-zone discipline, growth-gated compression, and tier accounting. Compaction triggers only when context exceeds a floor fraction of the window AND has grown by a threshold since last compression - so brief spikes do not burn tokens on summarization.

The KEEP/DROP doctrine and the model's right to refuse are embedded in compression prompts. Result: the agent keeps reasoning over hours of work instead of forgetting everything every few turns.

### APPA permission mode

Shift-tab between `interactive`, `readonly`, `autopilot`, and `headless-strict` live. The built-in `permission` tool exposes:

- Current mode and session allow-all state
- Pre-check / admit decisions
- Admission checks

This is not a settings menu you have to leave the session to edit. The agent can read its own constraints and explain why it is or is not doing something. Trust, authority, and trajectory confinement - inspectable, auditable, recoverable.

### Live browser control

`browser` opens a visible Chromium window. Use `/browser` to navigate, then `browser_*` tools to interact. `content` returns trimmed visible text, not raw HTML, so one page cannot eat your window. `close` releases the driver; `open` spawns fresh. Multiple isolated profiles, proxy support via config or `/browser proxy`.

Real web interaction inside the agent loop: auth flows, dashboards, docs, any surface a human can see.

### Curated skill library

34 pre-built skills ship with a1 and install to `~/.a1/skills/` on first run. Each is a `SKILL.md` with YAML frontmatter, a structured operating loop, and a `references/` directory of deep-dive docs. The harness loads them into the system prompt and routes by keyword trigger.

- **Keel skills** - `rigorous-coding` (5-gate loop, VERIFIED vs ASSUMED labels), `agent-orchestration` (delegation contracts, output enforcement, red-team critique), `spec-driven-development` (EARS requirements, PROGRESS.md, ADRs), `debugging-wizard` (systematic method, bug taxonomies, blameless postmortems)
- **Language skills** - `golang-pro`, `rust-pro`, `python-pro`, `typescript-pro`, `cpp-pro`, `dotnet-pro`, `swift-pro`, `jvm-pro`, `php-pro`, `ruby-pro`, `lua-pro`
- **Framework skills** - `react-pro`, `astro-pro` (110 design templates, 4 archetypes), `vue-pro`, `angular-pro`, `htmx-pro`, `alpine-pro`
- **Domain skills** - `system-architecture` (DDD, microservices, strangler fig), `api-design` (REST, gRPC, GraphQL, WebSocket), `cloud-native` (K8s, GitOps, service mesh), `sre-reliability` (SLOs, observability, chaos), `data-engineering`, `llm-engineering`, `testing-master`, `code-reviewer` (OWASP, SAST, CVSS)

Treat skills as binding playbooks. When a task matches a skill, read its `SKILL.md` first and follow the operating loop exactly. Skipping steps is the most common source of bugs.

## The stack

Everything else is still phi under the hood: same TUI, same sub-agent model, same MCP meta-tool design, same extension protocol.

What changed:

- **Replaced hash-based edit anchors:** the old `edit` tool required copying `@file path#TAG` and `LINE#HASH` anchors from `read`/`grep` output. That forced the LLM to act as a hash calculator — something it is bad at — and burned tokens on mechanical copying. The new `edit` tool uses plain `old_str` / `new_str` matching, which plays to the LLM's actual strength (pattern matching and text copying) and matches the standard format used by aider, cursor, and cline.
- **Rebranded identity:** CLI is `a1`, config dir is `~/.a1/`, env vars are `A1_*`.
- **Expanded tool surface:** 30+ built-in analysis, search, graph, browser, and permission tools.
- **Session memory bank:** JSONL-backed memory under `~/.a1/sessions/memory/`; successful and failed tool calls are journaled and injected as system messages.
- **Terminal history navigation:** Up/Down arrows browse prior submissions; history reloads from memory bank when resuming sessions.
- **Semantic code search:** optional local embeddings via Ollama + file-chunk index, exposed as `vector_search`.
- **Call graph / dependency tracer:** `graph` builds a directed dependency graph from imports. Default supports Go, Python, Rust, JS/TS; build with `-tags treesitter` for 40+ languages.
- **Batch editor with dependency ordering:** `batch` sorts files by topological order so dependencies are edited before dependents.
- **Test impact selector:** `testimpact` inverts the call graph to find only tests affected by a changed source file.
- **Environment snapshot / rollback:** `snapshot` creates temporary git branches before risky changes and can roll back or delete them.
- **Multi-language error translation:** `errtrans` normalizes Rust, Python, Bash, Lua, TypeScript, Go, and build-system errors to actionable fixes.
- **Build semantics:** `build` understands Makefiles, CMake, Meson, Cargo, Go modules, npm scripts, and Gradle.
- **Task-aware budget allocation:** `tokenbudget` distributes context budget across tools by task type so high-value passes run first.
- **Stricter system doctrine:** the embedded system prompt encodes operating mode, core principles, a 5-gate cognitive loop, workflow rules, deliverable standards, constraints, failure recovery, and ADHD-oriented reporting.
- **Kilo Gateway provider:** `kilo` model preset wires the harness to an OpenRouter-compatible gateway; browser auth and usage popups via a vendored TypeScript extension.

## References

- Context management: training-free multi-generational compression for long-lived coding agents - [billion-context-pi](https://github.com/ranxianglei/billion-context-pi/blob/master/paper/model-driven-incremental-hierarchical-compression-training-free-multi-generational-context-management-for-long-lived-coding-agents.md)
- APPA: Recoverable Information-Flow Control - [arXiv:2607.24625](https://arxiv.org/abs/2607.24625)

## Tools

| Tool           | Purpose                                      |
| ---            | ---                                          |
| `bash`         | Run a shell command in the working directory |
| `read`         | Read a file                                  |
| `write`        | Write a file (gated by permissions)          |
| `edit`         | Targeted edit of a file                      |
| `grep`         | Regex search across files                    |
| `find`         | File patterns (fd)                           |
| `ls`           | Directory listing                            |
| `context`      | Fold-based context analysis                  |
| `tokenbudget`  | Token budget allocation across tools         |
| `errtrans`     | Multi-language error translation             |
| `build`        | Build system semantics                       |
| `deadcode`     | Unused exported symbols                      |
| `coverage`     | Parse `go test -coverprofile`                |
| `docsync`      | Doc-to-code sync checker                     |
| `apidoc`       | API doc stubs from godoc                     |
| `vuln`         | Vulnerability pattern scan                   |
| `nplusone`     | Database queries inside loops                |
| `secret`       | Secret scanner                               |
| `error`        | Known error pattern matcher                  |
| `test`         | Test output interpreter                      |
| `rank`         | File importance heuristics                   |
| `impact`       | Refactor/migration blast radius              |
| `deps`         | Dependency analysis                          |
| `migration`    | Multi-file migration assistance              |
| `property`     | Property-based test support                  |
| `stack`        | Stack trace navigation                       |
| `todo`         | TODO / FIXME collector                       |
| `scaffold`     | Project scaffold aware tools                 |
| `journal`      | Action journal / audit trail                 |
| `graph`        | Dependency/call graph explorer via imports   |
| `batch`        | Dependency-ordered batch file editing        |
| `testimpact`   | Test files affected by a source file change  |
| `snapshot`     | Git-based snapshot and rollback for safe edits |
| `vector_search`| Local semantic code search via Ollama embeddings |
| `judge`        | Local judgment/evaluation via Ollama           |
| `scratchpad`   | Agent notes CRUD under ~/.a1/scratchpad        |
| `config_validate` | Validate ~/.a1/config.yaml and env         |
| `fetch`        | Sandboxed HTTP GET/POST web fetcher            |
| `runtime`      | Parse `go test -json` failures and stack traces |
| `browser`      | Live browser control via Playwright              |
| `permission`   | Live APPA permission inspection and mode control |
| `agent_spawn`  | Start an isolated sub-agent job (async)      |
| `agent_wait`   | Wait for a job; returns short summary only   |
| `agent_list`   | List jobs                                    |
| `agent_cancel` | Cancel a running job                         |

Sub-agent transcripts live under `~/.a1/jobs/<id>/` and are **not** injected into the parent context.

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, code style, and commit conventions.
