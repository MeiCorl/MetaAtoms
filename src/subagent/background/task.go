package background

import (
	"time"

	subagentruntime "github.com/metaatoms/metaatoms/src/subagent/runtime"
)

const (
	StatusQueued    TaskStatus = "queued"
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
	StatusCanceled  TaskStatus = "canceled"
)

const (
	BackgroundReasonExplicit = "explicit"
	BackgroundReasonTimeout  = "timeout"
	BackgroundReasonManual   = "manual"
)

// TaskStatus describes the process-local lifecycle state of one subagent task.
type TaskStatus string

// TaskSnapshot is the immutable, UI-facing view of one background task.
type TaskSnapshot struct {
	ID               string                       `json:"id"`
	Type             string                       `json:"type"`
	RoleName         string                       `json:"role_name"`
	Status           TaskStatus                   `json:"status"`
	CreatedAt        time.Time                    `json:"created_at"`
	StartedAt        time.Time                    `json:"started_at,omitempty"`
	EndedAt          time.Time                    `json:"ended_at,omitempty"`
	Background       bool                         `json:"background"`
	BackgroundReason string                       `json:"background_reason,omitempty"`
	Prompt           subagentruntime.PromptTrace  `json:"prompt"`
	Output           subagentruntime.OutputTrace  `json:"output"`
	FinalText        string                       `json:"final_text,omitempty"`
	StructuredOutput map[string]any               `json:"structured_output,omitempty"`
	Error            string                       `json:"error,omitempty"`
	Iterations       int                          `json:"iterations"`
	ToolCalls        int                          `json:"tool_calls"`
	Usage            subagentruntime.UsageSummary `json:"usage"`
	Trace            *subagentruntime.Trace       `json:"trace,omitempty"`
}

func cloneSnapshot(in TaskSnapshot) TaskSnapshot {
	out := in
	out.Prompt.Metadata = cloneMap(in.Prompt.Metadata)
	out.Prompt.SystemBlocks = nil
	out.Prompt.ToolNames = append([]string(nil), in.Prompt.ToolNames...)
	out.Output.StructuredOutput = cloneMap(in.Output.StructuredOutput)
	out.StructuredOutput = cloneMap(in.StructuredOutput)
	if in.Trace != nil {
		trace := *in.Trace
		trace.Prompt.Metadata = cloneMap(in.Trace.Prompt.Metadata)
		trace.Prompt.SystemBlocks = nil
		trace.Prompt.ToolNames = append([]string(nil), in.Trace.Prompt.ToolNames...)
		trace.Output.StructuredOutput = cloneMap(in.Trace.Output.StructuredOutput)
		out.Trace = &trace
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
