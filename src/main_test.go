package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureTenantDirsCreatesSettingFile(t *testing.T) {
	userDir := filepath.Join(t.TempDir(), "user-123")

	if err := ensureTenantDirs(userDir); err != nil {
		t.Fatalf("ensureTenantDirs failed: %v", err)
	}

	for _, name := range []string{"sessions", "logs", "memory", "skills", "agents", "workspace"} {
		info, err := os.Stat(filepath.Join(userDir, name))
		if err != nil {
			t.Fatalf("stat %s failed: %v", name, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", name)
		}
	}

	got, err := os.ReadFile(filepath.Join(userDir, "setting.json"))
	if err != nil {
		t.Fatalf("read setting.json failed: %v", err)
	}
	if string(got) != "{}\n" {
		t.Fatalf("setting.json = %q, want %q", string(got), "{}\n")
	}
}

func TestEnsureTenantDirsPreservesExistingSettingFile(t *testing.T) {
	userDir := filepath.Join(t.TempDir(), "user-123")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatalf("mkdir user dir failed: %v", err)
	}
	settingPath := filepath.Join(userDir, "setting.json")
	want := "{\n  \"memory\": {\"enabled\": false}\n}\n"
	if err := os.WriteFile(settingPath, []byte(want), 0644); err != nil {
		t.Fatalf("write setting.json failed: %v", err)
	}

	if err := ensureTenantDirs(userDir); err != nil {
		t.Fatalf("ensureTenantDirs failed: %v", err)
	}

	got, err := os.ReadFile(settingPath)
	if err != nil {
		t.Fatalf("read setting.json failed: %v", err)
	}
	if string(got) != want {
		t.Fatalf("setting.json was overwritten: got %q, want %q", string(got), want)
	}
}

func TestTenantMemoryRootUsesTenantMemoryDirectory(t *testing.T) {
	baseDir := filepath.Join("home", ".metaatoms")
	userDir := filepath.Join(baseDir, "user-123")

	tests := []struct {
		name string
		dir  string
		want string
	}{
		{
			name: "global",
			dir:  baseDir,
			want: filepath.Join("home", ".metaatoms", "memory"),
		},
		{
			name: "user",
			dir:  userDir,
			want: filepath.Join("home", ".metaatoms", "user-123", "memory"),
		},
		{
			name: "empty",
			dir:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tenantMemoryRoot(tt.dir)
			if got != tt.want {
				t.Fatalf("tenantMemoryRoot(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}
