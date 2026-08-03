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

	assertProductDeliveryBody(t, s.Body())
}

func TestProductDeliverySourceFileFrontmatter(t *testing.T) {
	s, err := parseFrontmatterLocal("builtin/product-delivery/SKILL.md")
	if err != nil {
		t.Fatalf("parse product-delivery source file: %v", err)
	}
	if s.Name != "product-delivery" {
		t.Fatalf("source skill name = %q, want product-delivery", s.Name)
	}
	assertProductDeliveryBody(t, s.Body())
}

func assertProductDeliveryBody(t *testing.T, body string) {
	t.Helper()
	for _, want := range []string{
		"全栈工程师",
		"产品经理能力",
		"架构师能力",
		"开发工程师能力",
		"基础验证能力",
		"统一五步流程",
		"项目初始化",
		"需求分析",
		"编码实现",
		"基础验证",
		"轻量预算",
		"零依赖文件检查",
		"不要为了验证临时安装依赖",
		"短超时",
		"architecture.md",
		"workflow.json",
		"implementation",
		"verification",
		"clarification_request",
		"clarification_cards",
		"待确认点卡牌 JSON 示例",
		"required",
		"allow_custom",
		"options",
		"recommended",
		"workflow.json.clarifications.status=waiting_user",
		"associate_project",
		"不要传 `project_path`",
		"不要在调用前创建目录",
		"工具返回的 `project.name`",
		"workspace/${project_name}/docs",
		"workspace/${project_name}/src",
		"SubAgent 系统和内置角色定义继续保留",
		"workflow_invocation",
		"disabled",
		"schema_version\": \"product-delivery/v3\"",
		"phase",
		"steps",
		"### 2. 需求分析",
		"### 3. 架构设计",
		"### 4. 编码实现",
		"### 5. 基础验证",
		"不调用 SubAgent",
		"不设置独立测试阶段",
		"UTF-8",
		"相对用户工作区的路径",
		"不要输出完整云端绝对路径",
		"右侧工作区查看",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("product-delivery skill body missing %q", want)
		}
	}
	if lines := strings.Count(body, "\n") + 1; lines > 200 {
		t.Fatalf("product-delivery skill body has %d lines, want <= 200", lines)
	}
	if strings.Contains(body, "docs/product-delivery") {
		t.Fatalf("product-delivery skill should not use nested docs/product-delivery paths")
	}
	for _, forbidden := range []string{
		"role\": \"tech-lead\"",
		"`tech-lead`",
		"tasks_path",
		"implementation_plan",
		"checklists_path",
		"test-results",
		"result_path",
		"docs/test-report.md",
		"test_plan.md",
		"测试计划",
		"编码实现与测试计划生成",
		"暂时取消测试工程师环节",
		"Agent 参数",
		"`task` 必传字段",
		"`role=product-manager`",
		"`role=tester`, `background=true`",
		"测试计划与工程实现并行",
		"等待二者都完成后再启动测试执行",
		"tester(mode=create_plan)",
		"mode=run_tests",
		"docs/test_plan/test-report.md",
		"unit_test_plan.md",
		"functional_test_plan.md",
		"smoke_test_plan.md",
		"integration_test_plan.md",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("product-delivery skill should not reference removed workflow artifact %q", forbidden)
		}
	}
}
