package skill

import (
	"strings"
	"testing"
)

func TestLoadAllIncludesProductDeliveryBuiltinSkill(t *testing.T) {
	root := t.TempDir()
	reg, issues, err := LoadAll(root, root, root, 0)
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("LoadAll issues: %+v", issues)
	}

	s, ok := reg.Get("product-delivery")
	if !ok {
		t.Fatalf("missing built-in product-delivery skill")
	}
	if s.Source != SourceBuiltin {
		t.Fatalf("product-delivery source = %s, want builtin", s.Source.String())
	}
	body := s.Body()
	for _, want := range []string{"product-manager", "architecture.md", "workflow.json", "clarification_cards", "associate_project", "workspace/${project_name}/docs", "workspace/${project_name}/src"} {
		if !strings.Contains(body, want) {
			t.Fatalf("product-delivery skill body missing %q", want)
		}
	}
	if strings.Contains(body, "docs/product-delivery") {
		t.Fatalf("product-delivery skill should not use nested docs/product-delivery paths")
	}
}
