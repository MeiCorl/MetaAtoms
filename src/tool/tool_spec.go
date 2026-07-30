package tool

import (
	"encoding/json"
	"fmt"
	"sort"
)

// ToolSpec 是工具的协议层描述，剥离了执行逻辑与权限分级。
//
// 设计目的：解决 llm 包 ↔ tool 包的循环依赖——
//
//	llm 包需要知道工具的 Name/Description/InputSchema 才能让 LLM 看到工具定义，
//	但不应反向依赖 Tool 接口本身（Permission/Execute 属于运行期能力）。
//
// 依赖方向：
//
//	tool 包 -> 自包含（仅 encoding/json）
//	llm 包 -> tool 包（通过 []ToolSpec 传入 Provider.StreamChat）
//
// ToolSpec 不携带 Permission/Execute，因此 llm 包无法误调执行，
// 真正的工具调用必须由 conversation manager 通过 ToolHandler 走 Registry。
type ToolSpec struct {
	// Name 为工具的 snake_case 标识，与 Tool.Name() 一致
	Name string
	// Description 为工具的 LLM 可读描述
	Description string
	// InputSchema 为工具输入参数的 JSON Schema（json.RawMessage，避免二次解析）
	InputSchema json.RawMessage
}

// SpecsFromTools 从工具快照生成稳定有序的 ToolSpec 列表。
//
// 输入切片会被复制后按 Tool.Name() 排序；nil 工具会被忽略。
// 该函数用于子 Agent 这类需要先冻结工具视图、再生成 LLM 工具描述的场景。
func SpecsFromTools(tools []Tool) []ToolSpec {
	ordered := append([]Tool(nil), tools...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i] == nil {
			return false
		}
		if ordered[j] == nil {
			return true
		}
		return ordered[i].Name() < ordered[j].Name()
	})
	out := make([]ToolSpec, 0, len(ordered))
	for _, t := range ordered {
		if t == nil {
			continue
		}
		out = append(out, ToolSpec{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	return out
}

// ToSpecs 返回按 enabled 过滤后的 ToolSpec 列表。
//
// enabled 为空时返回所有已注册工具的描述；
// 否则仅返回 enabled 中实际存在的工具（去重、按 Name 排序）。
//
// 返回的列表是稳定有序的，便于 LLM 端观察到一致的描述顺序。
// 内部复用 EnabledNames 保证与 List() / ToSpecs() 过滤语义一致。
//
// 安全防护：当 enabled 非空但匹配结果为空、且 Registry 中有工具时，
// 输出 warn 日志提示用户检查配置中的工具名是否正确（常见原因：
// snake_case vs PascalCase 大小写不匹配）。
func (r *Registry) ToSpecs(enabled []string) []ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := r.EnabledNames(enabled)
	out := make([]ToolSpec, 0, len(names))
	for _, name := range names {
		t, ok := r.tools[name]
		if !ok {
			continue
		}
		out = append(out, ToolSpec{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	// 防护日志：用户配置了白名单但零匹配，很可能工具名写错了
	if len(enabled) > 0 && len(out) == 0 && len(r.tools) > 0 {
		registered := make([]string, 0, len(r.tools))
		for name := range r.tools {
			registered = append(registered, name)
		}
		sort.Strings(registered)
		fmt.Printf("[warn] tools.enabled 配置了 %v，但未匹配到任何已注册工具。已注册工具名：%v\n",
			enabled, registered)
	}
	return out
}
