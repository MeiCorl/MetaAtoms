## §1 配置文件总览

### 路径说明

MetaAtoms 云端多用户模式支持两层配置文件,**用户级覆盖全局**(同名字段用户级优先):

| 层级 | 路径 | 适用场景 |
|------|------|---------|
| 全局 | `~/.metaatoms/setting.json` | 平台/管理员维护的默认配置(如默认模型、通用权限规则、所有用户都要用的 MCP);MetaAtoms 启动时加载 |
| 用户级 | `~/.metaatoms/${user_id}/setting.json` | 当前登录用户的配置(如用户级 MCP、用户级权限、个人压缩阈值);用户登录接入时加载 |

这里不存在原 CodePilot 的工区路径概念。用户的工作目录固定为 `~/.metaatoms/${user_id}`;UI 不展示也不要求用户选择工作路径。

同样的两层目录规则也适用于 `skills/`、`agents/`、`memory/`:

| 资源 | 全局路径 | 用户级路径 | 加载时机 |
|------|----------|------------|----------|
| setting | `~/.metaatoms/setting.json` | `~/.metaatoms/${user_id}/setting.json` | 全局启动加载;用户级登录加载 |
| skills | `~/.metaatoms/skills/` | `~/.metaatoms/${user_id}/skills/` | 全局启动加载;用户级登录加载 |
| agents | `~/.metaatoms/agents/` | `~/.metaatoms/${user_id}/agents/` | 全局启动加载;用户级登录加载 |
| memory | `~/.metaatoms/memory/` | `~/.metaatoms/${user_id}/memory/` | 全局可读基线;用户级自动生成和更新 |

### 合并规则

- **标量字段**(string / int / bool):用户级非零值覆盖全局;
- **对象/数组字段**(、`mcp.servers[]`):用户级数组**整体替换**全局数组,不做元素级合并;
- **省略字段**:沿用另一层(全局有就沿用全局,全局无则用用户级)。

### 覆盖优先级(从高到低)

```
用户级 setting.json  >  全局 setting.json  >  内置默认值
```

### 全局 vs 用户级 决策树

```
普通登录用户请求「帮我加个 MCP」「配个权限」       → 改用户级
用户提到「我的配置 / 我自己的 MCP / 我登录后」      → 改用户级
管理员提到「所有用户 / 全局 / 平台默认 / 启动基线」 → 讨论全局;普通用户不可直接编辑
用户未表态 + 单会话临时改即可                     → 用会话级授权或询问是否写入用户级
```

### 完整示例

```json
{
  "provider": "anthropic",
  "model": "claude-sonnet-4-20250514",
  "api_key": "sk-ant-xxx"
}
```

### 字段默认值与单位

所有字段均有内置默认值(详见各 section);`setting.json` 不存在的字段自动填默认。

### 是否需要重启

全局配置需要重启 MetaAtoms 进程才能重新加载。用户级配置在用户登录接入时加载;用户已在线时，除权限运行时追加外，通常需要该用户重新接入或服务重启才会完整生效。

### 错误排查

- 启动报错 `配置文件不存在: <path>` → 在该路径手动创建 `setting.json`,可复制 `config/setting.example.json` 作为起点;
- 启动报错 `解析配置文件失败(请检查 JSON 格式)` → 多半是 JSON 语法错(见 §11)。

---
