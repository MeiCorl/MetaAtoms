# Step 12 — SubAgent

## 背景

MetaAtoms 已具备 LLM 适配、工具系统、Agent Loop、System Prompt、权限、MCP、上下文管理、记忆、Slash 命令、Skill 与 Hook。当前主 Agent 在复杂任务中仍需要把探索、规划、实现等不同阶段塞进同一段对话历史，容易造成上下文污染、权限状态混杂与长任务阻塞。

SubAgent 的目标是在工具层提供一个主 Agent 可调用的统一能力：主 Agent 用同一个工具把子任务交给独立子 Agent；子 Agent 在自己的运行时状态中跑到底，完成后把结构化结果异步回送主对话。这样主 Agent 只保留任务结果，不承接子 Agent 的中间推理、工具噪声与权限轨迹。

## 目标用户

- 使用 MetaAtoms 完成复杂代码任务的终端用户。
- 需要把探索、规划、实现、审查等任务拆给独立上下文执行的主 Agent。
- 希望通过角色定义扩展 MetaAtoms 工作方式的项目维护者与高级用户。

## 能力清单

1. 提供一个统一的 Agent 工具，工具列表对 LLM 始终稳定；通过类型参数分流到「定义式」与「Fork 式」两条执行路径。
2. 支持 Markdown + YAML frontmatter 的 Agent 角色定义，frontmatter 描述角色名、用途说明、工具白名单、工具黑名单、模型、最大轮次与权限模式，正文作为子 Agent 生命周期内固定系统提示。
3. 支持多来源加载 Agent 定义，按项目级高于用户级、高于内置、高于插件的顺序覆盖同名角色。
4. 内置 Explore、Plan、General-Purpose 三个角色定义：Explore 只读探索，Plan 只规划不执行，General-Purpose 拥有完整工具权限并用于需要完整能力的独立任务。
5. 定义式子 Agent 从空白对话启动，只带固定角色系统提示与本次任务输入。
6. Fork 式子 Agent 继承父对话历史与父工具集，使首次请求尽量复用父上下文的 prompt cache；Fork 式强制后台运行。
7. 子 Agent 运行时状态相互隔离，包括消息历史、权限追踪、文件读取缓存与 token 计数；共享 LLM 客户端、Hook 引擎、文件系统与工具实现基础设施。
8. 子 Agent 采用非交互「跑到底」模式，模型不再调用工具即视为完成；完成、失败或超时后生成结构化结果。
9. 提供多层工具过滤防线：全局禁止、角色额外限制、后台白名单与防嵌套限制，阻止子 Agent 无限创建子 Agent。
10. 提供后台任务管理器追踪子 Agent 状态、结果、错误与用量，支持显式后台、超时自动转后台、手动切后台三种路径。
11. 子 Agent 结果通过异步通知回主对话，主 Agent 可继续基于结果推进 ReAct 决策。
12. UI 上需要像工具调用、Skill 调用一样呈现 SubAgent 调用轨迹，至少包含 SubAgent 类型、角色名、输入给 SubAgent 的结构化 prompt、运行状态、结构化输出、错误与用量摘要。
13. Hook 的 agent action 升级为复用 SubAgent 基础设施，保留现有配置兼容性。

## 非功能要求

- **上下文隔离**：子 Agent 的中间消息、工具结果与权限决策不得写入主会话历史。
- **工具稳定性**：主 Agent 可见工具清单不因角色定义数量变化而变化；角色选择通过统一工具参数表达。
- **安全优先**：子 Agent 权限不得高于全局策略；角色白名单只能收窄能力，不能绕过全局禁止与沙箱。
- **后台可观测**：后台任务至少可观测任务 ID、类型、角色、状态、开始/结束时间、错误、最终文本和 token 用量。
- **UI 可观测**：每次 SubAgent 调用都应在对话 UI 中形成可展开记录，用户能看到输入结构、输出结构与最终状态；敏感内容按现有权限/脱敏策略处理。
- **降级清晰**：角色定义加载失败、缺字段、同名冲突或工具过滤后为空时，要有明确错误，不影响主流程启动。
- **兼容现有架构**：SubAgent 属于工具层；运行编排复用引擎层公开能力；权限与沙箱仍由安全层兜底。
- **测试覆盖**：覆盖定义加载、工具过滤、运行隔离、后台状态、统一工具 schema、Hook agent action 兼容与端到端工具调用。

## 设计骨架

目录结构示意：

```text
src/internal/subagent/
  definition/        # Agent Markdown 定义解析、来源扫描、覆盖合并
  runtime/           # 子 Agent 运行器、定义式/Fork 式启动上下文、状态隔离
  background/        # 后台任务管理、状态查询、异步通知
  tool/              # 统一 Agent 工具与后台查询/切后台能力
  builtin/           # 内置 Explore / Plan / General-Purpose Markdown 定义

src/internal/hook/executor/
  agent.go           # 从单轮 stub 升级为复用 SubAgent 运行器

src/internal/interaction/web/
  protocol.go        # 扩展 SubAgent 调用开始/状态/结果通知协议
  handler.go         # 注入 SubAgent 运行器、广播异步结果
  static/app.js      # 渲染 SubAgent 调用卡片、结构化 prompt 与结构化输出
  static/style.css   # SubAgent 调用展示样式

src/main.go          # 装配 Agent 定义加载器、运行器、后台管理器与工具注册
```

运行关系示意：

```text
主 Agent
  -> 统一 Agent 工具
    -> 定义式: 角色定义 + 空白消息历史 + 过滤后的工具集
    -> Fork 式: 父对话历史快照 + 父工具集快照 + 过滤后的工具集
      -> 独立 AgentLoop 跑到底
        -> 后台任务管理器记录状态/用量/结果
        -> 异步通知主对话
        -> WebUI 展示调用类型、结构化 prompt、结构化输出与状态
```

## Out of Scope

- 不做 Worktree 级文件隔离；子 Agent 与主 Agent 共享同一项目文件系统与当前沙箱边界。
- 不做多 Agent 团队编排、依赖图调度或角色间自动协作。
- 不做后台任务跨进程或跨会话持久化；本步骤后台任务仅在当前进程内有效。
- 不做子 Agent 与用户的交互式 HITL 对话；需要用户确认的场景沿用权限系统，但子 Agent 自身不直接追问用户。
- 不做插件完整生态；仅为 Agent 定义加载预留插件来源输入，并保证优先级低于内置。
- 不做远程分布式执行、资源配额集群调度或外部队列。
- 不做完整的 WebUI 后台任务管理面板；本步骤只保证 SubAgent 调用轨迹、结果通知与必要状态查询可见。
