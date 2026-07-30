## §10 改写工作流

### 通用五步流程

```
1. 读    → ReadFile <目标 setting.json>,确认存在并定位锚点
2. 定位  → 用 EditFile 时先 grep 找到要插入/替换的位置;WriteFile 时先 cat 全文
3. 改    → 构造完整 JSON 片段(单字段不要缺逗号、不要漏引号)
4. 写    → EditFile(增量)或 WriteFile(全量);WriteFile 前可用 ReadFile 备份
5. 验证  → 重启 MetaAtoms,按字段类型观察生效信号(见下方)
```

### 全局 vs 用户级 选择决策树

```
用户措辞                → 写入位置
─────────────────────────────────────
"我自己的 / 当前用户"     → ~/.metaatoms/${user_id}/setting.json
"帮我加个 MCP / 配权限"   → ~/.metaatoms/${user_id}/setting.json
"所有用户 / 平台默认"     → ~/.metaatoms/setting.json(仅管理员/部署维护)
"全局"                   → 确认是否为管理员维护全局基线;普通用户不编辑全局
未表态 + 模糊            → 默认用户级;涉及全局时必须询问并说明只读约束
```

不明确且可能写全局时**必须主动询问**,避免误写全局层级造成所有用户受影响。

### Removed permissions config


### 修改后如何验证生效

| 字段类型 | 验证信号 |
|----------|---------|
| `mcp.servers[]` | 重启后启动日志 `[mcp] connected: <name> healthy=ok tools=N` |
| `compaction.*` | 重启后观察 WebUI 头部 ctx 进度条 + 启动日志 |
| `memory.*` | 重启后 WebUI SP 可观测性面板中 `memory_index` 段 + MEMORY.md 注入行数 |
| `skill.*` | 重启后 WebUI `/skills` 列表变化(关闭后为空) |
| `tools.enabled` | 重启后 WebUI 工具下拉列表与 LLM 工具调用列表 |
| `hook.entries[]` | 重启后 WebUI 状态栏 hooks 子项、日志中的 hook 触发记录 |
| 顶层 LLM 参数 | 重启后 WebUI 头部 ctx 进度条按新 `context_window_size` 计算 |
| `model` / `api_key` | 重启后首次 LLM 请求成功 = 生效 |

### 是否需要重启(汇总)

- **需要重启或用户重新接入**: `mcp.*` / `hook.*` / `compaction.*` / `memory.*` / `skill.*` / `tools.*` / 顶层 LLM 参数

---
