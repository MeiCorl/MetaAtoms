# SubAgent — MetaAtoms 实现原理

> 状态:**Step 12 实现中** | 架构层:第 3 层 工具层 | 入口:统一 `agent` 工具 + Hook agent action

## §1 定位

SubAgent 是主 Agent 可调用的特殊工具能力。主 Agent 通过稳定的 `agent` 工具把子任务交给独立子 Agent；子 Agent 使用独立 `ConversationManager`、隔离权限状态和过滤后的工具视图跑到底，最终把结构化结果返回前台或进入进程内后台任务管理器。

## §2 核心模块

- `src/subagent/definition`: 解析 Markdown + YAML frontmatter 角色定义，按 plugin -> builtin -> global -> user 顺序加载，用户级可覆盖低优先级同名定义，同级冲突为启动期错误。当前部分内部 source 名仍沿用 user/project 兼容枚举，tenant 装配会把 globalDir/userDir 映射到新语义。
- `subagent/builtin`: 内置 `explore`、`plan`、`general-purpose` 兜底角色，以及 `product-manager`、`architect`、`tech-lead`、`engineer`、`tester` 产品交付角色，并通过 `embed.FS` 进入源码运行路径。
- `src/subagent/runtime`: 提供 `Runner.RunDefined` 与 `Runner.RunFork`。defined 从空白历史启动；fork 复制父历史、父 System Prompt 稳定块与父工具快照，并强制后台语义。
- `src/subagent/background`: 进程内后台任务管理器，记录 queued/running/completed/failed/canceled 生命周期、结构化 prompt/output、最终文本、错误和 token 用量。
- `src/subagent/tool`: 注册稳定工具对 `agent` / `task_status`。角色数量变化不会改变主 Agent 工具清单。

## §3 主流程装配

`main.go` 在权限系统和路径沙箱就绪后加载 Agent 定义并创建 Runner。Web `Handler` 创建后注册 `agent` 与 `task_status` 工具，因为 fork 模式需要 Handler 在调用瞬间提供父会话历史、父工具快照和当前稳定 System Prompt。后台任务管理器的通知回调绑定到 `Handler.HandleSubAgentTaskEvent`：所有状态变化通过 WebSocket 推送 `subagent_call_start`、`subagent_status_update` 和 `subagent_result`；completed/failed/canceled 终态还会作为内部消息主动回灌主对话，由主 Agent 展示子 Agent 结果，不依赖轮询。

HookEngine 创建时也会收到同一份定义注册表与 Runner。`hook/executor/agent.go` 已从 Step 11 的单轮 LLM stub 升级为定义式 SubAgent 调用：旧配置的 `prompt`、`max_iterations`、`allow_tools`、`timeout` 继续可用；未指定角色时默认使用安全只读的 `explore`。

## §4 安全与隔离


## §5 发布资源

内置 Agent 定义同时通过 Go embed 和 dist 文件副本可用。构建脚本会把 `src/subagent/builtin/*.md` 复制到 `build/dist/subagent/builtin/`，与 `definition.DefaultLoadOptions` 的 `subagent/builtin` 路径对齐；源码运行即使没有 dist 副本也可通过 embed 加载全部内置角色。
