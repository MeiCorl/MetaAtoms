package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/metaatoms/metaatoms/src/config"
	"github.com/metaatoms/metaatoms/src/engine/conversation"
	promptpkg "github.com/metaatoms/metaatoms/src/engine/prompt"
	"github.com/metaatoms/metaatoms/src/hook"
	"github.com/metaatoms/metaatoms/src/llm"
	"github.com/metaatoms/metaatoms/src/security"
	"github.com/metaatoms/metaatoms/src/subagent/definition"
	"github.com/metaatoms/metaatoms/src/tool"
)

const defaultRunnerMaxTurns = 50

// Runner owns the dependencies needed to execute child agents in an isolated
// conversation state.
type Runner struct {
	provider       llm.Provider
	parentRegistry *tool.Registry
	parentChecker  *security.Checker
	hookEngine     *hook.Engine
	subAgentConfig config.SubAgentConfig
	workdir        string
	toolTimeout    time.Duration
	maxTurns       int
	contextWindow  int
	safetyMargin   int
}

// RunnerConfig wires the shared infrastructure reused by subagent runs.
type RunnerConfig struct {
	Provider        llm.Provider
	ParentRegistry  *tool.Registry
	ParentChecker   *security.Checker
	HookEngine      *hook.Engine
	SubAgentConfig  config.SubAgentConfig
	Workdir         string
	ToolTimeout     time.Duration
	DefaultMaxTurns int
	ContextWindow   int
	SafetyMargin    int
}

// SetHookEngine updates the hook engine used by future SubAgent runs.
// It lets main wire the runner before HookEngine exists, then complete the
// circular-looking dependency after hooks are loaded.
func (r *Runner) SetHookEngine(engine *hook.Engine) {
	if r == nil {
		return
	}
	r.hookEngine = engine
}
func NewRunner(cfg RunnerConfig) (*Runner, error) {
	if cfg.Provider == nil {
		return nil, errors.New("subagent: provider is nil")
	}
	if cfg.ParentRegistry == nil {
		return nil, errors.New("subagent: parent tool registry is nil")
	}
	maxTurns := cfg.DefaultMaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultRunnerMaxTurns
	}
	return &Runner{
		provider:       cfg.Provider,
		parentRegistry: cfg.ParentRegistry,
		parentChecker:  cfg.ParentChecker,
		hookEngine:     cfg.HookEngine,
		subAgentConfig: cfg.SubAgentConfig,
		workdir:        cfg.Workdir,
		toolTimeout:    cfg.ToolTimeout,
		maxTurns:       maxTurns,
		contextWindow:  cfg.ContextWindow,
		safetyMargin:   cfg.SafetyMargin,
	}, nil
}

// DefinedRunRequest describes one definition-based child-agent invocation.
type DefinedRunRequest struct {
	Definition *definition.AgentDefinition
	Task       string
	Metadata   map[string]any
	Background bool
	MaxTurns   int
}

// RunDefined starts a child agent from an empty conversation with the role
// definition as fixed system prompt and the task as the only user message.
func (r *Runner) RunDefined(ctx context.Context, req DefinedRunRequest) (*RunResult, error) {
	if r == nil {
		return nil, errors.New("subagent: runner is nil")
	}
	if req.Definition == nil {
		return nil, errors.New("subagent: definition is nil")
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return nil, errors.New("subagent: task is empty")
	}

	startedAt := time.Now()
	maxTurns := r.resolveMaxTurns(req)
	iso, err := NewIsolatedContext(IsolationConfig{
		ParentRegistry: r.parentRegistry,
		ParentChecker:  r.parentChecker,
		Definition:     req.Definition,
		SubAgentConfig: r.subAgentConfig,
		Background:     req.Background,
		Workdir:        r.workdir,
		ToolTimeout:    r.toolTimeout,
	})
	if err != nil {
		return nil, err
	}
	if r.hookEngine != nil && iso.ToolHandler != nil {
		iso.ToolHandler.SetHookEngine(r.hookEngine)
	}

	sp := promptpkg.BuildSubAgentSystemPrompt(req.Definition.SystemPrompt)
	manager := conversation.NewConversationManager(0)
	manager.SetSessionID(fmt.Sprintf("subagent-defined-%d", startedAt.UnixNano()))
	manager.SetContextWindowSize(r.contextWindow)
	if r.hookEngine != nil {
		manager.SetHookEngine(r.hookEngine, r.workdir)
	}
	manager.AddUserMessage(task)

	trace := Trace{
		Type:      SubAgentTypeDefined,
		RoleName:  req.Definition.Name,
		Status:    RunStatusCompleted,
		StartedAt: startedAt,
		Prompt: PromptTrace{
			Type:      SubAgentTypeDefined,
			RoleName:  req.Definition.Name,
			Task:      task,
			Metadata:  cloneMetadata(req.Metadata),
			ToolNames: append([]string(nil), iso.ToolView.Names...),
			Model:     req.Definition.Model,
			MaxTurns:  maxTurns,
		},
	}

	loopResult := manager.RunAgentLoop(ctx, r.provider, sp, iso.ToolView.Specs, iso.ToolHandler,
		conversation.AgentLoopConfig{
			MaxIterations:       maxTurns,
			ContextWindowSize:   r.contextWindow,
			ContextSafetyMargin: r.safetyMargin,
		},
		conversation.AgentLoopHooks{},
	)

	usage := usageSummary(manager.TokenUsageSnapshot())
	status := statusFromLoop(loopResult)
	errorText := ""
	if loopResult.Error != nil {
		errorText = loopResult.Error.Error()
	}
	structured := structuredOutput(loopResult, usage, errorText)

	trace.Status = status
	trace.EndedAt = time.Now()
	trace.Output = OutputTrace{
		Status:           status,
		FinalText:        loopResult.FinalText,
		StructuredOutput: structured,
		StopReason:       string(loopResult.StopReason),
		Iterations:       loopResult.Iterations,
		ToolCalls:        loopResult.TotalToolCalls,
		Usage:            usage,
		Error:            errorText,
	}

	result := &RunResult{
		Type:             SubAgentTypeDefined,
		RoleName:         req.Definition.Name,
		Status:           status,
		FinalText:        loopResult.FinalText,
		StructuredOutput: structured,
		StopReason:       loopResult.StopReason,
		Iterations:       loopResult.Iterations,
		ToolCalls:        loopResult.TotalToolCalls,
		Usage:            usage,
		Error:            errorText,
		Trace:            trace,
	}
	return result, loopResult.Error
}

func (r *Runner) resolveMaxTurns(req DefinedRunRequest) int {
	if req.MaxTurns > 0 {
		return req.MaxTurns
	}
	if req.Definition != nil && req.Definition.MaxTurns > 0 {
		return req.Definition.MaxTurns
	}
	if r.maxTurns > 0 {
		return r.maxTurns
	}
	return defaultRunnerMaxTurns
}

func usageSummary(usage llm.TokenUsage) UsageSummary {
	return UsageSummary{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.InputTokens + usage.OutputTokens,
	}
}

func statusFromLoop(result conversation.AgentLoopResult) string {
	if result.Aborted || result.StopReason == conversation.StopReasonAborted {
		return RunStatusAborted
	}
	if result.Error != nil || result.StopReason == conversation.StopReasonError {
		return RunStatusFailed
	}
	return RunStatusCompleted
}

func structuredOutput(result conversation.AgentLoopResult, usage UsageSummary, errorText string) map[string]any {
	out := map[string]any{
		"final_text":  result.FinalText,
		"stop_reason": string(result.StopReason),
		"iterations":  result.Iterations,
		"tool_calls":  result.TotalToolCalls,
		"usage":       usage,
	}
	if errorText != "" {
		out["error"] = errorText
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.FinalText)), &parsed); err == nil && parsed != nil {
		out["parsed_json"] = parsed
	}
	return out
}

func cloneMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
