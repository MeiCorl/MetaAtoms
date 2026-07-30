package runtime

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/metaatoms/metaatoms/src/config"
	"github.com/metaatoms/metaatoms/src/subagent/definition"
	"github.com/metaatoms/metaatoms/src/tool"
)

var ErrNoToolsAvailable = errors.New("subagent: no tools available after filtering")

// DefaultNestedDeniedTools 是防嵌套工具名黑名单。
//
// Task 6 会真正注册统一 Agent 工具;这里提前把常见命名都纳入防线,确保
// 后续工具名确定后仍默认不能出现在子 Agent 工具视图中。
var DefaultNestedDeniedTools = []string{
	"Agent",
	"SubAgent",
	"SubAgentStatus",
	"TaskStatus",
	"agent",
	"subagent",
	"subagent_status",
	"task_status",
}

// ToolPolicy 描述子 Agent 工具视图的收窄策略。
//
// 合并顺序固定为:
// 父工具集快照 -> 全局禁止 -> 防嵌套禁止 -> 角色白名单/黑名单 -> 后台白名单。
type ToolPolicy struct {
	GlobalDeniedTools      []string
	RoleAllowedTools       []string
	RoleDeniedTools        []string
	BackgroundAllowedTools []string
	NestedDeniedTools      []string
	Background             bool
}

// ToolView 是子 Agent 本次运行使用的冻结工具视图。
type ToolView struct {
	Registry *tool.Registry
	Tools    []tool.Tool
	Specs    []tool.ToolSpec
	Names    []string
}

// PolicyFromDefinition 从角色定义与全局 SubAgent 配置构造 ToolPolicy。
func PolicyFromDefinition(def *definition.AgentDefinition, cfg config.SubAgentConfig, background bool) ToolPolicy {
	policy := ToolPolicy{
		GlobalDeniedTools:      append([]string(nil), cfg.GlobalDeniedTools...),
		BackgroundAllowedTools: append([]string(nil), cfg.BackgroundAllowedTools...),
		Background:             background,
	}
	if def != nil {
		policy.RoleAllowedTools = append([]string(nil), def.AllowedTools...)
		policy.RoleDeniedTools = append([]string(nil), def.DeniedTools...)
	}
	return policy
}

// BuildToolView 基于父工具 Registry 的调用瞬间快照构造子 Agent 工具视图。
func BuildToolView(parent *tool.Registry, policy ToolPolicy) (*ToolView, error) {
	if parent == nil {
		return nil, fmt.Errorf("subagent: parent tool registry is nil")
	}
	tools := parent.Snapshot()
	tools = removeTools(tools, policy.GlobalDeniedTools)
	tools = removeTools(tools, nestedDeniedTools(policy.NestedDeniedTools))
	if len(cleanNames(policy.RoleAllowedTools)) > 0 {
		tools = keepTools(tools, policy.RoleAllowedTools)
	}
	tools = removeTools(tools, policy.RoleDeniedTools)
	if policy.Background && len(cleanNames(policy.BackgroundAllowedTools)) > 0 {
		tools = keepTools(tools, policy.BackgroundAllowedTools)
	}
	if len(tools) == 0 {
		return nil, ErrNoToolsAvailable
	}

	stableTools := stableToolList(tools)
	reg, err := tool.NewRegistryFromTools(stableTools)
	if err != nil {
		return nil, fmt.Errorf("subagent: build isolated tool registry: %w", err)
	}
	return &ToolView{
		Registry: reg,
		Tools:    stableTools,
		Specs:    tool.SpecsFromTools(stableTools),
		Names:    toolNames(stableTools),
	}, nil
}

func nestedDeniedTools(extra []string) []string {
	out := append([]string(nil), DefaultNestedDeniedTools...)
	out = append(out, extra...)
	return out
}

func removeTools(tools []tool.Tool, names []string) []tool.Tool {
	denied := nameSet(names)
	if len(denied) == 0 {
		return stableToolList(tools)
	}
	out := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		if _, found := denied[t.Name()]; found {
			continue
		}
		out = append(out, t)
	}
	return stableToolList(out)
}

func keepTools(tools []tool.Tool, names []string) []tool.Tool {
	allowed := nameSet(names)
	if len(allowed) == 0 {
		return stableToolList(tools)
	}
	out := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		if _, found := allowed[t.Name()]; found {
			out = append(out, t)
		}
	}
	return stableToolList(out)
}

func stableToolList(tools []tool.Tool) []tool.Tool {
	out := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		if t != nil {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

func toolNames(tools []tool.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, t := range stableToolList(tools) {
		out = append(out, t.Name())
	}
	return out
}

func nameSet(names []string) map[string]struct{} {
	cleaned := cleanNames(names)
	out := make(map[string]struct{}, len(cleaned))
	for _, name := range cleaned {
		out[name] = struct{}{}
	}
	return out
}

func cleanNames(names []string) []string {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
