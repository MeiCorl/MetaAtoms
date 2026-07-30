package prompt

import (
	"strings"

	"github.com/metaatoms/metaatoms/src/llm"
)

const subAgentRuntimeConstraints = `你正在作为子 Agent 非交互运行。

约束：
1. 只围绕本次子任务行动，不询问用户，也不要把问题交还给主 Agent。
2. 可以使用可用工具推进任务；当不再需要工具时，直接给出最终结果。
3. 最终回复应简洁、完整、可由主 Agent 直接消费。
4. 不要声称已修改主会话上下文；你的中间推理、工具结果和压缩状态只属于本次子 Agent 运行。`

const forkSubAgentConstraints = `你正在以 Fork 式子 Agent 运行。

你继承的是父会话在启动瞬间的只读历史快照和工具快照。父会话后续变化与你无关；你的中间消息、工具结果和最终回复不得回写父会话历史。`

// BuildSubAgentSystemPrompt assembles a fixed role prompt for a non-interactive
// child agent run. The role body stays cacheable and runtime constraints are
// appended as a separate stable block.
func BuildSubAgentSystemPrompt(rolePrompt string, extraConstraints ...string) llm.SystemPrompt {
	rolePrompt = strings.TrimSpace(rolePrompt)
	blocks := make([]llm.SystemBlock, 0, 2+len(extraConstraints))
	if rolePrompt != "" {
		blocks = append(blocks, llm.SystemBlock{Text: rolePrompt, Cacheable: true})
	}
	blocks = append(blocks, llm.SystemBlock{Text: subAgentRuntimeConstraints, Cacheable: true})
	for _, constraint := range extraConstraints {
		constraint = strings.TrimSpace(constraint)
		if constraint == "" {
			continue
		}
		blocks = append(blocks, llm.SystemBlock{Text: constraint, Cacheable: true})
	}
	return llm.SystemPrompt{SystemBlocks: blocks}
}

// BuildForkSubAgentSystemPrompt preserves the parent stable system prompt prefix
// and appends fork-specific child-agent constraints after it.
func BuildForkSubAgentSystemPrompt(parent llm.SystemPrompt, rolePrompt string, extraConstraints ...string) llm.SystemPrompt {
	blocks := make([]llm.SystemBlock, 0, len(parent.SystemBlocks)+3+len(extraConstraints))
	for _, block := range parent.SystemBlocks {
		if strings.TrimSpace(block.Text) == "" {
			continue
		}
		blocks = append(blocks, block)
	}
	rolePrompt = strings.TrimSpace(rolePrompt)
	if rolePrompt != "" {
		blocks = append(blocks, llm.SystemBlock{Text: rolePrompt, Cacheable: true})
	}
	blocks = append(blocks, llm.SystemBlock{Text: subAgentRuntimeConstraints, Cacheable: true})
	blocks = append(blocks, llm.SystemBlock{Text: forkSubAgentConstraints, Cacheable: true})
	for _, constraint := range extraConstraints {
		constraint = strings.TrimSpace(constraint)
		if constraint == "" {
			continue
		}
		blocks = append(blocks, llm.SystemBlock{Text: constraint, Cacheable: true})
	}
	return llm.SystemPrompt{SystemBlocks: blocks}
}
