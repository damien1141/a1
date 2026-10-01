# A1 v1.0.0

一个终端编码代理框架，专为希望在长会话中保持推理能力、能自检权限、且无需离开终端即可浏览网页的人设计。

基于 [phi](https://github.com/pulseaiclub/phi) 扩展而来，核心差异在于：fold-based 上下文管理、实时 APPA 权限控制、以及 Playwright 驱动的浏览器自动化。

**文档：** [pulseaiclub.github.io](https://pulseaiclub.github.io/)

**推荐本地模型：** 本地 LLM 推理推荐 [el4/Agents-A1-ONYX-GGUF](https://huggingface.co/el4/Agents-A1-ONYX-GGUF)；嵌入模型推荐 [nomic-ai/nomic-embed-text-v2-moe-GGUF](https://huggingface.co/nomic-ai/nomic-embed-text-v2-moe-GGUF)。

## 为什么用这个

phi 已经是我用过的最精简、最实用的终端编码代理框架。这个 fork 保留原有核心，并新增了真正改变工作方式的三件事：

1. **能扩容的上下文管理** — `context` 把会话视为可折叠的对象，而非待截断的对象。Fold-based 压缩配合增长门控，避免短暂峰值触发昂贵的摘要调用。KEEP/DROP 原则和模型拒绝权内嵌于压缩提示中，结果是：代理可以在数小时的工作后继续推理，而不是每隔几轮就把一切忘掉。
2. **实时 APPA 权限控制** — Shift-Tab 在 `interactive`、`readonly`、`autopilot`、`headless-strict` 之间循环切换，无需重启。内置 `permission` 工具可查看当前模式、会话 allow-all 状态、pre-check/admit 决策和 admission 检查，代理可以自行检查信任等级。这不是一个你必须离开会话去改的配置面板，而是一个透明的控制平面。
3. **终端里的实时浏览器** — `browser` 启动一个可见的 Chromium 窗口。用 `/browser` 导航，再用 `browser_*` 工具交互。`content` 返回修剪后的可见文本而非原始 HTML，避免单个页面撑爆上下文窗口。隔离 profile、代理支持、以及 `close`/`open` 生命周期管理，让网页交互成为代理循环的一部分。

此外还有 30+ 分析工具、语义代码搜索、依赖图、按导入拓扑排序的批量编辑、测试影响选择、快照/回滚、多语言错误归一化、构建系统感知，以及一个让模型默认“证据优先、叙述其次”的系统 doctrine。

## 哲学

**能力优先，控制次之。**

大多数代理框架通过限制工具面来防止模型做坏事。本项目反其道而行：给模型所有有用的工具，再通过权限策略和可观测性来管理风险。

这就是为什么 30+ 工具感觉轻盈而非沉重。上下文管理的边际成本几乎为零，能力的边际价值很高。有了基于折叠的压缩和增长门控的压缩，添加一个工具的成本几乎为零；而当一个冷门工具真正被触发时，它会为整个工具面带来回报。

同样的哲学也适用于权限系统。APPA 集成不是禁用工具 —— 它标记信任、记录效果、执行 admission 检查，并使整个状态可查。安全不是靠移除能力实现的，而是靠让能力可见、可审计来实现的。

设计目标：

- **广度优于限制：** 工具和功能应该累加，而不是坍缩。
- **可观测性优于预防：** 先看见正在发生什么，再决定怎么做。
- **模型在环权限：** agent 可以通过 `permission` 工具检查自身的权限状态。
- **低摩擦切换：** Shift-Tab 实时循环权限模式，无需重启。

## 亮点功能

### Fold-based 上下文管理

长会话不必死于截断。`context` 报告受保护区域、最近区纪律、增长门控压缩和分层统计。压缩仅在上下文超过窗口的地板比例 且 自上次压缩以来增长超过阈值时触发 —— 因此短暂峰值不会浪费 token 做摘要。

KEEP/DROP 原则和模型的拒绝权被嵌入压缩提示。结果：代理可以在数小时的工作后继续推理，而不是每隔几轮就把上下文忘掉。

### APPA 权限模式

Shift-Tab 在 `interactive`、`readonly`、`autopilot` 和 `headless-strict` 之间实时切换。内置 `permission` 工具可查看：

- 当前模式和会话 allow-all 状态
- Pre-check / admit 决策
- Admission 检查

这不是你必须离开会话去改的配置菜单。代理可以阅读自身的约束，并解释为什么做或不做某事。信任、授权和轨迹限制 —— 可查、可审计、可恢复。

### 实时浏览器控制

`browser` 启动一个可见的 Chromium 窗口。用 `/browser` 导航，再用 `browser_*` 工具交互。`content` 返回修剪后的可见文本而非原始 HTML，避免单个页面撑爆上下文窗口。隔离 profile、代理支持，以及 `close`/`open` 生命周期管理。

在代理循环内完成真实网页交互：认证流程、仪表盘、文档，人类能看的东西代理都能看。

### 精选技能库

34 个预置技能随 a1 内嵌，首次运行时自动安装到 `~/.a1/skills/`。每个技能是一个 `SKILL.md`，带 YAML frontmatter、结构化操作循环，以及 `references/` 深度文档目录。框架将它们加载进系统提示，并按关键词触发路由。

- **基座技能** — `rigorous-coding`（5 门循环、VERIFIED vs ASSUMED 标签）、`agent-orchestration`（委派契约、输出强制、红队批判）、`spec-driven-development`（EARS 需求、PROGRESS.md、ADR）、`debugging-wizard`（系统化方法、bug 分类、无责复盘）
- **语言技能** — `golang-pro`、`rust-pro`、`python-pro`、`typescript-pro`、`cpp-pro`、`dotnet-pro`、`swift-pro`、`jvm-pro`、`php-pro`、`ruby-pro`、`lua-pro`
- **框架技能** — `react-pro`、`astro-pro`（110 个设计模板、4 种设计原型）、`vue-pro`、`angular-pro`、`htmx-pro`、`alpine-pro`
- **领域技能** — `system-architecture`（DDD、微服务、Strangler Fig）、`api-design`（REST、gRPC、GraphQL、WebSocket）、`cloud-native`（K8s、GitOps、服务网格）、`sre-reliability`（SLO、可观测性、混沌工程）、`data-engineering`、`llm-engineering`、`testing-master`、`code-reviewer`（OWASP、SAST、CVSS）

技能是约束和捷径，而非建议。当任务匹配时，先读 `SKILL.md` 再遵循其操作循环——跳过步骤是 bug 的最常见来源。

## 技术栈

底层仍然是 phi：相同的 TUI、相同的子代理模型、相同的 MCP 元工具设计、相同的扩展协议。

新增内容：

- **身份重命名：** CLI 是 `a1`，配置目录是 `~/.a1/`，环境变量是 `A1_*`。
- **工具集扩展：** 30+ 内置分析、搜索、图、浏览器和权限工具。
- **会话记忆库：** 基于 JSONL 的会话记忆位于 `~/.a1/sessions/memory/`；成功和失败的工具调用自动记录并以系统消息形式注入上下文。
- **终端历史导航：** 上/下箭头浏览历史输入；恢复旧会话时会从 memory bank 重新加载历史。
- **语义代码搜索：** 可选的本地语义搜索，基于 Ollama 嵌入 + 文件块索引，以 `vector_search` 工具暴露。
- **调用图 / 依赖追踪：** `graph` 从源码导入语句构建有向依赖图。默认支持 Go、Python、Rust、JS/TS；使用 `-tags treesitter` 构建可支持 40+ 语言。
- **批量编辑器与依赖排序：** `batch` 使用拓扑排序对文件排序，确保先编辑依赖项再编辑被依赖项。
- **测试影响选择器：** `testimpact` 反转调用图，查找仅受变更源文件影响的测试文件。
- **环境快照 / 回滚：** `snapshot` 在 risky changes 前创建临时 git 分支，事后可回滚或删除。
- **多语言错误翻译：** `errtrans` 将 Rust、Python、Bash、Lua、TypeScript、Go 和构建系统的错误映射为可操作修复。
- **构建语义感知：** `build` 理解 Makefile、CMake、Meson、Cargo、Go modules、npm scripts 和 Gradle。
- **任务感知的预算分配：** `tokenbudget` 按任务类型分配上下文预算，优先保障高价值分析通道。
- **更严格的系统 doctrine：** 嵌入式系统提示编码了运行模式、核心原则、5 门认知循环、工作流规则、交付标准、约束、失败恢复，以及面向 ADHD 的报告规则。
- **Kilo Gateway 提供者：** `kilo` 模型预设将 harness 连接到与 OpenRouter 兼容的 Kilo Gateway；浏览器认证和用量弹窗通过 vendored 的 TypeScript 扩展实现。

## 参考资料

- 上下文管理：面向长生命周期 coding agents 的无训练多代压缩 — [billion-context-pi](https://github.com/ranxianglei/billion-context-pi/blob/master/paper/model-driven-incremental-hierarchical-compression-training-free-multi-generational-context-management-for-long-lived-coding-agents.md)
- APPA：可恢复信息流控制 — [arXiv:2607.24625](https://arxiv.org/abs/2607.24625)

## 资源占用

- 发布二进制：~15 MB
- 空闲 RSS：~21 MB
- 首帧时间：~31 ms
- Go 源码：~82.7k LOC / 551 文件 / 115 个包
- 会话记忆库：`~/.a1/sessions/memory/<session_id>.jsonl`
- 向量搜索索引：工作区本地，启用 `vector_search` 时按需创建

## 工具

| 工具            | 用途                                      |
| ---             | ---                                      |
| `bash`          | 在工作目录运行 shell 命令                 |
| `read`          | 读取文件                                 |
| `write`         | 写入文件（受权限门控）                    |
| `edit`          | 精准编辑文件某一段                        |
| `grep`          | 跨文件正则搜索                            |
| `find`          | 文件模式匹配（fd）                        |
| `ls`            | 目录列表                                  |
| `context`       | Fold 感知的上下文分析                     |
| `tokenbudget`   | 按任务类型分配 token 预算                 |
| `errtrans`      | 多语言错误翻译                            |
| `build`         | 构建系统语义                              |
| `deadcode`      | 未使用的导出符号                          |
| `coverage`      | 解析 `go test -coverprofile`              |
| `docsync`       | 文档与代码同步检查                        |
| `apidoc`        | 从 godoc 生成 API 文档存根                |
| `vuln`          | 漏洞模式扫描                              |
| `nplusone`      | 循环内的数据库查询                        |
| `secret`        | 密钥扫描                                  |
| `error`         | 已知错误模式匹配                          |
| `test`          | 测试输出解释器                            |
| `rank`          | 文件重要性启发式排序                      |
| `impact`        | 重构/迁移影响面评估                       |
| `deps`          | 依赖分析                                  |
| `migration`     | 多文件迁移辅助                            |
| `property`      | 属性测试支持                              |
| `stack`         | 堆栈跟踪导航                              |
| `todo`          | TODO / FIXME 收集器                       |
| `scaffold`      | 项目脚手架感知工具                        |
| `journal`       | 操作日志 / 审计追踪                       |
| `graph`         | 基于导入语句的依赖/调用图 explorer         |
| `batch`         | 依赖排序的批量文件编辑                     |
| `testimpact`    | 源文件变更影响的测试文件                   |
| `snapshot`      | 基于 git 的快照与回滚，保障安全编辑         |
| `vector_search` | 基于 Ollama 嵌入的本地语义代码搜索         |
| `judge`         | 基于 Ollama 的本地判断/评估                 |
| `scratchpad`    | 在 ~/.a1/scratchpad 下做笔记 CRUD          |
| `config_validate` | 校验 ~/.a1/config.yaml 与环境变量         |
| `fetch`         | 沙箱化 HTTP GET/POST 网页获取器            |
| `runtime`       | 解析 `go test -json` 失败与堆栈信息        |
| `browser`       | 通过 Playwright 控制实时浏览器              |
| `permission`    | 实时 APPA 权限检查与模式控制               |
| `agent_spawn`   | 启动隔离子代理任务（异步）                |
| `agent_wait`    | 等待任务；仅返回简短总结                  |
| `agent_list`    | 列出任务                                  |
| `agent_cancel`  | 取消运行中的任务                          |

子代理完整记录存放在 `~/.a1/jobs/<id>/`，子代理上下文**不会**注入父代理上下文。

## 快速开始

```sh
curl -fsSL https://raw.githubusercontent.com/damien1141/a1/main/scripts/install.sh | bash
```

```sh
a1 config
a1 run -p "fix the failing test in internal/tools"
```

从源码构建：

```sh
make build          # 生成 ./a1
make install        # 构建并安装到 $GOBIN
```

首次启动时，A1 会自动创建 `~/.a1/{bin,skills,hooks,session}`。搜索工具（`fd`、`rg`）缺失时会在后台下载到 `~/.a1/bin`。

开发环境搭建、代码风格与提交规范见 [CONTRIBUTING.md](CONTRIBUTING.md)。
