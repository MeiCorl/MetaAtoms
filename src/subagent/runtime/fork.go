package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/metaatoms/metaatoms/src/engine/conversation"
	promptpkg "github.com/metaatoms/metaatoms/src/engine/prompt"
	"github.com/metaatoms/metaatoms/src/llm"
	"github.com/metaatoms/metaatoms/src/subagent/definition"
	"github.com/metaatoms/metaatoms/src/tool"
)

// ForkSnapshot captures the parent state that a fork subagent is allowed to
// inherit. All fields describe the call-time snapshot, not live parent state.
type ForkSnapshot struct {
	History      conversation.HistorySnapshot
	SystemPrompt llm.SystemPrompt
	Tools        []tool.Tool
}

// NewForkSnapshot captures parent history, stable system prompt, and tool set
// at one point in time for a future fork subagent run.
func NewForkSnapshot(parent *conversation.ConversationManager, parentSP llm.SystemPrompt, parentRegistry *tool.Registry) ForkSnapshot {
	snapshot := ForkSnapshot{SystemPrompt: cloneSystemPrompt(parentSP)}
	if parent != nil {
		snapshot.History = parent.HistorySnapshot()
	}
	if parentRegistry != nil {
		snapshot.Tools = parentRegistry.Snapshot()
	}
	return snapshot
}

// Clone returns a defensive copy of the snapshot container. Tool instances are
// intentionally reused; the registry mapping built from them is isolated.
func (s ForkSnapshot) Clone() ForkSnapshot {
	return ForkSnapshot{
		History:      s.History.Clone(),
		SystemPrompt: cloneSystemPrompt(s.SystemPrompt),
		Tools:        append([]tool.Tool(nil), s.Tools...),
	}
}

// ForkRunRequest describes one fork-based child-agent invocation.
// Background is accepted for API symmetry but fork always runs with background
// semantics when building policy and trace.
type ForkRunRequest struct {
	Definition *definition.AgentDefinition
	Task       string
	Metadata   map[string]any
	Parent     ForkSnapshot
	Background bool
	MaxTurns   int
}

// RunFork starts a child agent from a parent conversation snapshot. The parent
// history is copied into an isolated manager and the task instruction is appended
// at the tail, preserving the parent prompt prefix for prompt-cache reuse.
func (r *Runner) RunFork(ctx context.Context, req ForkRunRequest) (*RunResult, error) {
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
	parent := req.Parent.Clone()
	parentRegistry, err := r.forkParentRegistry(parent)
	if err != nil {
		return nil, err
	}
	maxTurns := r.resolveForkMaxTurns(req)

	iso, err := NewIsolatedContext(IsolationConfig{
		ParentRegistry: parentRegistry,
		ParentChecker:  r.parentChecker,
		Definition:     req.Definition,
		SubAgentConfig: r.subAgentConfig,
		Background:     true,
		Workdir:        r.workdir,
		ToolTimeout:    r.toolTimeout,
	})
	if err != nil {
		return nil, err
	}
	if r.hookEngine != nil && iso.ToolHandler != nil {
		iso.ToolHandler.SetHookEngine(r.hookEngine)
	}

	sp := promptpkg.BuildForkSubAgentSystemPrompt(parent.SystemPrompt, req.Definition.SystemPrompt)
	manager := conversation.NewConversationManager(0)
	manager.SetSessionID(fmt.Sprintf("subagent-fork-%d", startedAt.UnixNano()))
	manager.SetContextWindowSize(r.contextWindow)
	manager.SetLeadUserMessage(parent.History.LeadUserMessage)
	parentMessages := sanitizeForkHistory(parent.History.Messages)
	manager.Reset(parentMessages)
	if r.hookEngine != nil {
		manager.SetHookEngine(r.hookEngine, r.workdir)
	}
	manager.AddUserMessage(task)

	trace := Trace{
		Type:      SubAgentTypeFork,
		RoleName:  req.Definition.Name,
		Status:    RunStatusCompleted,
		StartedAt: startedAt,
		Prompt: PromptTrace{
			Type:               SubAgentTypeFork,
			RoleName:           req.Definition.Name,
			Task:               task,
			Metadata:           cloneMetadata(req.Metadata),
			ToolNames:          append([]string(nil), iso.ToolView.Names...),
			Model:              req.Definition.Model,
			MaxTurns:           maxTurns,
			Background:         true,
			ParentMessageCount: len(parentMessages),
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
		Type:             SubAgentTypeFork,
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

func (r *Runner) forkParentRegistry(snapshot ForkSnapshot) (*tool.Registry, error) {
	if len(snapshot.Tools) > 0 {
		reg, err := tool.NewRegistryFromTools(snapshot.Tools)
		if err != nil {
			return nil, fmt.Errorf("subagent: build fork parent tool snapshot: %w", err)
		}
		return reg, nil
	}
	if r.parentRegistry == nil {
		return nil, errors.New("subagent: fork parent tool registry is nil")
	}
	return r.parentRegistry, nil
}

func (r *Runner) resolveForkMaxTurns(req ForkRunRequest) int {
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

func cloneSystemPrompt(in llm.SystemPrompt) llm.SystemPrompt {
	out := llm.SystemPrompt{
		LeadUserMessage: in.LeadUserMessage,
		TotalTokens:     in.TotalTokens,
	}
	if len(in.SystemBlocks) > 0 {
		out.SystemBlocks = append([]llm.SystemBlock(nil), in.SystemBlocks...)
	}
	if len(in.Stats) > 0 {
		out.Stats = append([]llm.SourceStat(nil), in.Stats...)
	}
	return out
}
func sanitizeForkHistory(messages []llm.Message) []llm.Message {
	cloned := conversation.CloneMessages(messages)
	validLen := 0
	for i := 0; i < len(cloned); i++ {
		required := toolUseIDs(cloned[i])
		if len(required) == 0 {
			validLen = i + 1
			continue
		}
		if i+1 >= len(cloned) || cloned[i+1].Role != llm.RoleUser || !messageHasToolResults(cloned[i+1], required) {
			break
		}
		validLen = i + 2
		i++
	}
	return cloned[:validLen]
}

func toolUseIDs(msg llm.Message) []string {
	if msg.Role != llm.RoleAssistant {
		return nil
	}
	ids := make([]string, 0)
	for _, block := range msg.Content {
		use, ok := block.(*llm.ToolUseBlock)
		if ok && use.ID != "" {
			ids = append(ids, use.ID)
		}
	}
	return ids
}

func messageHasToolResults(msg llm.Message, required []string) bool {
	if len(required) == 0 {
		return true
	}
	seen := make(map[string]bool, len(required))
	for _, block := range msg.Content {
		result, ok := block.(*llm.ToolResultBlock)
		if ok && result.ToolUseID != "" {
			seen[result.ToolUseID] = true
		}
	}
	for _, id := range required {
		if !seen[id] {
			return false
		}
	}
	return true
}
