package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	memsession "github.com/metaatoms/metaatoms/src/memory/session"
	"github.com/metaatoms/metaatoms/src/tool"
)

const AssociateProjectToolName = "associate_project"

type projectSessionAssociator interface {
	AssociateCurrentGeneratedProject(memsession.GeneratedProject) (string, error)
}

type associateProjectInput struct {
	ProjectName  string `json:"project_name"`
	ProjectPath  string `json:"project_path,omitempty"`
	WorkflowID   string `json:"workflow_id,omitempty"`
	WorkflowPath string `json:"workflow_path,omitempty"`
}

type AssociateProjectTool struct {
	tool.BaseTool
	userDir       string
	workspaceRoot string
	associator    projectSessionAssociator
}

func NewAssociateProjectTool(userDir string, associator projectSessionAssociator) *AssociateProjectTool {
	return &AssociateProjectTool{
		BaseTool: tool.BaseTool{
			ToolName:        AssociateProjectToolName,
			ToolDescription: "Reserve and associate a generated project under the user's workspace. For a new product-delivery project, call this before writing files and omit project_path so an existing workspace/<project_name> is automatically advanced to workspace/<project_name>-2, -3, and so on. Use the returned project.name/path/workflow_path for all generated files.",
			ToolInputSchema: json.RawMessage(`{"type":"object","properties":{"project_name":{"type":"string","description":"Candidate project slug or name, for example breakout-game. When project_path is omitted, the tool returns a unique name under workspace, adding -2, -3, and so on if needed."},"project_path":{"type":"string","description":"Existing project directory to associate for resume/update flows. Omit for new projects so the tool can allocate a unique workspace/<project_name> path."},"workflow_id":{"type":"string","description":"Optional product-delivery workflow id. Defaults to the final project name when omitted."},"workflow_path":{"type":"string","description":"Optional path to docs/workflow.json. Defaults to docs/workflow.json under the final project path."}},"required":["project_name"]}`),
			ToolPermission:  tool.PermWrite,
		},
		userDir:       userDir,
		workspaceRoot: filepath.Join(userDir, "workspace"),
		associator:    associator,
	}
}

func (t *AssociateProjectTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t == nil || t.associator == nil {
		return "", errors.New("associate_project is not configured")
	}
	var in associateProjectInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("parse input: %w", err)
	}
	projectName := strings.TrimSpace(in.ProjectName)
	if projectName == "" {
		return "", errors.New("project_name cannot be empty")
	}

	projectPath, finalProjectName, err := t.resolveProjectPath(projectName, in.ProjectPath)
	if err != nil {
		return "", err
	}
	projectName = finalProjectName

	workflowID := strings.TrimSpace(in.WorkflowID)
	if workflowID == "" {
		workflowID = projectName
	}
	workflowPath := strings.TrimSpace(in.WorkflowPath)
	if workflowPath == "" {
		workflowPath = filepath.Join(projectPath, "docs", "workflow.json")
	}
	if workflowPath != "" {
		workflowPath, err = t.resolveWorkflowPath(projectPath, workflowPath)
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		return "", fmt.Errorf("reserve project directory: %w", err)
	}

	sessionID, err := t.associator.AssociateCurrentGeneratedProject(memsession.GeneratedProject{
		Name:         projectName,
		Path:         projectPath,
		WorkflowID:   workflowID,
		WorkflowPath: workflowPath,
	})
	if err != nil {
		return "", err
	}
	out := map[string]any{
		"status":     "associated",
		"session_id": sessionID,
		"project": map[string]string{
			"name":          projectName,
			"path":          projectPath,
			"workflow_id":   workflowID,
			"workflow_path": workflowPath,
		},
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *AssociateProjectTool) resolveProjectPath(projectName, rawPath string) (string, string, error) {
	if strings.TrimSpace(t.userDir) == "" {
		return "", "", errors.New("user directory is not configured")
	}
	path := strings.TrimSpace(rawPath)
	if path == "" {
		projectName = t.uniqueProjectName(projectName)
		path = filepath.Join(t.workspaceRoot, projectName)
	} else {
		path = expandHome(path)
		if !filepath.IsAbs(path) {
			clean := filepath.Clean(path)
			if clean == projectName {
				path = filepath.Join(t.workspaceRoot, clean)
			} else {
				path = filepath.Join(t.userDir, clean)
			}
		}
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve project_path: %w", err)
	}
	absWorkspace, err := filepath.Abs(t.workspaceRoot)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace root: %w", err)
	}
	if !isPathInside(absPath, absWorkspace) {
		return "", "", fmt.Errorf("project_path must be under workspace: %s", absWorkspace)
	}
	if isSamePath(absPath, absWorkspace) {
		return "", "", fmt.Errorf("project_path must point to a project directory under workspace: %s", absWorkspace)
	}
	return filepath.Clean(absPath), projectName, nil
}

func (t *AssociateProjectTool) uniqueProjectName(base string) string {
	if !pathExists(filepath.Join(t.workspaceRoot, base)) {
		return base
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("%s-%d", base, i)
		if !pathExists(filepath.Join(t.workspaceRoot, name)) {
			return name
		}
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

func (t *AssociateProjectTool) resolveWorkflowPath(projectPath, rawPath string) (string, error) {
	path := expandHome(strings.TrimSpace(rawPath))
	if !filepath.IsAbs(path) {
		clean := filepath.Clean(path)
		if clean == "workspace" || strings.HasPrefix(clean, "workspace"+string(filepath.Separator)) {
			path = filepath.Join(t.userDir, clean)
		} else {
			path = filepath.Join(projectPath, clean)
		}
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve workflow_path: %w", err)
	}
	if !isPathInside(absPath, projectPath) {
		return "", fmt.Errorf("workflow_path must be under project_path: %s", projectPath)
	}
	return filepath.Clean(absPath), nil
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func isPathInside(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func isSamePath(a, b string) bool {
	rel, err := filepath.Rel(filepath.Clean(a), filepath.Clean(b))
	return err == nil && rel == "."
}
