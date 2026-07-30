# UI / WebUI 交互 — MetaAtoms 实现原理

> 隶属 Step 1.1（UI 界面重构 TUI→WebUI）/ 1.2（对话栏富文本渲染）/ 1.3（WebUI 流式渲染）/ 1.4（WebUI 工具展示优化）| 架构层:第 1 层 交互层 | 核心入口:`src/main.go`

## §1 模块定位

MetaAtoms 的**主入口**(Step 1.1 由 Bubble Tea TUI 重构而来),位于 5 层架构的**第 1 层 交互层**,负责终端 UI 渲染、输入捕获、输出格式化、与 Agent Loop 的双向通信。

- **五区布局**:顶栏(状态/会话切换/权限档位) + 会话侧边栏 + 主对话区(流式 Markdown) + 输入区 + 右下角流式状态徽标
- **HTTP + WebSocket 双通道**:HTTP 提供嵌入静态资源,WS 提供实时双向业务消息
- **固定端口云端访问**:Web 服务监听全局 `setting.json.server_port` 指定端口(默认 8969),用户手动访问 `http://<server-ip>:<port>`
- **highlight.js v11** 代码块语法高亮 + 双栏 diff(Step 1.4 引入 `diff-match-patch`)
- **流式 Markdown 实时渲染** + 80ms 防抖合并 + 未闭合围栏自动补齐(Step 1.3)

## §2 核心数据结构

- `Server`(`src/interaction/web/server.go`)— 承载 HTTP 静态服务与 WS 升级入口,字段含 `addr / ready / wsMgr / router / httpSrv`。`addr` 来自全局 `setting.json.server_port`,默认 `0.0.0.0:8969`。
- `Router`(`src/interaction/web/router.go`)— 消息路由分发器,内部 `map[string]HandlerFunc + sync.RWMutex`;`HandleLoop`(`router.go`)是 WS 读循环 + 解码 + 分发三段式。
- `Handler`(`src/interaction/web/handler.go`)— 业务消息处理器,持 `conv / sessMgr / sp / stream / mu` 等字段,负责 `runStream` 流式编排、`handleSkills` / `handleResumeSession` / `handleNewSession` 等。
- `SlashCommandEntry`(`src/interaction/web/handler.go`)— web 层消费的最小命令投影;`SlashCommandProvider` 接口(`handler.go`)让 web 包不直接 import command/slash,由 main.go 适配注入。
- `ToolExecutionEvent`(`src/engine/conversation/tool_handler.go`)— 工具生命周期事件(`running/completed/error/aborted`),WebUI 据此渲染工具徽标 + 「查看改动」按钮。

## §3 关键流程

### 3.1 启动与手动访问

`Server.Start(ctx)`(`server.go`)流程:

1. `fs.Sub(staticFS, "static")` 提取 `//go:embed static`(`server.go`)的子目录作为静态资源根
2. `mux.Handle("/", http.FileServer(http.FS(staticSub)))` + `mux.HandleFunc(WSPath, s.wsMgr.HandleWS)` 装配双通道(`server.go`)
3. `net.Listen("tcp", wantAddr)` 监听 `0.0.0.0:<server_port>` 固定端口 → 成功后 `close(s.ready)` 通知上层(`server.go`)
4. `main.go` 在 `<-server.Ready()` 后打印访问提示,用户自行在浏览器访问 `http://<server-ip>:<server_port>`

[Why] 固定端口而非随机端口:**Why** Atoms 是云端多租户 Agent,服务地址需要稳定可访问,不能依赖本机随机端口 + 自动弹浏览器的单机交互模式。`Ready()` 通道仍保留,用于精确等待端口监听成功。

### 3.2 客户端 → WS 消息分发

`Router.HandleLoop(conn)`(`router.go`)流程:

1. `conn.ReadMessage()` 阻塞读 WS 消息 → 客户端 CloseNormal/GoingAway/Abnormal 静默 return
2. `Decode(data)` 反序列化为 `Message{Type, Payload}`(`router.go`)→ 失败回推 `stream_error(invalid_message)` 并 continue
3. `Route(conn, msg)` 按 `msg.Type` 查 `handlers` map(`router.go`)→ 未知类型回推 `stream_error(unknown_message_type)`

### 3.3 LLM 流式响应 → 浏览器

`runStream`(handler.go 内部)流程:

1. `provider.StreamChat(ctx, sp, messages, toolSpecs)` 拿到 `chunkCh`(Anthropic / OpenAI 流式响应,见 `src/llm/anthropic.go` / `src/llm/openai.go`)
2. 每个 chunk 触发 `hooks.OnStreamChunk`,handler 据此推送 WS 业务消息 `stream_chunk` 到前端
3. 流结束(`chunk.Done=true`)推 `stream_end` + `context_usage`(Step 7 引入)
4. 工具调用块(`ToolUseBlock`)触发 `tool_call_start`,执行结束触发 `tool_call_end`,前端据此渲染紫色「skill: <name>」徽标(Step 10 Task 6 落地,见 `src/skill/adapter/slash.go`)

### 3.4 双栏 diff(Step 1.4)

WriteFile/EditFile 工具头部渲染「查看改动」按钮 + 双栏 diff 弹窗:

- `tool.FileDiffStore`(`src/tool/file_diff.go`)是进程内 LRU 存储,`Set(toolUseID, FileDiffEntry{FilePath, Before, After})` + `Get(id)` 拉取
- WriteFile 的 `recordDiff`(`src/tool/builtin/write_file.go`)与 EditFile 的 `recordDiff`(`src/tool/builtin/edit_file.go`)在执行成功后把 before/after 写入 Store
- 前端在工具徽标点击时拉取 diff,经 `diff-match-patch` 做行级 diff 渲染(JS 侧实现,见 `src/interaction/web/static/`)

[Why] 把 diff 存进程内存而非磁盘:diff 是辅助 UI 展示数据,生命周期与会话一致;重新启动进程不会"残留"过时 diff 误导用户。

## §4 与其他模块的依赖

- **上游依赖**(交互层依赖它们):
  - `engine/conversation`(`src/engine/conversation/`)— `ConversationManager.RunAgentLoop` / `runStream` 由 handler 编排
  - `llm.Provider`(`src/llm/`)— Anthropic/OpenAI 双适配
  - `tool.Registry`(`src/tool/registry.go`)— 工具查找执行入口
  - `skill.Registry`(`src/skill/registry.go`)— 供 `handleSkills` 按 Source 分组列出(`src/interaction/web/handler.go`)
  - `slash.Registry`(`src/command/slash/command.go`)— 经 `slashAdapter`(`src/main.go`)投影为 `SlashCommandProvider`
- **下游被依赖**:浏览器(WebUI 前端 JS)+ 浏览器 DevTools(开发期)

## [Why] 五区布局 + 双通道的设计动机

[Why] 把 UI 拆为五区是为了**职责单一**:顶栏(状态) / 侧边栏(会话列表) / 主对话区(LLM 响应 + 工具块) / 输入区(用户输入) / 流式状态徽标(右下角 loading)。各区独立刷新,避免 LLM 流式响应时整页 re-render。[Why] HTTP+WS 双通道是**异步 + 实时**的最优解:HTTP 一次性提供静态资源(浏览器缓存友好),WS 单连接双向实时(适配 LLM 流式 ~20Hz chunk 频率)。

## §5 设计决策

### 决策 1:HTTP+WS 双通道

- **问题**:浏览器需要双向实时通信,但纯 HTTP 短轮询延迟高、长轮询资源消耗大
- **方案**:`http.FileServer` 提供静态资源,WebSocket 承载所有业务消息
- **理由**:HTTP 是浏览器天然支持的协议,静态资源零成本;WS 单连接双向实时,适配 LLM 流式响应与工具调用的细粒度事件

### 决策 2:全局固定端口 + `Ready()` 通道

- **问题**:云端服务需要稳定访问地址;`time.Sleep` 等端口 ready 存在竞态
- **方案**:全局 `setting.json.server_port` 配置固定端口,`net.Listen("tcp", "0.0.0.0:<port>")` 监听,`close(s.ready)` 通知上层
- **理由**:固定端口便于反向代理、负载均衡和用户手动访问;`Ready()` 是「事件驱动」语义,无 sleep 浪费也避免竞态

### 决策 3:WebUI 二次开发对接 + 静态资源 `embed.FS`

- **问题**:TUI(Bubble Tea)与终端耦合,IDE/移动端无法使用;静态资源分发需独立 web server
- **方案**:`//go:embed static`(`server.go`)把 `src/interaction/web/static/` 全部嵌入二进制,HTTP server 直接 `http.FileServer(http.FS(staticSub))`
- **理由**:**Why** 单一二进制可分发(无需附带 `dist/`);`embed.FS` 是 Go 1.16+ 标准库,零依赖

### 决策 4:slash 命令 `slashAdapter` 投影

- **问题**:web 包需要消费 slash 命令清单,但直接 import `command/slash` 会让交互层反向依赖命令层(架构分层违规)
- **方案**:web 包定义 `SlashCommandProvider` 最小接口(`handler.go`),main.go 顶层构造 `slashAdapter`(`src/main.go`)适配
- **理由**:层间仅通过接口交互(架构约束 #3);web 包不知道「命令」由谁实现,新增 Skill 命令时零改动 web

### 决策 5:流式 Markdown 80ms 防抖合并

- **问题**:LLM 流式 chunk 频率高(~20Hz),每个 chunk 都触发 DOM 更新会让浏览器卡顿
- **方案**:前端 JS 把同一帧内的多个 chunk 合并后渲染,80ms 防抖
- **理由**:**Why** 80ms 是人眼可感知阈值(约 12 FPS),低于此频率用户感知不到延迟;高于此频率无明显收益却浪费 CPU

## §6 关键文件索引

| 路径 | 角色 |
|------|------|
| `src/interaction/web/server.go` | `//go:embed static` 嵌入静态资源 |
| `src/interaction/web/server.go` | `Server.Start` 启动入口 |
| `src/interaction/web/server.go` | `close(s.ready)` 通知端口就绪 |
| `src/interaction/web/router.go` | `Router` 消息路由分发器 |
| `src/interaction/web/router.go` | `Router.HandleLoop` WS 读循环 |
| `src/interaction/web/handler.go` | `Handler` 业务消息处理器 |
| `src/interaction/web/handler.go` | `SlashCommandProvider` web 层投影接口 |
| `src/interaction/web/handler.go` | `handleSkills` Skill 列表响应 |
| `src/tool/file_diff.go` | `FileDiffStore` 进程内 LRU diff 存储(Step 1.4) |
| `src/tool/builtin/write_file.go` | `recordDiff` WriteFile diff 写入 |
| `src/tool/builtin/edit_file.go` | `recordDiff` EditFile diff 写入 |
| `src/main.go` | `slashAdapter` 投影 slash.Registry 为 web 接口 |
