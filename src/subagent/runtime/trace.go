package runtime

import "time"

// PromptTrace captures the exact structured input used to start a subagent.
type PromptTrace struct {
	Type               string         `json:"type"`
	RoleName           string         `json:"role_name"`
	Task               string         `json:"task"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	SystemBlocks       []string       `json:"system_blocks"`
	ToolNames          []string       `json:"tool_names"`
	Model              string         `json:"model,omitempty"`
	MaxTurns           int            `json:"max_turns"`
	Background         bool           `json:"background"`
	ParentMessageCount int            `json:"parent_message_count,omitempty"`
}

// OutputTrace captures UI-facing status and final output fields.
type OutputTrace struct {
	Status           string         `json:"status"`
	FinalText        string         `json:"final_text"`
	StructuredOutput map[string]any `json:"structured_output,omitempty"`
	StopReason       string         `json:"stop_reason"`
	Iterations       int            `json:"iterations"`
	ToolCalls        int            `json:"tool_calls"`
	Usage            UsageSummary   `json:"usage"`
	Error            string         `json:"error,omitempty"`
}

// Trace is the UI-ready record for one subagent invocation.
type Trace struct {
	Type      string      `json:"type"`
	RoleName  string      `json:"role_name"`
	Status    string      `json:"status"`
	StartedAt time.Time   `json:"started_at"`
	EndedAt   time.Time   `json:"ended_at"`
	Prompt    PromptTrace `json:"prompt"`
	Output    OutputTrace `json:"output"`
}
