# 会话管理 - MetaAtoms 实现原则

> 记忆层核心入口：`src/memory/session/session.go`

## 模块定位

会话管理负责会话的创建、持久化、恢复、删除、列表和上下文压缩归档。当前实现已经取消旧项目分组概念，所有会话数据按用户和会话分目录落盘。

## 路径约定

全局日志：

```text
~/.metaatoms/logs/metaatoms.log
```

会话级数据：

```text
~/.metaatoms/<user_id>/sessions/<session_id>/
  meta.json
  messages.jsonl
  history_archive.jsonl
  metaatoms.log
  tool_results/
```

说明：

- `~/.metaatoms/<user_id>/sessions` 是 `SessionManager` 的 `sessionsRoot`。
- `<session_id>` 目录直接挂在 `sessionsRoot` 下，不再经过旧项目名 / `filepath.Base(workdir)` / hash 目录。
- `NewSessionManagerWithDir(sessionsRoot, workdir)` 中的 `workdir` 仅为兼容旧调用保留，不参与路径计算。
- `SessionManager.SessionsRoot()` 返回 `sessionsRoot`，用于工具结果等会话附属产物装配。

## 核心结构

- `SessionManager`：持有 `sessionsRoot`，所有会话目录的父目录。
- `Session`：单个会话，包含 `ID / CreatedAt / UpdatedAt / Messages`。
- `SessionSummary`：列表展示用摘要，仅包含元信息和首条用户消息预览。
- `ToolResultStore`：接收 `sessionsRoot`，将工具结果写入 `<session_id>/tool_results/`。

## 关键流程

### 新建会话

`Handler` 调用 `sessMgr.CreateNew()` 生成 UUID，并在首次追加消息时通过 `AppendMessages` 惰性创建：

```text
<sessionsRoot>/<session_id>/meta.json
<sessionsRoot>/<session_id>/messages.jsonl
```

### 恢复和列表

`ListSessions` / `LoadLatest` / `Load` 只扫描 `sessionsRoot` 下的会话子目录。非目录文件会被忽略。

### 会话日志

`web.Handler.openSessionLogger(id)` 调用：

```go
logger.OpenSession(id, h.sessMgr.SessionDir(id))
```

因此会话日志写入：

```text
~/.metaatoms/<user_id>/sessions/<session_id>/metaatoms.log
```

### 上下文持久化

压缩归档和工具结果与会话目录同级约定对齐：

```text
~/.metaatoms/<user_id>/sessions/<session_id>/history_archive.jsonl
~/.metaatoms/<user_id>/sessions/<session_id>/tool_results/<tool_use_id>
```

## 相关文件

| 路径 | 角色 |
|------|------|
| `src/main.go` | 按用户装配 `~/.metaatoms/<user_id>/sessions` |
| `src/auth/auth.go` | 计算 `~/.metaatoms` 和用户目录 |
| `src/memory/session/session.go` | 会话目录、meta、messages 持久化 |
| `src/memory/session/archive.go` | `history_archive.jsonl` 归档 |
| `src/memory/context/tool_result_store.go` | 工具结果落盘 |
| `src/logger/logger.go` | 全局日志与会话日志 |
| `src/interaction/web/handler.go` | 会话切换时打开会话级 logger |
