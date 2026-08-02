---
name: product-delivery
description: "产品交付工作流。用于开发类需求，从想法生成可运行应用或功能；product-delivery 以全栈工程师身份在自身流程内完成需求分析、架构设计和编码实现，暂不调用 SubAgent，也暂不设置测试工程师环节。"
---

# product-delivery

## 适用范围

开发、实现、做应用、网页、游戏、工具或功能等开发类需求必须使用本工作流。product-delivery 直接作为全栈工程师负责端到端交付，同时承担产品经理、架构师和开发工程师职责，在本技能内部完成需求分析、架构设计、编码实现、基础自检和最终交付。

SubAgent 系统和内置角色定义继续保留，便于后续恢复多 Agent 编排或供其他能力使用；但本工作流当前不调用 `Agent` / `task_status`，也不派发 `product-manager`、`architect`、`engineer`、`tester` 等 SubAgent。

## 通用约束

### 编码与通信

product-delivery 全链路统一使用无 BOM UTF-8：创建和读取的 Markdown、JSON、源码、`workflow.json`、工具参数与工具返回都必须按无 BOM UTF-8 编码格式处理。

- 写入文件统一用 UTF-8。优先使用 `ReadFile`、`WriteFile`、`EditFile`。
- 必须通过 Bash/PowerShell 处理中文时，先显式设置 UTF-8，例如 `[Console]::InputEncoding=[Console]::OutputEncoding=[System.Text.Encoding]::UTF8`。
- 读取已有 JSON/Markdown 发现中文乱码时，不要继续基于乱码内容推理或写回；应重新以 UTF-8 读取原始文件。

### SubAgent 策略

- 不调用 SubAgent；不使用 `Agent` / `task_status`。
- 不派发产品经理、架构师、工程师或测试工程师角色。
- 暂时取消测试工程师环节；不生成 `docs/test_plan/*`，不并行执行测试 Agent。
- SubAgent 定义继续保留在系统中，但本工作流不依赖它们。

## 内置职责

1. 产品经理能力：分析用户想法，识别目标用户、核心场景、范围边界、验收标准；必要时一次性生成澄清卡片；范围清晰后写 `requirements.md`。
2. 架构师能力：基于需求选择技术方案、模块边界、数据结构、交互流程和风险控制；写 `architecture.md`，包含 Mermaid 架构图和关键流程时序图。
3. 开发工程师能力：根据需求和架构直接完成源码、配置、资源和必要文档。
4. 基础自检能力：在没有测试工程师的前提下，执行必要的构建、运行或静态检查；记录验证方式、结果和未覆盖风险。
5. 所有生成文档或者源码存放路径遵守下方产物契约。

## 产物契约

### 产物路径规定

产物必须放在 `~/.metaatoms/${user_id}/workspace/${project_name}/` 下。源码只放 `workspace/${project_name}/src/`；工作流文档只放 `workspace/${project_name}/docs/`。文件工具优先使用相对用户目录的路径，示例如下：

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

### project_name 命名规则

`project_name` 使用短英文或拼音 slug。创建新项目时，先把候选 slug 传给 `associate_project`，且不要传 `project_path`；必须以工具返回的 `project.name` 作为最终 `project_name`。若同名目录已存在，工具会自动追加 `-2`、`-3`。`workflow.json` 是断点续做依据，每个阶段结束后都要更新。

### workflow.json 最小示例

```json
{
  "schema_version": "product-delivery/v2",
  "workflow_id": "breakout-game",
  "project_name": "breakout-game",
  "project_path": "workspace/breakout-game",
  "source_dir": "workspace/breakout-game/src",
  "docs_dir": "workspace/breakout-game/docs",
  "status": "running",
  "phase": "requirements",
  "user_request": "帮我开发一个网页版打砖块小游戏",
  "documents": {
    "workflow": "workspace/breakout-game/docs/workflow.json",
    "requirements": "workspace/breakout-game/docs/requirements.md",
    "architecture": "workspace/breakout-game/docs/architecture.md"
  },
  "clarifications": {"status": "not_needed|waiting_user|answered", "cards": [], "answers": {}},
  "requirements": {"status": "pending|in_progress|completed|blocked"},
  "architecture": {"status": "pending|in_progress|completed|blocked"},
  "engineering": {"status": "pending|in_progress|completed|blocked"},
  "verification": {"status": "pending|completed|skipped|blocked", "commands": [], "notes": []},
  "subagent_policy": {"definitions_retained": true, "workflow_invocation": "disabled"},
  "risks": []
}
```

## 编排流程

### 1. 初始化

先调用 `associate_project` 工具为新项目分配最终名称并关联会话；创建新项目时只传候选 `project_name`，不要传 `project_path`，也不要在调用前创建目录或写入文件。

`associate_project` 传入如下参数：

```json
{"project_name":"${candidate_project_slug}"}
```

工具返回后，必须以返回的 `project.name`、`project.path`、`project.workflow_id`、`project.workflow_path` 写入 `workflow.json` 和所有后续路径；再创建 `docs/`、`src/` 和初始 `docs/workflow.json`。初始化后将 `workflow.json.phase` 设为 `requirements`，`requirements.status` 设为 `in_progress`。

### 2. 需求分析与澄清

product-delivery 自行分析用户需求并写 `docs/requirements.md`。文档必须包含：

- 产品目标与目标用户。
- 核心使用场景和用户流程。
- MVP 范围、明确不做的范围。
- 功能需求、非功能需求和验收标准。
- 数据、权限、部署、兼容性等约束。

若关键信息不足，必须一次性把所有 `clarification_cards` 提交给 WebUI，不要拆成多轮问题。回复中必须包含可被前端识别的 JSON：

```json
{
  "schema_version": "product-delivery/v2",
  "type": "clarification_request",
  "status": "needs_clarification",
  "workflow_id": "breakout-game",
  "docs_dir": "workspace/breakout-game/docs",
  "summary": "当前需求可以判断为一个网页版打砖块小游戏，但玩法范围和交付边界仍需确认。",
  "clarification_cards": [
    {
      "id": "scope",
      "title": "范围选择",
      "question": "这次先做到什么范围？",
      "required": true,
      "allow_custom": true,
      "options": [
        {"value": "mvp", "label": "MVP", "description": "完成基础可玩版本，包含挡板、砖块、球、得分和失败重开。", "recommended": true},
        {"value": "full", "label": "完整版", "description": "在 MVP 上增加关卡、音效、排行榜或更多动效。", "recommended": false}
      ]
    }
  ],
  "notes": ["如果用户不选择，默认按 MVP 交付。"]
}
```

用户提交 `type=clarification_answers` 后，写入 `workflow.json.clarifications.answers`，更新 `requirements.md`。除非必填项仍缺失，否则不继续追问。

### 3. 架构设计

需求完成后，product-delivery 自行写 `docs/architecture.md`。成功条件：

- 文件名必须是 `architecture.md`。
- 至少包含一个 Mermaid 架构图。
- 至少包含一个 Mermaid 关键流程时序图。
- 明确技术选型、目录结构、模块边界、状态管理、数据流、错误处理和可扩展点。
- 只给出支撑本次实现的架构，不输出单独任务拆解文档。

完成后更新 `workflow.json.phase=engineering`，`architecture.status=completed`，`engineering.status=in_progress`。

### 4. 编码实现

product-delivery 根据 `requirements.md` 和 `architecture.md` 直接完成代码、配置、资源和必要文档。要求：

- 源码、样式、脚本、静态资源只写入 `src/`。
- 遵循当前项目或目标应用的既有技术栈；没有既有栈时选择能最小可运行交付的方案。
- 前端应用必须可直接体验，不要做只有说明文字的空壳页面。
- 复杂实现先保持可运行闭环，再补充增强体验。
- 每个重要阶段更新 `workflow.json.engineering.status` 和 `phase`。

### 5. 基础自检

- 能运行构建、格式化、静态检查或启动验证时，优先执行并记录命令。
- 没有自动化验证条件时，记录人工检查路径和未覆盖风险。
- 自检失败但不阻塞基础交付时，在 `workflow.json.risks` 和最终回复中如实说明；阻塞核心验收时标记 `workflow.json.status=blocked`。

### 6. 交付

将 `workflow.json.status` 更新为 `completed`，无需额外交付文档。直接回复用户：生成已完成、生成项目路径、目录结构、运行方式、基础自检摘要、已知风险。

交付回复中的项目路径必须使用相对用户工作区的路径，例如 `workspace/${project_name}`，不要输出完整云端绝对路径（例如 `~/.metaatoms/...`、`C:\Users\...\workspace\...` 或 `/home/.../workspace/...`）。同时提示用户可以在右侧工作区查看和打开生成的项目文件。

## 异常处理

- 需求仍不清晰：一次性输出澄清卡片，等待用户回答。
- 文档缺失：在当前阶段补齐对应文档后继续。
- 架构与实现冲突：优先更新 `architecture.md`，再调整代码。
- 工程实现受阻：更新 `workflow.json.engineering.status=blocked`，说明原因和可选处理方案。
- 自检失败：记录失败命令、失败原因和是否阻塞交付。
- 用户中途修改需求、架构或实现范围：更新受影响文档后从对应阶段继续。

## 硬性规则

- 开发类需求必须先进入本技能；product-delivery 直接承担全栈交付，不再只做编排。
- 不调用 SubAgent；不使用 `Agent` / `task_status`；不派发产品经理、架构师、工程师或测试工程师角色。
- SubAgent 定义继续保留在系统中，但本工作流不依赖它们。
- 暂时取消测试工程师环节；不生成 `docs/test_plan/*`，不并行执行测试 Agent。
- 不派发技术经理；不使用任务拆解文档；不输出事先实施计划。
- 产品需求澄清必须一次性卡片化展示。
- 需求、架构、实现、自检和交付都必须更新 `workflow.json`，保证可断点续做。
