# 权限管理 - MetaAtoms 实现原理

> 隶属 Step 5（安全边界）| 架构层：第 5 层 安全层 | 核心入口：`src/security/checker.go`

## §1 模块定位

MetaAtoms 当前权限系统已收敛为固定安全边界，不再提供可配置的 `allow / ask / deny` 规则、权限模式 `mode` 或 HITL 人工确认流程。

保留能力只有两类：

- **Bash 黑名单**：`CheckBashCommand` 在工具执行前拦截危险命令。
- **用户目录路径限制**：`ReadFile` / `WriteFile` / `EditFile` / `Glob` / `Grep` 等路径类工具只能访问当前用户目录内的路径。

这意味着用户不能通过 `setting.json` 扩大工具权限，也没有 WebUI 权限弹窗或“记住本次选择”写回流程。

## §2 核心数据结构

- `Checker`（`src/security/checker.go`）：持有当前 `workdir`，负责 Bash 黑名单和路径越界判断。
- `Decision`（`checker.go`）：单次检查结果，字段为 `Allowed / Reason / TargetPath / Workdir`。
- `Interceptor`（`src/security/interceptor.go`）：工具执行前统一调用 `Checker.Decide`，只处理允许或拒绝。
- `SandboxMiddleware`（`src/security/sandbox_middleware.go`）：路径类工具的硬边界，解析路径并把规范化结果注入 `PathResolver`。
- `ResolveInSandbox` / `IsPathOutsideSandbox`（`src/security/sandbox.go`）：工作目录边界校验。
- `CheckBashCommand`（`src/security/blacklist.go`）：Bash 危险命令黑名单。

## §3 执行链路

工具调用流程：

```text
ToolHandler.doExecute
  -> PermissionForInput        # 仅用于 read/write/exec 调度分类
  -> Interceptor.Check
       -> Checker.Decide
            -> Bash: CheckBashCommand
            -> path tools: IsPathOutsideSandbox(workdir)
            -> other tools: allow
  -> SandboxMiddleware
       -> ResolveInSandbox(workdir)
       -> WithPathResolver(ctx, absPath)
  -> tool.Execute(ctx, input)
  -> ToolHandler fire OnEnd
```

要点：

- Bash 命中黑名单直接拒绝；未命中则继续执行。
- 路径工具在 `Checker` 和 `SandboxMiddleware` 两层都按 `workdir` 兜底校验。
- `SandboxMiddleware` 是不可配置的硬边界，防止工具实现遗漏路径检查。
- 非路径、非 Bash 的工具默认允许；其能力暴露主要通过 `tools.enabled` 控制。

## §4 特殊路径

`SandboxMiddleware` 对内置 `embedded://skill/builtin/...` 的 `ReadFile` 保留固定放行，用于读取内置 Skill reference 文档。这不是用户可配置权限，也不会放宽普通文件系统路径。

MCP 远端工具统一按执行类工具注册。远端工具本身的副作用不可由本地精确判断，因此本地安全层只提供统一拦截入口和路径边界兜底；是否暴露 MCP 工具由 MCP 配置与 `tools.enabled` 控制。

## §5 已移除能力

以下旧能力已经删除，不应再作为当前实现描述或配置建议：

- `permissions.mode`
- `permissions.rules[]`
- `allow / ask / deny`
- HITL 权限确认弹窗
- one-time / session / permanent 授权范围
- 权限规则写回 `setting.json`
- `security/config.go`、`policy.go`、`hitl.go`、`path_pattern.go`
- `LoadPermissions`、`PermissionPolicy`、`Rule`
- `WithReadRoots`、`buildMemoryReadRoots`、`buildSkillReadRoots`

## §6 关键文件索引

| 路径 | 角色 |
|------|------|
| `src/security/checker.go` | 固定安全决策：Bash 黑名单 + 用户目录路径限制 |
| `src/security/interceptor.go` | 工具执行前拦截入口 |
| `src/security/sandbox_middleware.go` | 路径类工具沙箱中间件 |
| `src/security/sandbox.go` | 路径规范化与 workdir 边界校验 |
| `src/security/blacklist.go` | Bash 危险命令黑名单 |
| `src/engine/conversation/tool_handler.go` | 工具执行链与中间件装配 |
| `src/main.go` | `Checker`、`Interceptor`、`SandboxMiddleware` 装配 |
