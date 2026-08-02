package tool

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/metaatoms/metaatoms/src/subagent/background"
	coretool "github.com/metaatoms/metaatoms/src/tool"
)

const TaskStatusToolName = "task_status"

type TaskStatusTool struct {
	coretool.BaseTool
	controller *BackgroundController
}

func NewTaskStatusTool(controller *BackgroundController) *TaskStatusTool {
	return &TaskStatusTool{
		BaseTool: coretool.BaseTool{
			ToolName:        TaskStatusToolName,
			ToolDescription: "Query SubAgent background task status by task_id, or wait for a batch of task_ids to reach terminal status during an orchestrated workflow.",
			ToolInputSchema: taskStatusToolSchema,
			ToolPermission:  coretool.PermRead,
		},
		controller: controller,
	}
}

type taskStatusToolInput struct {
	TaskID         string   `json:"task_id"`
	TaskIDs        []string `json:"task_ids"`
	List           bool     `json:"list"`
	Wait           bool     `json:"wait"`
	TimeoutSeconds int      `json:"timeout_seconds"`
}

type taskStatusToolOutput struct {
	OK       bool                      `json:"ok"`
	Found    bool                      `json:"found,omitempty"`
	Complete bool                      `json:"complete,omitempty"`
	Task     *background.TaskSnapshot  `json:"task,omitempty"`
	Tasks    []background.TaskSnapshot `json:"tasks,omitempty"`
	Error    string                    `json:"error,omitempty"`
}

func (t *TaskStatusTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return marshalTaskStatusOutput(taskStatusError(err.Error())), nil
	}
	var in taskStatusToolInput
	if err := json.Unmarshal(input, &in); err != nil {
		return marshalTaskStatusOutput(taskStatusError("parse task_status input: " + err.Error())), nil
	}
	if t == nil || t.controller == nil {
		return marshalTaskStatusOutput(taskStatusError(ErrBackgroundManagerMissing.Error())), nil
	}
	if in.List {
		tasks, err := t.controller.List()
		if err != nil {
			return marshalTaskStatusOutput(taskStatusError(err.Error())), nil
		}
		return marshalTaskStatusOutput(taskStatusToolOutput{OK: true, Tasks: tasks}), nil
	}
	ids := cleanTaskIDs(in.TaskID, in.TaskIDs)
	if len(ids) > 1 || (in.Wait && len(ids) > 0) {
		out := t.statusMany(ctx, ids, in.Wait, time.Duration(in.TimeoutSeconds)*time.Second)
		return marshalTaskStatusOutput(out), nil
	}
	taskID := ""
	if len(ids) == 1 {
		taskID = ids[0]
	}
	if taskID == "" {
		return marshalTaskStatusOutput(taskStatusError("task_id or task_ids must not be empty")), nil
	}
	res, err := t.controller.Status(taskID)
	if err != nil {
		return marshalTaskStatusOutput(taskStatusError(err.Error())), nil
	}
	out := taskStatusToolOutput{OK: true, Found: res.Found}
	if res.Found {
		out.Task = &res.Task
		out.Complete = isTerminalStatus(res.Task.Status)
	}
	return marshalTaskStatusOutput(out), nil
}

func (t *TaskStatusTool) statusMany(ctx context.Context, ids []string, wait bool, timeout time.Duration) taskStatusToolOutput {
	if len(ids) == 0 {
		return taskStatusError("task_ids must not be empty")
	}
	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		out := t.collectStatuses(ids)
		if !out.OK || !wait || out.Complete {
			return out
		}
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			out.OK = false
			out.Error = "wait timed out before all tasks reached terminal status"
			return out
		}
		sleep := 200 * time.Millisecond
		if !deadline.IsZero() {
			remaining := time.Until(deadline)
			if remaining < sleep {
				sleep = remaining
			}
		}
		if sleep <= 0 {
			continue
		}
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return taskStatusError(ctx.Err().Error())
		case <-timer.C:
		}
	}
}

func (t *TaskStatusTool) collectStatuses(ids []string) taskStatusToolOutput {
	tasks := make([]background.TaskSnapshot, 0, len(ids))
	for _, id := range ids {
		res, err := t.controller.Status(id)
		if err != nil {
			return taskStatusError(err.Error())
		}
		if !res.Found {
			return taskStatusToolOutput{OK: false, Found: false, Tasks: tasks, Error: "task not found: " + id}
		}
		tasks = append(tasks, res.Task)
	}
	complete := true
	for _, task := range tasks {
		if !isTerminalStatus(task.Status) {
			complete = false
			break
		}
	}
	return taskStatusToolOutput{OK: true, Found: true, Complete: complete, Tasks: tasks}
}

func cleanTaskIDs(taskID string, taskIDs []string) []string {
	seen := make(map[string]struct{}, len(taskIDs)+1)
	out := make([]string, 0, len(taskIDs)+1)
	for _, id := range append([]string{taskID}, taskIDs...) {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func isTerminalStatus(status background.TaskStatus) bool {
	switch status {
	case background.StatusCompleted, background.StatusFailed, background.StatusCanceled:
		return true
	default:
		return false
	}
}

func taskStatusError(msg string) taskStatusToolOutput {
	return taskStatusToolOutput{OK: false, Error: msg}
}

func marshalTaskStatusOutput(out taskStatusToolOutput) string {
	data, err := json.Marshal(out)
	if err != nil {
		fallback, _ := json.Marshal(taskStatusError(err.Error()))
		return string(fallback)
	}
	return string(data)
}

var taskStatusToolSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "task_id": {
      "type": "string",
      "description": "SubAgent background task id."
    },
    "task_ids": {
      "type": "array",
      "items": {"type": "string"},
      "description": "SubAgent background task ids for batch status checks or workflow waits."
    },
    "list": {
      "type": "boolean",
      "description": "When true, list all known process-local SubAgent tasks instead of querying one task id."
    },
    "wait": {
      "type": "boolean",
      "description": "When true with task_id or task_ids, block until all requested tasks reach completed, failed, or canceled."
    },
    "timeout_seconds": {
      "type": "integer",
      "description": "Maximum seconds to wait when wait=true. Omit or set 0 for no tool-level timeout beyond the execution context."
    }
  },
  "required": []
}`)
