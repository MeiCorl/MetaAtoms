package runtime

import (
	"time"

	"github.com/metaatoms/metaatoms/src/config"
	"github.com/metaatoms/metaatoms/src/engine/conversation"
	"github.com/metaatoms/metaatoms/src/security"
	"github.com/metaatoms/metaatoms/src/subagent/definition"
	"github.com/metaatoms/metaatoms/src/tool"
)

// IsolationConfig 描述创建子 Agent 隔离运行上下文所需的基础设施。
type IsolationConfig struct {
	ParentRegistry *tool.Registry
	ParentChecker  *security.Checker
	Definition     *definition.AgentDefinition
	SubAgentConfig config.SubAgentConfig
	Background     bool
	Workdir        string
	ToolTimeout    time.Duration
}

// IsolatedContext 是子 Agent 本次运行专属的工具与权限执行环境。
type IsolatedContext struct {
	ToolView    *ToolView
	Checker     *security.Checker
	ToolHandler *conversation.ToolHandler
}

// NewIsolatedContext 创建子 Agent 专属工具视图、权限检查器与 ToolHandler。
//
// 工具能力在 ToolView 中收窄;权限 Checker 从父会话克隆配置级策略,但不继承
// 会话级授权和一次性路径授权。沙箱和 Bash 黑名单仍在 ToolHandler 的安全链路执行。
func NewIsolatedContext(cfg IsolationConfig) (*IsolatedContext, error) {
	toolView, err := BuildToolView(cfg.ParentRegistry, PolicyFromDefinition(cfg.Definition, cfg.SubAgentConfig, cfg.Background))
	if err != nil {
		return nil, err
	}

	workdir := cfg.Workdir
	if workdir == "" && cfg.ParentChecker != nil {
		workdir = cfg.ParentChecker.Workdir()
	}

	checker := cloneChecker(cfg.ParentChecker, workdir)

	handler := conversation.NewIsolatedToolHandler(
		toolView.Registry,
		cfg.ToolTimeout,
		workdir,
		checker,
	)

	return &IsolatedContext{
		ToolView:    toolView,
		Checker:     checker,
		ToolHandler: handler,
	}, nil
}

func cloneChecker(parent *security.Checker, workdir string) *security.Checker {
	if parent != nil {
		if workdir != "" {
			return parent.CloneIsolatedForWorkdir(workdir)
		}
		return parent.CloneIsolated()
	}
	return security.NewChecker(workdir)
}
