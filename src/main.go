// Package main 是 MetaAtoms 云端产品交付 Agent 的程序入口。
//
// 启动链路：
//  1. 初始化文件日志（失败不阻塞主流程）
//  2. 加载 ~/.metaatoms/setting.json 配置
//  3. 按配置构造 LLM Provider
//  4. 创建会话管理器（自动恢复最近一个会话）
//  5. 启动 HTTP + WebSocket 服务，监听固定端口；端口由全局 setting.json
//     的 server_port 配置，默认 8969
//  6. 通过 server.Ready() 等待 listen 完成后，在终端打印访问地址
//  7. 阻塞等待以下任一触发以进入退出流程：
//     - SIGINT / SIGTERM 信号（终端 Ctrl+C）
//     - Web 服务运行异常
//
// 退出流程：取消 runCtx → Server.Start 内部完成 Shutdown →
// 关闭 WebSocket 连接、关闭文件日志、返回进程退出码 0。
package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/gorilla/websocket"

	"github.com/metaatoms/metaatoms/src/auth"
	"github.com/metaatoms/metaatoms/src/command/slash"
	"github.com/metaatoms/metaatoms/src/config"
	"github.com/metaatoms/metaatoms/src/engine/conversation"
	"github.com/metaatoms/metaatoms/src/engine/prompt"
	"github.com/metaatoms/metaatoms/src/engine/prompt/sources"
	hookexecutor "github.com/metaatoms/metaatoms/src/hook/executor"
	"github.com/metaatoms/metaatoms/src/interaction/web"
	"github.com/metaatoms/metaatoms/src/llm"
	"github.com/metaatoms/metaatoms/src/logger"
	"github.com/metaatoms/metaatoms/src/mcp/adapter"
	mcpconfig "github.com/metaatoms/metaatoms/src/mcp/config"
	"github.com/metaatoms/metaatoms/src/mcp/session"
	"github.com/metaatoms/metaatoms/src/memory/autolearn"
	memctx "github.com/metaatoms/metaatoms/src/memory/context"
	memsession "github.com/metaatoms/metaatoms/src/memory/session"
	"github.com/metaatoms/metaatoms/src/security"
	"github.com/metaatoms/metaatoms/src/skill"
	skilladapter "github.com/metaatoms/metaatoms/src/skill/adapter"
	skillsources "github.com/metaatoms/metaatoms/src/skill/sources"
	"github.com/metaatoms/metaatoms/src/subagent/background"
	"github.com/metaatoms/metaatoms/src/subagent/definition"
	subagentruntime "github.com/metaatoms/metaatoms/src/subagent/runtime"
	subagenttool "github.com/metaatoms/metaatoms/src/subagent/tool"
	"github.com/metaatoms/metaatoms/src/tool"
	// import 触发 builtin 包的 init()，将 5 个内置工具以 cwd + 30s 兜底
	// 注册到 tool.DefaultRegistry()；main 随后按 cfg 调
	// builtin.RegisterWithOptions 用 cfg 中的工作目录/超时覆盖默认实例。
	"github.com/metaatoms/metaatoms/src/tool/builtin"
)

const (
	// defaultMaxRounds 为兼容旧构造链保留的历史参数；当前不再用于裁剪上下文。
	defaultMaxRounds = 50
	// tenantRuntimeRefreshInterval controls proactive hot reload checks for
	// active tenant runtimes.
	tenantRuntimeRefreshInterval = 30 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "[error]", err)
		os.Exit(1)
	}
}

// ---- Slash 命令注册表适配器（Step 9.1 Task 6） ----
//
// [Why 需要 adapter] web.Handler 通过 SlashCommandProvider 接口（List + OnChange）消费
// slash 命令清单；*slash.Registry 本身提供的方法签名（List 返回 []SlashCommand 接口切片、
// OnChange 入参 func()）与 web.SlashCommandProvider 接口（List 返回 []SlashCommandEntry
// 具体切片、OnChange 入参 func()）**形状不同**——web.SlashCommandEntry 是 handler 包内
// 私有 struct，slash.Registry 自然不能直接返回该类型。因此在 main.go 顶层做一次薄包装：
//   - adapter 持有 *slash.Registry 引用
//   - List() 把 []SlashCommand 转 []SlashCommandEntry（5 字段投影）
//   - OnChange() 透传 registry.OnChange
//
// 这样 web 包只知道"拿到的是 web.SlashCommandEntry 列表"，不感知 slash 包的存在，
// 维持 spec.md 中"web → slash 单向依赖"的方向约束（slash.builtin 持 *web.Handler
// 引用但 web 包不 import slash 包）。

// slashAdapter 把 *slash.Registry 适配为 web.SlashCommandProvider 接口。
// 单一职责：字段投影 + 回调透传，零业务逻辑。
type slashAdapter struct {
	registry *slash.Registry
}

// newSlashAdapter 构造一个把 *slash.Registry 适配为 web.SlashCommandProvider 的实例。
// 参数：
//   - registry：slash 命令注册中心指针；为 nil 时 List 返回空切片、OnChange 为 no-op
//
// 返回值：*slashAdapter 指针，满足 web.SlashCommandProvider 接口约束。
func newSlashAdapter(registry *slash.Registry) *slashAdapter {
	return &slashAdapter{registry: registry}
}

// List 返回当前所有已注册命令的 web.SlashCommandEntry 投影（按 Registry 注册顺序）。
// 实现 web.SlashCommandProvider.List 签名要求。
func (a *slashAdapter) List() []web.SlashCommandEntry {
	if a.registry == nil {
		return nil
	}
	cmds := a.registry.List()
	if len(cmds) == 0 {
		return []web.SlashCommandEntry{}
	}
	entries := make([]web.SlashCommandEntry, 0, len(cmds))
	for _, c := range cmds {
		entries = append(entries, web.SlashCommandEntry{
			Name:        c.Name(),
			Description: c.Description(),
			NeedsArg:    c.NeedsArg(),
			ArgHint:     c.ArgHint(),
			Category:    c.Category(),
		})
	}
	return entries
}

// OnChange 透传 slash.Registry.OnChange；handler 注入后注册一个"命令清单变化"回调，
// 用于在 Step 10 Skill 动态注册场景下推 slash_commands_updated。
func (a *slashAdapter) OnChange(fn func()) {
	if a.registry == nil {
		return
	}
	a.registry.OnChange(fn)
}

// Execute 按 name 查找 slash 命令并执行。
//
// Step 10 引入：覆盖 Skill 系统的 /<skill-name> 等无专属 MsgType 的命令。
// 实现 web.SlashCommandProvider.Execute 签名要求。
//
// 行为：
//   - registry 为 nil → 返回 error("slash registry not set")，由 handler 包成
//     stream_error 回推前端；
//   - registry 中找不到 name → 返回 error("slash command not found: <name>")，
//     同上回推；
//   - 命中 → 调 cmd.Execute(ctx, conn, arg)，Execute 自身负责业务（如 Skill
//     注入 LeadUserMessage）；Execute 返回的 error 也透传，由 handler 包装。
//
// [Why 透传到 registry] slash.Registry 已经实现了 thread-safe 的 Get + cmd.Execute
// 路径（Get 持读锁、Execute 自带 ctx 取消检查），adapter 无需再加锁或包装；
// 单一职责：web → registry 转发。
func (a *slashAdapter) Execute(ctx context.Context, conn *websocket.Conn, name, arg string) error {
	if a.registry == nil {
		return fmt.Errorf("slash registry not set")
	}
	cmd, ok := a.registry.Get(name)
	if !ok {
		return fmt.Errorf("slash command not found: %s", name)
	}
	return cmd.Execute(ctx, conn, arg)
}

// ---- Skill 注册表适配器（Step 10 Task 6） ----
//
// [Why 需要 adapter] web.Handler 通过 SkillProvider 接口（List + ListBySource）
// 消费 Skill 清单；*skill.Registry 暴露的 List/ListBySource 返回 *skill.Skill，
// 包含 Source 枚举（int）等内部细节，web 包不能直接依赖（避免 import cycle
// 与分层倒挂）。因此在 main.go 顶层做一次薄包装：
//   - adapter 持有 *skill.Registry 引用
//   - List() 把 []*skill.Skill 投影为 []web.SkillEntry（4 字段投影）
//   - ListBySource(source) 按 source 字符串过滤后投影
//
// web 包只知道"拿到的是 web.SkillEntry 列表"，不感知 skill 包的存在。
// 与 slashAdapter 的设计模式完全一致：web → 单向消费上层数据，避免 web → skill
// 反向 import 链路。

// skillProviderAdapter 把 *skill.Registry 适配为 web.SkillProvider 接口。
// 单一职责：字段投影（*skill.Skill → web.SkillEntry），零业务逻辑。
type skillProviderAdapter struct {
	registry *skill.Registry
}

// newSkillProviderAdapter 构造一个把 *skill.Registry 适配为 web.SkillProvider 的实例。
// 参数：
//   - registry：Skill 注册中心指针；为 nil 时 List / ListBySource 返回空切片
//
// 返回值：*skillProviderAdapter 指针，满足 web.SkillProvider 接口约束。
func newSkillProviderAdapter(registry *skill.Registry) *skillProviderAdapter {
	return &skillProviderAdapter{registry: registry}
}

// skillToEntry 把 *skill.Skill 投影为 web.SkillEntry（4 字段）。
// Source 字段走 skill.Source.String() 字符串投影（与前端 SkillsListPayload
// 的三档数组对应：project / user / builtin）。
// registry 为 nil 时返回 nil 切片；ListBySource source 不识别时同样返回 nil。
func (a *skillProviderAdapter) skillToEntry(s *skill.Skill) web.SkillEntry {
	if s == nil {
		return web.SkillEntry{}
	}
	return web.SkillEntry{
		Name:        s.Name,
		Description: s.Description,
		Source:      s.Source.String(),
		Path:        s.RootPath,
	}
}

// List 返回所有已加载 Skill 的扁平投影列表（按 Registry 注册顺序）。
// 实际按 Source 顺序（项目级 → 用户级 → 内置级）排列，由 Registry.List 内部保证。
// 实现 web.SkillProvider.List 签名要求。registry 为 nil 时返回 nil 切片。
func (a *skillProviderAdapter) List() []web.SkillEntry {
	if a.registry == nil {
		return nil
	}
	skills := a.registry.List()
	if len(skills) == 0 {
		return []web.SkillEntry{}
	}
	out := make([]web.SkillEntry, 0, len(skills))
	for _, s := range skills {
		out = append(out, a.skillToEntry(s))
	}
	return out
}

// ListBySource 按 source 字符串（"project" / "user" / "builtin"）返回该档下的
// Skill 投影列表，按 Registry 注册顺序。未识别的 source 返回 nil 切片。
// 实现 web.SkillProvider.ListBySource 签名要求。
func (a *skillProviderAdapter) ListBySource(source string) []web.SkillEntry {
	if a.registry == nil {
		return nil
	}
	var src skill.Source
	switch source {
	case "project":
		src = skill.SourceProject
	case "user":
		src = skill.SourceUser
	case "builtin":
		src = skill.SourceBuiltin
	default:
		// 防御性：handler 端约定只传 "project" / "user" / "builtin"；
		// 收到其他值时返回 nil（不暴露给前端错误状态，list_skills payload
		// 退化为单档空数组，前端 tab 列表为空）。
		return nil
	}
	skills := a.registry.ListBySource(src)
	if len(skills) == 0 {
		return []web.SkillEntry{}
	}
	out := make([]web.SkillEntry, 0, len(skills))
	for _, s := range skills {
		out = append(out, a.skillToEntry(s))
	}
	return out
}

// ---- Skill 注入适配器（Step 10 Task 7） ----
//
// [Why 需要 adapter] Skill 适配器包定义的 LeadMessageInjector 接口（InjectLeadUserMessage
// 方法）要求把 Skill 完整内容 + 可选 <user_args> 段写入 *ConversationManager.leadUserMessage，
// 但 adapter 包不能直接 import engine/conversation（避免反向依赖）。*web.Handler
// 是唯一直接持有 *ConversationManager 的层（assembleSP 在内部直接访问 h.conv），
// Step 10 Task 7 在 web 包内增加 Handler.InjectLeadUserMessage 导出方法包装
// h.conv.SetLeadUserMessage；main.go 顶层把 *web.Handler 适配为 LeadMessageInjector：
//   - leadInjectorAdapter 持有 *web.Handler 引用；
//   - InjectLeadUserMessage 转发到 handler.InjectLeadUserMessage。
//
// 这样 Skill 适配器（slash 子包）只依赖接口、不感知 conversation 包或 web 包
// 的具体实现（adapter 看到的接口只声明 InjectLeadUserMessage 方法，main.go
// 顶层实现负责 wire）。维持 spec.md「slash 不依赖 web/conversation」的边界约束。
type leadInjectorAdapter struct {
	h *web.Handler
}

// newLeadInjectorAdapter 构造一个把 *web.Handler 适配为
// skilladapter.LeadMessageInjector 的实例。
// 参数：
//   - h：web.Handler 指针；为 nil 时 InjectLeadUserMessage 直接返回 nil（降级）
func newLeadInjectorAdapter(h *web.Handler) *leadInjectorAdapter {
	return &leadInjectorAdapter{h: h}
}

// InjectLeadUserMessage 把 content（含 Skill 完整正文与 <user_args> 段）写入
// 对话管理器的 leadUserMessage 字段，由 GetContext 在窗口派生结果前拼到 messages 最前。
//
// [Why 写入 leadUserMessage] 与 spec §B.3 对齐：用户触发 /<skill> 时 Skill 完整内容
// 应作为 LeadUserMessage 注入到下一轮 user 消息头部，LLM 端据此理解 Skill 工作流。
// leadUserMessage 字段是会话级一次性注入（Step 4 Task 5 已实现），由 prompt.Builder
// 在每次会话启动时重置。
func (a *leadInjectorAdapter) InjectLeadUserMessage(content, _ string) error {
	if a == nil || a.h == nil {
		return nil
	}
	return a.h.InjectLeadUserMessage(content)
}

// hookSubAgentAdapter 把 Step 12 的 definitions + runtime.Runner 适配为 hook/executor
// 需要的窄接口。这样 hook 包不直接 import subagent/runtime，避免形成循环依赖。
type hookSubAgentAdapter struct {
	definitions *definition.Registry
	runner      *subagentruntime.Runner
}

func ensureEnabledTools(enabled []string, names ...string) []string {
	if len(enabled) == 0 || len(names) == 0 {
		return enabled
	}
	seen := make(map[string]struct{}, len(enabled)+len(names))
	out := append([]string(nil), enabled...)
	for _, name := range enabled {
		seen[name] = struct{}{}
	}
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func newHookSubAgentAdapter(defs *definition.Registry, runner *subagentruntime.Runner) hookexecutor.AgentSubAgentRunner {
	if defs == nil || runner == nil {
		return nil
	}
	return &hookSubAgentAdapter{definitions: defs, runner: runner}
}

func (a *hookSubAgentAdapter) RunHookAgent(ctx context.Context, req hookexecutor.HookAgentRequest) (hookexecutor.HookAgentResult, error) {
	if a == nil || a.definitions == nil || a.runner == nil {
		return hookexecutor.HookAgentResult{}, fmt.Errorf("hook subagent adapter: not configured")
	}
	role := definition.NormalizeName(req.Role)
	if role == "" {
		role = "explore"
	}
	def, ok := a.definitions.Get(role)
	if !ok {
		return hookexecutor.HookAgentResult{}, fmt.Errorf("hook subagent adapter: role %q not found", role)
	}
	if req.MaxIterations > 0 {
		def.MaxTurns = req.MaxIterations
	}
	if req.AllowToolsSet {
		def.AllowedTools = append([]string(nil), req.AllowTools...)
	}
	result, err := a.runner.RunDefined(ctx, subagentruntime.DefinedRunRequest{
		Definition: def,
		Task:       req.Prompt,
		Metadata:   req.Metadata,
		Background: false,
		MaxTurns:   req.MaxIterations,
	})
	if result == nil {
		return hookexecutor.HookAgentResult{}, err
	}
	return hookexecutor.HookAgentResult{
		Role:       result.RoleName,
		Status:     result.Status,
		StopReason: string(result.StopReason),
		FinalText:  result.FinalText,
		Iterations: result.Iterations,
		ToolCalls:  result.ToolCalls,
		Error:      result.Error,
	}, err
}

// findActiveBuiltinRoot 计算 builtin Skill 的「实际可读文件系统根目录」绝对路径,
// 供 use_skill 工具在返回时前置「Skill 根路径提示」,让 LLM 知道怎么用 ReadFile
// 拼出 reference/*.md 等子文档的绝对路径(Step 10.2 Bugfix)。
//
// 优先级(与 skill.LoadAll 的三段式 fallback 同构):
//  1. exeDir 副本路径:<execDir>/skill/builtin/(dist 模式 binary 旁的副本)
//  2. workdir 向上 16 级找 src/skill/builtin/(dev 模式 / 非 dist 部署)
//
// 两段是「或」关系,任一段成功即可;两段都失败返回空字符串(此时 use_skill 会告诉
// LLM「embedded-only,无 filesystem 副本」)。
//
// 防御性:任一候选路径要求至少有一个子目录含 SKILL.md(scanner.findSrcBuiltinFallback
// 的同款防御,避免空目录误命中)。
//
// [Why 不复用 scanner.findSrcBuiltinFallback] findSrcBuiltinFallback 是 unexported,
// 且它只向上找 src/ 路径,不含 execDir 副本分支;这里需要两个分支同时考虑,
// 故单独实现一个 main.go 顶层 helper。
//
// [Why 返回绝对路径而非相对路径] use_skill 提示里的路径是给 LLM 直接 ReadFile 用的,
// LLM 用 ReadFile 时传入相对路径会以 sandboxDir(workdir)为基准,可能导致路径越界,
// 绝对路径更安全。
func findActiveBuiltinRoot(workdir, execDir string) string {
	// 1. exeDir 副本优先(dist 模式)
	if execDir != "" {
		candidate := filepath.Join(execDir, "skill", "builtin")
		if hasBuiltinSkillMDAt(candidate) {
			if abs, err := filepath.Abs(candidate); err == nil {
				return abs
			}
		}
	}
	// 2. workdir 向上 16 级找 src/(dev 模式 / 非 dist 部署)
	if workdir != "" {
		absWD, err := filepath.Abs(workdir)
		if err == nil {
			cur := absWD
			for i := 0; i < 16; i++ {
				candidate := filepath.Join(cur, "src", "skill", "builtin")
				if hasBuiltinSkillMDAt(candidate) {
					return candidate
				}
				parent := filepath.Dir(cur)
				if parent == cur {
					break
				}
				cur = parent
			}
		}
	}
	return ""
}

// hasBuiltinSkillMDAt 校验 candidate 目录下是否至少存在一个子目录含 SKILL.md。
//
// 与 scanner.findSrcBuiltinFallback 的同名防御逻辑一致:避免「目录存在但没有 SKILL.md」
// 时误命中(如某些项目里 src/skill/builtin 目录里只是 README.md 占位)。
func hasBuiltinSkillMDAt(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(dir, e.Name(), "SKILL.md")
		if _, ferr := os.Stat(skillPath); ferr == nil {
			return true
		}
	}
	return false
}

// run 是主流程入口；返回 error 表示启动或运行过程中发生不可恢复错误。
// 拆出独立函数便于在测试中调用（虽然 step1.1 暂未引入 main 测试）。
type tenantRuntime struct {
	router                  *web.Router
	handler                 *web.Handler
	fingerprint             string
	projectTreeFingerprints map[string]string
	cancel                  context.CancelFunc
	mcpPool                 *session.Pool
	closeOnce               sync.Once
}

type tenantManager struct {
	baseDir string
	connMgr *web.ConnectionManager
	mu      sync.Mutex
	items   map[string]*tenantRuntime
	conns   map[string]map[*websocket.Conn]struct{}
}

func newTenantManager(baseDir string, connMgr *web.ConnectionManager) *tenantManager {
	return &tenantManager{
		baseDir: baseDir,
		connMgr: connMgr,
		items:   make(map[string]*tenantRuntime),
		conns:   make(map[string]map[*websocket.Conn]struct{}),
	}
}

func (rt *tenantRuntime) Close() {
	if rt == nil {
		return
	}
	rt.closeOnce.Do(func() {
		if rt.cancel != nil {
			rt.cancel()
		}
		if rt.mcpPool != nil {
			_ = rt.mcpPool.CloseAll(context.Background())
		}
	})
}

func (m *tenantManager) CloseAll() {
	m.mu.Lock()
	items := make([]*tenantRuntime, 0, len(m.items))
	for _, rt := range m.items {
		items = append(items, rt)
	}
	conns := make([]*websocket.Conn, 0)
	for _, byUser := range m.conns {
		for conn := range byUser {
			conns = append(conns, conn)
		}
	}
	m.items = make(map[string]*tenantRuntime)
	m.conns = make(map[string]map[*websocket.Conn]struct{})
	m.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
	for _, rt := range items {
		rt.Close()
	}
}

func (m *tenantManager) RouterForUser(userID string) (*web.Router, func(*websocket.Conn), func(*websocket.Conn), error) {
	if _, err := m.runtimeForUser(userID); err != nil {
		return nil, nil, nil, err
	}
	router := web.NewDynamicRouter(func(conn *websocket.Conn, msg web.Message) (*web.Router, error) {
		before := m.cachedFingerprint(userID)
		beforeProjectTrees := m.cachedProjectTreeFingerprints(userID)
		latest, err := m.runtimeForUser(userID)
		if err != nil {
			return nil, err
		}
		if before != "" && before != latest.fingerprint {
			latest.handler.PushSlashCommandsUpdated(conn)
			latest.handler.BroadcastMCPStatus()
			if changedScopes, err := m.refreshProjectTreeFingerprints(userID, latest, beforeProjectTrees); err != nil {
				logger.Warn("project tree refresh on websocket message failed",
					zap.String("user_id", userID),
					zap.Error(err),
				)
			} else if len(changedScopes) > 0 {
				latest.handler.BroadcastProjectTreeUpdated(changedScopes)
			}
		}
		return latest.router, nil
	})
	onOpen := func(conn *websocket.Conn) {
		m.trackConnectionOpen(userID, conn)
		latest, err := m.runtimeForUser(userID)
		if err != nil {
			logger.Warn("tenant runtime refresh on websocket open failed",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			return
		}
		latest.handler.PushSlashCommandsOnOpen(conn)
	}
	onClose := func(conn *websocket.Conn) {
		m.trackConnectionClose(userID, conn)
	}
	return router, onOpen, onClose, nil
}

func (m *tenantManager) trackConnectionOpen(userID string, conn *websocket.Conn) {
	if userID == "" || conn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	byUser := m.conns[userID]
	if byUser == nil {
		byUser = make(map[*websocket.Conn]struct{})
		m.conns[userID] = byUser
	}
	byUser[conn] = struct{}{}
}

func (m *tenantManager) trackConnectionClose(userID string, conn *websocket.Conn) {
	if userID == "" || conn == nil {
		return
	}
	var rt *tenantRuntime
	m.mu.Lock()
	if byUser := m.conns[userID]; byUser != nil {
		delete(byUser, conn)
		if len(byUser) == 0 {
			delete(m.conns, userID)
			rt = m.items[userID]
			delete(m.items, userID)
		}
	}
	m.mu.Unlock()
	if rt != nil {
		rt.Close()
		logger.Info("tenant runtime closed after last websocket disconnected", zap.String("user_id", userID))
	}
}

func (m *tenantManager) CloseUser(userID string) {
	if userID == "" {
		return
	}
	var rt *tenantRuntime
	var conns []*websocket.Conn
	m.mu.Lock()
	rt = m.items[userID]
	delete(m.items, userID)
	if byUser := m.conns[userID]; byUser != nil {
		conns = make([]*websocket.Conn, 0, len(byUser))
		for conn := range byUser {
			conns = append(conns, conn)
		}
		delete(m.conns, userID)
	}
	m.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
	if rt != nil {
		rt.Close()
		logger.Info("tenant runtime closed for user", zap.String("user_id", userID))
	}
}

func (m *tenantManager) StartAutoRefresh(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = tenantRuntimeRefreshInterval
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.refreshCachedRuntimes()
			}
		}
	}()
}

func (m *tenantManager) refreshCachedRuntimes() {
	users := m.cachedRuntimeUsers()
	for _, userID := range users {
		before := m.cachedFingerprint(userID)
		beforeProjectTrees := m.cachedProjectTreeFingerprints(userID)
		rt, err := m.runtimeForUser(userID)
		if err != nil {
			logger.Warn("tenant runtime auto refresh failed",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			continue
		}
		if before != "" && rt != nil && rt.fingerprint != before {
			m.pushSlashCommandsUpdated(userID, rt)
			rt.handler.BroadcastMCPStatus()
		}
		if rt != nil {
			changedScopes, err := m.refreshProjectTreeFingerprints(userID, rt, beforeProjectTrees)
			if err != nil {
				logger.Warn("project tree auto refresh fingerprint failed",
					zap.String("user_id", userID),
					zap.Error(err),
				)
				continue
			}
			if len(changedScopes) > 0 {
				rt.handler.BroadcastProjectTreeUpdated(changedScopes)
			}
		}
	}
}

func (m *tenantManager) cachedRuntimeUsers() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	users := make([]string, 0, len(m.items))
	for userID := range m.items {
		users = append(users, userID)
	}
	return users
}

func (m *tenantManager) pushSlashCommandsUpdated(userID string, rt *tenantRuntime) {
	if rt == nil || rt.handler == nil {
		return
	}
	conns := m.connectionsForUser(userID)
	for _, conn := range conns {
		rt.handler.PushSlashCommandsUpdated(conn)
	}
}

func (m *tenantManager) connectionsForUser(userID string) []*websocket.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	byUser := m.conns[userID]
	if len(byUser) == 0 {
		return nil
	}
	conns := make([]*websocket.Conn, 0, len(byUser))
	for conn := range byUser {
		conns = append(conns, conn)
	}
	return conns
}

func (m *tenantManager) runtimeForUser(userID string) (*tenantRuntime, error) {
	m.mu.Lock()
	var oldToClose *tenantRuntime
	defer func() {
		m.mu.Unlock()
		if oldToClose != nil {
			oldToClose.Close()
			logger.Info("tenant runtime hot reloaded", zap.String("user_id", userID))
		}
	}()
	fingerprint, fpErr := m.fingerprintForUser(userID)
	if rt := m.items[userID]; rt != nil && fpErr != nil {
		logger.Warn("tenant runtime fingerprint failed; keeping cached runtime",
			zap.String("user_id", userID),
			zap.Error(fpErr),
		)
		return rt, nil
	}
	if rt := m.items[userID]; rt != nil && rt.fingerprint == fingerprint {
		return rt, nil
	}
	if fpErr != nil {
		return nil, fpErr
	}
	old := m.items[userID]
	rt, err := m.buildRuntime(userID, fingerprint)
	if err != nil {
		if old != nil {
			logger.Warn("tenant runtime hot reload failed; keeping cached runtime",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			return old, nil
		}
		return nil, err
	}
	m.items[userID] = rt
	if old != nil {
		oldToClose = old
	}
	return rt, nil
}

func (m *tenantManager) cachedFingerprint(userID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt := m.items[userID]; rt != nil {
		return rt.fingerprint
	}
	return ""
}

func (m *tenantManager) cachedProjectTreeFingerprints(userID string) map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt := m.items[userID]; rt != nil && len(rt.projectTreeFingerprints) > 0 {
		out := make(map[string]string, len(rt.projectTreeFingerprints))
		for scope, fp := range rt.projectTreeFingerprints {
			out[scope] = fp
		}
		return out
	}
	return nil
}

func (m *tenantManager) refreshProjectTreeFingerprints(userID string, rt *tenantRuntime, before map[string]string) ([]string, error) {
	if rt == nil {
		return nil, nil
	}
	userDir := auth.UserDir(m.baseDir, userID)
	latest, err := projectTreeFingerprintsForUser(userDir)
	if err != nil {
		return nil, err
	}
	scopes := []string{web.ProjectFileScopeWorkspace, web.ProjectFileScopeSetting}
	changed := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if before[scope] != "" && latest[scope] != before[scope] {
			changed = append(changed, scope)
		}
	}
	m.mu.Lock()
	if current := m.items[userID]; current != nil {
		current.projectTreeFingerprints = latest
	}
	m.mu.Unlock()
	return changed, nil
}

func (m *tenantManager) fingerprintForUser(userID string) (string, error) {
	userDir := auth.UserDir(m.baseDir, userID)
	return fingerprintPaths([]string{
		filepath.Join(m.baseDir, "setting.json"),
		filepath.Join(userDir, "setting.json"),
		filepath.Join(m.baseDir, "skills"),
		filepath.Join(userDir, "skills"),
		filepath.Join(m.baseDir, "agents"),
		filepath.Join(userDir, "agents"),
	})
}

func fingerprintPaths(paths []string) (string, error) {
	h := sha256.New()
	for _, path := range paths {
		if err := fingerprintPath(h, path); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fingerprintPath(w io.Writer, path string) error {
	clean := filepath.Clean(path)
	info, err := os.Stat(clean)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(w, "missing:%s\n", filepath.ToSlash(clean))
			return nil
		}
		return fmt.Errorf("fingerprint stat %s: %w", clean, err)
	}
	if !info.IsDir() {
		return fingerprintFile(w, clean, clean, info)
	}
	return filepath.WalkDir(clean, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("fingerprint walk %s: %w", path, walkErr)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("fingerprint info %s: %w", path, err)
		}
		if info.IsDir() {
			rel, _ := filepath.Rel(clean, path)
			_, _ = fmt.Fprintf(w, "dir:%s\n", filepath.ToSlash(filepath.Join(clean, rel)))
			return nil
		}
		if !info.Mode().IsRegular() {
			_, _ = fmt.Fprintf(w, "skip:%s:%s\n", filepath.ToSlash(path), info.Mode().String())
			return nil
		}
		return fingerprintFile(w, clean, path, info)
	})
}

func fingerprintFile(w io.Writer, root, path string, info os.FileInfo) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	_, _ = fmt.Fprintf(w, "file:%s:%d:%d\n",
		filepath.ToSlash(filepath.Join(root, rel)),
		info.Size(),
		info.ModTime().UnixNano(),
	)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("fingerprint open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("fingerprint read %s: %w", path, err)
	}
	_, _ = io.WriteString(w, "\n")
	return nil
}

func projectTreeFingerprintsForUser(userDir string) (map[string]string, error) {
	settingFP, err := fingerprintPathsMetadata([]string{
		filepath.Join(userDir, "setting.json"),
		filepath.Join(userDir, "skills"),
		filepath.Join(userDir, "agents"),
		filepath.Join(userDir, "memory"),
	})
	if err != nil {
		return nil, err
	}
	workspaceFP, err := fingerprintPathsMetadata([]string{
		filepath.Join(userDir, "workspace"),
	})
	if err != nil {
		return nil, err
	}
	return map[string]string{
		web.ProjectFileScopeWorkspace: workspaceFP,
		web.ProjectFileScopeSetting:   settingFP,
	}, nil
}

func fingerprintPathsMetadata(paths []string) (string, error) {
	h := sha256.New()
	for _, path := range paths {
		if err := fingerprintPathMetadata(h, path); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fingerprintPathMetadata(w io.Writer, path string) error {
	clean := filepath.Clean(path)
	info, err := os.Stat(clean)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(w, "missing:%s\n", filepath.ToSlash(clean))
			return nil
		}
		return fmt.Errorf("fingerprint stat %s: %w", clean, err)
	}
	if !info.IsDir() {
		fingerprintFileMetadata(w, clean, clean, info)
		return nil
	}
	return filepath.WalkDir(clean, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("fingerprint walk %s: %w", path, walkErr)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("fingerprint info %s: %w", path, err)
		}
		if info.IsDir() {
			rel, _ := filepath.Rel(clean, path)
			_, _ = fmt.Fprintf(w, "dir:%s:%d\n",
				filepath.ToSlash(filepath.Join(clean, rel)),
				info.ModTime().UnixNano(),
			)
			return nil
		}
		if !info.Mode().IsRegular() {
			_, _ = fmt.Fprintf(w, "skip:%s:%s:%d\n", filepath.ToSlash(path), info.Mode().String(), info.ModTime().UnixNano())
			return nil
		}
		fingerprintFileMetadata(w, clean, path, info)
		return nil
	})
}

func fingerprintFileMetadata(w io.Writer, root, path string, info os.FileInfo) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	_, _ = fmt.Fprintf(w, "file:%s:%d:%d\n",
		filepath.ToSlash(filepath.Join(root, rel)),
		info.Size(),
		info.ModTime().UnixNano(),
	)
}

func buildTenantSkillRootBySource(userDir, globalDir, execDir string) map[skill.Source]string {
	rootBySource := make(map[skill.Source]string, 3)
	rootBySource[skill.SourceBuiltin] = findActiveBuiltinRoot(userDir, execDir)
	if globalDir != "" {
		rootBySource[skill.SourceUser] = filepath.Join(globalDir, "skills")
	}
	if userDir != "" {
		rootBySource[skill.SourceProject] = filepath.Join(userDir, "skills")
	}
	return rootBySource
}

func buildTenantMCPPool(cfg *config.Config) (*session.Pool, *mcpconfig.BuildResult) {
	if cfg == nil || len(cfg.MCP.Servers) == 0 {
		return nil, nil
	}
	mcpBuild := mcpconfig.BuildTransports(cfg, logger.L())
	if len(mcpBuild.PoolConfigs) == 0 {
		return nil, mcpBuild
	}
	for i := range mcpBuild.PoolConfigs {
		if f, ok := mcpBuild.ReconnectFactory[mcpBuild.PoolConfigs[i].Name]; ok {
			mcpBuild.PoolConfigs[i].ReconnectFactory = f
		}
	}
	if len(mcpBuild.Skipped) > 0 {
		logger.Warn("tenant MCP server skipped", zap.Int("count", len(mcpBuild.Skipped)))
		for name, reason := range mcpBuild.Skipped {
			logger.Warn("tenant MCP skipped detail", zap.String("server", name), zap.String("reason", reason))
		}
	}
	return session.NewPool(logger.L()), mcpBuild
}

func startTenantMCP(ctx context.Context, mcpPool *session.Pool, mcpBuild *mcpconfig.BuildResult, toolRegistry *tool.Registry, handler *web.Handler) {
	if mcpPool == nil || mcpBuild == nil || len(mcpBuild.PoolConfigs) == 0 || toolRegistry == nil || handler == nil {
		return
	}
	poolConfigs := mcpBuild.PoolConfigs
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("tenant MCP background init panic recovered",
					zap.Any("panic", r),
					zap.String("stack", string(debug.Stack())),
				)
			}
		}()

		initCtx, initCancel := context.WithTimeout(context.Background(), 60*time.Second)
		if err := mcpPool.InitializeAll(initCtx, poolConfigs); err != nil {
			logger.Warn("tenant MCP pool init returned error", zap.Error(err))
		}
		initCancel()

		healthy := mcpPool.HealthyNames()
		logger.Info("tenant MCP pool started",
			zap.Int("healthy", len(healthy)),
			zap.Strings("healthy_names", healthy),
			zap.Int("unhealthy", len(mcpBuild.Skipped)+len(mcpPool.Unhealthy())),
		)
		if ctx.Err() != nil {
			return
		}

		regCtx, regCancel := context.WithTimeout(context.Background(), 15*time.Second)
		stats, regErr := adapter.RegisterAll(regCtx, mcpPool, toolRegistry, logger.L())
		regCancel()
		if regErr != nil {
			logger.Warn("tenant MCP tool registration failed", zap.Error(regErr))
		} else if stats != nil {
			logger.Info("tenant MCP tools registered",
				zap.Int("tools", stats.ToolsRegistered),
				zap.Int("servers", stats.ServersProcessed),
				zap.Int("skipped", stats.SkippedDuplicate),
			)
		}

		if ctx.Err() == nil {
			handler.BroadcastMCPStatus()
		}
	}()
}

func (m *tenantManager) buildRuntime(userID, fingerprint string) (*tenantRuntime, error) {
	userDir := auth.UserDir(m.baseDir, userID)
	if err := ensureTenantDirs(userDir); err != nil {
		return nil, err
	}
	cfg, err := config.LoadMerged(filepath.Join(m.baseDir, "setting.json"), filepath.Join(userDir, "setting.json"))
	if err != nil {
		return nil, err
	}
	cfg.ToolWorkingDirectory = userDir
	provider, err := llm.NewProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("init LLM provider: %w", err)
	}

	sessMgr, err := memsession.NewSessionManagerWithDir(filepath.Join(userDir, "sessions"), userDir)
	if err != nil {
		return nil, fmt.Errorf("create session manager: %w", err)
	}
	toolRegistry := tool.NewRegistry()
	bashTimeout := time.Duration(cfg.ToolExecutionTimeoutSeconds) * time.Second
	builtin.RegisterWithOptions(toolRegistry, userDir, bashTimeout)
	fileDiffStore := web.NewFileDiffStore()
	if wfTool, ok := toolRegistry.Get(builtin.WriteFileName); ok {
		if wf, ok := wfTool.(*builtin.WriteFileTool); ok {
			wf.SetDiffSink(fileDiffStore)
		}
	}
	if efTool, ok := toolRegistry.Get(builtin.EditFileName); ok {
		if ef, ok := efTool.(*builtin.EditFileTool); ok {
			ef.SetDiffSink(fileDiffStore)
		}
	}

	toolHandler := conversation.NewToolHandler(toolRegistry, bashTimeout, userDir)
	checker := security.NewChecker(userDir)
	interceptor := security.NewInterceptor(checker)
	toolHandler.SetInterceptor(interceptor)

	execDir := userDir
	if exe, err := os.Executable(); err == nil {
		execDir = filepath.Dir(exe)
	}
	var skillReg *skill.Registry
	if cfg.Skill.IsEnabled() {
		reg, issues, loadErr := skill.LoadAll(userDir, m.baseDir, execDir, cfg.Skill.MaxSkillSizeBytes)
		if loadErr != nil {
			return nil, fmt.Errorf("load skills: %w", loadErr)
		}
		for _, iss := range issues {
			logger.Warn("skill load issue", zap.String("path", iss.Path), zap.Error(iss.Err))
		}
		skillReg = reg
		if err := toolRegistry.Register(skilladapter.NewUseSkillTool(skillReg, buildTenantSkillRootBySource(userDir, m.baseDir, execDir))); err != nil {
			logger.Warn("register use_skill failed", zap.Error(err))
		}
	}

	globalMemoryRoot := tenantMemoryRoot(m.baseDir)
	userMemoryRoot := tenantMemoryRoot(userDir)
	memoryReadStore := autolearn.NewStore(globalMemoryRoot, userMemoryRoot)
	memoryWriteStore := autolearn.NewStore(userMemoryRoot, userMemoryRoot)
	memEnabled := cfg.Memory.IsEnabled()
	memoryReviewer := autolearn.NewReviewer(provider, memoryWriteStore, autolearn.ReviewerConfig{
		Enabled:       memEnabled,
		ReviewTimeout: 60 * time.Second,
	})
	agentsSource := sources.NewAgentsMDSource()
	agentsSource.HomeDirForTest = filepath.Dir(m.baseDir)
	agentsSource.GetwdForTest = func() (string, error) { return userDir, nil }
	promptSources := []sources.Source{
		sources.NewStaticSource(),
		sources.NewEnvironmentSource(),
		agentsSource,
		sources.NewMemoryIndexSource(memoryReadStore, sources.MemoryIndexOptions{
			Enabled:  memEnabled,
			MaxLines: cfg.Memory.IndexMaxLines,
			MaxBytes: cfg.Memory.IndexMaxBytes,
		}),
	}
	if skillReg != nil {
		promptSources = append(promptSources, skillsources.NewSkillsIndexSource(skillReg))
	}
	promptSources = append(promptSources,
		sources.NewConfigAwarenessSource(),
		sources.NewCodebaseAwarenessSource(),
	)
	promptBuilder := prompt.NewBuilder(promptSources...)

	var subAgentDefinitions *definition.Registry
	var subAgentRunner *subagentruntime.Runner
	if cfg.SubAgent.IsEnabled() {
		loadOpts := definition.DefaultLoadOptions(userDir, m.baseDir, execDir, cfg.SubAgent.MaxDefinitionSizeBytes)
		defs, issues, loadErr := definition.LoadAll(loadOpts)
		if loadErr != nil {
			return nil, fmt.Errorf("load subagent definitions: %w", loadErr)
		}
		for _, iss := range issues {
			logger.Warn("subagent definition load issue", zap.String("path", iss.Path), zap.Error(iss.Err))
		}
		runner, runnerErr := subagentruntime.NewRunner(subagentruntime.RunnerConfig{
			Provider:        provider,
			ParentRegistry:  toolRegistry,
			ParentChecker:   checker,
			SubAgentConfig:  cfg.SubAgent,
			Workdir:         userDir,
			ToolTimeout:     bashTimeout,
			DefaultMaxTurns: cfg.MaxAgentLoopIterations,
			ContextWindow:   cfg.ContextWindowSize,
			SafetyMargin:    cfg.ContextSafetyMargin,
		})
		if runnerErr != nil {
			return nil, fmt.Errorf("init subagent runner: %w", runnerErr)
		}
		subAgentDefinitions = defs
		subAgentRunner = runner
	}

	handler := web.NewHandler(provider, sessMgr, cfg, defaultMaxRounds, promptBuilder, cfg.ContextWindowSize, userDir, toolRegistry, toolHandler, fileDiffStore)
	handler.SetConnMgr(m.connMgr)
	handler.SetConnSnapshotProvider(func() []*websocket.Conn {
		return m.connectionsForUser(userID)
	})
	handler.SetReviewer(memoryReviewer)
	handler.SetSkillProvider(newSkillProviderAdapter(skillReg))
	toolRegistry.MustReplace(web.NewAssociateProjectTool(userDir, handler))
	cfg.Tools.Enabled = ensureEnabledTools(cfg.Tools.Enabled, web.AssociateProjectToolName)

	slashRegistry := slash.NewRegistry()
	if err := slash.RegisterBuiltin(slashRegistry, handler); err != nil {
		return nil, fmt.Errorf("register slash commands: %w", err)
	}
	if err := slashRegistry.Register(&skilladapter.SkillsListCmd{}); err != nil {
		logger.Warn("register /skills failed", zap.Error(err))
	}
	if skillReg != nil {
		if errs := skilladapter.RegisterAll(slashRegistry, skillReg.List(), newLeadInjectorAdapter(handler)); len(errs) > 0 {
			for _, e := range errs {
				logger.Warn("register skill slash failed", zap.Error(e))
			}
		}
	}
	handler.SetSlashRegistry(newSlashAdapter(slashRegistry))

	if subAgentDefinitions != nil && subAgentRunner != nil {
		foregroundTimeout := time.Duration(cfg.SubAgent.DefaultBackgroundTimeoutSeconds) * time.Second
		subAgentBackground := background.NewManager(background.ManagerOptions{
			DefaultForegroundTimeout: foregroundTimeout,
			Notify:                   handler.HandleSubAgentTaskEvent,
		})
		handler.SetSubAgentManager(subAgentBackground)
		agentTool := subagenttool.NewAgentTool(subagenttool.AgentToolOptions{
			Definitions:       subAgentDefinitions,
			Runner:            subAgentRunner,
			BackgroundManager: subAgentBackground,
			ForkSnapshot:      handler.ForkSnapshot,
			ForegroundTimeout: foregroundTimeout,
		})
		statusTool := subagenttool.NewTaskStatusTool(subagenttool.NewBackgroundController(subAgentBackground))
		builtin.RegisterSubAgentTools(toolRegistry, agentTool, statusTool)
		cfg.Tools.Enabled = ensureEnabledTools(cfg.Tools.Enabled, subagenttool.AgentToolName, subagenttool.TaskStatusToolName)
	}

	toolResultStore := memctx.NewToolResultStore(sessMgr.SessionsRoot())
	handler.SetToolResultStore(toolResultStore)
	if cfg.Compaction.IsEnabled() {
		lightCompactor := memctx.NewLightCompactor(toolResultStore, cfg.Compaction)
		summaryCompactor := memctx.NewSummaryCompactor(sessMgr, cfg.Compaction)
		handler.SetCompactor(memctx.NewCompactor(lightCompactor, summaryCompactor, cfg.Compaction))
	}
	toolHandler.RegisterMiddleware(security.SandboxMiddleware(userDir))

	mcpPool, mcpBuild := buildTenantMCPPool(cfg)
	handler.SetMCPPool(mcpPool)
	runtimeCtx, runtimeCancel := context.WithCancel(context.Background())
	startTenantMCP(runtimeCtx, mcpPool, mcpBuild, toolRegistry, handler)

	router := web.NewRouter()
	handler.Register(router)
	projectTreeFingerprints, fpErr := projectTreeFingerprintsForUser(userDir)
	if fpErr != nil {
		logger.Warn("project tree initial fingerprint failed",
			zap.String("user_id", userID),
			zap.Error(fpErr),
		)
	}
	logger.Info("tenant runtime ready", zap.String("user_id", userID), zap.String("user_dir", userDir))
	return &tenantRuntime{
		router:                  router,
		handler:                 handler,
		fingerprint:             fingerprint,
		projectTreeFingerprints: projectTreeFingerprints,
		cancel:                  runtimeCancel,
		mcpPool:                 mcpPool,
	}, nil
}

func ensureTenantDirs(userDir string) error {
	for _, name := range []string{"", "sessions", "logs", "memory", "skills", "agents", "workspace"} {
		dir := userDir
		if name != "" {
			dir = filepath.Join(userDir, name)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	if err := ensureTenantSettingFile(filepath.Join(userDir, "setting.json")); err != nil {
		return err
	}
	return nil
}

func ensureTenantSettingFile(path string) error {
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("tenant setting path is a directory: %s", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("{}\n")
	return err
}

func tenantMemoryRoot(tenantDir string) string {
	if strings.TrimSpace(tenantDir) == "" {
		return ""
	}
	return filepath.Join(tenantDir, "memory")
}

func serverListenAddr(port int) string {
	if port <= 0 {
		port = 8969
	}
	return fmt.Sprintf("0.0.0.0:%d", port)
}

func serverAccessHint(port int) string {
	if port <= 0 {
		port = 8969
	}
	return fmt.Sprintf("http://<server-ip>:%d", port)
}

func run() error {
	if err := logger.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "[warning] logger init failed: %v\n", err)
	}
	defer logger.Sync()
	defer logger.Close()
	defer logger.CloseAllSessions()

	baseDir, err := auth.BaseDir()
	if err != nil {
		return fmt.Errorf("resolve metaatoms base dir: %w", err)
	}
	globalCfg, err := config.LoadMerged(filepath.Join(baseDir, "setting.json"), "")
	if err != nil {
		return fmt.Errorf("load global config: %w", err)
	}
	logger.Info("global config loaded", zap.String("path", filepath.Join(baseDir, "setting.json")))
	authStore, err := auth.NewStore(baseDir)
	if err != nil {
		return fmt.Errorf("init auth store: %w", err)
	}
	server := web.NewServer(serverListenAddr(globalCfg.ServerPort))
	tenants := newTenantManager(baseDir, server.ConnectionManager())
	defer tenants.CloseAll()
	server.SetTenantRouter(authStore.UserIDFromRequest, tenants.RouterForUser)
	server.AddHTTPHandlers(func(mux *http.ServeMux) {
		registerAuthAPI(mux, authStore, tenants)
	})

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tenants.StartAutoRefresh(runCtx, tenantRuntimeRefreshInterval)
	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.Start(runCtx); err != nil {
			serverErrCh <- err
		}
		close(serverErrCh)
	}()

	select {
	case <-server.Ready():
		fmt.Fprintf(os.Stdout, "[info] MetaAtoms started on %s\n", server.Addr())
		fmt.Fprintf(os.Stdout, "[info] Open %s in your browser (replace <server-ip> with this server's address)\n", serverAccessHint(globalCfg.ServerPort))
		logger.Info("MetaAtoms started",
			zap.String("listen_addr", server.Addr()),
			zap.Int("server_port", globalCfg.ServerPort),
		)
	case err := <-serverErrCh:
		if err != nil {
			return err
		}
		return nil
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", zap.String("signal", sig.String()))
	case err := <-serverErrCh:
		if err != nil {
			return err
		}
		return nil
	}
	cancel()
	if err := <-serverErrCh; err != nil {
		logger.Warn("server shutdown returned error", zap.Error(err))
	}
	return nil
}

func registerAuthAPI(mux *http.ServeMux, store *auth.Store, tenants *tenantManager) {
	type authRequest struct {
		Nickname string `json:"nickname"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	writeJSON := func(w http.ResponseWriter, status int, v interface{}) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	readReq := func(w http.ResponseWriter, r *http.Request) (authRequest, bool) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return authRequest{}, false
		}
		var req authRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return authRequest{}, false
		}
		req.Email = strings.TrimSpace(req.Email)
		return req, true
	}
	mux.HandleFunc("/api/register", func(w http.ResponseWriter, r *http.Request) {
		req, ok := readReq(w, r)
		if !ok {
			return
		}
		user, err := store.Register(req.Nickname, req.Email, req.Password)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		token, _, err := store.Login(req.Email, req.Password)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		auth.SetSessionCookie(w, token)
		writeJSON(w, http.StatusOK, map[string]interface{}{"user_id": user.UserID, "nickname": user.Nickname, "email": user.Email})
	})
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		req, ok := readReq(w, r)
		if !ok {
			return
		}
		token, user, err := store.Login(req.Email, req.Password)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		auth.SetSessionCookie(w, token)
		writeJSON(w, http.StatusOK, map[string]interface{}{"user_id": user.UserID, "nickname": user.Nickname, "email": user.Email})
	})
	mux.HandleFunc("/api/logout", func(w http.ResponseWriter, r *http.Request) {
		userID, _ := store.UserIDFromRequest(r)
		if c, err := r.Cookie(auth.CookieName); err == nil {
			store.Logout(c.Value)
		}
		if tenants != nil && userID != "" {
			tenants.CloseUser(userID)
		}
		auth.ClearSessionCookie(w)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("/api/me", func(w http.ResponseWriter, r *http.Request) {
		userID, ok := store.UserIDFromRequest(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		user, ok := store.PublicUser(userID)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		writeJSON(w, http.StatusOK, user)
	})
	mux.HandleFunc("/api/workspace/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		userID, ok := store.UserIDFromRequest(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if tenants == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "tenant manager unavailable"})
			return
		}
		project, ok := cleanWorkspaceDownloadPath(r.URL.Query().Get("path"))
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid workspace project path"})
			return
		}
		workspaceRoot := filepath.Join(auth.UserDir(tenants.baseDir, userID), "workspace")
		target := filepath.Join(workspaceRoot, project)
		if err := serveWorkspaceProjectZip(w, workspaceRoot, target, project); err != nil {
			logger.Warn("workspace project zip failed",
				zap.String("user_id", userID),
				zap.String("project", project),
				zap.Error(err),
			)
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	})
}

func cleanWorkspaceDownloadPath(raw string) (string, bool) {
	p := strings.TrimSpace(filepath.ToSlash(raw))
	p = strings.Trim(p, "/")
	if p == "" || p == "." || p == ".." || strings.Contains(p, "/") || strings.ContainsRune(p, '\x00') {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(p))
	if clean == "." || clean == ".." || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
		return "", false
	}
	return filepath.Base(clean), true
}

func serveWorkspaceProjectZip(w http.ResponseWriter, workspaceRoot, target, projectName string) error {
	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}
	absWorkspace = filepath.Clean(absWorkspace)
	if realWorkspace, err := filepath.EvalSymlinks(absWorkspace); err == nil {
		absWorkspace = filepath.Clean(realWorkspace)
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}
	realTarget = filepath.Clean(realTarget)
	if !security.IsPathInside(realTarget, absWorkspace) {
		return fmt.Errorf("project path escapes workspace")
	}
	info, err := os.Stat(realTarget)
	if err != nil {
		return fmt.Errorf("stat project: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace item is not a directory")
	}

	filename := projectName + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, strings.ReplaceAll(filename, `"`, "")))
	zw := zip.NewWriter(w)
	defer zw.Close()
	return filepath.WalkDir(realTarget, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == realTarget {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		realFile, err := filepath.EvalSymlinks(current)
		if err != nil {
			return err
		}
		if !security.IsPathInside(filepath.Clean(realFile), realTarget) {
			return nil
		}
		rel, err := filepath.Rel(realTarget, current)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(projectName, rel))
		header.Method = zip.Deflate
		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		f, err := os.Open(current)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}
