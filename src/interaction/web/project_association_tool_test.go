package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	memsession "github.com/metaatoms/metaatoms/src/memory/session"
)

type fakeProjectAssociator struct {
	sessionID string
	project   memsession.GeneratedProject
}

func (f *fakeProjectAssociator) AssociateCurrentGeneratedProject(project memsession.GeneratedProject) (string, error) {
	f.project = project
	if f.sessionID == "" {
		f.sessionID = "session-1"
	}
	return f.sessionID, nil
}

func TestAssociateProjectToolDefaultsToWorkspacePath(t *testing.T) {
	userDir := t.TempDir()
	assoc := &fakeProjectAssociator{}
	tool := NewAssociateProjectTool(userDir, assoc)

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"project_name":"breakout-game","workflow_id":"breakout-game"}`))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	wantPath := filepath.Join(userDir, "workspace", "breakout-game")
	if assoc.project.Path != wantPath {
		t.Fatalf("project path = %q, want %q", assoc.project.Path, wantPath)
	}
	if info, err := os.Stat(wantPath); err != nil || !info.IsDir() {
		t.Fatalf("reserved project directory missing: info=%v err=%v", info, err)
	}
	if assoc.project.WorkflowID != "breakout-game" {
		t.Fatalf("workflow id = %q", assoc.project.WorkflowID)
	}
	wantWorkflowPath := filepath.Join(userDir, "workspace", "breakout-game", "docs", "workflow.json")
	if assoc.project.WorkflowPath != wantWorkflowPath {
		t.Fatalf("workflow path = %q, want %q", assoc.project.WorkflowPath, wantWorkflowPath)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("output should be JSON: %s", out)
	}
}

func TestAssociateProjectToolAllocatesUniqueWorkspacePath(t *testing.T) {
	userDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(userDir, "workspace", "breakout-game"), 0755); err != nil {
		t.Fatalf("create existing project: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(userDir, "workspace", "breakout-game-2"), 0755); err != nil {
		t.Fatalf("create existing project: %v", err)
	}
	assoc := &fakeProjectAssociator{}
	tool := NewAssociateProjectTool(userDir, assoc)

	out, err := tool.Execute(context.Background(), json.RawMessage(`{"project_name":"breakout-game"}`))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	wantName := "breakout-game-3"
	wantPath := filepath.Join(userDir, "workspace", wantName)
	if assoc.project.Name != wantName {
		t.Fatalf("project name = %q, want %q", assoc.project.Name, wantName)
	}
	if assoc.project.Path != wantPath {
		t.Fatalf("project path = %q, want %q", assoc.project.Path, wantPath)
	}
	if assoc.project.WorkflowID != wantName {
		t.Fatalf("workflow id = %q, want %q", assoc.project.WorkflowID, wantName)
	}
	if assoc.project.WorkflowPath != filepath.Join(wantPath, "docs", "workflow.json") {
		t.Fatalf("workflow path = %q", assoc.project.WorkflowPath)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("output should be JSON: %s", out)
	}
}

func TestAssociateProjectToolResolvesWorkflowPathInsideProject(t *testing.T) {
	userDir := t.TempDir()
	assoc := &fakeProjectAssociator{}
	tool := NewAssociateProjectTool(userDir, assoc)

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"project_name":"breakout-game","workflow_path":"docs/workflow.json"}`))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	wantPath := filepath.Join(userDir, "workspace", "breakout-game", "docs", "workflow.json")
	if assoc.project.WorkflowPath != wantPath {
		t.Fatalf("workflow path = %q, want %q", assoc.project.WorkflowPath, wantPath)
	}
}

func TestAssociateProjectToolRejectsOutsideWorkspace(t *testing.T) {
	userDir := t.TempDir()
	tool := NewAssociateProjectTool(userDir, &fakeProjectAssociator{})

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"project_name":"bad","project_path":"../bad"}`))
	if err == nil {
		t.Fatal("expected outside workspace path to be rejected")
	}
}
