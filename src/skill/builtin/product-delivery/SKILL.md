---
name: product-delivery
description: "产品交付工作流。用于用户提出开发类需求、从想法生成可运行应用或功能时，强制主智能体按产品经理需求澄清、架构设计、任务拆解、工程实现、测试计划与测试报告的阶段化流程执行；通过 product-manager、architect、tech-lead、engineer、tester 子智能体协作，落盘 requirements.md、architecture.md、tasks.md、checklists.md、test-report.md 和 workflow.json，支持卡片选项式一次性澄清、结构化输入输出、任务进度跟踪与断点续做。"
---
# product-delivery

本技能用于开发类需求的阶段化产品交付。只要用户要求“开发、实现、做一个应用、做一个网页/游戏/工具/功能”，主智能体必须使用本工作流，不能直接跳到编码。

## 目标

主智能体是全局项目管理者，只做编排、状态维护、用户沟通和最终交付说明。具体阶段交给子智能体：

1. `product-manager`: 需求分析，必要时一次性生成澄清卡片，最终写 `requirements.md`
2. `architect`: 架构设计，写 `architecture.md`，必须包含 Mermaid 架构图和时序图
3. `tech-lead`: 任务拆解，写 `tasks.md`，每个任务可由独立工程师会话完成
4. `tester`: `mode=create_plan` 时写 `checklists.md`
5. `engineer`: 按 `tasks.md` 逐个完成子任务
6. `tester`: `mode=run_tests` 时执行测试，更新 `checklists.md`，写 `test-report.md`

## 工作目录

新项目目录：

```text
workspace/${project_name}/
  docs/
    workflow.json
    requirements.md
    architecture.md
    tasks.md
    checklists.md
    test-report.md
  src/
    ...应用源码文件...
```

`project_name` 使用短英文或拼音 slug；若项目目录已存在，追加 `-2`、`-3`。

`workflow.json` 是机器编排的权威状态；Markdown 文档是人类可读交付物。主智能体每个阶段结束后都要更新 `workflow.json`。

## workflow.json 最小结构

```json
{
  "schema_version": "product-delivery/v1",
  "workflow_id": "breakout-game",
  "project_name": "breakout-game",
  "project_path": "workspace/breakout-game",
  "source_dir": "workspace/breakout-game/src",
  "status": "running",
  "phase": "requirements",
  "user_request": "帮我开发一个网页版打砖块小游戏",
  "docs_dir": "workspace/breakout-game/docs",
  "documents": {
    "workflow": "workspace/breakout-game/docs/workflow.json",
    "requirements": "workspace/breakout-game/docs/requirements.md",
    "architecture": "workspace/breakout-game/docs/architecture.md",
    "tasks": "workspace/breakout-game/docs/tasks.md",
    "checklists": "workspace/breakout-game/docs/checklists.md",
    "test_report": "workspace/breakout-game/docs/test-report.md"
  },
  "clarifications": {
    "status": "not_needed|waiting_user|answered",
    "cards": [],
    "answers": {}
  },
  "tasks": [
    {"id": "T01", "title": "项目骨架", "status": "pending", "dependencies": []}
  ],
  "subagent_runs": [],
  "risks": []
}
```

## 项目工作区与会话关联

- 所有生成项目产物必须放在 `~/.metaatoms/${user_id}/workspace/${project_name}/` 下。
- 工作流文档必须放在 `workspace/${project_name}/docs/` 下。
- 应用源码文件必须放在 `workspace/${project_name}/src/` 下，不要把源码文件散落在项目根目录。
- 调用文件工具时，优先使用相对当前用户目录的路径，例如 `workspace/breakout-game/src/index.html`。
- 主智能体创建项目目录和初始 `workflow.json` 后，必须立刻调用 `associate_project` 工具，并传入下面的结构化参数：

```json
{
  "project_name": "${project_name}",
  "project_path": "workspace/${project_name}",
  "workflow_id": "${workflow_slug}",
  "workflow_path": "workspace/${project_name}/docs/workflow.json"
}
```

- 会话关联用于后续恢复或切换 session 后继续更新同一个生成项目；只在 Markdown 中记录路径是不够的。

## 编排流程

### 1. 初始化

主智能体创建项目目录、`docs/`、`src/` 和 `docs/workflow.json`，然后派发：

```json
{
  "type": "defined",
  "role": "product-manager",
  "task": "...结构化任务文本...",
  "metadata": {
    "workflow_id": "<workflow_id>",
    "phase": "requirements",
    "project_path": "workspace/${project_name}",
    "source_dir": "workspace/${project_name}/src",
    "docs_dir": "<docs_dir>",
    "expected_schema": "product-delivery/v1"
  }
}
```

所有子智能体最终回复必须是严格 JSON。主智能体必须读取 `structured_output.parsed_json`；如果缺失，重试一次并要求“只返回 JSON”。重试后仍缺失则标记 `blocked`。

### 2. 需求澄清

`product-manager` 如果返回：

```json
{"status": "needs_clarification", "clarification_cards": [...]}
```

主智能体必须把所有卡片一次性提交给 WebUI，不要拆成多轮零散问题。主智能体回复用户时必须包含一个可被前端识别的 JSON 对象（直接 JSON 或 fenced `json` 代码块均可），结构如下：

```json
{
  "schema_version": "product-delivery/v1",
  "type": "clarification_request",
  "status": "needs_clarification",
  "workflow_id": "breakout-game",
  "docs_dir": "workspace/breakout-game/docs",
  "summary": "请确认以下需求选项。",
  "clarification_cards": [
    {
      "id": "scope",
      "title": "范围选择",
      "question": "这次先做到什么范围？",
      "required": true,
      "allow_custom": true,
      "options": [
        {
          "value": "mvp",
          "label": "MVP",
          "description": "先完成最小可玩版本。",
          "recommended": true
        },
        {
          "value": "full",
          "label": "完整版",
          "description": "增加关卡、音效、排行榜等增强能力。",
          "recommended": false
        }
      ]
    }
  ]
}
```

WebUI 识别到上述 JSON 后会弹出多 tab 需求确认窗口：每个 tab 对应一张澄清卡片；每张卡片用单选按钮展示选项，推荐项可默认选中；最后固定提供“其它”选项和输入框。用户完成所有必选项后一次性提交，前端会发送如下结构化用户消息：

```json
{
  "schema_version": "product-delivery/v1",
  "type": "clarification_answers",
  "workflow_id": "breakout-game",
  "docs_dir": "workspace/breakout-game/docs",
  "answers": {
    "scope": {
      "question": "这次先做到什么范围？",
      "value": "mvp",
      "label": "MVP",
      "description": "先完成最小可玩版本。",
      "custom": false,
      "custom_text": ""
    }
  }
}
```

主智能体收到 `type=clarification_answers` 后，必须写入 `workflow.json.clarifications.answers`，再次派发 `product-manager`，要求生成 `requirements.md`。除非用户提交后仍缺失必填项，否则产品经理不应继续追问。

### 3. 架构设计

PM 完成后派发 `architect`。成功条件：
- `architecture.md` 存在
- 文档包含至少一个 Mermaid 架构图
- 文档包含至少一个 Mermaid 时序图
- 子智能体 JSON `status=completed`

文件名必须是 `architecture.md`。

### 4. 任务拆解

架构师完成后派发 `tech-lead`。成功条件：
- `tasks.md` 存在
- `parsed_json.tasks` 非空
- 每个任务有 `id/title/status/dependencies`
- 初始状态为 `pending`

主智能体将任务数组同步到 `workflow.json.tasks`。

### 5. 测试计划与工程实现

技术经理完成后，主智能体派发 `tester` 的 `mode=create_plan` 生成 `checklists.md`。

优先尝试与工程实现并行：如果当前配置允许后台子智能体写文件，可用 `background=true` 启动测试计划；否则以前台快速生成测试计划，再继续工程任务。不要因为并行能力受限而跳过测试计划。

工程任务按依赖顺序逐个派发给 `engineer`：
- 每次选择 `status != completed` 且依赖都为 `completed` 的最小任务
- 派发前在 `workflow.json` 标记该任务为 `in_progress`
- 工程师完成后，校验 JSON 和 `tasks.md` 状态
- 成功则更新 `workflow.json` 为 `completed`
- 状态为 `blocked` 则停止编排，向用户报告阻塞原因和选择项

### 6. 测试执行与报告

所有工程任务完成后，派发 `tester` 的 `mode=run_tests`。

成功条件：
- `checklists.md` 更新了实际结果和结论
- `test-report.md` 存在
- JSON `test_summary` 有 passed/failed/skipped

如果失败项不影响基础交付，主智能体可以如实交付并标明风险；如果失败项阻塞核心验收，标记工作流状态为 `blocked` 并说明需要修复。

### 7. 交付

主智能体将 `workflow.json.status` 更新为 `completed`，回复用户：
- 完成了什么
- 文档目录
- 运行方式
- 测试结果摘要
- 已知风险

## 异常处理

- 子智能体返回 `blocked`: 立即停止自动编排，向用户展示原因和可选处理方案。
- 缺少 `parsed_json`: 重试一次；仍失败则标记为 `blocked`。
- 文档缺失: 要求同一角色修复。
- 工程师修改了非当前任务状态: 主智能体纠正 `workflow.json`，必要时修复 `tasks.md`。
- 用户中途要求修改需求/架构/任务: 停止工程编排，更新对应文档后从受影响阶段继续。

## 主智能体硬性规则

- 开发类需求必须先进入本技能。
- 不要直接编码。
- 不要跳过 PM、架构师、技术经理和测试计划。
- 产品经理澄清必须一次性卡片化展示。
- 所有子智能体输入都带 `metadata`。
- 所有子智能体输出都以 `parsed_json` 为准。
- `workflow.json` 是断点续做依据。
