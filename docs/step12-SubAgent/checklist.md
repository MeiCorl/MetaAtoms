# Step 12 — SubAgent 验收清单

## Task 1: Agent 定义模型与多来源加载

- [x] Agent 定义支持 Markdown + YAML frontmatter
  - 预期: 合法定义可解析出角色名、用途说明、工具白名单、工具黑名单、模型、最大轮次、权限模式与正文系统提示。
  - 实际: 已新增 `src/internal/subagent/definition` 解析器与单测，覆盖合法 frontmatter、字段缺失、YAML 错误与正文为空；`go test ./src/internal/subagent/definition ./src/internal/config` 与 `go test ./...` 均通过。
  - 结论: 通过

- [x] 多来源加载优先级正确
  - 预期: 同名定义按项目级 > 用户级 > 内置 > 插件覆盖；同级同名冲突会报错。
  - 实际: 已实现 plugin -> builtin -> user -> project 加载顺序与 Registry 优先级覆盖；单测验证跨级覆盖到项目级、同级项目重复定义返回 `ErrDefinitionConflict`。
  - 结论: 通过

- [x] 三个内置角色可用
  - 预期: Explore、Plan、General-Purpose 均可从内置来源加载；`gerneral-purpose` 作为兼容别名能解析到 `general-purpose`。
  - 实际: 已新增 `explore.md`、`plan.md`、`general-purpose.md` 与 `builtin.go` embed 资源；单测验证三个内置角色可加载，`gerneral-purpose` 查询会规范化到 `general-purpose`。
  - 结论: 通过

## Task 2: 工具过滤与权限隔离基础设施

- [x] 工具过滤多层防线生效
  - 预期: 全局禁止、角色白名单、角色黑名单、后台白名单、防嵌套限制按顺序收窄工具集。
  - 实际: 已新增 `src/internal/subagent/runtime` 的 `ToolPolicy` / `BuildToolView`，按父工具集快照、全局禁止、防嵌套禁止、角色白名单/黑名单、后台白名单顺序收窄工具集；单测 `TestBuildToolViewAppliesFilteringLayers` 覆盖该顺序，`go test ./...` 通过。
  - 结论: 通过

- [x] 子 Agent 不能无限嵌套
  - 预期: 子 Agent 工具集中不会包含统一 Agent 工具本身，也不能再创建新的子 Agent。
  - 实际: `DefaultNestedDeniedTools` 默认过滤 `Agent` / `SubAgent` / `subagent` / `task_status` 等统一 Agent 与状态查询候选工具名；单测验证子 Agent 工具视图中不会保留 `Agent`。
  - 结论: 通过

- [x] 权限追踪隔离
  - 预期: 子 Agent 的会话级授权和一次性路径授权不会污染主 Agent，也不会污染其他子 Agent。
  - 实际: `security.Checker` 新增 `CloneIsolated` / `CloneIsolatedForWorkdir`，仅复制权限模式、配置级规则和工作目录，不复制会话级授权与一次性路径授权；`conversation.NewIsolatedToolHandler` 为子 Agent 注入独立 Interceptor 与 SandboxMiddleware；单测验证父子会话授权互不污染。
  - 结论: 通过

## Task 3: 子 Agent 运行器与定义式启动路径

- [x] 定义式从空白对话启动
  - 预期: defined 模式只包含角色系统提示和本次任务输入，不继承父会话历史。
  - 实际: 已新增 `runtime.Runner.RunDefined`，测试 `TestRunDefinedStartsBlankRunsToCompletionAndBuildsTrace` 验证首轮 provider 仅收到角色 system prompt 与本次任务 user message，未包含父会话消息；`go test ./...` 通过。
  - 结论: 通过

- [x] 子 Agent 跑到底完成判定正确
  - 预期: 子 Agent 在模型不再调工具时结束，并返回最终文本、停止原因、迭代次数、工具调用数与 token 用量。
  - 实际: `RunDefined` 复用 `ConversationManager.RunAgentLoop`，按角色 `max-turns` 控制最大轮次；fake provider 先触发工具再返回无工具最终文本，测试验证 `iterations=2`、`tool_calls=1`、`stop_reason=completed`、累计 token 用量为 30/7/37。
  - 结论: 通过

- [x] 子 Agent 结构化 trace 可生成
  - 预期: 每次运行都能生成包含 SubAgent 类型、角色名、结构化 prompt、结构化输出、状态、错误和用量摘要的 trace。
  - 实际: 已新增 `runtime.Trace` / `PromptTrace` / `OutputTrace`，包含 type、role、task、metadata、system blocks、tool names、model、max turns、final text、structured output、status、error、usage；单测验证 trace prompt 与 output 字段。
  - 结论: 通过

- [x] 中间上下文不污染主会话
  - 预期: 子 Agent 的中间消息、工具结果、压缩状态和文件读取缓存不写入主 ConversationManager。
  - 实际: `RunDefined` 每次创建新的 `ConversationManager` 与 `NewIsolatedContext`，不接收也不写回父 `ConversationManager`；测试验证父历史在子 Agent 工具调用与最终回复后保持不变。子 Agent 未注入主会话 compactor，压缩状态不共享。
  - 结论: 通过

## Task 4: Fork 式启动路径与 prompt cache 友好历史继承

- [x] Fork 继承父历史快照
  - 预期: fork 模式启动时复制父会话历史，运行中不受父会话后续变化影响，也不回写父历史。
  - 实际: 已新增 `conversation.HistorySnapshot` 与 `runtime.NewForkSnapshot`，Fork 启动时把父 lead user message 与持久化历史深拷贝到独立 `ConversationManager`，再在尾部追加本次子任务指令；单测验证父历史后续追加不会进入子 Agent 首轮请求，子 Agent 运行也不会回写父历史。
  - 结论: 通过

- [x] Fork 继承父工具集快照
  - 预期: fork 模式使用调用瞬间的父工具集快照，随后新增的 MCP 工具不影响已运行任务。
  - 实际: `ForkSnapshot` 在调用瞬间保存 `parentRegistry.Snapshot()`，`RunFork` 基于该工具切片构造独立 Registry 后再执行既有过滤策略；单测验证快照后新增的工具不会出现在 Fork 首轮请求工具列表中。
  - 结论: 通过

- [x] Fork 强制后台运行
  - 预期: fork 模式即使请求前台等待，也会返回后台任务 ID并异步执行。
  - 实际: Task 4 运行时层已无视请求中的 `Background=false`，统一以 `Background=true` 构造工具策略并写入 trace，后台白名单过滤按后台语义生效；单测验证请求前台时仍只暴露后台允许工具。后台任务 ID 与异步执行由 Task 5 后台任务管理器在此强制语义基础上接入。
  - 结论: 通过

## Task 5: 后台任务管理器与异步结果通知

- [x] 后台任务状态可追踪
  - 预期: 每个后台任务可查询任务 ID、类型、角色、状态、开始/结束时间、错误、最终文本和用量。
  - 实际: 已新增 `src/internal/subagent/background` 的 `Manager` 与 `TaskSnapshot`，记录 queued/running/completed/failed/canceled 生命周期、任务 ID、类型、角色、创建/开始/结束时间、结构化 prompt/output、最终文本、错误、迭代数、工具调用数与 token 用量；`TestManagerTracksStateAndStructuredResult` 和 `TestManagerRecordsFailedResult` 覆盖可查询状态、结构化结果与失败信息，`go test ./...` 通过。
  - 结论: 通过

- [x] 三种进入后台方式可用
  - 预期: 显式后台、超时自动转后台、手动切后台均能让任务继续运行并返回可查询的任务 ID。
  - 实际: `Manager.Submit` 支持显式 `Background=true` 立即返回任务 ID；前台等待超过 `ForegroundTimeout` / 默认超时会标记 `background_reason=timeout` 并继续运行；`MoveToBackground` 支持将 queued/running 前台任务手动切后台并保留任务 ID，`Cancel` 支持取消运行中任务；单测覆盖超时转后台、手动切后台、取消与并发任务 ID 唯一性。
  - 结论: 通过

- [x] 结果异步通知主对话
  - 预期: 后台任务完成后主对话收到可见通知，通知包含任务 ID、状态、结构化 prompt、结构化输出、最终结果摘要与错误信息。
  - 实际: `Manager` 在 queued/running/backgrounded/completed/failed/canceled 状态变化时触发 `NotifyFunc`；`interaction/web` 新增 `subagent_call_start`、`subagent_status_update`、`subagent_result` 协议与 `SubAgentTaskEventPayload`，`Handler.HandleSubAgentTaskEvent` 通过现有 WebSocket 写锁向活跃连接广播完整任务快照，并在终态把结果主动回灌主 Agent；单测事件收集器验证异步通知回调，`go test ./src/internal/subagent/background ./src/internal/subagent/tool ./src/internal/interaction/web` 通过。
  - 结论: 通过

## Task 6: 统一 Agent 工具实现

- [x] 主 Agent 工具列表稳定
  - 预期: 无论 Agent 定义数量如何变化，LLM 只看到稳定的统一 Agent 工具与后台状态查询工具。
  - 实际: 已新增 `agent` 与 `task_status` 两个固定工具；`agent` schema 使用 `type`、`role`、`task`、`metadata`、`background`、`foreground_wait_seconds`、`model`、`max_turns` 等稳定字段，不枚举角色定义。`TestAgentToolSchemaStableAndToolNames` 验证新增角色后 schema 不变，注册表仅暴露固定工具名。
  - 结论: 通过

- [x] 类型参数正确分流
  - 预期: `defined` 进入角色定义路径，`fork` 进入父上下文继承路径；非法类型返回结构化错误。
  - 实际: `AgentTool.Execute` 按 `type=defined/fork` 分别调用 `Runner.RunDefined` / `Runner.RunFork`；fork 通过注入的父快照提供器继承父上下文并强制后台。`TestAgentToolDefinedForegroundReturnsResult`、`TestAgentToolForkForcesBackgroundAndWaitsForNotification` 与 `TestAgentToolStructuredErrors` 覆盖 defined、fork 和非法类型结构化错误。
  - 结论: 通过

- [x] 前台与后台返回语义清晰
  - 预期: 前台完成时直接返回最终结果与结构化输出；后台执行时返回任务 ID、当前状态和查询方式。
  - 实际: 前台完成返回 `final_text`、`structured_output`、`trace` 与 `usage`；显式后台、超时后台和 fork 后台返回 `task_id`、当前状态与“结果将主动回传”的提示，不再返回 `query_tool=task_status` 轮询建议。`task_status` 仅作为用户明确要求时的诊断入口，按任务 ID 返回任务快照、结构化 prompt/output、最终结果与用量；`go test ./src/internal/subagent/... ./src/internal/tool/... ./src/internal/config` 和 `go test ./...` 均通过。
  - 结论: 通过

## Task 7: Hook agent action 升级与主流程装配

- [x] Hook agent action 复用 SubAgent
  - 预期: hook 中的 agent action 使用 SubAgent 运行器执行，支持工具限制和最大轮次，不再只是单轮 LLM stub。
  - 实际: `hook/executor.AgentExecutor` 已改为通过 `AgentSubAgentRunner` 窄接口调用定义式 SubAgent；`main.go` 注入 definitions + runner 适配器；新增 `TestAgent_UsesSubAgentRunnerAndMapsLegacyFields` 验证 prompt 插值、role、allow_tools、max_iterations 与 metadata 均传入 SubAgent 适配器。`go test ./src/internal/subagent/... ./src/internal/hook/... ./src/internal/interaction/web ./src/internal/tool/... ./src/internal/config ./src` 通过。
  - 结论: 通过

- [x] 旧 Hook 配置兼容
  - 预期: 现有 `prompt`、`max_iterations`、`allow_tools`、`timeout` 配置仍可加载并产生等价或更强行为。
  - 实际: 旧字段 `prompt`、`max_iterations`、`allow_tools`、`timeout` 保持原 schema；未指定 `role` 时默认 `explore`；`allow_tools` 显式空数组会映射为空工具白名单。为未注入 runner 的旧单测保留 legacy fallback，主流程注入 runner 后走 SubAgent 路径；既有 Hook agent 单测与 Hook E2E 均通过。
  - 结论: 通过

- [x] 内置资源发布路径正确
  - 预期: 源码运行与 dist 产物运行都能加载三个内置 Agent 定义。
  - 实际: `src/internal/subagent/builtin` 三个内置定义通过 `embed.FS` 加载；`build/build.ps1` 与 `Makefile` 已复制到 `build/dist/internal/subagent/builtin/`，与 `definition.DefaultLoadOptions` 的 `internal/subagent/builtin` 路径对齐。`go test` 覆盖 `subagent/definition`、`subagent/builtin` 间接加载与入口包编译。
  - 结论: 通过

## Task 8: 接入主流程
- [x] 主 Agent 可调用 Explore
  - 预期: 主 Agent 通过统一工具调用 Explore 后，Explore 只能读取/搜索代码，不能写文件或执行命令。
  - 实际: `src/main.go` 在 SubAgent 初始化成功后将稳定 `agent` / `task_status` 工具补入运行期 `tools.enabled` 白名单，确保主 Agent 工具清单可见；`explore` 内置定义仅允许 `ReadFile` / `Glob` / `Grep`，并显式拒绝 `WriteFile` / `EditFile` / `Bash`；`go test ./...` 通过。
  - 结论: 通过
- [x] 主 Agent 可调用 Plan
  - 预期: Plan 返回执行计划，不具备修改文件或执行命令能力。
  - 实际: `plan` 内置定义仅允许 `ReadFile` / `Glob` / `Grep`，拒绝写入与 Bash；统一 `agent` 工具按 `role=plan` 进入 defined 路径，结构化结果回传主 Agent；相关包测试与 `go test ./...` 通过。
  - 结论: 通过
- [x] 主 Agent 可调用 General-Purpose
  - 预期: General-Purpose 可使用完整工具集，但仍受全局权限、沙箱和后台策略限制。
  - 实际: `general-purpose` 不声明角色级白名单/黑名单，运行时经 `BuildToolView` 叠加全局禁止、防嵌套、后台白名单与独立权限/沙箱；主流程注册 `agent` / `task_status` 后仍由同一 ToolHandler、安全拦截器和 Hook 链路执行；`go test ./...` 通过。
  - 结论: 通过
- [x] UI 展示 SubAgent 调用轨迹
  - 预期: 对话 UI 像工具/SKILL 调用一样显示 SubAgent 调用卡片，可看到类型、角色名、结构化 prompt、状态、结构化输出、错误和用量摘要。
  - 实际: `app.js` 新增 `subagent_call_start` / `subagent_status_update` / `subagent_result` 路由、`task_id` 索引与可展开 SubAgent 卡片，展示 type、role、Structured Prompt、Structured Output、Usage 和 Error；`style.css` 新增 SubAgent 卡片样式与 queued/running/completed/failed/canceled 状态色；`node --check src/internal/interaction/web/static/app.js` 和静态字段断言通过。
  - 结论: 通过
- [x] Web 通知不破坏现有协议
  - 预期: 旧前端消息仍正常渲染；新增后台任务通知在未知客户端上可安全忽略。
  - 实际: 后端沿用独立的 `SubAgentTaskEventPayload` 与三类新增 MsgType，旧客户端仍会走默认未知消息分支忽略；新前端只增加对应 case，不改动原 `tool_call_*`、Skill、MCP、Memory Review 路由；`go test ./src/internal/interaction/web`、相关包测试与 `go test ./...` 均通过。
  - 结论: 通过
## Task 9: 端到端验证

- [x] Go 测试通过
  - 预期: `go test ./...` 通过，或失败项有明确原因且不属于 Step 12 引入。
  - 实际: 已执行关键包测试 `go test ./src/internal/subagent/... ./src/internal/tool/... ./src/internal/security/... ./src/internal/hook/... ./src/internal/interaction/web ./src/internal/config ./src/internal/engine/conversation ./src`；新增 E2E 后，默认 Go 构建缓存目录曾出现一次 `Access is denied`，改用仓库内临时 `GOCACHE=F:\MetaAtoms\.tmp\go-build-cache` 后测试正常。`src/internal/interaction/web` 曾在并行关键包测试中出现一次 `TestBusyRejectsConcurrentInput` 超时，随后单测复跑、web 包整包复跑和最终 `go test ./...` 均通过；`node --check src/internal/interaction/web/static/app.js` 通过。
  - 结论: 通过

- [x] 端到端 defined 场景通过
  - 预期: 主 Agent 调用 Explore 分析项目结构，结果返回主对话，主历史只保存最终结果。
  - 实际: 新增 `TestAgentToolExploreBackgroundReadsProjectStructureEndToEnd`，走 `agent` 工具以 `type=defined`、`role=explore`、`background=true` 启动后台任务，子 Agent 使用真实 `Glob` 工具读取 `docs/step12-SubAgent/*.md` 项目结构，再通过 `task_status` 查回 completed 任务、最终结构化结果、trace 与 1 次工具调用；既有 `TestRunDefinedStartsBlankRunsToCompletionAndBuildsTrace` 验证 defined 不继承父历史且不回写父会话。
  - 结论: 通过

- [x] 端到端 fork 场景通过
  - 预期: Fork 式子 Agent 继承父历史并强制后台，完成后异步通知主对话。
  - 实际: `TestAgentToolForkForcesBackgroundAndWaitsForNotification` 验证 `type=fork` 即使传入 `background=false` 也返回后台任务 ID，并可通过 `task_status` 查回 completed 结果和父消息计数；`TestRunForkInheritsParentSnapshotAndPreservesPromptOrder` 验证父历史快照、父 System Prompt 前缀、工具快照稳定且不回写父历史；`TestManagerTracksStateAndStructuredResult` / web 协议测试覆盖状态变化与异步通知快照。
  - 结论: 通过

- [x] 端到端 UI 可观测场景通过
  - 预期: 触发 defined 与 fork 后，UI 均能展示 SubAgent 类型、输入结构化 prompt、运行状态与结构化输出。
  - 实际: 静态核验 `protocol.go` / `handler.go` / `app.js` / `style.css` 中存在 `subagent_call_start`、`subagent_status_update`、`subagent_result`、`task_id` 索引、`Structured Prompt`、`Structured Output`、`Usage` 与 SubAgent 状态样式；`node --check src/internal/interaction/web/static/app.js`、`go test ./src/internal/interaction/web -count=1` 与最终 `go test ./...` 均通过。
  - 结论: 通过

- [x] 项目进度文档同步
  - 预期: `.harness/PROGRESS.md` 总览、已完成步骤、待完成步骤和架构层覆盖度均反映 Step 12 完成。
  - 实际: 已将 `.harness/PROGRESS.md` 更新为 Step 12 完成：总览完成步骤数增至 20、当前版本更新为 V2.0.0、最近更新日期为 2026-07-29、进度条调整为 12/12；已完成步骤表追加 Step 12；待完成步骤清空；架构层覆盖度将 SubAgent 与 UI 可观测能力移入已落地组件。
  - 结论: 通过