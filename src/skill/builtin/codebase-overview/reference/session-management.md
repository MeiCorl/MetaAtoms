# 会话管理 — MetaAtoms 实现原理

> 隶属 Step 9（快捷命令系统）/ 9.1（Slash 注册后端化）| 架构层:第 4 层 记忆层 | 核心入口:`src/memory/session/session.go`

## §1 模块定位

会话管理位于第 4 层 记忆层,负责会话的创建、持久化、恢复、删除、列表与导出,让用户中断后能继续工作、多会话并行场景下能切换上下文。

- **会话 JSON 持久化**:`~/.metaatoms/${user_id}/sessions/<session-id>/messages.jsonl`;用户目录即工作目录,不再按原 CodePilot 工区路径分组
- **三条核心命令**:`/new`(新建)、`/sessions`(列表面板,client 类由前端拦截)、`/resume <id>`(按 ID 前缀恢复)
- **配套命令**:`/clear`(清空当前会话,Step 9 引入)、`/compact`(手动触发摘要压缩,Step 7)
- **会话级临时规则**:`permission.Checker.sessionRules` 字段,跨工具调用生效
- **与 Slash 系统解耦**:`/new` 等命令注册到 `slash.Registry`(`src/command/slash/command.go`),Execute 委托给 `web.Handler` 的既有 `handleNewSession` 等函数

## §2 核心数据结构

- `SessionManager`(`src/memory/session/session.go`)— 会话管理器,旧字段含 `projectName / projectPath / projectDir`;云端 tenant 路径下这些字段实际对应用户目录分组
- `Session`(session.go)— 单个会话,字段含 `ID / CreatedAt / UpdatedAt / Messages []llm.Message` 等
- `SessionSummary`(session.go)— 会话摘要,只含元信息与预览文本(避免加载完整消息列表)
- `projectMeta`(session.go)— `.project.json` 内容,字段 `Path / Basename / CreatedAt`
- `ResumeSessionPayload{ID}`(`src/interaction/web/protocol.go`)— 恢复会话请求,ID 支持前缀匹配
- `DeleteSessionPayload`(protocol.go)— 删除会话请求,ID 必须为完整 ID
- `ListSessionsPayload{Mode}`(`protocol.go`)— 列表请求,`Mode="table"` 取最近 10 条 + `CreatedAt` 降序,空 Mode 按 `UpdatedAt` 降序全量
- `slash.Command` / `slash.Registry`(`src/command/slash/command.go`)— Step 9.1 引入的命令抽象 + 注册中心

## §3 关键流程

### 3.1 会话目录结构

云端模式下 `SessionManager` 的根位于 `~/.metaatoms/${user_id}/sessions`。旧实现按 `workdir` 定位项目目录,在 tenant 装配里 `workdir` 固定为用户目录:

```
~/.metaatoms/${user_id}/sessions/           ← sessionsRoot
  <userDirName>/                            ← 由 filepath.Base(workdir) 算出;通常等于 user_id
    .project.json                           ← 兼容元信息(path / basename / created_at)
    <session-uuid>/
      messages.jsonl                        ← 消息流(每行一条 JSON)
```

`newSessionManager(sessionsRoot, workdir)`(`session.go`)流程:

1. `os.MkdirAll(sessionsRoot, 0755)` 确保根目录存在
2. `resolveProjectDir(sessionsRoot, workdir)` 算 `projectName`;失败时降级为 `shortHash(workdir)` 哈希目录(`session.go`),保证会话功能可用
3. `writeProjectMeta(projectDir, workdir)` 写 `.project.json`(便于后续识别归属)

[Why] 降级到哈希目录:**Why** 目录解析失败是配置问题而非致命错误,降级到哈希目录保证启动不被阻断,用户仍能继续会话操作。

### 3.2 `/new` 新建会话

`newCmd.Execute(ctx, conn, arg)`(`src/command/slash/builtin.go`)委托给 `c.h.HandleNewSessionForSlash(conn)`(`src/interaction/web/handler.go`):

1. `h.sessMgr.CreateNew()` 创建新会话(分配 UUID + 初始化消息列表为空)
2. `h.saveCurrentSessionLocked()` 增量落盘当前会话(持 `h.mu` 写锁)
3. `h.current = newSess` + `h.conv.Reset(empty)` + `h.openSessionLogger(newSess.ID)`
4. `h.assembleSP()` 重新组装 SP(SP 与会话绑定)
5. 推 `session_loaded` 给前端,前端刷新对话区与侧边栏

### 3.3 `/sessions` 列表面板(client 类)

`sessionsCmd`(`builtin.go`)的 `Category() = CategoryClient("client")` 标识由前端识别后走本地逻辑(打开表格视图),`Execute` 是占位返回 nil。前端调 `list_sessions` 拉取,handler 调 `h.sessMgr.ListSessions()` 返回 `[]SessionSummary`。

[Why] client 类:**Why** 列表面板的打开/关闭/分页是 UI 关注点,后端只负责提供数据;`Execute` 委托会让"打开/关闭"这类纯 UI 事件多一次 WS 往返。

### 3.4 `/resume <id>` 恢复会话

`resumeCmd.Execute(ctx, conn, arg)`(`builtin.go`)委托给 `c.h.HandleResumeSessionForSlash(conn, arg)`:

1. 调 `h.sessMgr.ListSessions()` 获取所有会话摘要
2. 对每个摘要 `strings.HasPrefix(s.ID, p.ID)` 做前缀匹配
3. **0 匹配** → 推 `stream_error(session_not_found)`
4. **1 匹配** → `h.sessMgr.Load(matches[0].ID)` 加载完整消息 → 注入 `h.conv` + `h.current = sess` + `assembleSP()`
5. **多匹配** → 推 `stream_error(session_ambiguous)`,要求用户输入更长的 ID 前缀

[Why] 前缀匹配:**Why** UUID 太长,用户记忆成本高;前缀匹配让"复制前 8 位"就能定位,匹配歧义时强制要求更精确输入(纵深防御)。

## §4 与其他模块的依赖

- **上游**(会话模块依赖):
  - `src/llm`(Message / SystemPrompt)— 消息结构与 SP 序列化
  - `src/logger`(`src/logger/`)— 每个会话打开专属日志器(便于多会话并行时日志分流)
- **下游被依赖**:
  - `src/interaction/web/handler`— `sessMgr / conv / current` 字段
  - `src/command/slash/builtin.go`— `/new` `/sessions` `/resume` 命令
  - `src/command/slash/registry.go`— slash 命令注册中心

## §5 设计决策

### 决策 1:JSONL 而非 JSON 持久化消息

- **问题**:JSON 单文件存所有消息会导致「读一条消息要 parse 整文件」;JSON 写是全量覆盖,中断会丢全部
- **方案**:JSONL(messages.jsonl,每行一条 JSON),AppendOnly
- **理由**:**Why** 启动期增量同步「会话恢复到内存」时只需读最后 N 行;Append 写不会因中断丢历史(只丢最后一条);行级定位便于多会话并行场景

### 决策 2:`/sessions` 走 client 类而非 Execute

- **问题**:列表面板的打开/关闭/分页是 UI 关注点,后端只需要提供数据
- **方案**:`Category = "client"`,前端识别后调本地 `openSessionsTable()`,`Execute` 永远返回 nil
- **理由**:**Why** "打开面板"是纯 UI 事件,不应走 WS 业务消息;与 Step 10 `/skills` client 类同款(`src/skill/adapter/client.go`)

### 决策 3:`/resume` 用前缀匹配 + 模糊保护

- **问题**:UUID 太长,用户输入完整 ID 易错;但纯模糊匹配又会让多会话场景歧义
- **方案**:`strings.HasPrefix` 前缀匹配;多匹配时强制要求更长前缀
- **理由**:**Why** 前缀匹配是 UUID 场景的工业惯例(Git commit hash 也用);多匹配兜底防误恢复

### 决策 4:会话级临时规则放在 Checker.sessionRules

- **问题**:用户对某次操作选了"本会话允许",该权限应跨工具调用生效(而非每次重新问)
- **方案**:`permission.Checker.sessionRules []Rule`(`src/security/checker.go`),用户选"本会话允许"时 `AddSessionRule` 追加
- **理由**:**Why** 临时规则与会话生命周期绑定,会话切换 / 新建会自动失效;比"全局 allow"安全,比"每次问"友好

### 决策 5:`HandleNewSessionForSlash` 委托既有 handle 函数

- **问题**:slash 命令注册后,既有 `handleNewSession` 等函数已经能处理业务,重复实现会让代码分叉
- **方案**:`newCmd.Execute` 调 `c.h.HandleNewSessionForSlash(conn)`,后者薄包装 `handleNewSession`
- **理由**:**Why** 业务逻辑 0 改动,slash 注册只是新增触发路径;后续若 `handleNewSession` 改动,slash 路径自动同步

## §6 关键文件索引

| 路径 | 角色 |
|------|------|
| `src/memory/session/session.go` | `SessionManager` 会话管理器 |
| `src/memory/session/session.go` | `NewSessionManager` 构造(旧名定位项目目录;云端模式下定位用户目录) |
| `src/memory/session/session.go` | `newSessionManager` 共用实现(降级哈希目录) |
| `src/memory/session/session.go` | `ListSessions` 列表(按 UpdatedAt 降序) |
| `src/command/slash/command.go` | `SlashCommand` 接口 |
| `src/command/slash/command.go` | `slash.Registry` 注册中心 |
| `src/command/slash/builtin.go` | `newCmd` `/new` 命令 |
| `src/command/slash/builtin.go` | `sessionsCmd` `/sessions` client 类 |
| `src/command/slash/builtin.go` | `resumeCmd` `/resume` 命令 |
| `src/interaction/web/handler.go` | `handleResumeSession` 前缀匹配恢复 |
| `src/interaction/web/handler.go` | `handleDeleteSession` 删除会话 |
| `src/interaction/web/protocol.go` | `ResumeSessionPayload` 恢复请求 |
