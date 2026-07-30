package runtime

import "github.com/metaatoms/metaatoms/src/engine/conversation"

const (
	SubAgentTypeDefined = "defined"
	SubAgentTypeFork    = "fork"

	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusAborted   = "aborted"
)

// UsageSummary records cumulative model usage observed during one subagent run.
type UsageSummary struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// RunResult is the structured output returned by a subagent runner.
type RunResult struct {
	Type             string                  `json:"type"`
	RoleName         string                  `json:"role_name"`
	Status           string                  `json:"status"`
	FinalText        string                  `json:"final_text"`
	StructuredOutput map[string]any          `json:"structured_output,omitempty"`
	StopReason       conversation.StopReason `json:"stop_reason"`
	Iterations       int                     `json:"iterations"`
	ToolCalls        int                     `json:"tool_calls"`
	Usage            UsageSummary            `json:"usage"`
	Error            string                  `json:"error,omitempty"`
	Trace            Trace                   `json:"trace"`
}
