# 背景

我们正在实现一款类似 Atoms（https://atoms.dev/）的云端多租户 AI Agent。Atoms 的定位是用一支多智能体 AI 团队，帮助用户从想法出发，完成市场研究、产品定义、全栈开发、部署上线、SEO/广告增长和数据分析；Atoms 官网也把它描述为“把想法变成可销售的产品”的 AI 开发平台，并强调无需编码。作为一个非商业化的个人项目，MetaAtoms 不用完全复制 Atoms，实现方式也不一定要完全参考 Atoms，只需要实现核心功能：根据用户想法交付一个可运行的应用源码，并且具有良好的交互体验。在支持基本功能的前提下，也可以提供一些和 Atoms 差异化的功能思考和实现，比如支持用户自定义配置 MCP、Skill、Agent，以及针对用户的长期学习记忆能力等。

本质上 MetaAtoms 也是一个 AI Coding Agent，需要具备 Agent 的基本能力，比如循环推理、调用工具、反馈、会话管理、上下文管理等机制。这部分计划直接从之前的 CodePilot（https://github.com/MeiCorl/CodePilot）迁移过来。但与 CodePilot 个人终端 AI 编程助手的定位不同，MetaAtoms 是一款面向多租户的云端 AI 编程助手，因此很多功能迁移后需要作适当修改。比如需要 UI 重构、登录验证、会话支持多租户、数据（包括配置数据、会话数据、记忆数据等）需要按租户隔离、权限系统需要支持多租户并确保用户不能看到其他用户的数据、Agent 行为模式调整等。

# 项目架构

MetaAtoms 的底层思路参考 CodePilot：保留「LLM Provider -> Conversation / Agent Loop -> Tool Registry -> Tool Result 回灌」这条 AI Coding Agent 主链路，并继续沿用 System Prompt 组合、工具注册、会话上下文、文件/命令工具等核心抽象。与 CodePilot 的个人终端助手不同，MetaAtoms 在这条主链路外增加了 WebUI、多租户 runtime、认证、租户级数据隔离、权限沙箱、用户级 Skill/MCP/Agent 配置和长期记忆能力。

整体采用「5 层垂直分层 + 安全横切」架构，`src/main.go` 作为组合根负责装配各层依赖：

| 层级 | 代码区域 | 核心职责 | 主要组件 |
| --- | --- | --- | --- |
| 第 1 层：交互层 | `src/interaction/web/` | 用户入口、HTTP/WS 通信、页面渲染、流式消息、权限确认、工具结果展示 | Web Server、Router、Handler、WebSocket、静态资源、diff 展示、会话侧边栏 |
| 第 2 层：引擎层 | `src/engine/`、`src/llm/` | ReAct 循环、LLM 适配、Prompt 构建、会话历史编排、Agent 运行控制 | ConversationManager、AgentLoop、ToolHandler、Prompt Builder、Anthropic/OpenAI Provider |
| 第 3 层：工具层 | `src/tool/`、`src/skill/`、`src/mcp/`、`src/command/`、`src/hook/`、`src/subagent/` | 把内置工具、Skill、MCP、Slash、Hook、SubAgent 统一抽象为可注册、可审计、可权限控制的 Agent 能力 | Tool Registry、内置工具、use_skill、MCP Adapter、Slash Registry、HookEngine、agent/task_status 工具 |
| 第 4 层：记忆与数据层 | `src/memory/`、会话持久化相关包 | 会话状态、上下文窗口、压缩摘要、长期记忆、用户级数据持久化 | Session Store、Context Compactor、Tool Result Store、AutoLearn Store、MEMORY.md 索引 |
| 第 5 层：安全层 | `src/auth/`、`src/security/` | 登录认证、权限决策、HITL、人类确认、路径沙箱、黑名单、多租户边界保护 | Auth、Checker、Interceptor、SandboxMiddleware、Path Policy、Blacklist |

## 核心运行模型

一次用户请求的主流程如下：

```text
Browser/WebUI
  -> HTTP/WS
  -> web.Handler
  -> Prompt Builder + Session Manager
  -> ConversationManager.RunAgentLoop
  -> LLM Provider stream
  -> ToolHandler.ExecuteBatch
  -> Security Interceptor + SandboxMiddleware
  -> Tool / Skill / MCP / SubAgent / Hook
  -> tool_result 写回会话历史
  -> WebSocket 推送流式文本、工具状态、权限请求和最终结果
```

这个模型保持了 CodePilot 的 Agent 内核优势：ConversationManager 持有对话历史，Agent Loop 负责 LLM/工具/观察回灌闭环，Tool Registry 提供统一工具发现入口。MetaAtoms 的扩展点则集中在 runtime 装配、用户级资源加载、安全拦截和 WebUI 协议层，避免把云端多租户逻辑散落到每个工具实现里。

## 多租户 Runtime 与数据隔离

- 全局配置：`~/.metaatoms/setting.json` 作为系统基线，提供默认 LLM、权限、MCP、Skill、记忆等配置。
- 用户 runtime：用户登录后创建 `~/.metaatoms/${user_id}`，该目录是用户固定工作目录，不再沿用 CodePilot 的任意工区路径模型。
- 配置合并：运行时按「全局默认 -> 用户级覆盖」合并配置，用户可以自定义 MCP、Skill、Agent、记忆和权限策略。
- 资源加载：Skill/Agent/Memory 支持 builtin、global、user 多来源加载；用户级资源优先级最高，写入只落到用户目录。
- 数据隔离：配置数据、会话数据、记忆数据、工具写入路径都按用户目录隔离；跨用户读取和写入由路径沙箱兜底阻断。

## 关键模块关系

- `src/main.go`：组合根。负责加载配置、创建 Provider、注册工具、装配权限/沙箱、加载 Skill/MCP/Agent、启动 Web Server。
- `src/interaction/web/`：云端交互入口。HTTP 提供静态页面，WebSocket 承载实时输入、流式输出、工具状态、权限确认和会话事件。
- `src/engine/conversation/`：Agent Loop 核心。继承 CodePilot 的循环推理模式，统一处理 LLM 调用、tool_use、tool_result、终止原因和上下文溢出。
- `src/engine/prompt/`：System Prompt 构建。按 Source 组合静态规则、环境信息、AGENTS.md、Skill 索引、记忆索引、配置自感知和代码自感知。
- `src/llm/`：模型适配层。通过统一 `Provider` 接口屏蔽 Anthropic/OpenAI 等供应商差异。
- `src/tool/`：统一工具协议。内置 ReadFile/WriteFile/EditFile/Bash/Glob/Grep 等工具，所有外部能力最终都注册为 `tool.Tool`。
- `src/skill/`：Skill 系统。支持内置、全局、用户级 Skill，并通过 `use_skill` 让 Agent 按需加载长文档能力。
- `src/mcp/`：MCP 集成。支持 stdio/HTTP transport、JSON-RPC、连接池、重连和工具适配。
- `src/security/`：权限与沙箱。对工具执行链进行 allow/ask/deny 判定，结合 HITL 和路径硬边界保护多租户数据。
- `src/memory/`：会话、上下文和长期记忆。负责 session 持久化、上下文压缩、自动学习和记忆召回。
- `src/hook/`：生命周期扩展。把工具、会话、压缩、Prompt、Agent Loop 等事件开放给 command/http/prompt/agent action。
- `src/subagent/`：多 Agent 协作能力。通过统一 `agent` 工具调度 defined/fork 子 Agent，并支持后台任务状态查询。

## 架构约束

- 上层可以通过接口调用下层能力，业务包之间避免直接依赖对方内部实现；跨模块装配优先放在 `src/main.go`。
- Web 层只负责交互协议和展示编排，不承载 Agent Loop 的核心推理状态。
- 工具实现只返回结构化结果，不直接控制 WebUI；展示形态由 `interaction/web` 协议和前端决定。
- Skill、MCP、SubAgent 都必须适配为统一 Tool，复用 ToolHandler、权限拦截、沙箱和日志链路。
- 权限策略可配置，但路径沙箱是硬边界；任何文件读写/命令执行都不能绕过安全层。
- 长文档和系统自感知信息采用「短 Prompt 指引 + Skill/reference 按需加载」，避免常驻占用上下文窗口。

# 项目实现进度

## 已完成 / 进行中

- [x] 项目定位与总体方案：确定 MetaAtoms 以 CodePilot Agent 内核为基础，演进为云端多租户 AI Coding Agent。
- [x] CodePilot 核心能力迁移：完成 LLM 调用、ConversationManager、ReAct Agent Loop、工具调用和 tool_result 回灌主链路。
- [x] System Prompt 调整：完成静态规则、环境上下文、AGENTS.md、Skill 索引、记忆索引、配置自感知、代码自感知等 Source 组合。
- [x] 注册与登录认证：增加用户认证入口和用户身份识别能力。
- [x] 多用户 runtime：支持用户级工作目录、会话数据、配置数据和记忆数据隔离。
- [x] WebUI 重构：由个人终端交互演进为 HTTP + WebSocket 的 Web 交互入口，支持流式渲染、工具状态和 diff 展示。
- [x] LLM 多 Provider 适配：支持 Anthropic/OpenAI 统一 Provider 抽象。
- [x] 内置工具系统：完成 ReadFile、WriteFile、EditFile、Bash、Glob、Grep 等工具注册与执行。
- [x] 权限系统与路径沙箱：完成 allow/ask/deny、HITL、黑名单、路径边界和工具执行中间件。
- [x] MCP 集成：支持用户自定义 MCP 配置、stdio/HTTP transport、会话池和工具适配注册。
- [x] Skill 系统：支持 builtin/global/user 多来源 Skill、`use_skill` 工具和 Skill 索引注入。
- [x] 会话管理：支持新建、恢复、列表、导出和消息持久化。
- [x] 上下文管理：支持窗口测量、轻量压缩、摘要压缩、工具结果归档和上下文溢出兜底。
- [x] 自动学习记忆：支持用户级长期记忆写入、索引注入、后台 Reviewer 和敏感信息处理。
- [x] Hook 系统：支持多类生命周期事件、条件匹配和 command/http/prompt/agent action。
- [x] SubAgent 基础能力：完成 agent/task_status 工具、内置角色、defined/fork 运行、后台任务和 WebUI 状态回传。

## 后续优化

- [ ] WebUI 体验增强：完善项目文件浏览、任务看板、权限确认体验、移动端适配和长任务状态管理。
- [ ] Agent 行为流程定制：面向“从想法到可运行应用”的任务阶段，沉淀需求澄清、方案设计、开发、测试、部署、增长分析等可配置流程。
- [ ] 测试与发布工程：补齐端到端测试、关键安全回归测试、构建产物校验和部署文档。
