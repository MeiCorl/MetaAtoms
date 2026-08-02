package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/metaatoms/metaatoms/src/engine/conversation"
	"github.com/metaatoms/metaatoms/src/subagent/background"
	subagentruntime "github.com/metaatoms/metaatoms/src/subagent/runtime"
)

func TestTaskStatusWaitsForBatchTerminalStatus(t *testing.T) {
	manager := background.NewManager(background.ManagerOptions{})
	statusTool := NewTaskStatusTool(NewBackgroundController(manager))

	first := submitCompletingTask(t, manager, 20*time.Millisecond)
	second := submitCompletingTask(t, manager, 40*time.Millisecond)

	outText, err := statusTool.Execute(context.Background(), mustJSON(t, map[string]any{
		"task_ids":        []string{first, second},
		"wait":            true,
		"timeout_seconds": 1,
	}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if strings.Contains(outText, "secret-system") {
		t.Fatalf("task_status output leaked system prompt: %s", outText)
	}

	var out taskStatusToolOutput
	if err := json.Unmarshal([]byte(outText), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if !out.OK || !out.Complete {
		t.Fatalf("wait output = %+v, want ok complete", out)
	}
	if len(out.Tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(out.Tasks))
	}
	for _, task := range out.Tasks {
		if task.Status != background.StatusCompleted {
			t.Fatalf("task %s status = %s, want completed", task.ID, task.Status)
		}
	}
}

func TestAgentToolRunResultTraceRedactsSystemBlocks(t *testing.T) {
	result := &subagentruntime.RunResult{
		Type:       subagentruntime.SubAgentTypeDefined,
		RoleName:   "tester",
		Status:     subagentruntime.RunStatusCompleted,
		FinalText:  "{}",
		StopReason: conversation.StopReasonCompleted,
		Trace: subagentruntime.Trace{
			Type:     subagentruntime.SubAgentTypeDefined,
			RoleName: "tester",
			Status:   subagentruntime.RunStatusCompleted,
			Prompt: subagentruntime.PromptTrace{
				Type:         subagentruntime.SubAgentTypeDefined,
				RoleName:     "tester",
				Task:         "{}",
				SystemBlocks: []string{"secret-system"},
			},
		},
	}

	out := outputFromRunResult(result, "subagent-task-1")
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal output: %v", err)
	}
	if strings.Contains(string(data), "secret-system") || strings.Contains(string(data), "system_blocks") {
		t.Fatalf("agent output leaked system blocks: %s", string(data))
	}
}

func submitCompletingTask(t *testing.T, manager *background.Manager, delay time.Duration) string {
	t.Helper()
	res, err := manager.Submit(context.Background(), background.SubmitRequest{
		Type:       subagentruntime.SubAgentTypeDefined,
		RoleName:   "tester",
		Background: true,
		Prompt: subagentruntime.PromptTrace{
			Type:         subagentruntime.SubAgentTypeDefined,
			RoleName:     "tester",
			Task:         "{}",
			SystemBlocks: []string{"secret-system"},
		},
		Run: func(context.Context) (*subagentruntime.RunResult, error) {
			time.Sleep(delay)
			return &subagentruntime.RunResult{
				Type:       subagentruntime.SubAgentTypeDefined,
				RoleName:   "tester",
				Status:     subagentruntime.RunStatusCompleted,
				FinalText:  "{}",
				StopReason: conversation.StopReasonCompleted,
				Trace: subagentruntime.Trace{
					Type:     subagentruntime.SubAgentTypeDefined,
					RoleName: "tester",
					Status:   subagentruntime.RunStatusCompleted,
					Prompt: subagentruntime.PromptTrace{
						Type:         subagentruntime.SubAgentTypeDefined,
						RoleName:     "tester",
						Task:         "{}",
						SystemBlocks: []string{"secret-system"},
					},
					Output: subagentruntime.OutputTrace{Status: subagentruntime.RunStatusCompleted},
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("submit task: %v", err)
	}
	return res.Task.ID
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	return data
}
