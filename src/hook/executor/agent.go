// Package executor implements Hook action executors.
//
// Step 12 upgrades the hook agent action from the Step 11 one-shot LLM stub to
// a narrow SubAgent adapter interface. The concrete runtime adapter is wired by
// main.go so this package does not import the SubAgent runtime and create a
// hook/runtime import cycle.
package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/metaatoms/metaatoms/src/hookcontext"
	"github.com/metaatoms/metaatoms/src/llm"
	"github.com/metaatoms/metaatoms/src/tool"
	"go.uber.org/zap"
)

const defaultHookAgentRole = "explore"

// AgentSubAgentRunner 是 Hook agent action 复用 SubAgent 的最小接口。
// 由 main.go 的适配器实现,避免 hook/executor 直接依赖 subagent/runtime。
type AgentSubAgentRunner interface {
	RunHookAgent(ctx context.Context, req HookAgentRequest) (HookAgentResult, error)
}

// HookAgentRequest 描述一次 hook agent action 对 SubAgent 的调用请求。
type HookAgentRequest struct {
	Role          string
	Prompt        string
	MaxIterations int
	AllowTools    []string
	AllowToolsSet bool
	Metadata      map[string]any
}

// HookAgentResult 是 Hook 日志所需的 SubAgent 结果投影。
type HookAgentResult struct {
	Role       string
	Status     string
	StopReason string
	FinalText  string
	Iterations int
	ToolCalls  int
	Error      string
}

// AgentConfig 是 agent action 的 type-specific 配置,对应 setting.json:
//
//	{
//	  "type": "agent",
//	  "prompt": "请检查 $TOOL_INPUT_FILE_PATH 是否有安全漏洞",
//	  "role": "explore",            // 可选;旧配置不写时默认 explore
//	  "max_iterations": 3,           // 映射为本次 SubAgent max_turns
//	  "allow_tools": ["ReadFile"],   // 映射为本次角色工具白名单;显式 [] 表示无工具
//	  "timeout": "60s"
//	}
type AgentConfig struct {
	// Prompt 为必填的 LLM 提示词,支持 $VAR 插值。
	Prompt string `json:"prompt"`
	// Role 为可选 SubAgent 角色名。旧配置未指定时默认使用 explore 这个安全只读角色。
	Role string `json:"role,omitempty"`
	// MaxIterations 映射为本次 SubAgent 最大迭代轮次。
	MaxIterations int `json:"max_iterations,omitempty"`
	// AllowTools 映射为本次角色允许工具白名单。nil 表示沿用角色定义;显式 [] 表示不暴露工具。
	AllowTools []string `json:"allow_tools,omitempty"`
	// Timeout 字符串(默认 60s)。
	Timeout string `json:"timeout,omitempty"`
}

// AgentExecutor 是 hook agent action 的执行器。它通过注入的 SubAgent 适配器执行
// 独立子任务,并保持 Step 11 约束:结果只记日志,不写回主会话 history。
type AgentExecutor struct {
	cfg AgentConfig

	provider llm.Provider
	runner   AgentSubAgentRunner
	timeout  time.Duration
}

// NewAgentExecutor 解析 raw action JSON 并构造 AgentExecutor。
// SubAgent 运行依赖由 Hook Engine 在 LoadEntries 阶段通过 SetSubAgentRunner 注入。
func NewAgentExecutor(raw json.RawMessage) (*AgentExecutor, error) {
	var cfg AgentConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("hook agent action: parse: %w", err)
	}
	timeout, err := ParseDuration(cfg.Timeout, DefaultAgentTimeout)
	if err != nil {
		return nil, fmt.Errorf("hook agent action: timeout: %w", err)
	}
	if timeout <= 0 {
		timeout = DefaultAgentTimeout
	}
	return &AgentExecutor{cfg: cfg, timeout: timeout}, nil
}

// SetProvider 保留给旧测试/装配代码的兼容 setter。Step 12 后 agent action 不再
// 直接做单轮 LLM 调用,而是通过 SetSubAgentRunner 注入的适配器执行。
func (e *AgentExecutor) SetProvider(p llm.Provider) { e.provider = p }

// SetRegistry 保留给旧测试/装配代码的兼容 setter。工具限制由 SubAgent runtime
// 按角色定义和本次 allow_tools 覆盖统一过滤。
func (e *AgentExecutor) SetRegistry(_ *tool.Registry) {}

// SetSubAgentRunner 注入 SubAgent 适配器。
func (e *AgentExecutor) SetSubAgentRunner(runner AgentSubAgentRunner) { e.runner = runner }

// Timeout 返回本执行器计算后的 timeout 值(供 Engine/统计使用)。
func (e *AgentExecutor) Timeout() time.Duration { return e.timeout }

// Type 返回 "agent"。
func (e *AgentExecutor) Type() string { return ActionTypeAgent }

// Execute 通过定义式 SubAgent 执行 hook agent action。
func (e *AgentExecutor) Execute(ctx context.Context, hookCtx *hookcontext.HookContext, vars map[string]string) error {
	rendered := hookcontext.Interpolate(e.cfg.Prompt, vars)
	if trimSpaces(rendered) == "" {
		return ErrEmptyAgentPrompt
	}

	if e.runner == nil {
		// TODO(Step 12 SubAgent compatibility): keep the Step 11 one-shot path only
		// for legacy unit tests or partially wired Engine instances. main.go wires a
		// SubAgent runner, so production hook agent actions use the runtime path below.
		return e.executeLegacySingleLLM(ctx, rendered)
	}

	role := strings.TrimSpace(e.cfg.Role)
	if role == "" {
		role = defaultHookAgentRole
	}
	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	result, err := e.runner.RunHookAgent(runCtx, HookAgentRequest{
		Role:          role,
		Prompt:        rendered,
		MaxIterations: e.cfg.MaxIterations,
		AllowTools:    append([]string(nil), e.cfg.AllowTools...),
		AllowToolsSet: e.cfg.AllowTools != nil,
		Metadata:      hookAgentMetadata(hookCtx),
	})
	logAgentResult(e.cfg, role, result, err)
	if err != nil {
		return fmt.Errorf("hook agent: subagent run: %w", err)
	}
	if result.Status == "failed" && strings.TrimSpace(result.Error) != "" {
		return fmt.Errorf("hook agent: subagent failed: %s", result.Error)
	}
	return nil
}

func (e *AgentExecutor) executeLegacySingleLLM(ctx context.Context, rendered string) error {
	if e.provider == nil {
		return &ErrNoLLMProvider{}
	}
	runCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	messages := []llm.Message{
		{
			Role: llm.RoleUser,
			Content: []llm.ContentBlock{
				llm.NewTextBlock(rendered),
			},
		},
	}
	ch, err := e.provider.StreamChat(runCtx, llm.SystemPrompt{}, messages, nil)
	if err != nil {
		return fmt.Errorf("hook agent: stream init: %w", err)
	}
	var final strings.Builder
	var streamErr error
	hasDone := false
	sawToolUse := false
	for chunk := range ch {
		if chunk.Content != "" {
			final.WriteString(chunk.Content)
		}
		if len(chunk.ToolUses) > 0 {
			sawToolUse = true
		}
		if chunk.Err != nil {
			streamErr = chunk.Err
		}
		if chunk.Done {
			hasDone = true
			break
		}
	}
	if !hasDone && streamErr == nil {
		streamErr = context.Cause(runCtx)
		if streamErr == nil {
			streamErr = errors.New("hook agent: stream closed without done chunk")
		}
	}
	zap.L().Debug("hook agent: legacy one-shot response received",
		zap.Int("max_iterations", e.cfg.MaxIterations),
		zap.Strings("allow_tools", e.cfg.AllowTools),
		zap.String("text", truncate(final.String(), 512)),
		zap.Bool("saw_tool_use", sawToolUse),
		zap.Error(streamErr),
	)
	if streamErr != nil {
		return fmt.Errorf("hook agent: stream: %w", streamErr)
	}
	return nil
}
func hookAgentMetadata(hookCtx *hookcontext.HookContext) map[string]any {
	meta := map[string]any{"source": "hook_agent_action"}
	if hookCtx == nil {
		return meta
	}
	if hookCtx.Event != "" {
		meta["hook_event"] = hookCtx.Event
	}
	if hookCtx.SessionID != "" {
		meta["session_id"] = hookCtx.SessionID
	}
	if hookCtx.ToolName != "" {
		meta["tool_name"] = hookCtx.ToolName
	}
	return meta
}

func logAgentResult(cfg AgentConfig, role string, result HookAgentResult, err error) {
	logger := zap.L()
	fields := []zap.Field{
		zap.String("role", role),
		zap.Int("max_iterations", cfg.MaxIterations),
		zap.Strings("allow_tools", cfg.AllowTools),
		zap.String("status", result.Status),
		zap.String("stop_reason", result.StopReason),
		zap.Int("iterations", result.Iterations),
		zap.Int("tool_calls", result.ToolCalls),
		zap.String("text", truncate(result.FinalText, 512)),
		zap.Error(err),
	}
	if err != nil {
		logger.Debug("hook agent: subagent returned error", fields...)
		return
	}
	logger.Debug("hook agent: subagent response received", fields...)
}
