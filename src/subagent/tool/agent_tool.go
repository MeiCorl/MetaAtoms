package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/metaatoms/metaatoms/src/subagent/background"
	"github.com/metaatoms/metaatoms/src/subagent/definition"
	subagentruntime "github.com/metaatoms/metaatoms/src/subagent/runtime"
	coretool "github.com/metaatoms/metaatoms/src/tool"
)

const AgentToolName = "Agent"

// ForkSnapshotProvider returns the parent state captured at the instant a fork
// subagent is requested. Task 7 wires this to the web handler/session owner.
type ForkSnapshotProvider func() subagentruntime.ForkSnapshot

// ModelValidator optionally rejects a requested model override before a run is
// submitted. A nil validator means the current provider accepts the request.
type ModelValidator func(model string) error

// AgentToolOptions wires the runtime pieces used by the unified agent tool.
type AgentToolOptions struct {
	Definitions       *definition.Registry
	Runner            *subagentruntime.Runner
	BackgroundManager *background.Manager
	ForkSnapshot      ForkSnapshotProvider
	ForegroundTimeout time.Duration
	ModelValidator    ModelValidator
}

// AgentTool exposes definition-based and fork-based subagents through one
// stable tool schema. Role definitions are data, not separate tools.
type AgentTool struct {
	coretool.BaseTool
	definitions       *definition.Registry
	runner            *subagentruntime.Runner
	manager           *background.Manager
	forkSnapshot      ForkSnapshotProvider
	foregroundTimeout time.Duration
	modelValidator    ModelValidator
}

func NewAgentTool(opts AgentToolOptions) *AgentTool {
	return &AgentTool{
		BaseTool: coretool.BaseTool{
			ToolName:        AgentToolName,
			ToolDescription: "Run an isolated SubAgent. Use type=defined for a role definition from a blank child conversation, or type=fork to inherit the parent conversation snapshot and run in the background. Backgrounded runs notify the main Agent automatically on completion; orchestrated workflows may use task_status with wait=true to wait for a known batch.",
			ToolInputSchema: agentToolSchema,
			ToolPermission:  coretool.PermExec,
		},
		definitions:       opts.Definitions,
		runner:            opts.Runner,
		manager:           opts.BackgroundManager,
		forkSnapshot:      opts.ForkSnapshot,
		foregroundTimeout: opts.ForegroundTimeout,
		modelValidator:    opts.ModelValidator,
	}
}

// DynamicPermission narrows the launch permission for safe, definition-based
// SubAgents. Fork and unconstrained roles stay exec because they may inherit or
// expose write/exec-capable tools.
func (t *AgentTool) DynamicPermission(input json.RawMessage) coretool.ToolPermission {
	var in agentToolInput
	if err := json.Unmarshal(input, &in); err != nil {
		return t.Permission()
	}
	runType := strings.ToLower(strings.TrimSpace(in.Type))
	if runType == "" {
		runType = subagentruntime.SubAgentTypeDefined
	}
	if runType == subagentruntime.SubAgentTypeFork {
		return coretool.PermExec
	}
	if runType != subagentruntime.SubAgentTypeDefined {
		return coretool.PermExec
	}
	role := definition.NormalizeName(in.Role)
	if role == "" {
		role = definition.CanonicalGeneralPurposeName
	}
	if t == nil || t.definitions == nil {
		return coretool.PermExec
	}
	def, ok := t.definitions.Get(role)
	if !ok || def == nil {
		return coretool.PermExec
	}
	return permissionForRoleTools(def.AllowedTools, def.DeniedTools)
}

type agentToolInput struct {
	Type                  string         `json:"type"`
	Role                  string         `json:"role"`
	Task                  string         `json:"task"`
	Metadata              map[string]any `json:"metadata"`
	Background            bool           `json:"background"`
	ForegroundWaitSeconds int            `json:"foreground_wait_seconds"`
	Model                 string         `json:"model"`
	MaxTurns              int            `json:"max_turns"`
}

type agentToolOutput struct {
	OK               bool                         `json:"ok"`
	Type             string                       `json:"type,omitempty"`
	Role             string                       `json:"role,omitempty"`
	Status           string                       `json:"status,omitempty"`
	Backgrounded     bool                         `json:"backgrounded"`
	TaskID           string                       `json:"task_id,omitempty"`
	Message          string                       `json:"message,omitempty"`
	QueryTool        string                       `json:"query_tool,omitempty"`
	QueryInput       map[string]string            `json:"query_input,omitempty"`
	Task             *background.TaskSnapshot     `json:"task,omitempty"`
	FinalText        string                       `json:"final_text,omitempty"`
	StructuredOutput map[string]any               `json:"structured_output,omitempty"`
	Trace            *subagentruntime.Trace       `json:"trace,omitempty"`
	Usage            subagentruntime.UsageSummary `json:"usage,omitempty"`
	Error            string                       `json:"error,omitempty"`
}

func (t *AgentTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var in agentToolInput
	if err := json.Unmarshal(input, &in); err != nil {
		return marshalAgentOutput(agentToolError("invalid_input", fmt.Sprintf("parse agent input: %v", err))), nil
	}
	return marshalAgentOutput(t.execute(ctx, in)), nil
}

func (t *AgentTool) execute(ctx context.Context, in agentToolInput) agentToolOutput {
	if err := ctx.Err(); err != nil {
		return agentToolError("canceled", err.Error())
	}
	if t == nil || t.definitions == nil {
		return agentToolError("not_configured", "agent definition registry is nil")
	}
	if t.runner == nil {
		return agentToolError("not_configured", "subagent runner is nil")
	}
	if t.manager == nil {
		return agentToolError("not_configured", "subagent background manager is nil")
	}

	runType := strings.ToLower(strings.TrimSpace(in.Type))
	if runType == "" {
		runType = subagentruntime.SubAgentTypeDefined
	}
	if runType != subagentruntime.SubAgentTypeDefined && runType != subagentruntime.SubAgentTypeFork {
		return agentToolError("invalid_type", fmt.Sprintf("unsupported agent type %q (must be defined or fork)", in.Type))
	}

	role := definition.NormalizeName(in.Role)
	if role == "" {
		role = definition.CanonicalGeneralPurposeName
	}
	def, ok := t.definitions.Get(role)
	if !ok {
		return agentToolError("role_not_found", fmt.Sprintf("agent role %q not found; available roles: %s", role, strings.Join(t.definitions.Names(), ", ")))
	}
	task := strings.TrimSpace(in.Task)
	if task == "" {
		return agentToolError("invalid_input", "task must not be empty")
	}
	if in.Model != "" {
		if t.modelValidator != nil {
			if err := t.modelValidator(in.Model); err != nil {
				return agentToolError("model_not_supported", err.Error())
			}
		}
		def.Model = in.Model
	}

	backgroundMode := in.Background || def.Background.Default || runType == subagentruntime.SubAgentTypeFork
	foregroundTimeout := t.foregroundTimeout
	if in.ForegroundWaitSeconds > 0 {
		foregroundTimeout = time.Duration(in.ForegroundWaitSeconds) * time.Second
	}
	prompt := subagentruntime.PromptTrace{
		Type:       runType,
		RoleName:   def.Name,
		Task:       task,
		Metadata:   cloneAnyMap(in.Metadata),
		Model:      def.Model,
		MaxTurns:   requestedMaxTurns(in.MaxTurns, def.MaxTurns),
		Background: backgroundMode,
	}

	submitReq := background.SubmitRequest{
		Type:              runType,
		RoleName:          def.Name,
		Prompt:            prompt,
		Background:        backgroundMode,
		ForegroundTimeout: foregroundTimeout,
	}

	switch runType {
	case subagentruntime.SubAgentTypeDefined:
		submitReq.Run = func(runCtx context.Context) (*subagentruntime.RunResult, error) {
			return t.runner.RunDefined(runCtx, subagentruntime.DefinedRunRequest{
				Definition: def,
				Task:       task,
				Metadata:   cloneAnyMap(in.Metadata),
				Background: backgroundMode,
				MaxTurns:   in.MaxTurns,
			})
		}
	case subagentruntime.SubAgentTypeFork:
		if t.forkSnapshot == nil {
			return agentToolError("not_configured", "fork snapshot provider is nil")
		}
		parent := t.forkSnapshot()
		prompt.ParentMessageCount = len(parent.History.Messages)
		submitReq.Prompt = prompt
		submitReq.Run = func(runCtx context.Context) (*subagentruntime.RunResult, error) {
			return t.runner.RunFork(runCtx, subagentruntime.ForkRunRequest{
				Definition: def,
				Task:       task,
				Metadata:   cloneAnyMap(in.Metadata),
				Parent:     parent,
				Background: true,
				MaxTurns:   in.MaxTurns,
			})
		}
	}

	submitRes, err := t.manager.Submit(ctx, submitReq)
	if submitRes.Backgrounded {
		return agentToolOutput{
			OK:           true,
			Type:         runType,
			Role:         def.Name,
			Status:       string(submitRes.Task.Status),
			Backgrounded: true,
			TaskID:       submitRes.Task.ID,
			Message:      "SubAgent is running in the background. The result will be delivered automatically when the task finishes, fails, or is canceled. For orchestrated batch waits, call task_status with wait=true and the known task_ids.",
		}
	}
	if err != nil {
		out := agentToolError("run_failed", err.Error())
		out.Type = runType
		out.Role = def.Name
		out.Status = string(submitRes.Task.Status)
		if submitRes.Task.ID != "" {
			out.TaskID = submitRes.Task.ID
			out.Task = &submitRes.Task
		}
		if submitRes.Result != nil {
			out.FinalText = submitRes.Result.FinalText
			out.StructuredOutput = cloneAnyMap(submitRes.Result.StructuredOutput)
			out.Trace = redactedTrace(submitRes.Result.Trace)
			out.Usage = submitRes.Result.Usage
		}
		return out
	}
	if submitRes.Result == nil {
		return agentToolError("run_failed", "subagent finished without result")
	}
	return outputFromRunResult(submitRes.Result, submitRes.Task.ID)
}

func outputFromRunResult(result *subagentruntime.RunResult, taskID string) agentToolOutput {
	out := agentToolOutput{
		OK:               result.Status == subagentruntime.RunStatusCompleted,
		Type:             result.Type,
		Role:             result.RoleName,
		Status:           result.Status,
		TaskID:           taskID,
		FinalText:        result.FinalText,
		StructuredOutput: cloneAnyMap(result.StructuredOutput),
		Trace:            redactedTrace(result.Trace),
		Usage:            result.Usage,
		Error:            result.Error,
	}
	return out
}

func redactedTrace(in subagentruntime.Trace) *subagentruntime.Trace {
	out := in
	out.Prompt.SystemBlocks = nil
	out.Prompt.Metadata = cloneAnyMap(in.Prompt.Metadata)
	out.Prompt.ToolNames = append([]string(nil), in.Prompt.ToolNames...)
	out.Output.StructuredOutput = cloneAnyMap(in.Output.StructuredOutput)
	return &out
}

func permissionForRoleTools(allowedTools, deniedTools []string) coretool.ToolPermission {
	allowed := cleanToolNamesForPermission(allowedTools)
	if len(allowed) == 0 {
		return coretool.PermExec
	}
	denied := toolNameSetForPermission(deniedTools)
	perm := coretool.PermRead
	kept := 0
	for _, name := range allowed {
		if _, blocked := denied[strings.ToLower(name)]; blocked {
			continue
		}
		kept++
		perm = maxPermission(perm, permissionForKnownToolName(name))
	}
	if kept == 0 {
		return coretool.PermExec
	}
	return perm
}

func permissionForKnownToolName(name string) coretool.ToolPermission {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "readfile", "read_file", "glob", "grep", "task_status", "useskill", "use_skill":
		return coretool.PermRead
	case "writefile", "write_file", "editfile", "edit_file":
		return coretool.PermWrite
	case "bash":
		return coretool.PermExec
	default:
		return coretool.PermExec
	}
}

func maxPermission(a, b coretool.ToolPermission) coretool.ToolPermission {
	if b > a {
		return b
	}
	return a
}

func cleanToolNamesForPermission(names []string) []string {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	return out
}

func toolNameSetForPermission(names []string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}
func requestedMaxTurns(inputMax, defMax int) int {
	if inputMax > 0 {
		return inputMax
	}
	return defMax
}

func agentToolError(status, msg string) agentToolOutput {
	return agentToolOutput{OK: false, Status: status, Error: msg}
}

func marshalAgentOutput(out agentToolOutput) string {
	data, err := json.Marshal(out)
	if err != nil {
		fallback, _ := json.Marshal(agentToolError("marshal_failed", err.Error()))
		return string(fallback)
	}
	return string(data)
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

var agentToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "type": {
      "type": "string",
      "enum": ["defined", "fork"],
      "description": "SubAgent invocation type. defined starts from a blank child conversation; fork inherits a parent conversation snapshot and always runs in the background. Defaults to defined."
    },
    "role": {
      "type": "string",
      "description": "Agent role name, for example explore, plan, or general-purpose. Defaults to general-purpose."
    },
    "task": {
      "type": "string",
      "description": "The concrete task instruction to give to the SubAgent."
    },
    "metadata": {
      "type": "object",
      "description": "Structured prompt metadata for UI trace and diagnostics."
    },
    "background": {
      "type": "boolean",
      "description": "When true, return a task id immediately and continue the SubAgent in the background. The main Agent is notified automatically when the task reaches a terminal state; orchestrated workflows may wait for known batches with task_status(wait=true). Fork requests are always backgrounded."
    },
    "foreground_wait_seconds": {
      "type": "integer",
      "description": "Foreground wait limit before automatically returning a background task id. Uses the configured default when omitted."
    },
    "model": {
      "type": "string",
      "description": "Optional model override for the role. The runtime may reject unsupported models."
    },
    "max_turns": {
      "type": "integer",
      "description": "Optional maximum child AgentLoop turns for this invocation."
    }
  },
  "required": ["task"]
}`)
