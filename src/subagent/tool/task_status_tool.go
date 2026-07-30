package tool

import (
	"context"
	"encoding/json"
	"strings"

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
			ToolDescription: "Diagnostic/manual query for a SubAgent background task by task_id. Background SubAgent completion is delivered to the main Agent automatically, so do not poll this tool while waiting unless the user explicitly asks for status.",
			ToolInputSchema: taskStatusToolSchema,
			ToolPermission:  coretool.PermRead,
		},
		controller: controller,
	}
}

type taskStatusToolInput struct {
	TaskID string `json:"task_id"`
	List   bool   `json:"list"`
}

type taskStatusToolOutput struct {
	OK    bool                      `json:"ok"`
	Found bool                      `json:"found,omitempty"`
	Task  *background.TaskSnapshot  `json:"task,omitempty"`
	Tasks []background.TaskSnapshot `json:"tasks,omitempty"`
	Error string                    `json:"error,omitempty"`
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
	taskID := strings.TrimSpace(in.TaskID)
	if taskID == "" {
		return marshalTaskStatusOutput(taskStatusError("task_id must not be empty")), nil
	}
	res, err := t.controller.Status(taskID)
	if err != nil {
		return marshalTaskStatusOutput(taskStatusError(err.Error())), nil
	}
	out := taskStatusToolOutput{OK: true, Found: res.Found}
	if res.Found {
		out.Task = &res.Task
	}
	return marshalTaskStatusOutput(out), nil
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
      "description": "SubAgent background task id for an explicit user-requested status diagnostic."
    },
    "list": {
      "type": "boolean",
      "description": "When true, list all known process-local SubAgent tasks instead of querying one task id."
    }
  },
  "required": []
}`)
