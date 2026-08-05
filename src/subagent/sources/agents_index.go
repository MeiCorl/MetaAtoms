// Package sources provides System Prompt sources for the SubAgent subsystem.
package sources

import (
	"context"
	"strconv"
	"strings"

	promptsources "github.com/metaatoms/metaatoms/src/engine/prompt/sources"
	"github.com/metaatoms/metaatoms/src/engine/prompt/tokens"
	"github.com/metaatoms/metaatoms/src/subagent/definition"
)

const (
	agentsIndexMaxLines = 240
	SourceName          = "agents_index"
)

// AgentsIndexSource injects a lightweight index of available SubAgent roles.
//
// It intentionally exposes only role metadata. The role SystemPrompt remains
// private to the child Agent runtime and is applied only when the Agent tool
// starts a SubAgent.
type AgentsIndexSource struct {
	registry *definition.Registry
	maxLines int
}

func NewAgentsIndexSource(r *definition.Registry) *AgentsIndexSource {
	return &AgentsIndexSource{
		registry: r,
		maxLines: agentsIndexMaxLines,
	}
}

func (s *AgentsIndexSource) Name() string {
	return SourceName
}

func (s *AgentsIndexSource) Assemble(_ context.Context, _ promptsources.Env) (promptsources.Section, error) {
	if s.registry == nil || s.registry.Count() == 0 {
		return promptsources.Section{
			Name:      SourceName,
			Content:   "",
			Placement: promptsources.PlacementUserMessage,
			Tokens:    0,
		}, nil
	}

	body := s.renderIndexBody()
	if strings.TrimSpace(body) == "" {
		return promptsources.Section{
			Name:      SourceName,
			Content:   "",
			Placement: promptsources.PlacementUserMessage,
			Tokens:    0,
		}, nil
	}

	body = s.truncateIndexBody(body)
	content := "<agents_index>\n" + body + "\n</agents_index>"
	return promptsources.Section{
		Name:      SourceName,
		Content:   content,
		Placement: promptsources.PlacementUserMessage,
		Tokens:    tokens.Estimate(content),
	}, nil
}

func (s *AgentsIndexSource) renderIndexBody() string {
	header := "以下是当前可用的 SubAgent 角色列表（渐进式披露：这里只提供角色摘要；执行子任务时通过 Agent 工具选择 role，不把角色 SystemPrompt 注入主上下文）："

	list := s.registry.List()
	projectAgents := filterBySource(list, definition.SourceProject)
	userAgents := filterBySource(list, definition.SourceUser)
	builtinAgents := filterBySource(list, definition.SourceBuiltin)
	pluginAgents := filterBySource(list, definition.SourcePlugin)

	var parts []string
	parts = append(parts, header)
	if block := renderSourceBlock("project", projectAgents); block != "" {
		parts = append(parts, block)
	}
	if block := renderSourceBlock("user", userAgents); block != "" {
		parts = append(parts, block)
	}
	if block := renderSourceBlock("builtin", builtinAgents); block != "" {
		parts = append(parts, block)
	}
	if block := renderSourceBlock("plugin", pluginAgents); block != "" {
		parts = append(parts, block)
	}
	return strings.Join(parts, "\n\n")
}

func filterBySource(list []*definition.AgentDefinition, source definition.Source) []*definition.AgentDefinition {
	out := make([]*definition.AgentDefinition, 0, len(list))
	for _, def := range list {
		if def != nil && def.SourceInfo.Source == source {
			out = append(out, def)
		}
	}
	return out
}

func renderSourceBlock(source string, list []*definition.AgentDefinition) string {
	if len(list) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, def := range list {
		if def == nil {
			continue
		}
		sb.WriteString("[")
		sb.WriteString(source)
		sb.WriteString("] ")
		sb.WriteString(def.Name)
		sb.WriteString("\n  描述: ")
		sb.WriteString(def.Description)
		if len(def.Aliases) > 0 {
			sb.WriteString("\n  别名: ")
			sb.WriteString(strings.Join(def.Aliases, ", "))
		}
		if len(def.AllowedTools) > 0 {
			sb.WriteString("\n  工具白名单: ")
			sb.WriteString(strings.Join(def.AllowedTools, ", "))
		}
		if len(def.DeniedTools) > 0 {
			sb.WriteString("\n  工具黑名单: ")
			sb.WriteString(strings.Join(def.DeniedTools, ", "))
		}
		if def.Model != "" {
			sb.WriteString("\n  模型: ")
			sb.WriteString(def.Model)
		}
		if def.Background.Default {
			sb.WriteString("\n  默认后台: true")
		}
		if def.Background.TimeoutSeconds > 0 {
			sb.WriteString("\n  后台等待秒数: ")
			sb.WriteString(strconv.Itoa(def.Background.TimeoutSeconds))
		}
		if def.MaxTurns > 0 {
			sb.WriteString("\n  最大轮次: ")
			sb.WriteString(strconv.Itoa(def.MaxTurns))
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (s *AgentsIndexSource) truncateIndexBody(body string) string {
	maxLines := s.maxLines
	if maxLines <= 0 {
		maxLines = agentsIndexMaxLines
	}
	lines := strings.Split(body, "\n")
	if len(lines) <= maxLines {
		return body
	}
	return strings.Join(lines[:maxLines], "\n")
}

var _ promptsources.Source = (*AgentsIndexSource)(nil)
