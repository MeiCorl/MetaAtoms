package sources

import (
	"context"
	"strings"
	"testing"
)

func TestCodebaseAwarenessSourceIncludesHookEntry(t *testing.T) {
	src := NewCodebaseAwarenessSource()
	if src.Name() != "codebase_awareness" {
		t.Fatalf("Name() = %q", src.Name())
	}

	section, err := src.Assemble(context.Background(), Env{})
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	if section.Name != "codebase_awareness" {
		t.Fatalf("section.Name = %q", section.Name)
	}
	if section.Placement != PlacementSystem {
		t.Fatalf("Placement = %v, want PlacementSystem", section.Placement)
	}
	for _, want := range []string{
		"hooks",
		"codebase-overview",
		"config-management",
		"use_skill+ReadFile",
	} {
		if !strings.Contains(section.Content, want) {
			t.Fatalf("Content missing %q: %s", want, section.Content)
		}
	}
	legacyHookSectionName := "hooks" + "_awareness"
	if strings.Contains(section.Content, legacyHookSectionName) {
		t.Fatalf("Content should not mention %s: %s", legacyHookSectionName, section.Content)
	}
}
