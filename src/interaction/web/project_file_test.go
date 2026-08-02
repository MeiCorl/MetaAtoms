package web

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectFileBrowserCreateEntry(t *testing.T) {
	root := t.TempDir()
	browser := NewProjectFileBrowser(root)

	fileResult, err := browser.CreateEntry("skills/demo/SKILL.md", "file")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if !fileResult.OK || fileResult.File.Type != ProjectFileTypeFile {
		t.Fatalf("create file result = %+v", fileResult)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "demo", "SKILL.md")); err != nil {
		t.Fatalf("created file missing: %v", err)
	}

	dirResult, err := browser.CreateEntry("agents/writer", "directory")
	if err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if !dirResult.OK || dirResult.File.Type != ProjectFileTypeDirectory {
		t.Fatalf("create directory result = %+v", dirResult)
	}
	if info, err := os.Stat(filepath.Join(root, "agents", "writer")); err != nil || !info.IsDir() {
		t.Fatalf("created directory missing or not dir: info=%v err=%v", info, err)
	}

	dupResult, err := browser.CreateEntry("agents/writer", "dir")
	if err != nil {
		t.Fatalf("duplicate directory should return stable result, got error: %v", err)
	}
	if dupResult.OK || dupResult.Reason != ProjectFileReasonAlreadyExists {
		t.Fatalf("duplicate result = %+v", dupResult)
	}
}

func TestFilterSettingRootEntriesKeepsOnlyCoreEntries(t *testing.T) {
	result := ProjectDirResult{
		Path: "",
		Entries: []ProjectFileEntry{
			{Name: "custom.md", Path: "custom.md", Type: ProjectFileTypeFile},
			{Name: "skills", Path: "skills", Type: ProjectFileTypeDirectory},
			{Name: "agents", Path: "agents", Type: ProjectFileTypeDirectory},
			{Name: "logs", Path: "logs", Type: ProjectFileTypeDirectory},
			{Name: "memory", Path: "memory", Type: ProjectFileTypeDirectory},
			{Name: "notes", Path: "notes", Type: ProjectFileTypeDirectory},
			{Name: "sessions", Path: "sessions", Type: ProjectFileTypeDirectory},
			{Name: "setting.json", Path: "setting.json", Type: ProjectFileTypeFile},
			{Name: "workspace", Path: "workspace", Type: ProjectFileTypeDirectory},
		},
	}

	filterSettingRootEntries(&result)

	names := make([]string, 0, len(result.Entries))
	for _, entry := range result.Entries {
		names = append(names, entry.Name)
	}
	want := []string{"setting.json", "skills", "agents", "memory"}
	if len(names) != len(want) {
		t.Fatalf("entry names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("entry names = %v, want %v", names, want)
		}
	}
}

func TestFilterSettingRootEntriesAddsSettingJSONPlaceholder(t *testing.T) {
	result := ProjectDirResult{
		Path:    "",
		Entries: []ProjectFileEntry{{Name: "custom.md", Path: "custom.md", Type: ProjectFileTypeFile}},
	}

	filterSettingRootEntries(&result)

	if len(result.Entries) != 1 {
		t.Fatalf("entries = %+v, want setting.json placeholder only", result.Entries)
	}
	if result.Entries[0].Name != "setting.json" || result.Entries[0].RenderType != ProjectRenderTypeJSON {
		t.Fatalf("first entry = %+v, want setting.json placeholder", result.Entries[0])
	}
}

func TestProjectFileBrowserCreateEntryRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	browser := NewProjectFileBrowser(root)

	result, err := browser.CreateEntry("../outside.txt", "file")
	if err == nil {
		t.Fatal("expected escaping create path to fail")
	}
	if result.Reason != ProjectFileReasonOutsideWorkdir {
		t.Fatalf("reason = %q, want %q", result.Reason, ProjectFileReasonOutsideWorkdir)
	}
	if _, statErr := os.Stat(filepath.Join(root, "..", "outside.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("outside file should not exist, stat err=%v", statErr)
	}
}

func TestProjectFileBrowserDeleteEntry(t *testing.T) {
	root := t.TempDir()
	browser := NewProjectFileBrowser(root)
	if err := os.MkdirAll(filepath.Join(root, "skills", "demo"), 0755); err != nil {
		t.Fatalf("create directory fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "demo", "SKILL.md"), []byte("# Demo"), 0644); err != nil {
		t.Fatalf("create file fixture: %v", err)
	}

	result, err := browser.DeleteEntry("skills/demo")
	if err != nil {
		t.Fatalf("delete directory: %v", err)
	}
	if !result.OK || result.File.Type != ProjectFileTypeDirectory {
		t.Fatalf("delete result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "skills", "demo")); !os.IsNotExist(err) {
		t.Fatalf("deleted directory still exists, stat err=%v", err)
	}

	rootResult, err := browser.DeleteEntry("")
	if err == nil {
		t.Fatal("expected deleting browser root to fail")
	}
	if rootResult.Reason != ProjectFileReasonInvalidPath {
		t.Fatalf("root delete reason = %q, want %q", rootResult.Reason, ProjectFileReasonInvalidPath)
	}
}
