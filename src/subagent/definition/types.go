package definition

import "strings"

const (
	CanonicalGeneralPurposeName = "general-purpose"
	LegacyGeneralPurposeName    = "gerneral-purpose"
)

// Source 表示 Agent 定义来源层级。数值越小优先级越高。
type Source int

const (
	SourceUnknown Source = iota
	SourceProject
	SourceUser
	SourceBuiltin
	SourcePlugin
)

func (s Source) String() string {
	switch s {
	case SourceProject:
		return "project"
	case SourceUser:
		return "user"
	case SourceBuiltin:
		return "builtin"
	case SourcePlugin:
		return "plugin"
	default:
		return "unknown"
	}
}

// NormalizeName 返回 Agent 名称的注册表规范形式。
func NormalizeName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == LegacyGeneralPurposeName {
		return CanonicalGeneralPurposeName
	}
	return name
}

// BackgroundPolicy 描述角色是否倾向后台执行以及角色级等待上限。
type BackgroundPolicy struct {
	Default        bool `yaml:"default" json:"default"`
	TimeoutSeconds int  `yaml:"timeout-seconds" json:"timeout_seconds"`
}

// SourceInfo 保留定义的来源和定位信息,供启动日志、UI 和错误报告使用。
type SourceInfo struct {
	Source   Source `json:"source"`
	Path     string `json:"path"`
	RootPath string `json:"root_path,omitempty"`
	Embedded bool   `json:"embedded,omitempty"`
}

// AgentDefinition 是 Markdown + YAML frontmatter 解析后的角色定义。
type AgentDefinition struct {
	Name         string           `json:"name"`
	Aliases      []string         `json:"aliases,omitempty"`
	Description  string           `json:"description"`
	AllowedTools []string         `json:"allowed_tools,omitempty"`
	DeniedTools  []string         `json:"denied_tools,omitempty"`
	Model        string           `json:"model,omitempty"`
	MaxTurns     int              `json:"max_turns,omitempty"`
	Background   BackgroundPolicy `json:"background"`
	SystemPrompt string           `json:"system_prompt"`
	SourceInfo   SourceInfo       `json:"source_info"`
}

func (d *AgentDefinition) clone() *AgentDefinition {
	if d == nil {
		return nil
	}
	cp := *d
	cp.Aliases = append([]string(nil), d.Aliases...)
	cp.AllowedTools = append([]string(nil), d.AllowedTools...)
	cp.DeniedTools = append([]string(nil), d.DeniedTools...)
	return &cp
}
