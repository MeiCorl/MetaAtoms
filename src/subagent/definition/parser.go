package definition

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const frontmatterMarker = "---"

type frontmatter struct {
	Name         string           `yaml:"name"`
	Description  string           `yaml:"description"`
	AllowedTools []string         `yaml:"allowed-tools"`
	DeniedTools  []string         `yaml:"denied-tools"`
	Model        string           `yaml:"model"`
	MaxTurns     int              `yaml:"max-turns"`
	Background   BackgroundPolicy `yaml:"background"`
}

type ErrMissingFrontmatter struct{ Path string }

func (e *ErrMissingFrontmatter) Error() string {
	return fmt.Sprintf("parse %s: missing frontmatter (expected --- ... --- at top of file)", e.Path)
}

type ErrMissingField struct {
	Path  string
	Field string
}

func (e *ErrMissingField) Error() string {
	return fmt.Sprintf("parse %s: %s is required", e.Path, e.Field)
}

type ErrEmptyBody struct{ Path string }

func (e *ErrEmptyBody) Error() string {
	return fmt.Sprintf("parse %s: body system prompt is required", e.Path)
}

type ErrYAML struct {
	Path string
	Err  error
}

func (e *ErrYAML) Error() string {
	return fmt.Sprintf("parse %s: yaml parse error: %v", e.Path, e.Err)
}

func (e *ErrYAML) Unwrap() error { return e.Err }

// ParseFile 读取并解析一个 Agent Markdown 定义。
func ParseFile(path string, maxBytes int) (*AgentDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return nil, fmt.Errorf("parse %s: definition size %d exceeds limit %d", path, len(data), maxBytes)
	}
	return ParseString(path, string(data))
}

// ParseString 解析内存中的 Agent Markdown 定义。path 仅用于错误定位和 SourceInfo。
func ParseString(path, raw string) (*AgentDefinition, error) {
	fm, body, err := splitFrontmatter(raw, path)
	if err != nil {
		return nil, err
	}
	if err := validateFrontmatter(fm, path); err != nil {
		return nil, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, &ErrEmptyBody{Path: path}
	}

	name := NormalizeName(fm.Name)
	aliases := make([]string, 0, 1)
	if strings.TrimSpace(fm.Name) != name {
		aliases = append(aliases, strings.TrimSpace(fm.Name))
	}

	return &AgentDefinition{
		Name:         name,
		Aliases:      aliases,
		Description:  strings.TrimSpace(fm.Description),
		AllowedTools: cleanList(fm.AllowedTools),
		DeniedTools:  cleanList(fm.DeniedTools),
		Model:        strings.TrimSpace(fm.Model),
		MaxTurns:     fm.MaxTurns,
		Background:   fm.Background,
		SystemPrompt: body,
		SourceInfo: SourceInfo{
			Path:     path,
			RootPath: filepath.Dir(path),
		},
	}, nil
}

func splitFrontmatter(raw, path string) (frontmatter, string, error) {
	lines := strings.Split(raw, "\n")
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) || strings.TrimSpace(strings.TrimPrefix(lines[start], "\ufeff")) != frontmatterMarker {
		return frontmatter{}, "", &ErrMissingFrontmatter{Path: path}
	}

	end := -1
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == frontmatterMarker {
			end = i
			break
		}
	}
	if end == -1 {
		return frontmatter{}, "", &ErrMissingFrontmatter{Path: path}
	}

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(strings.Join(lines[start+1:end], "\n")), &fm); err != nil {
		return frontmatter{}, "", &ErrYAML{Path: path, Err: err}
	}
	body := strings.Join(lines[end+1:], "\n")
	body = strings.TrimLeft(body, "\n")
	return fm, body, nil
}

func validateFrontmatter(fm frontmatter, path string) error {
	if strings.TrimSpace(fm.Name) == "" {
		return &ErrMissingField{Path: path, Field: "name"}
	}
	if strings.TrimSpace(fm.Description) == "" {
		return &ErrMissingField{Path: path, Field: "description"}
	}
	return nil
}

func cleanList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
