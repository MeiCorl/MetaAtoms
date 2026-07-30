package integration

import (
	"github.com/metaatoms/metaatoms/src/engine/conversation"
	"github.com/metaatoms/metaatoms/src/hook"
)

// WireToolHandler 把 HookEngine 注入 ToolHandler,由 ToolHandler 内部触发工具前后事件。
func WireToolHandler(engine *hook.Engine, h *conversation.ToolHandler) {
	if h == nil {
		return
	}
	h.SetHookEngine(engine)
}
