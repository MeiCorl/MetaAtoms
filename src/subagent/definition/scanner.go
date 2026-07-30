package definition

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agentbuiltin "github.com/metaatoms/metaatoms/src/subagent/builtin"
)

const (
	ProjectAgentsDir = "agents"
	UserAgentsDir    = "agents"
	BuiltinRelPath   = "subagent/builtin"
)

type LoadIssue struct {
	Path   string
	Err    error
	Source Source
}

func (i LoadIssue) Error() string {
	return fmt.Sprintf("[%s] %s: %v", i.Source.String(), i.Path, i.Err)
}

type EmbeddedDefinition struct {
	Name    string
	Path    string
	Content string
}

type LoadOptions struct {
	PluginDirs             []string
	BuiltinDirs            []string
	UserDirs               []string
	ProjectDirs            []string
	EmbeddedBuiltinEntries []EmbeddedDefinition
	MaxDefinitionSizeBytes int
}

// DefaultLoadOptions 生成源码/发布模式都可用的默认来源列表。
func DefaultLoadOptions(workdir, homeDir, execDir string, maxBytes int) LoadOptions {
	return LoadOptions{
		BuiltinDirs:            []string{filepath.Join(execDir, BuiltinRelPath)},
		UserDirs:               []string{filepath.Join(homeDir, UserAgentsDir)},
		ProjectDirs:            []string{filepath.Join(workdir, ProjectAgentsDir)},
		MaxDefinitionSizeBytes: maxBytes,
	}
}

// LoadDefaultBuiltins 从 go:embed 内置资源读取三个内置 Agent 定义。
func LoadDefaultBuiltins() ([]EmbeddedDefinition, error) {
	entries, err := agentbuiltin.Embedded()
	if err != nil {
		return nil, err
	}
	out := make([]EmbeddedDefinition, 0, len(entries))
	for _, entry := range entries {
		out = append(out, EmbeddedDefinition{Name: entry.Name, Path: entry.Path, Content: entry.Content})
	}
	return out, nil
}

// LoadAll 按 plugin -> builtin -> user -> project 顺序加载并合并 Agent 定义。
func LoadAll(opts LoadOptions) (*Registry, []LoadIssue, error) {
	reg := NewRegistry()
	var issues []LoadIssue

	if err := scanDirs(reg, opts.PluginDirs, SourcePlugin, opts.MaxDefinitionSizeBytes, &issues); err != nil {
		return reg, issues, err
	}
	if len(opts.EmbeddedBuiltinEntries) == 0 {
		entries, err := LoadDefaultBuiltins()
		if err != nil {
			issues = append(issues, LoadIssue{Path: agentbuiltin.EmbeddedRoot, Err: err, Source: SourceBuiltin})
		} else {
			opts.EmbeddedBuiltinEntries = entries
		}
	}
	if err := scanEmbedded(reg, opts.EmbeddedBuiltinEntries, opts.MaxDefinitionSizeBytes, &issues); err != nil {
		return reg, issues, err
	}
	if err := scanDirsWithOptions(reg, opts.BuiltinDirs, SourceBuiltin, opts.MaxDefinitionSizeBytes, &issues, scanOptions{SkipDuplicateSameSource: true}); err != nil {
		return reg, issues, err
	}
	if err := scanDirs(reg, opts.UserDirs, SourceUser, opts.MaxDefinitionSizeBytes, &issues); err != nil {
		return reg, issues, err
	}
	if err := scanDirs(reg, opts.ProjectDirs, SourceProject, opts.MaxDefinitionSizeBytes, &issues); err != nil {
		return reg, issues, err
	}
	return reg, issues, nil
}

type scanOptions struct {
	SkipDuplicateSameSource bool
}

func scanEmbedded(reg *Registry, entries []EmbeddedDefinition, maxBytes int, issues *[]LoadIssue) error {
	for _, entry := range entries {
		path := entry.Path
		if !strings.HasPrefix(path, "embedded://") {
			path = agentbuiltin.EmbeddedRoot + "/" + strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "/")
		}
		if maxBytes > 0 && len([]byte(entry.Content)) > maxBytes {
			*issues = append(*issues, LoadIssue{Path: path, Err: fmt.Errorf("definition size %d exceeds limit %d", len([]byte(entry.Content)), maxBytes), Source: SourceBuiltin})
			continue
		}
		d, err := ParseString(path, entry.Content)
		if err != nil {
			*issues = append(*issues, LoadIssue{Path: path, Err: err, Source: SourceBuiltin})
			continue
		}
		d.SourceInfo.Source = SourceBuiltin
		d.SourceInfo.Path = path
		d.SourceInfo.RootPath = strings.TrimSuffix(path, "/"+filepath.Base(path))
		d.SourceInfo.Embedded = true
		if err := reg.Register(d); err != nil {
			var conflict *ErrDefinitionConflict
			if errors.As(err, &conflict) {
				*issues = append(*issues, LoadIssue{Path: path, Err: conflict, Source: SourceBuiltin})
				return conflict
			}
			*issues = append(*issues, LoadIssue{Path: path, Err: err, Source: SourceBuiltin})
		}
	}
	return nil
}

func scanDirs(reg *Registry, dirs []string, src Source, maxBytes int, issues *[]LoadIssue) error {
	return scanDirsWithOptions(reg, dirs, src, maxBytes, issues, scanOptions{})
}

func scanDirsWithOptions(reg *Registry, dirs []string, src Source, maxBytes int, issues *[]LoadIssue, opts scanOptions) error {
	for _, dir := range dirs {
		if err := scanDir(reg, dir, src, maxBytes, issues, opts); err != nil {
			return err
		}
	}
	return nil
}

func scanDir(reg *Registry, dir string, src Source, maxBytes int, issues *[]LoadIssue, opts scanOptions) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		*issues = append(*issues, LoadIssue{Path: dir, Err: fmt.Errorf("stat agent definition dir: %w", err), Source: src})
		return nil
	}
	if !info.IsDir() {
		*issues = append(*issues, LoadIssue{Path: dir, Err: fmt.Errorf("agent definition source is not a directory"), Source: src})
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		*issues = append(*issues, LoadIssue{Path: dir, Err: fmt.Errorf("read agent definition dir: %w", err), Source: src})
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		d, err := ParseFile(path, maxBytes)
		if err != nil {
			*issues = append(*issues, LoadIssue{Path: path, Err: err, Source: src})
			continue
		}
		d.SourceInfo.Source = src
		d.SourceInfo.Path = path
		d.SourceInfo.RootPath = dir
		if opts.SkipDuplicateSameSource {
			if existing, ok := reg.Get(d.Name); ok && existing.SourceInfo.Source == src {
				continue
			}
		}
		if err := reg.Register(d); err != nil {
			var conflict *ErrDefinitionConflict
			if errors.As(err, &conflict) {
				*issues = append(*issues, LoadIssue{Path: path, Err: conflict, Source: src})
				return conflict
			}
			*issues = append(*issues, LoadIssue{Path: path, Err: err, Source: src})
		}
	}
	return nil
}
