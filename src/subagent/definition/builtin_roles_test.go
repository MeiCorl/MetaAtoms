package definition

import (
	"strings"
	"testing"
)

func TestLoadAllIncludesProductDeliveryBuiltinRoles(t *testing.T) {
	reg, issues, err := LoadAll(LoadOptions{
		MaxDefinitionSizeBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("LoadAll issues: %+v", issues)
	}

	for _, name := range []string{
		"product-manager",
		"architect",
		"tech-lead",
		"engineer",
		"tester",
	} {
		def, ok := reg.Get(name)
		if !ok {
			t.Fatalf("missing built-in SubAgent role %q; got names %v", name, reg.Names())
		}
		if def.SourceInfo.Source != SourceBuiltin {
			t.Fatalf("role %q source = %s, want builtin", name, def.SourceInfo.Source.String())
		}
		if def.SystemPrompt == "" {
			t.Fatalf("role %q has empty system prompt", name)
		}
	}
}

func TestProductDeliveryBuiltinRolesIncludeStructuredTemplates(t *testing.T) {
	reg, issues, err := LoadAll(LoadOptions{
		MaxDefinitionSizeBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("LoadAll issues: %+v", issues)
	}

	cases := map[string][]string{
		"product-manager": {
			"```json",
			"## requirements.md 模板",
			"clarification_cards",
			"requirements_summary",
		},
		"architect": {
			"```json",
			"## architecture.md 模板",
			"```mermaid",
			"architecture_summary",
		},
		"tech-lead": {
			"```json",
			"## tasks.md 模板",
			"engineer_prompt",
			"estimated_session",
		},
		"engineer": {
			"```json",
			"tasks.md 状态更新样例",
			"files_changed",
			"verification",
		},
		"tester": {
			"```json",
			"## checklists.md 模板",
			"## test-report.md 模板",
			"test_summary",
		},
	}

	for name, wants := range cases {
		def, ok := reg.Get(name)
		if !ok {
			t.Fatalf("missing built-in SubAgent role %q", name)
		}
		for _, want := range wants {
			if !strings.Contains(def.SystemPrompt, want) {
				t.Fatalf("role %q prompt missing %q", name, want)
			}
		}
		if strings.Contains(def.SystemPrompt, "docs/product-delivery") {
			t.Fatalf("role %q prompt should not use nested docs/product-delivery paths", name)
		}
	}
}

func TestProductDeliveryBuiltinRoleToolPolicies(t *testing.T) {
	reg, issues, err := LoadAll(LoadOptions{
		MaxDefinitionSizeBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("LoadAll issues: %+v", issues)
	}

	for _, name := range []string{"engineer", "tester"} {
		def, ok := reg.Get(name)
		if !ok {
			t.Fatalf("missing built-in SubAgent role %q", name)
		}
		for _, want := range []string{"ReadFile", "WriteFile", "EditFile", "Glob", "Grep", "Bash"} {
			if !containsTool(def.AllowedTools, want) {
				t.Fatalf("role %q allowed-tools missing %q: %v", name, want, def.AllowedTools)
			}
		}
	}

	for _, name := range []string{"product-manager", "architect", "tech-lead"} {
		def, ok := reg.Get(name)
		if !ok {
			t.Fatalf("missing built-in SubAgent role %q", name)
		}
		if !containsTool(def.DeniedTools, "Bash") {
			t.Fatalf("role %q should deny Bash: %v", name, def.DeniedTools)
		}
	}
}

func containsTool(tools []string, want string) bool {
	for _, tool := range tools {
		if tool == want {
			return true
		}
	}
	return false
}
