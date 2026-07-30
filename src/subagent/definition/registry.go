package definition

import (
	"fmt"
	"sort"
	"sync"
)

// ErrDefinitionConflict 表示同一来源层级存在同名 Agent 定义。
type ErrDefinitionConflict struct {
	Name           string
	ExistingSource Source
	ExistingPath   string
	IncomingPath   string
}

func (e *ErrDefinitionConflict) Error() string {
	return fmt.Sprintf("agent definition name conflict: %s (source: %s, existing: %s, incoming: %s)",
		e.Name, e.ExistingSource.String(), e.ExistingPath, e.IncomingPath)
}

// Registry 保存多来源合并后的 Agent 定义。
type Registry struct {
	mu     sync.RWMutex
	byName map[string]*AgentDefinition
	order  []string
}

func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]*AgentDefinition)}
}

func (r *Registry) Register(d *AgentDefinition) error {
	if r == nil {
		return fmt.Errorf("definition.Registry: Register called on nil Registry")
	}
	if d == nil {
		return fmt.Errorf("definition.Registry: Register called with nil AgentDefinition")
	}
	name := NormalizeName(d.Name)
	if name == "" {
		return fmt.Errorf("definition.Registry: AgentDefinition.Name must not be empty")
	}
	incoming := d.clone()
	incoming.Name = name
	incoming.SourceInfo.Source = d.SourceInfo.Source
	if incoming.SourceInfo.Source == SourceUnknown {
		return fmt.Errorf("definition.Registry: AgentDefinition.SourceInfo.Source must not be unknown")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.byName[name]; ok && existing != nil {
		if existing.SourceInfo.Source == incoming.SourceInfo.Source {
			return &ErrDefinitionConflict{
				Name:           name,
				ExistingSource: existing.SourceInfo.Source,
				ExistingPath:   existing.SourceInfo.Path,
				IncomingPath:   incoming.SourceInfo.Path,
			}
		}
		if existing.SourceInfo.Source < incoming.SourceInfo.Source {
			return nil
		}
		r.byName[name] = incoming
		return nil
	}

	r.byName[name] = incoming
	r.order = append(r.order, name)
	return nil
}

func (r *Registry) Get(name string) (*AgentDefinition, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.byName[NormalizeName(name)]
	return d.clone(), ok
}

func (r *Registry) List() []*AgentDefinition {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*AgentDefinition, 0, len(r.order))
	for _, name := range r.order {
		if d, ok := r.byName[name]; ok && d != nil {
			out = append(out, d.clone())
		}
	}
	return out
}

func (r *Registry) Names() []string {
	list := r.List()
	out := make([]string, 0, len(list))
	for _, d := range list {
		out = append(out, d.Name)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byName)
}
