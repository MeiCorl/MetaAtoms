## §9 subagent — SubAgent 系统配置与角色定义

### 路径说明

`subagent` 段控制 Step 12 SubAgent 系统的总开关、角色定义文件大小上限、前台等待转后台的默认超时，以及子 Agent 工具视图的全局过滤策略。配置可放全局或用户级 `setting.json`。

自定义 Agent 角色**不写在** `setting.json` 中，而是写成 Markdown 定义文件：

| 层级 | 路径 | 优先级 | 适用场景 |
|------|------|--------|----------|
| 用户级 | `~/.metaatoms/${user_id}/agents/*.md` | 最高 | 当前登录用户的自定义角色 |
| 全局 | `~/.metaatoms/agents/*.md` | 中 | 平台/管理员维护的通用角色 |
| 内置 | `<exec>/subagent/builtin/*.md` 与内嵌资源 | 最低 | `explore` / `plan` / `general-purpose` 兜底角色，以及 `product-manager` / `architect` / `tech-lead` / `engineer` / `tester` 产品交付角色 |

同名角色按优先级覆盖：用户级覆盖全局，全局覆盖内置。新增或修改角色定义后，需要该用户重新接入或重启 MetaAtoms 才会重新加载。

### JSON schema 摘要

```jsonc
{
  "subagent": {
    "enabled": true,
    "max_definition_size_bytes": 65536,
    "default_background_timeout_seconds": 300,
    "global_denied_tools": ["agent", "task_status"],
    "background_allowed_tools": ["ReadFile", "Glob", "Grep"]
  }
}
```

### 完整示例

```json
{
  "subagent": {
    "enabled": true,
    "max_definition_size_bytes": 65536,
    "default_background_timeout_seconds": 300,
    "global_denied_tools": ["agent", "task_status"],
    "background_allowed_tools": ["ReadFile", "Glob", "Grep", "Bash"]
  }
}
```

### 字段默认值与单位

| 字段 | 默认 | 单位 | 说明 |
|------|------|------|------|
| `enabled` | true | - | `false` 时跳过 Agent 定义加载、SubAgent Runner 装配、`Agent` / `task_status` 工具注册和 Hook agent action 升级 |
| `max_definition_size_bytes` | 65536 (64 KB) | 字节 | 单个 Agent Markdown 定义文件大小上限，超过后启动期记录加载问题并跳过该定义 |
| `default_background_timeout_seconds` | 300 | 秒 | 前台 SubAgent 等待上限；超过后自动进入后台并返回任务 ID，终态会主动通知主 Agent |
| `global_denied_tools` | `["agent", "task_status"]` | 工具名列表 | 所有子 Agent 都要移除的工具；只能收窄能力，不能绕过全局权限、沙箱和 Bash 黑名单 |
| `background_allowed_tools` | `[]` | 工具名列表 | 后台运行时额外允许保留的工具白名单；空数组表示不额外收窄，非空时后台工具视图只保留这些工具 |

工具名大小写敏感，应使用实际 `Tool.Name()`。统一 SubAgent 工具名是 `Agent`，状态查询工具名是 `task_status`。为了防止子 Agent 嵌套调子 Agent，建议把两者都放进 `global_denied_tools`；旧配置只写 `agent` 时也能继续工作，但不如显式列全清楚。

### 角色定义格式

每个角色定义是一个 `.md` 文件，文件头必须是 YAML frontmatter，正文就是该角色的 system prompt：

```md
---
name: code-reviewer
description: 只读代码审查角色，用于发现缺陷、风险和测试缺口。
allowed-tools:
  - ReadFile
  - Glob
  - Grep
denied-tools:
  - WriteFile
  - EditFile
  - Bash
max-turns: 8
background:
  default: false
  timeout-seconds: 120
---
你是一个专注代码审查的 SubAgent。
优先指出真实 bug、行为回归、边界条件和缺失测试。
输出要包含文件路径、原因和建议修复方向。
```

| frontmatter 字段 | 必填 | 说明 |
|------------------|------|------|
| `name` | 是 | 角色名；会规范化为小写，旧拼写 `gerneral-purpose` 会归一为 `general-purpose` |
| `description` | 是 | 给主 Agent 选择角色时看的简短说明 |
| `allowed-tools` | 否 | 角色可见工具白名单；省略表示不按角色额外收窄 |
| `denied-tools` | 否 | 角色拒绝工具列表，会在白名单之后继续移除 |
| `model` | 否 | 预留的模型覆盖字段；也可在 `Agent` 工具调用时传 `model` 覆盖 |
| `max-turns` | 否 | 该角色默认最大子 Agent 迭代轮数；工具调用里的 `max_turns` 可临时覆盖 |
| `background.default` | 否 | `true` 时该角色默认后台运行 |
| `background.timeout-seconds` | 否 | 角色级前台等待秒数说明；主工具仍可用 `foreground_wait_seconds` 临时覆盖 |

### 内置角色

| 角色 | 典型用途 | 默认工具倾向 |
|------|----------|--------------|
| `explore` | 只读探索代码结构、调用链、事实依据 | `ReadFile` / `Glob` / `Grep` |
| `plan` | 整理需求、约束和执行计划，不直接实现 | `ReadFile` / `Glob` / `Grep` |
| `general-purpose` | 通用子任务，工具视图更完整 | 仍受全局权限、沙箱、后台白名单和防嵌套规则约束 |
| `product-manager` | 开发类需求分析、一次性澄清卡片、生成 `requirements.md` | `ReadFile` / `WriteFile` / `EditFile` / `Glob` / `Grep` |
| `architect` | 根据需求生成 `architecture.md`，包含 Mermaid 架构图和时序图 | `ReadFile` / `WriteFile` / `EditFile` / `Glob` / `Grep` |
| `tech-lead` | 根据需求和架构生成可断点续做的 `tasks.md` | `ReadFile` / `WriteFile` / `EditFile` / `Glob` / `Grep` |
| `engineer` | 按 `tasks.md` 的单个任务独立编码实现 | 完整工具视图，仍受全局权限、沙箱和防嵌套规则约束 |
| `tester` | 按测试类型生成 `docs/test_plan/*_test_plan.md`，并在执行阶段更新对应计划文件内 case 状态 | 完整工具视图，仍受全局权限、沙箱和防嵌套规则约束 |

### 与 tools / safety 的关系

- `tools.enabled` 控制主 Agent 启动时注册后可见的工具白名单。使用 SubAgent 时应确保主 Agent 能看到 `Agent`；`main.go` 会在 SubAgent 初始化成功后运行期补齐 `Agent` / `task_status`。
- 子 Agent 的工具视图按顺序收窄：父工具快照 → `global_denied_tools` → 防嵌套工具过滤 → 角色 `allowed-tools` → 角色 `denied-tools` → 后台 `background_allowed_tools`。
- `subagent` 只能减少子 Agent 能力，不能放大权限。最终写入、执行命令、路径越界等仍由 permissions、沙箱和 Bash 黑名单兜底。

### 是否需要重启

**需要重启或用户重新接入**。全局 `subagent` 配置在启动期加载;用户级配置和 `~/.metaatoms/${user_id}/agents/*.md` 在用户登录接入时加载。

### 错误排查

- 自定义角色不可用 → 确认文件在 `~/.metaatoms/${user_id}/agents/*.md` 或 `~/.metaatoms/agents/*.md`，扩展名是 `.md`，并且已重启或让用户重新接入。
- 启动日志提示 `missing frontmatter` → 文件开头必须是 `---` YAML frontmatter，并且有结束 `---`。
- 启动日志提示 `name is required` / `description is required` → 补齐必填字段。
- 启动日志提示定义过大 → 调大 `subagent.max_definition_size_bytes`，或精简角色正文。
- 角色能启动但工具调不到 → 检查 `allowed-tools`、`denied-tools`、`subagent.global_denied_tools`、`subagent.background_allowed_tools` 和 `tools.enabled`。
- 后台任务没有预期工具 → 若 `background_allowed_tools` 非空，后台子 Agent 只能看到该白名单里的工具。

---
