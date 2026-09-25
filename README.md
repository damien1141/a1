# A1

A fork of [phi](https://github.com/pulseaiclub/phi) (e1079e0) with a different direction: tighter UX defaults, fold-based context tooling, task-aware tool allocation, multi-language error translation, build-system awareness, and a stricter operating doctrine baked into the system prompt.

**Docs:** [pulseaiclub.github.io](https://pulseaiclub.github.io/)

**Recommended local models:** for local LLM inference, [el4/Agents-A1-ONYX-GGUF](https://huggingface.co/el4/Agents-A1-ONYX-GGUF); for embeddings, [nomic-ai/nomic-embed-text-v2-moe-GGUF](https://huggingface.co/nomic-ai/nomic-embed-text-v2-moe-GGUF).

## What changed from phi

- **Rebranded identity:** CLI is `a1`, config dir is `~/.a1/`, env vars are `A1_*`.
- **Expanded tool surface:** `context`, `tokenbudget`, `errtrans`, `build`, `deadcode`, `coverage`, `doc`, `apidoc`, `vuln`, `nplusone`, `secret`, `error`, `test`, `rank`, `impact`, `deps`, `migration`, `property`, `stack`, `todo`, `scaffold`, `journal`.
- **Fold-aware context analysis:** `context` reports protected zones, recent-zone discipline, growth-gated compression, and tier accounting instead of generic budget warnings.
- **Task-aware budget allocation:** `tokenbudget` distributes context budget across tools by task type so high-value passes run first.
- **Error normalization:** `errtrans` maps compiler, runtime, and shell errors from Rust, Python, Bash, Lua, TypeScript, Go, and build systems to actionable fixes.
- **Build semantics:** `build` understands Makefiles, CMake, Meson, Cargo, Go modules, npm scripts, and Gradle tasks.
- **Stricter system doctrine:** the embedded system prompt now encodes operating mode, core principles, a 5-gate cognitive loop, workflow rules, deliverable standards, constraints, failure recovery, and ADHD-oriented reporting rules.
- **Dark theme refresh:** default theme aligned to a new dark palette.
- **Terminal history navigation:** Up/Down arrows in the composer browse prior submissions; history is reloaded from the memory bank when resuming old sessions.
- **Session memory bank:** every session gets a JSONL-backed memory bank under `~/.a1/sessions/memory/`; successful and failed tool calls are journaled automatically, and recent memories are injected into the model context as a system message.
- **Semantic code search:** optional local semantic search via Ollama embeddings + a file chunk index; enabled from the config UI or `~/.a1/config.yaml`, exposed as the `vector_search` tool.
- **Call graph / dependency tracer:** `graph` tool builds a directed dependency graph from source imports. Default build uses lightweight parsers for Go, Python, Rust, and JS/TS; build with `-tags treesitter` to enable full tree-sitter grammar support for 40+ languages.

Everything else is still phi under the hood: same TUI, same sub-agent model, same MCP meta-tool design, same extension protocol.

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

## Why this exists

phi is already the leanest practical terminal coding agent harness I have used. This fork does not try to rewrite that core. It adds:

1. More built-in analysis tools so the agent stays inside the terminal instead of switching to shell one-liners.
2. Context tooling that treats the session as something foldable, not just something to truncate.
3. A stricter operating doctrine in the system prompt so the model defaults to evidence-first behavior instead of narration-first behavior.

If you want the original phi without these additions, use [pulseaiclub/phi](https://github.com/pulseaiclub/phi).

## Footprint

- Release binary: ~15 MB
- Idle RSS: ~21 MB
- Time to first frame: ~31 ms
- Go source: ~51k LOC / 316 files / 97 packages
- Session memory bank: `~/.a1/sessions/memory/<session_id>.jsonl`
- Vector search index: workspace-local, created on demand when `vector_search` is used

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
| `doc`          | Doc-to-code sync checker                     |
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
| `vector_search`| Local semantic code search via Ollama embeddings |
| `agent_spawn`  | Start an isolated sub-agent job (async)      |
| `agent_wait`   | Wait for a job; returns short summary only   |
| `agent_list`   | List jobs                                    |
| `agent_cancel` | Cancel a running job                         |

Sub-agent transcripts live under `~/.a1/jobs/<id>/` and are **not** injected into the parent context.

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, code style, and commit conventions.
