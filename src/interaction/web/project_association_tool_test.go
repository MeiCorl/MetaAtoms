package web

import (
	"context"
	"encoding/json"
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
	if assoc.project.WorkflowID != "breakout-game" {
		t.Fatalf("workflow id = %q", assoc.project.WorkflowID)
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
