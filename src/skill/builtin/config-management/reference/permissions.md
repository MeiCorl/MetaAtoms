## §3 permissions - 已移除的权限配置

MetaAtoms 当前不再支持 `permissions` 配置段。

旧版的 `permissions.mode`、`permissions.rules[]`、`allow / ask / deny`、HITL 权限确认和规则写回都已经从代码中移除。不要再向全局或用户级 `setting.json` 写入 `permissions` 字段。

## 当前安全边界

权限系统现在是固定行为：

- Bash 工具只保留硬编码危险命令黑名单。
- 用户可控的文件读写、搜索路径限制在当前用户目录内。
- 不存在可配置白名单、询问模式、权限模式或永久授权。
- WebUI 不再显示权限确认弹窗或权限模式下拉。

## 应该配置什么

如果目标是控制模型能看到哪些工具，请使用 `tools.enabled`。

如果目标是控制 MCP 工具暴露，请修改 `mcp.servers[]` 或配合 `tools.enabled` 限制工具列表。

如果目标是限制文件系统访问，不需要配置；路径边界由运行时固定在用户目录。

## 是否需要重启

删除旧 `permissions` 字段本身不影响运行。涉及 `tools.enabled`、`mcp.servers[]` 等配置调整时，按对应 reference 文档的重启要求处理。

## 迁移建议

- 从 `setting.json` 删除 `permissions` 字段。
- 不要把旧 HITL 写回规则迁移到其它字段。
- 原本用于禁用工具的规则，改用 `tools.enabled` 收窄可见工具集合。
