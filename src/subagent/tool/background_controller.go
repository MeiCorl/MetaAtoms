package tool

import (
	"errors"

	"github.com/metaatoms/metaatoms/src/subagent/background"
)

var ErrBackgroundManagerMissing = errors.New("subagent tool: background manager is nil")

// BackgroundController is the narrow API that Task 6 tools can use to query
// and control process-local SubAgent tasks without depending on Manager internals.
type BackgroundController struct {
	manager *background.Manager
}

func NewBackgroundController(manager *background.Manager) *BackgroundController {
	return &BackgroundController{manager: manager}
}

type TaskStatusResponse struct {
	Found bool                    `json:"found"`
	Task  background.TaskSnapshot `json:"task,omitempty"`
}

func (c *BackgroundController) Status(id string) (TaskStatusResponse, error) {
	if c == nil || c.manager == nil {
		return TaskStatusResponse{}, ErrBackgroundManagerMissing
	}
	task, ok := c.manager.Get(id)
	return TaskStatusResponse{Found: ok, Task: task}, nil
}

func (c *BackgroundController) List() ([]background.TaskSnapshot, error) {
	if c == nil || c.manager == nil {
		return nil, ErrBackgroundManagerMissing
	}
	return c.manager.List(), nil
}

func (c *BackgroundController) MoveToBackground(id string) (background.TaskSnapshot, error) {
	if c == nil || c.manager == nil {
		return background.TaskSnapshot{}, ErrBackgroundManagerMissing
	}
	return c.manager.MoveToBackground(id)
}

func (c *BackgroundController) Cancel(id string) (background.TaskSnapshot, error) {
	if c == nil || c.manager == nil {
		return background.TaskSnapshot{}, ErrBackgroundManagerMissing
	}
	return c.manager.Cancel(id)
}
