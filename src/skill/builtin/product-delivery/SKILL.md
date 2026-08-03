---

name: product-delivery
description: 产品交付工作流。用于开发类需求，从用户想法生成可运行应用或功能；必须按五步统一流程执行：项目初始化、需求分析、架构设计、编码实现、基础验证。
---

# product-delivery

## 适用范围

开发、实现、做应用、网页、游戏、工具或功能等开发类需求必须使用本工作流。product-delivery 直接作为全栈工程师负责端到端交付，同时承担产品经理能力、架构师能力、开发工程师能力和基础验证能力。

本工作流当前不调用 SubAgent，不使用 `Agent` / `task_status`，不派发 `product-manager`、`architect`、`engineer`、`tester` 等角色。SubAgent 系统和内置角色定义继续保留在系统中，但 product-delivery 的主流程不依赖它们。

## 统一五步流程

UI 和 Agent 行为都必须围绕下列五步对齐；任何阶段推进都要更新 `docs/workflow.json`。

1. 项目初始化：关联会话与项目，创建 `docs/`、`src/` 和初始 `workflow.json`。
2. 需求分析：澄清范围，生成或更新 `docs/requirements.md`。
3. 架构设计：生成或更新 `docs/architecture.md`，包含 Mermaid 架构图和关键流程时序图。
4. 编码实现：实现应用源码、配置、资源和必要说明。
5. 基础验证：执行能运行的构建、启动、静态检查或人工验证记录，更新验证结果。



## 通用约束

- 全链路统一使用 UTF-8。写入 Markdown、JSON、源代码、`workflow.json`、工具参数与工具返回都必须按 UTF-8 处理。
- 读写文件优先使用 `ReadFile`、`WriteFile`、`EditFile`。
- 必须通过 Bash/PowerShell 处理中文时，先显式设置 UTF-8，例如 `[Console]::InputEncoding=[Console]::OutputEncoding=[System.Text.Encoding]::UTF8`。
- 不要把乱码内容写入文档、源码或最终 JSON；若读取时发现中文乱码，重新以 UTF-8 读取原始文件。
- 不调用 SubAgent；不设置独立测试阶段。



## 产物路径契约

产物必须放在 `~/.metaatoms/${user_id}/workspace/${project_name}/` 下。源码只放 `workspace/${project_name}/src/`；工作流文档只放 `workspace/${project_name}/docs/`。

```text
workspace/${project_name}/
  docs/
    workflow.json
    requirements.md
    architecture.md
  src/
    index.html
    js/
      main.js
    css/
      main.css
```

`project_name` 使用短英文或拼音 slug。创建新项目时，先把候选 slug 传给 `associate_project`，且不要传 `project_path`，不要在调用前创建目录或写入文件。必须以工具返回的 `project.name`、`project.path`、`project.workflow_id`、`project.workflow_path` 作为最终路径依据。

## workflow.json 契约

`workflow.json` 是断点续做和 UI 展示依据。`phase` 只能使用 `initialization`、`requirements`、`architecture`、`implementation`、`verification`。每个阶段结束后都要更新状态。

```json
{
  "schema_version": "product-delivery/v3",
  "workflow_id": "breakout-game",
  "project_name": "breakout-game",
  "project_path": "workspace/breakout-game",
  "source_dir": "workspace/breakout-game/src",
  "docs_dir": "workspace/breakout-game/docs",
  "status": "running",
  "phase": "requirements",
  "user_request": "帮我开发一个网页版打砖块小游戏",
  "steps": [
    {"id": "initialization", "label": "项目初始化", "status": "completed"},
    {"id": "requirements", "label": "需求分析", "status": "in_progress"},
    {"id": "architecture", "label": "架构设计", "status": "pending"},
    {"id": "implementation", "label": "编码实现", "status": "pending"},
    {"id": "verification", "label": "基础验证", "status": "pending"}
  ],
  "documents": {
    "workflow": "workspace/breakout-game/docs/workflow.json",
    "requirements": "workspace/breakout-game/docs/requirements.md",
    "architecture": "workspace/breakout-game/docs/architecture.md"
  },
  "clarifications": {"status": "not_needed|waiting_user|answered", "cards": [], "answers": {}},
  "requirements": {"status": "pending|in_progress|completed|blocked"},
  "architecture": {"status": "pending|in_progress|completed|blocked"},
  "implementation": {"status": "pending|in_progress|completed|blocked"},
  "verification": {"status": "pending|completed|skipped|blocked", "commands": [], "notes": []},
  "subagent_policy": {"definitions_retained": true, "workflow_invocation": "disabled"},
  "risks": []
}
```



## 编排规则



### 1. 项目初始化

先调用 `associate_project` 为新项目分配最终名称并关联会话。调用参数如下：

```json
{"project_name":"${candidate_project_slug}"}
```

工具返回后，创建 `docs/`、`src/` 和 `docs/workflow.json`。初始化后将 `workflow.json.phase` 设为 `requirements`，`steps.initialization.status=completed`，`requirements.status=in_progress`。

### 2. 需求分析

自行分析用户需求并写 `docs/requirements.md`，必须包含产品目标、目标用户、核心场景、MVP 范围、不做范围、功能需求、非功能需求、验收标准、数据/权限/部署/兼容约束。

若关键信息不足，必须一次性输出可被前端识别的 `clarification_request` JSON，不要拆成多轮零散追问。该 JSON 必须包含 `schema_version`、`type`、`status`、`workflow_id`、`summary` 和 `clarification_cards`。用户提交 `type=clarification_answers` 后，写入 `workflow.json.clarifications.answers` 并继续，不要重复追问非阻塞细节。

### 3. 架构设计

需求完成后写 `docs/architecture.md`。文件必须包含技术选型、目录结构、模块边界、状态管理、数据流、错误处理、可扩展点，且至少包含一个 Mermaid 架构图和一个 Mermaid 关键流程时序图。完成后更新 `workflow.json.phase=implementation`。

### 4. 编码实现

根据 `requirements.md` 和 `architecture.md` 完成源码、配置、资源和必要文档。源码、样式、脚本、静态资源只写入 `src/`。前端应用必须可直接体验，不要做只有说明文字的空壳页面。

实现完成后更新 `workflow.json.phase=verification`。

### 5. 基础验证

能运行构建、格式化、静态检查或启动验证时，优先执行并记录命令。没有自动化条件时，记录人工检查路径和未覆盖风险。验证失败但不阻塞基础交付时，在 `workflow.json.risks` 和最终回复中说明；阻塞核心验收时标记 `workflow.json.status=blocked`。

## 交付回复

验证完成后将 `workflow.json.status` 更新为 `completed`。最终直接回复：生成已完成、项目路径、目录结构、运行方式、基础验证摘要、已知风险。项目路径必须使用相对用户工作区的路径，例如 `workspace/${project_name}`，不要输出完整云端绝对路径。提示用户可在右侧工作区查看生成的项目文件。

## 异常处理

- 需求仍不清晰：一次性输出澄清卡片，等待用户回答。
- 文档缺失：在当前阶段补齐对应文档后继续。
- 架构与实现冲突：优先更新 `architecture.md`，再调整代码。
- 工程实现受阻：更新 `workflow.json.implementation.status=blocked`，说明原因和可选方案。
- 基础验证失败：记录失败命令、失败原因和是否阻塞交付。
- 用户中途修改范围：更新受影响文档后从对应阶段继续。



## 硬性规则

- 开发类需求必须先进入本技能，且按五步流程推进。
- 不调用 SubAgent；不使用 `Agent` / `task_status`。
- 不写 `tasks.md`、`checklists.md` 或 `delivery.md`。
- 需求澄清必须一次性卡片化展示。
- 需求、架构、实现、基础验证和交付都必须更新 `workflow.json`，保证可断点续做。

