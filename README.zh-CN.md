# A1

这是 [phi](https://github.com/pulseaiclub/phi) (e1079e0) 的一个 fork，方向不同：更紧凑的 UX 默认值、fold-based 上下文工具、任务感知的工具分配、多语言错误翻译、构建系统感知，以及更严格的内嵌系统 doctrine。

**文档：** [pulseaiclub.github.io](https://pulseaiclub.github.io/)

- **推荐本地模型：** 本地 LLM 推理推荐 [el4/Agents-A1-ONYX-GGUF](https://huggingface.co/el4/Agents-A1-ONYX-GGUF)；嵌入模型推荐 [nomic-ai/nomic-embed-text-v2-moe-GGUF](https://huggingface.co/nomic-ai/nomic-embed-text-v2-moe-GGUF)。

## 与 phi 的区别

- **身份重命名：** CLI 是 `a1`，配置目录是 `~/.a1/`，环境变量是 `A1_*`。
- **工具集扩展：** `context`、`tokenbudget`、`errtrans`、`build`、`deadcode`、`coverage`、`doc`、`apidoc`、`vuln`、`nplusone`、`secret`、`error`、`test`、`rank`、`impact`、`deps`、`migration`、`property`、`stack`、`todo`、`scaffold`、`journal`。
- **Fold 感知的上下文分析：** `context` 报告受保护区域、最近区纪律、增长门控压缩和分层统计，而不是泛化的预算警告。
- **任务感知的预算分配：** `tokenbudget` 按任务类型在工具间分配上下文预算，优先保障高价值分析通道。
- **错误归一化：** `errtrans` 把 Rust、Python、Bash、Lua、TypeScript、Go 和构建系统的编译器/运行时/Shell 错误映射为可操作修复。
- **构建语义感知：** `build` 理解 Makefile、CMake、Meson、Cargo、Go modules、npm scripts 和 Gradle tasks。
- **更严格的系统 doctrine：** 嵌入式系统提示现在编码了运行模式、核心原则、5 门认知循环、工作流规则、交付标准、约束、失败恢复，以及面向 ADHD 的报告规则。
- **默认主题刷新：** 默认主题对齐新的暗色调色板。
- **终端历史导航：** 在输入框中用上/下箭头浏览历史提交；恢复旧会话时会从 memory bank 重新加载历史输入。
- **会话记忆库：** 每个会话在 `~/.a1/sessions/memory/` 下获得一个 JSONL 记忆库；成功和失败的工具调用会自动记录，最近的记忆会以系统消息形式注入模型上下文。
- **语义代码搜索：** 可选的本地语义搜索，基于 Ollama 嵌入 + 文件块索引；通过 config UI 或 `~/.a1/config.yaml` 启用，以 `vector_search` 工具暴露给模型。
- **调用图 / 依赖追踪：** `graph` 工具从源码导入语句构建有向依赖图。默认构建使用轻量解析器支持 Go、Python、Rust 和 JS/TS；使用 `-tags treesitter` 构建可启用完整 tree-sitter 语法支持，覆盖 40+ 语言。
- **批量编辑器与依赖排序：** `batch` 工具使用拓扑排序对文件进行依赖排序，确保先编辑依赖项再编辑被依赖项。

底层仍然是 phi：相同的 TUI、相同的子代理模型、相同的 MCP 元工具设计、相同的扩展协议。

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

## 为什么存在这个 fork

phi 已经是我用过的最精简、最实用的终端编码代理框架。这个 fork 并不想重写那个核心。它新增的是：

1. 更多内置分析工具，让 agent 留在终端内，而不是不停切到 shell 一行命令。
2. 上下文工具把会话当成可折叠的东西，而不是只当成要截断的东西。
3. 系统提示里更严格的操作 doctrine，让模型默认证据优先，而不是叙述优先。

如果你想要没有这些 additions 的原始 phi，使用 [pulseaiclub/phi](https://github.com/pulseaiclub/phi)。

## 资源占用

- 发布二进制：~15 MB
- 空闲 RSS：~21 MB
- 首帧时间：~31 ms
- Go 源码：~51k LOC / 316 个文件 / 97 个包
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
| `doc`           | 文档与代码同步检查                        |
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
| `vector_search` | 基于 Ollama 嵌入的本地语义代码搜索         |
| `agent_spawn`   | 启动隔离子代理任务（异步）                |
| `agent_wait`    | 等待任务；仅返回简短总结                  |
| `agent_list`    | 列出任务                                  |
| `agent_cancel`  | 取消运行中的任务                          |

子代理完整记录存放在 `~/.a1/jobs/<id>/`，子代理上下文**不会**注入父代理上下文。

开发环境搭建、代码风格与提交规范见 [CONTRIBUTING.md](CONTRIBUTING.md)。
