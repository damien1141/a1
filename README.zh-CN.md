# A1 v2.0.1

一个精简的 Go 代理框架，双前端：bubbletea TUI 和 Web GUI（`a1 --web`）。Claude Code 风格的工具循环——规划、调用工具、用权限门控风险——配合基于折叠的上下文管理，让会话可以连续工作数小时而不死于截断。

约 91k 行 Go 代码。刻意保持极少的直接依赖，没有运行时膨胀。

**文档：** [doc/](doc/) — [模型配置](doc/models.md) · [扩展](doc/extensions.md) · [MCP](doc/mcp.md) · [TUI](doc/tui.md) · [项目结构](doc/project-layout.md) · [上下文折叠数学](doc/context-folding-math.md) · [会话上下文构建](doc/session-context-building.md)

**English docs:** [README.md](README.md)

## 快速开始

```sh
git clone https://github.com/damien1141/a1 && cd a1
make build          # 生成 ./a1
make install        # 构建并安装到 $GOBIN
```

```sh
a1 config           # 选择模型（打开配置编辑器）
a1 run -p "fix the failing test in internal/tools"
a1                  # TUI
a1 web              # Web GUI，监听 http://127.0.0.1:9001
```

首次启动时，a1 会创建 `~/.a1/{bin,skills,extensions,session,personas,jobs}`。搜索二进制（`fd`、`rg`）缺失时会自动下载到 `~/.a1/bin`。

模型在 `~/.a1/config.yaml` 中配置，每个条目带显式 `api` 字段（`OpenAI` | `OpenAIResponses` | `Anthropic` | `Gemini`）。内置预设覆盖当前 GPT、DeepSeek、Gemini、Kimi、GLM、Kilo 模型——只需 name + api_key，base URL、上下文窗口、图片支持、thinking 默认值自动填充。详见 [doc/models.md](doc/models.md)。

```yaml
models:
  - name: deepseek-flash
    api_key: sk-...
    default: true
```

## 包含什么

**代理内核**（`internal/agent`）—— 流式执行器，带 5-gate 认知循环、子代理（`agent_spawn` / `agent_wait`）、以及编码"证据优先、完成前必验证"原则的精简系统提示（约 2.8k token）。

**19 个内置工具**（估算 schema 共约 3.7k token）—— `bash`、`read`、`write`、`search`、`fetch`、`browser`（+ 原子化 `browser_navigate` / `browser_interact` / `browser_read`）、`code`（一个工具 19 种代码分析模式）、`context`、`document`、`docsync`、`vector_search`、`test`、`secret`、`ask_user`、`permission`、`list_skills`。多平面 `search`（rg + graph + fd，自动排序）、深度研究 `fetch`（多 URL、元数据、大纲、锚文本链接）、以及带 favicon 来源药丸的 DDG-Lite 查询路由。

**人格（Personas）**（`internal/persona`）—— 性格档案，带每人格 SQLite 语义记忆、提示注入、头像、以及每人格运行时覆盖：模型、权限模式、记忆开关。切换人格会在会话中途重新定向整个代理栈。

**记忆**（`internal/memstore`）—— 每人格语义记忆，Ollama embedding：入口每回合注入相关记忆，出口在后台整理新记忆。按激活度评分检索，绝不用原始余弦相似度。

**权限**（`internal/permission`）—— APPA 风格实时模式循环：`interactive`、`readonly`、`autopilot`、`headless-strict`。`permission` 工具让代理能自检信任等级。切换模式无需重启。

**上下文管理** —— 基于折叠的压缩配增长门控：只有当上下文超过窗口的地板比例 且 自上次压缩以来真实增长时才触发压缩，短暂峰值不会浪费 token。KEEP/DROP 原则内嵌于压缩提示。

**Web GUI**（`internal/webserver` + `web/`）—— 内嵌于二进制的 Preact + Vite 前端：以组件仪表盘为落地页（GitHub 风格的使用热力图、置顶会话、个人三件套 —— 月历网格式 .ics 日历（含 RRULE 展开）、看板、多账户邮件阅读器（本地 Maildir 与远程 IMAP 服务器皆可，账户与应用密码可直接在应用内配置：新增/编辑/连通性测试，只动钥匙串、不动信件）—— 由单一 `personal.enabled` 开关加逐模型读写授权把关）、分屏文件浏览器（全系统导航、预览与编辑均有精简内置的语法高亮、`A1_FILES` 熔断开关）、SSE 流式聊天、历史会话回放、带头像上传的人格管理器、权限模式循环、分支选择器、快照/回滚、上下文分解、技能与设置弹窗、配置编辑器、深色模式，以及默认收起、鼠标贴近左缘即滑出的会话抽屉。

**TUI**（`internal/tui`）—— bubbletea 前端，同一引擎：命令面板（Ctrl+K）、斜杠命令、APPA 模式循环（shift-tab）、TODO.md 清单渲染、会话恢复。

**技能** —— 41 个预置技能（语言专家、框架专家、以及 rigorous-coding、agent-orchestration、spec-driven-development 等领域剧本），加载进系统提示并按关键词触发路由。`list_skills` 向模型暴露它们。

**扩展与 MCP** —— PXB 子进程扩展（`~/.a1/extensions` 下的 `a1.yaml`）、MCP 元工具（`mcp_list` / `mcp_inspect` / `mcp_call` —— 永不把原始 schema 压给模型）。

## 哲学

能力优先，控制次之。大多数框架靠收缩工具面来防止模型做坏事；a1 给模型所有有用的工具，再用权限策略和可观测性管理风险。上下文管理很便宜，能力很贵——有了基于折叠的压缩，一个工具的边际成本接近零，而冷门工具被触发时为整个工具面买单。

## 状态

活跃开发中。[TODO.md](TODO.md) 追踪下一步工作，[CHANGELOG.md](CHANGELOG.md) 追踪发布。历史工作记录（最初的 issue 积压、各任务的进度笔记）归档在 [doc/history-*.md](doc/)。

## 参与贡献

见 [CONTRIBUTING.md](CONTRIBUTING.md)。AGENTS.md 是仓库的工程约束。用 `make fmt` 格式化；测试用 testify。
