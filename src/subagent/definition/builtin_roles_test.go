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
			"只返回完成状态",
		},
		"architect": {
			"```json",
			"## architecture.md 模板",
			"```mermaid",
			"只返回完成状态",
		},
		"tech-lead": {
			"```json",
			"## tasks.md 模板",
			"engineer_prompt",
			"estimated_session",
		},
		"engineer": {
			"```json",
			"只返回完成状态",
			"不返回实现步骤",
			"reason",
		},
		"tester": {
			"```json",
			"## 测试计划文档模板",
			"docs/test_plan",
			"unit_test_plan.md",
			"docs/test_plan/test-report.md",
			"只返回完成状态",
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

	architect, _ := reg.Get("architect")
	for _, forbidden := range []string{"implementation_plan", "engineer_prompt"} {
		if strings.Contains(architect.SystemPrompt, forbidden) {
			t.Fatalf("architect prompt should not pre-plan engineering work with %q", forbidden)
		}
	}
	engineer, _ := reg.Get("engineer")
	for _, forbidden := range []string{"implementation_id", "assigned_item"} {
		if strings.Contains(engineer.SystemPrompt, forbidden) {
			t.Fatalf("engineer prompt should not depend on pre-assigned implementation items with %q", forbidden)
		}
	}
	for _, forbidden := range []string{"engineering_summary", "engineering_status", "files_changed", "verification"} {
		if strings.Contains(engineer.SystemPrompt, forbidden) {
			t.Fatalf("engineer prompt should not require verbose completion output %q", forbidden)
		}
	}
	tester, _ := reg.Get("tester")
	for _, forbidden := range []string{"checklists_path", "## checklists.md 模板", "test-results", "result_path"} {
		if strings.Contains(tester.SystemPrompt, forbidden) {
			t.Fatalf("tester prompt should not use shared checklist artifact %q", forbidden)
		}
	}
}

func TestEngineerSourceFileFrontmatter(t *testing.T) {
	def, err := ParseFile("../builtin/engineer.md", 64*1024)
	if err != nil {
		t.Fatalf("parse engineer source file: %v", err)
	}
	if def.Name != "engineer" {
		t.Fatalf("engineer source name = %q, want engineer", def.Name)
	}
	for _, want := range []string{"ReadFile", "WriteFile", "EditFile", "Glob", "Grep", "Bash"} {
		if !containsTool(def.AllowedTools, want) {
			t.Fatalf("engineer source allowed-tools missing %q: %v", want, def.AllowedTools)
		}
	}
	for _, want := range []string{"只返回完成状态", "不返回实现步骤", "reason"} {
		if !strings.Contains(def.SystemPrompt, want) {
			t.Fatalf("engineer source prompt missing %q", want)
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

func TestProductDeliveryBuiltinRolesRequireUTF8Communication(t *testing.T) {
	reg, issues, err := LoadAll(LoadOptions{
		MaxDefinitionSizeBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("LoadAll issues: %+v", issues)
	}

	for _, name := range []string{"product-manager", "architect", "engineer", "tester"} {
		def, ok := reg.Get(name)
		if !ok {
			t.Fatalf("missing built-in SubAgent role %q", name)
		}
		for _, want := range []string{"UTF-8", "最终 JSON", "工具参数"} {
			if !strings.Contains(def.SystemPrompt, want) {
				t.Fatalf("role %q prompt missing UTF-8 communication rule %q", name, want)
			}
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
