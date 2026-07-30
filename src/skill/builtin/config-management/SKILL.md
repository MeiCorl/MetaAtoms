---
name: config-management
description: "管理 MetaAtoms 云端多用户配置(setting.json、skills、agents、memory)的总索引。用于添加、删除、修改、查看 server_port 服务端口、用户级 MCP server、Hook 钩子、SubAgent 子代理、Agent 角色定义、上下文压缩、记忆系统、Skill 系统、工具白名单、模型/API key/base_url、超时、上下文窗口、Agent 循环参数等配置；当用户提到加/配/改/删/设置/管理/开启/关闭 MCP、hook/hooks/钩子、event/action/condition、subagent/子代理/自定义 agent/agent role/角色定义、compaction/context window/压缩、memory/记忆、skill/技能、tools/tool、server_port/端口、model/模型/API key/base_url/timeout/retries、用户目录配置等场景时使用。本 Skill 只提供简介和索引；详细 JSON schema、默认值、示例、加载时机、重启要求和排错说明按需读取 reference/*.md。改写配置一律使用 ReadFile + EditFile/WriteFile，普通用户只写用户级目录；server_port 只写全局配置。"
---

# config-management — MetaAtoms 配置管理总索引

本 Skill 是 `setting.json` 的轻量入口索引：

- `SKILL.md` 只保留配置域导航、读取规则和改写原则。
- `reference/*.md` 保存各模块的 JSON schema、示例、默认值、是否需要重启和错误排查。
- 回答或改写配置前，只读取用户问题涉及的 reference 文件，避免一次加载全部配置细节。

## 加载方式

拿到本 Skill 后，先根据下方索引定位 reference 文件；如果 `use_skill` 返回了 Skill 根路径提示，复制提示里的真实路径，只替换 `reference\<file>` 文件名部分。

读取细节时使用 `ReadFile`，不要用 shell 搜索子文档；改写配置时使用 `ReadFile` + `EditFile`/`WriteFile`，不要凭空重写整份 JSON。

## 配置文件位置

| 层级 | 路径 | 适用场景 |
|------|------|---------|
| 全局 | `~/.metaatoms/setting.json` | 平台/管理员维护的默认配置、通用 MCP、通用权限规则；MetaAtoms 启动时加载，普通用户不编辑 |
| 用户级 | `~/.metaatoms/${user_id}/setting.json` | 当前登录用户的配置、用户级 MCP、权限规则和个性化参数；用户登录接入时加载 |

选择原则：普通用户请求改配置时默认写用户级；只有明确的管理员/部署维护请求才讨论全局配置。这里不存在原 CodePilot 的工区路径概念，用户工作目录固定为 `~/.metaatoms/${user_id}`。

## 模块索引

| # | 配置域 | 何时读取 | 文件 |
|---|--------|----------|------|
| 1 | 配置文件总览 | 路径、合并规则、全局与用户级选择 | `reference/overview.md` |
| 2 | MCP | 新增、修改、禁用 stdio/http MCP server 或调整握手/缓存超时 | `reference/mcp.md` |
| 3 | permissions(已移除) | 旧权限配置迁移说明；不要再写入 allow/ask/deny、HITL 或权限模式 | `reference/permissions.md` |
| 4 | compaction | 调整上下文压缩阈值、关闭压缩、排查压缩触发 | `reference/compaction.md` |
| 5 | memory | 开关自动学习记忆、调整 MEMORY.md 索引注入上限 | `reference/memory.md` |
| 6 | skill | 开关 Skill 系统、调整单个 SKILL.md 截断阈值 | `reference/skill.md` |
| 7 | tools | 设置 LLM 可见工具白名单、隐藏或禁用工具 | `reference/tools.md` |
| 8 | hook | 配置 Hook 事件、condition DSL、command/http/prompt/agent action | `reference/hook.md` |
| 9 | subagent | 开关 SubAgent、限制角色定义大小、设置后台等待/工具过滤、编写自定义 Agent 角色定义 | `reference/subagent.md` |
| 10 | 顶层 LLM / Agent 参数 | 修改 provider/model/api_key/base_url/token/timeout/context window/迭代上限 | `reference/llm-agent.md` |
| 11 | 改写工作流 | 需要实际编辑 setting.json 时读取，确认读写、验证和重启流程 | `reference/workflow.md` |
| 12 | 错误排查 | JSON 语法、字段拼写、类型错误、启动校验失败 | `reference/troubleshooting.md` |

Hook 的源码实现与调度链路属于代码功能感知，详见 `codebase-overview/reference/hook-system.md`；本 Skill 只负责 Hook 配置写法。
SubAgent 的源码实现与运行链路属于代码功能感知，详见 `codebase-overview/reference/sub-agent.md`；本 Skill 只负责 `setting.json` 的 `subagent` 段和角色定义文件写法。

## 改写原则

1. 先读取目标 `setting.json`，确认当前结构和写入层级。
2. 只修改用户要求的配置域；不顺手重排、不格式化无关字段、不删除未知字段。
3. 新增复杂 section 前，先读取对应 reference 文件中的 schema 和完整示例。
4. `permissions` 配置已移除；主要配置通常需要重启，具体以对应 reference 为准。
5. 改完后给出验证信号，例如 MCP 连接日志、Hook 状态栏、工具白名单变化或首次 LLM 请求成功。

## 维护说明

- 入口文件保持轻量，只追加索引项，不放大段 schema 或完整示例。
- 详细说明统一放在 `reference/` 一层目录下，避免深层跳转。
- 新增配置域时：添加 `reference/<module>.md` → 在模块索引追加一行 → 保持 frontmatter 的触发词覆盖该配置域。
