# Step 12 — SubAgent 任务拆解

## Task 1: Agent 定义模型与多来源加载
**状态**: 已完成

**目标**: 实现 Markdown + YAML frontmatter 的 Agent 定义解析、扫描、优先级覆盖与内置定义落盘。

**影响文件**:
- `src/internal/subagent/definition/` — 新建,定义 Agent frontmatter、解析器、扫描器、注册表与来源优先级。
- `src/internal/subagent/builtin/explore.md` — 新建,内置 Explore 定义。
- `src/internal/subagent/builtin/plan.md` — 新建,内置 Plan 定义。
- `src/internal/subagent/builtin/general-purpose.md` — 新建,内置 General-Purpose 定义。
- `src/internal/subagent/builtin/builtin.go` — 新建,内嵌内置 Agent 定义资源。
- `src/internal/skill/scanner.go` — 参考,不直接修改或仅在必要时抽取可复用思路。
- `src/internal/config/config.go` — 修改,新增 SubAgent 配置段的默认值与校验。
- `config/setting.example.json` / `config/setting.example.openai.json` — 修改,补充 SubAgent 配置示例。

**依赖**: 无

**具体内容**:
1. 定义 AgentDefinition 数据结构，覆盖 name、description、allowed-tools、denied-tools、model、max-turns、permission-mode、background 默认策略、正文系统提示与来源信息。
2. 实现 Markdown frontmatter 解析，缺少必填字段、YAML 错误、正文为空时返回带路径的错误。
3. 实现多来源扫描：插件、内置、用户、项目四类来源均可输入；加载合并结果满足项目级高于用户级、高于内置、高于插件。
4. 实现同级同名冲突检测；跨级同名按优先级覆盖。
5. 补齐 Explore、Plan、General-Purpose 三个内置 Markdown 定义；规范名使用 `general-purpose`，兼容 `gerneral-purpose` 作为别名解析但不作为内置主名。
6. 配置段支持启用开关、最大定义大小、默认后台超时、全局禁止工具、后台白名单等字段。
7. 单测覆盖合法解析、字段缺失、来源覆盖、同级冲突、内置三角色加载与别名兼容。

**参考资料**:
- `src/internal/skill/scanner.go` — `LoadAll` / `scanEmbeddedBuiltins` / `scanLevelWithOptions`
- `src/internal/skill/registry.go` — `Register` 的覆盖与冲突规则
- `src/internal/skill/builtin/builtin.go` — `embed.FS` 内置资源模式
- `src/internal/config/config.go` — `SkillConfig` / `HookConfig` 默认值与校验模式

## Task 2: 工具过滤与权限隔离基础设施
**状态**: 已完成

**目标**: 构建子 Agent 专用工具视图与权限追踪隔离，确保全局策略、角色限制、后台白名单、防嵌套限制层层生效。

**影响文件**:
- `src/internal/subagent/runtime/` — 新建,实现工具视图构建、运行配置与隔离上下文。
- `src/internal/tool/registry.go` — 修改或新增辅助方法,支持稳定快照与按名称过滤。
- `src/internal/tool/tool_spec.go` — 修改或新增辅助方法,支持从工具快照生成稳定 ToolSpec。
- `src/internal/security/` — 修改,提供权限检查器的隔离会话或克隆能力。
- `src/internal/engine/conversation/tool_handler.go` — 修改,允许子 Agent 构造独立 ToolHandler 并注入隔离权限状态。

**依赖**: Task 1

**具体内容**:
1. 定义 ToolPolicy 合并流程：父工具集快照 → 全局禁止 → 防嵌套禁止 → 角色白名单/黑名单 → 后台白名单。
2. 保证统一 Agent 工具本身不会出现在子 Agent 可用工具集中，防止子 Agent 无限嵌套。
3. 为定义式与 Fork 式都生成稳定排序的工具描述，避免同一任务中工具清单抖动影响缓存。
4. 为每个子 Agent 构造独立权限追踪状态，会话级授权和一次性路径授权不得回写主 Agent。
5. 保持沙箱、Bash 黑名单、路径越界策略仍由安全层执行，子 Agent 只做能力收窄。
6. 单测覆盖白名单、黑名单、全局禁止、防嵌套、后台白名单、权限状态不串线。

**参考资料**:
- `src/internal/tool/registry.go` — `List` / `EnabledNames`
- `src/internal/tool/tool_spec.go` — `ToSpecs`
- `src/internal/security/checker.go` — `Checker` 会话规则与模式
- `src/internal/security/interceptor.go` — `Interceptor.Check`
- `src/internal/engine/conversation/tool_handler.go` — `NewToolHandler` / `ExecuteBatch`

## Task 3: 子 Agent 运行器与定义式启动路径
**状态**: 已完成

**目标**: 实现定义式子 Agent 的非交互「跑到底」运行器，从空白对话按固定角色系统提示执行任务并产出结构化结果。

**影响文件**:
- `src/internal/subagent/runtime/runner.go` — 新建,运行器入口。
- `src/internal/subagent/runtime/result.go` — 新建,结构化结果与用量统计。
- `src/internal/subagent/runtime/trace.go` — 新建,定义 SubAgent 输入 prompt、输出与 UI 展示所需的结构化 trace。
- `src/internal/subagent/runtime/context.go` — 新建,运行时状态隔离容器。
- `src/internal/engine/conversation/manager.go` — 修改或新增导出辅助,支持安全复制消息与用量读取。
- `src/internal/engine/conversation/agent_loop.go` — 修改或复用,支持子 Agent 最大轮次与完成判定。
- `src/internal/engine/prompt/` — 修改或新增辅助,支持角色系统提示组装。

**依赖**: Task 1, Task 2

**具体内容**:
1. 定义 Runner 依赖注入结构，复用 LLM provider、工具注册表、Hook 引擎、工作目录与全局配置。
2. 定义式启动时创建新的 ConversationManager，不继承父历史，只写入本次任务用户消息。
3. 将角色正文作为子 Agent 固定系统提示，叠加必要的非交互约束和输出约束。
4. 使用角色最大轮次控制 AgentLoop；模型不再调用工具时判定完成。
5. 收集最终文本、结构化输出、停止原因、迭代次数、工具调用数、token 用量与错误信息。
6. 确保子 Agent 中间消息、工具结果和压缩状态不写入主会话。
7. 生成 UI 可展示的结构化 trace，包含 SubAgent 类型、角色名、结构化 prompt、结构化输出、状态与用量摘要。
8. 单测使用 fake provider 和 fake tool 验证定义式从空白历史启动、跑到底、完成判定、状态隔离与 trace 生成。

**参考资料**:
- `src/internal/engine/conversation/agent_loop.go` — `AgentLoop` / `AgentLoopConfig` / `AgentLoopResult`
- `src/internal/engine/conversation/manager.go` — `NewConversationManager` / `Reset` / `AllMessages` / `UpdateUsage`
- `src/internal/engine/prompt/builder.go` — System Prompt 组装模式
- `src/internal/memory/context/compactor.go` — 运行期压缩依赖注入模式

## Task 4: Fork 式启动路径与 prompt cache 友好历史继承
**状态**: 已完成

**目标**: 实现 Fork 式子 Agent，使其继承父对话历史与父工具集快照，强制后台运行并尽量命中首次请求缓存。

**影响文件**:
- `src/internal/subagent/runtime/fork.go` — 新建,Fork 输入快照与启动逻辑。
- `src/internal/engine/conversation/manager.go` — 修改,提供父历史安全快照能力。
- `src/internal/engine/prompt/` — 修改或新增辅助,复用父 System Prompt 稳定块。
- `src/internal/interaction/web/handler.go` — 修改,在调用统一 Agent 工具前提供父会话快照。

**依赖**: Task 2, Task 3

**具体内容**:
1. Fork 式从父会话获取只读消息快照，复制到子 Agent 的独立 ConversationManager。
2. 继承父工具集快照，再叠加子 Agent 工具过滤策略；不得在运行中因后续 MCP 工具注册改变本次工具集。
3. 继承父 System Prompt 的稳定部分，使首次 LLM 请求尽量与父对话共享缓存前缀。
4. Fork 输入在父历史尾部追加本次子任务指令，不修改父历史。
5. Fork 式无论工具参数如何配置都强制后台运行。
6. 单测覆盖父历史复制、不回写父历史、工具快照稳定、Fork 强制后台与首轮请求上下文顺序。

**参考资料**:
- `src/internal/engine/conversation/manager.go` — `GetContext` / `AllMessages`
- `src/internal/engine/prompt/sources/agents_md.go` — 稳定 prompt source 模式
- `src/llm/types.go` — `SystemPrompt` / `SystemBlock`

## Task 5: 后台任务管理器与异步结果通知
**状态**: 已完成

**目标**: 实现进程内后台任务管理，追踪子 Agent 状态、结构化输入输出、用量，并支持显式后台、超时自动转后台、手动切后台。

**影响文件**:
- `src/internal/subagent/background/manager.go` — 新建,后台任务管理器。
- `src/internal/subagent/background/task.go` — 新建,任务状态、结果、用量模型。
- `src/internal/subagent/tool/` — 新建或修改,提供后台查询与手动切后台能力。
- `src/internal/interaction/web/protocol.go` — 修改,新增子 Agent 调用开始、状态更新与结果通知消息。
- `src/internal/interaction/web/websocket.go` / `handler.go` — 修改,广播后台状态、结构化 prompt 与结构化结果。

**依赖**: Task 3, Task 4

**具体内容**:
1. 定义后台任务生命周期：queued、running、completed、failed、canceled。
2. 任务管理器记录任务 ID、子 Agent 类型、角色名、创建时间、开始/结束时间、状态、结构化 prompt、结构化输出、最终文本、错误、迭代次数、工具调用数与 token 用量。
3. 显式后台：统一 Agent 工具参数指定后台时立即返回任务 ID，任务异步执行。
4. 超时自动转后台：前台等待超过阈值时返回任务 ID，任务继续运行。
5. 手动切后台：主会话可将仍在前台等待的子 Agent 切入后台。
6. Fork 式调用强制进入后台。
7. 任务开始、状态变化、完成后通过 WebSocket 或主会话回调异步通知；通知包含 UI 展示所需的结构化字段，但不污染主 Agent 中间上下文。
8. 单测覆盖状态流转、结构化输入输出保存、超时转后台、取消、并发任务与异步通知回调。

**参考资料**:
- `src/internal/hook/engine.go` — async goroutine 与 Shutdown 模式
- `src/internal/interaction/web/protocol.go` — WebSocket 消息类型定义
- `src/internal/interaction/web/handler.go` — AgentLoop 回调与事件推送模式

## Task 6: 统一 Agent 工具实现
**状态**: 已完成

**目标**: 把 SubAgent 暴露为主 Agent 可调用的稳定工具，用类型参数分流定义式与 Fork 式，并提供后台任务查询能力。

**影响文件**:
- `src/internal/subagent/tool/agent_tool.go` — 新建,统一 Agent 工具。
- `src/internal/subagent/tool/task_status_tool.go` — 新建,后台任务状态查询工具。
- `src/internal/tool/builtin/register.go` — 修改,注册 SubAgent 工具。
- `src/internal/tool/tool.go` — 参考,不改变 Tool 接口契约。
- `src/internal/config/config.go` — 修改,把 SubAgent 工具纳入配置校验与默认行为。

**依赖**: Task 1, Task 2, Task 5

**具体内容**:
1. 工具 schema 固定包含类型、角色、任务输入、结构化 prompt 元数据、是否后台、前台等待时间、模型覆盖等字段；角色数量变化不得改变工具列表。
2. 类型为 defined 时按角色定义启动；类型为 fork 时继承父上下文并强制后台。
3. 工具返回值在前台完成时返回最终结果与结构化输出；进入后台时返回任务 ID、状态与查询方式。
4. 查询工具按任务 ID 返回当前状态、结构化 prompt、结构化输出、结果与用量。
5. 统一处理角色不存在、工具过滤后为空、模型不支持、超时、取消、运行失败等错误。
6. 确保 SubAgent 工具自身不出现在子 Agent 工具集中。
7. 单测覆盖 schema 稳定、defined/fork 分流、后台返回、前台完成、结构化 trace、错误返回与防嵌套。

**参考资料**:
- `src/internal/tool/tool.go` — `Tool` 接口契约
- `src/internal/tool/builtin/register.go` — 内置工具注册模式
- `src/internal/tool/builtin/read_file.go` / `grep.go` / `glob.go` — 工具 schema 与 Execute 写法

## Task 7: Hook agent action 升级与主流程装配
**状态**: 已完成

**目标**: 把 SubAgent 运行器接入 main、ToolHandler、Web Handler 与 Hook agent action，完成主流程可用链路。

**影响文件**:
- `src/main.go` — 修改,装配 Agent 定义加载、后台管理器、SubAgent 运行器与工具注册。
- `src/internal/interaction/web/handler.go` — 修改,持有并提供父会话快照、异步通知回调与手动切后台入口。
- `src/internal/hook/executor/agent.go` — 修改,从单轮 LLM stub 升级为 SubAgent 调用。
- `src/internal/hook/engine.go` — 修改,向 agent executor 注入 SubAgent 运行器或兼容适配器。
- `src/internal/skill/builtin/codebase-overview/reference/sub-agent.md` — 修改,把 STUB 更新为已实现导览。
- `src/internal/engine/prompt/sources/codebase_awareness.go` — 如需,更新自感知摘要。
- `build/build.ps1` / `Makefile` — 修改,确保内置 Agent 定义资源进入发布产物。

**依赖**: Task 1, Task 5, Task 6

**具体内容**:
1. 启动期加载 Agent 定义注册表并记录加载问题；非致命问题只告警，致命冲突按配置校验失败处理。
2. 将 SubAgent 运行器注入统一 Agent 工具、后台任务管理器、Web Handler 与 Hook Engine。
3. Hook agent action 保留现有 setting.json schema：旧配置未指定角色时默认走一个安全的定义式角色。
4. Hook agent action 的 allow_tools 与 max_iterations 字段映射到 SubAgent 角色限制与运行上限。
5. Web Handler 提供父会话历史、父工具快照、父 System Prompt 稳定块和结果通知回调。
6. 更新 codebase-overview 的 SubAgent 文档，从 stub 改为实现导览。
7. 构建脚本复制或嵌入内置 Agent 定义，dist 模式与源码模式均能加载。
8. 集成测试覆盖 main 装配、Hook agent action 兼容、内置定义发布路径。

**参考资料**:
- `src/main.go` — Skill、Hook、ToolHandler、Web Handler 装配区
- `src/internal/hook/executor/agent.go` — 当前 agent action stub 与 TODO
- `src/internal/hook/engine.go` — `EngineConfig` 依赖注入
- `src/internal/skill/builtin/codebase-overview/reference/sub-agent.md` — 当前 SubAgent stub

## Task 8: 接入主流程
**状态**: 已完成

**目标**: 完成 SubAgent 从配置、工具清单、权限、Hook、WebSocket 通知到主 Agent ReAct 循环的全链路接入。

**影响文件**:
- `src/main.go` — 修改,最终串联所有 SubAgent 组件。
- `src/internal/engine/conversation/tool_handler.go` — 修改,必要时支持手动切后台的执行上下文。
- `src/internal/interaction/web/protocol.go` — 修改,补齐协议字段与前端兼容。
- `src/internal/interaction/web/static/app.js` / `style.css` — 修改,像工具/SKILL 调用一样展示 SubAgent 调用卡片、类型、结构化 prompt、状态与结构化输出。
- `README.md` — 修改,补充 SubAgent 使用说明。

**依赖**: Task 1, Task 2, Task 3, Task 4, Task 5, Task 6, Task 7

**具体内容**:
1. 确认统一 Agent 工具出现在主 Agent 工具清单，且工具 schema 不随角色数量变化。
2. 确认 defined 调用可在前台完成并把最终结果回传给主 Agent。
3. 确认 fork 调用必定后台执行，并在完成后异步通知。
4. 确认每次 SubAgent 调用在 UI 中形成可展开记录，展示 SubAgent 类型、角色名、结构化 prompt、运行状态、结构化输出、错误与用量摘要。
5. 确认全局工具配置、权限模式、沙箱、Hook 与压缩系统在子 Agent 运行中均按预期生效。
6. 如前端已有通用工具调用展示能力则复用；否则补充 SubAgent 专用的最小展示组件。
7. 更新 README 的功能说明、角色定义路径、内置角色、UI 展示形态与限制边界。

**参考资料**:
- `src/main.go` — 启动装配顺序
- `src/internal/interaction/web/handler.go` — RunAgentLoop 主流程
- `src/internal/interaction/web/static/app.js` — WebSocket 消息处理
- `README.md` — 用户可见功能说明

## Task 9: 端到端验证
**状态**: 已完成

**目标**: 用自动化测试和手动冒烟验证 Step 12 的关键能力全部可观测通过，并同步项目进度文档。

**影响文件**:
- `docs/step12-SubAgent/checklist.md` — 修改,填写实际验证结果与结论。
- `docs/step12-SubAgent/tasks.md` — 修改,所有 Task 标记为已完成。
- `.harness/PROGRESS.md` — 修改,同步 Step 12 完成状态。
- 相关测试文件 — 新增或修改,覆盖端到端路径。

**依赖**: Task 8

**具体内容**:
1. 运行 Go 单元测试，覆盖 subagent、tool、security、hook、web 相关包。
2. 运行至少一个端到端测试：主 Agent 调用 Explore 读取项目结构并后台返回结果。
3. 验证 Plan 不具备写入/执行能力，General-Purpose 具备完整权限但仍受全局策略限制。
4. 验证 Fork 继承父历史且强制后台，完成后异步通知主对话。
5. 验证 UI 展示 SubAgent 类型、输入结构化 prompt、运行状态与结构化输出。
6. 验证 Hook agent action 仍兼容旧配置并复用 SubAgent 运行器。
7. 填写 checklist 的实际结果与通过/不通过结论。
8. 更新 `.harness/PROGRESS.md`：总览、已完成步骤、待完成步骤与架构层覆盖度。

**参考资料**:
- `docs/step12-SubAgent/spec.md`
- `docs/step12-SubAgent/checklist.md`
- `.harness/PROGRESS.md`
- `go test ./...`





