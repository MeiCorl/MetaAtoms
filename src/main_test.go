package main

import (
	"path/filepath"
	"testing"
)

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
