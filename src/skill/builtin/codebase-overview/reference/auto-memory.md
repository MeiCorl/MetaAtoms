# 自动学习记忆 - MetaAtoms 实现原理

> 隶属 Step 8（自动学习记忆）| 架构层：第 4 层 记忆层 | 核心入口：`src/memory/autolearn/store.go`

## 1. 模块定位

自动学习记忆是 MetaAtoms 的长期记忆能力：Agent 在每轮对话结束后，由后台 Reviewer 判断是否有值得长期保留的信息，并沉淀为独立 Markdown 文件，后续会话启动时通过 `MEMORY.md` 索引注入上下文。

MetaAtoms 当前没有项目级记忆概念，自动学习只记录 3 类用户级记忆：

- `user_role`：用户长期身份、职责、技能背景、工作领域或协作身份。
- `user_preference`：用户长期做事方式、编码风格、回复语言、工具使用习惯等偏好。
- `user_feedback`：用户对 Agent 输出的纠正、评价和正确做法。

存储上保留两类目录：

- 全局只读基线：`~/.metaatoms/memory`
- 用户级读写目录：`~/.metaatoms/${user_id}/memory`

全局目录只参与索引注入，自动学习写入只落到当前用户目录。

## 2. 核心结构

- `MemoryType`（`src/memory/autolearn/types.go`）：三类记忆枚举，常量为 `MemoryTypeUserRole / MemoryTypeUserPreference / MemoryTypeUserFeedback`。
- `StorageScope`（types.go）：存储域，当前使用 `ScopeGlobal / ScopeUser`；`ScopeProject` 仅作为兼容别名保留并指向 `ScopeUser`。
- `ScopeOf(t MemoryType)`（types.go）：所有合法记忆类型都返回 `ScopeUser`。
- `Store`（`src/memory/autolearn/store.go`）：文件持久化抽象，维护 `globalRoot / userRoot / mu`。
- `IndexEntry`（types.go）：`MEMORY.md` 索引行，格式为 `- [type](slug.md)——一句话简介`。
- `Reviewer`（`src/memory/autolearn/reviewer.go`）：后台异步回顾器。
- `MemoryIndexSource`（`src/engine/prompt/sources/memory_index.go`）：读取全局基线和用户记忆索引，注入 Prompt。

## 3. 关键流程

### 3.1 索引渲染

`Store.RewriteIndex(scope, entries)` 会按 `memoryTypeOrder` 固定顺序渲染 `MEMORY.md`：

1. `user_role`
2. `user_preference`
3. `user_feedback`

非法类型会在解析或渲染时被过滤，因此旧的 `project_knowledge` / `reference` 不会再进入新的 Prompt 索引。

### 3.2 Prompt 注入

`MemoryIndexSource.Assemble` 每次组装 Prompt 时读取：

1. 用户级索引：`store.ReadIndex(ScopeUser)`
2. 全局基线索引：`store.ReadIndex(ScopeGlobal)`

二者渲染进 `<memory_index>`，Placement 为 `UserMessage`。这样长索引不会常驻 System Prompt，降低注意力稀释。

### 3.3 后台 Reviewer

`Reviewer.OnLoopDone` 在 Agent Loop 正常完成后触发：

1. `shouldReview(req)` 过滤空输入、闲聊和未完成轮次。
2. 同一 session 通过 `inflight` 做串行保护，上一轮仍在回顾时丢弃本次回顾请求。
3. 后台 goroutine 调用 LLM，要求只输出 JSON 数组。
4. `parseReviewDecisions` 校验 `action/type/slug/content`。
5. `applyOne` 统一通过 `ScopeOf` 写入用户级目录，并刷新 `MEMORY.md`。

### 3.4 敏感信息保护

自动学习有两道防线：

- Prompt 明确禁止记录 API key、密码、token、私钥、`.env` 密钥和数据库口令。
- `Sanitize` 在落盘前用正则兜底脱敏高熵凭证、Bearer token 和键值对口令。

## 4. Tenant 装配

云端 tenant 装配在 `src/main.go` 中计算两个根：

1. `globalRoot = ~/.metaatoms/memory`
2. `userRoot = ~/.metaatoms/${user_id}/memory`
3. `memoryReadStore = autolearn.NewStore(globalRoot, userRoot)`
4. `memoryWriteStore = autolearn.NewStore(userRoot, userRoot)`

读写分离保证全局基线只读，自动学习只更新当前登录用户的记忆。

## 5. 文件索引

| 路径 | 角色 |
| --- | --- |
| `src/memory/autolearn/types.go` | 记忆类型、存储域、索引结构 |
| `src/memory/autolearn/store.go` | 记忆文件和 `MEMORY.md` 持久化 |
| `src/memory/autolearn/prompt.go` | Reviewer Prompt、JSON 决策解析 |
| `src/memory/autolearn/reviewer.go` | 后台异步回顾和落盘编排 |
| `src/memory/autolearn/sanitizer.go` | 敏感信息脱敏 |
| `src/engine/prompt/sources/memory_index.go` | 记忆索引注入 |
| `src/main.go` | tenant memory read/write store 装配 |
