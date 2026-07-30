# 系统整体架构 — MetaAtoms 实现原理

> 隶属全局架构总览 | 架构层:5 层垂直分层 + 安全横切层 | 核心入口:`src/main.go`

## §1 模块定位

本文是 `codebase-overview` 的「地图层」文档,回答 MetaAtoms 作为一个完整 Agent 系统如何拼起来。它不展开每个模块的内部细节,而是说明各层职责、启动装配顺序、一次用户请求的端到端数据流、跨层依赖边界和后续扩展点。

适用问题:

- MetaAtoms 整体架构是什么,为什么这样分层
- 从启动到 WebUI 可用,主流程做了哪些装配
- 用户在页面输入一句话后,请求如何经过 LLM / 工具 / 记忆 / 安全层
- Tool / Skill / MCP / Prompt / Session / Security 之间是什么关系
- 新能力应该挂在哪一层,避免破坏依赖方向

## §2 总体分层

MetaAtoms 按 `.harness/PROJECT.md` 定义采用 5 层垂直分层架构,安全层作为横切关注点为上层提供兜底保护。

| 层级 | 代码区域 | 核心职责 | 当前主要组件 |
|---|---|---|---|
| 第 1 层 交互层 | `src/interaction/web/` | 用户入口、HTTP/WS 通道、页面渲染、业务消息推送 | Web Server、Handler、ConnectionManager、静态资源、权限弹窗、工具展示 |
| 第 2 层 引擎层 | `src/engine/` + `src/llm/` | ReAct 循环、LLM 适配、Prompt 构建、对话历史编排 | ConversationManager、AgentLoop、Prompt Builder、Anthropic/OpenAI Provider |
| 第 3 层 工具层 | `src/tool/` + `src/skill/` + `src/mcp/` + `src/command/` | Agent 能力集合,把外部世界抽象为可调用工具 | 内置工具、Tool Registry、Skill/use_skill、MCP 适配器、Slash 命令 |
| 第 4 层 记忆层 | `src/memory/` + session 相关包 | 状态持久化、上下文压缩、跨会话记忆召回 | Session Store、Context Manager、AutoLearn Reviewer、MEMORY.md 索引 |

[Why] 安全层不是普通底层依赖:**Why** 权限与沙箱需要拦住 Web 发起的命令、LLM 触发的工具、Skill/MCP 动态注册的工具,因此它横切工具执行链路,不能只挂在某个业务模块内部。

## §3 启动装配链路

`main.run()`(`src/main.go`)是组合根(composition root)。在云端多用户模式下，它先加载全局基线并启动 Web Server，再按登录用户创建独立 tenant runtime。关键装配顺序如下:

1. **全局基线**:MetaAtoms 启动时加载 `~/.metaatoms/setting.json`，全局 `skills/agents/memory` 作为只读基线资源。
2. **用户 runtime**:用户登录接入时创建 `~/.metaatoms/${user_id}`，固定为该用户工作目录;不存在原 CodePilot 的工区路径概念。
3. **合并配置**:加载 `~/.metaatoms/setting.json` + `~/.metaatoms/${user_id}/setting.json`，用户级覆盖全局。
4. **LLM Provider**:按合并后配置创建 Anthropic 或 OpenAI Provider,向引擎层暴露统一的 `llm.Provider` 能力。
5. **工具注册表**:`tool.NewRegistry()` 后调用 `builtin.RegisterWithOptions(...)`(`src/main.go`),注册 ReadFile / WriteFile / EditFile / Bash / Glob / Grep。
6. **ToolHandler**:创建 `conversation.NewToolHandler(...)`,持有 registry、超时、用户目录、事件回调与后续中间件。
7. **Skill 系统**:`skill.LoadAll(...)`(`src/main.go`)加载 builtin / global / user 三档 Skill;随后把 `use_skill` 适配为普通 Tool 注册到 Tool Registry。
8. **MCP 系统**:按合并配置为该用户启动 stdio/HTTP MCP 客户端,把远端 tools 通过 adapter 注册进同一个 Tool Registry。
9. **权限与沙箱**:构造 `security.Checker` 与 `security.Interceptor`,再通过 `toolHandler.SetInterceptor(...)`(`src/main.go`)和 `toolHandler.RegisterMiddleware(security.SandboxMiddleware(...))`(`src/main.go`)插入工具执行链。
10. **只读资源提示**:`buildTenantSkillRootBySource(...)`(`src/main.go`)为 `use_skill` 注入 Skill 根路径提示；tenant memory read store 负责把记忆索引注入 Prompt。文件工具普通路径仍只认用户目录，内置 Skill 通过 `embedded://` 固定放行。
11. **Prompt Source 装配**:`prompt.NewBuilder(...)`(`src/main.go`)按顺序组合 Static / Environment / AGENTS.md / MemoryIndex / SkillsIndex / ConfigAwareness / CodebaseAwareness。
12. **Web Handler 与 Server**:`web.NewHandler(...)`(`src/main.go`)注入 provider、session manager、prompt builder、tool registry、tool handler;`web.NewServer(...)`(`src/main.go`)启动 HTTP + WS 服务。

[Why] main.go 做集中装配:**Why** 各业务包保持小接口依赖,避免包之间互相 new 对方内部实现;未来替换 Provider、增加 MCP transport、扩展 Prompt Source 时,主要改组合根而不是打散到业务模块。

## §4 一次用户请求的端到端数据流

一次 WebUI 输入会经过「交互层 → 引擎层 → 工具层/安全层 → 记忆层 → 交互层」闭环:

```text
Browser
  │ WebSocket user_input
  ▼
web.Handler.runStream
  │ 生成/恢复 Session,构建 System Prompt,收集 ToolSpec
  ▼
ConversationManager.RunAgentLoop
  │ LLM stream: text_delta / tool_use
  ├────────────── text_delta ──────────────► WebSocket assistant_delta
  │
  └─ tool_use
       ▼
     ToolHandler.ExecuteBatch
       │ SandboxMiddleware 路径兜底
       ▼
     Tool.Execute / MCP Tool / use_skill
       │
       ▼
     tool_result 回灌 Conversation history
       │
       └──────────── 继续下一轮 LLM,直到 end_turn / abort / overflow
```

关键控制点:

1. `web.Handler.runStream` 在持锁快照当前会话后调用 `h.conv.RunAgentLoop(...)`(`src/interaction/web/handler.go`)。
2. `ConversationManager.RunAgentLoop(...)`(`src/engine/conversation/manager.go`)持有会话历史,负责每轮 LLM 调用、工具结果写回、终止原因归类。
3. LLM 返回 `tool_use` 后,`ToolHandler.ExecuteBatch(...)`(`src/engine/conversation/tool_handler.go`)按权限分级调度:只读可并行,写入/执行串行。
5. 工具结果以 `tool_result` 形式追加回 conversation history,下一轮 LLM 基于观察继续推理。
6. 一轮结束后 Web 层发送最终消息与状态;自动学习记忆 Reviewer 在后台异步处理,不阻塞用户响应。

## §5 核心数据面与控制面

- **配置面**:全局 `setting.json` + 用户级 `setting.json` / 默认配置驱动 Provider、上下文阈值、Skill/MCP 开关、记忆开关等。
- **会话面**:SessionManager 持久化会话元信息与消息历史,WebUI 的 `/new` `/sessions` `/resume` 都围绕它工作。
- **Prompt 面**:Prompt Builder 把静态规则、运行环境、AGENTS.md、记忆索引、Skill 索引与自感知提示合成最终 System Prompt。
- **工具面**:Tool Registry 是统一发现入口;内置工具、MCP 工具、Skill 工具最终都暴露为同一种 ToolSpec 给 LLM。
- **记忆面**:上下文管理负责短期窗口控制;自动学习记忆读取全局只读基线并写入用户级 memory;二者通过 Prompt Source 被召回。
- **协议面**:Web protocol 把 LLM token、工具事件、权限请求、session 事件统一转成前端可渲染的业务消息。

## §6 依赖方向与边界

推荐依赖方向:

```text
interaction/web
  └─ engine/conversation + prompt + memory/session
       └─ llm + tool registry/handler
            └─ builtin tools + skill adapter + mcp adapter
security 横切 tool handler / web tool status / sandbox path resolver
main.go 作为组合根可以装配所有层
```

边界规则:

- Web 层可以编排 ConversationManager,但具体 Agent Loop 停止原因、tool_result 回灌等逻辑应留在 engine 层。
- 工具实现不应直接依赖 WebUI;工具只返回结构化文本/错误,展示形态由 Web protocol 和前端决定。
- Skill/MCP 适配器只负责把外部能力包装成 `tool.Tool`,不反向控制 Agent Loop。
- Prompt Source 只生成文本片段,不执行工具、不读写会话历史。
- 安全层可以被工具执行链调用,但具体业务模块不应绕过 ToolHandler 直接执行危险操作。
- main.go 是允许知道全局对象的地方;业务包之间需要新增关系时,优先通过接口注入。

## §7 扩展点

| 想扩展的能力 | 推荐入口 | 接入原则 |
|---|---|---|
| 新内置工具 | `src/tool/builtin/` + `RegisterWithOptions` | 实现 `tool.Tool`,定义 schema 和权限,路径类工具接入 SandboxMiddleware |
| 新 Skill | builtin/global/user Skill 目录 | `SKILL.md` 只放索引,大内容拆到 `reference/`,通过 `use_skill` 按需加载 |
| 新 MCP 服务器 | setting MCP 配置 + `src/mcp/` | 由 MCP client 发现工具,adapter 注册为 Tool,复用权限与沙箱链 |
| 新 Prompt 注入 | `src/engine/prompt/` Source | 保持短小、可观测、可配置,避免把长文档常驻注入 |
| 新 Slash 命令 | `src/command/slash/` | 适合确定性 UI/会话操作,不需要 LLM 推理时绕过 Agent Loop |
| 新记忆类型 | `src/memory/` | 区分短期上下文压缩和长期事实沉淀,召回走 Prompt Source |
| Hook 系统 | Step 11 规划 | 应优先挂 ToolHandler/AgentLoop 生命周期事件,不要侵入工具实现 |
| SubAgent 系统 | Step 12 规划 | 作为工具层特殊能力由主 Agent 调度,保持上下文隔离和结果回传 |

## §8 关键设计决策

### 决策 1:WebUI + 本地 HTTP/WS 作为主入口

- **问题**:终端 Agent 需要同时展示流式文本、工具调用状态、diff 等复杂交互。
- **方案**:本地 Web Server + 浏览器页面,用 HTTP 加载静态资源,用 WebSocket 承载实时业务消息。
- **理由**:**Why** WebUI 比传统 TUI 更适合富文本、代码高亮、弹窗确认和多区域布局;后端仍保持本地进程,不牺牲文件/工具访问能力。

### 决策 2:ReAct 循环留在 ConversationManager

- **问题**:LLM 调用、tool_use、tool_result、上下文溢出和用户中断都需要稳定地写入同一份会话历史。
- **方案**:`ConversationManager.RunAgentLoop` 作为对外入口,内部循环执行 LLM → 工具 → 观察回灌。
- **理由**:**Why** 会话历史所有权集中后,Web 层只负责事件转发,工具层只负责执行;终止原因也能统一归类。

### 决策 3:所有能力统一为 Tool Registry

- **问题**:内置工具、Skill、MCP、未来 SubAgent 来源不同,但 LLM 只需要统一的 tool schema。
- **方案**:全部适配成 `tool.Tool`,注册进同一个 Registry,再转换为 Provider tool spec。
- **理由**:**Why** Agent Loop 不需要知道能力来源;权限、沙箱、日志、Web 展示都能复用同一条执行链。

### 决策 4:长知识采用二级加载

- **问题**:系统自感知、配置说明、模块原理等文档很长,常驻塞进 System Prompt 会压缩对话窗口。
- **方案**:System Prompt 只注入短提示;`use_skill` 返回索引;具体 reference 文档通过 ReadFile 按需读取。
- **理由**:**Why** 常驻 token 小,回答需要细节时又能读取真实实现文档,兼顾自感知能力和上下文预算。

### 决策 5:权限拦截与路径沙箱分层

- **问题**:权限规则可配置,但路径越界属于硬安全边界,不能完全依赖 LLM 或配置正确性。

## §9 关键文件索引

| 文件 | 作用 |
|---|---|
| `src/main.go` | `run()` 组合根,全局启动 + 用户 runtime 装配入口 |
| `src/main.go` | 内置工具注册入口 `builtin.RegisterWithOptions` |
| `src/main.go` | Skill 三档加载入口 `skill.LoadAll` |
| `src/main.go` | ToolHandler 注册 `SandboxMiddleware` |
| `src/main.go` | `prompt.NewBuilder` 装配 Prompt Source |
| `src/main.go` | `web.NewHandler` 注入核心依赖 |
| `src/main.go` | `web.NewServer` 创建 HTTP/WS Server |
| `src/interaction/web/handler.go` | Web 请求进入 `RunAgentLoop` 的关键调用点 |
| `src/engine/conversation/manager.go` | Agent Loop 对外入口 |
| `src/engine/conversation/tool_handler.go` | 多工具批量执行与并发/串行调度 |
| `src/security/sandbox_middleware.go` | 路径沙箱中间件 |
| `src/skill/scanner.go` | Skill 加载总入口 |
| `src/tool/registry.go` | Tool Registry 核心结构 |
| `src/llm/` | 多 Provider 统一抽象与内容块模型 |
